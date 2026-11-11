package sdk

import (
	"net/http"
	"testing"

	"github.com/kratejs/krate/packages/native/internal/hosttransport"
)

func TestServeParentTCP(t *testing.T) {
	h := NewHost()
	defer h.Close()
	addr, err := h.ServeParentTCP()
	if err != nil {
		t.Fatal(err)
	}
	if again, _ := h.ServeParentTCP(); again != addr {
		t.Errorf("second call = %q, want %q", again, addr)
	}
	ep, err := hosttransport.Parse(addr)
	if err != nil || ep.Network != hosttransport.TCP || ep.Token == "" {
		t.Fatalf("address %q: %+v %v", addr, ep, err)
	}
	status := func(token string) int {
		req, _ := http.NewRequest(http.MethodGet, "http://"+ep.Addr+"/__krate/signals", nil)
		if token != "" {
			req.Header.Set(hosttransport.TokenHeader, token)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	if c := status(""); c != 401 {
		t.Errorf("without a token: %d", c)
	}
	if c := status(ep.Token); c == 401 {
		t.Errorf("with the token: %d", c)
	}
}
