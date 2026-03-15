// Example: cursor-based pagination.
package main

import (
	"context"
	"embed"
	"fmt"
	"log"
	"strconv"

	"github.com/borud/dbx"
	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

type event struct {
	ID   int64  `db:"id"`
	Name string `db:"name"`
	TS   int64  `db:"ts"`
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

	// Seed data.
	insert := dbx.NewNamedExecFunc[event](db, "INSERT INTO events (name, ts) VALUES (:name, :ts)")
	for i := range 25 {
		if _, err := insert(ctx, event{Name: fmt.Sprintf("event-%02d", i), TS: int64(i * 100)}); err != nil {
			log.Fatal(err)
		}
	}

	// Create a paginated select function.
	// Parameters: db, table, order column, optional WHERE clause, cursor extractor.
	list := dbx.NewPaginatedSelectFunc(db, "events", "ts", "",
		func(e event) string { return strconv.FormatInt(e.TS, 10) },
	)

	// Page through all results, 5 at a time.
	page := dbx.PageRequest{PageSize: 5}
	pageNum := 0

	for {
		rows, pr, err := list(ctx, page)
		if err != nil {
			log.Fatal(err)
		}

		pageNum++
		fmt.Printf("--- page %d ---\n", pageNum)
		for _, r := range rows {
			fmt.Printf("  %s (ts=%d)\n", r.Name, r.TS)
		}

		if !pr.HasMore {
			break
		}
		page = dbx.PageRequest{PageSize: 5, After: pr.LastCursor}
	}

	// Paginated select with a WHERE clause.
	fmt.Println("\n--- filtered (ts < 500) ---")
	filtered := dbx.NewPaginatedSelectFunc(db, "events", "ts", "WHERE ts < ?",
		func(e event) string { return strconv.FormatInt(e.TS, 10) },
	)

	rows, pr, err := filtered(ctx, dbx.PageRequest{PageSize: 10}, 500)
	if err != nil {
		log.Fatal(err)
	}
	for _, r := range rows {
		fmt.Printf("  %s (ts=%d)\n", r.Name, r.TS)
	}
	fmt.Printf("has more: %v\n", pr.HasMore)
}
