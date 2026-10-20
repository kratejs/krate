package build

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRuntimeNeedsCompat(t *testing.T) {
	tests := []struct {
		name string
		js   string
		want bool
	}{
		{"plain signals", `createSignal(0);createEffect(fn);`, false},
		{"deps helper is core", `createEffectDeps(fn,deps);`, false},
		{"children", `Children.count(items);`, true},
		{"cloneElement", `cloneElement(node, props);`, true},
		{"transition", `startTransition(fn);`, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := runtimeNeedsCompat([]*PageResult{{HydrationJS: tt.js}})
			if got != tt.want {
				t.Fatalf("runtimeNeedsCompat = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRuntimeChunkWithoutSPA(t *testing.T) {
	pkgDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	repoRoot := filepath.Clean(filepath.Join(pkgDir, "..", "..", "..", ".."))
	outDir := t.TempDir()
	rel := writeRuntimeChunk(outDir, true, repoRoot, runtimeChunkOptions{SPA: false})
	if rel == "" {
		t.Skip("runtime dist or lite bundle not available")
	}
	runtimeJS, err := os.ReadFile(filepath.Join(outDir, rel))
	if err != nil {
		t.Fatal(err)
	}
	got := string(runtimeJS)
	if strings.Contains(got, "prefetchCache") || strings.Contains(got, "popstate") {
		t.Errorf("lite runtime still contains the SPA router")
	}
	if !strings.Contains(got, "initRouter") {
		t.Errorf("lite runtime is missing the navigation stubs")
	}
}

func TestRuntimeChunkCompatConditional(t *testing.T) {
	pkgDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	repoRoot := filepath.Clean(filepath.Join(pkgDir, "..", "..", "..", ".."))
	outDir := t.TempDir()

	readChunk := func(compat bool) string {
		rel := writeRuntimeChunk(outDir, true, repoRoot, runtimeChunkOptions{SPA: true, Compat: compat})
		if rel == "" {
			t.Skip("runtime dist not available")
		}
		data, err := os.ReadFile(filepath.Join(outDir, rel))
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}

	noCompat := readChunk(false)
	if strings.Contains(noCompat, "useImperativeHandle") {
		t.Error("compat bootstrap was included when no page needed it")
	}
	if !strings.Contains(noCompat, "createEffectDeps") {
		t.Error("core dependency-aware effect helper was omitted")
	}
	if !strings.Contains(readChunk(true), "useImperativeHandle") {
		t.Error("compat bootstrap was omitted when requested")
	}
}
