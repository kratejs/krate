package build

import (
	"strconv"
	"strings"
	"testing"
)

func sampleHydrationJS() string {
	var b strings.Builder
	b.WriteString("var __cache=new Map();function scan(){/* minify target */}\n")
	for i := 0; i < 400; i++ {
		b.WriteString("const value" + strconv.Itoa(i) + " = " + strconv.Itoa(i) + ";\n")
		b.WriteString("function helper" + strconv.Itoa(i) + "(a, b) { return a + b + value" + strconv.Itoa(i) + "; }\n")
	}
	return b.String()
}

// TestMinifyMemoMatchesRaw guards the memo against returning anything other than
// a fresh esbuild transform (and the identity fallback on error).
func TestMinifyMemoMatchesRaw(t *testing.T) {
	js := sampleHydrationJS()
	want := minifyJSRaw(js)
	if got := minifyJS(js); got != want { // miss
		t.Fatalf("first minify != raw")
	}
	if got := minifyJS(js); got != want { // memo hit
		t.Fatalf("memoized minify != raw")
	}
	// Invalid input must fall back to the original, matched by the memo too.
	bad := "function ( {"
	if got := minifyJS(bad); got != bad {
		t.Fatalf("error fallback = %q, want input", got)
	}
	if got := minifyJS(bad); got != bad {
		t.Fatalf("error fallback (memo) = %q, want input", got)
	}
}

// BenchmarkMinifyJSCold measures a fresh esbuild transform of ~30KB (the real
// minification work plus esbuild's fixed per-call overhead).
func BenchmarkMinifyJSCold(b *testing.B) {
	base := sampleHydrationJS()
	b.SetBytes(int64(len(base)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if out := minifyJSRaw(base + "//" + strconv.Itoa(i)); out == "" {
			b.Fatal("empty")
		}
	}
}

// BenchmarkMinifyMemoHit measures the reuse path for an identical input.
func BenchmarkMinifyMemoHit(b *testing.B) {
	js := sampleHydrationJS()
	minifyJS(js)
	b.SetBytes(int64(len(js)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if out := minifyJS(js); out == "" {
			b.Fatal("empty")
		}
	}
}

// BenchmarkMinifyTiny isolates esbuild's fixed per-call (service) overhead from
// the actual minification work.
func BenchmarkMinifyTiny(b *testing.B) {
	const js = "var a = 1; function f(x){ return x + a; }"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if out := minifyJSRaw(js); out == "" {
			b.Fatal("empty")
		}
	}
}
