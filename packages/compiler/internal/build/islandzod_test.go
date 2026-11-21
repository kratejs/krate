package build

import (
	"bytes"
	"compress/gzip"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

// zodIslandProject is a page whose islands validate with zod 4 through
// `import { z } from 'zod'`, the way an app usually imports it. With both, a
// real-React island uses it too.
func zodIslandProject(t *testing.T, both bool) (string, *config.Config) {
	t.Helper()
	repo, err := repoRootPath()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(repo, "packages", "runtime", "dist", "krate-runtime.js")); err != nil {
		requireE2E(t, "the runtime package is not built: %v", err)
	}
	zod, err := filepath.EvalSymlinks(filepath.Join(repo, "packages", "react-oracle", "node_modules", "zod"))
	if err != nil {
		requireE2E(t, "zod is not installed for the island tests (run pnpm install): %v", err)
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
	if err := os.Symlink(zod, filepath.Join(root, "node_modules", "zod")); err != nil {
		t.Skipf("cannot link zod: %v", err)
	}
	realReactFixtureFrom(t, root, repo)
	writeProjectFile(t, root, "node_modules/countlib/package.json", `{"name":"countlib","version":"1.0.0","main":"index.js","peerDependencies":{"react":"^19.0.0"}}`)
	writeProjectFile(t, root, "node_modules/countlib/index.js", `import { useEffect, useState } from 'react';
export function useOnce(fn) {
	const [v, setV] = useState('0');
	useEffect(() => { setV(fn()); }, []);
	return v;
}
`)
	writeProjectFile(t, root, "src/components/Name.tsx", `import { useOnce } from 'countlib';
import { z } from 'zod';
const name = z.string().min(2);
export default function Name() {
	const v = useOnce(() => name.parse('ok') + '|' + name.safeParse(7).success);
	return <p id="preact">{v}</p>;
}
`)
	page := `import Name from '../components/Name';
export default function Page() { return <main><h1>home</h1><Name /></main>; }
`
	if both {
		writeProjectFile(t, root, "src/components/Scene.tsx", `'use client';
import { useEffect, useState } from 'react';
import { Canvas, Rect } from 'fake-canvas';
import { z } from 'zod';
export default function Scene() {
	const [v, setV] = useState('0');
	useEffect(() => { setV(z.string().parse('ok') + '|' + z.string().safeParse(7).success); }, []);
	return (
		<div>
			<Canvas id="cv" width={20} height={20}><Rect x={0} y={0} w={5} h={5} fill="red" /></Canvas>
			<p id="real">{v}</p>
		</div>
	);
}
`)
		page = `import Name from '../components/Name';
import Scene from '../components/Scene';
export default function Page() { return <main><h1>home</h1><Name /><Scene /></main>; }
`
	}
	writeProjectFile(t, root, "src/pages/index.tsx", page)
	return root, cfg
}

// TestIslandZodTreeShaken: `import { z } from 'zod'` reaches zod's classic
// API as a namespace re-exported by its index, which esbuild alone bundles
// whole, with every locale. An island that uses z.string() ships none of the
// locales and a zod chunk under 40 kB gzipped, whether zod is bundled into
// the island or shared between both kinds of island.
func TestIslandZodTreeShaken(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("node"); err != nil {
		requireE2E(t, "node is not installed")
	}
	for _, both := range []bool{false, true} {
		name := "one kind of island"
		if both {
			name = "both kinds of island"
		}
		t.Run(name, func(t *testing.T) {
			root, cfg := zodIslandProject(t, both)
			out, err := captureBuild(New(root, cfg))
			if err != nil {
				t.Fatalf("BuildAll: %v\n%s", err, out)
			}
			chunks, _ := filepath.Glob(filepath.Join(cfg.OutDir, "chunks", "*.js"))
			var holders []string
			for _, c := range chunks {
				text := readBuilt(t, c)
				// Czech and Spanish messages from zod/v4/locales.
				for _, locale := range []string{"hodnota", "elementos"} {
					if strings.Contains(text, locale) {
						t.Errorf("%s ships a zod locale (%q)", filepath.Base(c), locale)
					}
				}
				if !strings.Contains(text, "$ZodString") {
					continue
				}
				holders = append(holders, filepath.Base(c))
				var gz bytes.Buffer
				w := gzip.NewWriter(&gz)
				_, _ = w.Write([]byte(text))
				_ = w.Close()
				t.Logf("%s holds zod: %d bytes, %d gzipped", filepath.Base(c), len(text), gz.Len())
				if gz.Len() > 40*1024 {
					t.Errorf("%s holds zod and is %d bytes gzipped, want under 40 kB", filepath.Base(c), gz.Len())
				}
			}
			if len(holders) == 0 {
				t.Fatal("no chunk holds zod's $ZodString, so the check found nothing to measure")
			}
			if both && (len(holders) != 1 || !strings.HasPrefix(holders[0], "isn")) {
				t.Errorf("want zod once, in a shared isn chunk, found in %v", holders)
			}
		})
	}
}

// TestBrowserIslandZod: the tree-shaken zod still validates in both kinds of
// island.
func TestBrowserIslandZod(t *testing.T) {
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
	script, err := filepath.Abs(filepath.Join("testdata", "browser_island_zod.mjs"))
	if err != nil {
		t.Fatal(err)
	}
	if !canStopInProcessServer {
		t.Skip("cannot stop an in-process server on this platform")
	}
	root, cfg := zodIslandProject(t, true)
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
