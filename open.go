// Package dbx implements a small set of rather opinionated utilities for dealing with databases.
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
	ErrDatabaseTooNew     = errors.New("database may be newer than migrations available in this binary")
	ErrNoDSN              = errors.New("no data source name given")
	ErrNoMigrationDrivers = errors.New("no migration drivers registered")
)

// OpenSQLX is a wrapper for Open that returns an *sqlx.DB rather than sql.DB
func OpenSQLX(opts ...Option) (*sqlx.DB, error) {
	// this is a bit suboptimal, but we need to do it to get the driverName
	config := defaultConfig()
	for _, opt := range opts {
		if err := opt(&config); err != nil {
			return nil, err
		}
	}

	db, err := Open(opts...)
	if err != nil {
		return nil, err
	}
	return sqlx.NewDb(db, config.driverName), nil
}

// Open is a helper for opening a database and possibly applying pragmas, migrations etc.
func Open(opts ...Option) (*sql.DB, error) {
	config := defaultConfig()

	for _, opt := range opts {
		if err := opt(&config); err != nil {
			return nil, err
		}
	}

	if config.dsn == "" {
		return nil, ErrNoDSN
	}

	if config.migrations != nil && len(config.migrationDrivers) == 0 {
		return nil, ErrNoMigrationDrivers
	}

	db, err := sql.Open(config.driverName, config.dsn)
	if err != nil {
		return nil, err
	}

	for _, p := range config.pragmas {
		if _, err := db.Exec(p); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("pragma %q: %w", p, err)
		}
	}

	if config.migrations != nil {
		ver, dirty, err := upMigrations(db, config)
		if err != nil {
			_ = db.Close()
			if errors.Is(err, os.ErrNotExist) {
				return nil, ErrDatabaseTooNew
			}
			return nil, fmt.Errorf("running migrations: %w", err)
		}

		if dirty {
			_ = db.Close()
			return nil, errors.New("database is in a dirty migration state; fix or force version before continuing")
		}
		slog.Info("database migration", "version", ver)
	}
	return db, err
}

// upMigrations applies any up migrations that need to be performed
func upMigrations(db *sql.DB, config config) (uint, bool, error) {
	src, err := iofs.New(config.migrations, config.migrationsPath)
	if err != nil {
		return 0, false, fmt.Errorf("iofs: %w", err)
	}

	f, ok := config.migrationDrivers[config.driverName]
	if !ok {
		return 0, false, fmt.Errorf("no migrate driver function registered for sql driver %q", config.driverName)
	}

	dbDrv, drvName, err := f(db)
	if err != nil {
		return 0, false, fmt.Errorf("%s driver: %w", config.driverName, err)
	}

	m, err := migrate.NewWithInstance("iofs", src, drvName, dbDrv)
	if err != nil {
		return 0, false, err
	}

	// Run migrations. ErrNoChange is fine.
	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		// capture dirty state for caller
		if v, dirty, vErr := m.Version(); vErr == nil {
			return v, dirty, err
		}
		return 0, false, err
	}

	v, dirty, err := m.Version()
	if err == migrate.ErrNilVersion {
		// No migrations applied yet (empty dir): treat as version 0
		return 0, false, nil
	}
	return v, dirty, err
}
