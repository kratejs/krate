//go:build windows

package windows

import (
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

// The GUI tests need a display, the WebView2 Runtime and the main thread, so
// they run only with KRATE_NATIVE_GUI=1. TestMain owns the main thread: it
// runs the app loop there and runs the tests from onReady.

var (
	guiApp     *App
	guiEnabled = os.Getenv("KRATE_NATIVE_GUI") == "1"
	reports    = make(chan report, 8)
)

type report struct {
	Window     string    `json:"window"`
	Origin     string    `json:"origin"`
	Subtle     bool      `json:"subtle"`
	SubtleSum  string    `json:"subtleSum"`
	EchoOK     bool      `json:"echoOK"`
	EchoBytes  int       `json:"echoBytes"`
	StreamOK   bool      `json:"streamOK"`
	StreamAt   []float64 `json:"streamAt"`
	Error      string    `json:"error"`
	UserAgent  string    `json:"userAgent"`
	BlockedNav bool      `json:"blockedNav"`
}

const echoSize = 64 << 10

func guiHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, guiPage)
	})
	mux.HandleFunc("/echo", func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("X-Echo-Len", fmt.Sprint(len(b)))
		_, _ = w.Write(b)
	})
	mux.HandleFunc("/__krate/stream/gui", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		fl := w.(http.Flusher)
		for i := 0; i < 5; i++ {
			fmt.Fprintf(w, "chunk-%d\n", i)
			fl.Flush()
			select {
			case <-time.After(50 * time.Millisecond):
			case <-r.Context().Done():
				return
			}
		}
	})
	mux.HandleFunc("/report", func(w http.ResponseWriter, r *http.Request) {
		var rep report
		if err := json.NewDecoder(r.Body).Decode(&rep); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		reports <- rep
		w.WriteHeader(http.StatusNoContent)
	})
	return mux
}

func TestMain(m *testing.M) {
	if os.Getenv(toastHelperEnv) != "" {
		toastHelperMain()
	}
	if os.Getenv(runtimeHelperEnv) != "" {
		runtimeHelperMain()
	}
	if !guiEnabled {
		os.Exit(m.Run())
	}
	// Fake camera and microphone for the permission tests: a CI runner has none.
	if os.Getenv("WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS") == "" {
		os.Setenv("WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS", "--use-fake-device-for-media-stream")
	}
	app, err := New(Config{AppID: "dev.krate.native.guitest", AppName: "Krate GUI Test", Handler: withProcPages(withPermPages(withViewPing(guiHandler()))), Debug: true, KeepAlive: true, Extensions: true})
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

func needGUI(t *testing.T) {
	if !guiEnabled {
		t.Skip("set KRATE_NATIVE_GUI=1 to run the WebView2 tests")
	}
}

func waitReport(t *testing.T, want string) report {
	t.Helper()
	deadline := time.After(30 * time.Second)
	for {
		select {
		case r := <-reports:
			if r.Window == want {
				return r
			}
			reports <- r // not ours; put back for another test
			time.Sleep(10 * time.Millisecond)
		case <-deadline:
			t.Fatalf("no report from window %q within 30s", want)
		}
	}
}

func checkReport(t *testing.T, r report) {
	t.Helper()
	if r.Error != "" {
		t.Errorf("page error: %s", r.Error)
	}
	if r.Origin != "https://app.localhost" {
		t.Errorf("origin = %q", r.Origin)
	}
	if !r.Subtle || len(r.SubtleSum) != 64 {
		t.Errorf("crypto.subtle unavailable or broken: %+v", r)
	}
	if !r.EchoOK || r.EchoBytes != echoSize {
		t.Errorf("64 KiB POST echo failed: ok=%v bytes=%d", r.EchoOK, r.EchoBytes)
	}
	if !r.StreamOK {
		t.Errorf("streamed response was not received as 5 chunks: %v", r.StreamAt)
	}
	// 5 chunks 50 ms apart: the first must arrive well before the last.
	if n := len(r.StreamAt); n == 5 && r.StreamAt[4]-r.StreamAt[0] < 120 {
		t.Errorf("stream arrived all at once (buffered): %v", r.StreamAt)
	}
	if !r.BlockedNav {
		t.Errorf("navigation to another origin was not blocked")
	}
}

func TestGUIRoundTrip(t *testing.T) {
	needGUI(t)
	w, err := guiApp.NewWindow(WindowConfig{Title: "krate gui test", Width: 640, Height: 480, Resizable: true, Path: "/?w=main"})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	checkReport(t, waitReport(t, "main"))
	w.SetTitle("renamed")
	w.SetSize(500, 400)
	w.Eval("document.title = 'evaluated'")
}

func TestGUIDispatchFromManyGoroutines(t *testing.T) {
	needGUI(t)
	var n atomic.Int32
	var onMain atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 1000; i++ {
		wg.Add(1)
		go func() {
			guiApp.Dispatch(func() {
				n.Add(1)
				if guiApp.onMain() {
					onMain.Add(1)
				}
				wg.Done()
			})
		}()
	}
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(20 * time.Second):
		t.Fatalf("only %d of 1000 dispatched functions ran", n.Load())
	}
	if n.Load() != 1000 || onMain.Load() != 1000 {
		t.Fatalf("ran %d, on main thread %d", n.Load(), onMain.Load())
	}
}

func TestGUITwoWindows(t *testing.T) {
	needGUI(t)
	w1, err := guiApp.NewWindow(WindowConfig{Title: "one", Width: 480, Height: 360, Path: "/?w=one"})
	if err != nil {
		t.Fatal(err)
	}
	w2, err := guiApp.NewWindow(WindowConfig{Title: "two", Width: 480, Height: 360, Resizable: true, Path: "/?w=two"})
	if err != nil {
		t.Fatal(err)
	}
	checkReport(t, waitReport(t, "one"))
	checkReport(t, waitReport(t, "two"))
	w1.Close()
	time.Sleep(200 * time.Millisecond)
	w2.Eval("1+1") // the other window keeps working
	w2.Close()
}

const guiPage = `<!doctype html><meta charset=utf-8><title>krate gui test</title>
<body>running...<script>
(async () => {
  const rep = {window: new URLSearchParams(location.search).get('w') || '', origin: location.origin,
    subtle: false, subtleSum: '', echoOK: false, echoBytes: 0, streamOK: false, streamAt: [], error: '',
    userAgent: navigator.userAgent, blockedNav: false};
  try {
    rep.subtle = !!(window.crypto && crypto.subtle);
    const data = new Uint8Array(65536);
    for (let i = 0; i < data.length; i++) data[i] = (i * 31 + 7) & 255;
    const res = await fetch('/echo', {method: 'POST', body: data, headers: {'Content-Type': 'application/octet-stream'}});
    const back = new Uint8Array(await res.arrayBuffer());
    rep.echoBytes = back.length;
    rep.echoOK = back.length === data.length && back.every((v, i) => v === data[i]);
    if (rep.subtle) {
      const d = new Uint8Array(await crypto.subtle.digest('SHA-256', back));
      rep.subtleSum = Array.from(d, b => b.toString(16).padStart(2, '0')).join('');
    }
    const sres = await fetch("/__krate/stream/gui");
    const reader = sres.body.getReader();
    const t0 = performance.now();
    let text = '';
    while (true) {
      const {value, done} = await reader.read();
      if (done) break;
      rep.streamAt.push(Math.round(performance.now() - t0));
      text += new TextDecoder().decode(value);
    }
    rep.streamOK = text === 'chunk-0\nchunk-1\nchunk-2\nchunk-3\nchunk-4\n' && rep.streamAt.length === 5;
    // Top-level navigation to another origin must be cancelled: the page stays.
    const before = location.href;
    location.href = 'https://example.com/';
    await new Promise(r => setTimeout(r, 1500));
    rep.blockedNav = location.href === before;
  } catch (e) {
    rep.error = String(e);
  }
  await fetch('/report', {method: 'POST', body: JSON.stringify(rep)});
})();
</script>`
