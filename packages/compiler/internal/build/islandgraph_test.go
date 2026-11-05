package build

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// islandEntryFiles lists the entry file of each island chunk of a build.
func islandEntryFiles(outDir string) []string {
	files, _ := filepath.Glob(filepath.Join(outDir, "chunks", "island_*.js"))
	sort.Strings(files)
	return files
}

// islandCode is the text of every island entry and shared chunk of a build.
func islandCode(t *testing.T, outDir string) string {
	t.Helper()
	var sb strings.Builder
	for _, pattern := range []string{"island_*.js", "isl-*.js"} {
		files, _ := filepath.Glob(filepath.Join(outDir, "chunks", pattern))
		sort.Strings(files)
		for _, f := range files {
			data, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			sb.Write(data)
			sb.WriteByte('\n')
		}
	}
	if sb.Len() == 0 {
		t.Fatalf("no island chunks in %s", outDir)
	}
	return sb.String()
}
