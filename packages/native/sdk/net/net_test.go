package sdknet

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	sdkos "github.com/kratejs/krate/packages/native/sdk/os"
)

func TestSecretNotForwardedAcrossHosts(t *testing.T) {
	getSecret = func(context.Context, string) (string, error) { return "tok", nil }
	defer func() { getSecret = sdkos.SecretGet }()

	var otherSaw, sameSaw string
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { otherSaw = r.Header.Get("X-Api-Key") }))
	defer other.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/to-other":
			http.Redirect(w, r, other.URL, http.StatusFound)
		case "/to-self":
			http.Redirect(w, r, "/final", http.StatusFound)
		default:
			sameSaw = r.Header.Get("X-Api-Key")
		}
	}))
	defer origin.Close()

	do := func(path string) {
		req, _ := http.NewRequest("GET", origin.URL+path, nil)
		if err := WithSecret(context.Background(), req, "k", "X-Api-Key"); err != nil {
			t.Fatal(err)
		}
		resp, err := Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
	}
	do("/to-other")
	if otherSaw != "" {
		t.Fatalf("secret leaked to another host: %q", otherSaw)
	}
	do("/to-self")
	if sameSaw != "tok" {
		t.Fatalf("same-host redirect lost the header: %q", sameSaw)
	}
}
