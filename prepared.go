package dbx

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/jmoiron/sqlx"
)

type NamedExecFunc[T any] func(ctx context.Context, entity T) (Result, error)
type QueryRowxFunc[T any] func(ctx context.Context, entity T) (T, error)
type SelectFunc[T any] func(ctx context.Context, args ...any) ([]T, error)
type SelectIteratorFunc[T any] func(ctx context.Context, args ...any) (func(func(T, error) bool), error)
type ExecFunc func(ctx context.Context, args ...any) error

// NewNamedExecFunc creates a new prepared named exec function. This is suitable for
// inserts with entity, updates etc.
func NewNamedExecFunc[T any](db *sqlx.DB, stmt string) NamedExecFunc[T] {
	prepared, err := db.PrepareNamed(stmt)
	if err != nil {
		panic(err)
	}

	return func(ctx context.Context, entity T) (Result, error) {
		res, err := prepared.ExecContext(ctx, entity)
		if err != nil {
			return Result{}, fmt.Errorf("statement [%s]: %w", stmt, err)
		}

		result := newResult(res)
		if rows, ok := result.RowsAffected(); ok && rows == 0 {
			return result, sql.ErrNoRows
		}

		return result, nil
	}
}

// NewQueryRowxFunc creates a new prepared named exec function. This
// variant is suitable if you have a RETURNING clause in your statement.
func NewQueryRowxFunc[T any](db *sqlx.DB, stmt string) QueryRowxFunc[T] {
	prepared, err := db.PrepareNamed(stmt)
	if err != nil {
		panic(err)
	}

	var zero T

	return func(ctx context.Context, entity T) (T, error) {
		row := prepared.QueryRowxContext(ctx, entity)
		if row.Err() != nil {
			return zero, fmt.Errorf("statement [%s]: %w", stmt, row.Err())
		}

		var result T
		err := row.StructScan(&result)

		if err != nil {
			return zero, fmt.Errorf("failed StructScan [%s]: %w", stmt, err)
		}

		return result, nil
	}
}

func NewSelectFunc[T any](db *sqlx.DB, stmt string) SelectFunc[T] {
	prepared, err := db.Preparex(stmt)
	if err != nil {
		panic(err)
	}

	return func(ctx context.Context, args ...any) ([]T, error) {
		var entities []T
		err := prepared.Select(&entities, args...)
		if err != nil {
			return nil, fmt.Errorf("statement [%s]: %w", stmt, err)
		}

		return entities, nil
	}
}

func NewSelectIteratorFunc[T any](db *sqlx.DB, stmt string) SelectIteratorFunc[T] {
	prepared, err := db.Preparex(stmt)
	if err != nil {
		panic(err)
	}

	return func(ctx context.Context, args ...any) (func(func(T, error) bool), error) {
		rows, err := prepared.QueryxContext(ctx, args...)
		if err != nil {
			return nil, fmt.Errorf("statement [%s]: %w", stmt, err)
		}

		return func(yield func(T, error) bool) {
			defer rows.Close()

			// Next respects context cancellation
			for rows.Next() {
				var entity T
				if err := rows.StructScan(&entity); err != nil {
					_ = yield(*new(T), err)
					return
				}

				if !yield(entity, nil) {
					return
				}
			}

			if err := rows.Err(); err != nil {
				_ = yield(*new(T), err)
			}
		}, nil
	}
}

func NewExecFunc(db *sqlx.DB, stmt string) ExecFunc {
	prepared, err := db.Preparex(stmt)
	if err != nil {
		panic(err)
	}

	return func(ctx context.Context, args ...any) error {
		res, err := prepared.ExecContext(ctx, args...)
		if err != nil {
			return err
		}

		result := newResult(res)
		if rows, ok := result.RowsAffected(); ok && rows == 0 {
			return sql.ErrNoRows
		}

		return nil
	}
}
