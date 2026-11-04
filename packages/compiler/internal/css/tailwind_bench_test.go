package css

import (
	"fmt"
	"testing"
)

// BenchmarkTailwindGenerateLarge generates a few thousand distinct classes, the
// shape of a large site's class set.
func BenchmarkTailwindGenerateLarge(b *testing.B) {
	classes := map[string]bool{}
	for i := 0; i < 400; i++ {
		for _, p := range []string{"p", "m", "w", "h", "gap", "top", "mx", "text", "bg-red", "rounded"} {
			classes[fmt.Sprintf("%s-%d", p, i)] = true
		}
	}
	classes["hover:bg-blue-500"] = true
	g := NewTailwindGenerator()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = g.Generate(classes)
	}
}
