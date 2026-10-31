// Package dbx provides opinionated database utilities built on top of
// [sqlx]. It covers three areas: opening databases with migrations,
// type-safe prepared statement wrappers using generics, and cursor-based
// pagination.
//
// # Opening a database
//
// [Open] and [OpenSQLX] open a database connection and optionally apply
// SQLite pragmas and schema migrations. Migrations are read from an
// [fs.FS] (typically an embedded filesystem) and applied via
// golang-migrate. Configuration is done through functional options:
//
//	db, err := dbx.OpenSQLX(
//	    dbx.WithDSN(":memory:"),
//	    dbx.WithDriver("sqlite"),
//	    dbx.WithPragmas(
//	        "PRAGMA foreign_keys = ON",
//	        "PRAGMA synchronous = NORMAL",
//	    ),
//	    dbx.WithMigrations(migrationsFS, "migrations"),
//	)
//
// # Prepared statements
//
// The package provides generic function types that wrap prepared
// statements. Constructors panic on invalid SQL so that wiring errors
// are caught at init time rather than at runtime.
//
// The underlying prepared statements are captured in closures and
// cannot be closed individually — they are released when the database
// connection is closed. This is by design for the recommended
// init-time wiring pattern:
//
//   - [NewExecFunc] — simple operations (DELETE, etc.) with positional args
//   - [NewNamedExecFunc] — INSERT/UPDATE with a struct, returns [Result]
//   - [NewQueryRowxFunc] — single-row query with positional args
//   - [NewEntityQueryRowxFunc] — struct in, struct out (e.g. RETURNING *)
//   - [NewSelectFunc] — bounded result sets
//   - [NewQueryxIteratorFunc] — streaming large result sets via range iterator
//   - [NewPaginatedSelectFunc] — cursor-based paginated SELECT (see Pagination)
//
// # Transactions
//
// The Tx* function types mirror the prepared-statement types above but
// execute against a [*sqlx.Tx] instead of [*sqlx.DB]. They are prepared on
// the [*sqlx.DB] at construction time and bound to the transaction on each
// call, so the SQL is parsed once. Construct them outside any transaction
// (typically in a newXStmts constructor): on a database with a single
// connection, preparing while a transaction holds it would wait forever.
//
// [RunTx] handles the begin/commit/rollback boilerplate:
//
//	addTx := dbx.NewTxNamedExecFunc[Device](db, "INSERT INTO devices ... VALUES ...")
//	pskTx := dbx.NewTxNamedExecFunc[PSK](db, "INSERT INTO psk ... VALUES ...")
//
//	err := dbx.RunTx(ctx, db, func(tx *sqlx.Tx) error {
//	    if _, err := addTx(ctx, tx, device); err != nil {
//	        return err
//	    }
//	    _, err := pskTx(ctx, tx, psk)
//	    return err
//	})
//
// Available Tx function types:
//
//   - [NewTxExecFunc] — positional-arg exec inside a transaction
//   - [NewTxNamedExecFunc] — named exec (struct) inside a transaction
//   - [NewTxQueryRowxFunc] — single-row query inside a transaction
//   - [NewTxEntityQueryRowxFunc] — struct in, struct out inside a transaction
//   - [NewTxBatchExecFunc] — multi-row statement for a slice of structs
//
// # Batch inserts
//
// [NewTxBatchExecFunc] inserts many rows with few statements. It takes the
// statement head and a named template for one row, prepares statements for
// 1 to maxRows rows, and binds struct fields by their db tags:
//
//	insert := dbx.NewTxBatchExecFunc[Event](db,
//	    "INSERT INTO events (time, path) VALUES ", "(:time, :path)", 8)
//
//	err := dbx.RunTx(ctx, db, func(tx *sqlx.Tx) error {
//	    _, err := insert(ctx, tx, events)
//	    return err
//	})
//
// Each statement costs the driver a fixed amount of work (with a cancellable
// context, modernc.org/sqlite starts a goroutine per statement), so batching
// is much faster than one statement per row. Keep maxRows small: some
// drivers bind parameters in time quadratic in their number.
//
// # Pagination
//
// [NewPaginatedSelectFunc] builds cursor-based paginated SELECT queries.
// Table and column names are validated as safe SQL identifiers at
// construction time. Queries are rebound via sqlx.Rebind for
// cross-database placeholder compatibility.
//
// # Breaking changes in v1.0.0
//
// [WithPragmas] now accepts variadic string arguments instead of a
// string slice. Change WithPragmas([]string{...}) to WithPragmas(...).
//
// [ValidSQLIdentifier] now rejects identifiers that start with a digit,
// aligning with standard SQL identifier rules.
package dbx
