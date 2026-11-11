package shell

import (
	"os"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/kratejs/krate/packages/native/platform/caps"
)

// descendantPIDs lists the processes below this one (Linux).
func descendantPIDs() []int {
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

func webProcessCount() (n int) {
	for _, pid := range descendantPIDs() {
		b, _ := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/cmdline")
		if strings.Contains(string(b), "WebKitWebProcess") {
			n++
		}
	}
	return
}

func waitWebProcs(t *testing.T, want int) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if webProcessCount() == want {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("WebKitWebProcess count = %d, want %d", webProcessCount(), want)
}

// drain forgets the reports that arrived so far.
func (r *reportSink) drain() {
	for {
		select {
		case <-r.ch:
		default:
			r.pending = nil
			return
		}
	}
}

// Windows share one web process, release frees it, and Show brings the page
// back on its route with the current value of a host signal. The manifest
// sets window.keepAlive, so closing the last window releases it as well.
func TestGUIWindowsBackground(t *testing.T) {
	if guiCtl == nil {
		t.Skip("set KRATE_NATIVE_GUI=1 to run GUI tests")
	}
	if runtime.GOOS != "linux" {
		t.Skip("process counting reads /proc")
	}
	sink := guiSink
	waitWebProcs(t, 1)

	// Two more windows on the same app share the web process. The page opens
	// them through the SDK, as an app would.
	sink.drain()
	if err := guiCtl.Eval(`call("krate/window.open", [{path: "/?w=a"}]); call("krate/window.open", [{path: "/?w=b"}])`); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		sink.wait(t, "calls", nil)
	}
	waitWebProcs(t, 1)
	closeExtras := `(async () => { const l = (await call("krate/window.list")).ok; for (const id of l) if (id !== "main") await call("krate/window.close", [id]); })()`
	if err := guiCtl.Eval(closeExtras); err != nil {
		t.Fatal(err)
	}
	time.Sleep(500 * time.Millisecond)

	// Current value of the host signal, as the page sees it.
	sink.drain()
	if err := guiCtl.Eval("bump()"); err != nil {
		t.Fatal(err)
	}
	want := asInt(sink.wait(t, "sig", nil)["v"])

	// A route the page pushed itself must survive release and restore. The
	// window is driven through the platform interfaces here, the way host code
	// reacts to a tray click; the SDK wire path is covered in package sdk.
	if err := guiCtl.Eval(`history.pushState({}, "", "/?room=9")`); err != nil {
		t.Fatal(err)
	}
	main := guiCtl.wins.all()[0]
	rel, ok := main.(caps.WindowRelease)
	ctl, ok2 := main.(caps.WindowControls)
	if !ok || !ok2 {
		t.Fatalf("the platform window lacks release or controls: %T", main)
	}
	rel.Release()
	waitWebProcs(t, 0)

	sink.drain()
	ctl.Show()
	sink.wait(t, "sig", func(m map[string]any) bool { return asInt(m["v"]) == want })
	waitWebProcs(t, 1)
	got, err := guiCtl.Query(`location.pathname + location.search`)
	if err != nil || got != `"/?room=9"` {
		t.Errorf("route after restore = %s, %v", got, err)
	}

	// keepAlive (set in the manifest): closing the last window releases it;
	// the app keeps running and the window comes back.
	main.Close()
	waitWebProcs(t, 0)
	if !rel.Released() {
		t.Fatal("the last window was destroyed, not released")
	}
	sink.drain()
	ctl.Show()
	sink.wait(t, "sig", func(m map[string]any) bool { return asInt(m["v"]) == want })
	waitWebProcs(t, 1)
}
