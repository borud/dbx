package dbxtest

import (
	"database/sql"
	"embed"
	"os"
	"testing"

	"github.com/borud/dbx"
	"github.com/golang-migrate/migrate/v4/database"
	"github.com/golang-migrate/migrate/v4/database/sqlite3"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite" // use the modernc SQLite library
)

//go:embed testmigrations/*.sql
var migrationsFS embed.FS

func TestOpenFSMigrations(t *testing.T) {
	db, err := dbx.Open(
		dbx.WithDSN(":memory:"),
		dbx.WithDriver("sqlite"),
		dbx.WithPragmas([]string{
			"PRAGMA foreign_keys = ON",    // turn on foreign keys
			"PRAGMA synchronous = NORMAL", // this is the appropriate setting for WAL
			"PRAGMA secure_delete = OFF",  // we do not need to overwrite deleted data with zeroes
			"PRAGMA synchronous = NORMAL", // this is the appropriate setting for WAL
			"PRAGMA temp_store = MEMORY",  // store any temporary tables and indices in memory
		}),
		dbx.WithMigrations(migrationsFS, "testmigrations"),
		dbx.WithMigrationDriver("sqlite", func(db *sql.DB) (database.Driver, string, error) {
			d, err := sqlite3.WithInstance(db, &sqlite3.Config{})
			return d, "sqlite3", err
		}),
	)
	require.NoError(t, err)
	require.NotNil(t, db)
}

func TestOpenFilesystemMigrations(t *testing.T) {
	db, err := dbx.Open(
		dbx.WithDSN(":memory:"),
		dbx.WithDriver("sqlite"),
		dbx.WithMigrations(os.DirFS("testmigrations"), "."),
		dbx.WithMigrationDriver("sqlite", func(db *sql.DB) (database.Driver, string, error) {
			d, err := sqlite3.WithInstance(db, &sqlite3.Config{})
			return d, "sqlite3", err
		}),
	)
	require.NoError(t, err)
	require.NotNil(t, db)
}

func TestNoMigrationDrivers(t *testing.T) {
	db, err := dbx.Open(
		dbx.WithDSN(":memory:"),
		dbx.WithDriver("sqlite"),
		dbx.WithMigrations(migrationsFS, "testmigrations"),
	)
	require.ErrorIs(t, err, dbx.ErrNoMigrationDrivers)
	require.Nil(t, db)
}
