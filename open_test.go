package dbx_test

import (
	"database/sql"
	"os"
	"testing"

	mysql "github.com/golang-migrate/migrate/v4/database/mysql"
	pg "github.com/golang-migrate/migrate/v4/database/postgres"
	sqlserver "github.com/golang-migrate/migrate/v4/database/sqlserver"

	"github.com/borud/dbx"
	"github.com/golang-migrate/migrate/v4/database"
	"github.com/golang-migrate/migrate/v4/database/sqlite3"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite" // use the modernc SQLite library
)

// TestOpenFSMigrations tests both some pragmas and adds embedded FS migration.
func TestOpenFSMigrations(t *testing.T) {
	db, err := dbx.Open(
		dbx.WithDSN(":memory:"),
		dbx.WithDriver("sqlite"),
		dbx.WithPragmas(
			"PRAGMA foreign_keys = ON",    // turn on foreign keys
			"PRAGMA synchronous = NORMAL", // this is the appropriate setting for WAL
			"PRAGMA secure_delete = OFF",  // we do not need to overwrite deleted data with zeroes
			"PRAGMA temp_store = MEMORY",  // store any temporary tables and indices in memory
		),
		dbx.WithMigrations(migrationsFS, "testmigrations"),
	)
	require.NoError(t, err)
	require.NotNil(t, db)

	require.NoError(t, db.Close())
}

// TestOpenFilesystemMigrations shows how we can load migrations from the
// filesystem.
func TestOpenFilesystemMigrations(t *testing.T) {
	db, err := dbx.Open(
		dbx.WithDSN(":memory:"),
		dbx.WithDriver("sqlite"),
		dbx.WithMigrations(os.DirFS("testmigrations"), "."),
		dbx.WithMigrationDriver("sqlite", "sqlite3",
			func(db *sql.DB) (database.Driver, error) {
				return sqlite3.WithInstance(db, &sqlite3.Config{})
			}),
	)
	require.NoError(t, err)
	require.NotNil(t, db)

	require.NoError(t, db.Close())
}

// TestNoMigrationDrivers tests that we fail if we have `WithMigrations` but
// skip adding drivers.
func TestNoMigrationDrivers(t *testing.T) {
	db, err := dbx.Open(
		dbx.WithDSN(":memory:"),
		dbx.WithDriver("foo"),
		dbx.WithMigrations(migrationsFS, "testmigrations"),
	)
	require.Error(t, err)
	require.Nil(t, db)
}

// TestMigrationDrivers just tests that we can add some of the drivers and
// provide examples.
func TestMigrationDrivers(t *testing.T) {
	db, err := dbx.Open(
		dbx.WithDSN(":memory:"),
		dbx.WithDriver("sqlite"),
		dbx.WithMigrationDriver("sqlite", "sqlite3",
			func(db *sql.DB) (database.Driver, error) {
				return sqlite3.WithInstance(db, &sqlite3.Config{})
			}),
		dbx.WithMigrationDriver("sqlite3", "sqlite3",
			func(db *sql.DB) (database.Driver, error) {
				return sqlite3.WithInstance(db, &sqlite3.Config{})
			}),
		dbx.WithMigrationDriver("postgres", "postgres",
			func(db *sql.DB) (database.Driver, error) {
				return pg.WithInstance(db, &pg.Config{})
			}),
		dbx.WithMigrationDriver("pgx", "postgres",
			func(db *sql.DB) (database.Driver, error) {
				return pg.WithInstance(db, &pg.Config{})
			}),
		dbx.WithMigrationDriver("mysql", "mysql",
			func(db *sql.DB) (database.Driver, error) {
				return mysql.WithInstance(db, &mysql.Config{})
			}),
		dbx.WithMigrationDriver("sqlserver", "sqlserver",
			func(db *sql.DB) (database.Driver, error) {
				return sqlserver.WithInstance(db, &sqlserver.Config{})
			}),
	)
	require.NoError(t, err)
	require.NotNil(t, db)

	require.NoError(t, db.Close())
}
