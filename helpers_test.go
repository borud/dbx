package dbx_test

import "embed"

//go:embed testmigrations/*.sql
var migrationsFS embed.FS

type record struct {
	Name string `db:"name"`
	TS   int64  `db:"ts"`
}
