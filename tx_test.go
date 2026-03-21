package dbx_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/borud/dbx"
	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/require"
)

func openTestDB(t *testing.T) *sqlx.DB {
	t.Helper()
	db, err := dbx.OpenSQLX(
		dbx.WithDSN(":memory:"),
		dbx.WithDriver("sqlite"),
		dbx.WithMigrations(migrationsFS, "testmigrations"),
	)
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	return db
}

func TestRunTx_Commit(t *testing.T) {
	db := openTestDB(t)

	addTx := dbx.NewTxNamedExecFunc[record]("INSERT INTO foo (name,ts) VALUES(:name,:ts)")
	get := dbx.NewQueryRowxFunc[record](db, "SELECT * FROM foo WHERE name = ?")

	err := dbx.RunTx(context.Background(), db, func(tx *sqlx.Tx) error {
		_, err := addTx(context.Background(), tx, record{Name: "tx-test", TS: 42})
		return err
	})
	require.NoError(t, err)

	rec, err := get(context.Background(), "tx-test")
	require.NoError(t, err)
	require.Equal(t, "tx-test", rec.Name)
	require.Equal(t, int64(42), rec.TS)
}

func TestRunTx_Rollback(t *testing.T) {
	db := openTestDB(t)

	addTx := dbx.NewTxNamedExecFunc[record]("INSERT INTO foo (name,ts) VALUES(:name,:ts)")
	get := dbx.NewQueryRowxFunc[record](db, "SELECT * FROM foo WHERE name = ?")

	err := dbx.RunTx(context.Background(), db, func(tx *sqlx.Tx) error {
		if _, err := addTx(context.Background(), tx, record{Name: "rollback-test", TS: 1}); err != nil {
			return err
		}
		return sql.ErrNoRows // simulate failure
	})
	require.Error(t, err)

	// Row should not exist because tx was rolled back.
	_, err = get(context.Background(), "rollback-test")
	require.Error(t, err)
}

func TestTxExecFunc(t *testing.T) {
	db := openTestDB(t)

	add := dbx.NewNamedExecFunc[record](db, "INSERT INTO foo (name,ts) VALUES(:name,:ts)")
	delTx := dbx.NewTxExecFunc("DELETE FROM foo WHERE name = ?")
	get := dbx.NewQueryRowxFunc[record](db, "SELECT * FROM foo WHERE name = ?")

	_, err := add(context.Background(), record{Name: "to-delete", TS: 1})
	require.NoError(t, err)

	err = dbx.RunTx(context.Background(), db, func(tx *sqlx.Tx) error {
		return delTx(context.Background(), tx, "to-delete")
	})
	require.NoError(t, err)

	_, err = get(context.Background(), "to-delete")
	require.Error(t, err)
}

func TestTxQueryRowxFunc(t *testing.T) {
	db := openTestDB(t)

	add := dbx.NewNamedExecFunc[record](db, "INSERT INTO foo (name,ts) VALUES(:name,:ts)")
	getTx := dbx.NewTxQueryRowxFunc[record]("SELECT * FROM foo WHERE name = ?")

	_, err := add(context.Background(), record{Name: "query-test", TS: 99})
	require.NoError(t, err)

	var got record
	err = dbx.RunTx(context.Background(), db, func(tx *sqlx.Tx) error {
		var err error
		got, err = getTx(context.Background(), tx, "query-test")
		return err
	})
	require.NoError(t, err)
	require.Equal(t, "query-test", got.Name)
	require.Equal(t, int64(99), got.TS)
}

func TestTxMultipleOps(t *testing.T) {
	db := openTestDB(t)

	addTx := dbx.NewTxNamedExecFunc[record]("INSERT INTO foo (name,ts) VALUES(:name,:ts)")
	getTx := dbx.NewTxQueryRowxFunc[record]("SELECT * FROM foo WHERE name = ?")
	list := dbx.NewSelectFunc[record](db, "SELECT * FROM foo ORDER BY ts ASC")

	now := time.Now().UnixNano()

	// Insert two records atomically.
	err := dbx.RunTx(context.Background(), db, func(tx *sqlx.Tx) error {
		ctx := context.Background()
		if _, err := addTx(ctx, tx, record{Name: "multi-1", TS: now}); err != nil {
			return err
		}
		if _, err := addTx(ctx, tx, record{Name: "multi-2", TS: now + 1}); err != nil {
			return err
		}
		// Read back inside same tx.
		r, err := getTx(ctx, tx, "multi-1")
		if err != nil {
			return err
		}
		require.Equal(t, "multi-1", r.Name)
		return nil
	})
	require.NoError(t, err)

	recs, err := list(context.Background())
	require.NoError(t, err)
	require.Len(t, recs, 2)
}

func TestTxEntityQueryRowxFunc(t *testing.T) {
	db := openTestDB(t)

	addRetTx := dbx.NewTxEntityQueryRowxFunc[record]("INSERT INTO foo (name,ts) VALUES(:name,:ts) RETURNING *")

	var got record
	err := dbx.RunTx(context.Background(), db, func(tx *sqlx.Tx) error {
		var err error
		got, err = addRetTx(context.Background(), tx, record{Name: "entity-test", TS: 77})
		return err
	})
	require.NoError(t, err)
	require.Equal(t, "entity-test", got.Name)
	require.Equal(t, int64(77), got.TS)
}
