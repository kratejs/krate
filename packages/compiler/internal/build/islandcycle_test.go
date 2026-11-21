package build

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

// islandCycleProject is a page with a Preact island (Count, through a hook
// from an npm package built on React) and a real-React island (Scene, a
// react-reconciler renderer) that both import a.ts, where a.ts and b.ts import
// each other and each calls the other at its top level.
func islandCycleProject(t *testing.T) (string, *config.Config) {
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
	writeProjectFile(t, root, "src/lib/a.ts", `import { fromB, B } from './b';
let count = 0;
export function bump() { return ++count; }
export function fromA() { return 'cycle-a-mark'.slice(6, 7); }
export const A = 'A' + fromB();
export const both = () => A + '|' + B;
`)
	writeProjectFile(t, root, "src/lib/b.ts", `import { fromA } from './a';
export function fromB() { return 'cycle-b-mark'.slice(6, 7); }
export const B = 'B' + fromA();
`)
	writeProjectFile(t, root, "node_modules/countlib/package.json", `{"name":"countlib","version":"1.0.0","main":"index.js","peerDependencies":{"react":"^19.0.0"}}`)
	writeProjectFile(t, root, "node_modules/countlib/index.js", `import { useEffect, useState } from 'react';
export function useOnce(fn) {
	const [v, setV] = useState('0');
	useEffect(() => { setV(fn()); }, []);
	return v;
}
`)
	writeProjectFile(t, root, "src/components/Scene.tsx", `'use client';
import { useEffect, useState } from 'react';
import { Canvas, Rect } from 'fake-canvas';
import { bump, both } from '../lib/a';
export default function Scene() {
	const [n, setN] = useState('0');
	useEffect(() => { setN(bump() + '|' + both()); }, []);
	return (
		<div>
			<Canvas id="cv" width={20} height={20}><Rect x={0} y={0} w={5} h={5} fill="red" /></Canvas>
			<p id="real">{n}</p>
		</div>
	);
}
`)
	writeProjectFile(t, root, "src/components/Count.tsx", `import { useOnce } from 'countlib';
import { bump, both } from '../lib/a';
export default function Count() {
	const n = useOnce(() => bump() + '|' + both());
	return <p id="preact">{n}</p>;
}
`)
	writeProjectFile(t, root, "src/pages/index.tsx", `import Scene from '../components/Scene';
import Count from '../components/Count';
export default function Page() { return <main><h1>home</h1><Scene /><Count /></main>; }
`)
	return root, cfg
}

// TestIslandCycleSharedAcrossFlavours: an import cycle a Preact island and a
// real-React island both reach ships once, in one shared chunk, and is not
// reported as bundled twice.
func TestIslandCycleSharedAcrossFlavours(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("node"); err != nil {
		requireE2E(t, "node is not installed")
	}
	root, cfg := islandCycleProject(t)
	out, err := captureBuild(New(root, cfg))
	if err != nil {
		t.Fatalf("BuildAll: %v\n%s", err, out)
	}
	chunks, _ := filepath.Glob(filepath.Join(cfg.OutDir, "chunks", "*.js"))
	marks := map[string][]string{}
	for _, c := range chunks {
		text := readBuilt(t, c)
		for _, m := range []string{"cycle-a-mark", "cycle-b-mark", "++count"} {
			if strings.Contains(text, m) {
				marks[m] = append(marks[m], filepath.Base(c))
			}
		}
	}
	for _, m := range []string{"cycle-a-mark", "cycle-b-mark", "++count"} {
		if len(marks[m]) != 1 || !strings.HasPrefix(marks[m][0], "isn") {
			t.Errorf("%q should ship once, in a shared isn chunk; found in %v", m, marks[m])
		}
	}
	if len(marks["cycle-a-mark"]) == 1 && len(marks["cycle-b-mark"]) == 1 && marks["cycle-a-mark"][0] != marks["cycle-b-mark"][0] {
		t.Errorf("want the cycle in one chunk, a.ts is in %s and b.ts in %s", marks["cycle-a-mark"][0], marks["cycle-b-mark"][0])
	}
	if strings.Contains(out, "bundled twice") {
		t.Errorf("an import cycle that does not reach React must not be reported:\n%s", out)
	}
}

// TestBrowserIslandCycleShared: both islands hydrate, each bumps the counter
// of the shared cycle once (so they see 1 and 2), and the cycle ran in a
// valid order.
func TestBrowserIslandCycleShared(t *testing.T) {
	t.Parallel()
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
	script, err := filepath.Abs(filepath.Join("testdata", "browser_island_cycle.mjs"))
	if err != nil {
		t.Fatal(err)
	}
	if !canStopInProcessServer {
		t.Skip("cannot stop an in-process server on this platform")
	}
	root, cfg := islandCycleProject(t)
	if out, err := captureBuild(New(root, cfg)); err != nil {
		t.Fatalf("BuildAll: %v\n%s", err, out)
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
