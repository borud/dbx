# DBX - A set of opinionated database utilities

This library is a somewhat opinionated set of database tools that are useful to me, and perhaps to you.  Its primary job is to make my life a bit easier when using databases the way I prefer to use them.

I mostly use [Jason Moiron's](https://github.com/jmoiron) [sqlx](https://github.com/jmoiron/sqlx) excellent library since it provides me with a good balance between convenience and flexibility.  I'm not fond of ORMs, but I am also not very fond of complicating database operations more than they need to be. The `sqlx` library provides a very good balance I think.

## Open

The primary tool provided here is for opening databases and applying migrations.  Note that we do not bother with *down* migrations.  We only support *up* migrations.

You can use this to open databases like so:

```go
import (
    "github.com/borud/dbx"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

db, err := dbx.Open(
    dbx.WithDSN(":memory:"),
    dbx.WithDriver("sqlite"),
    dbx.WithMigrations(migrationsFS, "migrations"),
 )
```

In the above example we use an embedded filesystem `migrationsFS` for migrations.  If you want to do migrations from the filesystem you can replace

```go
dbx.WithMigrations(migrationsFS, "migrations"),
```

with

```go
dbx.WithMigrations(os.DirFS("migrations"), "."),
```

Which will do the same thing.

### Schema

Rather than a fixed single schema file we use migrations.  Typically you would want to put the migration files in a subdir and include them using an embedded filesystem.

Files have names that start with a number and may look something like this:

- `testmigrations/0001_init.up.sql`
- `testmigrations/0002_add_foo_field.up.sql`.
- `testmigrations/0003_add_bar_table.up.sql`.
- ...

etc

### Pragmas

Adding pragmas can be done using the `WithPragmas` option:

```go
dbx.WithPragmas([]string{
    "PRAGMA foreign_keys = ON",
    "PRAGMA synchronous = NORMAL",
    "PRAGMA secure_delete = OFF",
    "PRAGMA synchronous = NORMAL",
    "PRAGMA temp_store = MEMORY",
  }),
```

### Migration database drivers

The migration library I use ([github.com/golang-migrate/migrate](github.com/golang-migrate/migrate)) has support for a bunch of databases. We include a small set of drivers per default in the library purely as a convenience. But this does mean that we add dependencies you may not need.

Since I'm the only user of this code for now I can live with that.

If you want to use drivers that are not yet included here you can add those useing the `WithMigrationDriver` config option.  For instance if you want to add MySQL support:

```go
import (
  mysql "github.com/golang-migrate/migrate/v4/database/mysql"
)
```

and then you add the driver explicitly with

```go
dbx.WithMigrationDriver("mysql", "mysql",
   func(db *sql.DB) (database.Driver, error) {
      return mysql.WithInstance(db, &mysql.Config{})
   }),
```

## Prepared statements

This library includes a mechanism for dealing with prepared statements that makes life a bit easier.  It uses generics and a set of function types that you can use for prepared statements.

Here is an excerpt of the types from `prepared.go`.

```go

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

// QueryxIteratorFunc is useful for queries that return (poitentially) large
// result sets and you want to be able to stream the result.
type QueryxIteratorFunc[T any] func(ctx context.Context, args ...any) func(func(T, error) bool)
```

You can instantiate these with:

```go

// NewExecFunc creates an ExecFunc
func NewExecFunc(db *sqlx.DB, stmt string) ExecFunc

// NewNamedExecFunc creates a new NamedExecFunc
func NewNamedExecFunc[T any](db *sqlx.DB, stmt string) NamedExecFunc[T]

// NewQueryRowxFunc creates a new QueryRowxFunc
func NewQueryRowxFunc[T any](db *sqlx.DB, stmt string) QueryRowxFunc[T]

// NewEntityQueryRowxFunc creates a new EntityQueryRowxFunc
func NewEntityQueryRowxFunc[T any](db *sqlx.DB, stmt string) 

// NewSelectFunc creates a new SelectFunc
func NewSelectFunc[T any](db *sqlx.DB, stmt string) SelectFunc[T]

// NewQueryxIteratorFunc creates a new QueryxIteratorFunc
func NewQueryxIteratorFunc[T any](db *sqlx.DB, stmt string) QueryxIteratorFunc
```

You can look at the [unit test](dbxtest/prepared_test.go) for prepared statements to see an example of how you can make structs that hold the functions that define your interface.

### QueryxIteratorFunc

This type is particularly useful because it allows you to write very simple loops around queries that return possibly huge result sets.

Assume you have a struct with pointers to your database operations

```go
type Record struct {
   // some record structure representing the fields in a table
}

type Storage struct {
   List dbx.QueryxIteratorFunc[Record]
}
```

Here's how you would instantiate the storage:

```go
operations := Storage{
   List: dbx.NewQueryxIteratorFunc[record](db, "SELECT * FROM foo"),
}
```

```go
  // perhaps add a timeout
  ctx, cancel := context.WithTimeout(someCTX, 10*time.Millisecond)
  defer cancel()

  for record, err := range operations.ListRecords(ctx) {
    // check err and do something with record
  }
```

If `someCTX` comes from a HTTP call or a gRPC call, it will terminate the loop and return an error value if the context is cancelled.  This makes it easier to handle those queries that return a lot of rows without first slurping everything into memory before returning, but just stream entries as they are returned by the iterator.
