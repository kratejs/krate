package css

import "testing"

func BenchmarkTailwindGenerate(b *testing.B) {
	classes := map[string]bool{
		"flex": true, "p-4": true, "gap-2": true, "text-red-500": true,
		"hover:bg-blue-600": true, "md:grid-cols-2": true, "rounded-lg": true,
		"shadow-md": true, "font-bold": true, "items-center": true,
	}
	gen := NewTailwindGenerator()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = gen.Generate(classes)
	}
}
