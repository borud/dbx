package dbx

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io/fs"
	"strings"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/source"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/jmoiron/sqlx"
)

// errors
var (
	ErrDatabaseTooNew = errors.New("database may be newer than migrations available in this binary")
	ErrDirtyDatabase  = errors.New("database is in a dirty migration state; fix or force version before continuing")
	ErrNoDSN          = errors.New("no data source name given")
)

// OpenSQLX is a wrapper for Open that returns an *sqlx.DB rather than sql.DB
func OpenSQLX(opts ...Option) (*sqlx.DB, error) {
	config, db, err := open(opts...)
	if err != nil {
		return nil, err
	}
	return sqlx.NewDb(db, config.driverName), nil
}

// Open is a helper for opening a database and possibly applying pragmas, migrations etc.
func Open(opts ...Option) (*sql.DB, error) {
	_, db, err := open(opts...)
	return db, err
}

// open is the shared implementation for Open and OpenSQLX. It returns the
// resolved config alongside the opened database so that OpenSQLX can read
// the driver name without evaluating options twice.
func open(opts ...Option) (cfg config, _ *sql.DB, _ error) {
	cfg = defaultConfig()

	for _, opt := range opts {
		if err := opt(&cfg); err != nil {
			return cfg, nil, err
		}
	}

	if cfg.dsn == "" {
		return cfg, nil, ErrNoDSN
	}

	db, err := openDB(cfg.driverName, cfg.dsn, cfg.pragmas)
	if err != nil {
		return cfg, nil, err
	}
	if isSQLiteMemory(cfg.driverName, cfg.dsn) {
		// Each connection to an in-memory SQLite database is a separate,
		// empty database, so the pool must hold exactly one.
		db.SetMaxOpenConns(1)
	}

	if cfg.migrations != nil {
		ver, err := upMigrations(db, cfg)
		if err != nil {
			_ = db.Close()
			return cfg, nil, err
		}
		cfg.logger.Info("database migration", "version", ver)
	}
	return cfg, db, nil
}

// openDB opens the database. With pragmas, every new pooled connection runs
// them before use, and one connection is opened now so that a bad pragma
// fails here rather than on first use.
func openDB(driverName, dsn string, pragmas []string) (*sql.DB, error) {
	db, err := sql.Open(driverName, dsn)
	if err != nil {
		return nil, err
	}
	if len(pragmas) == 0 {
		return db, nil
	}

	connector, err := newConnector(db.Driver(), dsn)
	_ = db.Close()
	if err != nil {
		return nil, err
	}

	db = sql.OpenDB(pragmaConnector{Connector: connector, pragmas: pragmas})
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, err
	}

	return db, nil
}

// newConnector returns a connector for dsn on drv, the way sql.Open does.
func newConnector(drv driver.Driver, dsn string) (driver.Connector, error) {
	if dc, ok := drv.(driver.DriverContext); ok {
		return dc.OpenConnector(dsn)
	}
	return dsnConnector{driver: drv, dsn: dsn}, nil
}

// dsnConnector is the connector for a driver that does not implement
// [driver.DriverContext].
type dsnConnector struct {
	driver driver.Driver
	dsn    string
}

func (c dsnConnector) Connect(context.Context) (driver.Conn, error) {
	return c.driver.Open(c.dsn)
}

func (c dsnConnector) Driver() driver.Driver {
	return c.driver
}

// pragmaConnector runs pragmas on every connection it opens, since settings
// such as SQLite's foreign_keys and busy_timeout are per connection.
type pragmaConnector struct {
	driver.Connector
	pragmas []string
}

func (c pragmaConnector) Connect(ctx context.Context) (driver.Conn, error) {
	conn, err := c.Connector.Connect(ctx)
	if err != nil {
		return nil, err
	}

	execer, ok := conn.(driver.ExecerContext)
	if !ok {
		_ = conn.Close()
		return nil, fmt.Errorf("driver connection %T cannot execute pragmas", conn)
	}
	for _, p := range c.pragmas {
		if _, err := execer.ExecContext(ctx, p, nil); err != nil {
			_ = conn.Close()
			return nil, fmt.Errorf("pragma %q: %w", p, err)
		}
	}

	return conn, nil
}

// isSQLiteMemory reports whether dsn names an in-memory SQLite database.
func isSQLiteMemory(driverName, dsn string) bool {
	if driverName != "sqlite" && driverName != "sqlite3" {
		return false
	}
	return dsn == ":memory:" || strings.HasPrefix(dsn, "file::memory:") || strings.Contains(dsn, "mode=memory")
}

// upMigrations applies any up migrations that need to be performed and
// returns the resulting schema version.
//
// Migrations run on their own handle, closed afterwards, because some
// migrate drivers (postgres, pgx) hold a pooled connection until closed. An
// in-memory SQLite database exists only on db, so it is migrated there.
func upMigrations(db *sql.DB, cfg config) (uint, error) {
	src, err := iofs.New(cfg.migrations, cfg.migrationsPath)
	if err != nil {
		return 0, fmt.Errorf("migrations source %q: %w", cfg.migrationsPath, err)
	}

	last, hasMigrations, err := lastSourceVersion(src)
	if err != nil {
		_ = src.Close()
		return 0, fmt.Errorf("migrations source %q: %w", cfg.migrationsPath, err)
	}

	migrateDB := db
	if !isSQLiteMemory(cfg.driverName, cfg.dsn) {
		migrateDB, err = openDB(cfg.driverName, cfg.dsn, cfg.pragmas)
		if err != nil {
			_ = src.Close()
			return 0, fmt.Errorf("open migration handle: %w", err)
		}
	}

	dbDrv, drvName, err := cfg.getMigrationDriver(migrateDB)
	if err != nil {
		_ = src.Close()
		closeUnless(migrateDB, db)
		return 0, err
	}

	m, err := migrate.NewWithInstance("iofs", src, drvName, dbDrv)
	if err != nil {
		_ = src.Close()
		if migrateDB != db {
			// Closing the driver releases any connection it holds, then
			// closes migrateDB.
			_ = dbDrv.Close()
		}
		return 0, err
	}
	defer func() {
		// m.Close closes the database driver, which closes its *sql.DB.
		if migrateDB != db {
			_, _ = m.Close()
			return
		}
		_ = src.Close()
	}()

	return applyMigrations(m, last, hasMigrations)
}

// applyMigrations checks the database's version against the newest
// migration and migrates up.
func applyMigrations(m *migrate.Migrate, last uint, hasMigrations bool) (uint, error) {
	cur, dirty, err := m.Version()
	hasVersion := !errors.Is(err, migrate.ErrNilVersion)
	if err != nil && hasVersion {
		return 0, fmt.Errorf("read migration version: %w", err)
	}
	if dirty {
		return cur, fmt.Errorf("%w: version %d", ErrDirtyDatabase, cur)
	}
	if hasVersion && (!hasMigrations || cur > last) {
		return cur, fmt.Errorf("%w: database is at version %d, newest migration is %d", ErrDatabaseTooNew, cur, last)
	}
	if !hasMigrations {
		return 0, nil
	}

	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		if v, dirty, vErr := m.Version(); vErr == nil && dirty {
			return v, fmt.Errorf("%w: version %d: %w", ErrDirtyDatabase, v, err)
		}
		return 0, fmt.Errorf("running migrations: %w", err)
	}

	v, _, err := m.Version()
	if err != nil {
		return 0, fmt.Errorf("read migration version: %w", err)
	}
	return v, nil
}

// lastSourceVersion returns the newest migration version in src, and false
// if src holds no migrations.
func lastSourceVersion(src source.Driver) (uint, bool, error) {
	v, err := src.First()
	if errors.Is(err, fs.ErrNotExist) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}

	for {
		next, err := src.Next(v)
		if errors.Is(err, fs.ErrNotExist) {
			return v, true, nil
		}
		if err != nil {
			return 0, false, err
		}
		v = next
	}
}

// closeUnless closes db unless it is keep.
func closeUnless(db, keep *sql.DB) {
	if db != keep {
		_ = db.Close()
	}
}
