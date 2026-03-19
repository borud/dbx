package dbx

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/jmoiron/sqlx"
)

// ExecFunc is useful for very simple operations like DELETE with positional arguments.
type ExecFunc func(ctx context.Context, args ...any) error

// NamedExecFunc is useful for create, update etc where you send an entity in and
// just want a Result and error back.
type NamedExecFunc[T any] func(ctx context.Context, entity T) (Result, error)

// QueryRowxFunc is useful for queries that return one row and takes positional
// arguments.  For instance get operations on a single row.
type QueryRowxFunc[T any] func(ctx context.Context, args ...any) (T, error)

// EntityQueryRowxFunc is useful for queries that take some entity and return
// an entity of the same type.  For instance when using RETURNING in SQL
// statement.
type EntityQueryRowxFunc[T any] func(ctx context.Context, entity T) (T, error)

// SelectFunc is useful for when you perform selects and you know the result set will
// be small or at least bounded to acceptable size.
type SelectFunc[T any] func(ctx context.Context, args ...any) ([]T, error)

// QueryxIteratorFunc is useful for queries that return (potentially) large
// result sets and you want to be able to stream the result.
type QueryxIteratorFunc[T any] func(ctx context.Context, args ...any) func(func(T, error) bool)

// NewExecFunc creates an ExecFunc
func NewExecFunc(db *sqlx.DB, stmt string) ExecFunc {
	prepared, err := db.Preparex(stmt)
	if err != nil {
		panic(err)
	}

	return func(ctx context.Context, args ...any) error {
		res, err := prepared.ExecContext(ctx, args...)
		if err != nil {
			return fmt.Errorf("statement [%s]: %w", stmt, err)
		}

		result := NewResult(res)
		if rows, ok := result.RowsAffected(); ok && rows == 0 {
			return sql.ErrNoRows
		}

		return nil
	}
}

// NewNamedExecFunc creates a new NamedExecFunc
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

		return NewResult(res), nil
	}
}

// NewQueryRowxFunc creates a new QueryRowxFunc
func NewQueryRowxFunc[T any](db *sqlx.DB, stmt string) QueryRowxFunc[T] {
	prepared, err := db.Preparex(stmt)
	if err != nil {
		panic(err)
	}

	var zero T

	return func(ctx context.Context, args ...any) (T, error) {
		row := prepared.QueryRowxContext(ctx, args...)
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

// NewEntityQueryRowxFunc creates a new EntityQueryRowxFunc
func NewEntityQueryRowxFunc[T any](db *sqlx.DB, stmt string) EntityQueryRowxFunc[T] {
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

// NewSelectFunc creates a new SelectFunc
func NewSelectFunc[T any](db *sqlx.DB, stmt string) SelectFunc[T] {
	prepared, err := db.Preparex(stmt)
	if err != nil {
		panic(err)
	}

	return func(ctx context.Context, args ...any) ([]T, error) {
		var entities []T
		err := prepared.SelectContext(ctx, &entities, args...)
		if err != nil {
			return nil, fmt.Errorf("statement [%s]: %w", stmt, err)
		}

		return entities, nil
	}
}

// NewQueryxIteratorFunc creates a new QueryxIteratorFunc
func NewQueryxIteratorFunc[T any](db *sqlx.DB, stmt string) QueryxIteratorFunc[T] {
	prepared, err := db.Preparex(stmt)
	if err != nil {
		panic(err)
	}

	return func(ctx context.Context, args ...any) func(func(T, error) bool) {
		rows, err := prepared.QueryxContext(ctx, args...)

		return func(yield func(T, error) bool) {
			// Handle query error before iteration
			if err != nil {
				_ = yield(*new(T), fmt.Errorf("statement [%s]: %w", stmt, err))
				return
			}

			defer rows.Close()

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
		}
	}
}
