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
// are caught at init time rather than at runtime:
//
//   - [NewExecFunc] — simple operations (DELETE, etc.) with positional args
//   - [NewNamedExecFunc] — INSERT/UPDATE with a struct, returns [Result]
//   - [NewQueryRowxFunc] — single-row query with positional args
//   - [NewEntityQueryRowxFunc] — struct in, struct out (e.g. RETURNING *)
//   - [NewSelectFunc] — bounded result sets
//   - [NewQueryxIteratorFunc] — streaming large result sets via range iterator
//   - [NewPaginatedSelectFunc] — cursor-based paginated SELECT (see Pagination)
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
