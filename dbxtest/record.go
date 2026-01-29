package dbxtest

type record struct {
	Name string `db:"name"`
	TS   int64  `db:"ts"`
}
