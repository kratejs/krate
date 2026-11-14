package db

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kratejs/krate/packages/native/krate"
	"github.com/kratejs/krate/packages/native/scheme"
	"github.com/kratejs/krate/packages/native/sdk/internal/sdktest"
	"github.com/kratejs/krate/packages/native/sdk/permission"
	"github.com/kratejs/krate/packages/native/wire"
)

// TestMain doubles as the helper process of TestTwoProcesses.
func TestMain(m *testing.M) {
	if path := os.Getenv("KRATE_DB_HELPER"); path != "" {
		os.Exit(helper(path))
	}
	os.Exit(m.Run())
}

// helper opens the shared database and inserts rows tagged "child".
func helper(path string) int {
	d, err := OpenPath(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, "helper open:", err)
		return 1
	}
	defer d.Close()
	ctx := context.Background()
	for i := 0; i < 200; i++ {
		if _, err := d.Exec(ctx, "INSERT INTO t(who, n) VALUES('child', ?)", i); err != nil {
			fmt.Fprintln(os.Stderr, "helper insert:", err)
			return 1
		}
	}
	return 0
}

var bg = context.Background()

func open(t *testing.T) *DB {
	t.Helper()
	d, err := OpenPath(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

func code(err error) string {
	var ke *krate.Error
	if errors.As(err, &ke) {
		return ke.Code
	}
	return ""
}

func TestExecQueryValues(t *testing.T) {
	d := open(t)
	if _, err := d.Exec(bg, "CREATE TABLE t(id INTEGER PRIMARY KEY, i INTEGER, f REAL, s TEXT, b BLOB, n)"); err != nil {
		t.Fatal(err)
	}
	blob := []byte{0, 1, 2, 255, 0}
	res, err := d.Exec(bg, "INSERT INTO t(i, f, s, b, n) VALUES(?, ?, ?, ?, ?)", int64(1<<40), 2.5, "héllo", blob, nil)
	if err != nil || res.RowsAffected != 1 || res.LastInsertID != 1 {
		t.Fatalf("%+v %v", res, err)
	}
	// bool, int and time parameters are converted.
	when := time.Date(2026, 11, 11, 7, 23, 15, 123e6, time.UTC)
	if _, err := d.Exec(bg, "INSERT INTO t(i, s, n) VALUES(?, ?, ?)", true, when, 7); err != nil {
		t.Fatal(err)
	}
	rows, err := d.Query(bg, "SELECT * FROM t ORDER BY id")
	if err != nil || len(rows) != 2 {
		t.Fatalf("%v %v", rows, err)
	}
	r := rows[0]
	if r["i"] != int64(1<<40) || r["f"] != 2.5 || r["s"] != "héllo" || !bytes.Equal(r["b"].([]byte), blob) || r["n"] != nil {
		t.Fatalf("%#v", r)
	}
	if rows[1]["i"] != int64(1) || rows[1]["s"] != "2026-11-11T07:23:15.123Z" || rows[1]["n"] != int64(7) {
		t.Fatalf("%#v", rows[1])
	}
	cols, err := d.QueryRows(bg, "SELECT s AS a, i AS b, f AS c FROM t LIMIT 1")
	if err != nil || strings.Join(cols.Columns, ",") != "a,b,c" {
		t.Fatalf("column order: %v %v", cols.Columns, err)
	}
	if _, err := d.Exec(bg, "INSERT INTO t(i) VALUES(?)", map[string]int{}); err == nil {
		t.Fatal("a map parameter was accepted")
	}
	// An empty result is an empty slice, not nil.
	empty, err := d.Query(bg, "SELECT * FROM t WHERE id = -1")
	if err != nil || empty == nil || len(empty) != 0 {
		t.Fatalf("%#v %v", empty, err)
	}
}

func TestTransactionRollbackAndCommit(t *testing.T) {
	d := open(t)
	if _, err := d.Exec(bg, "CREATE TABLE t(id INTEGER PRIMARY KEY, v TEXT UNIQUE)"); err != nil {
		t.Fatal(err)
	}

	boom := errors.New("boom")
	err := d.Transaction(bg, func(tx *Tx) error {
		if _, err := tx.Exec(bg, "INSERT INTO t(v) VALUES('a')"); err != nil {
			t.Error(err)
		}
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatal(err)
	}
	if rows, _ := d.Query(bg, "SELECT * FROM t"); len(rows) != 0 {
		t.Fatalf("rollback left %v", rows)
	}

	// A panic rolls back too.
	func() {
		defer func() { _ = recover() }()
		if err := d.Transaction(bg, func(tx *Tx) error {
			if _, err := tx.Exec(bg, "INSERT INTO t(v) VALUES('p')"); err != nil {
				return err
			}
			panic("x")
		}); err != nil {
			t.Error(err)
		}
	}()
	if rows, _ := d.Query(bg, "SELECT * FROM t"); len(rows) != 0 {
		t.Fatalf("panic left %v", rows)
	}

	err = d.Transaction(bg, func(tx *Tx) error {
		if _, err := tx.Exec(bg, "INSERT INTO t(v) VALUES('a')"); err != nil {
			return err
		}
		rows, err := tx.Query(bg, "SELECT count(*) AS n FROM t")
		if err != nil || rows[0]["n"] != int64(1) {
			return fmt.Errorf("read your writes: %v %v", rows, err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// The pool must be usable after the transaction ends.
	if rows, _ := d.Query(bg, "SELECT * FROM t"); len(rows) != 1 {
		t.Fatalf("%v", rows)
	}

	// A constraint failure maps to its code.
	_, err = d.Exec(bg, "INSERT INTO t(v) VALUES('a')")
	if code(err) != "constraint" {
		t.Fatalf("%v", err)
	}
	_, err = d.Exec(bg, "SELEKT 1")
	if code(err) != "sql_error" {
		t.Fatalf("%v", err)
	}
}

// Two processes (the shell and a Go host child) share one file.
func TestTwoProcesses(t *testing.T) {
	path := filepath.Join(t.TempDir(), "shared.db")
	d, err := OpenPath(path)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if _, err := d.Exec(bg, "CREATE TABLE t(id INTEGER PRIMARY KEY, who TEXT, n INTEGER)"); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(os.Args[0], "-test.run=^$")
	cmd.Env = append(os.Environ(), "KRATE_DB_HELPER="+path)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 200; i++ {
		if _, err := d.Exec(bg, "INSERT INTO t(who, n) VALUES('parent', ?)", i); err != nil {
			t.Fatalf("parent insert %d: %v", i, err)
		}
	}
	if err := cmd.Wait(); err != nil {
		t.Fatalf("helper: %v\n%s", err, stderr.String())
	}
	rows, err := d.Query(bg, "SELECT who, count(*) AS c FROM t GROUP BY who ORDER BY who")
	if err != nil || len(rows) != 2 || rows[0]["c"] != int64(200) || rows[1]["c"] != int64(200) {
		t.Fatalf("%v %v", rows, err)
	}
}

func TestCheckUISQL(t *testing.T) {
	ok := []string{
		"SELECT 1", "insert into t values('attach database x')", "SELECT 'x'; -- ATTACH\nSELECT 2",
		"/* ATTACH */ SELECT 1", `SELECT "attach" FROM t`, "SELECT attach FROM t", "WITH x AS (SELECT 1) SELECT * FROM x",
		"CREATE TABLE detach_log(a)", "UPDATE t SET a = 'VACUUM'",
	}
	bad := []string{
		"ATTACH DATABASE '/etc/x' AS y", "attach '/x' as y", "  \n ATTACH 'x' AS y", "SELECT 1; ATTACH 'x' AS y",
		"DETACH y", "VACUUM", "vacuum into '/tmp/x'", "/* c */ VACUUM INTO 'x'", "SELECT 'a''b'; ATTACH 'x' AS y",
		"-- hi\nATTACH 'x' AS y", `SELECT "a;b"; ATTACH 'x' AS y`,
	}
	for _, q := range ok {
		if err := CheckUISQL(q); err != nil {
			t.Errorf("%q: %v", q, err)
		}
	}
	for _, q := range bad {
		if err := CheckUISQL(q); code(err) != "forbidden_sql" {
			t.Errorf("%q: %v", q, err)
		}
	}
}

func uiCaller(t *testing.T, d *DB, perms ...string) *sdktest.Caller {
	t.Helper()
	set, err := permission.Parse(perms)
	if err != nil {
		t.Fatal(err)
	}
	reg := scheme.NewRegistry()
	RegisterWith(reg, set, func() (*DB, error) { return d, nil })
	return sdktest.New(t, reg)
}

func TestUITransaction(t *testing.T) {
	d := open(t)
	c := uiCaller(t, d)
	if _, err := d.Exec(bg, "CREATE TABLE t(id INTEGER PRIMARY KEY, v TEXT UNIQUE)"); err != nil {
		t.Fatal(err)
	}
	stmts := func(vals ...string) func(*wire.Encoder) {
		return func(e *wire.Encoder) {
			e.BeginArray()
			for _, v := range vals {
				e.BeginObject()
				e.Key("sql")
				e.String("INSERT INTO t(v) VALUES(?)")
				e.Key("params")
				e.BeginArray()
				e.String(v)
				e.EndArray()
				e.EndObject()
			}
			e.EndArray()
		}
	}
	r := c.Call("krate/db.transaction", stmts("a", "b"))
	if r.Failed || r.OK != `[{"rowsAffected":1,"lastInsertId":1},{"rowsAffected":1,"lastInsertId":2}]` {
		t.Fatalf("%+v", r)
	}
	// The second statement violates UNIQUE; the first must roll back.
	r = c.Call("krate/db.transaction", stmts("c", "a"))
	if r.ErrCode != "constraint" || !strings.Contains(r.ErrMessage, "statement 1") {
		t.Fatalf("%+v", r)
	}
	rows, _ := d.Query(bg, "SELECT v FROM t ORDER BY v")
	if len(rows) != 2 {
		t.Fatalf("rollback failed: %v", rows)
	}
	// A forbidden statement anywhere stops the whole batch before it runs.
	r = c.Call("krate/db.transaction", func(e *wire.Encoder) {
		e.BeginArray()
		for _, q := range []string{"INSERT INTO t(v) VALUES('z')", "ATTACH 'x' AS y"} {
			e.BeginObject()
			e.Key("sql")
			e.String(q)
			e.EndObject()
		}
		e.EndArray()
	})
	if r.ErrCode != "forbidden_sql" {
		t.Fatalf("%+v", r)
	}
	if rows, _ := d.Query(bg, "SELECT v FROM t WHERE v = 'z'"); len(rows) != 0 {
		t.Fatal("a statement ran before the batch was rejected")
	}
}

func TestUIDeniedWithoutPermission(t *testing.T) {
	d := open(t)
	c := uiCaller(t, d, "fs")
	for _, id := range []string{"krate/db.exec", "krate/db.query", "krate/db.transaction"} {
		r := c.Call(id, func(e *wire.Encoder) { e.String("SELECT 1") })
		if r.ErrCode != "permission_denied" || r.ErrData != `{"permission":"db"}` {
			t.Errorf("%s: %+v", id, r)
		}
	}
}
