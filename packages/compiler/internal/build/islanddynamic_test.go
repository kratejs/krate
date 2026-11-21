package build

import (
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

// lazySharedProject writes a Preact island and a real-React island that both
// import src/lib/counter.ts and a 100 kB module statically, then import()
// src/lib/lazy.ts, which imports the same two modules.
func lazySharedProject(t *testing.T) (string, *config.Config) {
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
	writeProjectFile(t, root, "src/lib/counter.ts", `let n = 0;
export const next = () => {
	if (n < 0) throw new Error('lazy-counter-mark');
	return ++n;
};
`)
	writeProjectFile(t, root, "src/lib/big.ts", "export const BIG = 'big-module-mark:"+strings.Repeat("0123456789", 10000)+"';\n")
	writeProjectFile(t, root, "src/lib/lazy.ts", `import { next } from './counter';
import { BIG } from './big';
export const lazyNext = () => {
	if (!BIG) throw new Error('lazy-module-mark');
	return next();
};
`)
	writeProjectFile(t, root, "node_modules/countlib/package.json", `{"name":"countlib","version":"1.0.0","main":"index.js","peerDependencies":{"react":"^19.0.0"}}`)
	writeProjectFile(t, root, "node_modules/countlib/index.js", `import { useEffect, useState } from 'react';
export function useAsync(fn) {
	const [v, setV] = useState('');
	useEffect(() => { fn().then(setV); }, []);
	return v;
}
`)
	run := `async () => {
	const first = next() + (BIG.length > 0 ? 0 : 1);
	const m = await import('../lib/lazy');
	return first + ',' + m.lazyNext();
}`
	writeProjectFile(t, root, "src/components/Count.tsx", `import { useAsync } from 'countlib';
import { next } from '../lib/counter';
import { BIG } from '../lib/big';
const run = `+run+`;
export default function Count() {
	const v = useAsync(run);
	return <p id="preact">{v}</p>;
}
`)
	writeProjectFile(t, root, "src/components/Scene.tsx", `'use client';
import { useEffect, useState } from 'react';
import { Canvas, Rect } from 'fake-canvas';
import { next } from '../lib/counter';
import { BIG } from '../lib/big';
const run = `+run+`;
export default function Scene() {
	const [v, setV] = useState('');
	useEffect(() => { run().then(setV); }, []);
	return (
		<div>
			<Canvas id="cv" width={20} height={20}><Rect x={0} y={0} w={5} h={5} fill="red" /></Canvas>
			<p id="real">{v}</p>
		</div>
	);
}
`)
	writeProjectFile(t, root, "src/pages/index.tsx", `import Scene from '../components/Scene';
import Count from '../components/Count';
export default function Page() { return <main><h1>home</h1><Scene /><Count /></main>; }
`)
	return root, cfg
}

// TestIslandDynamicImportShared: a module an island imports and a module it
// loads with import() share every dependency: each ships in exactly one
// emitted file, and no standalone copy of the import() target is written.
func TestIslandDynamicImportShared(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("node"); err != nil {
		requireE2E(t, "node is not installed")
	}
	root, cfg := lazySharedProject(t)
	if out, err := captureBuild(New(root, cfg)); err != nil {
		t.Fatalf("BuildAll: %v\n%s", err, out)
	}
	found := map[string][]string{}
	_ = filepath.WalkDir(cfg.OutDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".js") || strings.Contains(filepath.ToSlash(p), "/.krate/") {
			return nil
		}
		text := readBuilt(t, p)
		for _, m := range []string{"big-module-mark", "lazy-counter-mark", "lazy-module-mark"} {
			if strings.Contains(text, m) {
				rel, _ := filepath.Rel(cfg.OutDir, p)
				found[m] = append(found[m], filepath.ToSlash(rel))
			}
		}
		return nil
	})
	for _, m := range []string{"big-module-mark", "lazy-counter-mark", "lazy-module-mark"} {
		if len(found[m]) != 1 {
			t.Errorf("%s should be in exactly one emitted file, found in %v", m, found[m])
		}
	}
	if len(found["lazy-module-mark"]) == 1 && len(found["lazy-counter-mark"]) == 1 && found["lazy-module-mark"][0] == found["lazy-counter-mark"][0] {
		t.Errorf("the import() chunk %s holds counter.ts instead of importing it", found["lazy-module-mark"][0])
	}
}

// TestBrowserIslandDynamicImportShared: the four next() calls (two islands,
// each once statically and once from the import()ed module) hit one counter.
func TestBrowserIslandDynamicImportShared(t *testing.T) {
	t.Parallel()
	browser := strings.ToLower(strings.TrimSpace(os.Getenv("KRATE_BROWSER")))
	if browser == "" {
		t.Skip("set KRATE_BROWSER=chromium|firefox|webkit to run browser checks")
	}
	if err := exec.Command("node", "-e", "require.resolve('playwright')").Run(); err != nil {
		requireE2E(t, "playwright is not installed (run pnpm install)")
	}
	script, err := filepath.Abs(filepath.Join("testdata", "browser_island_dynamic.mjs"))
	if err != nil {
		t.Fatal(err)
	}
	root, cfg := lazySharedProject(t)
	if out, err := captureBuild(New(root, cfg)); err != nil {
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
