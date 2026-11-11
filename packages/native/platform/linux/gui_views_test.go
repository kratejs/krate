//go:build linux

package linux

import (
	"io"
	"math"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ebitengine/purego"
	"github.com/kratejs/krate/packages/native/internal/memory"
	"github.com/kratejs/krate/packages/native/platform/caps"
)

// Tests for several webviews in one window: native view identity of requests,
// split panes (GtkPaned), dividers, minimum sizes, events, release and the
// shared web process.

type viewPing struct {
	name   string
	view   caps.ViewRef
	native bool
	header string
}

var viewPings = make(chan viewPing, 64)

func registerViewHandlers(s *testServer) {
	html := func(body string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = io.WriteString(w, body)
		}
	}
	// The page reports itself and forges the window header on its own call.
	s.mux.HandleFunc("/pane", html(`<!doctype html><title>pane</title><body style="margin:0">pane<script>
fetch("/ping?name="+encodeURIComponent(location.search),{headers:{"x-krate-window":"forged"}});
</script></body>`))
	s.mux.HandleFunc("/closer", html(`<!doctype html><title>closer</title><body>closer<script>
fetch("/ping?name=closer").then(function(){setTimeout(function(){window.close()},100)});
</script></body>`))
	s.mux.HandleFunc("/ping", func(w http.ResponseWriter, r *http.Request) {
		v, ok := caps.ViewOf(r)
		viewPings <- viewPing{name: r.URL.Query().Get("name"), view: v, native: ok, header: r.Header.Get("x-krate-window")}
		w.WriteHeader(204)
	})
}

// pingBacklog holds pings that arrived while waiting for another one, so the
// order in which two views load does not matter.
var pingBacklog []viewPing

func waitPing(t *testing.T, name string) viewPing {
	t.Helper()
	for i, p := range pingBacklog {
		if p.name == name {
			pingBacklog = append(pingBacklog[:i], pingBacklog[i+1:]...)
			return p
		}
	}
	deadline := time.After(20 * time.Second)
	for {
		select {
		case p := <-viewPings:
			if p.name == name {
				return p
			}
			pingBacklog = append(pingBacklog, p)
		case <-deadline:
			t.Fatalf("timed out waiting for the ping of %q (backlog %v)", name, pingBacklog)
		}
	}
}

func drainPings() {
	pingBacklog = nil
	for {
		select {
		case <-viewPings:
		default:
			return
		}
	}
}

func evalIn(t *testing.T, w *Window, ref caps.ViewRef, js string) string {
	t.Helper()
	ch := make(chan string, 1)
	guiApp.Dispatch(func() {
		v := w.viewByRef(ref)
		if v == nil || v.widget == 0 {
			ch <- "<no view>"
			return
		}
		guiApp.evalString(v.widget, js, func(s string) { ch <- s })
	})
	select {
	case s := <-ch:
		return s
	case <-time.After(10 * time.Second):
		t.Fatalf("eval %q did not answer", js)
		return ""
	}
}

// paneSize reads the allocated size of the first and second half of the split
// at path along its axis.
func paneSize(w *Window, path string) (first, second int) {
	w.app.callMain(func() {
		for _, p := range w.panes {
			if p.path == path {
				x := w.app.x
				if p.axis == caps.Vertical {
					first, second = int(x.gtkWidgetAllocH(p.first)), int(x.gtkWidgetAllocH(p.second))
				} else {
					first, second = int(x.gtkWidgetAllocW(p.first)), int(x.gtkWidgetAllocW(p.second))
				}
			}
		}
	})
	return
}

func viewWindow(t *testing.T, path string) *Window {
	t.Helper()
	drainPings()
	w := memWindow(t, path)
	return w
}

func collectViewEvents(t *testing.T) (events func() []caps.ViewEvent) {
	t.Helper()
	var mu sync.Mutex
	var got []caps.ViewEvent
	guiApp.OnViewEvent(func(e caps.ViewEvent) {
		mu.Lock()
		got = append(got, e)
		mu.Unlock()
	})
	t.Cleanup(func() { guiApp.OnViewEvent(nil) })
	return func() []caps.ViewEvent {
		mu.Lock()
		defer mu.Unlock()
		return append([]caps.ViewEvent(nil), got...)
	}
}

func TestGUIViewIdentity(t *testing.T) {
	needGUI(t)
	a := viewWindow(t, "/pane?a")
	pa := waitPing(t, "?a")
	if !pa.native || pa.view != a.PrimaryView() || pa.view == 0 {
		t.Fatalf("ping from window a: %+v, primary view %d", pa, a.PrimaryView())
	}
	// The page forged the header; the native identity does not depend on it.
	if pa.header != "forged" {
		t.Fatalf("header %q: the page's header should arrive unchanged", pa.header)
	}
	b := viewWindow(t, "/pane?b")
	pb := waitPing(t, "?b")
	if pb.view != b.PrimaryView() || pb.view == pa.view {
		t.Fatalf("window b: %+v, a's view %d", pb, pa.view)
	}
	extra, err := a.AddView(caps.ViewOptions{Path: "/pane?a2"})
	if err != nil {
		t.Fatal(err)
	}
	p2 := waitPing(t, "?a2")
	if p2.view != extra {
		t.Fatalf("a second view of window a reported %d, want %d", p2.view, extra)
	}
	for ref, want := range map[caps.ViewRef]caps.WindowRef{pa.view: a.Ref(), pb.view: b.Ref(), extra: a.Ref()} {
		if got, ok := guiApp.WindowOfView(ref); !ok || got != want {
			t.Errorf("WindowOfView(%d) = %d, %v; want %d", ref, got, ok, want)
		}
	}
	if _, ok := guiApp.WindowOfView(9999); ok {
		t.Error("WindowOfView found a view that does not exist")
	}
	a.Close()
	b.Close()
	waitFor(t, "windows closed", func() bool { return !a.alive && !b.alive })
	if _, ok := guiApp.WindowOfView(extra); ok {
		t.Error("a closed window's view is still known")
	}
}

func TestGUIViewsTwoPane(t *testing.T) {
	needGUI(t)
	waitProcs(t, 0)
	events := collectViewEvents(t)
	w := viewWindow(t, "/pane?main")
	defer w.Close() // a failed assertion must not leave its web process to the next test
	waitPing(t, "?main")
	waitProcs(t, 1)

	ref, err := w.AddView(caps.ViewOptions{Path: "/pane?side", Axis: caps.Horizontal, Share: 0.3, MinWidth: 120})
	if err != nil {
		t.Fatal(err)
	}
	waitPing(t, "?side")
	if got := w.Views(); len(got) != 2 || !got[0].Primary || got[1].Ref != ref || got[1].Primary {
		t.Fatalf("views %+v", got)
	}
	// One web process serves both views.
	waitProcs(t, 1)

	// The ratio lands near what was asked: the new view gets 30 percent.
	waitFor(t, "ratio applied", func() bool {
		l := w.Layout()
		return !l.IsLeaf() && math.Abs(l.Ratio-0.7) < 0.03
	})
	l := w.Layout()
	if l.Axis != caps.Horizontal || l.First.View != w.PrimaryView() || l.Second.View != ref || l.Second.MinWidth != 120 {
		t.Fatalf("layout %+v / %+v / %+v", l, l.First, l.Second)
	}
	// A compositor configures the surface asynchronously (Wayland), so the
	// paned has no real allocation yet when the views are added.
	waitFor(t, "pane allocated", func() bool { f, s := paneSize(w, ""); return f > 1 && s > 1 })
	if f, s := paneSize(w, ""); s < 120 {
		t.Fatalf("pane sizes %d, %d", f, s)
	}

	// A divider drag, synthesized through GTK, changes the ratio and the sizes.
	if err := w.SetDividerPosition("", 250); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "divider at 250", func() bool { f, _ := paneSize(w, ""); return f == 250 })
	f, s := paneSize(w, "")
	want := float64(f) / float64(f+s)
	if got := w.Layout().Ratio; math.Abs(got-want) > 0.002 {
		t.Fatalf("ratio after drag = %v, want %v (panes %d and %d)", got, want, f, s)
	}

	// The minimum size of the new pane holds: dragging the divider all the
	// way right leaves the second pane at least 120 px wide.
	if err := w.SetDividerPosition("", 100000); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "divider pushed right", func() bool { f2, _ := paneSize(w, ""); return f2 > 250 })
	if _, s2 := paneSize(w, ""); s2 < 120 {
		t.Errorf("second pane is %d px wide, below its minimum of 120", s2)
	}

	// Each view got the app's settings: developer extras (Debug is set) and the
	// memory profile.
	lib, err := purego.Dlopen("libwebkit2gtk-4.1.so.0", rtldNow|rtldGlobal)
	if err != nil {
		t.Fatal(err)
	}
	var devExtras, webgl func(s uintptr) int32
	purego.RegisterLibFunc(&devExtras, lib, "webkit_settings_get_enable_developer_extras")
	purego.RegisterLibFunc(&webgl, lib, "webkit_settings_get_enable_webgl")
	for name, r := range map[string]caps.ViewRef{"primary": w.PrimaryView(), "added": ref} {
		widget := w.viewWidget(r)
		guiApp.callMain(func() {
			set := guiApp.api.wkViewGetSettings(widget)
			if devExtras(set) != 1 {
				t.Errorf("%s view: developer extras off", name)
			}
			if (webgl(set) != 0) != (guiApp.profile == memory.Performance) {
				t.Errorf("%s view: webgl = %d under the %s profile", name, webgl(set), guiApp.profile)
			}
		})
	}

	// Removing the view gives the space back and reports it.
	if err := w.RemoveView(w.PrimaryView()); err == nil {
		t.Error("the primary view was removed")
	}
	if err := w.RemoveView(ref); err != nil {
		t.Fatal(err)
	}
	if got := w.Views(); len(got) != 1 {
		t.Fatalf("views after removal: %+v", got)
	}
	if !w.Layout().IsLeaf() {
		t.Error("layout is not a single pane after removal")
	}
	if _, ok := guiApp.WindowOfView(ref); ok {
		t.Error("a removed view is still known")
	}
	if err := w.RemoveView(ref); err == nil {
		t.Error("removing a view twice succeeded")
	}
	var kinds []caps.ViewEventKind
	for _, e := range events() {
		if e.View == ref {
			kinds = append(kinds, e.Kind)
		}
	}
	if len(kinds) < 3 || kinds[0] != caps.ViewAdded || kinds[len(kinds)-1] != caps.ViewRemoved {
		t.Errorf("events of the added view: %v, want added ... navigated ... removed", kinds)
	}
	sawNav := false
	for _, k := range kinds {
		sawNav = sawNav || k == caps.ViewNavigated
	}
	if !sawNav {
		t.Errorf("no navigated event: %v", kinds)
	}
	w.Close()
	waitFor(t, "closed", func() bool { return !w.alive })
	waitProcs(t, 0)
}

func TestGUIViewsNestedThreePane(t *testing.T) {
	needGUI(t)
	w := viewWindow(t, "/pane?root")
	waitPing(t, "?root")
	defer w.Close()
	b, err := w.AddView(caps.ViewOptions{Path: "/pane?b", Axis: caps.Horizontal, Share: 0.5})
	if err != nil {
		t.Fatal(err)
	}
	c, err := w.AddView(caps.ViewOptions{Path: "/pane?c", Beside: b, Axis: caps.Vertical, Share: 0.4, MinHeight: 80})
	if err != nil {
		t.Fatal(err)
	}
	waitPing(t, "?b")
	waitPing(t, "?c")

	l := w.Layout()
	if l.Axis != caps.Horizontal || l.First.View != w.PrimaryView() || l.Second.Axis != caps.Vertical ||
		l.Second.First.View != b || l.Second.Second.View != c {
		t.Fatalf("layout is not 1 | (b over c): %+v", l)
	}
	waitFor(t, "nested ratio", func() bool {
		r := w.Layout().Second.Ratio
		return math.Abs(r-0.6) < 0.03
	})
	waitProcs(t, 1)

	// Mark every page, then rearrange: the pages must not reload.
	for _, r := range []caps.ViewRef{w.PrimaryView(), b, c} {
		evalIn(t, w, r, `(window.__keep = "kept", "")`)
	}
	// Drag the nested divider (vertical): the top pane becomes 160 px.
	if err := w.SetDividerPosition("1", 160); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "nested divider", func() bool { f, _ := paneSize(w, "1"); return f == 160 })
	f, s := paneSize(w, "1")
	if got, want := w.Layout().Second.Ratio, float64(f)/float64(f+s); math.Abs(got-want) > 0.002 {
		t.Fatalf("nested ratio %v, want %v", got, want)
	}

	// Rearranged as three stacked panes: c, root, b with the ratios asked.
	three := &caps.LayoutNode{Axis: caps.Vertical, Ratio: 0.25,
		First: &caps.LayoutNode{View: c, MinHeight: 60},
		Second: &caps.LayoutNode{Axis: caps.Vertical, Ratio: 0.5,
			First:  caps.Leaf(w.PrimaryView()),
			Second: caps.Leaf(b)}}
	if err := w.SetLayout(three); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "stacked ratios", func() bool {
		l := w.Layout()
		return l.Axis == caps.Vertical && math.Abs(l.Ratio-0.25) < 0.03 && l.Second.First.View == w.PrimaryView()
	})
	for _, r := range []caps.ViewRef{w.PrimaryView(), b, c} {
		if got := evalIn(t, w, r, `String(window.__keep)`); got != "kept" {
			t.Errorf("view %d reloaded while the layout changed (marker %q)", r, got)
		}
	}
	if got := w.Layout().First.MinHeight; got != 60 {
		t.Errorf("min height of the first pane = %d, want 60", got)
	}

	// Invalid layouts are refused and change nothing.
	bad := map[string]*caps.LayoutNode{
		"missing view": {Axis: caps.Horizontal, First: caps.Leaf(w.PrimaryView()), Second: caps.Leaf(b)},
		"duplicate":    {Axis: caps.Horizontal, First: caps.Leaf(b), Second: &caps.LayoutNode{Axis: caps.Horizontal, First: caps.Leaf(b), Second: caps.Leaf(c)}},
		"foreign view": {Axis: caps.Horizontal, First: caps.Leaf(w.PrimaryView()), Second: &caps.LayoutNode{Axis: caps.Horizontal, First: caps.Leaf(b), Second: caps.Leaf(12345)}},
		"bad ratio":    {Axis: caps.Horizontal, Ratio: 2, First: caps.Leaf(w.PrimaryView()), Second: &caps.LayoutNode{Axis: caps.Horizontal, First: caps.Leaf(b), Second: caps.Leaf(c)}},
	}
	for name, n := range bad {
		if err := w.SetLayout(n); err == nil {
			t.Errorf("%s: SetLayout accepted it", name)
		}
	}
	if l := w.Layout(); l.Axis != caps.Vertical || len(l.Leaves()) != 3 {
		t.Errorf("layout changed after refused SetLayout: %+v", l)
	}

	// Removing the middle pane of a stack keeps the others.
	if err := w.RemoveView(w.PrimaryView()); err == nil {
		t.Error("primary removed")
	}
	if err := w.RemoveView(b); err != nil {
		t.Fatal(err)
	}
	if got := w.Layout().Leaves(); len(got) != 2 {
		t.Fatalf("leaves after removal: %v", got)
	}
}

func TestGUIViewsReleaseRestore(t *testing.T) {
	needGUI(t)
	waitProcs(t, 0)
	w := viewWindow(t, "/pane?r0")
	waitPing(t, "?r0")
	defer w.Close()
	ref, err := w.AddView(caps.ViewOptions{Path: "/tall?x=1", Share: 0.4})
	if err != nil {
		t.Fatal(err)
	}
	waitProcs(t, 1)
	waitFor(t, "ratio", func() bool { return math.Abs(w.Layout().Ratio-0.6) < 0.03 })
	// A scroll only sticks once the page is loaded and laid out; on a loaded
	// runner the added view can still be on its first document.
	waitFor(t, "the tall page to load", func() bool {
		return evalIn(t, w, ref, `document.readyState + location.pathname + (document.body ? document.body.scrollHeight > 3000 : false)`) == "complete/talltrue"
	})
	evalIn(t, w, ref, `(history.pushState({}, "", "/tall?x=2#frag"), window.scrollTo(0, 700), "")`)
	waitFor(t, "scrolled", func() bool { return evalIn(t, w, ref, `String(Math.round(scrollY))`) == "700" })
	if err := w.SetDividerPosition("", 300); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "divider", func() bool { f, _ := paneSize(w, ""); return f == 300 })
	f, s := paneSize(w, "")
	wantRatio := float64(f) / float64(f+s)

	// Release destroys every webview of the window; the web process exits.
	w.Release()
	waitFor(t, "released", w.Released)
	waitProcs(t, 0)
	if n := guiApp.WebViewCount(); n != 0 {
		t.Fatalf("%d webviews alive after release", n)
	}
	if _, err := w.AddView(caps.ViewOptions{}); err == nil {
		t.Error("AddView on a released window succeeded")
	}
	if got := w.Views(); len(got) != 2 {
		t.Fatalf("a released window keeps its views: %+v", got)
	}

	drainPings()
	w.Show()
	waitFor(t, "restored", func() bool { return !w.Released() })
	waitPing(t, "?r0")
	waitProcs(t, 1)
	if n := guiApp.WebViewCount(); n != 2 {
		t.Errorf("%d webviews after restore, want 2", n)
	}
	waitFor(t, "view route", func() bool {
		return evalIn(t, w, ref, `location.pathname + location.search + location.hash`) == "/tall?x=2#frag"
	})
	waitFor(t, "scroll restored", func() bool { return evalIn(t, w, ref, `String(Math.round(scrollY))`) == "700" })
	waitFor(t, "ratio restored", func() bool {
		f, s := paneSize(w, "")
		return f > 0 && math.Abs(float64(f)/float64(f+s)-wantRatio) < 0.02
	})
	// Ids survive: the ref of the added view is the same one.
	if got := w.Views(); len(got) != 2 || got[1].Ref != ref {
		t.Errorf("views after restore: %+v", got)
	}
}

func TestGUIViewClosedByItsPage(t *testing.T) {
	needGUI(t)
	events := collectViewEvents(t)
	w := viewWindow(t, "/pane?host")
	waitPing(t, "?host")
	defer w.Close()
	ref, err := w.AddView(caps.ViewOptions{Path: "/closer"})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "view closed by its page", func() bool { return len(w.Views()) == 1 })
	var last caps.ViewEvent
	for _, e := range events() {
		if e.View == ref {
			last = e
		}
	}
	if last.Kind != caps.ViewClosed {
		t.Errorf("last event of the view: %+v, want closed", last)
	}
	// Closing the window reports its views closed, before the window.
	if _, err := w.AddView(caps.ViewOptions{Path: "/pane?x"}); err != nil {
		t.Fatal(err)
	}
	order := make(chan string, 8)
	guiApp.OnViewEvent(func(e caps.ViewEvent) { order <- "view:" + string(e.Kind) })
	guiApp.OnWindowEvent(func(e caps.WindowEvent) { order <- "window:" + string(e.Kind) })
	defer guiApp.OnWindowEvent(nil)
	w.Close()
	var seen []string
	for len(seen) < 3 {
		select {
		case s := <-order:
			seen = append(seen, s)
		case <-time.After(5 * time.Second):
			t.Fatalf("events after close: %v", seen)
		}
	}
	if seen[2] != "window:closed" || seen[0] != "view:closed" || seen[1] != "view:closed" {
		t.Errorf("close order: %v", seen)
	}
}

// pssSplit returns the PSS in MB of the shell process and of the WebKit web
// process(es), measured after things settle.
func pssSplit() (shell, web float64) {
	// A new process shrinks for a few seconds as its start-up garbage goes;
	// the smallest of several late samples is the steady state.
	time.Sleep(5 * time.Second)
	shell, web = math.Inf(1), math.Inf(1)
	for i := 0; i < 4; i++ {
		sh, wb := float64(pssKB(os.Getpid()))/1024, 0.0
		for _, pid := range descendants() {
			if strings.Contains(procName(pid), "WebKitWebProcess") {
				wb += float64(pssKB(pid)) / 1024
			}
		}
		shell, web = math.Min(shell, sh), math.Min(web, wb)
		time.Sleep(700 * time.Millisecond)
	}
	return
}

// Reports what one extra view costs. All views must stay in one web process.
func TestGUIViewsMemory(t *testing.T) {
	needGUI(t)
	waitProcs(t, 0)
	w := viewWindow(t, "/pane?m0")
	defer w.Close() // a failed assertion must not leave its web process to the next test
	waitProcs(t, 1)
	shell1, web1 := pssSplit()
	const extra = 6
	for i := 0; i < extra; i++ {
		if _, err := w.AddView(caps.ViewOptions{Path: "/pane?m" + strconv.Itoa(i+1), Share: 0.2}); err != nil {
			t.Fatal(err)
		}
	}
	waitProcs(t, 1)
	if n := guiApp.WebViewCount(); n != extra+1 {
		t.Fatalf("%d webviews, want %d", n, extra+1)
	}
	shellN, webN := pssSplit()
	// A second window with one view, for comparison.
	w2 := memWindow(t, "/pane?m-win")
	waitProcs(t, 1)
	shellW, webW := pssSplit()
	t.Logf("shell PSS: 1 view %.1f MB, %d views %.1f MB, +1 window %.1f MB", shell1, extra+1, shellN, shellW)
	t.Logf("web process PSS: 1 view %.1f MB, %d views %.1f MB, +1 window %.1f MB", web1, extra+1, webN, webW)
	t.Logf("per extra view in a window: shell %+.1f MB, web process %+.1f MB; per extra window: shell %+.1f MB, web process %+.1f MB",
		(shellN-shell1)/extra, (webN-web1)/extra, shellW-shellN, webW-webN)
	if per := (webN - web1) / extra; per > 40 {
		t.Errorf("an extra view costs %.1f MB of web process memory", per)
	}
	w2.Close()
	w.Close()
	waitFor(t, "closed", func() bool { return !w.alive })
	waitProcs(t, 0)
}
