package jsruntime

import (
	"sync"
	"testing"
)

// A thrown expression must not leak implicit globals into a pooled VM: the
// strict IIFE turns an undeclared assignment into a throw instead of a global.
func TestEvaluateExprNoGlobalLeak(t *testing.T) {
	if _, err := EvaluateExpr("__krate_leak = 1"); err == nil {
		t.Fatal("expected strict-mode assignment to an undeclared variable to throw")
	}
	got, err := EvaluateExpr("typeof __krate_leak")
	if err != nil {
		t.Fatalf("typeof after throw: %v", err)
	}
	if got != "undefined" {
		t.Fatalf("pooled VM leaked a global: typeof __krate_leak = %q", got)
	}
}

// A failed evaluation must leave the pooled VM usable for the next call.
func TestEvaluateExprReuseAfterError(t *testing.T) {
	if _, err := EvaluateExpr("undefinedIdentifierXYZ"); err == nil {
		t.Fatal("expected reference error")
	}
	got, err := EvaluateExpr("21 * 2")
	if err != nil {
		t.Fatalf("evaluate after error: %v", err)
	}
	if got != "42" {
		t.Fatalf("got %q, want 42", got)
	}
}

// Concurrent evaluations must each use their own VM (QuickJS VMs are not
// concurrency-safe); run under -race to catch sharing.
func TestEvaluateExprConcurrent(t *testing.T) {
	const workers = 16
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				got, err := EvaluateExpr("Math.max(1, 41) + 1")
				if err != nil {
					errs <- err
					return
				}
				if got != "42" {
					errs <- errValue(got)
					return
				}
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
}

type errValue string

func (e errValue) Error() string { return "unexpected value: " + string(e) }

// Shared API bytecode relies on QuickJS serialized bytecode being portable
// across VMs of the same library version.
func TestSharedBytecodeAcrossVMs(t *testing.T) {
	a, err := New()
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	b, err := New()
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()

	bc, err := a.Compile("(41 + 1)", EvalGlobal)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	v, err := b.EvalBytecode(bc)
	if err != nil {
		t.Fatalf("eval bytecode on second VM: %v", err)
	}
	if v != float64(42) && v != int64(42) && v != 42 {
		t.Fatalf("got %v (%T), want 42", v, v)
	}
}

func TestSharedInvokeBytecodeCaches(t *testing.T) {
	apiInvokeBytecode.Delete("cachetest\x00GET")
	compiles := 0
	compile := func() ([]byte, error) {
		compiles++
		rt, err := New()
		if err != nil {
			return nil, err
		}
		defer rt.Close()
		return rt.Compile(apiInvokeJS("cachetest", "GET"), EvalGlobal)
	}
	for i := 0; i < 5; i++ {
		if _, err := sharedInvokeBytecode("cachetest\x00GET", compile); err != nil {
			t.Fatal(err)
		}
	}
	if compiles != 1 {
		t.Fatalf("compiled %d times, want 1", compiles)
	}
}

// BenchmarkEvaluateExpr measures the pooled path.
func BenchmarkEvaluateExpr(b *testing.B) {
	expr := "Math.round(3.14) + Date.parse('1970-01-01')/1000"
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := EvaluateExpr(expr); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkNewRuntime measures the per-call VM creation + polyfill injection +
// close that EvaluateExpr paid before pooling.
func BenchmarkNewRuntime(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rt, err := New()
		if err != nil {
			b.Fatal(err)
		}
		if _, err := rt.Execute("String(Math.round(3.14))"); err != nil {
			b.Fatal(err)
		}
		_ = rt.Close()
	}
}

// BenchmarkCompileInvokeWrapper measures the per-(route,method) compile that each
// worker previously performed on its first request for a route.
func BenchmarkCompileInvokeWrapper(b *testing.B) {
	rt, err := New()
	if err != nil {
		b.Fatal(err)
	}
	defer rt.Close()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := rt.Compile(apiInvokeJS("src/api/users/[id].ts", "GET"), EvalGlobal); err != nil {
			b.Fatal(err)
		}
	}
}
