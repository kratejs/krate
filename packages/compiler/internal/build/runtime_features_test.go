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

// Markers that only exist in one optional feature bundle.
const (
	markActions = "__krateBindFormAction"
	markClasses = "__krateClassError"
	markHelpers = "twMerge"
	markList    = "longestIncreasingSubsequence"
	markRouter  = "reinitRouter"
)

func TestRuntimeChunkTrimsUnusedFeatures(t *testing.T) {
	counter := `const [n,setN]=createSignal(0);createEffect(()=>{kbindText(1,()=>n())});`
	got := writeTrimmedChunk(t, runtimeChunkOptions{SPA: true, Scan: counter})
	if !strings.Contains(got, "createSignal") || !strings.Contains(got, markRouter) {
		t.Fatal("trimmed runtime lost the core or the router")
	}
	for _, m := range []string{markActions, markClasses, markHelpers, markList} {
		if strings.Contains(got, m) {
			t.Errorf("counter runtime still contains %s", m)
		}
	}
	full := writeTrimmedChunk(t, runtimeChunkOptions{SPA: true, Scan: "import('./x.js')"})
	for _, m := range []string{markActions, markClasses, markHelpers, markList} {
		if !strings.Contains(full, m) {
			t.Errorf("runtime for code with dynamic imports is missing %s", m)
		}
	}
	t.Logf("counter runtime: %d bytes min, %d gzip; full: %d min, %d gzip", len(got), gzipSize(got), len(full), gzipSize(full))
}

func TestRuntimeChunkKeepsReferencedFeatures(t *testing.T) {
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
			got := writeTrimmedChunk(t, runtimeChunkOptions{SPA: true, Scan: tt.scan})
			if !strings.Contains(got, tt.want) {
				t.Errorf("runtime is missing %s for %q", tt.want, tt.scan)
			}
		})
	}
}

func TestRuntimeScanTextActionForms(t *testing.T) {
	plain := runtimeScanText([]*PageResult{{HTML: "<button>x</button>", HydrationJS: "createSignal(0)"}})
	if strings.Contains(plain, actionsMarkup) {
		t.Fatal("a plain static page asked for the actions bundle")
	}
	for name, r := range map[string]*PageResult{
		"form":      {HTML: `<form action=/__krate/action/abc data-krate-action method=post>`},
		"ssr":       {Mode: RenderSSR},
		"streaming": {Mode: RenderStreaming},
		"regions":   {Regions: []Region{{}}},
	} {
		if !strings.Contains(runtimeScanText([]*PageResult{r}), actionsMarkup) {
			t.Errorf("%s page did not request the actions bundle", name)
		}
	}
	island := writeTrimmedChunk(t, runtimeChunkOptions{SPA: true, Scan: runtimeScanText([]*PageResult{{HTML: `<krate-island data-src="/chunks/x.js">`}})})
	for _, m := range []string{markActions, markClasses, markHelpers, markList} {
		if !strings.Contains(island, m) {
			t.Errorf("runtime for a page with a client component is missing %s", m)
		}
	}
	chunk := writeTrimmedChunk(t, runtimeChunkOptions{SPA: true, Scan: runtimeScanText([]*PageResult{{HTML: "<form data-krate-action>"}})})
	if !strings.Contains(chunk, markActions) {
		t.Error("runtime for a server action form is missing the actions bundle")
	}
}

func TestRuntimeChunkTrimmedLiteRouter(t *testing.T) {
	got := writeTrimmedChunk(t, runtimeChunkOptions{SPA: false, Scan: "createSignal(0)"})
	if strings.Contains(got, "popstate") {
		t.Error("lite trimmed runtime contains the SPA router")
	}
	if !strings.Contains(got, "initRouter") {
		t.Error("lite trimmed runtime is missing the navigation stubs")
	}
}

func TestIdentifierSet(t *testing.T) {
	set := identifierSet(`a.b_c($x, "__y1"); 9z`)
	for _, want := range []string{"a", "b_c", "$x", "__y1", "z"} {
		if !set[want] {
			t.Errorf("missing identifier %q in %v", want, set)
		}
	}
	if set["9z"] || set["1"] {
		t.Errorf("number-led token captured: %v", set)
	}
}
