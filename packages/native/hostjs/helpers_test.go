package hostjs

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/evanw/esbuild/pkg/api"

	"github.com/kratejs/krate/packages/native/scheme"
	"github.com/kratejs/krate/packages/native/wire"
)

// bundleTS compiles TypeScript to an IIFE with an external source map, as the
// compiler does for host code.
func bundleTS(t testing.TB, file, src string) (js, sourceMap []byte) {
	t.Helper()
	res := api.Build(api.BuildOptions{
		Stdin:     &api.StdinOptions{Contents: src, Loader: api.LoaderTS, Sourcefile: file},
		Outfile:   "host.js",
		Format:    api.FormatIIFE,
		Target:    api.ES2020,
		Sourcemap: api.SourceMapExternal,
		Write:     false,
		LogLevel:  api.LogLevelSilent,
	})
	if len(res.Errors) > 0 {
		t.Fatalf("esbuild: %v", res.Errors)
	}
	for _, f := range res.OutputFiles {
		if strings.HasSuffix(f.Path, ".map") {
			sourceMap = f.Contents
		} else {
			js = f.Contents
		}
	}
	return js, sourceMap
}

type rig struct {
	t    testing.TB
	h    *Host
	reg  *scheme.Registry
	ts   *httptest.Server
	logs *syncLog
}

type syncLog struct {
	mu   chan struct{}
	text strings.Builder
}

func (l *syncLog) add(s string) {
	l.mu <- struct{}{}
	l.text.WriteString(s + "\n")
	<-l.mu
}

func (l *syncLog) String() string {
	l.mu <- struct{}{}
	defer func() { <-l.mu }()
	return l.text.String()
}

// newRig starts a Host, loads src as src/lib/host.ts and serves it.
func newRig(t testing.TB, src string, o Options) *rig {
	t.Helper()
	logs := &syncLog{mu: make(chan struct{}, 1)}
	o.Log = logs.add
	h, err := New(o)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { h.Close() })
	js, sm := bundleTS(t, "src/lib/host.ts", src)
	if err := h.Load(js, sm, "host.js"); err != nil {
		t.Fatal(err)
	}
	return finishRig(t, h, logs)
}

func finishRig(t testing.TB, h *Host, logs *syncLog) *rig {
	t.Helper()
	reg := scheme.NewRegistry()
	h.Register(reg)
	ts := httptest.NewServer(scheme.New(scheme.Options{Registry: reg}))
	t.Cleanup(ts.Close)
	return &rig{t: t, h: h, reg: reg, ts: ts, logs: logs}
}

type callResult struct {
	ID   int64
	OK   any
	Err  map[string]any
	Strm string
}

// args writes the argument array.
type argsFn func(e *wire.Encoder)

func (r *rig) callFrame(id int64, fn string, args argsFn) []byte {
	e := wire.NewEncoder()
	e.BeginObject()
	e.Key("calls")
	e.BeginArray()
	e.BeginObject()
	e.Key("id")
	e.Int(id)
	e.Key("fn")
	e.String(fn)
	e.Key("args")
	e.BeginArray()
	if args != nil {
		args(e)
	}
	e.EndArray()
	e.EndObject()
	e.EndArray()
	e.EndObject()
	return e.Frame()
}

func (r *rig) post(path string, body []byte) *http.Response {
	r.t.Helper()
	req, _ := http.NewRequest(http.MethodPost, r.ts.URL+path, bytes.NewReader(body))
	req.Header.Set("x-krate-window", "w1")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		r.t.Fatal(err)
	}
	r.t.Cleanup(func() { resp.Body.Close() })
	return resp
}

// call performs one call and returns its result frame.
func (r *rig) call(fn string, args argsFn) callResult {
	r.t.Helper()
	resp := r.post("/__krate/call", r.callFrame(1, fn, args))
	return readResult(r.t, resp.Body)
}

func readResult(t testing.TB, body io.Reader) callResult {
	t.Helper()
	d := readFrame(t, body)
	v := toAny(t, d)
	m, ok := v.(map[string]any)
	if !ok {
		t.Fatalf("result frame is %T: %v", v, v)
	}
	res := callResult{OK: m["ok"]}
	if id, ok := m["id"].(float64); ok {
		res.ID = int64(id)
	}
	if e, ok := m["err"].(map[string]any); ok {
		res.Err = e
	}
	res.Strm, _ = m["stream"].(string)
	return res
}

func readFrame(t testing.TB, r io.Reader) *wire.Decoder {
	t.Helper()
	type res struct {
		d   *wire.Decoder
		err error
	}
	ch := make(chan res, 1)
	go func() {
		d, err := wire.ReadFrame(r, wire.WithMaxFrame(64<<20))
		ch <- res{d, err}
	}()
	select {
	case x := <-ch:
		if x.err != nil {
			t.Fatalf("reading frame: %v", x.err)
		}
		return x.d
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for a frame")
		return nil
	}
}

// jsMap and jsSet are what toAny returns for $m and $s values.
type (
	jsMap [][2]any
	jsSet []any
)

// toAny decodes a value generically: objects become maps, $m a jsMap, $s a
// jsSet, bytes []byte, dates time.Time, numbers float64.
func toAny(t testing.TB, d *wire.Decoder) any {
	t.Helper()
	switch d.Kind() {
	case wire.KindObject:
		if err := d.BeginObject(); err != nil {
			t.Fatal(err)
		}
		m := map[string]any{}
		for d.More() {
			k, err := d.Key()
			if err != nil {
				t.Fatal(err)
			}
			m[k] = toAny(t, d)
		}
		if err := d.EndObject(); err != nil {
			t.Fatal(err)
		}
		return m
	case wire.KindArray:
		if err := d.BeginArray(); err != nil {
			t.Fatal(err)
		}
		arr := []any{}
		for d.More() {
			arr = append(arr, toAny(t, d))
		}
		if err := d.EndArray(); err != nil {
			t.Fatal(err)
		}
		return arr
	case wire.KindMap:
		if err := d.BeginMap(); err != nil {
			t.Fatal(err)
		}
		m := jsMap{}
		for d.More() {
			if err := d.BeginArray(); err != nil {
				t.Fatal(err)
			}
			k := toAny(t, d)
			v := toAny(t, d)
			if err := d.EndArray(); err != nil {
				t.Fatal(err)
			}
			m = append(m, [2]any{k, v})
		}
		if err := d.EndArray(); err != nil {
			t.Fatal(err)
		}
		return m
	case wire.KindSet:
		if err := d.BeginSet(); err != nil {
			t.Fatal(err)
		}
		set := jsSet{}
		for d.More() {
			set = append(set, toAny(t, d))
		}
		if err := d.EndArray(); err != nil {
			t.Fatal(err)
		}
		return set
	case wire.KindBytes:
		b, err := d.Bytes()
		if err != nil {
			t.Fatal(err)
		}
		return b
	case wire.KindTime:
		tm, err := d.Time()
		if err != nil {
			t.Fatal(err)
		}
		return tm
	case wire.KindString:
		s, err := d.String()
		if err != nil {
			t.Fatal(err)
		}
		return s
	case wire.KindNumber:
		f, err := d.Float()
		if err != nil {
			t.Fatal(err)
		}
		return f
	case wire.KindBool:
		b, err := d.Bool()
		if err != nil {
			t.Fatal(err)
		}
		return b
	case wire.KindNull:
		d.IsNull()
		return nil
	}
	t.Fatalf("unexpected value kind at offset %d: %v", d.Offset(), d.Err())
	return nil
}

func str(v any) string { return fmt.Sprint(v) }

type rawResult struct {
	Err   map[string]any
	OKDec *wire.Decoder // positioned at the ok value
}

// callRaw performs one call and returns the decoder of the ok value, for tests
// that need the result's exact wire form. The caller's args write the single
// argument.
func (r *rig) callRaw(fn string, args argsFn) rawResult {
	r.t.Helper()
	resp := r.post("/__krate/call", r.callFrame(1, fn, args))
	d := readFrame(r.t, resp.Body)
	if err := d.BeginObject(); err != nil {
		r.t.Fatal(err)
	}
	var out rawResult
	for d.More() {
		k, err := d.Key()
		if err != nil {
			r.t.Fatal(err)
		}
		switch k {
		case "ok":
			sub, err := d.SubDecoder()
			if err != nil {
				r.t.Fatal(err)
			}
			out.OKDec = sub
		case "err":
			out.Err, _ = toAny(r.t, d).(map[string]any)
		default:
			if err := d.Skip(); err != nil {
				r.t.Fatal(err)
			}
		}
	}
	return out
}
