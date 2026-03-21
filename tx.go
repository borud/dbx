package dbx

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"

	"github.com/jmoiron/sqlx"
)

// RunTx executes fn inside a transaction. If fn returns an error or
// panics the transaction is rolled back; otherwise it is committed.
func RunTx(ctx context.Context, db *sqlx.DB, fn func(tx *sqlx.Tx) error) error {
	tx, err := db.BeginTxx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() {
		if err := tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
			slog.Error("rollback failed", "err", err)
		}
	}()

	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}

// TxExecFunc executes a statement with positional arguments inside a
// transaction. It mirrors [ExecFunc] but operates on a [*sqlx.Tx].
type TxExecFunc func(ctx context.Context, tx *sqlx.Tx, args ...any) error

// NewTxExecFunc creates a TxExecFunc for the given SQL statement.
// Unlike [NewExecFunc] the statement is not prepared at construction
// time since the transaction does not exist yet.
func NewTxExecFunc(stmt string) TxExecFunc {
	return func(ctx context.Context, tx *sqlx.Tx, args ...any) error {
		res, err := tx.ExecContext(ctx, stmt, args...)
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

// TxNamedExecFunc executes a named statement inside a transaction.
// It mirrors [NamedExecFunc] but operates on a [*sqlx.Tx].
type TxNamedExecFunc[T any] func(ctx context.Context, tx *sqlx.Tx, entity T) (Result, error)

// NewTxNamedExecFunc creates a TxNamedExecFunc for the given SQL statement.
func NewTxNamedExecFunc[T any](stmt string) TxNamedExecFunc[T] {
	return func(ctx context.Context, tx *sqlx.Tx, entity T) (Result, error) {
		res, err := tx.NamedExecContext(ctx, stmt, entity)
		if err != nil {
			return Result{}, fmt.Errorf("statement [%s]: %w", stmt, err)
		}

		return NewResult(res), nil
	}
}

// TxQueryRowxFunc queries a single row inside a transaction with
// positional arguments. It mirrors [QueryRowxFunc] but operates on a
// [*sqlx.Tx].
type TxQueryRowxFunc[T any] func(ctx context.Context, tx *sqlx.Tx, args ...any) (T, error)

// NewTxQueryRowxFunc creates a TxQueryRowxFunc for the given SQL statement.
func NewTxQueryRowxFunc[T any](stmt string) TxQueryRowxFunc[T] {
	var zero T

	return func(ctx context.Context, tx *sqlx.Tx, args ...any) (T, error) {
		row := tx.QueryRowxContext(ctx, stmt, args...)
		if row.Err() != nil {
			return zero, fmt.Errorf("statement [%s]: %w", stmt, row.Err())
		}

		var result T
		if err := row.StructScan(&result); err != nil {
			return zero, fmt.Errorf("failed StructScan [%s]: %w", stmt, err)
		}

		return result, nil
	}
}

// TxEntityQueryRowxFunc executes a named statement inside a transaction
// that takes an entity and returns an entity of the same type. It
// mirrors [EntityQueryRowxFunc] but operates on a [*sqlx.Tx].
type TxEntityQueryRowxFunc[T any] func(ctx context.Context, tx *sqlx.Tx, entity T) (T, error)

// NewTxEntityQueryRowxFunc creates a TxEntityQueryRowxFunc for the given SQL statement.
func NewTxEntityQueryRowxFunc[T any](stmt string) TxEntityQueryRowxFunc[T] {
	var zero T

	return func(ctx context.Context, tx *sqlx.Tx, entity T) (T, error) {
		row, err := sqlx.NamedQueryContext(ctx, tx, stmt, entity)
		if err != nil {
			return zero, fmt.Errorf("statement [%s]: %w", stmt, err)
		}
		defer row.Close()

		if !row.Next() {
			if err := row.Err(); err != nil {
				return zero, fmt.Errorf("statement [%s]: %w", stmt, err)
			}
			return zero, fmt.Errorf("statement [%s]: %w", stmt, sql.ErrNoRows)
		}

		var result T
		if err := row.StructScan(&result); err != nil {
			return zero, fmt.Errorf("failed StructScan [%s]: %w", stmt, err)
		}

		return result, nil
	}
}
