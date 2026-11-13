package chrome

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/native/jsruntime"
	"github.com/kratejs/krate/packages/native/platform/caps"
)

type fakeWin struct {
	calls []string
	err   error
	state caps.ChromeState
	ws    caps.WindowState
}

func (f *fakeWin) ChromeState() caps.ChromeState                  { return f.state }
func (f *fakeWin) SetTitleBar(string) error                       { return nil }
func (f *fakeWin) SetBackdrop(string) error                       { return nil }
func (f *fakeWin) SetVibrancy(string) error                       { return nil }
func (f *fakeWin) SetTitleBarOverlay(*caps.TitleBarOverlay) error { return nil }
func (f *fakeWin) SetTrafficLightPosition(caps.Point) error       { return nil }
func (f *fakeWin) StartDrag() error                               { f.calls = append(f.calls, "drag"); return f.err }
func (f *fakeWin) StartResize(e string) error                     { f.calls = append(f.calls, "resize:"+e); return f.err }
func (f *fakeWin) ToggleMaximize() error                          { f.calls = append(f.calls, "max"); return f.err }
func (f *fakeWin) State() caps.WindowState                        { return f.ws }
func (f *fakeWin) Ref() caps.WindowRef                            { return 1 }
func (f *fakeWin) SetAlwaysOnTop(bool)                            {}
func (f *fakeWin) SetFullscreen(bool)                             {}
func (f *fakeWin) SetDecorated(bool)                              {}
func (f *fakeWin) SetPosition(int, int)                           {}
func (f *fakeWin) Minimize()                                      {}
func (f *fakeWin) Maximize()                                      {}
func (f *fakeWin) Restore()                                       {}
func (f *fakeWin) Focus()                                         {}
func (f *fakeWin) Show()                                          {}
func (f *fakeWin) Hide()                                          {}
func (f *fakeWin) SetPreventClose(bool)                           {}

func serve(h func(http.ResponseWriter, *http.Request) bool, method, path, body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	if !h(rec, r) {
		rec.Code = -1
	}
	return rec
}

func TestHandlerOps(t *testing.T) {
	f := &fakeWin{}
	h := Handler(func(*http.Request) (any, bool) { return f, true })
	for _, c := range []struct{ body, call string }{
		{`{"op":"drag"}`, "drag"},
		{`{"op":"toggleMaximize"}`, "max"},
		{`{"op":"resize","edge":"bottomRight"}`, "resize:bottomRight"},
	} {
		rec := serve(h, "POST", Path, c.body)
		if rec.Code != http.StatusNoContent {
			t.Fatalf("%s: status %d", c.body, rec.Code)
		}
		if got := f.calls[len(f.calls)-1]; got != c.call {
			t.Errorf("%s: call %q, want %q", c.body, got, c.call)
		}
	}
	n := len(f.calls)
	for _, body := range []string{`{"op":"resize","edge":"middle"}`, `{"op":"explode"}`, `not json`, `{}`} {
		rec := serve(h, "POST", Path, body)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status %d", body, rec.Code)
		}
	}
	if len(f.calls) != n {
		t.Errorf("rejected requests reached the window: %v", f.calls[n:])
	}
	if rec := serve(h, "DELETE", Path, ""); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("DELETE status %d", rec.Code)
	}
	if rec := serve(h, "GET", "/other", ""); rec.Code != -1 {
		t.Errorf("other path was handled: %d", rec.Code)
	}
	f.err = errors.New("nope")
	if rec := serve(h, "POST", Path, `{"op":"drag"}`); rec.Code != http.StatusNotImplemented {
		t.Errorf("failing op status %d", rec.Code)
	}
}

func TestHandlerNeedsAWindowWithChrome(t *testing.T) {
	h := Handler(func(*http.Request) (any, bool) { return nil, false })
	if rec := serve(h, "POST", Path, `{"op":"drag"}`); rec.Code != http.StatusNotFound {
		t.Errorf("no window: %d", rec.Code)
	}
	h = Handler(func(*http.Request) (any, bool) { return struct{}{}, true })
	if rec := serve(h, "GET", Path, ""); rec.Code != http.StatusNotFound {
		t.Errorf("window without chrome: %d", rec.Code)
	}
}

func TestInfo(t *testing.T) {
	f := &fakeWin{
		state: caps.ChromeState{TitleBar: "none", Resizable: true, ScriptResize: true},
		ws:    caps.WindowState{Decorated: false},
	}
	h := Handler(func(*http.Request) (any, bool) { return f, true })
	rec := serve(h, "GET", Path, "")
	var got Info
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if !got.Edges || got.Decorated || got.TitleBar != "none" {
		t.Errorf("%+v", got)
	}
	f.ws.Maximized = true
	if BuildInfo(f.state, f.ws).Edges {
		t.Error("a maximized window needs no resize edges")
	}
	f.ws = caps.WindowState{Decorated: true}
	if BuildInfo(f.state, f.ws).Edges {
		t.Error("a decorated window needs no resize edges")
	}
	f.state.ScriptResize = false
	f.ws = caps.WindowState{}
	if BuildInfo(f.state, f.ws).Edges {
		t.Error("a platform with native resize edges needs none")
	}
}

func TestWanted(t *testing.T) {
	if !Wanted([]byte(`<div data-krate-drag>`)) || !Wanted([]byte(`a{-webkit-app-region:drag}`)) {
		t.Error("markers not found")
	}
	if Wanted([]byte(`<html><body>plain</body></html>`)) {
		t.Error("a plain page wants the script")
	}
}

// runJS evaluates the script in a bare QuickJS VM with a stub DOM and returns
// the result of expr.
func runJS(t *testing.T, expr string) any {
	t.Helper()
	rt, err := jsruntime.NewBare()
	if err != nil {
		t.Fatal(err)
	}
	const stub = `
var window = globalThis;
window.matchMedia = function (q) { return { matches: q.indexOf("dark") < 0 }; };
var document = {
  readyState: "complete",
  documentElement: {}, body: {},
  querySelectorAll: function () { return []; },
  addEventListener: function () {},
  createElement: function () { return { style: {}, setAttribute: function () {}, addEventListener: function () {} }; },
};
var addEventListener = function () {};
var clearTimeout = function () {}, setTimeout = function () { return 1; };
var fetch = function () { return { then: function () { return this; }, catch: function () { return this; } }; };
var MutationObserver = function () { this.observe = function () {}; };
`
	if _, err := rt.Execute(stub); err != nil {
		t.Fatalf("stub: %v", err)
	}
	if _, err := rt.Execute(scriptSource); err != nil {
		t.Fatalf("script: %v", err)
	}
	v, err := rt.Execute(expr)
	if err != nil {
		t.Fatalf("%s: %v", expr, err)
	}
	return v
}

func TestScriptParsesAppRegionRules(t *testing.T) {
	css := `
/* header */
.bar, header { -webkit-app-region: drag; height: 30px }
.bar button{-webkit-app-region:no-drag}
.x { app-region: drag !important; }
.y { color: red }
.z { app-region: none }
@media (prefers-color-scheme: dark) { .dark { app-region: drag } }
@media (min-width: 1px) { .wide { -webkit-app-region: drag } }
@import url(x.css);
.last{margin:0;-webkit-app-region:drag;app-region:no-drag}
`
	v := runJS(t, `JSON.stringify(__krateChrome._parse(`+jsString(css)+`))`)
	var got []struct{ Sel, V string }
	if err := json.Unmarshal([]byte(v.(string)), &got); err != nil {
		t.Fatal(err)
	}
	want := []struct{ Sel, V string }{
		{".bar, header", "drag"}, {".bar button", "no-drag"}, {".x", "drag"}, {".z", "none"},
		{".wide", "drag"}, {".last", "no-drag"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %+v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("rule %d: %+v, want %+v", i, got[i], want[i])
		}
	}
}

func jsString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func TestScriptReportsVersion(t *testing.T) {
	if v := runJS(t, `__krateChrome.version`); v != int64(1) && v != float64(1) && v != int(1) {
		t.Errorf("version %v (%T)", v, v)
	}
}

func TestDragRegionLeavesControlsAlone(t *testing.T) {
	// Elements are plain stubs; only the attributes regionOf reads exist.
	const defs = `
function el(tag, attrs, parent) {
  return { nodeType: 1, tagName: tag, parentElement: parent || null, isContentEditable: false,
    getAttribute: function (k) { return attrs && k in attrs ? attrs[k] : null; },
    matches: function () { return false; } };
}
var bar = el("HEADER", { "data-krate-drag": "" });
`
	cases := map[string]string{
		"plain":            `el("DIV", null, bar)`,
		"native button":    `el("BUTTON", null, bar)`,
		"role button":      `el("DIV", { role: "button" }, bar)`,
		"role Menuitem":    `el("SPAN", { role: " Menuitem " }, bar)`,
		"tabindex 0":       `el("DIV", { tabindex: "0" }, bar)`,
		"tabindex -1":      `el("DIV", { tabindex: "-1" }, bar)`,
		"role heading":     `el("H1", { role: "heading" }, bar)`,
		"child of control": `el("SPAN", null, el("DIV", { role: "tab" }, bar))`,
		"explicit drag":    `el("DIV", { role: "button", "data-krate-drag": "" }, bar)`,
	}
	want := map[string]string{
		"plain": "drag", "native button": "no-drag", "role button": "no-drag", "role Menuitem": "no-drag",
		"tabindex 0": "no-drag", "tabindex -1": "drag", "role heading": "drag", "child of control": "no-drag",
		"explicit drag": "drag",
	}
	for name, expr := range cases {
		got := runJS(t, defs+`__krateChrome.regionOf(`+expr+`)`)
		if got != want[name] {
			t.Errorf("%s: region %v, want %s", name, got, want[name])
		}
	}
}
