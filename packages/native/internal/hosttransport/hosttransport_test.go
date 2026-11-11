package hosttransport

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFor(t *testing.T) {
	cases := []struct {
		goos string
		js   bool
		want string
	}{
		{"windows", true, TCP},
		{"windows", false, Unix},
		{"linux", true, Unix},
		{"darwin", true, Unix},
		{"linux", false, Unix},
	}
	for _, c := range cases {
		if got := For(c.goos, c.js); got != c.want {
			t.Errorf("For(%s, %v) = %s, want %s", c.goos, c.js, got, c.want)
		}
	}
}

func TestParseRoundTrip(t *testing.T) {
	for _, s := range []string{
		"unix:/tmp/a.sock",
		`unix:C:\Users\x\a.sock`,
		"tcp:127.0.0.1:4000?token=abc123",
		"tcp:[::1]:4000?token=abc123",
		"tcp:127.0.0.1:0",
	} {
		ep, err := Parse(s)
		if err != nil {
			t.Errorf("Parse(%q): %v", s, err)
			continue
		}
		if ep.String() != s {
			t.Errorf("round trip %q -> %q", s, ep.String())
		}
	}
	ep, err := Parse(`C:\cache\g001.sock`)
	if err != nil || ep.Network != Unix || ep.Addr != `C:\cache\g001.sock` {
		t.Errorf("bare path = %+v, %v", ep, err)
	}
}

func TestParseRejects(t *testing.T) {
	for _, s := range []string{
		"", "unix:", "tcp:example.com:80", "tcp:0.0.0.0:80", "tcp:10.0.0.5:80",
		"tcp:127.0.0.1", "tcp:127.0.0.1:99999", "tcp:127.0.0.1:x",
	} {
		if ep, err := Parse(s); err == nil {
			t.Errorf("Parse(%q) = %+v, want an error", s, ep)
		}
	}
}

func TestNewTokenUnique(t *testing.T) {
	a, _ := NewToken()
	b, _ := NewToken()
	if len(a) != 32 || a == b {
		t.Errorf("tokens %q %q", a, b)
	}
}

func TestReadyAddr(t *testing.T) {
	cases := []struct {
		line string
		addr string
		ok   bool
	}{
		{"ready", "", true},
		{"ready tcp:127.0.0.1:5000", "127.0.0.1:5000", true},
		{"ready tcp:evil.example:5000", "", false},
		{"ready tcp:0.0.0.0:5000", "", false},
		{"ready now", "", false},
		{"not ready", "", false},
		{"", "", false},
	}
	for _, c := range cases {
		addr, ok := ReadyAddr(c.line)
		if addr != c.addr || ok != c.ok {
			t.Errorf("ReadyAddr(%q) = %q, %v", c.line, addr, ok)
		}
	}
	if got := ReadyLineFor(Endpoint{Network: TCP, Addr: "127.0.0.1:9"}); got != "ready tcp:127.0.0.1:9" {
		t.Errorf("ReadyLineFor = %q", got)
	}
	if got := ReadyLineFor(Endpoint{Network: Unix, Addr: "/x"}); got != "ready" {
		t.Errorf("ReadyLineFor unix = %q", got)
	}
}

// The handshake: a tcp server rejects requests without the token and serves
// those that carry it; the transport adds the header by itself.
func TestTCPHandshake(t *testing.T) {
	ln, ep, err := ListenTCP()
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: Guard(ep, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "hello")
	}))}
	go srv.Serve(ln)
	defer srv.Close()

	rt, tr := NewTransport(ep)
	defer tr.CloseIdleConnections()
	client := &http.Client{Transport: rt}
	resp, err := client.Get("http://child/x")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || string(b) != "hello" {
		t.Fatalf("with token: %d %q", resp.StatusCode, b)
	}

	bad := ep
	bad.Token = strings.Repeat("0", 32)
	rt2, tr2 := NewTransport(bad)
	defer tr2.CloseIdleConnections()
	resp, err = (&http.Client{Transport: rt2}).Get("http://child/x")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 401 {
		t.Errorf("wrong token: status %d, want 401", resp.StatusCode)
	}

	// No token at all.
	plain := &http.Transport{DialContext: tr.DialContext}
	resp, err = (&http.Client{Transport: plain}).Get("http://child/x")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 401 {
		t.Errorf("no token: status %d, want 401", resp.StatusCode)
	}
}

func TestGuardUnixPassesThrough(t *testing.T) {
	h := Guard(Endpoint{Network: Unix, Addr: "/x"}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(204)
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if rec.Code != 204 {
		t.Errorf("code %d", rec.Code)
	}
}

func TestGuardEmptyTokenRejects(t *testing.T) {
	h := Guard(Endpoint{Network: TCP, Addr: "127.0.0.1:1"}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if rec.Code != 401 {
		t.Errorf("empty configured token must reject, got %d", rec.Code)
	}
}
