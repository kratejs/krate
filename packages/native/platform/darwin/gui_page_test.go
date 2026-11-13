//go:build darwin

package darwin

import (
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ebitengine/purego/objc"
	"github.com/kratejs/krate/packages/native/platform/caps"
)

// GUI tests of the per-view web contents controls: zoom, find, capture, PDF
// output, downloads and cookies. They run with the other GUI tests
// (KRATE_NATIVE_GUI=1) in the CI native job on macos-latest.
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
	mux.HandleFunc("/pgslow", func(w http.ResponseWriter, r *http.Request) {
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
	w, err := guiApp.NewWindow(WindowOptions{Title: path, Width: 480, Height: 320, Resizable: true, Path: path})
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

// TestGUIPagePrintDialog opens the system print dialog. Nothing in a test can
// close it, so it only runs when asked to.
func TestGUIPagePrintDialog(t *testing.T) {
	requireGUI(t)
	if os.Getenv("KRATE_NATIVE_PRINT_DIALOG") != "1" {
		t.Skip("set KRATE_NATIVE_PRINT_DIALOG=1 to open the print dialog")
	}
	w, v := openPage(t, "/pgprint")
	if err := w.PrintDialog(v); err != nil {
		t.Fatal(err)
	}
	time.Sleep(10 * time.Second)
}

func pageEval(t *testing.T, w *Window, ref caps.ViewRef, js string) string {
	t.Helper()
	ch := make(chan string, 1)
	err := w.onView(ref, func(v *view) error {
		blk := objc.NewBlock(func(_ objc.Block, result, e objc.ID) {
			s := ""
			if e == 0 && result != 0 {
				s = goString(result.Send(sel("description")))
			}
			ch <- s
		})
		v.web.Send(sel("evaluateJavaScript:completionHandler:"), nsString(js), objc.ID(blk))
		blk.Release()
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case s := <-ch:
		return s
	case <-time.After(15 * time.Second):
		t.Fatalf("script %q did not answer", js)
		return ""
	}
}
