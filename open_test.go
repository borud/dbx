package dbx_test

import (
	"bytes"
	"context"
	"database/sql"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	mysql "github.com/golang-migrate/migrate/v4/database/mysql"
	pg "github.com/golang-migrate/migrate/v4/database/postgres"
	sqlserver "github.com/golang-migrate/migrate/v4/database/sqlserver"

	"github.com/borud/dbx/v2"
	"github.com/golang-migrate/migrate/v4/database"
	"github.com/golang-migrate/migrate/v4/database/sqlite"
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
				return sqlite.WithInstance(db, &sqlite.Config{})
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
				return sqlite.WithInstance(db, &sqlite.Config{})
			}),
		dbx.WithMigrationDriver("sqlite3", "sqlite3",
			func(db *sql.DB) (database.Driver, error) {
				return sqlite.WithInstance(db, &sqlite.Config{})
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

func TestOpenSQLiteMemorySingleConnection(t *testing.T) {
	for _, dsn := range []string{":memory:", "file::memory:", "file:x?mode=memory"} {
		db, err := dbx.Open(dbx.WithDSN(dsn), dbx.WithDriver("sqlite"))
		require.NoError(t, err)
		require.Equal(t, 1, db.Stats().MaxOpenConnections, dsn)
		require.NoError(t, db.Close())
	}

	db, err := dbx.Open(dbx.WithDSN("file:"+filepath.Join(t.TempDir(), "x.db")), dbx.WithDriver("sqlite"))
	require.NoError(t, err)
	require.Equal(t, 0, db.Stats().MaxOpenConnections)
	require.NoError(t, db.Close())
}

func TestOpenPragmasEveryConnection(t *testing.T) {
	db, err := dbx.Open(
		dbx.WithDSN("file:"+filepath.Join(t.TempDir(), "x.db")),
		dbx.WithDriver("sqlite"),
		dbx.WithPragmas("PRAGMA foreign_keys = ON", "PRAGMA busy_timeout = 1234"),
	)
	require.NoError(t, err)
	defer db.Close()

	conns := make([]*sql.Conn, 0, 3)
	for range 3 {
		c, err := db.Conn(context.Background())
		require.NoError(t, err)
		defer c.Close()
		conns = append(conns, c)
	}
	require.Equal(t, 3, db.Stats().OpenConnections)

	for _, c := range conns {
		var fk, timeout int
		require.NoError(t, c.QueryRowContext(context.Background(), "PRAGMA foreign_keys").Scan(&fk))
		require.NoError(t, c.QueryRowContext(context.Background(), "PRAGMA busy_timeout").Scan(&timeout))
		require.Equal(t, 1, fk)
		require.Equal(t, 1234, timeout)
	}
}

func TestOpenBadPragma(t *testing.T) {
	_, err := dbx.Open(dbx.WithDSN(":memory:"), dbx.WithDriver("sqlite"), dbx.WithPragmas("PRAGMA nonsense ="))
	require.ErrorContains(t, err, `pragma "PRAGMA nonsense ="`)
}

func migrationFS(files map[string]string) fstest.MapFS {
	m := fstest.MapFS{"m": &fstest.MapFile{Mode: fs.ModeDir}}
	for name, sql := range files {
		m["m/"+name] = &fstest.MapFile{Data: []byte(sql)}
	}
	return m
}

func openMigrated(dsn string, files map[string]string) (*sql.DB, error) {
	return dbx.Open(dbx.WithDSN(dsn), dbx.WithDriver("sqlite"), dbx.WithMigrations(migrationFS(files), "m"))
}

func TestMigrationsMissingDirectory(t *testing.T) {
	_, err := dbx.Open(dbx.WithDSN(":memory:"), dbx.WithDriver("sqlite"), dbx.WithMigrations(migrationsFS, "nope"))
	require.Error(t, err)
	require.NotErrorIs(t, err, dbx.ErrDatabaseTooNew)
	require.ErrorContains(t, err, `migrations source "nope"`)
}

func TestMigrationsEmptyDirectory(t *testing.T) {
	db, err := openMigrated(":memory:", nil)
	require.NoError(t, err)
	require.NoError(t, db.Close())
}

func TestMigrationsDatabaseTooNew(t *testing.T) {
	dsn := "file:" + filepath.Join(t.TempDir(), "x.db")
	v1 := map[string]string{"1_a.up.sql": "CREATE TABLE a (x INTEGER);"}
	v2 := map[string]string{"1_a.up.sql": v1["1_a.up.sql"], "2_b.up.sql": "CREATE TABLE b (x INTEGER);"}

	db, err := openMigrated(dsn, v2)
	require.NoError(t, err)
	require.NoError(t, db.Close())

	_, err = openMigrated(dsn, v1)
	require.ErrorIs(t, err, dbx.ErrDatabaseTooNew)
	require.ErrorContains(t, err, "version 2, newest migration is 1")

	_, err = openMigrated(dsn, nil)
	require.ErrorIs(t, err, dbx.ErrDatabaseTooNew)
}

func TestMigrationsDirty(t *testing.T) {
	dsn := "file:" + filepath.Join(t.TempDir(), "x.db")
	files := map[string]string{
		"1_a.up.sql": "CREATE TABLE a (x INTEGER);",
		"2_b.up.sql": "CREATE TABLE b (x INTEGER); GARBAGE;",
	}

	_, err := openMigrated(dsn, files)
	require.ErrorIs(t, err, dbx.ErrDirtyDatabase)
	require.ErrorContains(t, err, "version 2")

	// Reopening reports the dirty state rather than retrying.
	files["2_b.up.sql"] = "CREATE TABLE b (x INTEGER);"
	_, err = openMigrated(dsn, files)
	require.ErrorIs(t, err, dbx.ErrDirtyDatabase)
}

func TestWithLogger(t *testing.T) {
	var buf bytes.Buffer
	db, err := dbx.Open(
		dbx.WithDSN(":memory:"),
		dbx.WithDriver("sqlite"),
		dbx.WithMigrations(migrationsFS, "testmigrations"),
		dbx.WithLogger(slog.New(slog.NewTextHandler(&buf, nil))),
	)
	require.NoError(t, err)
	require.NoError(t, db.Close())
	require.Contains(t, buf.String(), "database migration")
}
