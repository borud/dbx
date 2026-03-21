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
// execute against a [*sqlx.Tx] instead of [*sqlx.DB]. Because the
// transaction does not exist at construction time, these functions are
// not pre-prepared — the SQL string is executed directly on each call.
//
// [RunTx] handles the begin/commit/rollback boilerplate:
//
//	addTx := dbx.NewTxNamedExecFunc[Device]("INSERT INTO devices ... VALUES ...")
//	pskTx := dbx.NewTxNamedExecFunc[PSK]("INSERT INTO psk ... VALUES ...")
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
