package jseval

import "testing"

// BenchmarkEvalConfig measures evaluating a small config module the way
// krate.config.ts is loaded: bundle, fresh VM, run, read the result.
func BenchmarkEvalConfig(b *testing.B) {
	dir := b.TempDir()
	src := `const config = { title: "Site", ssr: { apiRuntime: "quickjs" }, pages: ["a", "b"].map((p) => "/" + p) };
globalThis.__krateEvalResult = config;`
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := Eval(Options{Source: src, ResolveDir: dir, NodeShims: true}); err != nil {
			b.Fatal(err)
		}
	}
}
