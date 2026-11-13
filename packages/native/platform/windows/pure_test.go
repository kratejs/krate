package windows

import (
	"context"
	"io"
	"net/http"
	"sync"
	"testing"
)

func TestBuildRequest(t *testing.T) {
	r, err := buildRequest(context.Background(), "POST", "https://app.localhost/echo?x=1",
		[][2]string{{"Content-Type", "application/octet-stream"}, {"Host", "evil"}}, []byte("abc"))
	if err != nil {
		t.Fatal(err)
	}
	if r.Method != "POST" || r.URL.Path != "/echo" || r.URL.Query().Get("x") != "1" ||
		r.RequestURI != "/echo?x=1" || r.Host != "app.localhost" ||
		r.Header.Get("Content-Type") != "application/octet-stream" || r.ContentLength != 3 {
		t.Fatalf("bad request %+v", r)
	}
	b, _ := io.ReadAll(r.Body)
	if string(b) != "abc" {
		t.Fatalf("body %q", b)
	}
	g, err := buildRequest(context.Background(), "", "https://app.localhost/", nil, nil)
	if err != nil || g.Method != "GET" || g.Body != http.NoBody {
		t.Fatalf("%v %+v", err, g)
	}
	if _, err := buildRequest(context.Background(), "GET", "http://[bad", nil, nil); err == nil {
		t.Fatal("bad uri accepted")
	}
}

type captured struct {
	mu       sync.Mutex
	commits  int
	status   int
	header   http.Header
	stream   *blockBuf
	done     int
	body     []byte
	doneCode int
}

func newCaptured() (*captured, *responseWriter, func()) {
	c := &captured{}
	w := newResponseWriter(false,
		func(st int, h http.Header, s *blockBuf) {
			c.mu.Lock()
			c.commits++
			c.status, c.header, c.stream = st, h, s
			c.mu.Unlock()
		},
		func(st int, h http.Header, b []byte) {
			c.mu.Lock()
			c.done++
			c.doneCode, c.header, c.body = st, h, append([]byte(nil), b...)
			c.mu.Unlock()
		})
	return c, w, w.finish
}

func TestResponseWriterFlushStreams(t *testing.T) {
	c, w, fin := newCaptured()
	if _, err := io.WriteString(w, "one"); err != nil {
		t.Fatal(err)
	}
	w.Flush()
	if c.commits != 1 || c.stream == nil {
		t.Fatal("Flush must commit")
	}
	p := make([]byte, 16)
	if n, _ := c.stream.Read(p); string(p[:n]) != "one" {
		t.Fatalf("first chunk %q", p[:n])
	}
	if _, err := io.WriteString(w, "two"); err != nil {
		t.Fatal(err)
	}
	w.Flush()
	if n, _ := c.stream.Read(p); string(p[:n]) != "two" {
		t.Fatalf("second chunk %q", p[:n])
	}
	fin()
	if _, err := c.stream.Read(p); err != io.EOF {
		t.Fatalf("want EOF, got %v", err)
	}
	if c.commits != 1 || c.done != 0 {
		t.Fatalf("commits=%d done=%d", c.commits, c.done)
	}
}

func TestURLHelpers(t *testing.T) {
	for in, want := range map[string]string{
		"": "https://app.localhost/", "/": "https://app.localhost/",
		"a/b": "https://app.localhost/a/b", "/x?y=1": "https://app.localhost/x?y=1",
	} {
		if got := navURL(in); got != want {
			t.Errorf("navURL(%q) = %q", in, got)
		}
	}
	ok := []string{"https://app.localhost/", "https://app.localhost/a?b#c"}
	bad := []string{
		"http://app.localhost/", "https://example.com/", "https://app.localhost.evil.com/",
		"https://app.localhost:8443/", "https://user@app.localhost/", "about:blank",
		"file:///c:/x", "javascript:alert(1)", "", "https://evil.com/?https://app.localhost/",
	}
	for _, s := range ok {
		if !allowedNavigation(s) {
			t.Errorf("%q must be allowed", s)
		}
	}
	for _, s := range bad {
		if allowedNavigation(s) {
			t.Errorf("%q must be blocked", s)
		}
	}
}

func TestAllowedFrameNavigation(t *testing.T) {
	for _, s := range []string{"https://app.localhost/x", "about:blank", "about:srcdoc"} {
		if !allowedFrameNavigation(s) {
			t.Errorf("%q must be allowed in a frame", s)
		}
	}
	for _, s := range []string{"https://example.com/", "javascript:alert(1)", "data:text/html,x", "file:///c:/x", "http://app.localhost/", ""} {
		if allowedFrameNavigation(s) {
			t.Errorf("%q must be blocked in a frame", s)
		}
	}
}
