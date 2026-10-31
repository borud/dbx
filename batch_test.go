package dbx_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/borud/dbx/v2"
	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/require"
)

func TestTxBatchExecFunc(t *testing.T) {
	db := openTestDB(t)
	// One connection, as for SQLite in-memory databases: statements must be
	// prepared before the transaction holds it.
	db.SetMaxOpenConns(1)

	insert := dbx.NewTxBatchExecFunc[record](db, "INSERT INTO foo (name, ts) VALUES ", "(:name, :ts * 10)", 8)
	list := dbx.NewSelectFunc[record](db, "SELECT * FROM foo ORDER BY rowid")

	recs := make([]record, 0, 23)
	for i := range 23 {
		recs = append(recs, record{Name: fmt.Sprintf("r%02d", i), TS: int64(i)})
	}

	var results []dbx.Result
	err := dbx.RunTx(context.Background(), db, func(tx *sqlx.Tx) error {
		var err error
		results, err = insert(context.Background(), tx, recs)
		return err
	})
	require.NoError(t, err)
	require.Len(t, results, 3)

	rows, ok := results[2].RowsAffected()
	require.True(t, ok)
	require.Equal(t, int64(7), rows)
	last, ok := results[2].LastInsertID()
	require.True(t, ok)
	require.Equal(t, int64(23), last)

	got, err := list(context.Background())
	require.NoError(t, err)
	require.Len(t, got, 23)
	require.Equal(t, "r00", got[0].Name)
	require.Equal(t, "r22", got[22].Name)
	require.Equal(t, int64(220), got[22].TS)
}

func TestTxBatchExecFuncEmpty(t *testing.T) {
	db := openTestDB(t)
	insert := dbx.NewTxBatchExecFunc[record](db, "INSERT INTO foo (name, ts) VALUES ", "(:name, :ts)", 4)

	err := dbx.RunTx(context.Background(), db, func(tx *sqlx.Tx) error {
		results, err := insert(context.Background(), tx, nil)
		require.Empty(t, results)
		return err
	})
	require.NoError(t, err)
}

func TestTxBatchExecFuncLiteralColon(t *testing.T) {
	db := openTestDB(t)
	db.SetMaxOpenConns(1)
	insert := dbx.NewTxBatchExecFunc[record](db, "INSERT INTO foo (name, ts) VALUES ", "(:name || '::x', :ts)", 2)
	get := dbx.NewQueryRowxFunc[record](db, "SELECT * FROM foo")

	err := dbx.RunTx(context.Background(), db, func(tx *sqlx.Tx) error {
		_, err := insert(context.Background(), tx, []record{{Name: "a", TS: 1}})
		return err
	})
	require.NoError(t, err)

	r, err := get(context.Background())
	require.NoError(t, err)
	require.Equal(t, "a:x", r.Name)
}

func TestTxBatchExecFuncUnknownField(t *testing.T) {
	db := openTestDB(t)
	require.Panics(t, func() {
		dbx.NewTxBatchExecFunc[record](db, "INSERT INTO foo (name, ts) VALUES ", "(:name, :nope)", 2)
	})
}
