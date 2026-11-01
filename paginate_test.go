package dbx_test

import (
	"context"
	"fmt"
	"strconv"
	"testing"

	"github.com/borud/dbx/v2"
	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/require"
)

func openPaginateDB(t *testing.T) *sqlx.DB {
	t.Helper()

	db, err := dbx.OpenSQLX(
		dbx.WithDSN(":memory:"),
		dbx.WithDriver("sqlite"),
		dbx.WithMigrations(migrationsFS, "testmigrations"),
	)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	return db
}

func addRecords(t *testing.T, db *sqlx.DB, recs ...record) {
	t.Helper()

	add := dbx.NewNamedExecFunc[record](db, "INSERT INTO foo (name,ts) VALUES (:name,:ts)")
	for _, r := range recs {
		_, err := add(context.Background(), r)
		require.NoError(t, err)
	}
}

func recordCursor(r record) (string, string) {
	return strconv.FormatInt(r.TS, 10), r.Name
}

func recordNames(rows []record) []string {
	names := make([]string, 0, len(rows))
	for _, r := range rows {
		names = append(names, r.Name)
	}
	return names
}

// setupPaginateDB returns a list over 20 rows item-00..item-19 with ts 0..19.
func setupPaginateDB(t *testing.T) dbx.PaginatedSelectFunc[record] {
	t.Helper()

	db := openPaginateDB(t)
	for i := range 20 {
		addRecords(t, db, record{Name: fmt.Sprintf("item-%02d", i), TS: int64(i)})
	}

	return dbx.NewPaginatedSelectFunc[record](db, "foo", "ts", "name", "", recordCursor)
}

func TestPaginatedSelectAll(t *testing.T) {
	list := setupPaginateDB(t)

	rows, pr, err := list(context.Background(), dbx.PageRequest{})
	require.NoError(t, err)
	require.Len(t, rows, 20)
	require.False(t, pr.HasMore)
	require.NotEmpty(t, pr.FirstCursor)
	require.NotEmpty(t, pr.LastCursor)
}

func TestPaginatedSelectEmpty(t *testing.T) {
	list := dbx.NewPaginatedSelectFunc[record](openPaginateDB(t), "foo", "ts", "name", "", recordCursor)

	rows, pr, err := list(context.Background(), dbx.PageRequest{PageSize: 5})
	require.NoError(t, err)
	require.NotNil(t, rows)
	require.Empty(t, rows)
	require.Equal(t, dbx.PageResponse{}, pr)
}

func TestPaginatedSelectFirstPage(t *testing.T) {
	list := setupPaginateDB(t)

	rows, pr, err := list(context.Background(), dbx.PageRequest{PageSize: 5})
	require.NoError(t, err)
	require.Equal(t, []string{"item-00", "item-01", "item-02", "item-03", "item-04"}, recordNames(rows))
	require.True(t, pr.HasMore)
}

func TestPaginatedSelectForward(t *testing.T) {
	list := setupPaginateDB(t)

	_, pr, err := list(context.Background(), dbx.PageRequest{PageSize: 5})
	require.NoError(t, err)

	rows, pr, err := list(context.Background(), dbx.PageRequest{PageSize: 5, After: pr.LastCursor})
	require.NoError(t, err)
	require.Equal(t, []string{"item-05", "item-06", "item-07", "item-08", "item-09"}, recordNames(rows))
	require.True(t, pr.HasMore)
}

func TestPaginatedSelectBackward(t *testing.T) {
	list := setupPaginateDB(t)

	// Walk forward to the page starting at item-10.
	_, pr, err := list(context.Background(), dbx.PageRequest{PageSize: 10})
	require.NoError(t, err)
	rows, pr, err := list(context.Background(), dbx.PageRequest{PageSize: 5, After: pr.LastCursor})
	require.NoError(t, err)
	require.Equal(t, "item-10", rows[0].Name)

	rows, pr, err = list(context.Background(), dbx.PageRequest{PageSize: 5, Before: pr.FirstCursor})
	require.NoError(t, err)
	// Results are ascending despite the backward fetch.
	require.Equal(t, []string{"item-05", "item-06", "item-07", "item-08", "item-09"}, recordNames(rows))
	require.True(t, pr.HasMore, "rows exist before item-05")

	rows, pr, err = list(context.Background(), dbx.PageRequest{PageSize: 5, Before: pr.FirstCursor})
	require.NoError(t, err)
	require.Equal(t, "item-00", rows[0].Name)
	require.False(t, pr.HasMore, "no rows before item-00")
}

func TestPaginatedSelectLastPage(t *testing.T) {
	list := setupPaginateDB(t)

	_, pr, err := list(context.Background(), dbx.PageRequest{PageSize: 18})
	require.NoError(t, err)

	rows, pr, err := list(context.Background(), dbx.PageRequest{PageSize: 5, After: pr.LastCursor})
	require.NoError(t, err)
	require.Equal(t, []string{"item-18", "item-19"}, recordNames(rows))
	require.False(t, pr.HasMore)
}

// Rows sharing an orderCol value must neither be skipped nor repeated across
// page boundaries, in either direction.
func TestPaginatedSelectTies(t *testing.T) {
	db := openPaginateDB(t)
	want := []string{}
	for i := range 9 {
		r := record{Name: fmt.Sprintf("r%d", i), TS: int64(i / 3)}
		addRecords(t, db, r)
		want = append(want, r.Name)
	}
	list := dbx.NewPaginatedSelectFunc[record](db, "foo", "ts", "name", "", recordCursor)

	forward := []string{}
	page := dbx.PageRequest{PageSize: 2}
	for {
		rows, pr, err := list(context.Background(), page)
		require.NoError(t, err)
		forward = append(forward, recordNames(rows)...)
		if !pr.HasMore {
			break
		}
		require.NotEmpty(t, rows)
		page = dbx.PageRequest{PageSize: 2, After: pr.LastCursor}
	}
	require.Equal(t, want, forward)

	_, pr, err := list(context.Background(), dbx.PageRequest{})
	require.NoError(t, err)
	backward := []string{}
	page = dbx.PageRequest{PageSize: 2, Before: pr.LastCursor}
	for {
		rows, pr, err := list(context.Background(), page)
		require.NoError(t, err)
		backward = append(recordNames(rows), backward...)
		if !pr.HasMore {
			break
		}
		page = dbx.PageRequest{PageSize: 2, Before: pr.FirstCursor}
	}
	require.Equal(t, want[:8], backward)
}

func TestPaginatedSelectUniqueOrderColumn(t *testing.T) {
	db := openPaginateDB(t)
	for i := range 5 {
		addRecords(t, db, record{Name: fmt.Sprintf("r%d", i), TS: int64(i)})
	}
	list := dbx.NewPaginatedSelectFunc[record](db, "foo", "ts", "ts", "",
		func(r record) (string, string) { return strconv.FormatInt(r.TS, 10), "" })

	_, pr, err := list(context.Background(), dbx.PageRequest{PageSize: 2})
	require.NoError(t, err)
	rows, _, err := list(context.Background(), dbx.PageRequest{PageSize: 2, After: pr.LastCursor})
	require.NoError(t, err)
	require.Equal(t, []string{"r2", "r3"}, recordNames(rows))
}

func TestPaginatedSelectWithWhere(t *testing.T) {
	db := openPaginateDB(t)
	for i := range 10 {
		addRecords(t, db,
			record{Name: "alpha", TS: int64(i)},
			record{Name: "beta", TS: int64(100 + i)},
		)
	}

	for _, where := range []string{"WHERE name = ?", "where name = ?", "name = ?"} {
		list := dbx.NewPaginatedSelectFunc[record](db, "foo", "ts", "ts", where, recordCursor)

		rows, pr, err := list(context.Background(), dbx.PageRequest{PageSize: 5}, "alpha")
		require.NoError(t, err, where)
		require.Len(t, rows, 5)
		require.True(t, pr.HasMore)
		for _, r := range rows {
			require.Equal(t, "alpha", r.Name)
		}
	}
}

// An OR in baseWhere must not capture the cursor condition.
func TestPaginatedSelectWhereWithOr(t *testing.T) {
	db := openPaginateDB(t)
	addRecords(t, db,
		record{Name: "a", TS: 0},
		record{Name: "b", TS: 1},
		record{Name: "a", TS: 2},
		record{Name: "c", TS: 3},
	)
	list := dbx.NewPaginatedSelectFunc[record](db, "foo", "ts", "ts", "WHERE name = ? OR name = ?", recordCursor)

	rows, pr, err := list(context.Background(), dbx.PageRequest{PageSize: 1}, "a", "b")
	require.NoError(t, err)
	require.Equal(t, []int64{0}, []int64{rows[0].TS})

	rows, _, err = list(context.Background(), dbx.PageRequest{PageSize: 10, After: pr.LastCursor}, "a", "b")
	require.NoError(t, err)
	require.Len(t, rows, 2)
	require.Equal(t, int64(1), rows[0].TS)
	require.Equal(t, int64(2), rows[1].TS)
}

func TestPaginatedSelectAfterAndBeforeMutuallyExclusive(t *testing.T) {
	list := setupPaginateDB(t)

	_, _, err := list(context.Background(), dbx.PageRequest{PageSize: 5, After: "1", Before: "10"})
	require.ErrorIs(t, err, dbx.ErrMutuallyExclusive)
}

func TestPaginatedSelectNegativePageSize(t *testing.T) {
	list := setupPaginateDB(t)

	_, _, err := list(context.Background(), dbx.PageRequest{PageSize: -1})
	require.ErrorIs(t, err, dbx.ErrInvalidPageSize)
}

func TestPaginatedSelectInvalidCursor(t *testing.T) {
	list := setupPaginateDB(t)

	for _, cursor := range []string{"9", "!!", "bm90IGpzb24"} {
		_, _, err := list(context.Background(), dbx.PageRequest{PageSize: 5, After: cursor})
		require.ErrorIs(t, err, dbx.ErrInvalidCursor, cursor)
	}
}

func TestPaginatedSelectClampPageSize(t *testing.T) {
	list := setupPaginateDB(t)

	// Request more than MaxPageSize — should be clamped, not error
	rows, _, err := list(context.Background(), dbx.PageRequest{PageSize: dbx.MaxPageSize + 100})
	require.NoError(t, err)
	require.Len(t, rows, 20) // only 20 rows in the table
}

func TestValidSQLIdentifier(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"users", true},
		{"foo_bar", true},
		{"col123", true},
		{"123table", false},
		{"", false},
		{"foo bar", false},
		{"foo;DROP", false},
		{"foo-bar", false},
		{"table.col", false},
		{"foo`bar", false},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			require.Equal(t, tt.want, dbx.ValidSQLIdentifier(tt.input))
		})
	}
}

func TestNewPaginatedSelectFuncPanics(t *testing.T) {
	db := openPaginateDB(t)

	tests := []struct {
		name                     string
		table, order, key, where string
	}{
		{"invalid table", "foo;DROP", "ts", "name", ""},
		{"invalid order column", "foo", "ts;DROP", "name", ""},
		{"invalid key column", "foo", "ts", "name;DROP", ""},
		{"bad where", "foo", "ts", "name", "WHERE nope = ?"},
		{"reserved word", "foo", "order", "name", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Panics(t, func() {
				dbx.NewPaginatedSelectFunc[record](db, tt.table, tt.order, tt.key, tt.where, recordCursor)
			})
		})
	}
}
