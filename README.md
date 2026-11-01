# DBX - Opinionated database utilities for Go

[![Go Reference](https://pkg.go.dev/badge/github.com/borud/dbx/v2.svg)](https://pkg.go.dev/github.com/borud/dbx/v2)

Convenience layer on top of [sqlx](https://github.com/jmoiron/sqlx). No ORM — just prepared statements, migrations, and pagination.

## Table of contents

- [Opening a database](#opening-a-database)
  - [Migration files](#migration-files)
- [Prepared statements](#prepared-statements)
  - [Example: CRUD operations](#example-crud-operations)
  - [Streaming with QueryxIteratorFunc](#streaming-with-queryxiteratorfunc)
  - [Result type](#result-type)
- [Transactions](#transactions)
  - [RunTx helper](#runtx-helper)
  - [Transaction function types](#transaction-function-types)
  - [Batch inserts](#batch-inserts)
  - [Example: atomic multi-table insert](#example-atomic-multi-table-insert)
- [Pagination](#pagination)
  - [Setup](#setup)
  - [Fetching pages](#fetching-pages)
  - [PageRequest / PageResponse](#pagerequest--pageresponse)
  - [Full pagination loop](#full-pagination-loop)
- [Breaking changes in v2](#breaking-changes-in-v2)
- [Breaking changes in v1.0.0](#breaking-changes-in-v100)

## Opening a database

```go
import "github.com/borud/dbx/v2"

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Returns *sqlx.DB
db, err := dbx.OpenSQLX(
    dbx.WithDSN(":memory:"),
    dbx.WithDriver("sqlite"),
    dbx.WithMigrations(migrationsFS, "migrations"),
)

// Returns *sql.DB
db, err := dbx.Open(
    dbx.WithDSN(":memory:"),
    dbx.WithDriver("sqlite"),
    dbx.WithMigrations(migrationsFS, "migrations"),
)
```

**Filesystem migrations** (instead of embedded):

```go
dbx.WithMigrations(os.DirFS("migrations"), ".")
```

**Pragmas** run on every new connection in the pool, since many SQLite settings apply per connection. A bad pragma makes `Open` fail.

```go
dbx.WithPragmas(
    "PRAGMA foreign_keys = ON",
    "PRAGMA synchronous = NORMAL",
)
```

**Custom migration driver** (e.g. MySQL):

```go
import mysql "github.com/golang-migrate/migrate/v4/database/mysql"

dbx.WithMigrationDriver("mysql", "mysql",
    func(db *sql.DB) (database.Driver, error) {
        return mysql.WithInstance(db, &mysql.Config{})
    }),
```

### Migration files

Numbered SQL files in a directory:

```text
migrations/0001_init.up.sql
migrations/0002_add_foo_field.up.sql
migrations/0003_add_bar_table.up.sql
```

Only *up* migrations are supported. Migrations run on their own connection, closed when they finish (an in-memory SQLite database is migrated on the returned handle, since it exists nowhere else).

- A missing or unreadable migrations directory is reported as such. An empty directory is fine on a new database.
- `ErrDatabaseTooNew` means the database's schema version is higher than the newest migration in the binary.
- `ErrDirtyDatabase` means a migration failed partway, now or on an earlier run; fix the schema and force the version with the `migrate` tool. Check both with `errors.Is`.
- The `sqlite` and `sqlite3` drivers migrate through golang-migrate's pure-Go `database/sqlite` package, so no cgo is needed.
- The resulting version is logged at info level through `WithLogger` (default `slog.Default()`).

## Prepared statements

Function types that wrap prepared statements with generics:

| Type                       | Use case                                       | Constructor                  |
|----------------------------|-------------------------------------------------|------------------------------|
| `ExecFunc`                 | Simple ops (DELETE, etc.) with positional args   | `NewExecFunc`                |
| `NamedExecFunc[T]`         | INSERT/UPDATE with a struct, returns `Result`    | `NewNamedExecFunc[T]`        |
| `QueryRowxFunc[T]`         | Single-row query with positional args            | `NewQueryRowxFunc[T]`        |
| `EntityQueryRowxFunc[T]`   | Struct in, struct out (e.g. `RETURNING *`)       | `NewEntityQueryRowxFunc[T]`  |
| `SelectFunc[T]`            | Bounded result sets                              | `NewSelectFunc[T]`           |
| `QueryxIteratorFunc[T]`    | Streaming large result sets via range iterator   | `NewQueryxIteratorFunc[T]`   |
| `PaginatedSelectFunc[T]`   | Cursor-based paginated SELECT (see [Pagination](#pagination)) | `NewPaginatedSelectFunc[T]`  |

Every constructor prepares its statement immediately and panics on invalid SQL, so wiring mistakes surface at startup. This relies on the driver compiling statements in `Prepare`: `modernc.org/sqlite` does from v1.53.0 (dbx requires it), older versions only stored the text.

`ExecFunc` and `TxExecFunc` return an error wrapping `sql.ErrNoRows` ("no rows affected") when the driver reports zero rows affected. That suits `DELETE`/`UPDATE ... WHERE id = ?`, but also fires for DDL and for updates that leave a row unchanged on MySQL; use `NamedExecFunc`, which returns the `Result`, for those.

`QueryxIteratorFunc` runs its query each time the returned iterator is ranged over, so an iterator that is never ranged holds no connection.

**Note:** `NewPaginatedSelectFunc` constructs SQL from its `table`, `orderCol`, `keyCol` and `baseWhere` arguments. The table and column names are interpolated directly into queries — they are validated as safe SQL identifiers (ASCII letters, digits, underscores only) and the constructor panics on invalid input. The `baseWhere` clause is passed through as-is, so use `?` placeholders for any user-supplied values.

### Example: CRUD operations

```go
type Record struct {
    Name string `db:"name"`
    TS   int64  `db:"ts"`
}

// Define your storage interface as function fields
type Storage struct {
    Add    dbx.NamedExecFunc[Record]
    AddRet dbx.EntityQueryRowxFunc[Record]
    Get    dbx.QueryRowxFunc[Record]
    Update dbx.NamedExecFunc[Record]
    Delete dbx.ExecFunc
    List   dbx.SelectFunc[Record]
}

// Wire up
ops := Storage{
    Add:    dbx.NewNamedExecFunc[Record](db, "INSERT INTO foo (name,ts) VALUES (:name,:ts)"),
    AddRet: dbx.NewEntityQueryRowxFunc[Record](db, "INSERT INTO foo (name,ts) VALUES (:name,:ts) RETURNING *"),
    Get:    dbx.NewQueryRowxFunc[Record](db, "SELECT * FROM foo WHERE name = ?"),
    Update: dbx.NewNamedExecFunc[Record](db, "UPDATE foo SET ts = :ts WHERE name = :name"),
    Delete: dbx.NewExecFunc(db, "DELETE FROM foo WHERE name = ?"),
    List:   dbx.NewSelectFunc[Record](db, "SELECT * FROM foo ORDER BY ts ASC"),
}

// Insert
res, err := ops.Add(ctx, Record{Name: "alice", TS: 1})
lastID, _ := res.LastInsertID()

// Insert with RETURNING
rec, err := ops.AddRet(ctx, Record{Name: "bob", TS: 2})

// Get
rec, err := ops.Get(ctx, "alice")

// Update
res, err := ops.Update(ctx, Record{Name: "alice", TS: 99})

// Delete
err := ops.Delete(ctx, "alice")

// List all
records, err := ops.List(ctx)
```

### Streaming with QueryxIteratorFunc

For large result sets that shouldn't be loaded into memory at once:

```go
listIter := dbx.NewQueryxIteratorFunc[Record](db, "SELECT * FROM foo ORDER BY ts ASC")

ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
defer cancel()

for rec, err := range listIter(ctx) {
    // context cancellation terminates the loop with an error
    if err != nil {
        break
    }
    fmt.Println(rec)
}
```

### Result type

`NamedExecFunc` returns `dbx.Result` which wraps `sql.Result`:

```go
res, err := ops.Add(ctx, record)
if id, ok := res.LastInsertID(); ok { ... }
if n, ok := res.RowsAffected(); ok { ... }
```

## Transactions

The prepared-statement functions above operate on `*sqlx.DB` directly. When you need multiple operations to succeed or fail atomically, use the transaction variants.

### RunTx helper

`RunTx` handles begin, commit, and rollback. If the callback returns an error (or panics), the transaction is rolled back; otherwise it is committed.

```go
err := dbx.RunTx(ctx, db, func(tx *sqlx.Tx) error {
    // all operations here run in a single transaction
    return nil
})
```

### Transaction function types

Each prepared-statement type has a `Tx` counterpart that runs inside a `*sqlx.Tx`. The statement is prepared on the `*sqlx.DB` at construction time and bound to the transaction on each call, so it is parsed once per pooled connection. Construct these outside any transaction: on a database limited to one connection, preparing while a transaction holds the connection would wait forever. `Open` limits in-memory SQLite databases to one connection, since each connection to one is a separate database.

Each call binds the statement to the transaction, and `database/sql` keeps that binding until the transaction ends, so keep transactions that make very many calls short.

| Type                          | Mirrors                    | Constructor                     |
|-------------------------------|----------------------------|---------------------------------|
| `TxExecFunc`                  | `ExecFunc`                 | `NewTxExecFunc`                 |
| `TxNamedExecFunc[T]`          | `NamedExecFunc[T]`         | `NewTxNamedExecFunc[T]`         |
| `TxQueryRowxFunc[T]`          | `QueryRowxFunc[T]`         | `NewTxQueryRowxFunc[T]`         |
| `TxEntityQueryRowxFunc[T]`    | `EntityQueryRowxFunc[T]`   | `NewTxEntityQueryRowxFunc[T]`   |
| `TxBatchExecFunc[T]`          | (none)                     | `NewTxBatchExecFunc[T]`         |

### Batch inserts

`NewTxBatchExecFunc` inserts a slice of structs with multi-row statements, up to `maxRows` rows each, and returns one `Result` per statement. The statement is a head, the row template repeated once per entity, and a tail (such as `ON CONFLICT DO NOTHING`, or empty). Statements for 1 to `maxRows` rows are prepared up front.

Only the row template is parsed: its `:name` parameters become the driver's bindvars and `::` is a literal colon. The head and tail are used verbatim and must not contain placeholders. A nil pointer on the way to a field (for instance a nil element when `T` is a pointer type) is returned as an error.

```go
insert := dbx.NewTxBatchExecFunc[Event](db, "INSERT INTO events (time, path) VALUES ", "(:time, :path)", " ON CONFLICT DO NOTHING", 8)

err := dbx.RunTx(ctx, db, func(tx *sqlx.Tx) error {
    _, err := insert(ctx, tx, events)
    return err
})
```

Batching matters: each statement has a fixed cost in the driver (`modernc.org/sqlite` starts a goroutine per statement when the context can be cancelled), so inserting rows one statement at a time is several times slower. Keep `maxRows` small, since some drivers bind parameters in time quadratic in their number.

### Example: atomic multi-table insert

A common pattern is inserting related rows across tables atomically. The Tx function types can be wired up alongside their non-transactional counterparts:

```go
type DeviceStore struct {
    Add    dbx.NamedExecFunc[Device]        // normal, non-tx insert
    AddTx  dbx.TxNamedExecFunc[Device]      // same SQL, but for use in transactions
    Get    dbx.QueryRowxFunc[Device]
    GetTx  dbx.TxQueryRowxFunc[Device]
}

type PSKStore struct {
    AddTx dbx.TxNamedExecFunc[PSK]
}

// Wire up (typically at init time)
devices := DeviceStore{
    Add:   dbx.NewNamedExecFunc[Device](db, "INSERT INTO devices (id,name) VALUES(:id,:name)"),
    AddTx: dbx.NewTxNamedExecFunc[Device](db, "INSERT INTO devices (id,name) VALUES(:id,:name)"),
    Get:   dbx.NewQueryRowxFunc[Device](db, "SELECT * FROM devices WHERE id = ?"),
    GetTx: dbx.NewTxQueryRowxFunc[Device](db, "SELECT * FROM devices WHERE id = ?"),
}

psks := PSKStore{
    AddTx: dbx.NewTxNamedExecFunc[PSK](db, "INSERT INTO psk (identity,device_id,key) VALUES(:identity,:device_id,:key)"),
}

// Atomic insert: device + initial PSK
err := dbx.RunTx(ctx, db, func(tx *sqlx.Tx) error {
    if _, err := devices.AddTx(ctx, tx, device); err != nil {
        return err
    }
    _, err := psks.AddTx(ctx, tx, psk)
    return err
})
```

## Pagination

Cursor-based pagination via `PaginatedSelectFunc[T]`.

### Setup

```go
list := dbx.NewPaginatedSelectFunc[Record](
    db,
    "foo",  // table name
    "ts",   // ORDER BY column
    "id",   // unique tie-breaker column; pass "ts" again if ts is unique
    "",     // base WHERE condition ("" for none)
    func(r Record) (string, string) { // the row's ts and id values
        return strconv.FormatInt(r.TS, 10), strconv.FormatInt(r.ID, 10)
    },
)
```

Rows are ordered by `orderCol`, then `keyCol`, and cursors carry both, so rows sharing an `orderCol` value are never skipped or repeated across pages. Cursors are opaque strings: pass `FirstCursor`/`LastCursor` back unchanged; anything else returns `ErrInvalidCursor`.

The cursor values are bound as string parameters, so the database must compare `orderCol` and `keyCol` correctly against text. SQLite does for `INTEGER` columns; a timestamp must be formatted exactly as it is stored.

With a WHERE clause (the `WHERE` keyword is optional):

```go
list := dbx.NewPaginatedSelectFunc[Record](
    db, "foo", "ts", "id", "name = ? OR name = ?",
    func(r Record) (string, string) { return strconv.FormatInt(r.TS, 10), strconv.FormatInt(r.ID, 10) },
)

// Pass WHERE args after PageRequest
rows, page, err := list(ctx, dbx.PageRequest{PageSize: 10}, "alice", "bob")
```

The condition is wrapped in parentheses, so an `OR` in it cannot capture the cursor condition. Use `?` placeholders only; they are rebound for the driver. All four statements (all rows, first page, after, before) are prepared at construction, so a bad condition panics there.

### Fetching pages

```go
// All rows (no pagination)
rows, page, err := list(ctx, dbx.PageRequest{})

// First page
rows, page, err := list(ctx, dbx.PageRequest{PageSize: 10})

// Next page (forward)
rows, page, err := list(ctx, dbx.PageRequest{PageSize: 10, After: page.LastCursor})

// Previous page (backward)
rows, page, err := list(ctx, dbx.PageRequest{PageSize: 10, Before: page.FirstCursor})
```

### PageRequest / PageResponse

```go
type PageRequest struct {
    PageSize int    // 0 = unlimited, clamped to MaxPageSize (1000), negative is an error
    After    string // forward cursor (mutually exclusive with Before)
    Before   string // backward cursor (mutually exclusive with After)
}

type PageResponse struct {
    HasMore     bool   // more rows exist in the direction of travel
    FirstCursor string // cursor of first item in page
    LastCursor  string // cursor of last item in page
}
```

- `After` and `Before` are mutually exclusive (setting both returns `ErrMutuallyExclusive`)
- `PageSize` > `MaxPageSize` (1000) is silently clamped; a negative one returns `ErrInvalidPageSize`
- `HasMore` looks one way only: after `LastCursor` for a first or `After` page, before `FirstCursor` for a `Before` page
- Backward pagination (`Before`) returns results in ascending order
- Table and column names are validated as safe SQL identifiers (panics at init on invalid input)

### Full pagination loop

```go
var allRows []Record
req := dbx.PageRequest{PageSize: 20}

for {
    rows, page, err := list(ctx, req)
    if err != nil { return err }

    allRows = append(allRows, rows...)

    if !page.HasMore {
        break
    }
    req.After = page.LastCursor
}
```

## Breaking changes in v2

- The import path is `github.com/borud/dbx/v2`.
- The `NewTx*` constructors take the `*sqlx.DB` first and prepare their statement at construction: `NewTxExecFunc(db, stmt)`.
- `NewTxBatchExecFunc[T](db, head, row, tail, maxRows)` is new; pass `""` as `tail` when nothing follows the rows.
- `NewPaginatedSelectFunc[T](db, table, orderCol, keyCol, baseWhere, cursorFn)` takes a unique tie-breaker `keyCol`, and `cursorFn` returns the row's `orderCol` and `keyCol` values. Cursors are now opaque, so cursors saved from v1 are rejected with `ErrInvalidCursor`. A negative `PageSize` returns `ErrInvalidPageSize`.
- Pragmas run on every pooled connection rather than one, and in-memory SQLite is limited to one connection.
- `ErrDatabaseTooNew` is returned only when the schema version is ahead of the migrations, and wraps the versions; a missing migrations directory is its own error. `ErrDirtyDatabase` is new.
- `ExecFunc`/`TxExecFunc` wrap their `sql.ErrNoRows` with the statement, and scan errors use `statement [...]` instead of `failed StructScan [...]`. Code using `errors.Is` is unaffected.
- `QueryxIteratorFunc` runs its query when ranged over, not when called.
- dbx needs Go 1.25 and `modernc.org/sqlite` v1.53.0 or later.

## Breaking changes in v1.0.0

### `WithPragmas` now takes variadic arguments

The signature changed from `WithPragmas([]string{...})` to `WithPragmas(...)`.

Before:

```go
dbx.WithPragmas([]string{
    "PRAGMA foreign_keys = ON",
    "PRAGMA synchronous = NORMAL",
})
```

After:

```go
dbx.WithPragmas(
    "PRAGMA foreign_keys = ON",
    "PRAGMA synchronous = NORMAL",
)
```

### `ValidSQLIdentifier` rejects identifiers starting with a digit

`ValidSQLIdentifier("123table")` now returns `false`. This aligns with standard SQL identifier rules. Table and column names passed to `NewPaginatedSelectFunc` must not start with a digit.

### `NewNamedExecFunc` no longer returns `sql.ErrNoRows` on zero affected rows

Previously, `NewNamedExecFunc` returned both a valid `Result` and `sql.ErrNoRows` when `RowsAffected == 0`. This was misleading because the operation succeeded — zero rows affected is a valid outcome for many statements. The `Result` is still returned and callers should check `result.RowsAffected()` to detect zero affected rows when needed.

Note: `NewExecFunc` retains the `sql.ErrNoRows` behavior since it does not return a `Result`.
