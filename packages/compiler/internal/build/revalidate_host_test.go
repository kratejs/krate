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
	"time"
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

// fakeSidecar records authenticated cache-invalidate calls.
type fakeSidecar struct {
	mu     sync.Mutex
	bodies []map[string]any
	fail   bool
	secret string
}

func (f *fakeSidecar) handler(t *testing.T) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get(internalSecretHeader) != f.secret {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.mu.Lock()
		f.bodies = append(f.bodies, body)
		fail := f.fail
		f.mu.Unlock()
		if fail {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = io.WriteString(w, `{"error":"boom"}`)
			return
		}
		_, _ = io.WriteString(w, `{"ok":true}`)
	}
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

func TestRevalidateHostResolvesPathsAndFansOut(t *testing.T) {
	h := newTestRevalidateHost(t)
	side := &fakeSidecar{secret: h.secret}
	srv := httptest.NewServer(side.handler(t))
	defer srv.Close()
	h.ssrBase = srv.URL

	// Data cache entries tagged "posts" go with the tag.
	h.data.Set("k1", "v", time.Minute, []string{"posts"})
	h.data.Set("k2", "v", time.Minute, []string{"other"})

	// Go region renders of /blog/[slug] are dropped with the path.
	g := newGoAPISupervisor("", "", 0)
	g.regionCache["r1"] = regionCacheEntry{html: "x", exp: time.Now().Add(time.Minute), route: "/blog/[slug]"}
	g.regionCache["r2"] = regionCacheEntry{html: "y", exp: time.Now().Add(time.Minute), route: "/dash"}
	h.SetGoAPI(g)

	res, err := h.Apply(revalidateRequest{
		Paths: []revalidatePathReq{
			{Path: "/blog/hello?x=1", Type: "page"},
			{Path: "/blog/[slug]"},
			{Path: "/about"},
			{Path: "/docs/intro"},
			{Path: "/missing"},
		},
		Tags: []string{"posts"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(side.bodies) != 1 {
		t.Fatalf("sidecar calls = %d, want 1", len(side.bodies))
	}
	targets := side.bodies[0]["targets"].([]any)
	first := targets[0].(map[string]any)
	if first["route"] != "/blog/[slug]" || first["params"].(map[string]any)["slug"] != "hello" {
		t.Fatalf("concrete URL target = %v", first)
	}
	second := targets[1].(map[string]any)
	if second["route"] != "/blog/[slug]" || second["params"] != nil {
		t.Fatalf("pattern target must cover every variant, got %v", second)
	}
	if tags := side.bodies[0]["tags"].([]any); len(tags) != 1 || tags[0] != "posts" {
		t.Fatalf("tags = %v", tags)
	}
	if _, ok := h.data.Get("k1"); ok {
		t.Fatal("tagged data entry survived revalidateTag")
	}
	if _, ok := h.data.Get("k2"); !ok {
		t.Fatal("untagged data entry was dropped")
	}
	if _, ok := g.regionCache["r1"]; ok {
		t.Fatal("Go region of the revalidated route survived")
	}
	if _, ok := g.regionCache["r2"]; !ok {
		t.Fatal("Go region of another route was dropped")
	}

	skipped := map[string]string{}
	for _, s := range res.Skipped {
		skipped[s.Target] = s.Reason
	}
	if !strings.Contains(skipped["/about"], "static (SSG)") {
		t.Fatalf("/about must be skipped as static, got %v", res.Skipped)
	}
	if !strings.Contains(skipped["/docs/intro"], "static (SSG)") {
		t.Fatalf("/docs/intro (SSG dynamic route) must be skipped, got %v", res.Skipped)
	}
	if !strings.Contains(skipped["/missing"], "no page matches") {
		t.Fatalf("/missing must be skipped as unknown, got %v", res.Skipped)
	}
	kinds := map[string]string{}
	for _, r := range res.Revalidated {
		kinds[r.Target] = r.Kind
	}
	if kinds["/blog/hello?x=1"] != "path" || kinds["posts"] != "tag" {
		t.Fatalf("revalidated = %+v", res.Revalidated)
	}

	// layout: every request-time page under /blog, static ones skipped.
	side.bodies = nil
	res, err = h.Apply(revalidateRequest{Paths: []revalidatePathReq{{Path: "/blog", Type: "layout"}}})
	if err != nil {
		t.Fatal(err)
	}
	routes := map[string]bool{}
	for _, tg := range side.bodies[0]["targets"].([]any) {
		routes[tg.(map[string]any)["route"].(string)] = true
	}
	if !routes["/blog"] || !routes["/blog/[slug]"] || routes["/dash"] || len(routes) != 2 {
		t.Fatalf("layout targets = %v", routes)
	}
	if len(res.Skipped) != 1 || res.Skipped[0].Target != "/blog/archive" {
		t.Fatalf("layout skipped = %+v", res.Skipped)
	}

	// Only static targets: nothing is sent to the sidecar.
	side.bodies = nil
	if _, err := h.Apply(revalidateRequest{Paths: []revalidatePathReq{{Path: "/about"}}}); err != nil {
		t.Fatal(err)
	}
	if len(side.bodies) != 0 {
		t.Fatalf("static-only revalidation reached the sidecar: %v", side.bodies)
	}

	// A sidecar failure is an error (the caller must not report success).
	side.fail = true
	if _, err := h.Apply(revalidateRequest{Tags: []string{"posts"}}); err == nil {
		t.Fatal("want an error when the SSR renderer fails to invalidate")
	}
	code, out := postRevalidate(t, h, h.secret, `{"tags":["posts"]}`)
	if code != http.StatusBadGateway || !strings.Contains(out["error"].(string), "SSR renderer") {
		t.Fatalf("failed fan-out: status %d %v", code, out)
	}
}

func TestRevalidateHostQuickJSBridge(t *testing.T) {
	h := newTestRevalidateHost(t)
	var res map[string]any
	if err := json.Unmarshal([]byte(h.ApplyJSON(`{"paths":[{"path":"/about","type":"page"}],"tags":[]}`)), &res); err != nil {
		t.Fatal(err)
	}
	if sk := res["skipped"].([]any); len(sk) != 1 {
		t.Fatalf("skipped = %v", res)
	}
	if !strings.Contains(h.ApplyJSON(`nope`), "error") {
		t.Fatal("invalid payload must return an error")
	}
	hooks := h.CacheHooks()
	if hooks.Revalidate == nil || hooks.Data != h.data {
		t.Fatal("CacheHooks must expose the host's revalidation and data cache")
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

func TestRouteUnderAndNormalize(t *testing.T) {
	cases := []struct {
		route, prefix string
		want          bool
	}{
		{"/blog", "/blog", true},
		{"/blog/[slug]", "/blog", true},
		{"/blog/[slug]/comments", "/blog/hello", true},
		{"/blogs", "/blog", false},
		{"/about", "/", true},
		{"/a", "/a/b", false},
	}
	for _, c := range cases {
		if got := routeUnder(c.route, c.prefix); got != c.want {
			t.Errorf("routeUnder(%q, %q) = %v, want %v", c.route, c.prefix, got, c.want)
		}
	}
	if p, _ := normalizeRevalidatePath("/blog/a/?q=1#x"); p != "/blog/a" {
		t.Errorf("normalize = %q", p)
	}
	if _, err := normalizeRevalidatePath("blog"); err == nil {
		t.Error("relative path must be rejected")
	}
}

func TestGoRegionCacheBoundAndInvalidation(t *testing.T) {
	g := newGoAPISupervisor("", "", 0)
	for i := 0; i < goRegionCacheMax+50; i++ {
		g.mu.Lock()
		g.regionCacheSetLocked(strings.Repeat("k", 1)+string(rune('a'+i%26))+strings.Repeat("x", i), regionCacheEntry{html: "h", exp: time.Now().Add(time.Minute), route: "/r"})
		g.mu.Unlock()
	}
	if n := len(g.regionCache); n > goRegionCacheMax {
		t.Fatalf("region cache grew to %d (> %d)", n, goRegionCacheMax)
	}
	if n := g.InvalidateRoutes([]string{"/r"}); n == 0 || len(g.regionCache) != 0 {
		t.Fatalf("InvalidateRoutes removed %d, %d left", n, len(g.regionCache))
	}
	if r := regionRoute(goRenderRegion{Request: `{"url":"/video/[id]"}`}); r != "/video/[id]" {
		t.Fatalf("regionRoute from request = %q", r)
	}
}
