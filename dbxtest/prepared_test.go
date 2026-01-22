package dbxtest

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/borud/dbx"
	"github.com/golang-migrate/migrate/v4/database"
	"github.com/golang-migrate/migrate/v4/database/sqlite3"
	"github.com/stretchr/testify/require"
)

func TestPrepared(t *testing.T) {
	db, err := dbx.OpenSQLX(
		dbx.WithDSN(":memory:"),
		dbx.WithDriver("sqlite"),
		dbx.WithMigrations(migrationsFS, "testmigrations"),
		dbx.WithMigrationDriver("sqlite", "sqlite3",
			func(db *sql.DB) (database.Driver, error) {
				return sqlite3.WithInstance(db, &sqlite3.Config{})
			}),
	)
	require.NoError(t, err)
	require.NotNil(t, db)

	insert := dbx.NewNamedExecFunc[record](db, "INSERT INTO foo (name,ts) VALUES (:name,:ts)")
	require.NoError(t, err)

	res, err := insert(context.Background(), record{
		Name: "foo",
		TS:   time.Now().UnixMilli(),
	})
	require.NoError(t, err)
	lastID, ok := res.LastInsertID()
	require.True(t, ok)
	require.Equal(t, int64(1), lastID)

	rows, ok := res.RowsAffected()
	require.True(t, ok)
	require.Equal(t, int64(1), rows)

	rec1 := record{
		Name: "foo",
		TS:   time.Now().UnixMilli(),
	}

	// returning
	insertReturning := dbx.NewQueryRowxFunc[record](db, "INSERT INTO foo (name,ts) VALUES(:name,:ts) RETURNING *")
	rec2, err := insertReturning(context.Background(), rec1)
	require.NoError(t, err)
	require.Equal(t, rec1, rec2)

	// exec
	del := dbx.NewExecFunc(db, "DELETE FROM foo WHERE name = ?")
	require.NoError(t, del(context.Background(), "foo"))
}
