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

db, err := dbx.Open(
    dbx.WithDSN(":memory:"),
    dbx.WithDriver("sqlite"),
    dbx.WithMigrations(migrationsFS, "testmigrations"),
    dbx.WithMigrationDriver("sqlite", "sqlite3",
        func(db *sql.DB) (database.Driver, error) {
            return sqlite3.WithInstance(db, &sqlite3.Config{})
        }),
 )
```

In the above example we use an embedded filesystem `migrationsFS` for migrations.  If you want to do migrations from the filesystem you can replace

```go
dbx.WithMigrations(migrationsFS, "testmigrations"),
```

with

```go
dbx.WithMigrations(os.DirFS("testmigrations"), "."),
```

Which will do the same thing.

### Schema

Rather than a fixed single schema file we use migrations.  Typically you would want to put the migration files in a subdir and include them using an embedded filesystem.

If you look in the tests you have this line:

```go
//go:embed testmigrations/*.sql
var migrationsFS embed.FS
```

Then in the `testmigrations` subdirectory you have your migration SQL files.  Like `testmigrations/0001_init.up.sql`.

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

The migration library I use ([github.com/golang-migrate/migrate](github.com/golang-migrate/migrate)) has support for a bunch of databases.  In order to avoid dependency on a particular version of the database libraries involved I have opted to add a `WithMigrationDriver` config option that provides the driver mapping for migrations.  If you are using other databases you have to add the appropriate `WithMigrationDriver` config option for your database(s).

The import statements you will need for various drivers are some subset of the drivers found under <https://github.com/golang-migrate/migrate/tree/master/database>:

```go
import (
  mysql "github.com/golang-migrate/migrate/v4/database/mysql"
  pg "github.com/golang-migrate/migrate/v4/database/postgres"
  sqlserver "github.com/golang-migrate/migrate/v4/database/sqlserver"
  sqlite3 "github.com/golang-migrate/migrate/v4/database/sqlite3"
)
```

Here are some options for various databases.  You can probably figure out how this works for any database the library supports that isn't in an example below:

```go
dbx.WithMigrationDriver("sqlite", "sqlite3",
   func(db *sql.DB) (database.Driver, error) {
      return sqlite3.WithInstance(db, &sqlite3.Config{})
   }),

dbx.WithMigrationDriver("sqlite3", "sqlite3",
   func(db *sql.DB) (database.Driver, error) {
      return sqlite3.WithInstance(db, &sqlite3.Config{})
   }),

dbx.WithMigrationDriver("postgres", "postgres",
   func(db *sql.DB) (database.Driver, error) {
      return pg.WithInstance(db, &pg.Config{})
   }),

dbx.WithMigrationDriver("pgx", "postgres",
   func(db *sql.DB) (database.Driver, error) {
      return pg.WithInstance(db, &pg.Config{})
   }),

dbx.WithMigrationDriver("mysql", "mysql",
   func(db *sql.DB) (database.Driver, error) {
      return mysql.WithInstance(db, &mysql.Config{})
   }),

dbx.WithMigrationDriver("sqlserver", "sqlserver",
   func(db *sql.DB) (database.Driver, error) {
      return sqlserver.WithInstance(db, &sqlserver.Config{})
   }),
```

## RowIter

The `RowsIter` type implements an iterator that we can `range` over.  This is particularly useful when streaming a large result set to a client since we do not need to slurp the entire result set into memory before returning it.

To stop iteration you just cancel the context.

RowsIter ranges over rows and StructScan's into T, which must be a struct that has the appropriate struct tags for the fields.

```go
// let's say we have a table that matches this record
type record struct {
    ID   int64  `db:"id"`
    Name string `db:"name"`
}

// then we query the table
rows, err := db.QueryxContext(ctx, "SELECT * FROM person")
if err != nil {
    return err
}

// and then iterate over the rows.  If we get an error we break out of the loop
// and the `rec` will have the zero value for that type.
for rec, err := range dbx.RowsIter[record](ctx, rows) {
    if err != nil {
      break // or handle error
    }
    doSomethingWith(rec)
}
```
