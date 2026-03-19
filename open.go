package dbx

import (
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"os"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/jmoiron/sqlx"
)

// errors
var (
	ErrDatabaseTooNew = errors.New("database may be newer than migrations available in this binary")
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

	db, err := sql.Open(cfg.driverName, cfg.dsn)
	if err != nil {
		return cfg, nil, err
	}

	for _, p := range cfg.pragmas {
		if _, err := db.Exec(p); err != nil {
			_ = db.Close()
			return cfg, nil, fmt.Errorf("pragma %q: %w", p, err)
		}
	}

	if cfg.migrations != nil {
		ver, dirty, err := upMigrations(db, cfg)
		if err != nil {
			_ = db.Close()
			if errors.Is(err, os.ErrNotExist) {
				return cfg, nil, ErrDatabaseTooNew
			}
			return cfg, nil, fmt.Errorf("running migrations: %w", err)
		}

		if dirty {
			_ = db.Close()
			return cfg, nil, errors.New("database is in a dirty migration state; fix or force version before continuing")
		}
		slog.Info("database migration", "version", ver)
	}
	return cfg, db, nil
}

// upMigrations applies any up migrations that need to be performed
func upMigrations(db *sql.DB, config config) (uint, bool, error) {
	src, err := iofs.New(config.migrations, config.migrationsPath)
	if err != nil {
		return 0, false, fmt.Errorf("iofs: %w", err)
	}

	// Get the migration driver for this database driver
	dbDrv, drvName, err := config.getMigrationDriver(db)
	if err != nil {
		return 0, false, err
	}

	m, err := migrate.NewWithInstance("iofs", src, drvName, dbDrv)
	if err != nil {
		return 0, false, err
	}

	// Run migrations. ErrNoChange is fine.
	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		// capture dirty state for caller
		if v, dirty, vErr := m.Version(); vErr == nil {
			return v, dirty, err
		}
		return 0, false, err
	}

	v, dirty, err := m.Version()
	if errors.Is(err, migrate.ErrNilVersion) {
		// No migrations applied yet (empty dir): treat as version 0
		return 0, false, nil
	}
	return v, dirty, err
}
