package build

import (
	"net/http/httptest"
	"testing"
)

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
