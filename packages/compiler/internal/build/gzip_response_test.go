package build

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func gzipTestBody(n int) []byte {
	var b strings.Builder
	for i := 0; b.Len() < n; i++ {
		b.WriteString("<div class=\"row\"><span>item ")
		b.WriteString(strings.Repeat("x", i%17))
		b.WriteString("</span></div>\n")
	}
	return []byte(b.String())
}

func TestGzipMiddlewareRoundTripsWithPooledWriters(t *testing.T) {
	h := gzipMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Content-Length", "1")
		_, _ = w.Write(gzipTestBody(1000 + len(r.URL.Path)*500))
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		_, _ = w.Write([]byte("</tail>"))
	}))
	for i := 0; i < 20; i++ {
		req := httptest.NewRequest("GET", "/"+strings.Repeat("p", i), nil)
		req.Header.Set("Accept-Encoding", "gzip")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		res := rec.Result()
		if res.Header.Get("Content-Encoding") != "gzip" || res.Header.Get("Content-Length") != "" {
			t.Fatalf("headers: %v", res.Header)
		}
		if !strings.Contains(res.Header.Get("Vary"), "Accept-Encoding") {
			t.Fatalf("missing Vary: %v", res.Header)
		}
		zr, err := gzip.NewReader(res.Body)
		if err != nil {
			t.Fatalf("response %d: %v", i, err)
		}
		got, err := io.ReadAll(zr)
		if err != nil {
			t.Fatalf("response %d: %v", i, err)
		}
		want := append(gzipTestBody(1000+(i+1)*500), "</tail>"...)
		if !bytes.Equal(got, want) {
			t.Fatalf("response %d: body mismatch (%d vs %d bytes)", i, len(got), len(want))
		}
	}
}

func TestGzipMiddlewareSkipsPartialContent(t *testing.T) {
	h := gzipMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Header().Set("Content-Range", "bytes 0-4/10")
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write([]byte("hello"))
	}))
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusPartialContent || rec.Header().Get("Content-Encoding") != "" || rec.Body.String() != "hello" {
		t.Fatalf("partial response was compressed: %d %v %q", rec.Code, rec.Header(), rec.Body.String())
	}
}

func BenchmarkGzipResponse(b *testing.B) {
	body := gzipTestBody(50 << 10)
	h := gzipMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(body)
	}))
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	b.ReportAllocs()
	b.SetBytes(int64(len(body)))
	for i := 0; i < b.N; i++ {
		h.ServeHTTP(discardResponse{h: http.Header{}}, req)
	}
}

type discardResponse struct{ h http.Header }

func (d discardResponse) Header() http.Header         { return d.h }
func (d discardResponse) Write(p []byte) (int, error) { return len(p), nil }
func (d discardResponse) WriteHeader(int)             {}
