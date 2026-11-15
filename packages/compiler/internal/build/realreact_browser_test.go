package build

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestBrowserRealReactIsland renders a library built on react-reconciler (a
// tiny canvas renderer) in a real browser: the island paints a canvas, follows
// a prop that comes from a Krate signal, follows its own React state, and a
// page without such an island loads no React. It needs KRATE_BROWSER like the
// other browser gates.
func TestBrowserRealReactIsland(t *testing.T) {
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
	script, err := filepath.Abs(filepath.Join("testdata", "browser_real_react.mjs"))
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	cfg := setupIslandProject(t, root, true)
	cfg.Minify = true
	repo, err := repoRootPath()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(repo, "packages", "runtime", "dist", "krate-runtime.js")); err != nil {
		requireE2E(t, "the runtime package is not built: %v", err)
	}
	e2eEnv(t)
	if err := os.MkdirAll(filepath.Join(root, "node_modules", "@krate"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := linkRuntimePackage(t, filepath.Join(root, "node_modules", "@krate")); err != nil {
		t.Skipf("cannot link the runtime package: %v", err)
	}
	t.Chdir(root)
	realReactFixtureFrom(t, root, repo)
	realReactSceneFiles(t, root)
	realReactAsyncFiles(t, root)
	realReactClientFiles(t, root)
	if out, err := captureOutput(func() error { return New(root, cfg).BuildAll() }); err != nil {
		t.Fatalf("BuildAll: %v\n%s", err, out)
	}
	serverBase, err := startKrateServer(t, root, cfg)
	if err != nil {
		t.Fatalf("serve: %v", err)
	}
	cmd := exec.Command("node", script, cfg.OutDir, browser, serverBase)
	out, err := cmd.CombinedOutput()
	if err != nil {
		if strings.Contains(string(out), "Executable doesn't exist") || strings.Contains(string(out), "browserType.launch") {
			requireE2E(t, "%s browser is not installed: %s", browser, out)
		}
		t.Fatalf("browser checks failed: %v\n%s", err, out)
	}
	t.Logf("%s", out)
}

// TestBrowserRealReactIslandTags: a real-React island that uses <Link> and
// <Image> without importing them (the way migrate leaves a Next.js module)
// hydrates without console errors, and clicking the link navigates on the
// client: a marker set on window survives the navigation.
func TestBrowserRealReactIslandTags(t *testing.T) {
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
	script, err := filepath.Abs(filepath.Join("testdata", "browser_real_react_tags.mjs"))
	if err != nil {
		t.Fatal(err)
	}
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
	writeProjectFile(t, root, "src/components/Scene.tsx", `'use client';
import { Canvas, Rect } from 'fake-canvas';
export default function Scene() {
	return (
		<div>
			<Link href="/other" id="go" className="nav">other</Link>
			<Link href="https://example.com/x" id="ext">external</Link>
			<Image src="/a.png" alt="pic" width={10} height={10} priority id="pic" />
			<Canvas id="cv" width={20} height={20}><Rect x={0} y={0} w={5} h={5} fill="red" /></Canvas>
		</div>
	);
}
`)
	writeProjectFile(t, root, "src/pages/index.tsx", `import Scene from '../components/Scene';
export default function Page() { return <main><h1>home</h1><Scene /></main>; }
`)
	writeProjectFile(t, root, "src/pages/other.tsx", `export default function Page() { return <main><h1 id="other">other page</h1></main>; }
`)
	writeProjectFile(t, root, "public/a.png", "\x89PNG\r\n\x1a\n")
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

// TestBrowserClientProcessEnv: an unset process.env read in island code (a
// Preact island and a real-React island, project code and a library) yields
// undefined instead of throwing "process is not defined"; a set
// KRATE_PUBLIC_ value is inlined, and no raw process.env is left in a chunk.
func TestBrowserClientProcessEnv(t *testing.T) {
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
	script, err := filepath.Abs(filepath.Join("testdata", "browser_process_env.mjs"))
	if err != nil {
		t.Fatal(err)
	}
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
	t.Setenv("KRATE_PUBLIC_Y", "yval")
	if err := os.MkdirAll(filepath.Join(root, "node_modules", "@krate"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := linkRuntimePackage(t, filepath.Join(root, "node_modules", "@krate")); err != nil {
		t.Skipf("cannot link the runtime package: %v", err)
	}
	realReactFixtureFrom(t, root, repo)
	t.Chdir(root)
	writeProjectFile(t, root, "node_modules/envlib/package.json", `{"name":"envlib","version":"1.0.0","main":"index.js"}`)
	writeProjectFile(t, root, "node_modules/envlib/index.js", `import { jsx } from 'react/jsx-runtime';
import { useState } from 'react';
export function libRead() {
	return process.env.ENVLIB_MAX || 'lib-fallback';
}
export function Plain(props) {
	const [n] = useState(1);
	return jsx('p', { id: 'plain', children: props.text + n + '|' + libRead() + '|' + (process.env.KRATE_PUBLIC_Y || 'y-fallback') });
}
`)
	writeProjectFile(t, root, "src/lib/reads.ts", `import { libRead } from 'envlib';
export function reads(name: string) {
	return [
		process.env.KRATE_PUBLIC_X || 'x-fallback',
		process.env.FOO || 'foo-fallback',
		process.env[name] || 'dyn-fallback',
		process.env.KRATE_PUBLIC_Y || 'y-fallback',
		libRead(),
		typeof process === 'undefined' ? 'no-process' : 'has-process',
	].join('|');
}
`)
	writeProjectFile(t, root, "src/components/Scene.tsx", `'use client';
import { Canvas, Rect } from 'fake-canvas';
import { reads } from '../lib/reads';
export default function Scene() {
	return (
		<div>
			<Canvas id="cv" width={20} height={20}><Rect x={0} y={0} w={5} h={5} fill="red" /></Canvas>
			<p id="real">{reads('BAR')}</p>
		</div>
	);
}
`)
	writeProjectFile(t, root, "src/pages/index.tsx", `import Scene from '../components/Scene';
import { Plain } from 'envlib';
export default function Page() { return <main><h1>home</h1><Scene /><Plain text="p" /></main>; }
`)
	if out, err := captureOutput(func() error { return New(root, cfg).BuildAll() }); err != nil {
		t.Fatalf("BuildAll: %v\n%s", err, out)
	}
	chunks, _ := filepath.Glob(filepath.Join(cfg.OutDir, "chunks", "*.js"))
	if len(chunks) == 0 {
		t.Fatal("no chunks were emitted")
	}
	for _, c := range chunks {
		if strings.Contains(strings.ReplaceAll(readBuilt(t, c), "<define:process.env>", ""), "process.env") {
			t.Errorf("raw process.env left in %s", filepath.Base(c))
		}
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
