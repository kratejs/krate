//go:build linux

package linux

import (
	"io"
	"net/http"
	"strconv"
	"time"
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
