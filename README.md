# DBX - A set of opinionated database utilities

This library is a somewhat opinionated set of database tools that are useful to me, and perhaps to you.  Its primary job is to make my life a bit easier when using databases the way I prefer to use them.

I mostly use [Jason Moiron's](https://github.com/jmoiron) [sqlx](https://github.com/jmoiron/sqlx) library since it provides me with a good balance between convenience and flexibility.  I'm not fond of ORMs, but I am also not very fond of complicating database operations more than they need to be.

## Open

The primary tool provided here is for opening databases and applying migrations.  Note that we do not bother with *down* migrations.  We only support *up* migrations.

You can use this to open databases like so:

```go
db, err := dbx.Open(
  dbx.WithDSN(":memory:"),
  dbx.WithDriver("sqlite"),
  dbx.WithMigrations(migrationsFS, "testmigrations"),
  dbx.WithMigrationDriver("sqlite", func(db *sql.DB) (database.Driver, string, error) {
   d, err := sqlite3.WithInstance(db, &sqlite3.Config{})
   return d, "sqlite3", err
  }),
 )
```

In the above example we use an embedded filesystem for migrations.  If you want to do migrations from the filesystem you can replace

```go
dbx.WithMigrations(migrationsFS, "testmigrations"),
```

with 

```go
dbx.WithMigrations(os.DirFS("testmigrations"), "."),
```

Which will do the same thing.

## Pragmas

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

## Migration database drivers

The migration library I use ([github.com/golang-migrate/migrate](github.com/golang-migrate/migrate)) has support for a bunch of databases.  In order to avoid dependency on a particular version of the database libraries involved I have opted to add a `WithMigrationDriver` config option that provides the driver mapping for migrations.  If you are using other databases you have to add the appropriate `WithMigrationDriver` config option for your database(s).

The import statements you will need for various drivers is some subset of

```go
import (
  crdb "github.com/golang-migrate/migrate/v4/database/cockroachdb"
  mysql "github.com/golang-migrate/migrate/v4/database/mysql"
  pg "github.com/golang-migrate/migrate/v4/database/postgres"
  sqlserver "github.com/golang-migrate/migrate/v4/database/sqlserver"
  sqlite3 "github.com/golang-migrate/migrate/v4/database/sqlite3"
)
```

Here are some options for various databases:

### SQlite 3 ("modernc.org/sqlite")

```go
dbx.WithMigrationDriver("sqlite", func(db *sql.DB) (database.Driver, string, error) {
   d, err := sqlite3.WithInstance(db, &sqlite3.Config{})
   return d, "sqlite3", err
  }),
```

### SQlite3 (github.com/mattn/go-sqlite3)

```go
dbx.WithMigrationDriver("sqlite3", func(db *sql.DB) (database.Driver, string, error) {
   d, err := sqlite3.WithInstance(db, &sqlite3.Config{})
   return d, "sqlite3", err
  }),
```

### Postgres

```go
dbx.WithMigrationDriver("postgres", func(db *sql.DB) (database.Driver, string, error) {
  d, err := pg.WithInstance(db, &pg.Config{})
  return d, "postgres", err
}),
```

or

```go
dbx.WithMigrationDriver("pgx", func(db *sql.DB) (database.Driver, string, error) {
  d, err := pg.WithInstance(db, &pg.Config{})
  return d, "postgres", err
}),
```

### MySQL / MariaDB

```go
dbx.WithMigrationDriver("mysql", func(db *sql.DB) (database.Driver, string, error) {
  d, err := mysql.WithInstance(db, &mysql.Config{})
  return d, "mysql", err
}),
```

### SQL server

```go
dbx.WithMigrationDriver("sqlserver", func(db *sql.DB) (database.Driver, string, error) {
  d, err := mysql.WithInstance(db, &mysql.Config{})
  return d, "sqlserver", err
}),
```

You can probably figure out what it would be for any database not listed here.
