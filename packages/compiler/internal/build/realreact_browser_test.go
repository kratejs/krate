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

// TestBrowserIslandContextFromLayoutProvider: an async layout wraps the page
// in a "use client" provider (Radix's TooltipProvider and a counter context
// it owns). The page's server children hold a Preact and a real-React client
// component that both use Radix Tooltip and the counter. As in Next.js, both
// see the provider's context: the server renders the Preact one inside it,
// both tooltips open on hover, and a click in the provider updates both.
func TestBrowserIslandContextFromLayoutProvider(t *testing.T) {
	browser := strings.ToLower(strings.TrimSpace(os.Getenv("KRATE_BROWSER")))
	if browser == "" {
		t.Skip("set KRATE_BROWSER=chromium|firefox|webkit to run browser checks")
	}
	if err := exec.Command("node", "-e", "require.resolve('playwright')").Run(); err != nil {
		requireE2E(t, "playwright is not installed (run pnpm install)")
	}
	script, err := filepath.Abs(filepath.Join("testdata", "browser_island_context.mjs"))
	if err != nil {
		t.Fatal(err)
	}
	radix, err := filepath.Abs(filepath.Join("..", "..", "..", "..", "examples", "node_modules", "radix-ui"))
	if err == nil {
		radix, err = filepath.EvalSymlinks(radix)
	}
	if err != nil {
		requireE2E(t, "radix-ui is not installed for the examples (run pnpm install): %v", err)
	}
	b, root := scProject(t, map[string]string{
		"src/components/count.tsx": `'use client';
import { createContext, useContext, useState } from 'react';
const CountContext = createContext<{ n: number; bump: () => void } | null>(null);
export function CountProvider({ children }: { children: any }) {
  const [n, setN] = useState(0);
  return <CountContext.Provider value={{ n, bump: () => setN((v) => v + 1) }}>{children}</CountContext.Provider>;
}
export function useCount() {
  const c = useContext(CountContext);
  if (!c) throw new Error('useCount must be used within CountProvider');
  return c;
}
export function BumpButton() {
  const { n, bump } = useCount();
  return <button id="bump" onClick={bump}>bump {n}</button>;
}
`,
		"src/components/providers.tsx": `'use client';
import { Tooltip } from 'radix-ui';
import { CountProvider, BumpButton } from './count';
export function Providers({ children }: { children: any }) {
  return <CountProvider><Tooltip.Provider delayDuration={150}><BumpButton />{children}</Tooltip.Provider></CountProvider>;
}
`,
		"src/components/tip.tsx": `import { Tooltip } from 'radix-ui';
export function Tip({ id }: { id: string }) {
  return (
    <Tooltip.Root>
      <Tooltip.Trigger id={id + '-trigger'}>{id}</Tooltip.Trigger>
      <Tooltip.Portal><Tooltip.Content id={id + '-content'}>{id} tip</Tooltip.Content></Tooltip.Portal>
    </Tooltip.Root>
  );
}
`,
		"src/components/PreactTip.tsx": `'use client';
import { useCount } from './count';
import { Tip } from './tip';
export function PreactTip() {
  return <div><span id="preact-n">{useCount().n}</span><Tip id="preact" /></div>;
}
`,
		"src/components/RealTip.tsx": `'use client';
import { Canvas } from 'fake-canvas';
import { useCount } from './count';
import { Tip } from './tip';
export function RealTip() {
  return <div><span id="real-n">{useCount().n}</span><Tip id="real" /><Canvas id="cv" width={4} height={4} /></div>;
}
`,
		"src/pages/_layout.tsx": `import { Providers } from '../components/providers';
export default async function Layout({ children }: { children: any }) {
  await Promise.resolve();
  return <div id="shell"><Providers>{children}</Providers></div>;
}
`,
		"src/pages/index.tsx": `import { PreactTip } from '../components/PreactTip';
import { RealTip } from '../components/RealTip';
async function Section({ children }: { children: any }) {
  await Promise.resolve();
  return <section>{children}</section>;
}
export default async function Page() {
  await Promise.resolve();
  return <main><Section><PreactTip /></Section><RealTip /></main>;
}
`,
	})
	withPreact(t, root)
	realReactFixture(t, root)
	if err := os.Symlink(radix, filepath.Join(root, "node_modules", "radix-ui")); err != nil {
		t.Skipf("cannot link radix-ui: %v", err)
	}
	e2eEnv(t)
	if err := linkRuntimePackage(t, filepath.Join(root, "node_modules", "@krate")); err != nil {
		t.Fatal(err)
	}
	if out, err := captureOutput(func() error { return b.BuildAll() }); err != nil {
		t.Fatalf("BuildAll: %v\n%s", err, out)
	}
	base, err := startKrateServer(t, root, b.Cfg)
	if err != nil {
		t.Fatalf("serve: %v", err)
	}
	status, _, html := getBody(t, base+"/")
	if status != 200 || !strings.Contains(html, `<span id="preact-n">0</span>`) || !strings.Contains(html, `id="preact-trigger"`) {
		t.Fatalf("the server must render the nested Preact component inside the provider (status %d):\n%s", status, html)
	}
	out, err := exec.Command("node", script, browser, base).CombinedOutput()
	if err != nil {
		if strings.Contains(string(out), "Executable doesn't exist") || strings.Contains(string(out), "browserType.launch") {
			requireE2E(t, "%s browser is not installed: %s", browser, out)
		}
		t.Fatalf("browser checks failed: %v\n%s", err, out)
	}
}
