package dbx

import (
	"context"
	"fmt"
	"reflect"
	"strings"

	"github.com/jmoiron/sqlx"
	"github.com/jmoiron/sqlx/reflectx"
)

// TxBatchExecFunc executes a multi-row statement for a slice of entities
// inside a transaction, one statement per chunk of up to maxRows entities.
// It returns one [Result] per statement, in order.
type TxBatchExecFunc[T any] func(ctx context.Context, tx *sqlx.Tx, entities []T) ([]Result, error)

// NewTxBatchExecFunc creates a TxBatchExecFunc. head is the statement up to
// the rows, e.g. "INSERT INTO foo (name, ts) VALUES ", and row is the named
// template for one row, e.g. "(:name, :ts)". Names map to T's db tags as in
// [NewNamedExecFunc]; a literal colon is written "::".
//
// Statements for 1 to maxRows rows are prepared on db at construction time.
// Keep maxRows small: some drivers bind parameters in time quadratic in their
// number, and SQLite allows at most 32766 per statement.
func NewTxBatchExecFunc[T any](db *sqlx.DB, head, row string, maxRows int) TxBatchExecFunc[T] {
	if maxRows < 1 {
		panic(fmt.Sprintf("dbx: maxRows must be at least 1, got %d", maxRows))
	}

	positional, names := compileRow(row)
	fields := db.Mapper.TraversalsByName(reflect.TypeFor[T](), names)
	for i, f := range fields {
		if len(f) == 0 {
			panic(fmt.Sprintf("dbx: no field for :%s in %T", names[i], *new(T)))
		}
	}

	prepared := make([]*sqlx.Stmt, maxRows+1)
	for n := 1; n <= maxRows; n++ {
		st, err := db.Preparex(db.Rebind(head + strings.Repeat(positional+",", n-1) + positional))
		if err != nil {
			panic(err)
		}
		prepared[n] = st
	}

	return func(ctx context.Context, tx *sqlx.Tx, entities []T) ([]Result, error) {
		results := make([]Result, 0, (len(entities)+maxRows-1)/maxRows)
		args := make([]any, 0, min(len(entities), maxRows)*len(fields))

		for start := 0; start < len(entities); start += maxRows {
			chunk := entities[start:min(start+maxRows, len(entities))]

			args = args[:0]
			for i := range chunk {
				v := reflect.Indirect(reflect.ValueOf(&chunk[i]))
				for _, f := range fields {
					args = append(args, reflectx.FieldByIndexesReadOnly(v, f).Interface())
				}
			}

			res, err := tx.StmtxContext(ctx, prepared[len(chunk)]).ExecContext(ctx, args...)
			if err != nil {
				return results, fmt.Errorf("batch statement [%s%s] x%d: %w", head, row, len(chunk), err)
			}
			results = append(results, NewResult(res))
		}

		return results, nil
	}
}

// compileRow turns a named row template into one with "?" placeholders and
// returns the names in order. "::" is a literal colon.
func compileRow(row string) (string, []string) {
	var b strings.Builder
	names := []string{}

	for i := 0; i < len(row); i++ {
		c := row[i]
		if c != ':' {
			b.WriteByte(c)
			continue
		}
		if i+1 < len(row) && row[i+1] == ':' {
			b.WriteByte(':')
			i++
			continue
		}

		j := i + 1
		for j < len(row) && isNameByte(row[j]) {
			j++
		}
		if j == i+1 {
			b.WriteByte(c)
			continue
		}
		names = append(names, row[i+1:j])
		b.WriteByte('?')
		i = j - 1
	}

	return b.String(), names
}

func isNameByte(c byte) bool {
	return c == '_' || c == '.' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
}
