package dbx

import "database/sql"

// Result holds the outcome of a mutating operation
type Result struct {
	lastInsertID *int64
	rowsAffected *int64
}

// LastInsertID returns the last insert ID if available
func (r Result) LastInsertID() (int64, bool) {
	if r.lastInsertID == nil {
		return 0, false
	}
	return *r.lastInsertID, true
}

// RowsAffected returns the number of rows affected if available
func (r Result) RowsAffected() (int64, bool) {
	if r.rowsAffected == nil {
		return 0, false
	}
	return *r.rowsAffected, true
}

// NewResult wraps a sql.Result into a dbx.Result, extracting
// LastInsertId and RowsAffected if available.
func NewResult(sqlResult sql.Result) Result {
	r := Result{}
	if id, err := sqlResult.LastInsertId(); err == nil {
		r.lastInsertID = &id
	}
	if n, err := sqlResult.RowsAffected(); err == nil {
		r.rowsAffected = &n
	}
	return r
}
