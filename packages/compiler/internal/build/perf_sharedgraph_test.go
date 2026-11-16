package build

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

// The shared-graph fixture is the shape of a large migrated Next.js app: many
// request-time pages under an async root layout, each rendering a few "use
// client" components (Preact islands) and a component that renders through a
// react-reconciler library (a real-React island), all of them reaching one
// big graph of plain modules. The build must compile that graph a fixed number
// of times, however many pages share it.

const (
	sharedGraphModules = 2000
	sharedGraphWidgets = 12
)

// sharedGraphProject writes the fixture with pages pages into a new directory.
func sharedGraphProject(t *testing.T, pages int) (string, *config.Config) {
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

	// A binary tree of plain modules: m0 reaches all of them.
	for i := 0; i < sharedGraphModules; i++ {
		var src strings.Builder
		var calls []string
		for _, c := range []int{2*i + 1, 2*i + 2} {
			if c < sharedGraphModules {
				fmt.Fprintf(&src, "import { v%d } from './m%d';\n", c, c)
				calls = append(calls, fmt.Sprintf("v%d(n)", c))
			}
		}
		body := "'m" + strconv.Itoa(i) + "'"
		if len(calls) > 0 {
			body += " + " + strings.Join(calls, " + ")
		}
		fmt.Fprintf(&src, "export function v%d(n: number): string { return n > %d ? '' : %s; }\n", i, i%7, body)
		writeProjectFile(t, root, fmt.Sprintf("src/lib/g/m%d.ts", i), src.String())
	}
	for w := 0; w < sharedGraphWidgets; w++ {
		writeProjectFile(t, root, fmt.Sprintf("src/components/W%d.tsx", w), fmt.Sprintf(`'use client';
import { useState } from 'react';
import { v0 } from '../lib/g/m0';
export default function W%d() {
	const [n, setN] = useState(%d);
	return <button class="w%d" onClick={() => setN(n + 1)}>{v0(n).length}</button>;
}
`, w, w, w))
	}
	writeProjectFile(t, root, "src/components/Scene.tsx", `'use client';
import { useState } from 'react';
import { Canvas, Rect } from 'fake-canvas';
import { v0 } from '../lib/g/m0';
export default function Scene() {
	const [n] = useState(1);
	return <div><Canvas id="cv" width={20} height={20}><Rect x={0} y={0} w={5} h={5} fill="red" /></Canvas><p>{v0(n).length}</p></div>;
}
`)
	writeProjectFile(t, root, "src/pages/_layout.tsx", `import W0 from '../components/W0';
export default async function Layout({ children }) {
	const nav = await Promise.resolve('nav');
	return <div><nav>{nav}<W0 /></nav>{children}</div>;
}
`)
	for p := 0; p < pages; p++ {
		a, b, c := 1+p%(sharedGraphWidgets-1), 1+(p+3)%(sharedGraphWidgets-1), 1+(p+7)%(sharedGraphWidgets-1)
		writeProjectFile(t, root, fmt.Sprintf("src/pages/p%d.tsx", p), fmt.Sprintf(`import A from '../components/W%d';
import B from '../components/W%d';
import C from '../components/W%d';
import Scene from '../components/Scene';
export default async function Page() {
	const title = await Promise.resolve('page %d');
	return <main><h1>{title}</h1><A /><B /><C /><Scene /></main>;
}
`, a, b, c, p))
	}
	writeProjectFile(t, root, "src/pages/index.tsx", "export default function Home() { return <main><h1>home</h1></main>; }\n")
	return root, cfg
}

type sharedGraphRun struct {
	elapsed time.Duration
	// parses is the most times the compiler parsed one graph module, loads
	// the most times esbuild loaded one.
	parses, loads int
	esbuild       int
}

// buildSharedGraph builds the fixture with pages pages and reports how often
// its shared modules were compiled.
func buildSharedGraph(t *testing.T, pages int) sharedGraphRun {
	t.Helper()
	root, cfg := sharedGraphProject(t, pages)
	t.Chdir(root)
	counter := newFileLoadCounter()
	esbuildLoads.Store(counter)
	defer esbuildLoads.Store(nil)
	before := esbuildStatsSnapshot()
	b := New(root, cfg)
	start := time.Now()
	out, err := captureOutput(b.BuildAll)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("BuildAll with %d pages: %v\n%s", pages, err, out)
	}
	calls := 0
	for _, s := range esbuildStatsSince(before) {
		calls += s.n
	}
	run := sharedGraphRun{elapsed: elapsed, esbuild: calls}
	graph := canonicalPath(filepath.Join(root, "src", "lib", "g")) + string(filepath.Separator)
	inGraph := func(p string) bool { return strings.HasPrefix(canonicalPath(p), graph) }
	seen := 0
	for p, n := range b.moduleCache.ParseCounts() {
		if inGraph(p) {
			seen++
			run.parses = max(run.parses, n)
		}
	}
	if seen != sharedGraphModules {
		t.Fatalf("the compiler parsed %d of the %d graph modules: the fixture does not reach its graph", seen, sharedGraphModules)
	}
	// esbuild may reach one file under several spellings (on Windows its
	// short and long names), so loads are summed per canonical path.
	loads := map[string]int{}
	spellings := 0
	for p, n := range counter.counts() {
		if inGraph(p) {
			loads[canonicalPath(p)] += n
			spellings++
		}
	}
	if len(loads) != sharedGraphModules {
		t.Fatalf("esbuild loaded %d of the %d graph modules", len(loads), sharedGraphModules)
	}
	if spellings != len(loads) {
		t.Logf("esbuild named the %d graph modules by %d paths", len(loads), spellings)
	}
	for _, n := range loads {
		run.loads = max(run.loads, n)
	}
	return run
}

// TestSharedGraphCompiledOncePerBuild builds the fixture at two sizes. The
// compiler parses every shared module once per build; esbuild loads it a
// fixed number of times (the server render modules of the client components
// share one code-split build), not once per page; and doubling the pages
// less than doubles the build time.
func TestSharedGraphCompiledOncePerBuild(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a 2000-module fixture")
	}
	if raceEnabled {
		t.Skip("timed without -race by CI's build benchmark step")
	}
	if _, err := exec.LookPath("node"); err != nil {
		requireE2E(t, "node is not installed")
	}
	var small, large sharedGraphRun
	// Each build runs in its own subtest: it changes into the fixture, and
	// the subtest changes back.
	t.Run("30 pages", func(t *testing.T) { small = buildSharedGraph(t, 30) })
	t.Run("60 pages", func(t *testing.T) { large = buildSharedGraph(t, 60) })
	if t.Failed() {
		return
	}
	t.Logf("30 pages: %v, %d esbuild calls, graph modules parsed at most %dx, loaded by esbuild at most %dx",
		small.elapsed.Round(time.Millisecond), small.esbuild, small.parses, small.loads)
	t.Logf("60 pages: %v, %d esbuild calls, graph modules parsed at most %dx, loaded by esbuild at most %dx",
		large.elapsed.Round(time.Millisecond), large.esbuild, large.parses, large.loads)
	if os.Getenv("GITHUB_STEP_SUMMARY") != "" {
		if f, err := os.OpenFile(os.Getenv("GITHUB_STEP_SUMMARY"), os.O_APPEND|os.O_WRONLY, 0644); err == nil {
			fmt.Fprintf(f, "### Shared-graph build benchmark (%d modules)\n\n| pages | build | esbuild calls | max parses | max esbuild loads |\n|---|---|---|---|---|\n", sharedGraphModules)
			for _, r := range []struct {
				n   int
				run sharedGraphRun
			}{{30, small}, {60, large}} {
				fmt.Fprintf(f, "| %d | %v | %d | %d | %d |\n", r.n, r.run.elapsed.Round(time.Millisecond), r.run.esbuild, r.run.parses, r.run.loads)
			}
			fmt.Fprintln(f)
			f.Close()
		}
	}
	for _, r := range []sharedGraphRun{small, large} {
		if r.parses != 1 {
			t.Errorf("a shared module was parsed %d times in one build, want once", r.parses)
		}
	}
	// The loads must not grow with the pages. A shared module is loaded once
	// by the combined build of the client components' server render modules,
	// by a few batches of client chunk checks and hints renders, and by the
	// island chunks.
	if large.loads > small.loads+2 {
		t.Errorf("esbuild loaded a shared module %d times with 60 pages but %d times with 30: work grows with the pages", large.loads, small.loads)
	}
	if limit := 1 + 8; large.loads > limit {
		t.Errorf("esbuild loaded a shared module %d times in one build, want at most %d (one combined server render build and a few batches)", large.loads, limit)
	}
	if large.esbuild > small.esbuild+(60-30)*3 {
		t.Errorf("esbuild ran %d times with 60 pages and %d with 30", large.esbuild, small.esbuild)
	}
	if ratio := float64(large.elapsed) / float64(small.elapsed); ratio > 1.8 {
		t.Errorf("doubling the pages took %.2fx as long (%v -> %v), want sublinear growth", ratio, small.elapsed, large.elapsed)
	}
}
