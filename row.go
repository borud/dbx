package dbx

import (
	"context"
	"database/sql"
	"errors"

	"github.com/jmoiron/sqlx"
)

// errors
var (
	ErrZeroRowsAffected = errors.New("no rows affected by operation")
)

// RowsIter ranges over rows and StructScan's into T (which must be a struct).
//
//	rows, err := db.QueryxContext(ctx, "SELECT id, name FROM person")
//	if err != nil {
//	  return err
//	}
//
//	for rec, err := range dbx.RowsIter[record](ctx, rows) {
//	     if err != nil {
//	       break // or handle error
//	     }
//	     doSomethingWith(rec)
//	}
//
// T must be a struct type with matching field names or `db:"col"` tags. If the
// loop breaks early, rows are still closed via defer.
//
// This function is useful in cases where we stream the result of a query
// rather than slurp everything into memory before responding to client.  It is
// also somewhat more syntactically pleasing than the alternative.
func RowsIter[T any](ctx context.Context, rows *sqlx.Rows) func(func(T, error) bool) {
	return func(yield func(T, error) bool) {
		defer rows.Close()

		var zero T

		for rows.Next() {
			// early cancellation
			if err := ctx.Err(); err != nil {
				_ = yield(zero, err)
				return
			}

			var v T
			if err := rows.StructScan(&v); err != nil {
				_ = yield(zero, err)
				return
			}

			// deliver the value; stop if caller breaks
			if !yield(v, nil) {
				return
			}

			// post-yield cancellation
			if err := ctx.Err(); err != nil {
				_ = yield(zero, err)
				return
			}
		}

		// propagate driver iteration error after loop
		if err := rows.Err(); err != nil {
			_ = yield(zero, err)
		}
	}
}

// CheckForZeroRowsAffected ensures that if zero rows are affected by operations that
// should have side-effects, or an error is returned.
func CheckForZeroRowsAffected(r sql.Result, err error) error {
	if r == nil {
		return err
	}
	affected, err2 := r.RowsAffected()
	if err2 != nil {
		return err2
	}
	if affected == 0 {
		return ErrZeroRowsAffected
	}

	return err
}
