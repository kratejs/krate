package jsruntime

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/evanw/esbuild/pkg/api"
)

func TestDataCacheTagsExpiryAndBound(t *testing.T) {
	c := NewDataCache(3)
	c.Set("a", "1", time.Minute, []string{"t"})
	c.Set("b", "2", -1, []string{"t", "u"})
	c.Set("c", "3", time.Nanosecond, nil)
	c.Set("zero", "x", 0, nil)
	time.Sleep(time.Millisecond)
	if _, ok := c.Get("c"); ok {
		t.Fatal("expired entry served")
	}
	if _, ok := c.Get("zero"); ok {
		t.Fatal("ttl 0 must not be stored")
	}
	if n := c.InvalidateTags([]string{"u"}); n != 1 {
		t.Fatalf("InvalidateTags(u) = %d", n)
	}
	if _, ok := c.Get("a"); !ok {
		t.Fatal("entry without the tag was dropped")
	}
	for i := 0; i < 10; i++ {
		c.Set(fmt.Sprint("k", i), "v", time.Minute, nil)
	}
	if c.Len() > 3 {
		t.Fatalf("cache grew past its bound: %d", c.Len())
	}
}

func TestFetchCachePolicy(t *testing.T) {
	cases := []struct {
		method string
		opts   map[string]any
		ttl    time.Duration
		tags   int
	}{
		{"GET", map[string]any{"next": map[string]any{"revalidate": float64(30), "tags": []any{"a"}}}, 30 * time.Second, 1},
		{"GET", map[string]any{"cache": "force-cache"}, -1, 0},
		{"GET", map[string]any{"next": map[string]any{"revalidate": false}}, -1, 0},
		{"GET", map[string]any{"next": map[string]any{"revalidate": float64(0)}}, 0, 0},
		{"GET", map[string]any{"cache": "no-store", "next": map[string]any{"tags": []any{"a"}}}, 0, 1},
		{"POST", map[string]any{"next": map[string]any{"revalidate": float64(30)}}, 0, 0},
		{"GET", nil, 0, 0},
	}
	for i, c := range cases {
		ttl, tags := fetchCachePolicy(c.method, c.opts)
		if ttl != c.ttl || len(tags) != c.tags {
			t.Errorf("case %d: got (%v, %v), want (%v, %d tags)", i, ttl, tags, c.ttl, c.tags)
		}
	}
}

// TestQuickJSRouteUsesHostCache bundles a route that imports the real
// @krate/runtime cache module into the QuickJS IIFE shape and checks that
// unstable_cache, tagged fetch caching and revalidateTag go through the Go
// host hooks.
func TestQuickJSRouteUsesHostCache(t *testing.T) {
	cacheTS, err := filepath.Abs(filepath.Join("..", "..", "..", "runtime", "src", "cache.ts"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(cacheTS); err != nil {
		t.Skipf("runtime source not found: %v", err)
	}

	var hits int32
	data := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&hits, 1)
		_, _ = fmt.Fprintf(w, `{"n":%d}`, n)
	}))
	defer data.Close()

	src := fmt.Sprintf(`
import { unstable_cache, revalidateTag } from %q;
let calls = 0;
const getCount = unstable_cache(async () => ({ calls: ++calls }), ["count"], { tags: ["count"] });
export async function GET(request) {
  const url = new URL(request.url);
  if (url.searchParams.get("bust") === "1") revalidateTag("count");
  const cached = await getCount();
  const res = await fetch(%q, { next: { tags: ["remote"], revalidate: 60 } });
  const remote = await res.json();
  return Response.json({ cached: cached.calls, remote: remote.n });
}
`, cacheTS, data.URL)
	apiDir := filepath.Join(t.TempDir(), "api")
	res := api.Build(api.BuildOptions{
		Stdin:      &api.StdinOptions{Contents: src, Loader: api.LoaderTS, ResolveDir: filepath.Dir(cacheTS), Sourcefile: "route.ts"},
		Bundle:     true,
		Platform:   api.PlatformNode,
		Format:     api.FormatIIFE,
		GlobalName: "__krate_route",
		Outfile:    filepath.Join(apiDir, "c.js"),
		Write:      true,
		LogLevel:   api.LogLevelSilent,
	})
	if len(res.Errors) > 0 {
		t.Fatalf("bundle: %s", res.Errors[0].Text)
	}

	var revalidations []string
	dc := NewDataCache(0)
	rt := NewAPIRouteRuntime(apiDir)
	rt.SetPool(2, false)
	rt.SetCacheHooks(&CacheHooks{
		Data: dc,
		Revalidate: func(payload string) string {
			revalidations = append(revalidations, payload)
			var req struct {
				Tags []string `json:"tags"`
			}
			_ = json.Unmarshal([]byte(payload), &req)
			dc.InvalidateTags(req.Tags)
			return `{"revalidated":[],"skipped":[]}`
		},
	})

	get := func(q string) map[string]float64 {
		r := rt.Execute(APIRequest{Method: "GET", Path: "/api/c", URL: "http://localhost/api/c" + q})
		if r.Status != 200 {
			t.Fatalf("status %d body %s err %s", r.Status, r.Body, r.Error)
		}
		var out map[string]float64
		if err := json.Unmarshal([]byte(r.Body), &out); err != nil {
			t.Fatalf("body %q: %v", r.Body, err)
		}
		return out
	}
	first := get("")
	second := get("")
	if first["cached"] != 1 || second["cached"] != 1 {
		t.Fatalf("unstable_cache not served from the host cache: %v then %v", first, second)
	}
	if first["remote"] != 1 || second["remote"] != 1 || atomic.LoadInt32(&hits) != 1 {
		t.Fatalf("tagged fetch not cached: %v then %v (%d hits)", first, second, hits)
	}
	third := get("?bust=1")
	if len(revalidations) != 1 || !strings.Contains(revalidations[0], `"count"`) {
		t.Fatalf("revalidateTag did not reach the host: %v", revalidations)
	}
	if third["cached"] < 2 {
		t.Fatalf("revalidateTag did not invalidate unstable_cache: %v", third)
	}
	if third["remote"] != 1 {
		t.Fatalf("an unrelated tag dropped the fetch entry: %v", third)
	}
}

// TestQuickJSFetchOptions covers the embedded fetch polyfill: options reach
// the request (method, headers, body) and the response crosses the bridge.
func TestQuickJSFetchOptions(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b := new(strings.Builder)
		_, _ = fmt.Fprintf(b, "%s %s ", r.Method, r.Header.Get("X-Test"))
		buf := make([]byte, 64)
		n, _ := r.Body.Read(buf)
		b.Write(buf[:n])
		w.Header().Set("X-Echo", "1")
		_, _ = w.Write([]byte(b.String()))
	}))
	defer srv.Close()
	rt, err := New()
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Close()
	if _, err := rt.Execute(`var out; fetch(` + fmt.Sprintf("%q", srv.URL) + `, { method: "POST", headers: { "X-Test": "yes" }, body: "payload" })
	  .then(function (r) { out = r.status + " " + r.headers["X-Echo"] + " " + r.text(); }, function (e) { out = "ERR " + e; });`); err != nil {
		t.Fatal(err)
	}
	rt.DrainJobs()
	v, _ := rt.Execute("out")
	if v != "200 1 POST yes payload" {
		t.Fatalf("fetch result = %v", v)
	}
}
