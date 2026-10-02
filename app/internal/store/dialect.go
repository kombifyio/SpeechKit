package store

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// sqlDialect captures the handful of SQL-syntax differences between the SQLite
// and PostgreSQL backends so the shared sqlStore can run one set of queries
// against both. Queries are authored with `?` placeholders and rebind() adapts
// them to the backend's native placeholder style.
type sqlDialect struct {
	name string // "sqlite" | "postgres"
}

var (
	dialectSQLite   = sqlDialect{name: "sqlite"}
	dialectPostgres = sqlDialect{name: "postgres"}
)

func (d sqlDialect) isPostgres() bool { return d.name == "postgres" }

// rebind converts `?`-style placeholders to the backend's native form. SQLite
// uses `?`; PostgreSQL uses positional `$1, $2, ...`. Question marks inside
// single-quoted string literals are left untouched.
func (d sqlDialect) rebind(query string) string {
	if !d.isPostgres() {
		return query
	}
	var b strings.Builder
	b.Grow(len(query) + 8)
	n := 0
	inString := false
	for i := 0; i < len(query); i++ {
		c := query[i]
		switch {
		case c == '\'':
			inString = !inString
			b.WriteByte(c)
		case c == '?' && !inString:
			n++
			b.WriteByte('$')
			b.WriteString(strconv.Itoa(n))
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

// now returns the dialect's current-timestamp SQL function.
func (d sqlDialect) now() string {
	if d.isPostgres() {
		return "NOW()"
	}
	return "CURRENT_TIMESTAMP"
}

// boolLit returns the dialect's literal for a boolean used inline in SQL (not
// as a bind parameter): SQLite uses 1/0, PostgreSQL uses TRUE/FALSE.
func (d sqlDialect) boolLit(b bool) string {
	if d.isPostgres() {
		if b {
			return "TRUE"
		}
		return "FALSE"
	}
	if b {
		return "1"
	}
	return "0"
}

// timeArg returns a bind value for a timestamp comparison/insert appropriate to
// how the backend stores timestamps: SQLite columns are TEXT in
// "YYYY-MM-DD HH:MM:SS" UTC (matching CURRENT_TIMESTAMP); PostgreSQL columns are
// timestamptz and take a time.Time directly.
func (d sqlDialect) timeArg(t time.Time) any {
	if d.isPostgres() {
		return t.UTC()
	}
	return t.UTC().Format("2006-01-02 15:04:05")
}

// rowExecQuerier is the subset of *sql.Tx / *sql.DB the insert helper needs.
type rowExecQuerier interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// insertReturningID runs an INSERT (authored without a RETURNING clause and
// with `?` placeholders) and returns the new row's id. SQLite uses
// Exec + LastInsertId; PostgreSQL appends `RETURNING id` and scans it, since
// LastInsertId is unsupported by the pgx driver.
func (d sqlDialect) insertReturningID(ctx context.Context, q rowExecQuerier, query string, args ...any) (int64, error) {
	if d.isPostgres() {
		var id int64
		if err := q.QueryRowContext(ctx, d.rebind(query)+" RETURNING id", args...).Scan(&id); err != nil {
			return 0, err
		}
		return id, nil
	}
	res, err := q.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// jsonbInsert returns the cast suffix appended to a bind placeholder when
// inserting into a JSON column. PostgreSQL columns are jsonb and need an
// explicit "::jsonb" cast on the text bind value; SQLite stores JSON as TEXT
// and needs none.
func (d sqlDialect) jsonbInsert() string {
	if d.isPostgres() {
		return "::jsonb"
	}
	return ""
}

// jsonbText wraps a JSON column reference so it reads back as text. PostgreSQL
// jsonb needs "col::text" to scan into a Go string; SQLite returns TEXT as-is.
func (d sqlDialect) jsonbText(col string) string {
	if d.isPostgres() {
		return col + "::text"
	}
	return col
}

// boolValue scans a SQL boolean that a backend may represent as INTEGER (SQLite
// stores 0/1) or a native BOOLEAN (PostgreSQL). database/sql cannot scan an
// int64 into a *bool, so reads of boolean-ish columns use this Scanner.
type boolValue bool

func (b *boolValue) Scan(src any) error {
	switch v := src.(type) {
	case nil:
		*b = false
	case bool:
		*b = boolValue(v)
	case int64:
		*b = v != 0
	case []byte:
		*b = len(v) > 0 && v[0] != '0' && v[0] != 'f' && v[0] != 'F'
	case string:
		*b = v != "" && v != "0" && v != "f" && v != "false"
	default:
		return fmt.Errorf("store: cannot scan %T into bool", src)
	}
	return nil
}
