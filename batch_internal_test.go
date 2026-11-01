package dbx

import (
	"testing"

	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/require"
)

func TestBatchStatement(t *testing.T) {
	parts, names := compileRow("(:name, :ts::::int, '?')")
	require.Equal(t, []string{"name", "ts"}, names)

	tests := []struct {
		bindType int
		want     string
	}{
		{sqlx.QUESTION, "INSERT INTO foo VALUES (?, ?::int, '?'),(?, ?::int, '?') RETURNING id"},
		{sqlx.DOLLAR, "INSERT INTO foo VALUES ($1, $2::int, '?'),($3, $4::int, '?') RETURNING id"},
		{sqlx.NAMED, "INSERT INTO foo VALUES (:arg1, :arg2::int, '?'),(:arg3, :arg4::int, '?') RETURNING id"},
		{sqlx.AT, "INSERT INTO foo VALUES (@p1, @p2::int, '?'),(@p3, @p4::int, '?') RETURNING id"},
	}
	for _, tt := range tests {
		got := batchStatement(tt.bindType, "INSERT INTO foo VALUES ", parts, " RETURNING id", 2)
		require.Equal(t, tt.want, got)
	}
}
