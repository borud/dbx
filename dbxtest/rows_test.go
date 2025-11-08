package dbxtest

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/borud/dbx"
	"github.com/golang-migrate/migrate/v4/database"
	"github.com/golang-migrate/migrate/v4/database/sqlite3"
	"github.com/stretchr/testify/require"
)

type record struct {
	Name string `db:"name"`
	TS   int64  `db:"ts"`
}

func TestRowsIter(t *testing.T) {
	db, err := dbx.Open(
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

	// add some rows
	for i := range 3 {
		res, err := db.NamedExec("INSERT INTO foo (name,ts) VALUES(:name,:ts)", record{
			Name: fmt.Sprintf("name_%d", i),
			TS:   time.Now().UnixNano(),
		})
		require.NoError(t, err)
		require.NotNil(t, res)
	}

	// iterate over them
	fmt.Println("NORMAL")
	rows, err := db.Queryx("SELECT * FROM foo")
	require.NoError(t, err)

	for rec, err := range dbx.RowsIter[record](context.Background(), rows) {
		require.NoError(t, err)
		fmt.Println(rec)
	}

	// timeout
	{
		fmt.Println("TIMEOUT")

		// set timeout to 100 milliseconds which should allow enough time to run
		// the QueryxContext call.
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()

		rows, err = db.QueryxContext(ctx, "SELECT * FROM foo")
		require.NoError(t, err)

		// Now sleep for at least 101 milliseconds to time out.
		time.Sleep(200 * time.Millisecond)

		for _, err := range dbx.RowsIter[record](context.Background(), rows) {
			require.ErrorIs(t, err, context.DeadlineExceeded)
		}

	}
	// cancel
	{
		fmt.Println("CANCEL")

		rows, err = db.Queryx("SELECT * FROM foo")
		require.NoError(t, err)

		ctx2, cancel2 := context.WithCancel(context.Background())
		defer cancel2()

		count := 0
		for rec, err := range dbx.RowsIter[record](ctx2, rows) {
			count++
			if count == 2 {
				require.ErrorIs(t, err, context.Canceled)
				break
			}
			fmt.Println(rec)
			cancel2() // cancel after one row
		}
	}
	require.NoError(t, db.Close())
}

func BenchmarkRowIter(b *testing.B) {
	db, err := dbx.Open(
		dbx.WithDSN(":memory:"),
		dbx.WithDriver("sqlite"),
		dbx.WithPragmas([]string{
			"PRAGMA synchronous = NORMAL",
			"PRAGMA secure_delete = OFF",
			"PRAGMA synchronous = NORMAL",
			"PRAGMA temp_store = MEMORY",
			"PRAGMA cache_size = -800000",
		}),
		dbx.WithMigrations(migrationsFS, "testmigrations"),
		dbx.WithMigrationDriver("sqlite", "sqlite3",
			func(db *sql.DB) (database.Driver, error) {
				return sqlite3.WithInstance(db, &sqlite3.Config{})
			}),
	)
	require.NoError(b, err)
	require.NotNil(b, db)

	// add some rows
	for i := range b.N {
		res, err := db.NamedExec("INSERT INTO foo (name,ts) VALUES(:name,:ts)", record{
			Name: fmt.Sprintf("name_%d", i),
			TS:   time.Now().UnixNano(),
		})
		require.NoError(b, err)
		require.NotNil(b, res)
	}

	rows, err := db.Queryx("SELECT * FROM foo")
	require.NoError(b, err)

	b.ResetTimer()
	//revive:disable-next-line
	for range dbx.RowsIter[record](context.Background(), rows) {
	}
}
