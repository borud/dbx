package dbx

import (
	"context"

	"github.com/jmoiron/sqlx"
)

// RowsIter ranges over rows and StructScan's into T (which must be a struct).
//
//	  rows, err := db.QueryxContext(ctx, "SELECT id, name FROM person")
//	  if err != nil {
//	    return err
//	  }
//
//	  for rec, err := range dbx.RowsIter[record](ctx, rows) {
//		   if err != nil {
//		     break // or handle error
//		   }
//		   doSomethingWith(rec)
//	  }
//
// T must be a struct type with matching field names or `db:"col"` tags.
// If the loop breaks early, rows are still closed via defer.
// Check Err() after the loop to capture scan/iteration errors.
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
