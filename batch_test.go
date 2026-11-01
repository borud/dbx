package dbx_test

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"testing"

	"github.com/borud/dbx/v2"
	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/require"
)

func TestTxBatchExecFunc(t *testing.T) {
	db := openTestDB(t)

	insert := dbx.NewTxBatchExecFunc[record](db, "INSERT INTO foo (name, ts) VALUES ", "(:name, :ts * 10)", "", 8)
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
	insert := dbx.NewTxBatchExecFunc[record](db, "INSERT INTO foo (name, ts) VALUES ", "(:name, :ts)", "", 4)

	err := dbx.RunTx(context.Background(), db, func(tx *sqlx.Tx) error {
		results, err := insert(context.Background(), tx, nil)
		require.Empty(t, results)
		return err
	})
	require.NoError(t, err)
}

func TestTxBatchExecFuncLiteralColon(t *testing.T) {
	db := openTestDB(t)
	insert := dbx.NewTxBatchExecFunc[record](db, "INSERT INTO foo (name, ts) VALUES ", "(:name || '::x', :ts)", "", 2)
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
		dbx.NewTxBatchExecFunc[record](db, "INSERT INTO foo (name, ts) VALUES ", "(:name, :nope)", "", 2)
	})
}

func TestTxBatchExecFuncTail(t *testing.T) {
	db := openTestDB(t)
	_, err := db.Exec("CREATE TABLE uniq (name TEXT PRIMARY KEY, ts INTEGER NOT NULL)")
	require.NoError(t, err)

	upsert := dbx.NewTxBatchExecFunc[record](db, "INSERT INTO uniq (name, ts) VALUES ", "(:name, :ts)",
		" ON CONFLICT (name) DO UPDATE SET ts = excluded.ts", 4)
	get := dbx.NewQueryRowxFunc[record](db, "SELECT * FROM uniq WHERE name = ?")

	for _, ts := range []int64{1, 2} {
		err := dbx.RunTx(context.Background(), db, func(tx *sqlx.Tx) error {
			_, err := upsert(context.Background(), tx, []record{{Name: "a", TS: ts}, {Name: "b", TS: ts}})
			return err
		})
		require.NoError(t, err)
	}

	r, err := get(context.Background(), "b")
	require.NoError(t, err)
	require.Equal(t, int64(2), r.TS)
}

func TestTxBatchExecFuncLiteralQuestionMark(t *testing.T) {
	db := openTestDB(t)
	insert := dbx.NewTxBatchExecFunc[record](db, "INSERT INTO foo (name, ts) VALUES ", "(:name || '?', :ts)", "", 2)
	get := dbx.NewQueryRowxFunc[record](db, "SELECT * FROM foo")

	err := dbx.RunTx(context.Background(), db, func(tx *sqlx.Tx) error {
		_, err := insert(context.Background(), tx, []record{{Name: "a", TS: 1}})
		return err
	})
	require.NoError(t, err)

	r, err := get(context.Background())
	require.NoError(t, err)
	require.Equal(t, "a?", r.Name)
}

func TestTxBatchExecFuncNilPointer(t *testing.T) {
	db := openTestDB(t)
	insert := dbx.NewTxBatchExecFunc[*record](db, "INSERT INTO foo (name, ts) VALUES ", "(:name, :ts)", "", 4)

	err := dbx.RunTx(context.Background(), db, func(tx *sqlx.Tx) error {
		_, err := insert(context.Background(), tx, []*record{{Name: "a", TS: 1}, nil})
		return err
	})
	require.ErrorContains(t, err, "entity 1: nil pointer")
}

func TestTxBatchExecFuncNilEmbedded(t *testing.T) {
	type wrapped struct {
		*record
	}

	db := openTestDB(t)
	insert := dbx.NewTxBatchExecFunc[wrapped](db, "INSERT INTO foo (name, ts) VALUES ", "(:name, :ts)", "", 4)

	err := dbx.RunTx(context.Background(), db, func(tx *sqlx.Tx) error {
		_, err := insert(context.Background(), tx, []wrapped{{}})
		return err
	})
	require.ErrorContains(t, err, "nil pointer on the path to :name")
}

func TestTxBatchExecFuncFailsMidBatch(t *testing.T) {
	db := openTestDB(t)
	// NULLIF turns ts 5 into NULL, which violates NOT NULL in the second chunk.
	insert := dbx.NewTxBatchExecFunc[record](db, "INSERT INTO foo (name, ts) VALUES ", "(:name, NULLIF(:ts, 5))", "", 4)
	list := dbx.NewSelectFunc[record](db, "SELECT * FROM foo")

	recs := make([]record, 0, 10)
	for i := range 10 {
		recs = append(recs, record{Name: fmt.Sprintf("r%d", i), TS: int64(i)})
	}

	var results []dbx.Result
	err := dbx.RunTx(context.Background(), db, func(tx *sqlx.Tx) error {
		var err error
		results, err = insert(context.Background(), tx, recs)
		return err
	})
	require.ErrorContains(t, err, "x4")
	require.Len(t, results, 1)

	got, err := list(context.Background())
	require.NoError(t, err)
	require.Empty(t, got)
}

func TestTxBatchExecFuncManyConnections(t *testing.T) {
	db, err := dbx.OpenSQLX(
		dbx.WithDSN("file:"+filepath.Join(t.TempDir(), "test.db")+"?_pragma=busy_timeout(10000)&_txlock=immediate"),
		dbx.WithDriver("sqlite"),
		dbx.WithMigrations(migrationsFS, "testmigrations"),
	)
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	db.SetMaxOpenConns(4)

	insert := dbx.NewTxBatchExecFunc[record](db, "INSERT INTO foo (name, ts) VALUES ", "(:name, :ts)", "", 4)
	count := dbx.NewQueryRowxFunc[struct {
		N int `db:"n"`
	}](db, "SELECT count(*) AS n FROM foo")

	// Pin a connection, normally the idle one the statements were prepared
	// on, so the transactions below prepare them on other connections.
	pinned, err := db.Conn(context.Background())
	require.NoError(t, err)
	defer pinned.Close()

	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for g := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- dbx.RunTx(context.Background(), db, func(tx *sqlx.Tx) error {
				_, err := insert(context.Background(), tx, []record{
					{Name: fmt.Sprintf("g%d-a", g), TS: 1},
					{Name: fmt.Sprintf("g%d-b", g), TS: 2},
					{Name: fmt.Sprintf("g%d-c", g), TS: 3},
				})
				return err
			})
		}()
	}
	wg.Wait()
	require.Greater(t, db.Stats().OpenConnections, 1)
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}

	n, err := count(context.Background())
	require.NoError(t, err)
	require.Equal(t, 24, n.N)
}
