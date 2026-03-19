package dbx_test

import (
	"context"
	"fmt"
	"strconv"
	"testing"

	"github.com/borud/dbx"
	"github.com/stretchr/testify/require"
)

func setupPaginateDB(t *testing.T) *dbx.PaginatedSelectFunc[record] {
	t.Helper()

	db, err := dbx.OpenSQLX(
		dbx.WithDSN(":memory:"),
		dbx.WithDriver("sqlite"),
		dbx.WithMigrations(migrationsFS, "testmigrations"),
	)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })

	add := dbx.NewNamedExecFunc[record](db, "INSERT INTO foo (name,ts) VALUES (:name,:ts)")

	for i := range 20 {
		_, err := add(context.Background(), record{
			Name: fmt.Sprintf("item-%02d", i),
			TS:   int64(i),
		})
		require.NoError(t, err)
	}

	list := dbx.NewPaginatedSelectFunc[record](db, "foo", "ts", "",
		func(r record) string { return strconv.FormatInt(r.TS, 10) },
	)
	return &list
}

func TestPaginatedSelectAll(t *testing.T) {
	list := setupPaginateDB(t)

	rows, pr, err := (*list)(context.Background(), dbx.PageRequest{})
	require.NoError(t, err)
	require.Len(t, rows, 20)
	require.False(t, pr.HasMore)
	require.Equal(t, "0", pr.FirstCursor)
	require.Equal(t, "19", pr.LastCursor)
}

func TestPaginatedSelectFirstPage(t *testing.T) {
	list := setupPaginateDB(t)

	rows, pr, err := (*list)(context.Background(), dbx.PageRequest{PageSize: 5})
	require.NoError(t, err)
	require.Len(t, rows, 5)
	require.True(t, pr.HasMore)
	require.Equal(t, "0", pr.FirstCursor)
	require.Equal(t, "4", pr.LastCursor)
}

func TestPaginatedSelectForward(t *testing.T) {
	list := setupPaginateDB(t)

	// First page
	rows, pr, err := (*list)(context.Background(), dbx.PageRequest{PageSize: 5})
	require.NoError(t, err)
	require.Len(t, rows, 5)
	require.True(t, pr.HasMore)

	// Second page using After cursor
	rows2, pr2, err := (*list)(context.Background(), dbx.PageRequest{PageSize: 5, After: pr.LastCursor})
	require.NoError(t, err)
	require.Len(t, rows2, 5)
	require.True(t, pr2.HasMore)
	require.Equal(t, "5", pr2.FirstCursor)
	require.Equal(t, "9", pr2.LastCursor)
}

func TestPaginatedSelectBackward(t *testing.T) {
	list := setupPaginateDB(t)

	// Get a middle page first
	rows, _, err := (*list)(context.Background(), dbx.PageRequest{PageSize: 5, After: "9"})
	require.NoError(t, err)
	require.Len(t, rows, 5)

	// Go backward from the first item of that page
	rows2, pr2, err := (*list)(context.Background(), dbx.PageRequest{PageSize: 5, Before: "10"})
	require.NoError(t, err)
	require.Len(t, rows2, 5)
	// Results should be in ASC order despite backward fetch
	require.Equal(t, "5", pr2.FirstCursor)
	require.Equal(t, "9", pr2.LastCursor)
}

func TestPaginatedSelectLastPage(t *testing.T) {
	list := setupPaginateDB(t)

	rows, pr, err := (*list)(context.Background(), dbx.PageRequest{PageSize: 5, After: "17"})
	require.NoError(t, err)
	require.Len(t, rows, 2) // items 18 and 19
	require.False(t, pr.HasMore)
	require.Equal(t, "18", pr.FirstCursor)
	require.Equal(t, "19", pr.LastCursor)
}

func TestPaginatedSelectWithWhere(t *testing.T) {
	db, err := dbx.OpenSQLX(
		dbx.WithDSN(":memory:"),
		dbx.WithDriver("sqlite"),
		dbx.WithMigrations(migrationsFS, "testmigrations"),
	)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })

	add := dbx.NewNamedExecFunc[record](db, "INSERT INTO foo (name,ts) VALUES (:name,:ts)")
	for i := range 10 {
		_, err := add(context.Background(), record{
			Name: "alpha",
			TS:   int64(i),
		})
		require.NoError(t, err)
	}
	for i := range 10 {
		_, err := add(context.Background(), record{
			Name: "beta",
			TS:   int64(100 + i),
		})
		require.NoError(t, err)
	}

	list := dbx.NewPaginatedSelectFunc[record](db, "foo", "ts", "WHERE name = ?",
		func(r record) string { return strconv.FormatInt(r.TS, 10) },
	)

	// List only "alpha" rows, paginated
	rows, pr, err := list(context.Background(), dbx.PageRequest{PageSize: 5}, "alpha")
	require.NoError(t, err)
	require.Len(t, rows, 5)
	require.True(t, pr.HasMore)
	for _, r := range rows {
		require.Equal(t, "alpha", r.Name)
	}
}

func TestPaginatedSelectAfterAndBeforeMutuallyExclusive(t *testing.T) {
	list := setupPaginateDB(t)

	_, _, err := (*list)(context.Background(), dbx.PageRequest{PageSize: 5, After: "1", Before: "10"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "mutually exclusive")
}

func TestPaginatedSelectClampPageSize(t *testing.T) {
	list := setupPaginateDB(t)

	// Request more than MaxPageSize — should be clamped, not error
	rows, _, err := (*list)(context.Background(), dbx.PageRequest{PageSize: dbx.MaxPageSize + 100})
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

func TestNewPaginatedSelectFuncPanicsOnInvalidTable(t *testing.T) {
	db, err := dbx.OpenSQLX(
		dbx.WithDSN(":memory:"),
		dbx.WithDriver("sqlite"),
		dbx.WithMigrations(migrationsFS, "testmigrations"),
	)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })

	require.Panics(t, func() {
		dbx.NewPaginatedSelectFunc[record](db, "foo;DROP", "ts", "", func(_ record) string { return "" })
	})
}

func TestNewPaginatedSelectFuncPanicsOnInvalidColumn(t *testing.T) {
	db, err := dbx.OpenSQLX(
		dbx.WithDSN(":memory:"),
		dbx.WithDriver("sqlite"),
		dbx.WithMigrations(migrationsFS, "testmigrations"),
	)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })

	require.Panics(t, func() {
		dbx.NewPaginatedSelectFunc[record](db, "foo", "ts;DROP", "", func(_ record) string { return "" })
	})
}
