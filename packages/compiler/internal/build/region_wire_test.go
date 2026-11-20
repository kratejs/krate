package build

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/regionwire"
)

// frameServer answers /__krate/regions (or /__krate/gorender) with frames in
// one of three ways: "binary" only when the client asked for it (like a
// current renderer), "ndjson" always (like an older renderer that ignores
// Accept), or "binary-cut" (the binary format, cut off inside the last frame).
type frameServer struct {
	*httptest.Server
	mu     sync.Mutex
	accept []string
	body   []byte
}

func newFrameServer(t *testing.T, mode string, frames []regionFrame) *frameServer {
	t.Helper()
	fs := &frameServer{}
	fs.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		fs.mu.Lock()
		fs.accept = append(fs.accept, r.Header.Get("Accept"))
		fs.body = body
		fs.mu.Unlock()
		if mode != "ndjson" && strings.Contains(r.Header.Get("Accept"), regionwire.ContentType) {
			w.Header().Set("Content-Type", regionwire.ContentType)
			var out []byte
			for i := range frames {
				var err error
				if out, err = regionwire.AppendFrame(out, &frames[i]); err != nil {
					t.Error(err)
				}
			}
			if mode == "binary-cut" {
				out = out[:len(out)-3]
			}
			_, _ = w.Write(out)
			return
		}
		w.Header().Set("Content-Type", "application/x-ndjson")
		for _, f := range frames {
			line, _ := json.Marshal(f)
			_, _ = w.Write(append(line, '\n'))
		}
	}))
	t.Cleanup(fs.Close)
	return fs
}

func (fs *frameServer) port() int {
	p, _ := strconv.Atoi(fs.URL[strings.LastIndex(fs.URL, ":")+1:])
	return p
}

type fetched struct {
	html map[string]string
	res  regionPageResult
	err  error
}

func fetchFrom(t *testing.T, fs *frameServer, regions int) fetched {
	t.Helper()
	list := make([]map[string]string, regions)
	for i := range list {
		list[i] = map[string]string{"id": "r" + strconv.Itoa(i), "kind": "component"}
	}
	var out fetched
	out.html = map[string]string{}
	out.err = fetchNodeRegions(context.Background(), fs.port(), "/p/[id]", map[string]string{"id": "7"}, map[string]string{"q": "a<b"}, map[string]string{"Accept": "text/html"}, list, out.html, &out.res, "sekret")
	return out
}

var nodeFrameScenarios = map[string]struct {
	regions int
	frames  []regionFrame
}{
	"regions with head and title": {2, []regionFrame{
		{Type: "region", ID: "r0", Status: 200, HTML: "<p>a & b < c</p>", Title: "T", Head: `<meta name="a" content="b">`},
		{Type: "region", ID: "r1", Status: 200, HTML: "<i>é\U0001F600</i>", NoStore: true, ErrorCaught: true},
		{Type: "end", Count: 2},
	}},
	"isr page region": {1, []regionFrame{
		{Type: "region", ID: "page", Kind: "page", Status: 200, HTML: "<main>x</main>", CacheStatus: "stale"},
		{Type: "end", Count: 1},
	}},
	"page region fails": {1, []regionFrame{
		{Type: "region", ID: "page", Kind: "page", Status: 500, Error: "boom", Stack: "at x", Digest: "d1", Fatal: true},
		{Type: "end", Count: 1},
	}},
	"page region not found": {1, []regionFrame{
		{Type: "region", ID: "page", Kind: "page", Status: 404, NotFound: true},
		{Type: "end", Count: 1},
	}},
	"component region redirects and is denied": {2, []regionFrame{
		{Type: "region", ID: "r0", Status: 307, Redirect: "/login"},
		{Type: "region", ID: "r1", Status: 403, Denied: 403},
		{Type: "end", Count: 2},
	}},
	"error and skip frames": {2, []regionFrame{
		{Type: "error", ID: "r0", Status: 500, Error: "region failed", Fatal: true, Stack: "s", Digest: "d"},
		{Type: "skip", ID: "r1"},
		{Type: "end", Count: 2},
	}},
	"error frame without an id ends the stream": {2, []regionFrame{
		{Type: "error", Status: 500, Error: "no such page"},
		{Type: "end"},
	}},
}

func TestFetchNodeRegionsFormatsAgree(t *testing.T) {
	for name, sc := range nodeFrameScenarios {
		t.Run(name, func(t *testing.T) {
			nd := fetchFrom(t, newFrameServer(t, "ndjson", sc.frames), sc.regions)
			bin := newFrameServer(t, "binary", sc.frames)
			got := fetchFrom(t, bin, sc.regions)
			if !reflect.DeepEqual(nd, got) {
				t.Fatalf("binary and NDJSON results differ:\nndjson: %+v\nbinary: %+v", nd, got)
			}
			if bin.accept[0] != regionwire.ContentType {
				t.Fatalf("Accept = %q", bin.accept[0])
			}
		})
	}
}

func TestFetchNodeRegionsRequestBody(t *testing.T) {
	fs := newFrameServer(t, "binary", []regionFrame{{Type: "end"}})
	fetchFrom(t, fs, 1)
	var req struct {
		Route   string
		URL     string
		Method  string
		Headers map[string]string
		Params  map[string]string
		Query   map[string]string
		Regions []map[string]string
	}
	if err := json.Unmarshal(fs.body, &req); err != nil {
		t.Fatal(err)
	}
	want := "/p/[id] GET a<b 7 text/html r0"
	got := req.Route + " " + req.Method + " " + req.Query["q"] + " " + req.Params["id"] + " " + req.Headers["Accept"] + " " + req.Regions[0]["id"]
	if got != want {
		t.Fatalf("request = %q, want %q", got, want)
	}
}

func TestFetchNodeRegionsCutStreamMarksRenderErr(t *testing.T) {
	frames := []regionFrame{
		{Type: "region", ID: "r0", Status: 200, HTML: "<p>one</p>"},
		{Type: "region", ID: "r1", Status: 200, HTML: "<p>two</p>"},
	}
	got := fetchFrom(t, newFrameServer(t, "binary-cut", frames), 2)
	if got.err != nil {
		t.Fatal(got.err)
	}
	if got.html["r0"] != "<p>one</p>" || len(got.html) != 1 {
		t.Fatalf("html = %v", got.html)
	}
	if !strings.Contains(got.res.renderErr, "stream ended with 1 of 2 regions missing") {
		t.Fatalf("renderErr = %q", got.res.renderErr)
	}
}

func TestFetchNodeRegionsCorruptFrameMarksRenderErr(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", regionwire.ContentType+"; v=1")
		out, _ := regionwire.AppendFrame(nil, &regionFrame{Type: "region", ID: "r0", Status: 200, HTML: "ok"})
		_, _ = w.Write(out)
		_, _ = w.Write([]byte{0, 0, 0, 4, 9, 0, 0, 0}) // unknown frame type
		out, _ = regionwire.AppendFrame(nil, &regionFrame{Type: "region", ID: "r1", Status: 200, HTML: "never"})
		_, _ = w.Write(out)
	}))
	defer srv.Close()
	fs := &frameServer{Server: srv}
	got := fetchFrom(t, fs, 2)
	if got.html["r0"] != "ok" || len(got.html) != 1 || !strings.Contains(got.res.renderErr, "1 of 2 regions missing") {
		t.Fatalf("html = %v, renderErr = %q", got.html, got.res.renderErr)
	}
}

func TestStreamNodeRegionEventsFormats(t *testing.T) {
	frames := []regionFrame{
		{Type: "region", ID: "r0", Status: 200, HTML: "<p>a & b</p>", Head: "<meta>"},
		{Type: "region", ID: "r1", Status: 200, HTML: "<p>c</p>"},
		{Type: "end", Count: 2},
	}
	for _, mode := range []string{"ndjson", "binary"} {
		t.Run(mode, func(t *testing.T) {
			fs := newFrameServer(t, mode, frames)
			out := make(chan regionFrameEvent, 8)
			streamNodeRegionEvents(context.Background(), fs.port(), "/p", nil, nil, nil, []map[string]string{{"id": "r0"}, {"id": "r1"}}, "", out)
			close(out)
			var got []regionFrame
			for ev := range out {
				if ev.err != nil {
					t.Fatal(ev.err)
				}
				got = append(got, ev.frame)
			}
			if !reflect.DeepEqual(got, frames) {
				t.Fatalf("frames = %+v", got)
			}
		})
	}
}

func TestGoRenderContextFormats(t *testing.T) {
	frames := []regionFrame{
		{Type: "region", ID: "a", Status: 200, HTML: "<b>1 & 2 < 3</b>", Vary: &goVaryFrame{Query: []string{"q"}}},
		{Type: "region", ID: "b", Status: 500, Error: "kaboom"},
	}
	for _, mode := range []string{"ndjson", "binary", "binary-cut"} {
		t.Run(mode, func(t *testing.T) {
			fs := newFrameServer(t, mode, frames)
			g := newGoAPISupervisor("", "", 0)
			g.renderURL = fs.URL + "/__krate/gorender"
			out, err := g.RenderContext(context.Background(), []goRenderRegion{
				{ID: "a", Name: "Price", Props: `{"p":"<1>"}`, Revalidate: 60},
				{ID: "b", Name: "Bad", Props: "{}"},
			})
			if fs.accept[0] != regionwire.ContentType {
				t.Fatalf("Accept = %q", fs.accept[0])
			}
			if !strings.Contains(string(fs.body), `"props":"{\"p\":\"<1>\"}"`) {
				t.Fatalf("request body = %s", fs.body)
			}
			if mode == "binary-cut" {
				// The cut frame never arrives: its region is missing, no error.
				if err != nil || out["a"] != "<b>1 & 2 < 3</b>" || len(out) != 1 {
					t.Fatalf("out = %v, err = %v", out, err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), "go region render failed: kaboom") {
				t.Fatalf("err = %v", err)
			}
			if out["a"] != "<b>1 & 2 < 3</b>" || len(out) != 1 {
				t.Fatalf("out = %v", out)
			}
		})
	}
}

func TestGoRenderContextVaryAndCache(t *testing.T) {
	frames := []regionFrame{{Type: "region", ID: "a", Status: 200, HTML: "<b>x</b>", Vary: &goVaryFrame{Query: []string{"q"}, Full: false}}}
	for _, mode := range []string{"ndjson", "binary"} {
		t.Run(mode, func(t *testing.T) {
			fs := newFrameServer(t, mode, frames)
			g := newGoAPISupervisor("", "", 0)
			g.renderURL = fs.URL + "/__krate/gorender"
			reg := goRenderRegion{ID: "a", Name: "Price", Props: "{}", Request: `{"query":{"q":"1"}}`, Revalidate: 60}
			for i := 0; i < 2; i++ {
				if out, err := g.RenderContext(context.Background(), []goRenderRegion{reg}); err != nil || out["a"] != "<b>x</b>" {
					t.Fatalf("render %d: %v %v", i, out, err)
				}
			}
			if len(fs.accept) != 1 {
				t.Fatalf("the host was called %d times, want 1 (second is a cache hit)", len(fs.accept))
			}
			v := g.vary["Price"]
			if v == nil || v.full || !v.query["q"] {
				t.Fatalf("vary = %+v", v)
			}
		})
	}
}

func TestAppendJSONStringMatchesEncodingJSON(t *testing.T) {
	for _, s := range []string{"", "plain", `a"b\c`, "<a href='x'>&amp;</a>", "line\nbreak\r\ttab\x00\x01\x1f", "café 中 \U0001F600", "sep  ", "bad\xffutf\xc3", "\x7f del"} {
		var want string
		{
			b, _ := json.Marshal(s)
			// encoding/json escapes <, > and & as \u00XX; the hand-written form does not.
			var back string
			if err := json.Unmarshal(b, &back); err != nil {
				t.Fatal(err)
			}
			want = back
		}
		got := string(appendJSONString(nil, s))
		var back string
		if err := json.Unmarshal([]byte(got), &back); err != nil {
			t.Fatalf("%q: invalid JSON %s: %v", s, got, err)
		}
		if back != want {
			t.Errorf("%q: decodes to %q, want %q", s, back, want)
		}
		if strings.ContainsAny(s, "<>&") && strings.Contains(got, `\u00`) && !strings.ContainsAny(s, "\x00\x01\x1f") {
			t.Errorf("%q: HTML characters were escaped in %s", s, got)
		}
	}
}

func TestNodeRegionsRequestJSON(t *testing.T) {
	req := nodeRegionsRequest{
		Route: "/a/[id]", URL: "/a/1?x=<y>", Method: "GET",
		Headers: map[string]string{"Cookie": `a="b"`, "X-N": "\n"},
		Params:  map[string]string{"id": "1"},
		Regions: []map[string]string{{"id": "page", "kind": "page"}, {"id": "r1"}},
	}
	var got nodeRegionsRequest
	var raw struct {
		Route, URL, Method string
		Headers            map[string]string
		Params             map[string]string
		Query              map[string]string
		Regions            []map[string]string
	}
	if err := json.Unmarshal(req.marshal(), &raw); err != nil {
		t.Fatal(err)
	}
	got = nodeRegionsRequest{raw.Route, raw.URL, raw.Method, raw.Headers, raw.Params, raw.Query, raw.Regions}
	if !reflect.DeepEqual(got, req) {
		t.Fatalf("round trip: %+v", got)
	}
}

func TestGoRegionsRequestJSON(t *testing.T) {
	in := []goRenderRegion{{ID: "a", Name: "N", Props: `{"p":"<1>"}`, Request: `{"q":1}`, Revalidate: 30}, {ID: "b", Name: "M", Props: "{}"}}
	var out struct {
		Regions []goRenderRegion `json:"regions"`
	}
	if err := json.Unmarshal(marshalGoRegionsRequest(in), &out); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(out.Regions, in) {
		t.Fatalf("round trip: %+v", out.Regions)
	}
}
