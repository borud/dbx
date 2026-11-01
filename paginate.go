package dbx

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/jmoiron/sqlx"
)

// errors
var (
	// ErrMutuallyExclusive is returned when both After and Before are set in a PageRequest.
	ErrMutuallyExclusive = errors.New("after and before are mutually exclusive")
	// ErrInvalidPageSize is returned for a negative PageRequest.PageSize.
	ErrInvalidPageSize = errors.New("page size must not be negative")
	// ErrInvalidCursor is returned for an After or Before cursor that was not
	// produced by a PaginatedSelectFunc.
	ErrInvalidCursor = errors.New("invalid page cursor")
)

// MaxPageSize is the upper bound on PageRequest.PageSize. Requests exceeding
// this value are silently clamped.
const MaxPageSize = 1000

// PageRequest carries cursor-based pagination parameters for list operations.
// A zero PageSize means "return all results" (backward compatible); a
// negative one is an error.
//
// After and Before are mutually exclusive: setting both is an error.
// After requests results after the given cursor (forward pagination),
// while Before requests results before the given cursor (backward pagination).
//
// Note: After and Before are only applied when PageSize > 0 (i.e. when
// IsPaginated returns true). Setting a cursor without a page size will
// return all rows and silently ignore the cursor value.
type PageRequest struct {
	// PageSize is the maximum number of rows to return. Zero means unlimited.
	PageSize int
	// After is the cursor for forward pagination (exclusive lower bound).
	// Mutually exclusive with Before. Only applied when PageSize > 0.
	After string
	// Before is the cursor for backward pagination (exclusive upper bound).
	// Mutually exclusive with After. Only applied when PageSize > 0.
	Before string
}

// IsPaginated returns true when the caller has requested a bounded page.
func (p PageRequest) IsPaginated() bool {
	return p.PageSize > 0
}

// PageResponse carries pagination metadata returned from list operations.
// Cursors are opaque: pass them back as PageRequest.After or Before.
type PageResponse struct {
	// HasMore is true when more rows exist in the direction of travel:
	// after LastCursor for a first or After page, before FirstCursor for a
	// Before page. It says nothing about the opposite direction.
	HasMore bool
	// FirstCursor is the cursor of the first item in the returned page.
	FirstCursor string
	// LastCursor is the cursor of the last item in the returned page.
	LastCursor string
}

// ValidSQLIdentifier reports whether s is safe to interpolate as a SQL
// identifier. Only ASCII letters, digits, and underscores are allowed,
// and the identifier must not start with a digit.
func ValidSQLIdentifier(s string) bool {
	if s == "" {
		return false
	}
	for i, c := range s {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_') {
			return false
		}
		if i == 0 && c >= '0' && c <= '9' {
			return false
		}
	}
	return true
}

// PaginatedSelectFunc performs a cursor-based paginated SELECT query.
// The args are positional parameters for the base WHERE clause provided
// at construction time.
type PaginatedSelectFunc[T any] func(ctx context.Context, page PageRequest, args ...any) ([]T, PageResponse, error)

// NewPaginatedSelectFunc creates a PaginatedSelectFunc that returns rows
// ordered by orderCol, then keyCol.
//
// keyCol must be unique (usually the primary key): it breaks ties between
// rows with the same orderCol value, so no row is skipped or repeated across
// pages. If orderCol is itself unique, pass it as keyCol too.
//
// table, orderCol and keyCol must be valid SQL identifiers (the constructor
// panics otherwise, consistent with other dbx constructors that panic on
// invalid input detected at init time).
//
// baseWhere is an optional filter condition, with or without a leading WHERE
// keyword (e.g. "tenant_id = ?"). Pass "" to select all rows. It is wrapped
// in parentheses, so an OR in it cannot capture the cursor condition. Use ?
// placeholders only; they are rebound for the driver. baseWhere is
// interpolated directly into the query; it must be a trusted string — never
// pass user-controlled input.
//
// All statements are prepared at construction, so invalid SQL, including an
// identifier that is a reserved word, panics there.
//
// cursorFn returns a row's orderCol and keyCol values as strings. They are
// encoded into the opaque cursors of PageResponse and bound as string
// parameters when a cursor is used, so the database must compare those
// columns correctly against text (SQLite does for INTEGER columns; a
// timestamp must be formatted exactly as stored).
func NewPaginatedSelectFunc[T any](db *sqlx.DB, table, orderCol, keyCol, baseWhere string, cursorFn func(T) (order, key string)) PaginatedSelectFunc[T] {
	for _, id := range []string{table, orderCol, keyCol} {
		if !ValidSQLIdentifier(id) {
			panic(fmt.Sprintf("dbx: invalid SQL identifier %q", id))
		}
	}

	q := newPageQueries(db, table, orderCol, keyCol, baseWhere)

	return func(ctx context.Context, page PageRequest, args ...any) ([]T, PageResponse, error) {
		if page.After != "" && page.Before != "" {
			return nil, PageResponse{}, ErrMutuallyExclusive
		}
		if page.PageSize < 0 {
			return nil, PageResponse{}, fmt.Errorf("%w: %d", ErrInvalidPageSize, page.PageSize)
		}
		page.PageSize = min(page.PageSize, MaxPageSize)

		stmt, queryArgs, err := q.statement(page, args)
		if err != nil {
			return nil, PageResponse{}, err
		}

		rows := []T{}
		if err := stmt.SelectContext(ctx, &rows, queryArgs...); err != nil {
			return nil, PageResponse{}, fmt.Errorf("paginated select [%s]: %w", q.table, err)
		}

		var pr PageResponse
		if page.IsPaginated() && len(rows) > page.PageSize {
			pr.HasMore = true
			rows = rows[:page.PageSize]
		}

		// Backward pagination fetches in DESC order; reverse to restore ASC.
		if page.IsPaginated() && page.Before != "" {
			slices.Reverse(rows)
		}

		if len(rows) > 0 {
			pr.FirstCursor = encodeCursor(cursorFn(rows[0]))
			pr.LastCursor = encodeCursor(cursorFn(rows[len(rows)-1]))
		}

		return rows, pr, nil
	}
}

// pageQueries holds the prepared statements behind a PaginatedSelectFunc.
type pageQueries struct {
	table  string
	single bool
	all    *sqlx.Stmt
	first  *sqlx.Stmt
	after  *sqlx.Stmt
	before *sqlx.Stmt
}

func newPageQueries(db *sqlx.DB, table, orderCol, keyCol, baseWhere string) pageQueries {
	base := strings.TrimSpace(baseWhere)
	if keyword, rest, ok := strings.Cut(base, " "); ok && strings.EqualFold(keyword, "WHERE") {
		base = strings.TrimSpace(rest)
	}

	single := orderCol == keyCol
	asc := fmt.Sprintf("ORDER BY %s ASC, %s ASC", orderCol, keyCol)
	desc := fmt.Sprintf("ORDER BY %s DESC, %s DESC", orderCol, keyCol)
	afterCond := fmt.Sprintf("%[1]s > ? OR (%[1]s = ? AND %[2]s > ?)", orderCol, keyCol)
	beforeCond := fmt.Sprintf("%[1]s < ? OR (%[1]s = ? AND %[2]s < ?)", orderCol, keyCol)
	if single {
		asc = fmt.Sprintf("ORDER BY %s ASC", orderCol)
		desc = fmt.Sprintf("ORDER BY %s DESC", orderCol)
		afterCond = orderCol + " > ?"
		beforeCond = orderCol + " < ?"
	}

	prepare := func(cond, order, limit string) *sqlx.Stmt {
		query := fmt.Sprintf("SELECT * FROM %s%s %s%s", table, whereClause(base, cond), order, limit)
		st, err := db.Preparex(db.Rebind(query))
		if err != nil {
			panic(fmt.Errorf("paginated select [%s]: %w", query, err))
		}
		return st
	}

	return pageQueries{
		table:  table,
		single: single,
		all:    prepare("", asc, ""),
		first:  prepare("", asc, " LIMIT ?"),
		after:  prepare(afterCond, asc, " LIMIT ?"),
		before: prepare(beforeCond, desc, " LIMIT ?"),
	}
}

// statement picks the statement for page and appends its arguments to args.
func (q pageQueries) statement(page PageRequest, args []any) (*sqlx.Stmt, []any, error) {
	if !page.IsPaginated() {
		return q.all, args, nil
	}

	limit := page.PageSize + 1
	if page.After == "" && page.Before == "" {
		return q.first, append(slices.Clip(args), limit), nil
	}

	stmt, cursor := q.after, page.After
	if page.Before != "" {
		stmt, cursor = q.before, page.Before
	}

	order, key, err := decodeCursor(cursor)
	if err != nil {
		return nil, nil, err
	}
	queryArgs := append(slices.Clip(args), order)
	if !q.single {
		queryArgs = append(queryArgs, order, key)
	}

	return stmt, append(queryArgs, limit), nil
}

// whereClause combines the base condition and the cursor condition, each in
// parentheses so neither can capture the other's OR.
func whereClause(base, cond string) string {
	switch {
	case base == "" && cond == "":
		return ""
	case base == "":
		return fmt.Sprintf(" WHERE (%s)", cond)
	case cond == "":
		return fmt.Sprintf(" WHERE (%s)", base)
	default:
		return fmt.Sprintf(" WHERE (%s) AND (%s)", base, cond)
	}
}

func encodeCursor(order, key string) string {
	// Marshaling a string array cannot fail.
	b, _ := json.Marshal([2]string{order, key})
	return base64.RawURLEncoding.EncodeToString(b)
}

func decodeCursor(cursor string) (string, string, error) {
	b, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return "", "", fmt.Errorf("%w: %w", ErrInvalidCursor, err)
	}

	var v [2]string
	if err := json.Unmarshal(b, &v); err != nil {
		return "", "", fmt.Errorf("%w: %w", ErrInvalidCursor, err)
	}
	return v[0], v[1], nil
}
