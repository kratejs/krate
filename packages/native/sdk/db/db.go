// Package db is the SQLite database of the Krate Native SDK, on the pure Go
// driver modernc.org/sqlite (no cgo).
//
// Open(name) opens a file in the app data directory in WAL mode with a busy
// timeout, so the shell process and a Go host child process can use the same
// file at once. Default opens "app.db", the database the UI ids krate/db.*
// and package kv use.
//
// Values are int64, float64, string, []byte (blob), nil and time.Time (stored
// as an RFC 3339 string); a bool becomes 0 or 1. Query results use the same
// kinds, with time.Time for columns the driver parses as dates.
//
// Go host code has full SQL access. SQL that comes from the UI through the
// registered ids is checked first (see CheckUISQL).
package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/kratejs/krate/packages/native/krate"
	sdkos "github.com/kratejs/krate/packages/native/sdk/os"
	"modernc.org/sqlite"
)

func init() {
	krate.RegisterCapability("db", func() bool { return true })
	krate.RegisterCapability("kv", func() bool { return true })
}

// DB is an open database.
type DB struct {
	sql  *sql.DB
	path string
}

// DefaultName is the file the UI ids use.
const DefaultName = "app.db"

// BusyTimeout is how long a statement waits for another connection or
// process to release a lock.
const BusyTimeout = 5 * time.Second

var (
	defMu sync.Mutex
	def   *DB
)

// Default returns the shared app.db handle, opening it on first use.
func Default() (*DB, error) {
	defMu.Lock()
	defer defMu.Unlock()
	if def != nil {
		return def, nil
	}
	d, err := Open(DefaultName)
	if err != nil {
		return nil, err
	}
	def = d
	return d, nil
}

// CloseDefault closes the shared handle; the next Default call reopens it.
func CloseDefault() error {
	defMu.Lock()
	d := def
	def = nil
	defMu.Unlock()
	if d == nil {
		return nil
	}
	return d.Close()
}

// Open opens the database file name inside the app data directory, creating
// it if needed. name is a plain file name ("app.db"); ".db" is appended when
// it has no extension.
func Open(name string) (*DB, error) {
	if name == "" || name == "." || name == ".." || len(name) > 128 || strings.ContainsAny(name, `/\:`+"\x00") {
		return nil, fmt.Errorf("db: %q is not a plain database file name", name)
	}
	if filepath.Ext(name) == "" {
		name += ".db"
	}
	dir, err := sdkos.AppDataDir()
	if err != nil {
		return nil, err
	}
	return OpenPath(filepath.Join(dir, name))
}

// OpenPath opens the database at path (":memory:" for a private in-memory
// database). It applies WAL mode, a busy timeout, foreign keys and
// immediate write transactions.
func OpenPath(path string) (*DB, error) {
	mem := path == ":memory:"
	var dsn string
	q := url.Values{}
	q.Add("_pragma", fmt.Sprintf("busy_timeout(%d)", BusyTimeout.Milliseconds()))
	q.Add("_pragma", "foreign_keys(1)")
	if !mem {
		q.Add("_pragma", "journal_mode(WAL)")
		q.Add("_pragma", "synchronous(NORMAL)")
		q.Set("_txlock", "immediate")
		p := filepath.ToSlash(path)
		if !strings.HasPrefix(p, "/") {
			p = "/" + p // C:/x becomes /C:/x
		}
		dsn = (&url.URL{Scheme: "file", Path: p}).String() + "?" + q.Encode()
	} else {
		dsn = "file::memory:?" + q.Encode()
	}
	h, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	if mem {
		h.SetMaxOpenConns(1) // every connection would be its own empty database
	} else {
		h.SetMaxOpenConns(8)
	}
	if err := h.Ping(); err != nil {
		h.Close()
		return nil, fmt.Errorf("db: opening %s: %w", path, err)
	}
	return &DB{sql: h, path: path}, nil
}

// Path is the database file.
func (d *DB) Path() string { return d.path }

// Close closes the database.
func (d *DB) Close() error { return d.sql.Close() }

// SQL exposes the underlying handle for code that needs database/sql.
func (d *DB) SQL() *sql.DB { return d.sql }

// Result is the outcome of Exec.
type Result struct {
	RowsAffected int64
	LastInsertID int64
}

// Rows are query results with column order kept.
type Rows struct {
	Columns []string
	Data    [][]any
}

// Maps returns the rows as maps keyed by column name; with duplicate column
// names the last one wins.
func (r Rows) Maps() []map[string]any {
	out := make([]map[string]any, 0, len(r.Data))
	for _, row := range r.Data {
		m := make(map[string]any, len(r.Columns))
		for i, c := range r.Columns {
			m[c] = row[i]
		}
		out = append(out, m)
	}
	return out
}

// querier is what *sql.DB and *sql.Tx share.
type querier interface {
	ExecContext(ctx context.Context, q string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, q string, args ...any) (*sql.Rows, error)
}

// Norm converts a parameter to a kind the driver stores.
func norm(v any) (any, error) {
	switch x := v.(type) {
	case nil, int64, float64, string, []byte:
		return x, nil
	case bool:
		if x {
			return int64(1), nil
		}
		return int64(0), nil
	case int:
		return int64(x), nil
	case int8:
		return int64(x), nil
	case int16:
		return int64(x), nil
	case int32:
		return int64(x), nil
	case uint:
		if uint64(x) > math.MaxInt64 {
			return nil, errors.New("integer too large")
		}
		return int64(x), nil
	case uint8:
		return int64(x), nil
	case uint16:
		return int64(x), nil
	case uint32:
		return int64(x), nil
	case uint64:
		if x > math.MaxInt64 {
			return nil, errors.New("integer too large")
		}
		return int64(x), nil
	case float32:
		return float64(x), nil
	case time.Time:
		return x.UTC().Format("2006-01-02T15:04:05.000Z"), nil
	}
	return nil, fmt.Errorf("unsupported SQL parameter type %T (use int64, float64, string, []byte or nil)", v)
}

func normAll(args []any) ([]any, error) {
	out := make([]any, len(args))
	for i, a := range args {
		v, err := norm(a)
		if err != nil {
			return nil, fmt.Errorf("db: parameter %d: %w", i+1, err)
		}
		out[i] = v
	}
	return out, nil
}

func execOn(ctx context.Context, q querier, query string, args []any) (Result, error) {
	a, err := normAll(args)
	if err != nil {
		return Result{}, err
	}
	res, err := q.ExecContext(ctx, query, a...)
	if err != nil {
		return Result{}, wrap(err)
	}
	var r Result
	r.RowsAffected, _ = res.RowsAffected()
	r.LastInsertID, _ = res.LastInsertId()
	return r, nil
}

func queryOn(ctx context.Context, q querier, query string, args []any) (Rows, error) {
	a, err := normAll(args)
	if err != nil {
		return Rows{}, err
	}
	rows, err := q.QueryContext(ctx, query, a...)
	if err != nil {
		return Rows{}, wrap(err)
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return Rows{}, wrap(err)
	}
	out := Rows{Columns: cols}
	for rows.Next() {
		row := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range row {
			ptrs[i] = &row[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return Rows{}, wrap(err)
		}
		for i, v := range row {
			if b, ok := v.([]byte); ok {
				row[i] = append([]byte(nil), b...)
			}
		}
		out.Data = append(out.Data, row)
	}
	if err := rows.Err(); err != nil {
		return Rows{}, wrap(err)
	}
	return out, nil
}

// Exec runs a statement that returns no rows. args bind to ? placeholders.
func (d *DB) Exec(ctx context.Context, query string, args ...any) (Result, error) {
	return execOn(ctx, d.sql, query, args)
}

// QueryRows runs a query and returns its rows with column order kept.
func (d *DB) QueryRows(ctx context.Context, query string, args ...any) (Rows, error) {
	return queryOn(ctx, d.sql, query, args)
}

// Query runs a query and returns each row as a map.
func (d *DB) Query(ctx context.Context, query string, args ...any) ([]map[string]any, error) {
	r, err := d.QueryRows(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	return r.Maps(), nil
}

// Tx is a transaction.
type Tx struct{ tx *sql.Tx }

// Exec runs a statement in the transaction.
func (t *Tx) Exec(ctx context.Context, query string, args ...any) (Result, error) {
	return execOn(ctx, t.tx, query, args)
}

// QueryRows runs a query in the transaction.
func (t *Tx) QueryRows(ctx context.Context, query string, args ...any) (Rows, error) {
	return queryOn(ctx, t.tx, query, args)
}

// Query runs a query in the transaction and returns maps.
func (t *Tx) Query(ctx context.Context, query string, args ...any) ([]map[string]any, error) {
	r, err := t.QueryRows(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	return r.Maps(), nil
}

// Transaction runs f in a write transaction that takes the write lock up
// front. It commits when f returns nil and rolls back on an error or a panic.
func (d *DB) Transaction(ctx context.Context, f func(tx *Tx) error) (err error) {
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return wrap(err)
	}
	done := false
	defer func() {
		if !done {
			tx.Rollback()
		}
	}()
	if err := f(&Tx{tx: tx}); err != nil {
		return err
	}
	done = true
	if err := tx.Commit(); err != nil {
		tx.Rollback()
		return wrap(err)
	}
	return nil
}

// SQLite result code classes (low byte of the extended code).
const (
	codeBusy       = 5
	codeLocked     = 6
	codeConstraint = 19
)

// wrap gives driver errors a krate code: "busy", "constraint" or "sql_error".
func wrap(err error) error {
	if err == nil {
		return nil
	}
	var ke *krate.Error
	if errors.As(err, &ke) {
		return err
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	var se *sqlite.Error
	if errors.As(err, &se) {
		switch se.Code() & 0xff {
		case codeBusy, codeLocked:
			return krate.Errorf("busy", "%w", err)
		case codeConstraint:
			return krate.Errorf("constraint", "%w", err)
		}
	}
	return krate.Errorf("sql_error", "%w", err)
}

// CheckUISQL rejects SQL that would let the UI reach beyond the app database:
// ATTACH and DETACH (they open arbitrary files) and VACUUM (VACUUM INTO writes
// arbitrary files). The check is lexical per statement, so it is not fooled by
// quotes or comments. Go host code is not checked.
func CheckUISQL(query string) error {
	for _, w := range statementStarts(query) {
		switch strings.ToUpper(w) {
		case "ATTACH", "DETACH", "VACUUM":
			return krate.Errorf("forbidden_sql", "%s is not allowed from the UI", strings.ToUpper(w))
		}
	}
	return nil
}

// statementStarts returns the first word of every statement in query,
// skipping comments and quoted text.
func statementStarts(q string) []string {
	var out []string
	atStart := true
	i := 0
	for i < len(q) {
		c := q[i]
		switch {
		case c == '-' && i+1 < len(q) && q[i+1] == '-':
			for i < len(q) && q[i] != '\n' {
				i++
			}
		case c == '/' && i+1 < len(q) && q[i+1] == '*':
			i += 2
			for i+1 < len(q) && !(q[i] == '*' && q[i+1] == '/') {
				i++
			}
			i += 2
		case c == '\'' || c == '"' || c == '`':
			atStart = false
			i++
			for i < len(q) {
				if q[i] == c {
					if i+1 < len(q) && q[i+1] == c {
						i += 2
						continue
					}
					break
				}
				i++
			}
			i++
		case c == '[':
			atStart = false
			for i < len(q) && q[i] != ']' {
				i++
			}
			i++
		case c == ';':
			atStart = true
			i++
		case c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f' || c == '\v':
			i++
		default:
			j := i
			for j < len(q) && (q[j] == '_' || q[j] >= '0' && q[j] <= '9' || q[j] >= 'a' && q[j] <= 'z' || q[j] >= 'A' && q[j] <= 'Z') {
				j++
			}
			if atStart {
				if j > i {
					out = append(out, q[i:j])
				}
				atStart = false
			}
			if j == i {
				j++
			}
			i = j
		}
	}
	return out
}
