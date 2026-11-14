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

func TestGUIReleaseRestore(t *testing.T) {
	needGUI(t)
	waitProcs(t, 0)
	w := memWindow(t, "/tall?__krate_win=w7")
	defer w.Close()
	waitProcs(t, 1)
	// Navigation inside the page drops the query; the window id must survive.
	waitFor(t, "the tall page to load", func() bool {
		return evalValue(t, w, `document.readyState + location.pathname + (document.body ? document.body.scrollHeight > 3000 : false)`) == "complete/talltrue"
	})
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
