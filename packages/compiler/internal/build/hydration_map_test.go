package build

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
	"github.com/kratejs/krate/packages/compiler/internal/renderer"
)

// decodeVLQ decodes one source-map segment into its fields.
func decodeVLQ(seg string) []int {
	const chars = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
	var out []int
	shift, val := 0, 0
	for _, c := range seg {
		d := strings.IndexRune(chars, c)
		val += (d & 31) << shift
		if d&32 != 0 {
			shift += 5
			continue
		}
		neg := val&1 == 1
		val >>= 1
		if neg {
			val = -val
		}
		out = append(out, val)
		shift, val = 0, 0
	}
	return out
}

func TestHydrationSourceMapPointsAtTSX(t *testing.T) {
	for _, minify := range []bool{false, true} {
		root := t.TempDir()
		page := "import { createSignal } from \"@krate/runtime\";\n" + // line 1
			"export default function Page() {\n" + // 2
			"  const [n, setN] = createSignal(0);\n" + // 3
			"  return (\n" + // 4
			"    <div>\n" + // 5
			"      <p>spacer</p>\n" + // 6
			"      <button\n" + // 7
			"        onClick={() => {\n" + // 8
			"          setN(n() + 1);\n" + // 9
			"        }}\n" + // 10
			"      >\n" + // 11
			"        go\n" + // 12
			"      </button>\n" + // 13
			"    </div>\n" + // 14
			"  );\n" + // 15
			"}\n"
		writeFileRel(t, root, "src/pages/index.tsx", page)
		cfg := config.Default()
		cfg.PagesDir = filepath.Join(root, "src", "pages")
		cfg.OutDir = filepath.Join(root, "dist")
		cfg.Minify = minify
		cfg.Sourcemap = true
		if _, err := captureOutput(func() error { return New(root, cfg).BuildAll() }); err != nil {
			t.Fatalf("BuildAll: %v", err)
		}
		maps, _ := filepath.Glob(filepath.Join(cfg.OutDir, "index.*.js.map"))
		if len(maps) != 1 {
			t.Fatalf("minify=%v: maps = %v", minify, maps)
		}
		data, _ := os.ReadFile(maps[0])
		var sm struct {
			Sources        []string `json:"sources"`
			SourcesContent []string `json:"sourcesContent"`
			Mappings       string   `json:"mappings"`
		}
		if err := json.Unmarshal(data, &sm); err != nil {
			t.Fatal(err)
		}
		if len(sm.Sources) != 1 || !strings.HasSuffix(sm.Sources[0], "src/pages/index.tsx") {
			t.Fatalf("minify=%v: sources = %v (the map must point at the .tsx, not at itself)", minify, sm.Sources)
		}
		if len(sm.SourcesContent) != 1 || sm.SourcesContent[0] != page {
			t.Errorf("minify=%v: sourcesContent does not hold the original file", minify)
		}
		// Some mapping must land on the onClick arrow (line 8, column 18).
		found := false
		srcLine, srcCol := 0, 0
		for _, line := range strings.Split(sm.Mappings, ";") {
			for _, seg := range strings.Split(line, ",") {
				if seg == "" {
					continue
				}
				f := decodeVLQ(seg)
				if len(f) >= 4 {
					srcLine += f[2]
					srcCol += f[3]
					if srcLine == 7 && srcCol == 17 {
						found = true
					}
				}
			}
		}
		if !found {
			t.Errorf("minify=%v: no mapping to the onClick handler (7:17); mappings=%q", minify, sm.Mappings)
		}
	}
}

func TestBuildHydrationMapEndMarkerStopsTheMapping(t *testing.T) {
	js := "a;\n/*@k:0*/kbindHandler(\"x\",\"onClick\",()=>{\n  boom();\n});/*@k:end*/\nsetTimeout(f);\n"
	marks := []renderer.SrcMark{{File: "/p/src/pages/index.tsx", Line: 8, Col: 3}}
	out, mapJSON := buildHydrationMap(js, marks, "/p")
	if strings.Contains(out, "@k:") {
		t.Fatalf("markers left in the code: %q", out)
	}
	var sm sourceMap
	if err := json.Unmarshal([]byte(mapJSON), &sm); err != nil {
		t.Fatal(err)
	}
	if len(sm.Sources) != 1 || sm.Sources[0] != "src/pages/index.tsx" {
		t.Fatalf("sources %v", sm.Sources)
	}
	// Generated line 1 (0-based) has the mapped statement start and, on line
	// 3, the end marker as a one-field segment.
	lines := strings.Split(sm.Mappings, ";")
	if len(lines) < 4 || len(decodeVLQ(lines[1])) != 4 {
		t.Fatalf("mappings %q", sm.Mappings)
	}
	if got := decodeVLQ(strings.Split(lines[3], ",")[0]); len(got) != 1 {
		t.Errorf("end marker segment has %d fields: %q", len(got), lines[3])
	}
}
