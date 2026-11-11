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
	req := APIRequest{Method: "GET", Path: "/api/json", Headers: map[string][]string{}}

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
	req := APIRequest{Method: "GET", Path: "/api/json", Headers: map[string][]string{}}

	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			if r := rt.Execute(req); r.Status != 200 {
				b.Fatalf("status=%d body=%s", r.Status, r.Body)
			}
		}
	})
}

func benchBinaryRouteDir(b *testing.B) string {
	b.Helper()
	dir := b.TempDir()
	apiDir := filepath.Join(dir, "api")
	if err := os.MkdirAll(apiDir, 0755); err != nil {
		b.Fatal(err)
	}
	code := `var __krate_route = (() => {
  const out = new Uint8Array(1 << 20);
  for (let i = 0; i < out.length; i++) out[i] = (i * 7) & 255;
  async function POST(request) { const b = await request.arrayBuffer(); return Response.json({ n: b.byteLength }); }
  function GET() { return new Response(out, { headers: { 'content-type': 'application/octet-stream' } }); }
  return { GET, POST };
})();`
	if err := os.WriteFile(filepath.Join(apiDir, "bin.js"), []byte(code), 0644); err != nil {
		b.Fatal(err)
	}
	return apiDir
}

func benchBinary(b *testing.B, req APIRequest, check func(APIResult) bool) {
	rt := NewAPIRouteRuntime(benchBinaryRouteDir(b))
	rt.SetPool(1, false)
	if r := rt.Execute(req); !check(r) {
		b.Fatalf("warmup status=%d err=%s", r.Status, r.Error)
	}
	b.SetBytes(1 << 20)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if r := rt.Execute(req); !check(r) {
			b.Fatalf("status=%d err=%s", r.Status, r.Error)
		}
	}
}

// BenchmarkAPIBinaryRequest posts a 1 MiB binary body that the handler reads
// with arrayBuffer().
func BenchmarkAPIBinaryRequest(b *testing.B) {
	body := make([]byte, 1<<20)
	for i := range body {
		body[i] = byte(i)
	}
	req := APIRequest{Method: "POST", Path: "/api/bin", Headers: map[string][]string{"Content-Type": {"application/octet-stream"}}, Body: body}
	benchBinary(b, req, func(r APIResult) bool { return r.Status == 200 && string(r.Body) == `{"n":1048576}` })
}

// BenchmarkAPIBinaryResponse returns a 1 MiB binary body.
func BenchmarkAPIBinaryResponse(b *testing.B) {
	req := APIRequest{Method: "GET", Path: "/api/bin", Headers: map[string][]string{}}
	benchBinary(b, req, func(r APIResult) bool { return r.Status == 200 && len(r.Body) == 1<<20 })
}
