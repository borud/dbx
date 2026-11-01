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
// transaction. It mirrors [ExecFunc], including its [sql.ErrNoRows] on zero
// rows affected, but operates on a [*sqlx.Tx].
type TxExecFunc func(ctx context.Context, tx *sqlx.Tx, args ...any) error

// NewTxExecFunc creates a TxExecFunc. The statement is prepared on db at
// construction time and bound to the transaction on each call, so it is
// parsed once. Construct it outside any transaction: on a database with a
// single connection, preparing inside one would wait forever.
func NewTxExecFunc(db *sqlx.DB, stmt string) TxExecFunc {
	prepared, err := db.Preparex(stmt)
	if err != nil {
		panic(err)
	}

	return func(ctx context.Context, tx *sqlx.Tx, args ...any) error {
		res, err := tx.StmtxContext(ctx, prepared).ExecContext(ctx, args...)
		if err != nil {
			return fmt.Errorf("statement [%s]: %w", stmt, err)
		}

		result := NewResult(res)
		if rows, ok := result.RowsAffected(); ok && rows == 0 {
			return fmt.Errorf("statement [%s]: no rows affected: %w", stmt, sql.ErrNoRows)
		}

		return nil
	}
}

// TxNamedExecFunc executes a named statement inside a transaction.
// It mirrors [NamedExecFunc] but operates on a [*sqlx.Tx].
type TxNamedExecFunc[T any] func(ctx context.Context, tx *sqlx.Tx, entity T) (Result, error)

// NewTxNamedExecFunc creates a TxNamedExecFunc. The statement is prepared on
// db at construction time, see [NewTxExecFunc].
func NewTxNamedExecFunc[T any](db *sqlx.DB, stmt string) TxNamedExecFunc[T] {
	prepared, err := db.PrepareNamed(stmt)
	if err != nil {
		panic(err)
	}

	return func(ctx context.Context, tx *sqlx.Tx, entity T) (Result, error) {
		res, err := tx.NamedStmtContext(ctx, prepared).ExecContext(ctx, entity)
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

// NewTxQueryRowxFunc creates a TxQueryRowxFunc. The statement is prepared on
// db at construction time, see [NewTxExecFunc].
func NewTxQueryRowxFunc[T any](db *sqlx.DB, stmt string) TxQueryRowxFunc[T] {
	var zero T

	prepared, err := db.Preparex(stmt)
	if err != nil {
		panic(err)
	}

	return func(ctx context.Context, tx *sqlx.Tx, args ...any) (T, error) {
		row := tx.StmtxContext(ctx, prepared).QueryRowxContext(ctx, args...)
		if row.Err() != nil {
			return zero, fmt.Errorf("statement [%s]: %w", stmt, row.Err())
		}

		var result T
		if err := row.StructScan(&result); err != nil {
			return zero, fmt.Errorf("statement [%s]: %w", stmt, err)
		}

		return result, nil
	}
}

// TxEntityQueryRowxFunc executes a named statement inside a transaction
// that takes an entity and returns an entity of the same type. It
// mirrors [EntityQueryRowxFunc] but operates on a [*sqlx.Tx].
type TxEntityQueryRowxFunc[T any] func(ctx context.Context, tx *sqlx.Tx, entity T) (T, error)

// NewTxEntityQueryRowxFunc creates a TxEntityQueryRowxFunc. The statement is
// prepared on db at construction time, see [NewTxExecFunc]. A query that
// yields no rows returns an error wrapping [sql.ErrNoRows].
func NewTxEntityQueryRowxFunc[T any](db *sqlx.DB, stmt string) TxEntityQueryRowxFunc[T] {
	var zero T

	prepared, err := db.PrepareNamed(stmt)
	if err != nil {
		panic(err)
	}

	return func(ctx context.Context, tx *sqlx.Tx, entity T) (T, error) {
		var result T
		err := tx.NamedStmtContext(ctx, prepared).QueryRowxContext(ctx, entity).StructScan(&result)
		if err != nil {
			return zero, fmt.Errorf("statement [%s]: %w", stmt, err)
		}

		return result, nil
	}
}
