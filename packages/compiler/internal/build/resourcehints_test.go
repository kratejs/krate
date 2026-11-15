package build

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

// resourceHintsProject writes a site whose client components call react-dom's
// preload("/x.wasm") during render (twice each), next to an island that
// fetches /x.wasm after hydration: a Preact island and a real-React island on
// a static page and on a force-dynamic page, and the same two as client
// boundaries on an async page. Every page also has a second Preact component
// that preloads the same file.
func resourceHintsProject(t *testing.T) (string, *config.Config) {
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
	writeProjectFile(t, root, "public/x.wasm", "\x00asm\x01\x00\x00\x00")
	writeProjectFile(t, root, "public/img/a.png", "\x89PNG\r\n\x1a\n")
	// Hooks from an npm package built on React make a module a Preact island.
	writeProjectFile(t, root, "node_modules/countlib/package.json", `{"name":"countlib","version":"1.0.0","main":"index.js","peerDependencies":{"react":"^19.0.0"}}`)
	writeProjectFile(t, root, "node_modules/countlib/index.js", `import { useEffect, useState } from 'react';
export function useReady() {
	const [v, setV] = useState('');
	useEffect(() => { setV('ready'); }, []);
	return v;
}
export function useFetchSize(url) {
	const [n, setN] = useState('');
	useEffect(() => { fetch(url).then((r) => r.arrayBuffer()).then((b) => setN(String(b.byteLength))); }, []);
	return n;
}
`)
	// A library that needs a browser at import time: the build's hints render
	// of the real-React component replaces it.
	writeProjectFile(t, root, "node_modules/winlib/package.json", `{"name":"winlib","version":"1.0.0","type":"module","main":"index.js"}`)
	writeProjectFile(t, root, "node_modules/winlib/index.js", "export const width = window.innerWidth;\n")
	writeProjectFile(t, root, "src/lib/hint.ts", `import { preload } from 'react-dom';
export function hintX() {
	preload('/x.wasm', { as: 'fetch', crossOrigin: 'anonymous' });
}
`)
	preactHint := `import { useReady } from 'countlib';
import { hintX } from '../lib/hint';
export default function PreactHint() {
	hintX();
	hintX();
	return <p id="hinted">{useReady()}</p>;
}
`
	realHint := `import { useEffect, useState } from 'react';
import { Canvas, Rect } from 'fake-canvas';
import { width } from 'winlib';
import { hintX } from '../lib/hint';
import { preload } from 'react-dom';
export default function RealHint({ items, name }: { items: string[]; name: string }) {
	hintX();
	hintX();
	preload('/img/' + name + '.png', { as: 'image' });
	const [v, setV] = useState('');
	useEffect(() => { setV('ready'); }, []);
	// Rendered without props (the build does that for a client boundary to
	// collect its hints), this throws after the hints ran.
	const list = items.map((i) => <b key={i}>{i}</b>);
	return <div><Canvas id="cv" width={20} height={20}><Rect x={0} y={0} w={5} h={5} fill="red" /></Canvas><p id="hinted" data-width={width}>{v}</p>{list}</div>;
}
`
	fetcher := `import { useFetchSize } from 'countlib';
export default function Fetcher() {
	return <p id="fetched">{useFetchSize('/x.wasm')}</p>;
}
`
	hintOnly := `import { useReady } from 'countlib';
import { hintX } from '../lib/hint';
export default function HintOnly() {
	hintX();
	return <i data-ready={useReady()} />;
}
`
	writeProjectFile(t, root, "src/components/HintOnly.tsx", hintOnly)
	writeProjectFile(t, root, "src/components/HintOnlyCB.tsx", "'use client';\n"+hintOnly)
	writeProjectFile(t, root, "src/components/PreactHint.tsx", preactHint)
	writeProjectFile(t, root, "src/components/RealHint.tsx", realHint)
	writeProjectFile(t, root, "src/components/Fetcher.tsx", fetcher)
	writeProjectFile(t, root, "src/components/PreactHintCB.tsx", "'use client';\n"+preactHint)
	writeProjectFile(t, root, "src/components/RealHintCB.tsx", "'use client';\n"+realHint)
	writeProjectFile(t, root, "src/components/FetcherCB.tsx", "'use client';\n"+fetcher)
	writeProjectFile(t, root, "src/pages/index.tsx", `import PreactHint from '../components/PreactHint';
import Fetcher from '../components/Fetcher';
import HintOnly from '../components/HintOnly';
export default function Page() { return <main><PreactHint /><Fetcher /><HintOnly /></main>; }
`)
	writeProjectFile(t, root, "src/pages/real.tsx", `import RealHint from '../components/RealHint';
import Fetcher from '../components/Fetcher';
import HintOnly from '../components/HintOnly';
export default function Page() { return <main><RealHint items={['a']} name="a" /><Fetcher /><HintOnly /></main>; }
`)
	writeProjectFile(t, root, "src/pages/ssr.tsx", `import PreactHint from '../components/PreactHintCB';
import Fetcher from '../components/FetcherCB';
import HintOnly from '../components/HintOnlyCB';
export default async function Page() { await Promise.resolve(); return <main><PreactHint /><Fetcher /><HintOnly /></main>; }
`)
	writeProjectFile(t, root, "src/pages/real-ssr.tsx", `import RealHint from '../components/RealHintCB';
import Fetcher from '../components/FetcherCB';
import HintOnly from '../components/HintOnlyCB';
export default async function Page() { await Promise.resolve(); return <main><RealHint items={['a']} name="a" /><Fetcher /><HintOnly /></main>; }
`)
	// Request-time pages that are not async keep their islands: the server
	// emits the markup the build rendered for them.
	writeProjectFile(t, root, "src/pages/dyn.tsx", `import PreactHint from '../components/PreactHint';
import Fetcher from '../components/Fetcher';
import HintOnly from '../components/HintOnly';
export const dynamic = 'force-dynamic';
export default function Page() { return <main><PreactHint /><Fetcher /><HintOnly /></main>; }
`)
	writeProjectFile(t, root, "src/pages/real-dyn.tsx", `import RealHint from '../components/RealHint';
import Fetcher from '../components/Fetcher';
import HintOnly from '../components/HintOnly';
export const dynamic = 'force-dynamic';
export default function Page() { return <main><RealHint items={['a']} name="a" /><Fetcher /><HintOnly /></main>; }
`)
	return root, cfg
}

var resourceHintPaths = []string{"/", "/real/", "/ssr/", "/real-ssr/", "/dyn/", "/real-dyn/"}

// serveResourceHints builds the resourceHintsProject site, serves it and
// checks that the server HTML of every page links /x.wasm once, in the head.
func serveResourceHints(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("node"); err != nil {
		requireE2E(t, "node is not installed")
	}
	if !canStopInProcessServer {
		t.Skip("cannot stop an in-process server on this platform")
	}
	root, cfg := resourceHintsProject(t)
	if out, err := captureOutput(func() error { return New(root, cfg).BuildAll() }); err != nil {
		t.Fatalf("BuildAll: %v\n%s", err, out)
	}
	base, err := startKrateServer(t, root, cfg)
	if err != nil {
		t.Fatalf("serve: %v", err)
	}
	const want = `<link rel="preload" href="/x.wasm" as="fetch" crossorigin="">`
	for _, p := range resourceHintPaths {
		status, _, html := getWith(t, base+p, nil)
		if status != 200 {
			t.Errorf("%s: status %d:\n%.800s", p, status, html)
			continue
		}
		end := strings.Index(html, "</head>")
		if end < 0 {
			t.Errorf("%s: no </head>:\n%s", p, html)
			continue
		}
		if n := strings.Count(html[:end], want); n != 1 {
			t.Errorf("%s: the head must hold %s once, has it %d times:\n%s", p, want, n, html[:end])
		}
		if strings.Count(html, `href="/x.wasm"`) != 1 {
			t.Errorf("%s: the page must link /x.wasm once:\n%s", p, html[end:])
		}
		if strings.Contains(html, "undefined") {
			t.Errorf("%s: a hint built from a missing prop reached the page:\n%s", p, html)
		}
		// The build knows the props of an island (not those of a client
		// boundary).
		island := p == "/real/" || p == "/real-dyn/"
		if img := `<link rel="preload" href="/img/a.png" as="image">`; island != strings.Contains(html[:end], img) {
			t.Errorf("%s: want %s in the head only on the pages with real-React islands:\n%s", p, img, html[:end])
		}
		if strings.Contains(html, "krate-head") {
			t.Errorf("%s: head markers left in the page:\n%s", p, html)
		}
	}
	return base
}

// TestResourceHintsServerHTML: react-dom's preload() called while a client
// component renders puts one <link rel="preload"> into the <head> of the
// server HTML: Preact islands and real-React islands on a static page and on
// a force-dynamic page, and the same components as client boundaries on an
// async page. Several components and calls preloading the same file give one
// link.
func TestResourceHintsServerHTML(t *testing.T) {
	serveResourceHints(t)
}

// TestBrowserResourceHints: on the pages of TestResourceHintsServerHTML the
// browser requests /x.wasm once: the island that fetches it reuses the
// preload of the server HTML, and the preload() calls of the Preact and
// real-React components in the browser add nothing. Requires KRATE_BROWSER.
func TestBrowserResourceHints(t *testing.T) {
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
	script, err := filepath.Abs(filepath.Join("testdata", "browser_resource_hints.mjs"))
	if err != nil {
		t.Fatal(err)
	}
	base := serveResourceHints(t)
	out, err := exec.Command("node", append([]string{script, browser, base}, resourceHintPaths...)...).CombinedOutput()
	if err != nil {
		if strings.Contains(string(out), "Executable doesn't exist") || strings.Contains(string(out), "browserType.launch") {
			requireE2E(t, "%s browser is not installed: %s", browser, out)
		}
		t.Fatalf("browser checks failed: %v\n%s", err, out)
	}
}
