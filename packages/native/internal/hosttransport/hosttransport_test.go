package hosttransport

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The handshake: a tcp server rejects requests without the token and serves
// those that carry it; the transport adds the header by itself.
func TestTCPHandshake(t *testing.T) {
	ln, ep, err := ListenTCP()
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: Guard(ep, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := io.WriteString(w, "hello"); err != nil {
			t.Error(err)
		}
	}))}
	go func() { _ = srv.Serve(ln) }()
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

func TestGuardEmptyTokenRejects(t *testing.T) {
	h := Guard(Endpoint{Network: TCP, Addr: "127.0.0.1:1"}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if rec.Code != 401 {
		t.Errorf("empty configured token must reject, got %d", rec.Code)
	}
}
