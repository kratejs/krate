package shell

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/kratejs/krate/packages/native/internal/gohosttest"
	"github.com/kratejs/krate/packages/native/memstat"
	"github.com/kratejs/krate/packages/native/plugin"
)

// The GUI test needs a display and the WebKitGTK libraries on Linux. It runs
// only with KRATE_NATIVE_GUI=1 (see ~/wt/kn-sysroot/env.sh for a rootless
// setup). TestMain then runs the whole shell on the main goroutine, as the
// real binary does, and the tests drive it through its Control.

var (
	guiCtl  *Control
	guiSink *reportSink
	guiApp  *testApp
)

func TestMain(m *testing.M) {
	if os.Getenv("KRATE_NATIVE_GUI") != "1" {
		os.Exit(m.Run())
	}
	root, cleanup, err := gohosttest.TempDir("shellgui-")
	if err != nil {
		fmt.Fprintln(os.Stderr, "GUI test setup:", err)
		os.Exit(1)
	}
	guiSink = newReportSink()
	guiApp, err = buildTestApp(root, guiPage, true, 10)
	if err != nil {
		cleanup()
		fmt.Fprintln(os.Stderr, "GUI test setup:", err)
		os.Exit(1)
	}
	if err := enableGUIPlugin(guiApp.dir); err != nil {
		cleanup()
		fmt.Fprintln(os.Stderr, "GUI test setup:", err)
		os.Exit(1)
	}
	code := make(chan int, 1)
	err = Run(guiApp.dir, RunOptions{
		Dev:          true,
		Plugins:      []*plugin.Plugin{newGUIPlugin()},
		ExtraHandler: guiSink.handle,
		OnReady: func(ctl *Control) {
			guiCtl = ctl
			go func() {
				c := m.Run()
				ctl.Quit()
				code <- c
			}()
		},
	})
	cleanup()
	if err != nil {
		fmt.Fprintln(os.Stderr, "GUI test:", err)
		os.Exit(1)
	}
	select {
	case c := <-code:
		os.Exit(c)
	case <-time.After(10 * time.Second):
		os.Exit(1)
	}
}

func asInt(v any) int {
	f, _ := v.(float64)
	return int(f)
}

func TestGUIShell(t *testing.T) {
	if guiCtl == nil {
		t.Skip("set KRATE_NATIVE_GUI=1 to run GUI tests")
	}
	sink := guiSink

	// 1. The page loads, and calls reach host JS and the Go host child.
	rep := sink.wait(t, "calls", nil)
	if errs, _ := rep["errors"].([]any); len(errs) > 0 {
		t.Fatalf("page errors: %v", errs)
	}
	loadID := rep["loadId"]
	if loadID == nil || loadID == "" {
		t.Fatal("report has no loadId")
	}
	if asInt(rep["hostAdd"]) != 5 {
		t.Errorf("hostjs h.add(2,3) = %v", rep["hostAdd"])
	}
	if asInt(rep["goAdd"]) != 42 {
		t.Errorf("go t.Add(20,22) = %v", rep["goAdd"])
	}
	if rep["goVersion"] != "v10" {
		t.Errorf("go t.Version = %v", rep["goVersion"])
	}
	if got := fmt.Sprint(rep["rev"]); got != "[3 2 1]" {
		t.Errorf("go t.Rev([1 2 3]) = %v", got)
	}
	if rep["origin"] != guiCtl.Origin() {
		t.Errorf("origin = %v, want %v", rep["origin"], guiCtl.Origin())
	}

	// 2. A Go signal reaches the page, and updates.
	sig := func(want int, what string) {
		t.Helper()
		m := sink.wait(t, "sig", func(m map[string]any) bool { return asInt(m["v"]) == want })
		if m["loadId"] != loadID {
			t.Errorf("%s: the window reloaded (loadId %v, was %v)", what, m["loadId"], loadID)
		}
	}
	sig(10, "initial signal")
	if err := guiCtl.Eval("bump()"); err != nil {
		t.Fatal(err)
	}
	sig(11, "bumped signal")

	// 3. Replace the Go host binary and restart only the child.
	if _, err := guiApp.buildGoHost(500); err != nil {
		t.Fatal(err)
	}
	if err := guiCtl.RestartGoHost(); err != nil {
		t.Fatal(err)
	}
	sig(500, "signal after Go host restart")
	if err := guiCtl.Eval("recall()"); err != nil {
		t.Fatal(err)
	}
	rc := sink.wait(t, "recall", nil)
	if rc["goVersion"] != "v500" || rc["loadId"] != loadID {
		t.Errorf("recall after restart: %v", rc)
	}
	if rc["hostTag"] != "host-v1" {
		t.Errorf("host tag = %v", rc["hostTag"])
	}
	if err := guiCtl.Eval("bump()"); err != nil {
		t.Fatal(err)
	}
	sig(501, "bump after restart")

	// 4. Reload the host JS in place.
	if err := os.WriteFile(guiApp.dir+"/host/host.js", []byte(hostJSv2), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := guiCtl.ReloadHostJS(); err != nil {
		t.Fatal(err)
	}
	if err := guiCtl.Eval("recall()"); err != nil {
		t.Fatal(err)
	}
	rc = sink.wait(t, "recall", nil)
	if rc["hostTag"] != "host-v2" || asInt(rc["hostAdd"]) != 1005 || rc["loadId"] != loadID {
		t.Errorf("recall after reload: %v", rc)
	}

	// The index page was fetched exactly once: the window never reloaded.
	if n := sink.indexLoads(); n != 1 {
		t.Errorf("index.html served %d times, want 1", n)
	}
	if t.Failed() {
		return
	}
	if strings.TrimSpace(fmt.Sprint(rc["error"])) != "<nil>" {
		t.Errorf("page error: %v", rc["error"])
	}
}

// TestGUIMemory checks the memory report of the running window: the shell, the
// Go host child and the WebKit processes, over the control API and over the
// dev endpoint the page itself can fetch. It reads the app TestGUIShell leaves
// running (the Go child is restarted there, so only roles are compared).
func TestGUIMemory(t *testing.T) {
	if guiCtl == nil {
		t.Skip("set KRATE_NATIVE_GUI=1 to run GUI tests")
	}
	skipNoMemstat(t)
	rep, err := guiCtl.Memory()
	if err != nil {
		t.Fatalf("Memory: %v", err)
	}
	roles := memRoles(rep)
	want := []string{memstat.RoleShell, memstat.RoleGoHost}
	if runtime.GOOS == "linux" {
		want = append(want, memstat.RoleWebviewWeb, memstat.RoleWebviewNetwork)
	}
	for _, r := range want {
		if roles[r].PSS == 0 {
			t.Errorf("role %s missing or empty: %+v", r, rep.ByRole())
		}
	}
	if t.Failed() {
		t.Logf("notes: %v", rep.Notes)
		return
	}
	t.Logf("roles: %+v", rep.ByRole())

	out, err := guiCtl.Query(`fetch("/__krate/dev/memory").then(r => r.json())`)
	if err != nil {
		t.Fatalf("fetching the endpoint from the page: %v", err)
	}
	var got memstat.Report
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatal(err)
	}
	r := memRoles(&got)
	for _, role := range want {
		if r[role].PSS == 0 {
			t.Errorf("endpoint: role %s missing: %+v", role, got.ByRole())
		}
	}
}

const guiPage = `<!doctype html><html><head><meta charset="utf-8"><title>shell</title></head><body>shell<script>
const loadId = Math.random().toString(36).slice(2);
const te = new TextEncoder(), td = new TextDecoder();
function frame(obj, blobs) {
  blobs = blobs || [];
  const j = te.encode(JSON.stringify(obj));
  let size = 4 + j.length + 2;
  for (const b of blobs) size += 4 + b.length;
  const out = new Uint8Array(size), dv = new DataView(out.buffer);
  dv.setUint32(0, j.length); out.set(j, 4);
  let p = 4 + j.length;
  dv.setUint16(p, blobs.length); p += 2;
  for (const b of blobs) { dv.setUint32(p, b.length); p += 4; out.set(b, p); p += b.length; }
  return out;
}
async function* frames(resp) {
  const rd = resp.body.getReader();
  let buf = new Uint8Array(0);
  const need = async (n) => {
    while (buf.length < n) {
      const r = await rd.read();
      if (r.done) return false;
      const nb = new Uint8Array(buf.length + r.value.length);
      nb.set(buf); nb.set(r.value, buf.length); buf = nb;
    }
    return true;
  };
  for (;;) {
    if (!(await need(4))) return;
    const n = new DataView(buf.buffer, buf.byteOffset).getUint32(0);
    if (!(await need(4 + n + 2))) return;
    const k = new DataView(buf.buffer, buf.byteOffset).getUint16(4 + n);
    let p = 4 + n + 2;
    const blobs = [];
    for (let i = 0; i < k; i++) {
      if (!(await need(p + 4))) return;
      const m = new DataView(buf.buffer, buf.byteOffset).getUint32(p); p += 4;
      if (!(await need(p + m))) return;
      blobs.push(buf.slice(p, p + m)); p += m;
    }
    const json = JSON.parse(td.decode(buf.slice(4, 4 + n)));
    buf = buf.slice(p);
    yield {json, blobs};
  }
}
async function call(fn, args, blobs) {
  const r = await fetch("/__krate/call", {
    method: "POST",
    headers: {"x-krate-window": "w-" + loadId},
    body: frame({calls: [{id: 1, fn, args: args || []}]}, blobs),
  });
  for await (const f of frames(r)) {
    if (f.json.err) throw new Error(fn + ": " + JSON.stringify(f.json.err));
    return {ok: f.json.ok, blobs: f.blobs};
  }
  throw new Error(fn + ": no result");
}
function report(kind, data) {
  return fetch("/__test/report", {method: "POST", body: JSON.stringify(Object.assign({kind, loadId, origin: location.origin}, data))});
}
async function recall() {
  const rep = {};
  try {
    rep.hostAdd = (await call("h.add", [2, 3])).ok;
    rep.hostTag = (await call("h.tag")).ok;
    rep.goVersion = (await call("t.Version")).ok;
  } catch (e) { rep.error = String(e); }
  await report("recall", rep);
}
function bump() { return call("t.Bump"); }
// The plugin flow: call a plugin op from this window, then open a window whose
// scope is empty (scoped.html) and let it try the same call.
async function pluginFlow() {
  const rep = {};
  try {
    rep.hello = (await call("plugin/gt.hello")).ok;
    rep.state = (await call("plugin/gt.state")).ok;
    rep.opened = (await call("krate/window.open", [{path: "/scoped.html", permissions: []}])).ok;
  } catch (e) { rep.error = String(e); }
  await report("plugin", rep);
}
(async () => {
  const rep = {errors: []};
  try {
    rep.hostAdd = (await call("h.add", [2, 3])).ok;
    rep.goAdd = (await call("t.Add", [20, 22])).ok;
    const rev = await call("t.Rev", [{"$b": 0}], [new Uint8Array([1, 2, 3])]);
    rep.rev = Array.from(rev.blobs[rev.ok["$b"]]);
    rep.goVersion = (await call("t.Version")).ok;
  } catch (e) { rep.errors.push(String(e)); }
  await report("calls", rep);
  const r = await fetch("/__krate/signals?names=t.Count");
  for await (const f of frames(r)) {
    if ("v" in f.json) await report("sig", {v: f.json.v});
  }
})();
</script></body></html>`

// TestGUIZMemoryPanel runs the real dev overlay bundle in the window, opens
// its Memory panel and reads the numbers it renders from the live
// /__krate/dev/memory endpoint. It runs last because it leaves the overlay in
// the page. The bundle comes from packages/runtime/dist (pnpm build).
func TestGUIZMemoryPanel(t *testing.T) {
	if guiCtl == nil {
		t.Skip("set KRATE_NATIVE_GUI=1 to run GUI tests")
	}
	skipNoMemstat(t)
	bundle, err := os.ReadFile(filepath.Join("..", "..", "runtime", "dist", "krate-dev.js"))
	if err != nil {
		if os.Getenv("KRATE_REQUIRE_E2E") == "1" {
			t.Fatalf("the dev overlay bundle is not built: %v", err)
		}
		t.Skipf("the dev overlay bundle is not built: %v", err)
	}
	if !strings.Contains(string(bundle), "mempanel") {
		t.Fatal("the dev overlay bundle predates the Memory panel: rebuild @krate/runtime")
	}
	src, _ := json.Marshal(string(bundle))
	// Install the overlay, open the panel, wait for the first sample.
	expr := fmt.Sprintf(`(async () => {
  const el = document.createElement("script");
  el.textContent = %s;
  document.head.appendChild(el);
  if (!document.getElementById("krate-dev-root")) return {err: "the overlay did not mount"};
  const root = document.getElementById("krate-dev-root").shadowRoot;
  root.querySelector(".tb-btn.mem").click();
  for (let i = 0; i < 50; i++) {
    const t = root.querySelector(".mempanel").textContent;
    if (/PSS/.test(t)) break;
    await new Promise(r => setTimeout(r, 100));
  }
  const panel = root.querySelector(".mempanel");
  const rows = Array.from(panel.querySelectorAll(".memrow")).map(r => r.textContent);
  return {hidden: panel.hidden, rows, heap: typeof (performance.memory || {}).usedJSHeapSize};
})()`, src)
	out, err := guiCtl.Query(expr)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	var got struct {
		Err    string   `json:"err"`
		Hidden bool     `json:"hidden"`
		Rows   []string `json:"rows"`
		Heap   string   `json:"heap"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if got.Err != "" {
		t.Fatalf("installing the overlay: %s", got.Err)
	}
	if got.Hidden {
		t.Fatal("the Memory panel did not open")
	}
	joined := strings.Join(got.Rows, "\n")
	t.Logf("panel rows:\n%s", joined)
	for _, want := range []string{"shell (", "PSS ", "private dirty ", "total", "DOM nodes"} {
		if !strings.Contains(joined, want) {
			t.Errorf("panel lacks %q:\n%s", want, joined)
		}
	}
	if runtime.GOOS == "linux" && !strings.Contains(joined, "webview:web (") {
		t.Errorf("panel lacks the web process row:\n%s", joined)
	}
	if got.Heap == "number" {
		if !strings.Contains(joined, "JS heap") || strings.Contains(joined, "unavailable in this engine") {
			t.Errorf("the engine exposes the heap but the panel does not show it:\n%s", joined)
		}
	} else if !strings.Contains(joined, "unavailable in this engine") {
		t.Errorf("without performance.memory the panel must say the JS heap is unavailable:\n%s", joined)
	}
}
