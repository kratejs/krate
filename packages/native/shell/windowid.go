package shell

import (
	"bytes"
	"net/http"
	"strconv"
	"strings"
)

// windowIDScript tells the page which window it is in, so that its host calls
// carry the id the shell knows the window by (the x-krate-window header). The
// shell opens a scoped window at a URL with the krate.WindowQuery parameter;
// the script keeps the value in sessionStorage, which lives as long as the
// window's browsing context, so later pages of the window (links, reloads)
// still know it. A page opened without the parameter and with nothing stored
// keeps the client's random id.
//
// In a native window the header is only a label: the platform layer tags
// every request with the webview it came from and the shell maps that view to
// its window and scope, ignoring the header (see app.identify). Page script
// that sets another id gains nothing. The header decides only where there is
// no native view, in browser mode (krate native dev --browser), which has one
// browser context per tab and no way to tell tabs apart natively; there a
// scope narrows what a window's own code does by accident, and is not a
// sandbox for code you do not trust.
const windowIDScript = `<script>(function(){try{var k="__krate_win",v=new URLSearchParams(location.search).get(k)||sessionStorage.getItem(k);if(v){sessionStorage.setItem(k,v);globalThis.__KRATE_WINDOW__=v}}catch(e){}})()</script>`

// devErrorsScript records the errors a page throws, from the first line of
// the document, in globalThis.__krateDriverErrs (strings). Dev shells inject
// it so the test driver can report errors that happened before it looked.
const devErrorsScript = `<script>(function(){var a=globalThis.__krateDriverErrs=[];function s(e){try{return String((e&&e.stack)||(e&&e.message)||e)}catch(_){return "unprintable error"}}addEventListener("error",function(e){a.push(s(e.error||e.message))});addEventListener("unhandledrejection",function(e){a.push(s(e.reason))})})()</script>`

// windowIDHandler injects windowIDScript, and extra when given, into the HTML
// pages next serves.
func windowIDHandler(next http.Handler, extra string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if (r.Method != http.MethodGet && r.Method != http.MethodHead) || strings.HasPrefix(r.URL.Path, "/__krate/") {
			next.ServeHTTP(w, r)
			return
		}
		iw := &injectWriter{ResponseWriter: w, head: r.Method == http.MethodHead, extra: extra}
		next.ServeHTTP(iw, r)
		iw.finish()
	})
}

type injectWriter struct {
	extra string
	http.ResponseWriter
	head   bool
	status int
	html   bool
	buf    bytes.Buffer
	sent   bool
}

func (w *injectWriter) WriteHeader(status int) {
	if w.sent || w.status != 0 {
		return
	}
	w.status = status
	ct := w.Header().Get("Content-Type")
	w.html = strings.HasPrefix(ct, "text/html") && w.Header().Get("Content-Encoding") == ""
	if !w.html {
		w.sent = true
		w.ResponseWriter.WriteHeader(status)
	}
}

func (w *injectWriter) Write(p []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	if !w.html {
		return w.ResponseWriter.Write(p)
	}
	return w.buf.Write(p)
}

// Flush forwards to the underlying writer for non-HTML responses (streams).
func (w *injectWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok && !w.html {
		f.Flush()
	}
}

func (w *injectWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (w *injectWriter) finish() {
	if !w.html || w.sent {
		return
	}
	body := w.buf.Bytes()
	out := injectIntoHead(body, w.extra)
	h := w.Header()
	if cl := h.Get("Content-Length"); cl != "" {
		n := len(out)
		if w.head {
			old, _ := strconv.Atoi(cl)
			n = old + len(windowIDScript) + len(w.extra)
		}
		h.Set("Content-Length", strconv.Itoa(n))
	}
	w.ResponseWriter.WriteHeader(w.status)
	if !w.head {
		_, _ = w.ResponseWriter.Write(out) // the client went away; nothing to report
	}
}

// injectIntoHead puts the script right after the opening head tag, or at the
// start of the document when there is none (after the html tag or doctype
// when it has one).
func injectIntoHead(body []byte, extra ...string) []byte {
	lower := bytes.ToLower(body)
	at := 0
	for _, tag := range []string{"<head", "<html", "<!doctype"} {
		i := bytes.Index(lower, []byte(tag))
		if i < 0 {
			continue
		}
		if j := bytes.IndexByte(lower[i:], '>'); j >= 0 {
			at = i + j + 1
			break
		}
	}
	out := make([]byte, 0, len(body)+len(windowIDScript))
	out = append(out, body[:at]...)
	out = append(out, windowIDScript...)
	for _, e := range extra {
		out = append(out, e...)
	}
	out = append(out, body[at:]...)
	return out
}
