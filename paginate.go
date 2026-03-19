package dbx

import (
	"context"
	"fmt"
	"slices"

	"github.com/jmoiron/sqlx"
)

// MaxPageSize is the upper bound on PageRequest.PageSize. Requests exceeding
// this value are silently clamped.
const MaxPageSize = 1000

// PageRequest carries cursor-based pagination parameters for list operations.
// A zero PageSize means "return all results" (backward compatible).
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
type PageResponse struct {
	// HasMore is true when additional pages exist beyond this result set.
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

// NewPaginatedSelectFunc creates a PaginatedSelectFunc.
//
// table and orderCol must be valid SQL identifiers (the constructor panics
// otherwise, consistent with other dbx constructors that panic on invalid
// input detected at init time).
//
// baseWhere is an optional WHERE clause including the WHERE keyword
// (e.g. "WHERE tenant_id = ?"). Pass "" to select all rows.
//
// cursorFn extracts a cursor string from a result row for populating
// PageResponse.FirstCursor and LastCursor.
func NewPaginatedSelectFunc[T any](db *sqlx.DB, table, orderCol, baseWhere string, cursorFn func(T) string) PaginatedSelectFunc[T] {
	if !ValidSQLIdentifier(table) {
		panic(fmt.Sprintf("dbx: invalid table name %q", table))
	}
	if !ValidSQLIdentifier(orderCol) {
		panic(fmt.Sprintf("dbx: invalid order column %q", orderCol))
	}

	return func(ctx context.Context, page PageRequest, args ...any) ([]T, PageResponse, error) {
		if page.After != "" && page.Before != "" {
			return nil, PageResponse{}, fmt.Errorf("after and before are mutually exclusive")
		}
		if page.PageSize > MaxPageSize {
			page.PageSize = MaxPageSize
		}

		where := baseWhere
		queryArgs := append([]any{}, args...)

		if page.IsPaginated() {
			if page.After != "" {
				if where == "" {
					where = fmt.Sprintf("WHERE %s > ?", orderCol)
				} else {
					where += fmt.Sprintf(" AND %s > ?", orderCol)
				}
				queryArgs = append(queryArgs, page.After)
			} else if page.Before != "" {
				if where == "" {
					where = fmt.Sprintf("WHERE %s < ?", orderCol)
				} else {
					where += fmt.Sprintf(" AND %s < ?", orderCol)
				}
				queryArgs = append(queryArgs, page.Before)
			}
		}

		order := "ASC"
		if page.IsPaginated() && page.Before != "" {
			order = "DESC"
		}

		query := fmt.Sprintf("SELECT * FROM %s %s ORDER BY %s %s", table, where, orderCol, order)

		if page.IsPaginated() {
			query += fmt.Sprintf(" LIMIT %d", page.PageSize+1)
		}

		query = db.Rebind(query)

		var rows []T
		if err := db.SelectContext(ctx, &rows, query, queryArgs...); err != nil {
			return nil, PageResponse{}, err
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

		if len(rows) > 0 && cursorFn != nil {
			pr.FirstCursor = cursorFn(rows[0])
			pr.LastCursor = cursorFn(rows[len(rows)-1])
		}

		return rows, pr, nil
	}
}
