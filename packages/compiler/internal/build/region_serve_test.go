package build

import (
	"bufio"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// fakeRegionsSidecar is a bare TCP server that mimics the SSR sidecar's
// /__krate/regions endpoint: it reads one HTTP POST and replies with an HTTP
// response whose body is NDJSON frames (one per line).
type fakeRegionsSidecar struct {
	ln      net.Listener
	port    int
	frames  []string // raw NDJSON lines to send back
	mu      sync.Mutex
	gotBody string
}

func newFakeRegionsSidecar(t *testing.T, frames []string) *fakeRegionsSidecar {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("fake sidecar listen: %v", err)
	}
	s := &fakeRegionsSidecar{ln: ln, port: ln.Addr().(*net.TCPAddr).Port, frames: frames}
	go s.serve()
	t.Cleanup(func() { ln.Close() })
	return s
}

func (s *fakeRegionsSidecar) requestBody() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.gotBody
}

func (s *fakeRegionsSidecar) serve() {
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			return
		}
		go func(c net.Conn) {
			defer c.Close()
			br := bufio.NewReader(c)
			// Read the request line + headers until the blank line, tracking the
			// Content-Length. The body must be read AFTER the headers (bufio may
			// have buffered past them).
			contentLength := 0
			for {
				line, err := br.ReadString('\n')
				if err != nil {
					return
				}
				line = strings.TrimRight(line, "\r\n")
				if line == "" {
					break
				}
				if strings.HasPrefix(line, "Content-Length:") {
					_, _ = fmt.Sscanf(line, "Content-Length: %d", &contentLength)
				}
			}
			if contentLength > 0 {
				body := make([]byte, contentLength)
				ioReadFull(br, body)
				s.mu.Lock()
				s.gotBody = string(body)
				s.mu.Unlock()
			}
			resp := "HTTP/1.1 200 OK\r\nContent-Type: application/x-ndjson; charset=utf-8\r\nConnection: close\r\n\r\n"
			_, _ = c.Write([]byte(resp))
			for _, f := range s.frames {
				_, _ = c.Write([]byte(f + "\n"))
			}
		}(conn)
	}
}

func ioReadFull(br *bufio.Reader, buf []byte) {
	n := 0
	for n < len(buf) {
		m, _ := br.Read(buf[n:])
		if m == 0 {
			return
		}
		n += m
	}
}

// flushRecorder captures written output and records flushes.
type flushRecorder struct {
	*httptest.ResponseRecorder
	flushes int
}

func (f *flushRecorder) Flush() {
	f.flushes++
}

func TestStreamRegionPageSplicesFrames(t *testing.T) {
	absOut := t.TempDir()
	route := "/live"

	// Static shell with one suspense boundary whose baked fallback is the
	// loading text - matches what the compiler emits for a runtime primary.
	shell := `<!DOCTYPE html><html><body><div id=root>` +
		`<!--suspense:1-1--><span>loading</span><!--/suspense:1-1-->` +
		`</div></body></html>`
	rel := filepath.Join(absOut, "live", "index.html")
	if err := os.MkdirAll(filepath.Dir(rel), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(rel, []byte(shell), 0644); err != nil {
		t.Fatal(err)
	}

	// Fake sidecar returns the rendered region for id 1-1.
	sidecar := newFakeRegionsSidecar(t, []string{
		`{"type":"region","id":"1-1","status":200,"html":"<p>live world</p>"}`,
		`{"type":"end","count":1}`,
	})

	rec := &flushRecorder{ResponseRecorder: httptest.NewRecorder()}
	rr := streamRegionPage(rec, rec, newRegionShellCache(), absOut, route, sidecar.port, nil, nil, nil, nil, nil, false, 0, "")
	if !rr.served {
		t.Fatal("streamRegionPage returned served=false for a shell with suspense markers")
	}

	got := rec.Body.String()
	if !strings.Contains(got, `<p>live world</p>`) {
		t.Errorf("spliced output missing region HTML:\n%s", got)
	}
	if strings.Contains(got, "loading") {
		t.Errorf("spliced output still contains the baked fallback:\n%s", got)
	}
	if !strings.Contains(got, `<!--suspense:1-1-->`) || !strings.Contains(got, `<!--/suspense:1-1-->`) {
		t.Errorf("spliced output dropped the suspense markers:\n%s", got)
	}
	if rec.flushes < 1 {
		t.Errorf("expected at least one flush after the region splice, got %d", rec.flushes)
	}
	if !strings.Contains(sidecar.requestBody(), `"route":"/live"`) {
		t.Errorf("sidecar request body missing route:\n%s", sidecar.requestBody())
	}
}

var _ http.Flusher = (*flushRecorder)(nil)
