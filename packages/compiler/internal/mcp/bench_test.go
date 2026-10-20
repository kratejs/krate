package mcp

import "testing"

const benchmarkPatch = `--- a/src/pages/index.tsx
+++ b/src/pages/index.tsx
@@ -1,3 +1,3 @@
 export default function Page() {
-  return <h1>Old</h1>;
+  return <h1>New</h1>;
 }
`

func BenchmarkParseUnifiedPatch(b *testing.B) {
	for i := 0; i < b.N; i++ {
		files, err := parseUnifiedPatch(benchmarkPatch)
		if err != nil {
			b.Fatal(err)
		}
		if _, err := applyFilePatch("export default function Page() {\n  return <h1>Old</h1>;\n}\n", files[0]); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkGlobMatch(b *testing.B) {
	for i := 0; i < b.N; i++ {
		if !globMatch("src/**/*.tsx", "src/pages/blog/post.tsx") {
			b.Fatal("expected match")
		}
	}
}
