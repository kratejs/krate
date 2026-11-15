package build

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/evanw/esbuild/pkg/api"
	"github.com/kratejs/krate/packages/compiler/internal/config"
)

// sharedCounterProject writes a page with a Preact island and a real-React
// island that both import src/lib/counter.ts (module-level state, no React)
// and src/lib/hits.ts (module-level state and React).
func sharedCounterProject(t *testing.T) (string, *config.Config) {
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
	e2eEnv(t)
	if err := os.MkdirAll(filepath.Join(root, "node_modules", "@krate"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := linkRuntimePackage(t, filepath.Join(root, "node_modules", "@krate")); err != nil {
		t.Skipf("cannot link the runtime package: %v", err)
	}
	realReactFixtureFrom(t, root, repo)
	t.Chdir(root)
	writeProjectFile(t, root, "src/lib/counter.ts", `let count = 0;
export function next() {
	if (count < 0) throw new Error('shared-counter-mark');
	return ++count;
}
export function spare() { return 'counter-spare-mark'; }
`)
	// An npm library both islands import directly: shared, and still tree
	// shaken to what they use.
	writeProjectFile(t, root, "node_modules/neutrallib/package.json", `{"name":"neutrallib","version":"1.0.0","type":"module","main":"index.js","sideEffects":false}`)
	writeProjectFile(t, root, "node_modules/neutrallib/index.js", `export function used() { return 'neutral-used-mark'.length; }
export function unused() { return 'neutral-unused-mark'; }
export default function tag() { return 'neutral-default-mark'.length; }
`)
	writeProjectFile(t, root, "src/lib/hits.ts", `import { useState } from 'react';
let hits = 0;
export function useHits() {
	const [n] = useState(() => ++hits);
	return n;
}
`)
	writeProjectFile(t, root, "src/components/Scene.tsx", `'use client';
import { useEffect, useState } from 'react';
import { Canvas, Rect } from 'fake-canvas';
import tag, { used } from 'neutrallib';
import { next } from '../lib/counter';
import { useHits } from '../lib/hits';
export default function Scene() {
	const [n, setN] = useState(0);
	useHits();
	useEffect(() => { setN(next()); }, []);
	return (
		<div>
			<Canvas id="cv" width={20} height={20}><Rect x={0} y={0} w={5} h={5} fill="red" /></Canvas>
			<p id="real">{n}</p>
			<i hidden>{used() + tag()}</i>
		</div>
	);
}
`)
	// A hook from an npm package built on React makes Count a Preact island.
	writeProjectFile(t, root, "node_modules/countlib/package.json", `{"name":"countlib","version":"1.0.0","main":"index.js","peerDependencies":{"react":"^19.0.0"}}`)
	writeProjectFile(t, root, "node_modules/countlib/index.js", `import { useEffect, useState } from 'react';
export function useOnce(fn) {
	const [v, setV] = useState(0);
	useEffect(() => { setV(fn()); }, []);
	return v;
}
`)
	writeProjectFile(t, root, "src/components/Count.tsx", `import { useOnce } from 'countlib';
import { used } from 'neutrallib';
import { next } from '../lib/counter';
import { useHits } from '../lib/hits';
export default function Count() {
	useHits();
	const n = useOnce(next);
	return <p id="preact" data-used={used()}>{n}</p>;
}
`)
	writeProjectFile(t, root, "src/pages/index.tsx", `import Scene from '../components/Scene';
import Count from '../components/Count';
export default function Page() { return <main><h1>home</h1><Scene /><Count /></main>; }
`)
	return root, cfg
}

// TestIslandModuleSharedAcrossFlavours: a module a Preact island and a
// real-React island both import, with no React in it, ships once, in a shared
// chunk both import; a stateful module that imports React is reported.
func TestIslandModuleSharedAcrossFlavours(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		requireE2E(t, "node is not installed")
	}
	root, cfg := sharedCounterProject(t)
	out, err := captureOutput(func() error { return New(root, cfg).BuildAll() })
	if err != nil {
		t.Fatalf("BuildAll: %v\n%s", err, out)
	}
	chunks, _ := filepath.Glob(filepath.Join(cfg.OutDir, "chunks", "*.js"))
	var holders []string
	var importers []string
	marks := map[string][]string{}
	for _, c := range chunks {
		text := readBuilt(t, c)
		if strings.Contains(text, "shared-counter-mark") {
			holders = append(holders, filepath.Base(c))
		}
		for _, m := range []string{"neutral-used-mark", "neutral-default-mark", "neutral-unused-mark", "counter-spare-mark"} {
			if strings.Contains(text, m) {
				marks[m] = append(marks[m], filepath.Base(c))
			}
		}
		if !strings.HasPrefix(filepath.Base(c), "isn") && strings.Contains(text, "./"+islandNeutralEntryPrefix) {
			importers = append(importers, filepath.Base(c))
		}
	}
	if len(holders) != 1 || !strings.HasPrefix(holders[0], "isn") {
		t.Fatalf("counter.ts should ship once, in a shared isn chunk; found in %v", holders)
	}
	if holders[0] != strings.ToLower(holders[0]) {
		t.Errorf("shared chunk name is not lower-case: %s", holders[0])
	}
	// One Preact and one real-React chunk at least.
	if len(importers) < 2 {
		t.Errorf("want both island bundles to import %s, importers: %v", holders[0], importers)
	}
	for _, m := range []string{"neutral-used-mark", "neutral-default-mark"} {
		if len(marks[m]) != 1 || !strings.HasPrefix(marks[m][0], "isn") {
			t.Errorf("%s should ship once, in a shared chunk; found in %v", m, marks[m])
		}
	}
	for _, m := range []string{"neutral-unused-mark", "counter-spare-mark"} {
		if len(marks[m]) > 0 {
			t.Errorf("an export no island uses was not tree shaken: %s in %v", m, marks[m])
		}
	}
	if !strings.Contains(out, "KR8017") || !strings.Contains(out, "src/lib/hits.ts") {
		t.Errorf("want a KR8017 warning naming src/lib/hits.ts, got:\n%s", out)
	}
	if strings.Contains(out, "src/lib/counter.ts is bundled twice") {
		t.Errorf("counter.ts is shared and must not be reported:\n%s", out)
	}
}

// TestBrowserIslandModuleSharedAcrossFlavours: both islands call next() from
// the shared module once, so between them they see 1 and 2.
func TestBrowserIslandModuleSharedAcrossFlavours(t *testing.T) {
	browser := strings.ToLower(strings.TrimSpace(os.Getenv("KRATE_BROWSER")))
	if browser == "" {
		t.Skip("set KRATE_BROWSER=chromium|firefox|webkit to run browser checks")
	}
	if _, err := exec.LookPath("node"); err != nil {
		requireE2E(t, "node is not installed")
	}
	if err := exec.Command("node", "-e", "require.resolve('playwright')").Run(); err != nil {
		requireE2E(t, "playwright is not installed (run pnpm install)")
	}
	script, err := filepath.Abs(filepath.Join("testdata", "browser_island_shared.mjs"))
	if err != nil {
		t.Fatal(err)
	}
	root, cfg := sharedCounterProject(t)
	if out, err := captureOutput(func() error { return New(root, cfg).BuildAll() }); err != nil {
		t.Fatalf("BuildAll: %v\n%s", err, out)
	}
	if !canStopInProcessServer {
		t.Skip("cannot stop an in-process server on this platform")
	}
	base, err := startKrateServer(t, root, cfg)
	if err != nil {
		t.Fatalf("serve: %v", err)
	}
	out, err := exec.Command("node", script, browser, base).CombinedOutput()
	if err != nil {
		if strings.Contains(string(out), "Executable doesn't exist") || strings.Contains(string(out), "browserType.launch") {
			requireE2E(t, "%s browser is not installed: %s", browser, out)
		}
		t.Fatalf("browser checks failed: %v\n%s", err, out)
	}
}

func TestSharedImportsUsed(t *testing.T) {
	files := map[string]string{"/p/a.ts": "isn_aaaaaaaaaaaa.js", "/p/b.ts": "isn_bbbbbbbbbbbb.js", "/p/c.ts": "isn_cccccccccccc.js", "/p/d.ts": "isn_dddddddddddd.js", "/p/e.ts": "isn_eeeeeeeeeeee.js", "/p/f.ts": "isn_ffffffffffff.js"}
	out := []api.OutputFile{{Path: "/out/island_1.js", Contents: []byte(
		`import x,{next as o,prev}from"./isn_aaaaaaaaaaaa.js";import{"odd-name" as q}from"./isn_ffffffffffff.js";import*as N from"./isn_bbbbbbbbbbbb.js";` +
			`import"./isn_cccccccccccc.js";export{y as z}from"./isn_aaaaaaaaaaaa.js";const l=()=>import("./isn_dddddddddddd.js");`)}}
	used := sharedImportsUsed([][]api.OutputFile{out}, files)
	a := used["/p/a.ts"]
	if a == nil || a.all || !a.names["default"] || !a.names["next"] || !a.names["prev"] || !a.names["y"] || len(a.names) != 4 {
		t.Errorf("a.ts: %+v", a)
	}
	if b := used["/p/b.ts"]; b == nil || !b.all {
		t.Errorf("a namespace import needs every export: %+v", b)
	}
	if c := used["/p/c.ts"]; c == nil || c.all || len(c.names) != 0 {
		t.Errorf("a bare import needs only the module's effects: %+v", c)
	}
	if d := used["/p/d.ts"]; d == nil || !d.all {
		t.Errorf("import() needs every export: %+v", d)
	}
	if f := used["/p/f.ts"]; f == nil || !f.all {
		t.Errorf("an import it cannot read keeps every export: %+v", f)
	}
	if e := used["/p/e.ts"]; e != nil {
		t.Errorf("a module no graph imports needs no chunk: %+v", e)
	}
}
