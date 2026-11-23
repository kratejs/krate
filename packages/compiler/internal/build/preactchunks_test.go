package build

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

// A second full build empties the output directory again, so the shared Preact
// chunks the import map names must be written again.
func TestFullRebuildWritesTheSharedPreactChunksAgain(t *testing.T) {
	e2eEnv(t)
	b, root := scProject(t, map[string]string{
		"src/pages/index.tsx":       "import Toggle from \"../components/Toggle\";\nexport default async function Home() { return <main><h1>home</h1><Toggle /></main>; }\n",
		"src/components/Toggle.tsx": devToggleIsland,
	})
	withPreact(t, root)
	captureProgress(t)
	for i := 0; i < 2; i++ {
		if err := b.BuildAll(); err != nil {
			t.Fatalf("build %d: %v", i+1, err)
		}
		data, err := os.ReadFile(filepath.Join(root, "dist", "index.html"))
		if err != nil {
			t.Fatal(err)
		}
		files := regexp.MustCompile(`/chunks/preact[A-Za-z0-9_.\-]*\.js`).FindAllString(string(data), -1)
		if len(files) == 0 {
			t.Fatalf("build %d: the page names no Preact chunk:\n%s", i+1, data)
		}
		for _, f := range files {
			if _, err := os.Stat(filepath.Join(root, "dist", filepath.FromSlash(f))); err != nil {
				t.Errorf("build %d: %s is named by the import map but was not written", i+1, f)
			}
		}
	}
}
