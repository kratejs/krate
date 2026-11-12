//go:build windows

package windows

import (
	"bytes"
	"image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kratejs/krate/packages/native/platform/caps"
)

// GUI tests of the per-view web contents controls: zoom, find, capture, PDF
// output, downloads and cookies. They run with the other GUI tests
// (KRATE_NATIVE_GUI=1) in the CI native job on windows-latest.
//
// The print dialog is not opened here: a system dialog cannot be dismissed
// from a test and would hang the job. KRATE_NATIVE_PRINT_DIALOG=1 opens it for
// a manual check (see MANUAL_TEST.md).

var pgLoaded = make(chan string, 64)

// withPageHandlers wraps h with the pages of these tests.
func withPageHandlers(h http.Handler) http.Handler {
	page := func(body string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = io.WriteString(w, `<!doctype html><meta charset="utf-8"><title>pg</title>`+body+
				`<script>fetch("/pgloaded?p="+encodeURIComponent(location.pathname))</script>`)
		}
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/pgloaded", func(w http.ResponseWriter, r *http.Request) {
		select {
		case pgLoaded <- r.URL.Query().Get("p"):
		default:
		}
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("/pgfind", page(`<body style="margin:8px"><p>foo bar Foo baz foo</p><p>nothing else here</p></body>`))
	mux.HandleFunc("/pgcolor", page(`<body style="margin:0;background:#ff0000;color:#fff"><h1>capture me</h1></body>`))
	mux.HandleFunc("/pgprint", page(`<body style="background:#eee"><h1>Print me</h1><p>Some text for the page.</p></body>`))
	mux.HandleFunc("/pgdl", page(`<body>
<a id="a" download="hello.txt" href="/pgfile">file</a>
<a id="slow" download="slow.bin" href="/pgslow">slow</a>
<a id="att" href="/pgatt">attachment</a>
<a id="blob" download="blob.txt">blob</a>
<script>var u=URL.createObjectURL(new Blob(["blob body"],{type:"text/plain"}));document.getElementById("blob").href=u;</script></body>`))
	mux.HandleFunc("/pgfile", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Length", "14")
		_, _ = io.WriteString(w, "hello download")
	})
	mux.HandleFunc("/pgatt", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", `attachment; filename="att.bin"`)
		_, _ = io.WriteString(w, "attachment body")
	})
	// The app origin is served through WebResourceRequested, which hands the
	// webview the whole body only once the handler returns, so a download
	// cannot be made slow by pacing the writes. A large body gives the cancel
	// a window instead.
	const bigSize = 128 << 20
	mux.HandleFunc("/pgslow", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Length", strconv.Itoa(bigSize))
		chunk := make([]byte, 1<<20)
		for i := 0; i < bigSize/len(chunk); i++ {
			if _, err := w.Write(chunk); err != nil {
				return
			}
		}
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/pg") {
			mux.ServeHTTP(w, r)
			return
		}
		h.ServeHTTP(w, r)
	})
}

// openPage opens a window on path and waits until the page has loaded.
func openPage(t *testing.T, path string) (*Window, caps.ViewRef) {
	t.Helper()
	for len(pgLoaded) > 0 {
		<-pgLoaded
	}
	w, err := guiApp.NewWindow(WindowConfig{Title: path, Width: 480, Height: 320, Resizable: true, Path: path})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { w.Close() })
	waitLoaded(t, strings.SplitN(path, "?", 2)[0])
	return w, w.PrimaryView()
}

func waitLoaded(t *testing.T, path string) {
	t.Helper()
	deadline := time.After(30 * time.Second)
	for {
		select {
		case p := <-pgLoaded:
			if p == path {
				return
			}
		case <-deadline:
			t.Fatalf("page %s did not load", path)
		}
	}
}

func TestGUIPageZoom(t *testing.T) {
	needGUI(t)
	w, v := openPage(t, "/pgfind")
	if z, err := w.ZoomFactor(v); err != nil || z < 0.99 || z > 1.01 {
		t.Fatalf("initial zoom %v, %v", z, err)
	}
	if err := w.SetZoomFactor(v, 1.5); err != nil {
		t.Fatal(err)
	}
	if z, _ := w.ZoomFactor(v); z < 1.49 || z > 1.51 {
		t.Fatalf("zoom after set: %v", z)
	}
	if err := w.SetZoomFactor(v, 9); err == nil {
		t.Fatal("zoom 9 accepted")
	}
	if err := w.SetZoomFactor(v, 0.1); err == nil {
		t.Fatal("zoom 0.1 accepted")
	}
	extra, err := w.AddView(caps.ViewOptions{Path: "/pgfind"})
	if err != nil {
		t.Fatal(err)
	}
	waitLoaded(t, "/pgfind")
	if err := w.SetZoomFactor(extra, 2); err != nil {
		t.Fatal(err)
	}
	if z, _ := w.ZoomFactor(v); z < 1.49 || z > 1.51 {
		t.Fatalf("primary zoom changed to %v by the other view", z)
	}
	if z, _ := w.ZoomFactor(extra); z < 1.99 || z > 2.01 {
		t.Fatalf("extra zoom %v", z)
	}
	if _, err := w.ZoomFactor(999999); err == nil {
		t.Fatal("zoom of a missing view succeeded")
	}
	zoomShortcuts(t, w, v)
}

func collectFinds(t *testing.T) chan caps.FindEvent {
	t.Helper()
	ch := make(chan caps.FindEvent, 32)
	guiApp.OnFind(func(e caps.FindEvent) { ch <- e })
	t.Cleanup(func() { guiApp.OnFind(nil) })
	return ch
}

func nextFind(t *testing.T, ch chan caps.FindEvent) caps.FindEvent {
	t.Helper()
	select {
	case e := <-ch:
		return e
	case <-time.After(15 * time.Second):
		t.Fatal("no find event")
		return caps.FindEvent{}
	}
}

func TestGUIPageFind(t *testing.T) {
	needGUI(t)
	finds := collectFinds(t)
	w, v := openPage(t, "/pgfind")
	if err := w.Find(v, "foo", caps.FindOptions{}); err != nil {
		t.Fatal(err)
	}
	e := nextFind(t, finds)
	if e.View != v || e.Text != "foo" || e.Matches != 3 || e.Active != 1 || !e.Final {
		t.Fatalf("find foo: %+v", e)
	}
	if err := w.Find(v, "foo", caps.FindOptions{Continue: true}); err != nil {
		t.Fatal(err)
	}
	if e = nextFind(t, finds); e.Matches != 3 || e.Active != 2 {
		t.Fatalf("find next: %+v", e)
	}
	if err := w.Find(v, "foo", caps.FindOptions{MatchCase: true}); err != nil {
		t.Fatal(err)
	}
	if e = nextFind(t, finds); e.Matches != 2 {
		t.Fatalf("case-sensitive find: %+v", e)
	}
	if err := w.Find(v, "does not occur", caps.FindOptions{}); err != nil {
		t.Fatal(err)
	}
	if e = nextFind(t, finds); e.Matches != 0 || e.Active != 0 || !e.Final {
		t.Fatalf("find without match: %+v", e)
	}
	if err := w.StopFind(v); err != nil {
		t.Fatal(err)
	}
	if err := w.Find(999999, "x", caps.FindOptions{}); err == nil {
		t.Fatal("find in a missing view succeeded")
	}
}

func TestGUIPageCapture(t *testing.T) {
	needGUI(t)
	w, v := openPage(t, "/pgcolor")
	time.Sleep(500 * time.Millisecond) // let the page paint
	data, err := w.Capture(v, caps.CaptureOptions{})
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("not a PNG: %v", err)
	}
	b := img.Bounds()
	if b.Dx() < 300 || b.Dy() < 150 {
		t.Fatalf("snapshot is %dx%d, want at least the 480x320 view (minus window chrome)", b.Dx(), b.Dy())
	}
	r, g, bl, _ := img.At(b.Dx()-10, b.Dy()-10).RGBA()
	if r>>8 < 180 || g>>8 > 90 || bl>>8 > 90 {
		t.Fatalf("bottom right pixel is %x %x %x, want red", r>>8, g>>8, bl>>8)
	}
	if _, err := w.Capture(999999, caps.CaptureOptions{}); err == nil {
		t.Fatal("capture of a missing view succeeded")
	}
}

func TestGUIPagePrintPDF(t *testing.T) {
	needGUI(t)
	w, v := openPage(t, "/pgprint")
	data, err := w.PrintPDF(v, caps.PDFOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(data, []byte("%PDF")) || len(data) < 500 {
		t.Fatalf("not a PDF: %d bytes starting %q", len(data), data[:min(8, len(data))])
	}
	data, err = w.PrintPDF(v, caps.PDFOptions{PageWidth: 8.5, PageHeight: 11, Landscape: true, PrintBackground: true, Margins: &caps.Margins{Top: 1, Right: 1, Bottom: 1, Left: 1}})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(data, []byte("%PDF")) {
		t.Fatalf("not a PDF: %q", data[:min(8, len(data))])
	}
	if _, err := w.PrintPDF(v, caps.PDFOptions{PageWidth: -1, PageHeight: 3}); err == nil {
		t.Fatal("negative page size accepted")
	}
}

// TestGUIPagePrintDialog opens the system print dialog. Nothing in a test can
// close it, so it only runs when asked to.
func TestGUIPagePrintDialog(t *testing.T) {
	needGUI(t)
	if os.Getenv("KRATE_NATIVE_PRINT_DIALOG") != "1" {
		t.Skip("set KRATE_NATIVE_PRINT_DIALOG=1 to open the print dialog")
	}
	w, v := openPage(t, "/pgprint")
	if err := w.PrintDialog(v); err != nil {
		t.Fatal(err)
	}
	time.Sleep(10 * time.Second)
}

type downloadRig struct {
	mu      sync.Mutex
	events  []caps.DownloadEvent
	ch      chan caps.DownloadEvent
	started chan caps.DownloadRequest
}

func newDownloadRig(t *testing.T, decide func(r caps.DownloadRequest) string) *downloadRig {
	t.Helper()
	r := &downloadRig{ch: make(chan caps.DownloadEvent, 256), started: make(chan caps.DownloadRequest, 8)}
	guiApp.OnDownloadStart(func(req caps.DownloadRequest, d func(string)) {
		r.started <- req
		go d(decide(req))
	})
	guiApp.OnDownload(func(e caps.DownloadEvent) {
		r.mu.Lock()
		r.events = append(r.events, e)
		r.mu.Unlock()
		r.ch <- e
	})
	t.Cleanup(func() { guiApp.OnDownloadStart(nil); guiApp.OnDownload(nil) })
	return r
}

func (r *downloadRig) until(t *testing.T, id uint64, states ...caps.DownloadState) caps.DownloadEvent {
	t.Helper()
	deadline := time.After(45 * time.Second)
	for {
		select {
		case e := <-r.ch:
			if id != 0 && e.ID != id {
				continue
			}
			for _, s := range states {
				if e.State == s {
					return e
				}
			}
		case <-deadline:
			r.mu.Lock()
			defer r.mu.Unlock()
			t.Fatalf("no %v event; saw %+v", states, r.events)
		}
	}
}

func (r *downloadRig) request(t *testing.T) caps.DownloadRequest {
	t.Helper()
	select {
	case req := <-r.started:
		return req
	case <-time.After(30 * time.Second):
		t.Fatal("the page's download never reached the handler")
		return caps.DownloadRequest{}
	}
}

func click(t *testing.T, w *Window, v caps.ViewRef, id string) {
	t.Helper()
	pageEval(t, w, v, `(function(){document.getElementById("`+id+`").click();return "ok"})()`)
}

var end = []caps.DownloadState{caps.DownloadCompleted, caps.DownloadFailed, caps.DownloadCancelled}

func TestGUIPageDownloads(t *testing.T) {
	needGUI(t)
	dir := t.TempDir()
	rig := newDownloadRig(t, func(r caps.DownloadRequest) string { return filepath.Join(dir, r.SuggestedName) })
	for _, c := range []struct{ link, name, body string }{
		{"a", "hello.txt", "hello download"},
		{"att", "att.bin", "attachment body"},
		{"blob", "blob.txt", "blob body"},
	} {
		// A fresh page per download: Chromium lets a page start one download
		// without a user gesture and holds back the later ones, and a script
		// click is not a gesture.
		w, v := openPage(t, "/pgdl")
		t.Logf("downloading via #%s", c.link)
		click(t, w, v, c.link)
		req := rig.request(t)
		if req.SuggestedName != c.name || req.ID == 0 || req.View != v {
			t.Fatalf("%s: request %+v", c.link, req)
		}
		e := rig.until(t, req.ID, end...)
		if e.State != caps.DownloadCompleted || e.Path != filepath.Join(dir, c.name) {
			t.Fatalf("%s ended as %+v", c.link, e)
		}
		if b, err := os.ReadFile(e.Path); err != nil || string(b) != c.body {
			t.Fatalf("%s: file %q, %v", c.link, b, err)
		}
	}
}

func TestGUIPageDownloadDeclined(t *testing.T) {
	needGUI(t)
	rig := newDownloadRig(t, func(r caps.DownloadRequest) string { return "" })
	w, v := openPage(t, "/pgdl")
	click(t, w, v, "a")
	req := rig.request(t)
	if e := rig.until(t, req.ID, end...); e.State != caps.DownloadCancelled {
		t.Fatalf("a declined download ended as %+v", e)
	}
}

func TestGUIPageDownloadCancel(t *testing.T) {
	needGUI(t)
	dir := t.TempDir()
	rig := newDownloadRig(t, func(r caps.DownloadRequest) string { return filepath.Join(dir, r.SuggestedName) })
	w, v := openPage(t, "/pgdl")
	click(t, w, v, "slow")
	req := rig.request(t)
	rig.until(t, req.ID, caps.DownloadStarted)
	if err := guiApp.CancelDownload(req.ID); err != nil {
		// The body is already in memory, so a fast disk can finish first.
		if e := rig.until(t, req.ID, end...); e.State == caps.DownloadCompleted {
			t.Skipf("the download finished before it could be cancelled (%v)", err)
		}
		t.Fatal(err)
	}
	if e := rig.until(t, req.ID, end...); e.State != caps.DownloadCancelled {
		t.Fatalf("a cancelled download ended as %+v", e)
	}
	if err := guiApp.CancelDownload(req.ID); err == nil {
		t.Fatal("cancelling a finished download succeeded")
	}
}

func TestGUIPageCookies(t *testing.T) {
	needGUI(t)
	openPage(t, "/pgfind") // a window must exist for the cookie store
	const origin = "https://example.com"
	if err := guiApp.ClearSiteData(caps.ClearSiteDataOptions{Cookies: true}); err != nil {
		t.Fatal(err)
	}
	list := func(o string) map[string]caps.Cookie {
		cs, err := guiApp.Cookies(o)
		if err != nil {
			t.Fatal(err)
		}
		m := map[string]caps.Cookie{}
		for _, c := range cs {
			m[c.Name] = c
		}
		return m
	}
	if got := list(origin); len(got) != 0 {
		t.Fatalf("cookies after clearing: %+v", got)
	}
	exp := time.Now().Add(48 * time.Hour).UTC().Truncate(time.Second)
	if err := guiApp.SetCookie(origin, caps.Cookie{Name: "session", Value: "abc123", HTTPOnly: true, Secure: true, Expires: exp}); err != nil {
		t.Fatal(err)
	}
	if err := guiApp.SetCookie(origin, caps.Cookie{Name: "plain", Value: "xy", Path: "/"}); err != nil {
		t.Fatal(err)
	}
	got := list(origin)
	s, ok := got["session"]
	if !ok || s.Value != "abc123" || !s.HTTPOnly || !s.Secure || s.Path != "/" || s.Expires.Sub(exp).Abs() > 2*time.Second {
		t.Fatalf("session cookie: %+v (all %+v)", s, got)
	}
	if p := got["plain"]; p.Value != "xy" || !p.Expires.IsZero() {
		t.Fatalf("plain cookie: %+v", p)
	}
	if err := guiApp.DeleteCookie(origin, caps.Cookie{Name: "session"}); err != nil {
		t.Fatal(err)
	}
	got = list(origin)
	if _, ok := got["session"]; ok {
		t.Fatalf("session cookie survived deletion: %+v", got)
	}
	if _, ok := got["plain"]; !ok {
		t.Fatalf("plain cookie was deleted too: %+v", got)
	}
	appOriginCookies(t, list)
	if err := guiApp.SetCookie("not an origin", caps.Cookie{Name: "a", Value: "b"}); err == nil {
		t.Fatal("bad origin accepted")
	}
	if err := guiApp.ClearSiteData(caps.ClearSiteDataOptions{Cache: true, Storage: true}); err != nil {
		t.Fatal(err)
	}
	if got := list(origin); len(got) != 1 {
		t.Fatalf("clearing cache and storage touched the cookies: %+v", got)
	}
	if err := guiApp.ClearSiteData(caps.ClearSiteDataOptions{}); err != nil {
		t.Fatal(err)
	}
	if got := list(origin); len(got) != 0 {
		t.Fatalf("cookies after clearing everything: %+v", got)
	}
}

func pageEval(t *testing.T, w *Window, ref caps.ViewRef, js string) string {
	t.Helper()
	ch := make(chan string, 1)
	err := w.onView(ref, func(v *view) error {
		scriptResult(v, js, func(hr uintptr, s string) { ch <- s })
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case s := <-ch:
		return strings.Trim(s, `"`)
	case <-time.After(15 * time.Second):
		t.Fatalf("script %q did not answer", js)
		return ""
	}
}

func zoomShortcuts(t *testing.T, w *Window, v caps.ViewRef) {
	// WebView2 handles Ctrl plus, minus and 0 itself (browser accelerator
	// keys stay enabled); there is nothing of ours to drive.
}

func appOriginCookies(t *testing.T, list func(string) map[string]caps.Cookie) {
	// On Windows the app origin is an https origin and holds cookies.
	if err := guiApp.SetCookie(Origin, caps.Cookie{Name: "app", Value: "1"}); err != nil {
		t.Fatal(err)
	}
	if c, ok := list(Origin)["app"]; !ok || c.Value != "1" {
		t.Fatalf("cookie of the app origin: %+v", list(Origin))
	}
	if err := guiApp.DeleteCookie(Origin, caps.Cookie{Name: "app"}); err != nil {
		t.Fatal(err)
	}
	if _, ok := list(Origin)["app"]; ok {
		t.Fatal("cookie of the app origin survived deletion")
	}
}
