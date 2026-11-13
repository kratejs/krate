package build

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// newTestRevalidateHost builds a host over a manifest with static, ISR, SSR
// and dynamic routes.
func newTestRevalidateHost(t *testing.T) *revalidateHost {
	t.Helper()
	out := t.TempDir()
	m := Manifest{Pages: []PageMeta{
		{Route: "/", Mode: RenderSSG},
		{Route: "/about", Mode: RenderSSG},
		{Route: "/blog", Mode: RenderISR, Revalidate: 60},
		{Route: "/blog/[slug]", Mode: RenderISR, Revalidate: 60},
		{Route: "/blog/archive", Mode: RenderSSG},
		{Route: "/dash", Mode: RenderSSR},
		{Route: "/docs/[page]", Mode: RenderSSG},
	}}
	data, _ := json.Marshal(m)
	if err := os.WriteFile(filepath.Join(out, "manifest.json"), data, 0644); err != nil {
		t.Fatal(err)
	}
	h, err := newRevalidateHost(out)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(h.Close)
	return h
}

func postRevalidate(t *testing.T, h *revalidateHost, secret, body string) (int, map[string]any) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, h.url+revalidateEndpoint, strings.NewReader(body))
	if secret != "" {
		req.Header.Set(internalSecretHeader, secret)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

func TestRevalidateHostAuth(t *testing.T) {
	h := newTestRevalidateHost(t)
	if !strings.HasPrefix(h.url, "http://127.0.0.1:") {
		t.Fatalf("internal endpoint must be loopback-only, got %s", h.url)
	}
	if len(h.secret) < 32 {
		t.Fatalf("secret too short: %d", len(h.secret))
	}
	if code, _ := postRevalidate(t, h, "", `{"tags":["a"]}`); code != http.StatusForbidden {
		t.Fatalf("no secret: status %d, want 403", code)
	}
	if code, _ := postRevalidate(t, h, h.secret+"x", `{"tags":["a"]}`); code != http.StatusForbidden {
		t.Fatalf("wrong secret: status %d, want 403", code)
	}
	resp, err := http.Get(h.url + revalidateEndpoint)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("GET: status %d, want 405", resp.StatusCode)
	}
	if code, _ := postRevalidate(t, h, h.secret, `not json`); code != http.StatusBadRequest {
		t.Fatalf("bad body: status %d, want 400", code)
	}
	code, out := postRevalidate(t, h, h.secret, `{"tags":["a"]}`)
	if code != http.StatusOK {
		t.Fatalf("authorized: status %d (%v)", code, out)
	}
	env := h.Env()
	if env["KRATE_HOST_URL"] != h.url || env["KRATE_INTERNAL_SECRET"] != h.secret {
		t.Fatalf("Env() = %v", env)
	}
}

func TestSSRInternalProxyLockdown(t *testing.T) {
	h := newTestRevalidateHost(t)
	var hits []string
	var mu sync.Mutex
	sidecar := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hits = append(hits, r.URL.Path)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	defer sidecar.Close()
	port := sidecar.Listener.Addr().(*net.TCPAddr).Port
	public := httptest.NewServer(ssrInternalProxy(h, port))
	defer public.Close()

	for _, p := range []string{"/__krate/ssr/revalidate", "/__krate/ssr/refresh", "/__krate/ssr/invalidate", "/__krate/ssr/cache-invalidate"} {
		resp, err := http.Post(public.URL+p, "application/json", strings.NewReader(`{"route":"/"}`))
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusForbidden {
			t.Fatalf("unauthenticated public POST %s: status %d, want 403", p, resp.StatusCode)
		}
	}
	resp, err := http.Get(public.URL + "/__krate/ssr/health")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("health: status %d", resp.StatusCode)
	}
	req, _ := http.NewRequest(http.MethodPost, public.URL+"/__krate/ssr/revalidate", strings.NewReader(`{"route":"/"}`))
	req.Header.Set(internalSecretHeader, h.secret)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("authenticated POST: status %d", resp.StatusCode)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(hits) != 2 || hits[0] != "/__krate/ssr/health" || hits[1] != "/__krate/ssr/revalidate" {
		t.Fatalf("sidecar saw %v; rejected requests must not reach it", hits)
	}
}
