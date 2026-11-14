package darwin

import (
	"context"
	"io"
	"net/http"
	"testing"
)

func TestNavAllowed(t *testing.T) {
	cases := []struct {
		scheme, host string
		main, want   bool
	}{
		{"krate", "app", true, true},
		{"KRATE", "APP", true, true},
		{"krate", "other", true, false},
		{"https", "example.com", true, false},
		{"file", "", true, false},
		{"data", "", true, false},
		{"about", "", true, false},
		{"about", "", false, true},
		{"blob", "", false, true},
		{"https", "example.com", false, false},
		{"javascript", "", false, false},
	}
	for _, c := range cases {
		if got := navAllowed(c.scheme, c.host, c.main); got != c.want {
			t.Errorf("navAllowed(%q,%q,%v) = %v", c.scheme, c.host, c.main, got)
		}
	}
}

func TestNewHTTPRequest(t *testing.T) {
	r, err := newHTTPRequest(context.Background(), "", "krate://app/a/b%20c?q=1", map[string]string{"accept": "*/*"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if r.Method != "GET" || r.URL.Path != "/a/b c" || r.URL.Host != "" || r.Host != "app" || r.Header.Get("Accept") != "*/*" {
		t.Fatalf("request = %+v", r)
	}
	if r.Body != http.NoBody || r.ContentLength != 0 {
		t.Fatalf("body = %v len %d", r.Body, r.ContentLength)
	}
	r, err = newHTTPRequest(context.Background(), "POST", "krate://app/x", nil, []byte("abc"))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(r.Body)
	if string(b) != "abc" || r.ContentLength != 3 || r.Header.Get("Content-Length") != "3" {
		t.Fatalf("body %q len %d", b, r.ContentLength)
	}
}

func TestNewHTTPRequestRejectsForeignOrigin(t *testing.T) {
	for _, u := range []string{"https://app/x", "krate://other/x", "krate:///x", "%zz"} {
		if _, err := newHTTPRequest(context.Background(), "GET", u, nil, nil); err == nil {
			t.Errorf("%q accepted", u)
		}
	}
}
