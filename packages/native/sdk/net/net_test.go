package sdknet

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	sdkos "github.com/kratejs/krate/packages/native/sdk/os"
)

func TestClientTimeouts(t *testing.T) {
	c := Client()
	if c.Timeout != DefaultTimeout || c.Transport == nil {
		t.Fatalf("%+v", c)
	}
	if ClientWithTimeout(0).Timeout != 0 {
		t.Fatal("zero timeout not honored")
	}
	// A server that never answers is cut off.
	block := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-block }))
	defer srv.Close()
	defer close(block)
	short := ClientWithTimeout(100 * time.Millisecond)
	start := time.Now()
	if _, err := short.Get(srv.URL); err == nil {
		t.Fatal("hung request returned")
	}
	if time.Since(start) > 3*time.Second {
		t.Fatal("timeout not applied")
	}
}

func TestWithSecret(t *testing.T) {
	secrets := map[string]string{"api": "Bearer abc"}
	getSecret = func(_ context.Context, k string) (string, error) {
		if v, ok := secrets[k]; ok {
			return v, nil
		}
		return "", sdkos.ErrSecretNotFound
	}
	defer func() { getSecret = sdkos.SecretGet }()

	var gotHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { gotHeader = r.Header.Get("Authorization") }))
	defer srv.Close()

	req, _ := http.NewRequest("GET", srv.URL, nil)
	if err := WithSecret(context.Background(), req, "api", "Authorization"); err != nil {
		t.Fatal(err)
	}
	resp, err := Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if gotHeader != "Bearer abc" {
		t.Fatalf("server saw %q", gotHeader)
	}

	if err := WithSecret(context.Background(), req, "missing", "X-Key"); !errors.Is(err, sdkos.ErrSecretNotFound) {
		t.Fatalf("%v", err)
	}
	if err := WithSecret(context.Background(), req, "api", "bad header"); err == nil {
		t.Fatal("invalid header name accepted")
	}
	secrets["nl"] = "a\r\nInjected: 1"
	if err := WithSecret(context.Background(), req, "nl", "X-Key"); err == nil {
		t.Fatal("secret with CRLF accepted")
	}
}

func TestSecretNotForwardedAcrossHosts(t *testing.T) {
	getSecret = func(context.Context, string) (string, error) { return "tok", nil }
	defer func() { getSecret = sdkos.SecretGet }()

	var otherSaw, sameSaw string
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { otherSaw = r.Header.Get("X-Api-Key") }))
	defer other.Close()
	var origin *httptest.Server
	origin = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
