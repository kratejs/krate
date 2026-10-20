package mcp

import "testing"

func FuzzParseUnifiedPatch(f *testing.F) {
	f.Add([]byte("--- a/src/pages/index.tsx\n+++ b/src/pages/index.tsx\n@@ -1 +1 @@\n-old\n+new\n"))
	f.Add([]byte("--- /dev/null\n+++ b/src/pages/new.tsx\n@@ -0,0 +1 @@\n+export default 1;\n"))
	f.Add([]byte("not a patch"))
	f.Add([]byte(""))
	f.Fuzz(func(t *testing.T, data []byte) {
		files, err := parseUnifiedPatch(string(data))
		if err != nil {
			return
		}
		for _, file := range files {
			_, _ = applyFilePatch("old\n", file)
		}
	})
}

func FuzzGlobPolicy(f *testing.F) {
	f.Add("src/**/*.tsx", "src/pages/index.tsx")
	f.Add(".env*", "config/.env.local")
	f.Add("**/secret", "a/b/secret")
	f.Add("", "")
	f.Fuzz(func(t *testing.T, pattern, path string) {
		_ = globMatch(pattern, path)
		p := &Policy{WriteRoots: []string{pattern}}
		_ = p.withinWriteRoots(path)
	})
}
