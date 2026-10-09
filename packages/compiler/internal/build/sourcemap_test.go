package build

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

func TestGenerateSourcemapIsValid(t *testing.T) {
	gen := "var a=1;\nvar b=2;\nconsole.log(a+b);\n"
	src := "const a = 1;\nconst b = 2;\nconsole.log(a + b);\n"
	raw := generateSourcemap(gen, "krate-hydration.js", src)

	var m sourceMap
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatalf("map is not valid JSON: %v", err)
	}
	if m.Version != 3 {
		t.Fatalf("version = %d, want 3", m.Version)
	}
	if len(m.Sources) != 1 || m.Sources[0] != "krate-hydration.js" {
		t.Fatalf("sources = %v", m.Sources)
	}
	if len(m.SourcesContent) != 1 || m.SourcesContent[0] != src {
		t.Fatalf("sourcesContent not embedded")
	}
	// One mapping line per generated line.
	if got, want := strings.Count(m.Mappings, ";")+1, len(strings.Split(gen, "\n")); got != want {
		t.Fatalf("mapping line count = %d, want %d", got, want)
	}
}

func TestMinifyJSWithMapProducesMap(t *testing.T) {
	in := "function longName(argumentOne) {\n  const value = argumentOne + 1;\n  return value;\n}\nconsole.log(longName(2));\n"
	code, rawMap := minifyJSWithMap(in, "krate-hydration.js")
	if !strings.Contains(code, "console.log") {
		t.Fatalf("minified code missing console.log: %q", code)
	}
	var m sourceMap
	if err := json.Unmarshal([]byte(rawMap), &m); err != nil {
		t.Fatalf("esbuild map is not valid JSON: %v", err)
	}
	if len(m.Sources) == 0 || m.Mappings == "" {
		t.Fatalf("map missing sources/mappings: %+v", m)
	}
}

func TestSourcemapBuildEmitsMaps(t *testing.T) {
	root := t.TempDir()
	writeFileRel(t, root, "src/pages/index.tsx",
		"import { createSignal } from '@krate/runtime';\n"+
			"export default function Page() {\n"+
			"  const [n, setN] = createSignal(0);\n"+
			"  return <div><button onClick={() => setN(n() + 1)}>{n()}</button></div>;\n"+
			"}\n")

	cfg := config.Default()
	cfg.PagesDir = filepath.Join(root, "src", "pages")
	cfg.OutDir = filepath.Join(root, "dist")
	cfg.Minify = true
	cfg.Sourcemap = true
	if err := New(root, cfg).BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}

	// Find the emitted page JS + its map.
	var jsFile, mapFile string
	_ = filepath.WalkDir(cfg.OutDir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if strings.HasSuffix(p, ".js") && mapFile == "" {
			jsFile = p
			mapFile = p + ".map"
		}
		return nil
	})
	if jsFile == "" {
		t.Fatal("no hydration js emitted")
	}
	if _, err := os.Stat(mapFile); err != nil {
		t.Fatalf("source map not emitted next to %s: %v", jsFile, err)
	}
	js, _ := os.ReadFile(jsFile)
	if !strings.Contains(string(js), "sourceMappingURL=") {
		t.Fatalf("hydration js does not reference its map:\n%s", js)
	}
}
