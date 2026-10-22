package build

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSameOriginActionRequest(t *testing.T) {
	cases := []struct {
		name   string
		origin string
		site   string
		want   bool
	}{
		{name: "same origin", origin: "https://example.com", want: true},
		{name: "same-origin fetch metadata", site: "same-origin", want: true},
		{name: "cross-site metadata", site: "cross-site", want: false},
		{name: "cross-site origin", origin: "https://evil.example", want: false},
		{name: "malformed origin", origin: "not a url", want: false},
		{name: "no browser headers", want: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("POST", "https://example.com/__krate/action/a1", nil)
			r.Host = "example.com"
			if tc.origin != "" {
				r.Header.Set("Origin", tc.origin)
			}
			if tc.site != "" {
				r.Header.Set("Sec-Fetch-Site", tc.site)
			}
			if got := sameOriginActionRequest(r); got != tc.want {
				t.Fatalf("sameOriginActionRequest() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestSanitizeActionReturn(t *testing.T) {
	r := httptest.NewRequest("POST", "https://example.com/__krate/action/a1", nil)
	r.Host = "example.com"
	cases := map[string]string{
		"/react-actions/":               "/react-actions/",
		"https://example.com/next?a=1":  "/next?a=1",
		"//evil.example/path":           "",
		"https://evil.example/path":     "",
		"javascript:alert(1)":           "",
		"/ok\r\nLocation: https://evil": "",
	}
	for in, want := range cases {
		if got := sanitizeActionReturn(in, r); got != want {
			t.Errorf("sanitizeActionReturn(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestActionSidecarHandlesActions(t *testing.T) {
	for _, script := range []string{apiServerScriptContent, apiServerBunScriptContent} {
		for _, marker := range []string{"__krate/action/", "runAction", "formData"} {
			if !strings.Contains(script, marker) {
				t.Errorf("sidecar script missing %q", marker)
			}
		}
	}
}
