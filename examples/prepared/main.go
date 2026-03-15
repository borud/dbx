// Example: prepared statement functions for CRUD operations.
package main

import (
	"context"
	"embed"
	"fmt"
	"log"

	"github.com/borud/dbx"
	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

type user struct {
	ID    int64  `db:"id"`
	Name  string `db:"name"`
	Email string `db:"email"`
}

func main() {
	db, err := dbx.OpenSQLX(
		dbx.WithDSN(":memory:"),
		dbx.WithDriver("sqlite"),
		dbx.WithMigrations(migrationsFS, "migrations"),
	)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	ctx := context.Background()

	// NamedExecFunc — INSERT using named parameters from struct tags.
	insert := dbx.NewNamedExecFunc[user](db, "INSERT INTO users (name, email) VALUES (:name, :email)")

	// EntityQueryRowxFunc — INSERT with RETURNING to get the created row back.
	insertRet := dbx.NewEntityQueryRowxFunc[user](db, "INSERT INTO users (name, email) VALUES (:name, :email) RETURNING *")

	// QueryRowxFunc — SELECT single row with positional arguments.
	getByID := dbx.NewQueryRowxFunc[user](db, "SELECT * FROM users WHERE id = ?")

	// ExecFunc — DELETE with positional arguments.
	deleteByID := dbx.NewExecFunc(db, "DELETE FROM users WHERE id = ?")

	// SelectFunc — SELECT returning a slice.
	listAll := dbx.NewSelectFunc[user](db, "SELECT * FROM users ORDER BY id")

	// QueryxIteratorFunc — SELECT returning an iterator for large result sets.
	iterAll := dbx.NewQueryxIteratorFunc[user](db, "SELECT * FROM users ORDER BY id")

	// --- Insert with NamedExecFunc ---
	res, err := insert(ctx, user{Name: "Alice", Email: "alice@example.com"})
	if err != nil {
		log.Fatal(err)
	}
	id, _ := res.LastInsertID()
	fmt.Printf("inserted Alice with id=%d\n", id)

	// --- Insert with RETURNING ---
	bob, err := insertRet(ctx, user{Name: "Bob", Email: "bob@example.com"})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("inserted Bob: %+v\n", bob)

	// --- Get single row ---
	alice, err := getByID(ctx, 1)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("fetched: %+v\n", alice)

	// --- List all with SelectFunc ---
	users, err := listAll(ctx)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("all users (%d):\n", len(users))
	for _, u := range users {
		fmt.Printf("  %+v\n", u)
	}

	// --- Iterate with QueryxIteratorFunc ---
	fmt.Println("iterating:")
	for u, err := range iterAll(ctx) {
		if err != nil {
			log.Fatal(err)
		}
		fmt.Printf("  %+v\n", u)
	}

	// --- Delete ---
	err = deleteByID(ctx, alice.ID)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("deleted user id=%d\n", alice.ID)
}
