package build

import (
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeTrimmedChunk(t *testing.T, opts runtimeChunkOptions) string {
	t.Helper()
	repoRoot, err := repoRootPath()
	if err != nil {
		t.Fatal(err)
	}
	opts.Trim = true
	outDir := t.TempDir()
	rel, err := writeRuntimeChunk(outDir, true, repoRoot, opts)
	if err != nil {
		t.Fatal(err)
	}
	if rel == "" {
		requireE2E(t, "runtime dist not built")
		t.Skip("runtime dist not available")
	}
	data, err := os.ReadFile(filepath.Join(outDir, rel))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func gzipSize(s string) int {
	var buf bytes.Buffer
	w, _ := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	_, _ = w.Write([]byte(s))
	_ = w.Close()
	return buf.Len()
}

// Names the runtime chunk defines on window only when their optional
// feature ships.
const (
	markActions = "__krateBindFormAction:"
	markClasses = "__krateClass:"
	markHelpers = "twMerge:"
	markList    = "krateBindList:"
)

func TestRuntimeChunkKeepsReferencedFeatures(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		scan string
		want string
	}{
		{"actions", `__krateBindFormAction(el,function(){return save});`, markActions},
		{"list", `krateBindList(1, items, render);`, markList},
		{"classes", `__krateClass(Widget, props);`, markClasses},
		{"helpers", `const c = twMerge(a, b());`, markHelpers},
		{"string reference", `window["__krateBindFormAction"]`, markActions},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := writeTrimmedChunk(t, runtimeChunkOptions{SPA: true, Usage: runtimeUsage{Refs: jsRefs(tt.scan)}})
			if !strings.Contains(got, tt.want) {
				t.Errorf("runtime is missing %s for %q", tt.want, tt.scan)
			}
		})
	}
}
