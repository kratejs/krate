package build

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestAPISidecarScriptsParse(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		requireE2E(t, "node is not installed")
	}
	for name, script := range map[string]string{
		"node": apiServerScriptContent,
		"bun":  apiServerBunScriptContent,
	} {
		path := filepath.Join(t.TempDir(), name+".mjs")
		if err := os.WriteFile(path, []byte(script), 0o644); err != nil {
			t.Fatal(err)
		}
		if out, err := exec.Command(node, "--check", path).CombinedOutput(); err != nil {
			t.Errorf("%s API sidecar does not parse: %v\n%s", name, err, out)
		}
	}
}

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
		for _, marker := range []string{"__krate/action/", "krateRunAction", "formData", "AsyncLocalStorage"} {
			if !strings.Contains(script, marker) {
				t.Errorf("sidecar script missing %q", marker)
			}
		}
	}
}

// The host and the sidecar both mark action responses no-store; the client
// must see the header once.
func TestActionHandlerSendsSingleCacheControl(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Cache-Control", "no-store")
		_, _ = io.WriteString(w, "{}")
	})
	h := actionHandler(next, 1<<20)
	req := httptest.NewRequest("POST", "http://example.com/__krate/action/x", strings.NewReader("{}"))
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	req.Header.Set("X-Krate-Action", "fetch")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if got := rec.Header().Values("Cache-Control"); len(got) != 1 || got[0] != "no-store" {
		t.Errorf("Cache-Control = %v, want a single no-store", got)
	}
}
