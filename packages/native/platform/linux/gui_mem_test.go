//go:build linux

package linux

import (
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ebitengine/purego"
	"github.com/kratejs/krate/packages/native/platform/caps"
)

// Tests for shared web processes, release/restore, keepAlive and the
// memory-pressure event.

const tallPage = `<!doctype html><title>tall</title><body style="margin:0"><div style="height:6000px">tall</div><script>
try{var q=new URLSearchParams(location.search).get("__krate_win");if(q)sessionStorage.setItem("__krate_win",q)}catch(e){}
</script></body>`

const pressurePage = `<!doctype html><title>mp</title><body>mp<script>
window.addEventListener("krate:memory-pressure",function(){fetch("/mark?name=pressure")});
</script></body>`

func registerMemHandlers(s *testServer) {
	html := func(body string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = io.WriteString(w, body)
		}
	}
	s.mux.HandleFunc("/tall", html(tallPage))
	s.mux.HandleFunc("/pressure", html(pressurePage))
}

// descendants lists the pids below this process.
func descendants() []int {
	ents, _ := os.ReadDir("/proc")
	parent := map[int]int{}
	for _, e := range ents {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		b, err := os.ReadFile("/proc/" + e.Name() + "/status")
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(b), "\n") {
			if v, ok := strings.CutPrefix(line, "PPid:\t"); ok {
				pp, _ := strconv.Atoi(v)
				parent[pid] = pp
			}
		}
	}
	var out []int
	var walk func(p int)
	walk = func(p int) {
		for pid, pp := range parent {
			if pp == p {
				out = append(out, pid)
				walk(pid)
			}
		}
	}
	walk(os.Getpid())
	return out
}

func procName(pid int) string {
	b, _ := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/cmdline")
	return strings.ReplaceAll(string(b), "\x00", " ")
}

func webProcs() (n int) {
	for _, pid := range descendants() {
		if strings.Contains(procName(pid), "WebKitWebProcess") {
			n++
		}
	}
	return
}

func pssKB(pid int) int {
	b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/smaps_rollup")
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(line, "Pss:") {
			kb, _ := strconv.Atoi(strings.Fields(line)[1])
			return kb
		}
	}
	return 0
}

// totalPSSMB is the PSS of this process and its descendants.
func totalPSSMB() float64 {
	kb := pssKB(os.Getpid())
	for _, pid := range descendants() {
		kb += pssKB(pid)
	}
	return float64(kb) / 1024
}

func waitProcs(t *testing.T, want int) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if webProcs() == want {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("WebKitWebProcess count = %d, want %d", webProcs(), want)
}

func memWindow(t *testing.T, path string) *Window {
	t.Helper()
	loaded := make(chan struct{}, 8)
	w, err := guiApp.NewWindowNotify(WindowOptions{Title: path, Width: 400, Height: 300, Resizable: true, Path: path}, func() { loaded <- struct{}{} })
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-loaded:
	case <-time.After(20 * time.Second):
		t.Fatalf("window %s did not load", path)
	}
	return w
}

// evalValue evaluates js in the window's page and returns its string result.
func evalValue(t *testing.T, w *Window, js string) string {
	t.Helper()
	ch := make(chan string, 1)
	guiApp.Dispatch(func() {
		if w.view == 0 {
			ch <- "<no view>"
			return
		}
		guiApp.evalString(w.view, js, func(s string) { ch <- s })
	})
	select {
	case s := <-ch:
		return s
	case <-time.After(10 * time.Second):
		t.Fatalf("eval %q did not answer", js)
		return ""
	}
}

func TestGUISharedWebProcess(t *testing.T) {
	needGUI(t)
	waitProcs(t, 0)
	basePSS := totalPSSMB()
	a := memWindow(t, "/blank?a")
	waitProcs(t, 1)
	onePSS := totalPSSMB()
	b := memWindow(t, "/blank?b")
	c := memWindow(t, "/blank?c")
	d := memWindow(t, "/tall")
	waitProcs(t, 1)
	time.Sleep(1500 * time.Millisecond)
	fourPSS := totalPSSMB()
	t.Logf("PSS: before %.1f MB, 1 window %.1f MB, 4 windows %.1f MB (%.1f MB per extra window)",
		basePSS, onePSS, fourPSS, (fourPSS-onePSS)/3)

	// Settings apply to every view, not only the first.
	lib, err := purego.Dlopen("libwebkit2gtk-4.1.so.0", rtldNow|rtldGlobal)
	if err != nil {
		t.Fatal(err)
	}
	var devExtras func(s uintptr) int32
	purego.RegisterLibFunc(&devExtras, lib, "webkit_settings_get_enable_developer_extras")
	for name, w := range map[string]*Window{"a": a, "b": b, "c": c, "d": d} {
		var on int32
		guiApp.callMain(func() { on = devExtras(guiApp.api.wkViewGetSettings(w.view)) })
		if on != 1 {
			t.Errorf("window %s: developer extras = %d, want 1 (Debug is set)", name, on)
		}
	}

	// The first window can go; a new window still works and shares the process.
	a.Close()
	waitFor(t, "a closed", func() bool { return !a.alive })
	e := memWindow(t, "/blank?e")
	waitProcs(t, 1)
	e.Eval(`fetch("/mark?name=e-alive")`)
	guiSrv.waitMark(t, "e-alive")

	// When the last view goes the web process exits; the next window starts a new one.
	for _, w := range []*Window{b, c, d, e} {
		w.Close()
	}
	waitProcs(t, 0)
	f := memWindow(t, "/blank?f")
	waitProcs(t, 1)
	f.Close()
	waitProcs(t, 0)
}

func TestGUIReleaseRestore(t *testing.T) {
	needGUI(t)
	waitProcs(t, 0)
	w := memWindow(t, "/tall?__krate_win=w7")
	defer w.Close()
	waitProcs(t, 1)
	// Navigation inside the page drops the query; the window id must survive.
	evalValue(t, w, `(history.pushState({}, "", "/tall?x=2#frag"), window.scrollTo(0, 900), "")`)
	waitFor(t, "scrolled", func() bool { return evalValue(t, w, `String(Math.round(scrollY))`) == "900" })
	before := totalPSSMB()

	w.Release()
	waitFor(t, "released", w.Released)
	waitProcs(t, 0)
	if st := w.State(); st.Visible {
		t.Errorf("a released window is hidden, state %+v", st)
	}
	time.Sleep(700 * time.Millisecond)
	t.Logf("PSS with a window: %.1f MB, released: %.1f MB", before, totalPSSMB())
	if w.pv.state.Route != "/tall?x=2&__krate_win=w7#frag" || w.pv.state.Y != 900 {
		t.Errorf("saved state %+v", w.pv.state)
	}

	// Releasing twice is harmless.
	w.Release()

	loaded := make(chan struct{}, 4)
	w.OnLoadFinished(func() { loaded <- struct{}{} })
	w.Show()
	select {
	case <-loaded:
	case <-time.After(20 * time.Second):
		t.Fatal("restored window did not load")
	}
	waitProcs(t, 1)
	waitFor(t, "scroll restored", func() bool { return evalValue(t, w, `String(Math.round(scrollY))`) == "900" })
	if got := evalValue(t, w, `location.pathname + location.search + location.hash`); got != "/tall?x=2&__krate_win=w7#frag" {
		t.Errorf("route after restore = %q", got)
	}
}

func TestGUIKeepAlive(t *testing.T) {
	needGUI(t)
	waitProcs(t, 0)
	guiApp.callMain(func() { guiApp.keepAlive = true })
	defer guiApp.callMain(func() { guiApp.keepAlive = false })
	events := make(chan caps.WindowEvent, 16)
	guiApp.OnWindowEvent(func(e caps.WindowEvent) { events <- e })
	defer guiApp.OnWindowEvent(nil)

	a := memWindow(t, "/tall?q=1")
	waitProcs(t, 1)
	a.Close() // the last live window is parked, not destroyed
	waitFor(t, "released", a.Released)
	waitEvent := func(kind caps.WindowEventKind) {
		t.Helper()
		for {
			select {
			case e := <-events:
				if e.Window == a.Ref() && e.Kind == kind {
					return
				}
			case <-time.After(10 * time.Second):
				t.Fatalf("no %s event", kind)
			}
		}
	}
	waitEvent(caps.WindowReleased)
	waitProcs(t, 0)
	if !a.alive {
		t.Fatal("the parked window was destroyed")
	}

	// A second window closes for real while one is parked.
	b := memWindow(t, "/blank?b")
	b.Close()
	waitFor(t, "b destroyed", func() bool { return !b.alive })
	if !a.alive || !a.Released() {
		t.Fatal("closing another window disturbed the parked one")
	}

	// A title-bar close on the restored window parks it again.
	loaded := make(chan struct{}, 4)
	a.OnLoadFinished(func() { loaded <- struct{}{} })
	a.Show()
	select {
	case <-loaded:
	case <-time.After(20 * time.Second):
		t.Fatal("restored window did not load")
	}
	waitEvent(caps.WindowRestored)
	if got := evalValue(t, a, `location.pathname + location.search`); got != "/tall?q=1" {
		t.Errorf("route after restore = %q", got)
	}
	if !RequestClose(guiApp, a.Ref()) {
		t.Fatal("RequestClose")
	}
	waitFor(t, "parked again", a.Released)
	waitProcs(t, 0)

	// Closing the parked window destroys it; the app keeps running.
	a.Close()
	waitFor(t, "destroyed", func() bool { return !a.alive })
	if !guiApp.callMain(func() {}) || guiApp.stopped.Load() {
		t.Fatal("the app stopped with zero windows")
	}
	c := memWindow(t, "/blank?c")
	c.Close()
	waitFor(t, "c parked", c.Released)
	c.Show()
	waitFor(t, "c restored", func() bool { return !c.Released() })
	guiApp.callMain(func() { guiApp.keepAlive = false })
	c.Close()
	waitFor(t, "c destroyed", func() bool { return !c.alive })
}

func TestGUIMemoryPressureOnHide(t *testing.T) {
	needGUI(t)
	w := memWindow(t, "/pressure")
	defer w.Close()
	w.Hide()
	guiSrv.waitMark(t, "pressure")
	w.Show()
	waitFor(t, "shown", func() bool { return w.State().Visible })
	if os.Getenv("KRATE_NATIVE_WM") == "1" {
		w.Minimize()
		guiSrv.waitMark(t, "pressure")
	}
	// Hiding with release frees the page instead.
	w.Release()
	waitFor(t, "released", w.Released)
	w.Show()
	waitFor(t, "restored", func() bool { return !w.Released() })
}

func TestGUINavigateReleased(t *testing.T) {
	needGUI(t)
	w := memWindow(t, "/tall?x=1")
	defer w.Close()
	w.Release()
	waitFor(t, "released", w.Released)
	w.Navigate("/blank?other")
	loaded := make(chan struct{}, 4)
	w.OnLoadFinished(func() { loaded <- struct{}{} })
	w.Show()
	select {
	case <-loaded:
	case <-time.After(20 * time.Second):
		t.Fatal("restored window did not load")
	}
	if got := evalValue(t, w, `location.pathname + location.search`); got != "/blank?other" {
		t.Errorf("route = %q, want the route set while released", got)
	}
}
