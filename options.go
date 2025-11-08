package dbx

import (
	"database/sql"
	"io/fs"

	"github.com/golang-migrate/migrate/v4/database"
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
		dsn:              "",
		driverName:       "sqlite",
		pragmas:          []string{},
		migrations:       nil,
		migrationsPath:   "",
		migrationDrivers: map[string]DriverForFunc{},
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
// those provided in the default config.
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
