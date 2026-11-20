package build

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

// hintsFixture writes a site of pages pages, each rendering a real-React
// island of its own that calls resource hints while it renders.
func hintsFixture(t *testing.T, pages, bad int) (string, *config.Config) {
	t.Helper()
	repo, err := repoRootPath()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(repo, "packages", "runtime", "dist", "krate-runtime.js")); err != nil {
		requireE2E(t, "the runtime package is not built: %v", err)
	}
	root := t.TempDir()
	cfg := setupIslandProject(t, root, true)
	if err := os.MkdirAll(filepath.Join(root, "node_modules", "@krate"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := linkRuntimePackage(t, filepath.Join(root, "node_modules", "@krate")); err != nil {
		t.Skipf("cannot link the runtime package: %v", err)
	}
	realReactFixtureFrom(t, root, repo)
	writeProjectFile(t, root, "src/lib/assets.ts", `import { preload, preconnect } from 'react-dom';
export function hintAssets(k: number) {
  preconnect("https://cdn" + (k % 3) + ".example.com");
  preload("/models/m" + k + ".glb", { as: "fetch", crossOrigin: "anonymous" });
}
`)
	for p := 0; p < pages; p++ {
		throwAtLoad := ""
		if p == bad {
			// A module that fails while it loads, after the hint it calls.
			throwAtLoad = "hintAssets(1000);\nif (typeof window === 'undefined') throw new Error('needs a browser');\n"
		}
		writeProjectFile(t, root, fmt.Sprintf("src/components/Scene%d.tsx", p), fmt.Sprintf(`import { useState } from 'react';
import { Canvas, Rect } from 'fake-canvas';
import { hintAssets } from '../lib/assets';
`+throwAtLoad+`export default function Scene%d({ color }: { color: string }) {
	hintAssets(%d);
	const [n] = useState(%d);
	return <div><Canvas id="cv" width={20} height={20}><Rect x={n} y={0} w={5} h={5} fill={color} /></Canvas></div>;
}
`, p, p, p))
		writeProjectFile(t, root, fmt.Sprintf("src/pages/p%d.tsx", p), fmt.Sprintf(`import Scene from '../components/Scene%d';
export default function Page() { return <main><h1>page %d</h1><Scene color="red" /></main>; }
`, p, p))
	}
	writeProjectFile(t, root, "src/pages/index.tsx", "export default function Home() { return <main><h1>home</h1></main>; }\n")
	return root, cfg
}

// hintsBuild builds the fixture and returns every page's head and what the
// build cost.
func hintsBuild(t *testing.T, pages, bad int, merged bool) metaBuild {
	t.Helper()
	root, cfg := hintsFixture(t, pages, bad)
	b := New(root, cfg)
	b.hints().mergeOff = !merged
	cpu := processCPU()
	start := time.Now()
	out, err := captureBuild(b)
	m := metaBuild{
		out:  out,
		wall: time.Since(start),
		cpu:  processCPU() - cpu,
		eval: time.Duration(b.hints().nanos.Load()),
		runs: b.hints().runs.Load(),
	}
	if err != nil {
		t.Fatalf("BuildAll: %v\n%s", err, out)
	}
	m.heads = map[string]string{}
	for p := 0; p < pages; p++ {
		data, err := os.ReadFile(filepath.Join(cfg.OutDir, fmt.Sprintf("p%d", p), "index.html"))
		if err != nil {
			t.Fatal(err)
		}
		m.heads[fmt.Sprintf("p%d", p)] = headOf(string(data))
	}
	return m
}

// TestRealHintsMergedPerf builds the hints fixture rendering every island in
// a runtime of its own and with the renders of a batch sharing one, checks
// the heads match, and reports what each build cost.
func TestRealHintsMergedPerf(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("builds a 40-page fixture twice")
	}
	const pages = 40
	var plain, merged metaBuild
	t.Run("separate", func(t *testing.T) { plain = hintsBuild(t, pages, -1, false) })
	t.Run("merged", func(t *testing.T) { merged = hintsBuild(t, pages, -1, true) })
	if t.Failed() {
		return
	}
	compareHeads(t, plain.heads, merged.heads)
	if h := merged.heads["p7"]; !strings.Contains(h, "/models/m7.glb") || strings.Contains(h, "/models/m8.glb") {
		t.Errorf("p7 must carry its own hints only:\n%s", h)
	}
	for _, r := range []struct {
		name string
		m    metaBuild
	}{{"separate", plain}, {"merged", merged}} {
		t.Logf("%d pages %s: build %v, CPU %v, %d hints runtimes taking %v",
			pages, r.name, r.m.wall.Round(time.Millisecond), r.m.cpu.Round(time.Millisecond), r.m.runs, r.m.eval.Round(time.Millisecond))
	}
}

// A render whose module throws while it loads stops a merged run: it and the
// renders after it run alone, and every page gets the hints it gets when
// each render runs alone.
func TestRealHintsMergedFallback(t *testing.T) {
	t.Parallel()
	const pages = 6
	var plain, merged metaBuild
	t.Run("separate", func(t *testing.T) { plain = hintsBuild(t, pages, 2, false) })
	t.Run("merged", func(t *testing.T) { merged = hintsBuild(t, pages, 2, true) })
	if t.Failed() {
		return
	}
	compareHeads(t, plain.heads, merged.heads)
	for _, p := range []string{"p1", "p3", "p5"} {
		if !strings.Contains(merged.heads[p], "/models/m"+p[1:]+".glb") {
			t.Errorf("%s lacks its hints:\n%s", p, merged.heads[p])
		}
	}
	if merged.runs <= 1 {
		t.Errorf("the renders after the failing one must run alone, got %d runtimes", merged.runs)
	}
}

// metaBuild is what one fixture build measured.
type metaBuild struct {
	heads map[string]string
	out   string
	// wall is the build time, cpu the process CPU time it used, eval the
	// time spent in metadata evaluation runs and runs their number.
	wall, cpu, eval time.Duration
	runs            int64
}

// chunkHashRe matches the content hashes of chunk names, which depend on the
// project directory.
var chunkHashRe = regexp.MustCompile(`\.[0-9a-z]{6}\.js|island_[0-9a-f]{12}`)

// compareHeads compares the heads of two builds of one project made in
// different directories.
func compareHeads(t *testing.T, want, got map[string]string) {
	t.Helper()
	var keys []string
	for k := range want {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if chunkHashRe.ReplaceAllString(got[k], "#") != chunkHashRe.ReplaceAllString(want[k], "#") {
			t.Errorf("%s: grouped head differs\nungrouped:\n%s\ngrouped:\n%s", k, want[k], got[k])
		}
	}
	if len(got) != len(want) {
		t.Errorf("grouped build wrote %d pages, ungrouped %d", len(got), len(want))
	}
}

// TestMetadataGroupsPerf builds the fixture with and without grouped metadata
