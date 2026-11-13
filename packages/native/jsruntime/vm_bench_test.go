package jsruntime

import "testing"

// BenchmarkNewVM measures creating and closing a runtime: what every fresh-VM
// path (config eval, generateStaticParams, isolation-mode API requests) pays
// before it runs any user code.
func BenchmarkNewVM(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		rt, err := New()
		if err != nil {
			b.Fatal(err)
		}
		_ = rt.Close()
	}
}

// BenchmarkNewVMParallel is BenchmarkNewVM with concurrent creators, as a
// parallel build creates them.
func BenchmarkNewVMParallel(b *testing.B) {
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			rt, err := New()
			if err != nil {
				b.Fatal(err)
			}
			_ = rt.Close()
		}
	})
}

// BenchmarkNewVMUseAll creates a runtime and touches every prelude feature, the
// worst case for lazy loading.
func BenchmarkNewVMUseAll(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		rt, err := New()
		if err != nil {
			b.Fatal(err)
		}
		if _, err := rt.Execute(`[typeof setTimeout, typeof AbortController, typeof TextEncoder, typeof structuredClone,
typeof URL, typeof fetch, typeof ReadableStream, typeof FormData, typeof crypto, typeof console.log].join()`); err != nil {
			b.Fatal(err)
		}
		_ = rt.Close()
	}
}

// BenchmarkNewVMFetchOnly creates a runtime and uses Response, as a small route
// handler does.
func BenchmarkNewVMFetchOnly(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		rt, err := New()
		if err != nil {
			b.Fatal(err)
		}
		if _, err := rt.Execute(`Response.json({ a: 1 }).status`); err != nil {
			b.Fatal(err)
		}
		_ = rt.Close()
	}
}

// BenchmarkEvaluateExprFresh measures EvaluateExpr without the VM pool, so each
// call pays for a new expression VM.
func BenchmarkEvaluateExprFresh(b *testing.B) {
	b.Setenv("KRATE_NO_EXPR_VM_POOL", "1")
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := EvaluateExpr("[1, 2, 3].map((x) => x * 2).join()"); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkNewVMPreloadAll creates a runtime that installs every prelude
// feature up front, as API workers do when a build has no feature lists.
func BenchmarkNewVMPreloadAll(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		rt, err := NewWithOptions(Options{PreloadAll: true})
		if err != nil {
			b.Fatal(err)
		}
		_ = rt.Close()
	}
}

// BenchmarkNewBare creates a build-time expression VM.
func BenchmarkNewBare(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		rt, err := NewBare()
		if err != nil {
			b.Fatal(err)
		}
		_ = rt.Close()
	}
}
