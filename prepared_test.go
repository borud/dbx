package dbx_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"testing"
	"time"

	"github.com/borud/dbx/v2"
	"github.com/golang-migrate/migrate/v4/database"
	"github.com/golang-migrate/migrate/v4/database/sqlite"
	"github.com/stretchr/testify/require"
)

type operations struct {
	add       dbx.NamedExecFunc[record]
	addRet    dbx.EntityQueryRowxFunc[record]
	get       dbx.QueryRowxFunc[record]
	update    dbx.NamedExecFunc[record]
	updateRet dbx.EntityQueryRowxFunc[record]
	delete    dbx.ExecFunc
	list      dbx.SelectFunc[record]
	listIter  dbx.QueryxIteratorFunc[record]
}

func TestPrepared(t *testing.T) {
	db, err := dbx.OpenSQLX(
		dbx.WithDSN(":memory:"),
		dbx.WithDriver("sqlite"),
		dbx.WithMigrations(migrationsFS, "testmigrations"),
	)
	require.NoError(t, err)
	require.NotNil(t, db)

	ops := operations{
		add:       dbx.NewNamedExecFunc[record](db, "INSERT INTO foo (name,ts) VALUES (:name,:ts)"),
		addRet:    dbx.NewEntityQueryRowxFunc[record](db, "INSERT INTO foo (name,ts) VALUES (:name,:ts) RETURNING *"),
		get:       dbx.NewQueryRowxFunc[record](db, "SELECT * FROM foo WHERE name = ?"),
		update:    dbx.NewNamedExecFunc[record](db, "UPDATE foo SET ts = :ts WHERE name = :name"),
		updateRet: dbx.NewEntityQueryRowxFunc[record](db, "UPDATE foo SET ts = :ts WHERE name = :name RETURNING *"),
		delete:    dbx.NewExecFunc(db, "DELETE FROM foo WHERE name = ?"),
		list:      dbx.NewSelectFunc[record](db, "SELECT * FROM foo ORDER BY ts ASC"),
		listIter:  dbx.NewQueryxIteratorFunc[record](db, "SELECT * FROM foo ORDER BY ts ASC"),
	}

	rec := record{Name: "the name", TS: time.Now().UnixNano()}

	// add
	res, err := ops.add(context.Background(), rec)
	require.NoError(t, err)
	lastID, _ := res.LastInsertID()
	rowsAffected, _ := res.RowsAffected()

	// addRet
	r2, err := ops.addRet(context.Background(), record{
		Name: "another name",
		TS:   1234,
	})
	require.NoError(t, err)
	require.Equal(t, int64(1234), r2.TS)

	require.Equal(t, int64(1), lastID)
	require.Equal(t, int64(1), rowsAffected)

	// get
	recGet, err := ops.get(context.Background(), "the name")
	require.NoError(t, err)
	require.Equal(t, rec, recGet)

	// update
	updateRec := record{Name: rec.Name, TS: time.Now().UnixNano()}
	res, err = ops.update(context.Background(), updateRec)
	require.NoError(t, err)
	rowsAffected, _ = res.RowsAffected()
	require.Equal(t, int64(1), rowsAffected)

	// update with returning
	updateRec = record{Name: rec.Name, TS: time.Now().UnixNano()}
	recUpdate, err := ops.updateRet(context.Background(), updateRec)
	require.NoError(t, err)
	require.Equal(t, updateRec, recUpdate)

	// delete
	err = ops.delete(context.Background(), "the name")
	require.NoError(t, err)

	_, err = ops.get(context.Background(), "the name")
	require.Error(t, err)

	// add some rows
	for i := range 20 {
		res, err := ops.add(context.Background(), record{
			Name: fmt.Sprintf("name-%d", i),
			TS:   int64(i),
		})
		require.NoError(t, err)
		rowsAffected, ok := res.RowsAffected()
		require.True(t, ok)
		require.Equal(t, int64(1), rowsAffected)
	}

	// simple read-all list operation
	recs, err := ops.list(context.Background())
	require.NoError(t, err)
	require.Equal(t, 21, len(recs))

	// iterator
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	count := -1
	for re, err := range ops.listIter(ctx) {
		count++
		if count == 5 {
			slog.Info("cancel context")
			cancel()
			// give context cancellation a chance to propagate.  This is silly
			// and dirty, but since we leave context cancellation to the lowest
			// layer in the DB stack rather than duplicate the effort higher
			// up, it is worth the price of some ugliness.
			time.Sleep(time.Nanosecond)
		}

		if count > 5 && err != nil {
			require.ErrorIs(t, err, context.Canceled)
			break
		}

		require.NoError(t, err)
		require.NotEmpty(t, re.Name)
	}

	for re, err := range ops.listIter(ctx) {
		if err != nil {
			break
		}
		fmt.Println(re)
	}
}

func TestQueryXRowsIterator(t *testing.T) {
	db, err := dbx.OpenSQLX(
		dbx.WithDSN(":memory:"),
		dbx.WithDriver("sqlite"),
		dbx.WithMigrations(migrationsFS, "testmigrations"),
		dbx.WithMigrationDriver("sqlite", "sqlite3",
			func(db *sql.DB) (database.Driver, error) {
				return sqlite.WithInstance(db, &sqlite.Config{})
			}),
	)
	require.NoError(t, err)
	require.NotNil(t, db)

	ops := struct {
		add  dbx.NamedExecFunc[record]
		list dbx.QueryxIteratorFunc[record]
	}{
		add:  dbx.NewNamedExecFunc[record](db, "INSERT INTO foo (name,ts) VALUES(:name,:ts)"),
		list: dbx.NewQueryxIteratorFunc[record](db, "SELECT * FROM foo"),
	}

	// add some rows
	for i := range 30 {
		_, err := ops.add(context.Background(), record{
			Name: fmt.Sprintf("name-%d", i),
			TS:   time.Now().UnixMicro(),
		})
		require.NoError(t, err)
	}

	for r, err := range ops.list(context.Background()) {
		require.NoError(t, err)
		require.NotZero(t, r)
	}

	// Timeout
	{
		// set timeout to 100 milliseconds which should allow enough time to run
		// the QueryxContext call.
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
		defer cancel()

		count := 0

		for _, err := range ops.list(ctx) {
			count++
			time.Sleep(20 * time.Millisecond)
			if count == 2 {
				require.ErrorIs(t, err, context.DeadlineExceeded)
			}
		}
	}

	// cancel
	{
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		seenContextCanceled := false
		for _, err := range ops.list(ctx) {
			// cancel after first row.  Depending on how much data is cached, this should
			// terminate iteration after a few rows.  Usually 1-2 rows.
			cancel()
			time.Sleep(1 * time.Millisecond)

			if errors.Is(err, context.Canceled) {
				seenContextCanceled = true
			}
		}

		require.True(t, seenContextCanceled)
	}
	require.NoError(t, db.Close())
}

// An iterator that is never ranged must not hold a connection, and each
// range must run the query afresh.
func TestQueryxIteratorLazy(t *testing.T) {
	db := openTestDB(t)
	add := dbx.NewNamedExecFunc[record](db, "INSERT INTO foo (name,ts) VALUES (:name,:ts)")
	iter := dbx.NewQueryxIteratorFunc[record](db, "SELECT * FROM foo ORDER BY ts")

	_, err := add(context.Background(), record{Name: "a", TS: 1})
	require.NoError(t, err)

	seq := iter(context.Background())
	require.Equal(t, 0, db.Stats().InUse)

	// The in-memory database has one connection; this would block if the
	// unranged iterator held it.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err = add(ctx, record{Name: "b", TS: 2})
	require.NoError(t, err)

	for range 2 {
		names := []string{}
		for r, err := range seq {
			require.NoError(t, err)
			names = append(names, r.Name)
		}
		require.Equal(t, []string{"a", "b"}, names)
	}
	require.Equal(t, 0, db.Stats().InUse)
}

func TestQueryxIteratorErrorNamesStatement(t *testing.T) {
	db := openTestDB(t)
	add := dbx.NewNamedExecFunc[record](db, "INSERT INTO foo (name,ts) VALUES (:name,:ts)")
	iter := dbx.NewQueryxIteratorFunc[record](db, "SELECT * FROM foo")

	_, err := add(context.Background(), record{Name: "a", TS: 1})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, err := range iter(ctx) {
		require.ErrorIs(t, err, context.Canceled)
		require.ErrorContains(t, err, "statement [SELECT * FROM foo]")
	}
}

func TestExecFuncNoRowsAffected(t *testing.T) {
	db := openTestDB(t)
	del := dbx.NewExecFunc(db, "DELETE FROM foo WHERE name = ?")

	err := del(context.Background(), "missing")
	require.ErrorIs(t, err, sql.ErrNoRows)
	require.ErrorContains(t, err, "statement [DELETE FROM foo WHERE name = ?]: no rows affected")
}

func TestQueryRowxFuncNoRows(t *testing.T) {
	db := openTestDB(t)
	get := dbx.NewQueryRowxFunc[record](db, "SELECT * FROM foo WHERE name = ?")

	_, err := get(context.Background(), "missing")
	require.ErrorIs(t, err, sql.ErrNoRows)
	require.ErrorContains(t, err, "statement [SELECT * FROM foo WHERE name = ?]")
}

// Constructors prepare at init, so invalid SQL panics there.
func TestConstructorsPanicOnInvalidSQL(t *testing.T) {
	db := openTestDB(t)
	const bad = "SELECT nope FROM foo"

	tests := map[string]func(){
		"ExecFunc":              func() { dbx.NewExecFunc(db, bad) },
		"NamedExecFunc":         func() { dbx.NewNamedExecFunc[record](db, bad) },
		"QueryRowxFunc":         func() { dbx.NewQueryRowxFunc[record](db, bad) },
		"EntityQueryRowxFunc":   func() { dbx.NewEntityQueryRowxFunc[record](db, bad) },
		"SelectFunc":            func() { dbx.NewSelectFunc[record](db, bad) },
		"QueryxIteratorFunc":    func() { dbx.NewQueryxIteratorFunc[record](db, bad) },
		"TxExecFunc":            func() { dbx.NewTxExecFunc(db, bad) },
		"TxNamedExecFunc":       func() { dbx.NewTxNamedExecFunc[record](db, bad) },
		"TxQueryRowxFunc":       func() { dbx.NewTxQueryRowxFunc[record](db, bad) },
		"TxEntityQueryRowxFunc": func() { dbx.NewTxEntityQueryRowxFunc[record](db, bad) },
		"TxBatchExecFunc": func() {
			dbx.NewTxBatchExecFunc[record](db, "INSERT INTO nope (name, ts) VALUES ", "(:name, :ts)", "", 2)
		},
	}
	for name, fn := range tests {
		t.Run(name, func(t *testing.T) { require.Panics(t, fn) })
	}
}
