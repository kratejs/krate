package bridge

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kratejs/krate/packages/native/internal/hosttransport"
)

func TestRemoteTCPSendsToken(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get(hosttransport.TokenHeader)
		w.WriteHeader(204)
	}))
	defer srv.Close()
	addr := "tcp:" + srv.Listener.Addr().String() + "?token=tok123"
	r, err := newRemote(addr)
	if err != nil {
		t.Fatal(err)
	}
	defer r.close()
	resp, err := r.client.Get(r.base + "/x")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if got != "tok123" {
		t.Errorf("token header = %q", got)
	}
	if _, err := newRemote("tcp:example.com:80"); err == nil {
		t.Error("a non-loopback tcp address must be rejected")
	}
}
