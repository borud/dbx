// Example: opening a database with migrations and pragmas.
package main

import (
	"embed"
	"fmt"
	"log"

	"github.com/borud/dbx"
	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

func main() {
	// Open a standard *sql.DB with pragmas and embedded migrations.
	db, err := dbx.Open(
		dbx.WithDSN(":memory:"),
		dbx.WithDriver("sqlite"),
		dbx.WithPragmas(
			"PRAGMA foreign_keys = ON",
			"PRAGMA synchronous = NORMAL",
			"PRAGMA temp_store = MEMORY",
		),
		dbx.WithMigrations(migrationsFS, "migrations"),
	)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	// Verify the database is working.
	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM users").Scan(&count)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("users table ready, row count:", count)

	// OpenSQLX returns *sqlx.DB for convenience methods.
	sdb, err := dbx.OpenSQLX(
		dbx.WithDSN(":memory:"),
		dbx.WithDriver("sqlite"),
		dbx.WithMigrations(migrationsFS, "migrations"),
	)
	if err != nil {
		log.Fatal(err)
	}
	defer sdb.Close()

	fmt.Println("sqlx database opened successfully")
}
