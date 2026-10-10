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
	rr := streamRegionPage(rec, rec, absOut, route, sidecar.port, nil, nil, nil)
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

func TestStreamRegionPageCoarsePageRegion(t *testing.T) {
	absOut := t.TempDir()
	// Dynamic-route ISR/SSR pages: the Go handler resolves the concrete URL
	// (/video/abc) to the CANONICAL pattern route (/video/[id]) before calling
	// streamRegionPage, so the shell lives under the pattern dir and params are
	// forwarded separately.
	route := "/video/[id]"

	// SSR/ISR shell: the page body is wrapped in a coarse suspense marker whose
	// id is "page". The baked content is the stale/placeholder body.
	shell := `<!DOCTYPE html><html><head><title>Video: unknown</title></head><body><div id=root><main>` +
		`<!--suspense:page--><p>stale baked body</p><!--/suspense:page-->` +
		`</main></div></body></html>`
	rel := filepath.Join(absOut, "video", "[id]", "index.html")
	if err := os.MkdirAll(filepath.Dir(rel), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(rel, []byte(shell), 0644); err != nil {
		t.Fatal(err)
	}

	// Fake sidecar returns the coarse page-region render with a cache status
	// and a fresh title.
	sidecar := newFakeRegionsSidecar(t, []string{
		`{"type":"region","id":"page","kind":"page","status":200,"html":"<p>v-abc-fresh</p>","cacheStatus":"miss","title":"Video: abc"}`,
		`{"type":"end","count":1}`,
	})

	rec := &flushRecorder{ResponseRecorder: httptest.NewRecorder()}
	rec.Code = 200
	rec.Header().Set("Content-Type", "text/html; charset=utf-8")
	rr := streamRegionPage(rec, rec, absOut, route, sidecar.port, map[string]string{"id": "abc"}, nil, nil)
	if !rr.served {
		t.Fatal("streamRegionPage returned served=false for a shell with a page marker")
	}
	if rr.isISR != true || rr.cacheStatus != "miss" {
		t.Errorf("expected isISR+cacheStatus miss from page region frame, got isISR=%v status=%q", rr.isISR, rr.cacheStatus)
	}
	if rr.titleOverride != "Video: abc" {
		t.Errorf("expected title override, got %q", rr.titleOverride)
	}
	// Request must carry the CANONICAL route (for the sidecar's findPage), the
	// params (for the ISR variant cache key), and the regions list with kind page.
	body := sidecar.requestBody()
	for _, want := range []string{`"route":"/video/[id]"`, `"id":"abc"`, `"regions"`, `"kind":"page"`} {
		if !strings.Contains(body, want) {
			t.Errorf("sidecar request missing %s:\n%s", want, body)
		}
	}
	got := rec.Body.String()
	if !strings.Contains(got, `<p>v-abc-fresh</p>`) {
		t.Errorf("expected fresh page body spliced, got:\n%s", got)
	}
	if strings.Contains(got, "stale baked body") {
		t.Errorf("page body should replace baked body, got:\n%s", got)
	}
	if !strings.Contains(got, `<title>Video: abc</title>`) {
		t.Errorf("expected baked title replaced with fresh title, got:\n%s", got)
	}
}

func TestStreamRegionPagePageRegionErrorKeepsBakedBody(t *testing.T) {
	absOut := t.TempDir()
	route := "/isr"
	shell := `<html><body><main>` +
		`<!--suspense:page--><p>baked-isr-body</p><!--/suspense:page-->` +
		`</main></body></html>`
	rel := filepath.Join(absOut, "isr", "index.html")
	if err := os.MkdirAll(filepath.Dir(rel), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(rel, []byte(shell), 0644); err != nil {
		t.Fatal(err)
	}

	// Sidecar reports a 500 for the page region: the baked body must survive
	// (no empty body / no 200-with-blank-main), and renderErr surfaced.
	sidecar := newFakeRegionsSidecar(t, []string{
		`{"type":"region","id":"page","kind":"page","status":500,"html":"","error":"boom"}`,
		`{"type":"end","count":1}`,
	})

	rec := &flushRecorder{ResponseRecorder: httptest.NewRecorder()}
	rec.Code = 200
	rec.Header().Set("Content-Type", "text/html; charset=utf-8")
	rr := streamRegionPage(rec, rec, absOut, route, sidecar.port, nil, nil, nil)
	if !rr.served {
		t.Fatal("streamRegionPage returned served=false for a shell with a page marker")
	}
	if rr.renderErr == "" {
		t.Error("expected renderErr to be surfaced for a 500 page-region frame")
	}
	got := rec.Body.String()
	if !strings.Contains(got, "<p>baked-isr-body</p>") {
		t.Errorf("expected baked body preserved on page-region error, got:\n%s", got)
	}
}

func TestStreamRegionPageNoMarkersReturnsFalse(t *testing.T) {
	absOut := t.TempDir()
	rel := filepath.Join(absOut, "plain", "index.html")
	if err := os.MkdirAll(filepath.Dir(rel), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(rel, []byte("<html><body>static</body></html>"), 0644); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	rr := streamRegionPage(rec, nil, absOut, "/plain", 1, nil, nil, nil)
	if rr.served {
		t.Error("streamRegionPage should return served=false when the shell has no suspense markers")
	}
}

func TestStreamRegionPageKeepsFallbackOnError(t *testing.T) {
	absOut := t.TempDir()
	route := "/live"
	shell := `<div>` +
		`<!--suspense:9-1--><span>loading</span><!--/suspense:9-1-->` +
		`</div>`
	rel := filepath.Join(absOut, "live", "index.html")
	if err := os.MkdirAll(filepath.Dir(rel), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(rel, []byte(shell), 0644); err != nil {
		t.Fatal(err)
	}

	// Sidecar reports a region error frame - the baked fallback must remain.
	sidecar := newFakeRegionsSidecar(t, []string{
		`{"type":"error","id":"9-1","status":500,"error":"boom"}`,
		`{"type":"end","count":1}`,
	})

	rec := &flushRecorder{ResponseRecorder: httptest.NewRecorder()}
	rr := streamRegionPage(rec, rec, absOut, route, sidecar.port, nil, nil, nil)
	if !rr.served {
		t.Fatal("streamRegionPage returned served=false")
	}
	got := rec.Body.String()
	if !strings.Contains(got, "<span>loading</span>") {
		t.Errorf("expected baked fallback preserved on region error, got:\n%s", got)
	}
}

func TestStreamRegionPageMultipleRegions(t *testing.T) {
	absOut := t.TempDir()
	route := "/multi"
	shell := `<div>` +
		`<!--suspense:1-1--><span>a</span><!--/suspense:1-1-->` +
		`<p>middle</p>` +
		`<!--suspense:2-1--><span>b</span><!--/suspense:2-1-->` +
		`</div>`
	rel := filepath.Join(absOut, "multi", "index.html")
	if err := os.MkdirAll(filepath.Dir(rel), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(rel, []byte(shell), 0644); err != nil {
		t.Fatal(err)
	}

	sidecar := newFakeRegionsSidecar(t, []string{
		`{"type":"region","id":"1-1","status":200,"html":"<p>R1</p>"}`,
		`{"type":"region","id":"2-1","status":200,"html":"<p>R2</p>"}`,
		`{"type":"end","count":2}`,
	})

	rec := &flushRecorder{ResponseRecorder: httptest.NewRecorder()}
	rr := streamRegionPage(rec, rec, absOut, route, sidecar.port, nil, nil, nil)
	if !rr.served {
		t.Fatal("streamRegionPage returned served=false")
	}
	got := rec.Body.String()
	if !strings.Contains(got, "<p>R1</p>") || !strings.Contains(got, "<p>R2</p>") {
		t.Errorf("expected both region HTML blocks spliced, got:\n%s", got)
	}
	if !strings.Contains(got, "<p>middle</p>") {
		t.Errorf("expected static content between regions preserved, got:\n%s", got)
	}
	if strings.Contains(got, "<span>a</span>") || strings.Contains(got, "<span>b</span>") {
		t.Errorf("baked fallbacks should be replaced, got:\n%s", got)
	}
}

func TestStreamRegionPageStandaloneRegionMarker(t *testing.T) {
	absOut := t.TempDir()
	route := "/demo"

	// Standalone runtime component: an EMPTY splice slot between the two
	// <!--region:...--> markers (no baked fallback). streamRegionPage fills it.
	shell := `<div id=root>` +
		`<!--region:region-1.Widget_c0--><!--/region:region-1.Widget_c0-->` +
		`</div>`
	rel := filepath.Join(absOut, "demo", "index.html")
	if err := os.MkdirAll(filepath.Dir(rel), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(rel, []byte(shell), 0644); err != nil {
		t.Fatal(err)
	}

	sidecar := newFakeRegionsSidecar(t, []string{
		`{"type":"region","id":"region-1.Widget_c0","status":200,"html":"<p>hi x</p>"}`,
		`{"type":"end","count":1}`,
	})

	rec := &flushRecorder{ResponseRecorder: httptest.NewRecorder()}
	rr := streamRegionPage(rec, rec, absOut, route, sidecar.port, nil, nil, nil)
	if !rr.served {
		t.Fatal("streamRegionPage returned served=false for a shell with region markers")
	}
	got := rec.Body.String()
	if !strings.Contains(got, `<p>hi x</p>`) {
		t.Errorf("expected standalone region HTML spliced, got:\n%s", got)
	}
	if !strings.Contains(got, `<!--region:region-1.Widget_c0-->`) || !strings.Contains(got, `<!--/region:region-1.Widget_c0-->`) {
		t.Errorf("expected standalone region markers preserved, got:\n%s", got)
	}
}

var _ http.Flusher = (*flushRecorder)(nil)
