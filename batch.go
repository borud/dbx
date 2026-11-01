package dbx

import (
	"context"
	"fmt"
	"reflect"
	"strconv"
	"strings"

	"github.com/jmoiron/sqlx"
)

// TxBatchExecFunc executes a multi-row statement for a slice of entities
// inside a transaction, one statement per chunk of up to maxRows entities.
// It returns one [Result] per statement, in order.
type TxBatchExecFunc[T any] func(ctx context.Context, tx *sqlx.Tx, entities []T) ([]Result, error)

// NewTxBatchExecFunc creates a TxBatchExecFunc. The statement is head, then
// row once per entity separated by commas, then tail:
//
//	head: "INSERT INTO foo (name, ts) VALUES "
//	row:  "(:name, :ts)"
//	tail: " ON CONFLICT DO NOTHING" (or "")
//
// Names in row map to T's db tags as in [NewNamedExecFunc] and are replaced
// by the driver's bindvar; a literal colon in row is written "::". head and
// tail are used verbatim: they are not parsed for names, "::" in them stays
// "::", and they must not contain placeholders.
//
// Statements for 1 to maxRows rows are prepared on db at construction time.
// Keep maxRows small: some drivers bind parameters in time quadratic in their
// number, and SQLite allows at most 32766 per statement.
//
// A nil pointer on the path to a named field, such as a nil element when T
// is a pointer type, is returned as an error.
func NewTxBatchExecFunc[T any](db *sqlx.DB, head, row, tail string, maxRows int) TxBatchExecFunc[T] {
	if maxRows < 1 {
		panic(fmt.Sprintf("dbx: maxRows must be at least 1, got %d", maxRows))
	}

	parts, names := compileRow(row)
	fields := db.Mapper.TraversalsByName(reflect.TypeFor[T](), names)
	for i, f := range fields {
		if len(f) == 0 {
			panic(fmt.Sprintf("dbx: no field for :%s in %T", names[i], *new(T)))
		}
	}

	bindType := sqlx.BindType(db.DriverName())
	prepared := make([]*sqlx.Stmt, 0, maxRows)
	for n := 1; n <= maxRows; n++ {
		st, err := db.Preparex(batchStatement(bindType, head, parts, tail, n))
		if err != nil {
			for _, p := range prepared {
				_ = p.Close()
			}
			panic(err)
		}
		prepared = append(prepared, st)
	}

	stmt := head + row + tail
	return func(ctx context.Context, tx *sqlx.Tx, entities []T) ([]Result, error) {
		results := make([]Result, 0, (len(entities)+maxRows-1)/maxRows)
		args := make([]any, 0, min(len(entities), maxRows)*len(fields))

		for start := 0; start < len(entities); start += maxRows {
			chunk := entities[start:min(start+maxRows, len(entities))]

			args = args[:0]
			for i := range chunk {
				var err error
				args, err = appendFieldArgs(args, reflect.ValueOf(&chunk[i]).Elem(), fields, names)
				if err != nil {
					return results, fmt.Errorf("batch statement [%s] entity %d: %w", stmt, start+i, err)
				}
			}

			res, err := tx.StmtxContext(ctx, prepared[len(chunk)-1]).ExecContext(ctx, args...)
			if err != nil {
				return results, fmt.Errorf("batch statement [%s] x%d: %w", stmt, len(chunk), err)
			}
			results = append(results, NewResult(res))
		}

		return results, nil
	}
}

// appendFieldArgs appends the value of each field of v to args, returning an
// error instead of panicking on a nil pointer along a field's path.
func appendFieldArgs(args []any, v reflect.Value, fields [][]int, names []string) ([]any, error) {
	for k, f := range fields {
		fv := v
		for _, i := range f {
			fv = reflect.Indirect(fv)
			if !fv.IsValid() {
				return args, fmt.Errorf("nil pointer on the path to :%s", names[k])
			}
			fv = fv.Field(i)
		}
		args = append(args, fv.Interface())
	}

	return args, nil
}

// batchStatement builds the statement for the given number of rows, numbering
// bindvars across all rows.
func batchStatement(bindType int, head string, parts []string, tail string, rows int) string {
	var b strings.Builder
	b.WriteString(head)

	arg := 1
	for r := range rows {
		if r > 0 {
			b.WriteByte(',')
		}
		b.WriteString(parts[0])
		for _, p := range parts[1:] {
			b.WriteString(bindvar(bindType, arg))
			b.WriteString(p)
			arg++
		}
	}

	b.WriteString(tail)
	return b.String()
}

// bindvar returns the n'th (1-based) placeholder for bindType, as
// [sqlx.Rebind] writes it.
func bindvar(bindType int, n int) string {
	switch bindType {
	case sqlx.UNKNOWN, sqlx.QUESTION:
		return "?"
	case sqlx.DOLLAR:
		return "$" + strconv.Itoa(n)
	case sqlx.NAMED:
		return ":arg" + strconv.Itoa(n)
	case sqlx.AT:
		return "@p" + strconv.Itoa(n)
	default:
		panic(fmt.Sprintf("dbx: unexpected bind type: %d", bindType))
	}
}

// compileRow splits a named row template into the literal text around its
// names and returns the names in order, so len(parts) == len(names)+1. "::"
// is a literal colon.
func compileRow(row string) ([]string, []string) {
	var b strings.Builder
	parts := []string{}
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
		parts = append(parts, b.String())
		b.Reset()
		i = j - 1
	}

	return append(parts, b.String()), names
}

func isNameByte(c byte) bool {
	return c == '_' || c == '.' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
}
