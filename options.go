package dbx

import (
	"database/sql"
	"fmt"
	"io/fs"

	"github.com/golang-migrate/migrate/v4/database"
	"github.com/golang-migrate/migrate/v4/database/mysql"
	"github.com/golang-migrate/migrate/v4/database/pgx/v5"
	"github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/database/sqlite3"
)

// config is the configuration
type config struct {
	dsn              string
	driverName       string
	pragmas          []string
	migrations       fs.FS
	migrationsPath   string
	migrationDrivers map[string]DriverForFunc
}

// Option is a configuration option callback type
type Option func(*config) error

// DriverForFunc returns (driver, migrateDBName, error).
type DriverForFunc func(*sql.DB) (database.Driver, string, error)

func defaultConfig() config {
	return config{
		dsn:            "",
		driverName:     "sqlite",
		pragmas:        []string{},
		migrations:     nil,
		migrationsPath: "",
		migrationDrivers: map[string]DriverForFunc{
			"sqlite": func(db *sql.DB) (database.Driver, string, error) {
				d, err := sqlite3.WithInstance(db, &sqlite3.Config{})
				return d, "sqlite3", err
			},
			"sqlite3": func(db *sql.DB) (database.Driver, string, error) {
				d, err := sqlite3.WithInstance(db, &sqlite3.Config{})
				return d, "sqlite3", err
			},
			"postgres": func(db *sql.DB) (database.Driver, string, error) {
				d, err := postgres.WithInstance(db, &postgres.Config{})
				return d, "postgres", err
			},
			"pgx": func(db *sql.DB) (database.Driver, string, error) {
				d, err := pgx.WithInstance(db, &pgx.Config{})
				return d, "postgres", err
			},
			"mysql": func(db *sql.DB) (database.Driver, string, error) {
				d, err := mysql.WithInstance(db, &mysql.Config{})
				return d, "mysql", err
			},
		},
	}
}

// WithDSN sets the data source name
func WithDSN(dsn string) Option {
	return func(c *config) error {
		c.dsn = dsn
		return nil
	}
}

// WithMigrationDriver is provided in case you want to use SQL databases beyond
// those provided in the default config (sqlite, postgres, mysql).
func WithMigrationDriver(sqlDriverName string, migrateName string, create func(*sql.DB) (database.Driver, error)) Option {
	return func(c *config) error {
		c.migrationDrivers[sqlDriverName] = func(db *sql.DB) (database.Driver, string, error) {
			d, err := create(db)
			return d, migrateName, err
		}
		return nil
	}
}

// WithPragmas appends pragmas to the config
func WithPragmas(pragmas []string) Option {
	return func(c *config) error {
		c.pragmas = append(c.pragmas, pragmas...)
		return nil
	}
}

// WithDriver sets the driver name.
func WithDriver(driverName string) Option {
	return func(c *config) error {
		c.driverName = driverName
		return nil
	}
}

// WithMigrations sets the migrations filesystem and path within that
// filesystem.  You can either pass an embed.FS or a fs.FS for a OS filesystem
// path using os.DirFS(path).
func WithMigrations(fileSystem fs.FS, path string) Option {
	return func(c *config) error {
		c.migrations = fileSystem
		c.migrationsPath = path
		return nil
	}
}

// getMigrationDriver returns the migration driver for the configured database driver
func (c *config) getMigrationDriver(db *sql.DB) (database.Driver, string, error) {
	driverFunc, ok := c.migrationDrivers[c.driverName]
	if !ok {
		return nil, "", fmt.Errorf("no migration driver registered for database driver %q", c.driverName)
	}
	return driverFunc(db)
}
