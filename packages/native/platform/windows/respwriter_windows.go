package windows

import (
	"io"
	"net/http"
	"sort"
	"strings"
)

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
