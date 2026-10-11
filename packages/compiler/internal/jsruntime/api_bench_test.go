package jsruntime

import (
	"os"
	"path/filepath"
	"testing"
)

func benchRouteDir(b *testing.B) string {
	b.Helper()
	dir := b.TempDir()
	apiDir := filepath.Join(dir, "api")
	if err := os.MkdirAll(apiDir, 0755); err != nil {
		b.Fatal(err)
	}
	code := `var __krate_route = (() => {
  function GET(request) { return Response.json({ message: 'hello', id: 42 }); }
  return { GET };
})();`
	if err := os.WriteFile(filepath.Join(apiDir, "json.js"), []byte(code), 0644); err != nil {
		b.Fatal(err)
	}
	return apiDir
}

// BenchmarkAPIRoutePool measures the shared, prepare-once pool: one VM per
// worker, route loaded once, single-eval request path.
func BenchmarkAPIRoutePool(b *testing.B) {
	rt := NewAPIRouteRuntime(benchRouteDir(b))
	rt.SetPool(8, false)
	req := APIRequest{Method: "GET", Path: "/api/json", Headers: map[string]string{}}

	// Warm the pool / load the route before timing.
	for i := 0; i < 8; i++ {
		if r := rt.Execute(req); r.Status != 200 {
			b.Fatalf("warmup status=%d", r.Status)
		}
	}

	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			if r := rt.Execute(req); r.Status != 200 {
				b.Fatalf("status=%d body=%s", r.Status, r.Body)
			}
		}
	})
}

// BenchmarkAPIRouteIsolation measures the previous per-request model (fresh VM,
// full module re-evaluation) for comparison.
func BenchmarkAPIRouteIsolation(b *testing.B) {
	rt := NewAPIRouteRuntime(benchRouteDir(b))
	rt.SetPool(1, true)
	req := APIRequest{Method: "GET", Path: "/api/json", Headers: map[string]string{}}

	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			if r := rt.Execute(req); r.Status != 200 {
				b.Fatalf("status=%d body=%s", r.Status, r.Body)
			}
		}
	})
}
