package windows

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
)

// commitThreshold is how much a handler may write before the response is
// committed without a Flush. It mirrors net/http's 4 KiB write buffer, so
// ordinary small responses are sent whole (with an exact length) and large or
// streaming ones start flowing.
const commitThreshold = 4096

// streamBound limits how far a handler may run ahead of the webview.
const streamBound = 1 << 20

// responseWriter adapts the http.Handler contract to WebView2's response
// model. The response is committed (handed to the webview) on the first
// Flush, when more than commitThreshold bytes were written, or when the
// handler returns, whichever comes first.
//
//   - Committed early: commit is called once with a stream; the handler keeps
//     writing into it and finish closes it.
//   - Handler returned first: complete is called with the whole body.
type responseWriter struct {
	mu       sync.Mutex
	header   http.Header
	snapshot http.Header
	status   int
	buf      []byte
	stream   *blockBuf
	head     bool
	finished bool
	// whole holds the response back until the handler returns: WebView2 wants
	// the complete body when the response is installed.
	whole bool

	commit   func(status int, h http.Header, body *blockBuf)
	complete func(status int, h http.Header, body []byte)
}

func newResponseWriter(head bool,
	commit func(int, http.Header, *blockBuf),
	complete func(int, http.Header, []byte)) *responseWriter {
	return &responseWriter{header: http.Header{}, head: head, commit: commit, complete: complete}
}

func (w *responseWriter) Header() http.Header { return w.header }

func (w *responseWriter) WriteHeader(code int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.writeHeaderLocked(code)
}

func (w *responseWriter) writeHeaderLocked(code int) {
	if w.status != 0 || w.finished {
		return
	}
	if code >= 100 && code < 200 && code != 101 {
		return // informational responses are not forwarded
	}
	w.status = code
	w.snapshot = w.header.Clone()
}

func (w *responseWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	if w.finished {
		w.mu.Unlock()
		return 0, http.ErrHandlerTimeout
	}
	w.writeHeaderLocked(http.StatusOK)
	if w.head {
		w.mu.Unlock()
		return len(p), nil
	}
	if w.stream != nil {
		s := w.stream
		w.mu.Unlock()
		return s.Write(p)
	}
	w.buf = append(w.buf, p...)
	if !w.whole && len(w.buf) >= commitThreshold {
		w.commitLocked()
	}
	w.mu.Unlock()
	return len(p), nil
}

// Flush implements http.Flusher.
func (w *responseWriter) Flush() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.finished {
		return
	}
	w.writeHeaderLocked(http.StatusOK)
	if w.stream == nil && !w.whole {
		w.commitLocked()
	}
}

func (w *responseWriter) commitLocked() {
	if len(w.buf) > 0 && w.snapshot.Get("Content-Type") == "" {
		w.snapshot.Set("Content-Type", http.DetectContentType(w.buf))
	}
	s := newBlockBuf(streamBound)
	s.prime(w.buf)
	w.buf = nil
	w.stream = s
	w.commit(w.status, w.snapshot, s)
}

// finish is called once after the handler returned.
func (w *responseWriter) finish() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.finished {
		return
	}
	w.writeHeaderLocked(http.StatusOK)
	w.finished = true
	if w.stream != nil {
		w.stream.Close()
		return
	}
	if len(w.buf) > 0 && w.snapshot.Get("Content-Type") == "" {
		w.snapshot.Set("Content-Type", http.DetectContentType(w.buf))
	}
	w.complete(w.status, w.snapshot, w.buf)
}

// abort is called when the handler panicked.
func (w *responseWriter) abort() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.finished {
		return
	}
	w.finished = true
	if w.stream != nil {
		w.stream.CloseWithError(io.ErrUnexpectedEOF)
		return
	}
	h := http.Header{"Content-Type": {"text/plain; charset=utf-8"}}
	w.complete(http.StatusInternalServerError, h, []byte("internal error\n"))
}

// hopByHop headers are meaningless for the webview and dropped.
var hopByHop = map[string]bool{
	"Connection": true, "Keep-Alive": true, "Proxy-Connection": true,
	"Transfer-Encoding": true, "Upgrade": true, "Te": true, "Trailer": true,
}

// formatHeaders renders h as the CRLF-separated string that
// ICoreWebView2Environment::CreateWebResourceResponse takes. Keys are sorted
// for deterministic output, each value gets its own line (Set-Cookie must not
// be joined) and CR/LF inside values are replaced by spaces.
func formatHeaders(h http.Header) string {
	keys := make([]string, 0, len(h))
	for k := range h {
		if hopByHop[http.CanonicalHeaderKey(k)] || k == "" {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var sb strings.Builder
	for _, k := range keys {
		name := cleanHeaderToken(k)
		for _, v := range h[k] {
			sb.WriteString(name)
			sb.WriteString(": ")
			sb.WriteString(cleanHeaderValue(v))
			sb.WriteString("\r\n")
		}
	}
	return sb.String()
}

var crlfReplacer = strings.NewReplacer("\r", " ", "\n", " ")

func cleanHeaderValue(v string) string { return crlfReplacer.Replace(v) }

func cleanHeaderToken(k string) string {
	return strings.NewReplacer("\r", "", "\n", "", ":", "", " ", "").Replace(k)
}

// reasonPhrase returns a non-empty reason for a status code.
func reasonPhrase(code int) string {
	if s := http.StatusText(code); s != "" {
		return s
	}
	switch {
	case code >= 500:
		return "Server Error"
	case code >= 400:
		return "Client Error"
	case code >= 300:
		return "Redirect"
	}
	return "OK"
}

// buildRequest assembles the *http.Request the handler sees.
func buildRequest(ctx context.Context, method, rawURI string, headers [][2]string, body []byte) (*http.Request, error) {
	u, err := url.Parse(rawURI)
	if err != nil {
		return nil, err
	}
	if method == "" {
		method = http.MethodGet
	}
	var rd io.Reader = http.NoBody
	if len(body) > 0 {
		rd = bytes.NewReader(body)
	}
	r, err := http.NewRequestWithContext(ctx, method, rawURI, rd)
	if err != nil {
		return nil, err
	}
	r.RequestURI = u.RequestURI()
	r.RemoteAddr = "127.0.0.1:0"
	for _, kv := range headers {
		if strings.EqualFold(kv[0], "Host") {
			continue
		}
		r.Header.Add(kv[0], kv[1])
	}
	r.ContentLength = int64(len(body))
	return r, nil
}
