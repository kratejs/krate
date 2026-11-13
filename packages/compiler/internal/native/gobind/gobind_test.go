package gobind

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

var fixtureImports = []Import{
	{File: "src/lib/tasks.go", Names: []string{
		"List", "Add", "Version", "Clear", "Reset", "Fail", "Roundtrip", "Echo", "Shift", "Walk",
		"Narrow", "Pick", "Watch", "Ticks", "Count", "Titles", "Latest",
	}},
	{File: "src/lib/card.server.go", Names: []string{"Card"}},
}

func generateFixture(t *testing.T) (string, *Result) {
	t.Helper()
	root := newApp(t, "app")
	res, diags, err := Generate(root, fixtureImports, ".krate/native")
	if err != nil {
		t.Fatal(err)
	}
	if len(diags) > 0 {
		t.Fatalf("diagnostics: %v", diags)
	}
	return root, res
}

func TestGenerateGolden(t *testing.T) {
	root, res := generateFixture(t)

	if len(res.Glue) != 1 || res.Glue[0].ImportPath != "example.com/app/.krate/native/glue/src/lib" {
		t.Fatalf("glue = %+v", res.Glue)
	}
	if len(res.Modules) != 2 {
		t.Fatalf("modules = %+v", res.Modules)
	}
	card, tasks := res.Modules[0], res.Modules[1]
	if card.ID != "src/lib/card.server" || card.Stub != "" || strings.Join(card.Components, ",") != "Card" || len(card.Host) != 0 {
		t.Errorf("component module = %+v", card)
	}
	if tasks.ID != "src/lib/tasks" || tasks.Stub == "" || tasks.DTS == "" {
		t.Errorf("tasks module = %+v", tasks)
	}

	read := func(p string) []byte {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	golden(t, "glue.go.golden", read(res.Glue[0].File))
	golden(t, "tasks.go.js.golden", read(tasks.Stub))
	golden(t, "tasks.go.d.ts.golden", read(tasks.DTS))

	// Deterministic: a second run writes identical bytes.
	first := read(res.Glue[0].File)
	if _, diags, err := Generate(root, fixtureImports, ".krate/native"); err != nil || len(diags) > 0 {
		t.Fatalf("second run: %v %v", err, diags)
	}
	if string(read(res.Glue[0].File)) != string(first) {
		t.Error("output is not deterministic")
	}
	if got := filepath.ToSlash(strings.TrimPrefix(tasks.DTS, root)); got != "/.krate/native/types/src/lib/tasks.go.d.ts" {
		t.Errorf("d.ts path = %s", got)
	}
}

const e2eTest = `package glue

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/kratejs/krate/packages/native/scheme"
	"github.com/kratejs/krate/packages/native/wire"
)

type callArg func(e *wire.Encoder)

func newServer(t *testing.T) *httptest.Server {
	t.Helper()
	reg := scheme.NewRegistry()
	Register(reg)
	srv := httptest.NewServer(scheme.New(scheme.Options{Registry: reg}))
	t.Cleanup(srv.Close)
	return srv
}

// call posts one call and returns the result frame decoder positioned inside
// the object after the id key.
func call(t *testing.T, srv *httptest.Server, fn string, args ...callArg) *wire.Decoder {
	t.Helper()
	e := wire.NewEncoder()
	e.BeginObject()
	e.Key("calls")
	e.BeginArray()
	e.BeginObject()
	e.Key("id")
	e.Int(1)
	e.Key("fn")
	e.String(fn)
	e.Key("args")
	e.BeginArray()
	for _, a := range args {
		a(e)
	}
	e.EndArray()
	e.EndObject()
	e.EndArray()
	e.EndObject()
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/__krate/call", bytes.NewReader(e.Frame()))
	req.Header.Set("x-krate-window", "w1")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	d, err := wire.ReadFrame(resp.Body)
	if err != nil {
		t.Fatalf("%s: %v", fn, err)
	}
	if err := d.BeginObject(); err != nil {
		t.Fatal(err)
	}
	if k, _ := d.Key(); k != "id" {
		t.Fatalf("first key = %q", k)
	}
	if _, err := d.Int(); err != nil {
		t.Fatal(err)
	}
	return d
}

func ok(t *testing.T, d *wire.Decoder) {
	t.Helper()
	k, err := d.Key()
	if err != nil || k != "ok" {
		raw, _ := d.RawValue()
		t.Fatalf("want ok, got key %q (%v) %s", k, err, raw)
	}
}

func failure(t *testing.T, d *wire.Decoder) (code, msg string) {
	t.Helper()
	if k, _ := d.Key(); k != "err" {
		t.Fatalf("want err, got %q", k)
	}
	if err := d.BeginObject(); err != nil {
		t.Fatal(err)
	}
	for d.More() {
		k, _ := d.Key()
		switch k {
		case "code":
			code, _ = d.String()
		case "message":
			msg, _ = d.String()
		default:
			d.Skip()
		}
	}
	return
}

func str(s string) callArg { return func(e *wire.Encoder) { e.String(s) } }
func num(n int64) callArg  { return func(e *wire.Encoder) { e.Int(n) } }

func TestRoundTrips(t *testing.T) {
	srv := newServer(t)

	// Struct result with time, bytes, nested map, pointer and flattened embedding.
	d := call(t, srv, "src/lib/tasks.Add", str("Buy milk"), func(e *wire.Encoder) {
		e.BeginArray()
		e.String("a")
		e.String("b")
		e.EndArray()
	})
	ok(t, d)
	task, err := decodeTask(d)
	if err != nil {
		t.Fatal(err)
	}
	if task.ID != 7 || task.Title != "Buy milk" || len(task.Tags) != 2 || task.Tags[1] != "b" {
		t.Errorf("task = %+v", task)
	}
	if !task.Created.Equal(time.Date(2026, 11, 11, 7, 23, 15, 0, time.UTC)) {
		t.Errorf("created = %v", task.Created)
	}
	if string(task.Blob) != "Buy milk" || task.Timeout != 3*time.Second || task.Meta == nil || task.Meta.Labels["b"] != "2" {
		t.Errorf("task = %+v", task)
	}
	if task.Secret != "" {
		t.Errorf("secret leaked: %q", task.Secret)
	}

	// Error with a code.
	d = call(t, srv, "src/lib/tasks.Add", str("  "), func(e *wire.Encoder) { e.BeginArray(); e.EndArray() })
	if code, msg := failure(t, d); code != "invalid" || !strings.Contains(msg, "title is empty") {
		t.Errorf("error = %q %q", code, msg)
	}
	d = call(t, srv, "src/lib/tasks.Fail", str("teapot"))
	if code, _ := failure(t, d); code != "teapot" {
		t.Errorf("code = %q", code)
	}

	// Bytes.
	payload := []byte{0, 1, 2, 255, 128}
	d = call(t, srv, "src/lib/tasks.Roundtrip", func(e *wire.Encoder) { e.Bytes(payload) })
	ok(t, d)
	if got, err := d.Bytes(); err != nil || !bytes.Equal(got, payload) {
		t.Errorf("bytes = %v %v", got, err)
	}

	// Time and duration.
	at := time.Date(2026, 1, 2, 3, 4, 5, 6000000, time.UTC)
	d = call(t, srv, "src/lib/tasks.Shift", func(e *wire.Encoder) { e.Time(at) }, num(int64(time.Hour)))
	ok(t, d)
	if got, err := d.Time(); err != nil || !got.Equal(at.Add(time.Hour)) {
		t.Errorf("time = %v %v", got, err)
	}
	d = call(t, srv, "src/lib/tasks.Echo", func(e *wire.Encoder) { e.Bytes(payload) }, func(e *wire.Encoder) { e.Time(at) })
	ok(t, d)
	// Echoed is only a result type, so it has no decoder: read it by hand.
	d.BeginObject()
	for d.More() {
		k, _ := d.Key()
		switch k {
		case "data":
			if got, err := d.Bytes(); err != nil || !bytes.Equal(got, payload) {
				t.Errorf("echo data = %v %v", got, err)
			}
		case "at":
			if got, err := d.Time(); err != nil || !got.Equal(at) {
				t.Errorf("echo at = %v %v", got, err)
			}
		default:
			t.Errorf("unexpected key %q", k)
			d.Skip()
		}
	}

	// Recursive type, with unknown fields skipped and missing fields zero.
	d = call(t, srv, "src/lib/tasks.Walk", func(e *wire.Encoder) {
		e.BeginObject()
		e.Key("Name")
		e.String("root")
		e.Key("extra")
		e.BeginArray()
		e.Int(1)
		e.EndArray()
		e.Key("Kids")
		e.BeginArray()
		e.BeginObject()
		e.Key("Name")
		e.String("kid")
		e.EndObject()
		e.EndArray()
		e.EndObject()
	})
	ok(t, d)
	if tree, err := decodeTree(d); err != nil || tree.Name != "ROOT" || len(tree.Kids) != 1 || tree.Kids[0].Name != "KID" {
		t.Errorf("tree = %+v %v", tree, err)
	}

	// Narrow numbers, range checks.
	d = call(t, srv, "src/lib/tasks.Narrow", num(-5), num(300), func(e *wire.Encoder) { e.Float(2.5) }, num(10))
	ok(t, d)
	if n, err := d.Int(); err != nil || n != -5+300+2+10 {
		t.Errorf("narrow = %d %v", n, err)
	}
	d = call(t, srv, "src/lib/tasks.Narrow", num(200), num(0), num(0), num(0))
	if code, msg := failure(t, d); code != "bad_args" || !strings.Contains(msg, "overflows int8") {
		t.Errorf("overflow = %q %q", code, msg)
	}

	// Pointers and string-kinded map keys.
	d = call(t, srv, "src/lib/tasks.Pick", func(e *wire.Encoder) { e.Null() }, func(e *wire.Encoder) { e.Null() })
	ok(t, d)
	if !d.IsNull() {
		t.Error("nil pointer should round trip as null")
	}
	d = call(t, srv, "src/lib/tasks.Pick", func(e *wire.Encoder) { e.BeginObject(); e.Key("title"); e.String("x"); e.EndObject() },
		func(e *wire.Encoder) { e.BeginObject(); e.Key("open"); e.Int(1); e.EndObject() })
	ok(t, d)
	if p, err := decodePtrToTask(d); err != nil || p == nil || p.Title != "x!" {
		t.Errorf("pick = %+v %v", p, err)
	}

	// No results and error-only results answer null.
	for _, fn := range []string{"src/lib/tasks.Reset", "src/lib/tasks.Clear"} {
		d = call(t, srv, fn)
		ok(t, d)
		if !d.IsNull() {
			t.Errorf("%s should answer null", fn)
		}
	}
	d = call(t, srv, "src/lib/tasks.Version")
	ok(t, d)
	if s, _ := d.String(); s != "1.2.3" {
		t.Errorf("version = %q", s)
	}

	// Argument count and type errors.
	d = call(t, srv, "src/lib/tasks.Fail")
	if code, msg := failure(t, d); code != "bad_args" || !strings.Contains(msg, "missing argument 0 (code)") {
		t.Errorf("missing arg = %q %q", code, msg)
	}
	d = call(t, srv, "src/lib/tasks.Fail", str("a"), str("b"))
	if code, _ := failure(t, d); code != "bad_args" {
		t.Errorf("extra arg code = %q", code)
	}
	d = call(t, srv, "src/lib/tasks.Fail", num(1))
	if code, msg := failure(t, d); code != "bad_args" || !strings.Contains(msg, "argument 0 (code)") {
		t.Errorf("wrong type = %q %q", code, msg)
	}
	// Functions the UI did not import are not registered.
	d = call(t, srv, "src/lib/tasks.Unused")
	if code, _ := failure(t, d); code != "not_found" {
		t.Errorf("unused code = %q", code)
	}
}

func readFrames(t *testing.T, r io.Reader, n int) []*wire.Decoder {
	t.Helper()
	var out []*wire.Decoder
	for len(out) < n {
		d, err := wire.ReadFrame(r)
		if err != nil {
			t.Fatalf("frame %d: %v", len(out), err)
		}
		out = append(out, d)
	}
	return out
}

func TestStream(t *testing.T) {
	srv := newServer(t)
	d := call(t, srv, "src/lib/tasks.Watch", num(3))
	if k, _ := d.Key(); k != "stream" {
		t.Fatalf("key = %q", k)
	}
	id, _ := d.String()
	resp, err := http.Get(srv.URL + "/__krate/stream/" + id)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	frames := readFrames(t, resp.Body, 4)
	for i, f := range frames[:3] {
		f.BeginObject()
		if k, _ := f.Key(); k != "v" {
			t.Fatalf("frame %d key = %q", i, k)
		}
		task, err := decodeTask(f)
		if err != nil || task.ID != int64(i) {
			t.Errorf("frame %d = %+v %v", i, task, err)
		}
	}
	frames[3].BeginObject()
	if k, _ := frames[3].Key(); k != "end" {
		t.Errorf("last frame key = %q", k)
	}

	// A stream function that fails before streaming answers with an error.
	d = call(t, srv, "src/lib/tasks.Watch", num(-1))
	if k, _ := d.Key(); k != "stream" {
		t.Fatalf("key = %q", k)
	}
	id, _ = d.String()
	resp2, err := http.Get(srv.URL + "/__krate/stream/" + id)
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	ef := readFrames(t, resp2.Body, 1)[0]
	ef.BeginObject()
	if code, _ := failure(t, ef); code != "invalid" {
		t.Errorf("stream error code = %q", code)
	}
}

func TestSignals(t *testing.T) {
	srv := newServer(t)
	resp, err := http.Get(srv.URL + "/__krate/signals?names=src/lib/tasks.Count,src/lib/tasks.Titles")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	seen := map[string]bool{}
	var initial int64
	for _, f := range readFrames(t, resp.Body, 2) {
		f.BeginObject()
		f.Key()
		name, _ := f.String()
		f.Key()
		switch name {
		case "src/lib/tasks.Count":
			if n, err := f.Int(); err != nil || n < 0 {
				t.Errorf("count = %d %v", n, err)
			} else {
				initial = n
			}
		case "src/lib/tasks.Titles":
			if s, err := decodeSliceOfString(f); err != nil || len(s) != 0 {
				t.Errorf("titles = %v %v", s, err)
			}
		}
		seen[name] = true
	}
	if len(seen) != 2 {
		t.Errorf("seen = %v", seen)
	}

	// A change reaches the open connection.
	call(t, srv, "src/lib/tasks.Add", str("x"), func(e *wire.Encoder) { e.BeginArray(); e.EndArray() })
	got := map[string]bool{}
	for _, f := range readFrames(t, resp.Body, 1) {
		f.BeginObject()
		f.Key()
		name, _ := f.String()
		got[name] = true
		f.Key()
		if n, err := f.Int(); err != nil || n != initial+1 {
			t.Errorf("updated count = %d %v, want %d", n, err, initial+1)
		}
	}
	if !got["src/lib/tasks.Count"] {
		t.Errorf("no count update: %v", got)
	}
}
`

func TestGeneratedGlueEndToEnd(t *testing.T) {
	root, res := generateFixture(t)
	// The Latest signal is not exercised over HTTP; the test file only needs
	// the package to compile with it.
	dir := res.Glue[0].Dir
	if err := os.WriteFile(filepath.Join(dir, "e2e_test.go"), []byte(e2eTest), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("go", "test", "-count=1", "-timeout=60s", res.Glue[0].ImportPath)
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go test in the generated package failed: %v\n%s", err, out)
	}
	cmd = exec.Command("go", "vet", res.Glue[0].ImportPath)
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go vet on the generated package failed: %v\n%s", err, out)
	}
}
