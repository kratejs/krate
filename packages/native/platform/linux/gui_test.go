//go:build linux

package linux

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// The GUI tests need a display and the WebKitGTK libraries. They run only
// with KRATE_NATIVE_GUI=1 (see ~/wt/kn-sysroot/env.sh for a rootless setup).

var (
	guiApp   *App
	guiSrv   *testServer
	guiStart time.Time
)

func TestMain(m *testing.M) {
	if os.Getenv("KRATE_NATIVE_GUI") != "1" {
		os.Exit(m.Run())
	}
	guiStart = time.Now()
	guiSrv = newTestServer()
	app, err := New(Options{AppID: "dev.krate.guitest", Handler: guiSrv.mux, Debug: true, Memory: guiMemory()})
	if err != nil {
		fmt.Fprintln(os.Stderr, "GUI test setup:", err)
		os.Exit(1)
	}
	guiApp = app
	app.OnAllClosed(func() {})
	code := 0
	err = app.Run(func() {
		go func() {
			code = m.Run()
			app.Quit()
		}()
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		code = 1
	}
	os.Exit(code)
}

func needGUI(t *testing.T) {
	t.Helper()
	if guiApp == nil {
		t.Skip("set KRATE_NATIVE_GUI=1 to run GUI tests")
	}
}

type testServer struct {
	mux *http.ServeMux

	reports chan map[string]any
	marks   chan string
	seen    []string // only touched by the test goroutine

	mu          sync.Mutex
	streamTimes []int64
	hangStarted chan struct{}
	hangCancel  chan struct{}
	bigSum      [32]byte
}

const testPage = `<!doctype html><html><head><meta charset="utf-8"><title>t</title></head><body>caf&eacute;<script>
(async () => {
  const rep = {errors: []};
  const step = async (name, fn) => { try { await fn(); } catch (e) { rep.errors.push(name + ": " + e); } };
  await step("echo", async () => {
    const data = new Uint8Array(65536);
    for (let i = 0; i < data.length; i++) data[i] = (i * 31 + 7) & 255;
    const r = await fetch("/echo", {method: "POST", headers: {"x-test": "hello"}, body: data});
    const back = new Uint8Array(await r.arrayBuffer());
    rep.echoStatus = r.status;
    rep.echoLen = back.length;
    rep.echoOK = back.length === data.length && back.every((v, i) => v === data[i]);
    rep.echoHeader = r.headers.get("x-echo-test");
  });
  await step("stream", async () => {
    const r = await fetch("/stream");
    const rd = r.body.getReader();
    const dec = new TextDecoder();
    rep.chunks = []; rep.streamText = "";
    for (;;) {
      const {done, value} = await rd.read();
      if (done) break;
      rep.chunks.push(Date.now());
      rep.streamText += dec.decode(value);
    }
  });
  await step("big", async () => {
    const r = await fetch("/big");
    const buf = await r.arrayBuffer();
    rep.bigLen = buf.byteLength;
    const h = await crypto.subtle.digest("SHA-256", buf);
    rep.bigHash = Array.from(new Uint8Array(h)).map(b => b.toString(16).padStart(2, "0")).join("");
  });
  await step("env", async () => {
    rep.origin = location.origin;
    rep.secure = isSecureContext;
    rep.charset = document.characterSet;
    rep.subtle = typeof crypto.subtle.digest === "function";
    localStorage.setItem("k", "v");
    rep.local = localStorage.getItem("k");
    rep.ctype = (await fetch("/ctype")).headers.get("content-type");
    rep.notFound = (await fetch("/nope-404")).status;
    rep.post204 = (await fetch("/nocontent", {method: "POST"})).status;
  });
  await fetch("/report", {method: "POST", body: JSON.stringify(rep)});
})();
</script></body></html>`

func newTestServer() *testServer {
	s := &testServer{
		mux:         http.NewServeMux(),
		reports:     make(chan map[string]any, 8),
		marks:       make(chan string, 64),
		hangStarted: make(chan struct{}, 4),
		hangCancel:  make(chan struct{}, 4),
	}
	big := bigBody()
	s.bigSum = sha256.Sum256(big)
	s.mux.HandleFunc("/{$}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, testPage)
	})
	s.mux.HandleFunc("/blank", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, "<!doctype html><title>blank</title><p>%s</p>", r.URL.RawQuery)
	})
	s.mux.HandleFunc("/echo", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Echo-Test", r.Header.Get("X-Test"))
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = io.Copy(w, r.Body)
	})
	s.mux.HandleFunc("/stream", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		s.mu.Lock()
		s.streamTimes = nil
		s.mu.Unlock()
		for i := 0; i < 5; i++ {
			s.mu.Lock()
			s.streamTimes = append(s.streamTimes, time.Now().UnixMilli())
			s.mu.Unlock()
			fmt.Fprintf(w, "chunk-%d;", i)
			w.(http.Flusher).Flush()
			time.Sleep(50 * time.Millisecond)
		}
	})
	s.mux.HandleFunc("/big", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write(big)
	})
	s.mux.HandleFunc("/ctype", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, "{}")
	})
	s.mux.HandleFunc("/nocontent", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })
	s.mux.HandleFunc("/hang", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = io.WriteString(w, "first")
		w.(http.Flusher).Flush()
		s.hangStarted <- struct{}{}
		<-r.Context().Done()
		s.hangCancel <- struct{}{}
	})
	s.mux.HandleFunc("/mark", func(w http.ResponseWriter, r *http.Request) {
		s.marks <- r.URL.Query().Get("name")
		w.WriteHeader(204)
	})
	registerMemHandlers(s)
	registerViewHandlers(s)
	registerPermHandlers(s)
	registerPageHandlers(s)
	s.mux.HandleFunc("/report", func(w http.ResponseWriter, r *http.Request) {
		var rep map[string]any
		if err := json.NewDecoder(r.Body).Decode(&rep); err != nil {
			rep = map[string]any{"decodeError": err.Error()}
		}
		s.reports <- rep
		w.WriteHeader(204)
	})
	return s
}

func bigBody() []byte {
	b := make([]byte, 5<<20)
	x := uint32(1)
	for i := range b {
		x = x*1664525 + 1013904223
		b[i] = byte(x >> 24)
	}
	return b
}

// waitMark waits for a mark; marks seen earlier but not wanted stay available.
func (s *testServer) waitMark(t *testing.T, want string) {
	t.Helper()
	deadline := time.After(15 * time.Second)
	for {
		for i, m := range s.seen {
			if m == want {
				s.seen = append(s.seen[:i], s.seen[i+1:]...)
				return
			}
		}
		select {
		case got := <-s.marks:
			s.seen = append(s.seen, got)
		case <-deadline:
			t.Fatalf("timed out waiting for mark %q (seen %v)", want, s.seen)
		}
	}
}

func (s *testServer) waitReport(t *testing.T) map[string]any {
	t.Helper()
	select {
	case r := <-s.reports:
		return r
	case <-time.After(30 * time.Second):
		t.Fatal("timed out waiting for page report")
		return nil
	}
}

func rssMB(pid int) float64 {
	b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/status")
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(line, "VmRSS:") {
			f := strings.Fields(line)
			kb, _ := strconv.Atoi(f[1])
			return float64(kb) / 1024
		}
	}
	return 0
}

// childRSS sums RSS of direct children (WebKit's web and network processes).
func childRSS() (total float64, n int) {
	ents, _ := os.ReadDir("/proc")
	me := os.Getpid()
	for _, e := range ents {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		b, err := os.ReadFile("/proc/" + e.Name() + "/status")
		if err != nil {
			continue
		}
		if strings.Contains(string(b), "\nPPid:\t"+strconv.Itoa(me)+"\n") {
			total += rssMB(pid)
			n++
		}
	}
	return
}

func TestGUIPage(t *testing.T) {
	needGUI(t)
	t.Logf("WebKitGTK %s (%s)", guiApp.WebKitVersion(), guiApp.api.name)
	loaded := make(chan time.Time, 4)
	w, err := guiApp.NewWindowNotify(WindowOptions{Title: "page", Width: 800, Height: 600, Resizable: true, Path: "/"}, func() { loaded <- time.Now() })
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	select {
	case at := <-loaded:
		t.Logf("cold start (process start to first load event): %d ms", at.Sub(guiStart).Milliseconds())
	case <-time.After(30 * time.Second):
		t.Fatal("no load event")
	}
	rep := guiSrv.waitReport(t)
	b, _ := json.MarshalIndent(rep, "", " ")
	t.Logf("report: %s", b)

	if errs, _ := rep["errors"].([]any); len(errs) > 0 {
		t.Errorf("page errors: %v", errs)
	}
	if rep["echoOK"] != true || rep["echoLen"] != float64(65536) || rep["echoStatus"] != float64(200) {
		t.Errorf("echo failed: ok=%v len=%v status=%v", rep["echoOK"], rep["echoLen"], rep["echoStatus"])
	}
	if rep["echoHeader"] != "hello" {
		t.Errorf("request header not forwarded or response header lost: %v", rep["echoHeader"])
	}
	if rep["origin"] != "krate://app" {
		t.Errorf("origin = %v", rep["origin"])
	}
	if rep["secure"] != true || rep["subtle"] != true || rep["local"] != "v" {
		t.Errorf("secure context: secure=%v subtle=%v localStorage=%v", rep["secure"], rep["subtle"], rep["local"])
	}
	if rep["charset"] != "UTF-8" {
		t.Errorf("charset = %v", rep["charset"])
	}
	if rep["ctype"] != "application/json" {
		t.Errorf("content-type = %v", rep["ctype"])
	}
	if rep["notFound"] != float64(404) || rep["post204"] != float64(204) {
		t.Errorf("statuses: 404=%v 204=%v", rep["notFound"], rep["post204"])
	}
	sum := fmt.Sprintf("%x", guiSrv.bigSum)
	if rep["bigLen"] != float64(5<<20) || rep["bigHash"] != sum {
		t.Errorf("big download: len=%v hash=%v want %s", rep["bigLen"], rep["bigHash"], sum)
	}

	// Streaming: the first chunk must arrive before the last one is written.
	chunks, _ := rep["chunks"].([]any)
	guiSrv.mu.Lock()
	writes := append([]int64(nil), guiSrv.streamTimes...)
	guiSrv.mu.Unlock()
	if rep["streamText"] != "chunk-0;chunk-1;chunk-2;chunk-3;chunk-4;" {
		t.Errorf("stream text = %q", rep["streamText"])
	}
	if len(chunks) < 3 || len(writes) != 5 {
		t.Fatalf("stream arrived in %d reads (writes %d); not streamed", len(chunks), len(writes))
	}
	first := int64(chunks[0].(float64))
	last := int64(chunks[len(chunks)-1].(float64))
	t.Logf("stream: %d reads; first arrival %d ms after first write, last write at +%d ms, last arrival %d ms after last write",
		len(chunks), first-writes[0], writes[4]-writes[0], last-writes[4])
	if first >= writes[4] {
		t.Errorf("first chunk arrived (%d) after the last was written (%d): response was buffered", first, writes[4])
	}
	if last-first < 100 {
		t.Errorf("chunks arrived within %d ms of each other; expected spread over ~200 ms", last-first)
	}

	time.Sleep(500 * time.Millisecond)
	t.Logf("RSS: ui process %.0f MB, child processes %.0f MB (%d children)", rssMB(os.Getpid()), func() float64 { v, _ := childRSS(); return v }(), func() int { _, n := childRSS(); return n }())
}

func TestGUINavigationLock(t *testing.T) {
	needGUI(t)
	logs := make(chan string, 16)
	old := Logf
	Logf = func(format string, args ...any) { logs <- fmt.Sprintf(format, args...) }
	defer func() { Logf = old }()

	loaded := make(chan struct{}, 8)
	w, err := guiApp.NewWindowNotify(WindowOptions{Title: "lock", Width: 400, Height: 300, Resizable: true, Path: "/blank?lock"}, func() { loaded <- struct{}{} })
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	<-loaded
	w.Eval(`location.href = "https://example.invalid/away"`)
	select {
	case l := <-logs:
		if !strings.Contains(l, "https://example.invalid/away") {
			t.Errorf("unexpected log %q", l)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("no blocked-navigation log")
	}
	// The page must still be the app page.
	w.Eval(`fetch("/mark?name=" + encodeURIComponent(location.origin))`)
	guiSrv.waitMark(t, "krate://app")
}

func TestGUIRequestAbort(t *testing.T) {
	needGUI(t)
	loaded := make(chan struct{}, 8)
	w, err := guiApp.NewWindowNotify(WindowOptions{Title: "abort", Width: 400, Height: 300, Resizable: true, Path: "/blank?abort"}, func() { loaded <- struct{}{} })
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	<-loaded
	w.Eval(`window.ac = new AbortController(); fetch("/hang", {signal: ac.signal}).then(r => r.body.getReader().read()).catch(() => {})`)
	select {
	case <-guiSrv.hangStarted:
	case <-time.After(10 * time.Second):
		t.Fatal("handler did not start")
	}
	time.Sleep(200 * time.Millisecond)
	w.Eval(`ac.abort()`)
	select {
	case <-guiSrv.hangCancel:
	case <-time.After(10 * time.Second):
		t.Fatal("handler context was not cancelled after the page aborted the fetch")
	}
}

// TestGUIRequestBodyStress posts many request bodies at once. WebKit's body
// stream shares its FormData with the request on the UI side, and that object
// is not thread safe, so the stream must only be touched on the main thread;
// done elsewhere, this crashed the process every few hundred requests.
func TestGUIRequestBodyStress(t *testing.T) {
	needGUI(t)
	loaded := make(chan struct{}, 8)
	w, err := guiApp.NewWindowNotify(WindowOptions{Title: "stress", Width: 400, Height: 300, Resizable: true, Path: "/blank?stress"}, func() { loaded <- struct{}{} })
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	<-loaded
	w.Eval(`(async () => {
  const sizes = [0, 1, 17, 4096, 20000, 65536, 300000];
  let bad = 0;
  for (let round = 0; round < 6; round++) {
    await Promise.all(Array.from({length: 80}, async (_, i) => {
      const n = sizes[(i + round) % sizes.length];
      const data = new Uint8Array(n);
      for (let j = 0; j < n; j++) data[j] = (j * 7 + i) & 255;
      const r = await fetch("/echo", {method: "POST", body: data});
      const back = new Uint8Array(await r.arrayBuffer());
      if (back.length !== n) { bad++; return; }
      for (let j = 0; j < n; j += 997) if (back[j] !== data[j]) { bad++; return; }
    }));
  }
  fetch("/mark?name=" + (bad ? "stress-bad-" + bad : "stress-ok"));
})()`)
	guiSrv.waitMark(t, "stress-ok")
	if bodyOps.Load() == 0 {
		t.Error("no request body stream was read")
	}
	if n := bodyOffMain.Load(); n != 0 {
		t.Errorf("%d request body stream operations ran off the main thread", n)
	}
}
