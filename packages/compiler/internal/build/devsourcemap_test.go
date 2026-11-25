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

var devMapProject = map[string]string{
	"src/pages/index.tsx": `import Thrower from "../components/Thrower";
export default function Home() {
  return <main><h1>Home</h1><Thrower /></main>;
}
`,
	"src/pages/other.tsx": `import Thrower from "../components/Thrower";
export default function Other() {
  return <main><Thrower /></main>;
}
`,
	"src/components/Thrower.tsx": `import { createSignal } from "@krate/runtime";
export default function Thrower() {
  const [n, setN] = createSignal(0);
  function explode() {
    setN(n() + 1);
    const user: any = undefined;
    return user.profile.name;
  }
  return <button onClick={() => explode()}>Explode {n()}</button>;
}
`,
}

func buildDevMapProject(t *testing.T, dev, sourcemap bool) string {
	t.Helper()
	root := t.TempDir()
	writeProject(t, root, devMapProject)
	linkRuntimeDist(t, root)
	cfg := config.Default()
	cfg.PagesDir = filepath.Join(root, "src", "pages")
	cfg.OutDir = filepath.Join(root, "dist")
	cfg.Sourcemap = sourcemap
	b := New(root, cfg)
	b.DevMode = dev
	if err := b.BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	return cfg.OutDir
}

// decodeMapLines decodes a v3 mappings string into, per generated line, the
// [genCol, src, srcLine, srcCol] of each segment (srcLine 0-based).
func decodeMapLines(mappings string) [][][4]int {
	const b64 = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
	var out [][][4]int
	src, sl, sc := 0, 0, 0
	for _, line := range strings.Split(mappings, ";") {
		var segs [][4]int
		col := 0
		for _, part := range strings.Split(line, ",") {
			if part == "" {
				continue
			}
			var nums []int
			shift, v := 0, 0
			for _, ch := range part {
				d := strings.IndexRune(b64, ch)
				v += (d & 31) << shift
				if d&32 != 0 {
					shift += 5
					continue
				}
				if v&1 == 1 {
					nums = append(nums, -(v >> 1))
				} else {
					nums = append(nums, v>>1)
				}
				shift, v = 0, 0
			}
			col += nums[0]
			if len(nums) >= 4 {
				src += nums[1]
				sl += nums[2]
				sc += nums[3]
				segs = append(segs, [4]int{col, src, sl, sc})
			}
		}
		out = append(out, segs)
	}
	return out
}

// A dev build maps the page scripts back to the .tsx files with
// project-relative paths, so the overlay can show the component's own line.
func TestDevBuildMapsPageScriptsToSources(t *testing.T) {
	t.Parallel()
	out := buildDevMapProject(t, true, false)
	js := mustGlobOne(t, filepath.Join(out, "index.*.js"))
	code := readBuilt(t, js)
	if !strings.Contains(code, "//# sourceMappingURL="+filepath.Base(js)+".map") {
		t.Fatalf("dev page script has no sourceMappingURL:\n%s", code)
	}
	if strings.Contains(code, renderer.SrcMarkPrefix) {
		t.Errorf("source markers left in the script")
	}
	data, err := os.ReadFile(js + ".map")
	if err != nil {
		t.Fatal(err)
	}
	var sm struct {
		Sources        []string `json:"sources"`
		SourcesContent []string `json:"sourcesContent"`
		Mappings       string   `json:"mappings"`
	}
	if err := json.Unmarshal(data, &sm); err != nil {
		t.Fatal(err)
	}
	idx := -1
	for i, s := range sm.Sources {
		if s == "src/components/Thrower.tsx" {
			idx = i
		}
	}
	if idx < 0 {
		t.Fatalf("map sources = %v, want src/components/Thrower.tsx", sm.Sources)
	}
	// The generated line that dereferences user.profile maps into the
	// component (lines 4-9 of Thrower.tsx), not to some other file.
	genLines := strings.Split(code, "\n")
	lines := decodeMapLines(sm.Mappings)
	found := false
	for g, l := range genLines {
		if !strings.Contains(l, "user.profile.name") || g >= len(lines) {
			continue
		}
		// The mapping in force on this line: its own, or the last before it.
		var seg *[4]int
		for k := g; k >= 0 && seg == nil; k-- {
			if n := len(lines[k]); n > 0 {
				seg = &lines[k][n-1]
				if k == g {
					seg = &lines[k][0]
				}
			}
		}
		if seg != nil && seg[1] == idx && seg[2]+1 >= 4 && seg[2]+1 <= 9 {
			found = true
		} else {
			t.Errorf("line %d (%q) maps to %v of %v", g+1, l, seg, sm.Sources)
		}
	}
	if !found {
		t.Errorf("no mapped line dereferences user.profile:\n%s", code)
	}
	if len(sm.SourcesContent) != len(sm.Sources) || !strings.Contains(sm.SourcesContent[idx], "user.profile.name") {
		t.Errorf("sourcesContent does not hold the component")
	}
}

// Without --sourcemap a production build writes no maps, no
// sourceMappingURL and no source markers: the dev maps change nothing there.
func TestProductionBuildHasNoDevMaps(t *testing.T) {
	t.Parallel()
	out := buildDevMapProject(t, false, false)
	_ = filepath.Walk(out, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if strings.HasSuffix(p, ".map") {
			t.Errorf("production build wrote %s", p)
		}
		if strings.HasSuffix(p, ".js") {
			data, _ := os.ReadFile(p)
			if strings.Contains(string(data), "sourceMappingURL") || strings.Contains(string(data), renderer.SrcMarkPrefix) {
				t.Errorf("%s has a sourceMappingURL or source markers", p)
			}
		}
		return nil
	})
	// With --sourcemap the maps name the sources relative to the script,
	// as before (project-relative paths are for dev only).
	out = buildDevMapProject(t, false, true)
	found := false
	_ = filepath.Walk(out, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(p, ".js.map") {
			return nil
		}
		var sm struct {
			Sources []string `json:"sources"`
		}
		data, _ := os.ReadFile(p)
		_ = json.Unmarshal(data, &sm)
		for _, src := range sm.Sources {
			if strings.HasSuffix(src, "src/components/Thrower.tsx") {
				found = true
				want, _ := filepath.Rel(filepath.Dir(p), filepath.Join(filepath.Dir(out), "src", "components", "Thrower.tsx"))
				if src != filepath.ToSlash(want) {
					t.Errorf("%s names %q, want %q", p, src, want)
				}
			}
		}
		return nil
	})
	if !found {
		t.Errorf("no --sourcemap map names Thrower.tsx")
	}
}

// A statement mark follows the statement's lines into the source: each
// generated line maps to the source line holding its identifiers, at the
// identifiers' columns, though the reprint changed spacing and types.
func TestHydrationMapFollowsStatementLines(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	file := filepath.Join(dir, "src", "C.tsx")
	src := "export default function C() {\n  // a comment\n  function go(x: number) {\n    const user: any = undefined;\n\n    return user.profile.name + x;\n  }\n}\n"
	writeProject(t, dir, map[string]string{"src/C.tsx": src})
	js := "(function(){\n" + renderer.SrcMarkPrefix + "0*/function go(x) {\n  const user=void 0;\n  return user.profile.name+x;\n};" + renderer.SrcMarkEnd + "\nafter();\n})();\n"
	code, sm := buildHydrationMap(js, []renderer.SrcMark{{File: file, Line: 3, Col: 3, Lines: true}}, dir)
	if strings.Contains(code, "/*@k") {
		t.Fatalf("markers left: %s", code)
	}
	var m struct {
		Sources  []string `json:"sources"`
		Mappings string   `json:"mappings"`
	}
	if err := json.Unmarshal([]byte(sm), &m); err != nil {
		t.Fatal(err)
	}
	if len(m.Sources) != 1 || m.Sources[0] != "src/C.tsx" {
		t.Fatalf("sources = %v", m.Sources)
	}
	lines := decodeMapLines(m.Mappings)
	at := func(genLine, genCol int) [4]int {
		var best [4]int
		for _, seg := range lines[genLine] {
			if seg[0] <= genCol {
				best = seg
			}
		}
		return best
	}
	genLines := strings.Split(code, "\n")
	// "  return user.profile.name+x;" is generated line 3; "name" maps to
	// source line 6 (0-based 5) at its column.
	nameCol := strings.Index(genLines[3], "name")
	if got := at(3, nameCol); got[2] != 5 || got[3] != strings.Index(strings.Split(src, "\n")[5], "name") {
		t.Errorf("name maps to %v, want line 5 col %d", got, strings.Index(strings.Split(src, "\n")[5], "name"))
	}
	if got := at(2, 2); got[2] != 3 {
		t.Errorf("the const line maps to %v, want source line 3", got)
	}
	if got := at(1, 0); got[2] != 2 || got[3] != 2 {
		t.Errorf("the function line maps to %v, want line 2 col 2", got)
	}
	// After the end marker nothing maps.
	if len(lines) > 5 && len(lines[5]) != 0 {
		segs := lines[5]
		t.Errorf("code after the statement is mapped: %v", segs)
	}
}
