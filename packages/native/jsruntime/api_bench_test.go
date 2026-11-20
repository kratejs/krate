package jsruntime

import (
	"os"
	"path/filepath"
	"testing"
)

// BenchmarkAPIRequest measures one request through the pooled runtime for a
// handler that returns Response.json.
func BenchmarkAPIRequest(b *testing.B) {
	apiDir := filepath.Join(b.TempDir(), "api")
	if err := os.MkdirAll(apiDir, 0o755); err != nil {
		b.Fatal(err)
	}
	code := `function GET() { return Response.json({ ok: true, items: [1, 2, 3] }); }`
	if err := os.WriteFile(filepath.Join(apiDir, "json.js"), []byte(code), 0o644); err != nil {
		b.Fatal(err)
	}
	rt := NewAPIRouteRuntime(apiDir)
	rt.SetPool(1, false)
	defer rt.Close()
	req := APIRequest{Method: "GET", Path: "/api/json", Headers: map[string][]string{"Accept": {"*/*"}}}
	if r := rt.Execute(req); r.Status != 200 {
		b.Fatalf("status %d: %s", r.Status, r.Error)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if r := rt.Execute(req); r.Status != 200 {
			b.Fatalf("status %d", r.Status)
		}
	}
}
