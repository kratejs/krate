//go:build darwin

package darwin

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// GUI smoke test; see MANUAL_TEST.md. It needs a logged-in macOS session and
// runs only with KRATE_NATIVE_GUI=1. The test binary's main goroutine is
// pinned to the main thread by this package's init, so TestMain runs the
// AppKit loop there and executes the tests on another goroutine.

const guiPage = `<!doctype html><meta charset="utf-8"><title>krate gui test</title>
<body>running</body>
<script>
(async () => {
  const out = {id: location.search, origin: location.origin, secure: window.isSecureContext,
               subtle: !!(window.crypto && window.crypto.subtle), errors: []};
  try {
    const body = new Uint8Array(64 * 1024);
    for (let i = 0; i < body.length; i++) body[i] = (i * 7 + 3) & 255;
    const r = await fetch('/echo', {method: 'POST', body});
    const back = new Uint8Array(await r.arrayBuffer());
    out.echoLen = back.length;
    out.echoOK = back.length === body.length && back.every((v, i) => v === body[i]);
  } catch (e) { out.errors.push('echo: ' + e); }
  try {
    const t0 = performance.now();
    const r = await fetch('/stream');
    const rd = r.body.getReader();
    const dec = new TextDecoder();
    let text = '', times = [];
    for (;;) {
      const {done, value} = await rd.read();
      if (done) break;
      times.push(performance.now() - t0);
      text += dec.decode(value, {stream: true});
    }
    out.stream = text;
    out.reads = times.length;
    out.spanMs = times.length ? times[times.length - 1] - times[0] : 0;
  } catch (e) { out.errors.push('stream: ' + e); }
  try {
    if (window.crypto && window.crypto.subtle) {
      const d = await crypto.subtle.digest('SHA-256', new TextEncoder().encode('krate'));
      out.digestLen = d.byteLength;
    }
  } catch (e) { out.errors.push('subtle: ' + e); }
  await fetch('/report', {method: 'POST', body: JSON.stringify(out)});
})();
</script>`

type pageReport struct {
	ID        string   `json:"id"`
	Origin    string   `json:"origin"`
	Secure    bool     `json:"secure"`
	Subtle    bool     `json:"subtle"`
	DigestLen int      `json:"digestLen"`
	EchoOK    bool     `json:"echoOK"`
	EchoLen   int      `json:"echoLen"`
	Stream    string   `json:"stream"`
	Reads     int      `json:"reads"`
	SpanMs    float64  `json:"spanMs"`
	Errors    []string `json:"errors"`
}

var (
	guiApp   *App
	reports  = make(chan pageReport, 16)
	echoSeen atomic.Int64
)

func guiHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/{$}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, guiPage)
	})
	mux.HandleFunc("/echo", func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		echoSeen.Store(int64(len(b)))
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write(b)
	})
	mux.HandleFunc("/stream", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		fl := w.(http.Flusher)
		for i := 0; i < 5; i++ {
			if i > 0 {
				select {
				case <-time.After(50 * time.Millisecond):
				case <-r.Context().Done():
					return
				}
			}
			fmt.Fprintf(w, "chunk%d\n", i)
			fl.Flush()
		}
	})
	mux.HandleFunc("/report", func(w http.ResponseWriter, r *http.Request) {
		var rep pageReport
		b, _ := io.ReadAll(r.Body)
		if err := json.NewDecoder(bytes.NewReader(b)).Decode(&rep); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		reports <- rep
		w.WriteHeader(204)
	})
	return mux
}

func TestMain(m *testing.M) {
	if os.Getenv("KRATE_NATIVE_GUI") != "1" {
		os.Exit(m.Run()) // GUI tests skip themselves; unit tests still run
	}
	app, err := New(Options{AppID: "dev.krate.guitest", Handler: withViewPing(withPageHandlers(guiHandler())), Debug: true, KeepAlive: true})
	if err != nil {
		fmt.Fprintln(os.Stderr, "New:", err)
		os.Exit(2)
	}
	guiApp = app
	code := 0
	err = app.Run(func() {
		go func() {
			code = m.Run()
			app.Quit()
		}()
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "Run:", err)
		os.Exit(2)
	}
	os.Exit(code)
}

func requireGUI(t *testing.T) {
	t.Helper()
	if guiApp == nil {
		t.Skip("set KRATE_NATIVE_GUI=1 to run GUI tests")
	}
}

func waitReport(t *testing.T) pageReport {
	t.Helper()
	select {
	case r := <-reports:
		return r
	case <-time.After(30 * time.Second):
		t.Fatal("no report from the page within 30s")
		return pageReport{}
	}
}

func checkReport(t *testing.T, r pageReport) {
	t.Helper()
	if len(r.Errors) > 0 {
		t.Errorf("page errors: %v", r.Errors)
	}
	if r.Origin != OriginURL {
		t.Errorf("origin = %q, want %q", r.Origin, OriginURL)
	}
	if !r.Subtle || r.DigestLen != 32 {
		t.Errorf("crypto.subtle unavailable (secure context = %v, digest len %d)", r.Secure, r.DigestLen)
	}
	if !r.EchoOK {
		t.Errorf("64 KiB POST echo mismatch (echo len %d, server saw %d bytes)", r.EchoLen, echoSeen.Load())
	}
	if want := "chunk0\nchunk1\nchunk2\nchunk3\nchunk4\n"; r.Stream != want {
		t.Errorf("stream = %q", r.Stream)
	}
	// The server spaces the chunks over 200 ms; a buffered response arrives as
	// one read. WebKit delivers in batches, so the page's span varies (145 ms
	// was seen on CI): half the server's span still proves streaming.
	if r.Reads < 3 || r.SpanMs < 100 {
		t.Errorf("stream not incremental: %d reads spanning %.0f ms (want >= 3 reads over >= 100 ms)", r.Reads, r.SpanMs)
	}
}

func TestScenario(t *testing.T) {
	requireGUI(t)
	w, err := guiApp.NewWindow(WindowOptions{Title: "krate gui test", Width: 640, Height: 480, Resizable: true, Path: "/?w=1"})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	r := waitReport(t)
	checkReport(t, r)
	w.SetTitle("renamed")
	w.SetSize(500, 400)
	w.Eval("document.body.textContent = 'eval ok'")
}

func TestDispatchFromManyGoroutines(t *testing.T) {
	requireGUI(t)
	const n = 1000
	var ran, offMain atomic.Int64
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			guiApp.Dispatch(func() {
				defer wg.Done()
				ran.Add(1)
				if !isMainThread() {
					offMain.Add(1)
				}
			})
		}()
	}
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(20 * time.Second):
		t.Fatalf("only %d of %d dispatches ran", ran.Load(), n)
	}
	if offMain.Load() != 0 {
		t.Errorf("%d dispatches ran off the main thread", offMain.Load())
	}
	// Work queued by earlier tests (a window closing) may still be in flight;
	// a real leak never drains.
	for end := time.Now().Add(2 * time.Second); mainWork.len() != 0 && time.Now().Before(end); {
		time.Sleep(10 * time.Millisecond)
	}
	if mainWork.len() != 0 {
		t.Errorf("work queue leaked %d entries", mainWork.len())
	}
}
