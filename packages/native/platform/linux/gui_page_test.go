//go:build linux

package linux

import (
	"bytes"
	"errors"
	"image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/kratejs/krate/packages/native/krate"
	"github.com/kratejs/krate/packages/native/platform/caps"
)

// Tests for the per-view web contents controls: zoom, find, capture, print,
// downloads and site data.

func registerPageHandlers(s *testServer) {
	s.mux.HandleFunc("/pgfind", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, `<!doctype html><title>find</title><body style="margin:8px">
<p>foo bar Foo baz foo</p><p>nothing else here</p></body>`)
	})
	s.mux.HandleFunc("/pgcolor", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, `<!doctype html><title>color</title><body style="margin:0;background:#ff0000;color:#fff"><h1>capture me</h1></body>`)
	})
	s.mux.HandleFunc("/pgprint", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, `<!doctype html><title>print</title><body style="background:#eee"><h1>Print me</h1><p>Some text for the page.</p></body>`)
	})
	s.mux.HandleFunc("/pgdl", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, `<!doctype html><title>dl</title><body>
<a id="a" download="hello.txt" href="/pgfile">file</a>
<a id="slow" download="slow.bin" href="/pgslow">slow</a>
<a id="b" download="named.txt" href="data:text/plain,hi">data</a>
<a id="att" href="/pgatt">attachment</a>
<a id="blob" download="blob.txt">blob</a>
<script>var u=URL.createObjectURL(new Blob(["blob body"],{type:"text/plain"}));document.getElementById("blob").href=u;</script></body>`)
	})
	s.mux.HandleFunc("/pgfile", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Length", "14")
		_, _ = io.WriteString(w, "hello download")
	})
	s.mux.HandleFunc("/pgatt", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", `attachment; filename="att.bin"`)
		_, _ = io.WriteString(w, "attachment body")
	})
	s.mux.HandleFunc("/pgslow", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Length", strconv.Itoa(64*1024*100))
		chunk := make([]byte, 64*1024)
		for i := 0; i < 100; i++ {
			if _, err := w.Write(chunk); err != nil {
				return
			}
			w.(http.Flusher).Flush()
			select {
			case <-r.Context().Done():
				return
			case <-time.After(150 * time.Millisecond):
			}
		}
	})
	s.mux.HandleFunc("/pgcookie", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, `<!doctype html><title>cookie</title><body>cookie</body>`)
	})
}

func pageView(t *testing.T, path string) (*Window, caps.ViewRef) {
	t.Helper()
	w := memWindow(t, path)
	t.Cleanup(func() { w.Close() })
	return w, w.PrimaryView()
}

func TestGUIPageZoom(t *testing.T) {
	needGUI(t)
	w, v := pageView(t, "/pgfind")
	if z, err := w.ZoomFactor(v); err != nil || z != 1 {
		t.Fatalf("initial zoom %v, %v", z, err)
	}
	if err := w.SetZoomFactor(v, 1.5); err != nil {
		t.Fatal(err)
	}
	if z, _ := w.ZoomFactor(v); z != 1.5 {
		t.Fatalf("zoom after set: %v", z)
	}
	if err := w.SetZoomFactor(v, 9); err == nil {
		t.Fatal("zoom 9 accepted")
	}
	if err := w.SetZoomFactor(v, 0.1); err == nil {
		t.Fatal("zoom 0.1 accepted")
	}
	// The page sees the zoom through devicePixelRatio.
	waitFor(t, "devicePixelRatio 1.5", func() bool { return evalValue(t, w, "String(window.devicePixelRatio)") == "1.5" })

	// Ctrl plus / minus / 0 through the real signal on the webview.
	press := func(keyval uint32, state uint32) bool {
		ev := make([]byte, 64)
		*(*uint32)(unsafe.Pointer(&ev[24])) = state
		*(*uint32)(unsafe.Pointer(&ev[28])) = keyval
		var handled int32
		ch := make(chan struct{})
		guiApp.Dispatch(func() {
			emitKey(w.viewWidget(v), unsafe.Pointer(&ev[0]), &handled)
			close(ch)
		})
		<-ch
		return handled != 0
	}
	const ctrl = 1 << 2
	if !press(0x2b, ctrl|1) { // ctrl+shift+plus
		t.Fatal("ctrl+plus was not handled")
	}
	if z, _ := w.ZoomFactor(v); z != 1.75 {
		t.Fatalf("zoom after ctrl+plus: %v, want 1.75", z)
	}
	if !press(0x2d, ctrl) {
		t.Fatal("ctrl+minus was not handled")
	}
	if z, _ := w.ZoomFactor(v); z != 1.5 {
		t.Fatalf("zoom after ctrl+minus: %v", z)
	}
	if !press(0x30, ctrl) {
		t.Fatal("ctrl+0 was not handled")
	}
	if z, _ := w.ZoomFactor(v); z != 1 {
		t.Fatalf("zoom after ctrl+0: %v", z)
	}
	// Without ctrl, or with alt, the zoom stays (WebKit may still consume the key).
	press(0x2b, 0)
	press(0x2b, ctrl|(1<<3))
	if z, _ := w.ZoomFactor(v); z != 1 {
		t.Fatalf("zoom changed to %v by keys that are not the zoom shortcut", z)
	}

	// Each view keeps its own zoom.
	extra, err := w.AddView(caps.ViewOptions{Path: "/pgfind"})
	if err != nil {
		t.Fatal(err)
	}
	if err := w.SetZoomFactor(extra, 2); err != nil {
		t.Fatal(err)
	}
	if z, _ := w.ZoomFactor(v); z != 1 {
		t.Fatalf("primary zoom changed to %v by the other view", z)
	}
	if z, _ := w.ZoomFactor(extra); z != 2 {
		t.Fatalf("extra zoom %v", z)
	}
	if _, err := w.ZoomFactor(9999); err == nil {
		t.Fatal("zoom of a missing view succeeded")
	}
}

var emitKeyFn func(inst uintptr, name string, ev unsafe.Pointer, ret *int32)

func emitKey(widget uintptr, ev unsafe.Pointer, ret *int32) {
	if emitKeyFn == nil {
		h, err := purego.Dlopen("libgobject-2.0.so.0", rtldNow|rtldGlobal)
		if err != nil {
			panic(err)
		}
		purego.RegisterLibFunc(&emitKeyFn, h, "g_signal_emit_by_name")
	}
	emitKeyFn(widget, "key-press-event", ev, ret)
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
	case <-time.After(10 * time.Second):
		t.Fatal("no find event")
		return caps.FindEvent{}
	}
}

func TestGUIPageFind(t *testing.T) {
	needGUI(t)
	finds := collectFinds(t)
	w, v := pageView(t, "/pgfind")
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
	if err := w.Find(v, "foo", caps.FindOptions{Continue: true, Backward: true}); err != nil {
		t.Fatal(err)
	}
	if e = nextFind(t, finds); e.Matches != 3 || e.Active != 1 {
		t.Fatalf("find previous: %+v", e)
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
	if err := w.Find(9999, "x", caps.FindOptions{}); err == nil {
		t.Fatal("find in a missing view succeeded")
	}
}

func TestGUIPageCapture(t *testing.T) {
	needGUI(t)
	w, v := pageView(t, "/pgcolor")
	time.Sleep(300 * time.Millisecond) // let the page paint
	data, err := w.Capture(v, caps.CaptureOptions{})
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("not a PNG: %v", err)
	}
	var ww, hh int
	w.app.callMain(func() {
		vw := w.viewByRef(v).widget
		ww, hh = int(w.app.x.gtkWidgetAllocW(vw)), int(w.app.x.gtkWidgetAllocH(vw))
	})
	if b := img.Bounds(); b.Dx() != ww || b.Dy() != hh || ww < 100 || hh < 100 {
		t.Fatalf("snapshot %dx%d, view %dx%d", b.Dx(), b.Dy(), ww, hh)
	}
	r, g, bl, _ := img.At(img.Bounds().Dx()-5, img.Bounds().Dy()-5).RGBA()
	if r>>8 < 0xf0 || g>>8 > 0x10 || bl>>8 > 0x10 {
		t.Fatalf("bottom right pixel is %x %x %x, want red", r>>8, g>>8, bl>>8)
	}
	// A second view is captured on its own.
	extra, err := w.AddView(caps.ViewOptions{Path: "/pgfind"})
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(500 * time.Millisecond)
	d2, err := w.Capture(extra, caps.CaptureOptions{})
	if err != nil {
		t.Fatal(err)
	}
	img2, err := png.Decode(bytes.NewReader(d2))
	if err != nil {
		t.Fatal(err)
	}
	if img2.Bounds().Dx() >= img.Bounds().Dx() {
		t.Fatalf("the extra view's snapshot (%d wide) should be narrower than the whole window's primary view (%d)", img2.Bounds().Dx(), img.Bounds().Dx())
	}
}

var mediaBox = regexp.MustCompile(`/MediaBox\s*\[\s*0\s+0\s+([0-9.]+)\s+([0-9.]+)\s*\]`)

func pdfSize(t *testing.T, b []byte) (w, h float64) {
	t.Helper()
	m := mediaBox.FindSubmatch(b)
	if m == nil {
		t.Fatalf("no MediaBox in the PDF (%d bytes)", len(b))
	}
	w, _ = strconv.ParseFloat(string(m[1]), 64)
	h, _ = strconv.ParseFloat(string(m[2]), 64)
	return
}

func TestGUIPagePrintPDF(t *testing.T) {
	needGUI(t)
	w, v := pageView(t, "/pgprint")
	data, err := w.PrintPDF(v, caps.PDFOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(data, []byte("%PDF")) {
		t.Fatalf("not a PDF: %q", data[:min(8, len(data))])
	}
	pw, ph := pdfSize(t, data)
	if pw < 590 || pw > 600 || ph < 837 || ph > 847 {
		t.Fatalf("default page is %vx%v points, want A4 (595x842)", pw, ph)
	}
	data, err = w.PrintPDF(v, caps.PDFOptions{PageWidth: 8.5, PageHeight: 11, Landscape: true, PrintBackground: true, Margins: &caps.Margins{Top: 1, Right: 1, Bottom: 1, Left: 1}})
	if err != nil {
		t.Fatal(err)
	}
	pw, ph = pdfSize(t, data)
	if pw < 787 || pw > 797 || ph < 607 || ph > 617 {
		t.Fatalf("landscape Letter is %vx%v points, want 792x612", pw, ph)
	}
	if _, err := w.PrintPDF(v, caps.PDFOptions{PageWidth: -1, PageHeight: 3}); err == nil {
		t.Fatal("negative page size accepted")
	}
	if _, err := w.PrintPDF(9999, caps.PDFOptions{}); err == nil {
		t.Fatal("print of a missing view succeeded")
	}
}

// TestGUIPagePrintDialog opens the real, modal GTK print dialog. It runs only
// with KRATE_NATIVE_PRINT_DIALOG=1, as on macOS and Windows: CI sets it for the
// single GUI pass. In a long process that has opened many web contexts (the
// repeated pass, go test -count=3) WebKit's run_dialog was seen to block
// before entering its loop, hanging the job.
func TestGUIPagePrintDialog(t *testing.T) {
	needGUI(t)
	if os.Getenv("KRATE_NATIVE_PRINT_DIALOG") != "1" {
		t.Skip("set KRATE_NATIVE_PRINT_DIALOG=1 to open the print dialog")
	}
	w, v := pageView(t, "/pgprint")
	before, _ := guiApp.topLevelTitles()
	if err := w.PrintDialog(v); err != nil {
		t.Fatal(err)
	}
	var wins []uintptr
	waitFor(t, "the print dialog", func() bool {
		titles, ws := guiApp.topLevelTitles()
		if len(titles) <= len(before) {
			return false
		}
		for i, ti := range titles {
			if ti == "Print" {
				wins = append(wins, ws[i])
				return true
			}
		}
		return false
	})
	// Close it as a user would; the nested loop of run_dialog then returns.
	guiApp.Dispatch(func() { guiApp.x.gSignalEmitByName(wins[0], "response", gtkResponseCancel) })
	waitFor(t, "the print dialog to close", func() bool {
		titles, _ := guiApp.topLevelTitles()
		for _, ti := range titles {
			if ti == "Print" {
				return false
			}
		}
		return true
	})
}

// downloadRig installs download handlers that save into dir and collects the
// events.
type downloadRig struct {
	mu      sync.Mutex
	events  []caps.DownloadEvent
	reqs    []caps.DownloadRequest
	ch      chan caps.DownloadEvent
	decide  func(r caps.DownloadRequest) string
	started chan caps.DownloadRequest
}

func newDownloadRig(t *testing.T, decide func(r caps.DownloadRequest) string) *downloadRig {
	t.Helper()
	r := &downloadRig{ch: make(chan caps.DownloadEvent, 256), decide: decide, started: make(chan caps.DownloadRequest, 8)}
	guiApp.OnDownloadStart(func(req caps.DownloadRequest, decide func(string)) {
		r.mu.Lock()
		r.reqs = append(r.reqs, req)
		r.mu.Unlock()
		r.started <- req
		go decide(r.decide(req))
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
	deadline := time.After(30 * time.Second)
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
			t.Fatalf("no %v event; saw %+v", states, r.events)
		}
	}
}

func click(t *testing.T, w *Window, id string) {
	t.Helper()
	evalValue(t, w, `(function(){document.getElementById("`+id+`").click();return "ok"})()`)
}

func TestGUIPageDownloadCompletes(t *testing.T) {
	needGUI(t)
	dir := t.TempDir()
	rig := newDownloadRig(t, func(r caps.DownloadRequest) string { return filepath.Join(dir, r.SuggestedName) })
	w, _ := pageView(t, "/pgdl")
	click(t, w, "a")
	req := <-rig.started
	if req.SuggestedName != "hello.txt" || !strings.HasSuffix(req.URL, "/pgfile") || req.ID == 0 {
		t.Fatalf("request %+v", req)
	}
	if req.View != w.PrimaryView() {
		t.Errorf("download view %d, want %d", req.View, w.PrimaryView())
	}
	if req.Total != 14 {
		t.Errorf("total %d, want 14", req.Total)
	}
	done := rig.until(t, req.ID, caps.DownloadCompleted, caps.DownloadFailed, caps.DownloadCancelled)
	if done.State != caps.DownloadCompleted {
		t.Fatalf("download ended as %+v", done)
	}
	want := filepath.Join(dir, "hello.txt")
	if done.Path != want {
		t.Fatalf("final path %q, want %q", done.Path, want)
	}
	got, err := os.ReadFile(want)
	if err != nil || string(got) != "hello download" {
		t.Fatalf("file content %q, %v", got, err)
	}
	if done.Received != 14 && done.Received != 0 {
		t.Errorf("received %d", done.Received)
	}
}

func TestGUIPageDownloadDeclined(t *testing.T) {
	needGUI(t)
	rig := newDownloadRig(t, func(r caps.DownloadRequest) string { return "" })
	w, _ := pageView(t, "/pgdl")
	click(t, w, "a")
	req := <-rig.started
	e := rig.until(t, req.ID, caps.DownloadCompleted, caps.DownloadFailed, caps.DownloadCancelled)
	if e.State != caps.DownloadCancelled {
		t.Fatalf("a declined download ended as %+v", e)
	}
}

func TestGUIPageDownloadCancel(t *testing.T) {
	needGUI(t)
	dir := t.TempDir()
	rig := newDownloadRig(t, func(r caps.DownloadRequest) string { return filepath.Join(dir, r.SuggestedName) })
	w, _ := pageView(t, "/pgdl")
	click(t, w, "slow")
	req := <-rig.started
	rig.until(t, req.ID, caps.DownloadStarted)
	rig.until(t, req.ID, caps.DownloadProgress)
	if err := guiApp.CancelDownload(req.ID); err != nil {
		t.Fatal(err)
	}
	e := rig.until(t, req.ID, caps.DownloadCompleted, caps.DownloadFailed, caps.DownloadCancelled)
	if e.State != caps.DownloadCancelled {
		t.Fatalf("a cancelled download ended as %+v", e)
	}
	if err := guiApp.CancelDownload(req.ID); err == nil {
		t.Fatal("cancelling a finished download succeeded")
	}
}

func TestGUIPageCookies(t *testing.T) {
	needGUI(t)
	pageView(t, "/pgcookie")
	const origin = "https://example.com"
	if err := guiApp.ClearSiteData(caps.ClearSiteDataOptions{Cookies: true}); err != nil {
		t.Fatal(err)
	}
	list := func() map[string]caps.Cookie {
		cs, err := guiApp.Cookies(origin)
		if err != nil {
			t.Fatal(err)
		}
		m := map[string]caps.Cookie{}
		for _, c := range cs {
			m[c.Name] = c
		}
		return m
	}
	if got := list(); len(got) != 0 {
		t.Fatalf("cookies after clearing: %+v", got)
	}
	exp := time.Now().Add(48 * time.Hour).UTC().Truncate(time.Second)
	if err := guiApp.SetCookie(origin, caps.Cookie{Name: "session", Value: "abc123", HTTPOnly: true, Expires: exp}); err != nil {
		t.Fatal(err)
	}
	if err := guiApp.SetCookie(origin, caps.Cookie{Name: "plain", Value: "x y", Path: "/"}); err != nil {
		t.Fatal(err)
	}
	got := list()
	s, ok := got["session"]
	if !ok || s.Value != "abc123" || !s.HTTPOnly || !s.Expires.Equal(exp) || s.Path != "/" {
		t.Fatalf("session cookie: %+v (all %+v)", s, got)
	}
	if p := got["plain"]; p.Value != "x y" || !p.Expires.IsZero() {
		t.Fatalf("plain cookie: %+v", p)
	}
	// WebKit keeps no cookies for the app's custom scheme; the API says so.
	if _, err := guiApp.Cookies(Origin); !errors.Is(err, krate.ErrUnsupported) {
		t.Fatalf("Cookies(app origin) = %v, want ErrUnsupported", err)
	}
	if err := guiApp.SetCookie(Origin, caps.Cookie{Name: "a", Value: "b"}); !errors.Is(err, krate.ErrUnsupported) {
		t.Fatalf("SetCookie(app origin) = %v, want ErrUnsupported", err)
	}
	// Another host's cookies are not in the list.
	if cs, err := guiApp.Cookies("https://other.example.org"); err != nil || len(cs) != 0 {
		t.Fatalf("cookies of another host: %+v, %v", cs, err)
	}
	if err := guiApp.DeleteCookie(origin, caps.Cookie{Name: "session"}); err != nil {
		t.Fatal(err)
	}
	got = list()
	if _, ok := got["session"]; ok {
		t.Fatalf("session cookie survived deletion: %+v", got)
	}
	if _, ok := got["plain"]; !ok {
		t.Fatalf("plain cookie was deleted too: %+v", got)
	}
	if err := guiApp.SetCookie("not an origin", caps.Cookie{Name: "a", Value: "b"}); err == nil {
		t.Fatal("bad origin accepted")
	}
	if err := guiApp.SetCookie(origin, caps.Cookie{Name: "bad name", Value: "b"}); err == nil {
		t.Fatal("bad cookie name accepted")
	}
	if err := guiApp.ClearSiteData(caps.ClearSiteDataOptions{}); err != nil {
		t.Fatal(err)
	}
	if got := list(); len(got) != 0 {
		t.Fatalf("cookies after clearing everything: %+v", got)
	}
}

func TestGUIPageDownloadAttachmentAndBlob(t *testing.T) {
	needGUI(t)
	dir := t.TempDir()
	rig := newDownloadRig(t, func(r caps.DownloadRequest) string { return filepath.Join(dir, r.SuggestedName) })
	w, _ := pageView(t, "/pgdl")
	for id, want := range map[string][2]string{"att": {"att.bin", "attachment body"}, "blob": {"blob.txt", "blob body"}} {
		click(t, w, id)
		req := <-rig.started
		if req.SuggestedName != want[0] {
			t.Fatalf("%s: suggested name %q, want %q", id, req.SuggestedName, want[0])
		}
		e := rig.until(t, req.ID, caps.DownloadCompleted, caps.DownloadFailed, caps.DownloadCancelled)
		if e.State != caps.DownloadCompleted {
			t.Fatalf("%s ended as %+v", id, e)
		}
		if b, err := os.ReadFile(filepath.Join(dir, want[0])); err != nil || string(b) != want[1] {
			t.Fatalf("%s: file %q, %v", id, b, err)
		}
	}
}
