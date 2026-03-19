# DBX - Opinionated database utilities for Go

[![Go Reference](https://pkg.go.dev/badge/github.com/borud/dbx.svg)](https://pkg.go.dev/github.com/borud/dbx)

Convenience layer on top of [sqlx](https://github.com/jmoiron/sqlx). No ORM — just prepared statements, migrations, and pagination.

## Table of contents

- [Opening a database](#opening-a-database)
  - [Migration files](#migration-files)
- [Prepared statements](#prepared-statements)
  - [Example: CRUD operations](#example-crud-operations)
  - [Streaming with QueryxIteratorFunc](#streaming-with-queryxiteratorfunc)
  - [Result type](#result-type)
- [Pagination](#pagination)
  - [Setup](#setup)
  - [Fetching pages](#fetching-pages)
  - [PageRequest / PageResponse](#pagerequest--pageresponse)
  - [Full pagination loop](#full-pagination-loop)
- [Breaking changes in v1.0.0](#breaking-changes-in-v100)

## Opening a database

```go
import "github.com/borud/dbx"

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

**Pragmas:**

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

```
migrations/0001_init.up.sql
migrations/0002_add_foo_field.up.sql
migrations/0003_add_bar_table.up.sql
```text
migrations/0001_init.up.sql
migrations/0002_add_foo_field.up.sql
migrations/0003_add_bar_table.up.sql
```

Only *up* migrations are supported.

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

**Note:** `NewPaginatedSelectFunc` constructs SQL from its `table`, `orderCol`, and `baseWhere` arguments. The table and column names are interpolated directly into queries — they are validated as safe SQL identifiers (ASCII letters, digits, underscores only) and the constructor panics on invalid input. The `baseWhere` clause is passed through as-is, so use `?` placeholders for any user-supplied values.

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

## Pagination

Cursor-based pagination via `PaginatedSelectFunc[T]`.

### Setup

```go
list := dbx.NewPaginatedSelectFunc[Record](
    db,
    "foo",                                              // table name
    "ts",                                               // ORDER BY column (used as cursor)
    "",                                                 // base WHERE clause ("" for none)
    func(r Record) string { return strconv.FormatInt(r.TS, 10) }, // cursor extractor
)
```

With a WHERE clause:

```go
list := dbx.NewPaginatedSelectFunc[Record](
    db, "foo", "ts", "WHERE name = ?",
    func(r Record) string { return strconv.FormatInt(r.TS, 10) },
)

// Pass WHERE args after PageRequest
rows, page, err := list(ctx, dbx.PageRequest{PageSize: 10}, "alice")
```

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
    PageSize int    // 0 = unlimited, clamped to MaxPageSize (1000)
    After    string // forward cursor (mutually exclusive with Before)
    Before   string // backward cursor (mutually exclusive with After)
}

type PageResponse struct {
    HasMore     bool   // true if more pages exist
    FirstCursor string // cursor of first item in page
    LastCursor  string // cursor of last item in page
}
```

- `After` and `Before` are mutually exclusive (setting both returns an error)
- `PageSize` > `MaxPageSize` (1000) is silently clamped
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
