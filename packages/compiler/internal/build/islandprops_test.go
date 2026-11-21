package build

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// islandPropsPayloadRe matches the serialized props of an island: the
// opening <!--krate-island:{...}--> comment of a Preact island, the JSON
// script before a real-React <krate-island>, or the older data-props
// attribute.
var islandPropsPayloadRe = regexp.MustCompile(`(?s)<!--krate-island:.*?-->|<script type="application/json" data-krate-props>.*?</script>|data-props="[^"]*"`)

// islandPropsProject is a layout that wraps the page in a "use client"
// provider owning a context, and a page whose client island sits two server
// components below it with a 50 kB prop. The island shows only the prop's
// length, so the string itself belongs in the HTML once: in its props.
func islandPropsProject(t *testing.T) (*Builder, string, string) {
	big := strings.Repeat("abcdefghij", 5000)
	b, root := scProject(t, map[string]string{
		"src/components/count.tsx": `'use client';
import { createContext, useContext, useState } from 'react';
const CountContext = createContext<{ n: number; bump: () => void } | null>(null);
export function Providers({ children, menu }: { children: any; menu: any }) {
  const [n, setN] = useState(0);
  const [show, setShow] = useState(true);
  return (
    <CountContext.Provider value={{ n, bump: () => setN((v) => v + 1) }}>
      <header><span id="prov-n">prov {n}</span><button id="toggle" onClick={() => setShow((s) => !s)}>toggle</button><nav id="menu">{menu}</nav></header>
      {show ? <div id="page">{children}</div> : <p id="hidden">hidden</p>}
    </CountContext.Provider>
  );
}
export function Blob({ data }: { data: string }) {
  const c = useContext(CountContext);
  if (!c) throw new Error('Blob must be used within Providers');
  return <button id="blob" onClick={c.bump}>blob {data.length} {c.n}</button>;
}
`,
		"src/pages/_layout.tsx": `import { Providers } from '../components/count';
async function Menu() { await Promise.resolve(); return <a id="menu-link" href="/x">menu item</a>; }
export default async function Layout({ children }: { children: any }) {
  await Promise.resolve();
  return <div id="shell"><Providers menu={<Menu />}>{children}</Providers></div>;
}
`,
		"src/pages/index.tsx": `import { Blob } from '../components/count';
const BIG = "` + big + `";
async function Outer({ children }: { children: any }) {
  await Promise.resolve();
  return <section id="outer"><h2>outer</h2>{children}</section>;
}
async function Inner() {
  await Promise.resolve();
  return <article id="inner"><p>inner text</p><Blob data={BIG} /></article>;
}
export default async function Page() {
  await Promise.resolve();
  return <main><Outer><Inner /></Outer></main>;
}
`,
	})
	return b, root, big
}

// TestIslandPropsShipOnce: the props of an island nested in the server
// children of a client provider are in the document once. Server children of
// a client component are already in its server-rendered DOM, so they never
// travel again as HTML inside its props, and props are a JSON script, not an
// entity-escaped attribute. In a browser the nested island reads the
// provider's context, and the adopted children survive the provider's
// re-renders and a hide/show.
func TestIslandPropsShipOnce(t *testing.T) {
	t.Parallel()
	e2eEnv(t)
	b, root, big := islandPropsProject(t)
	withPreact(t, root)
	if err := linkRuntimePackage(t, filepath.Join(root, "node_modules", "@krate")); err != nil {
		t.Fatal(err)
	}
	if out, err := captureBuild(b); err != nil {
		t.Fatalf("BuildAll: %v\n%s", err, out)
	}
	base, err := startKrateServer(t, root, b.Cfg)
	if err != nil {
		t.Fatalf("serve: %v", err)
	}
	status, _, html := getBody(t, base+"/")
	if status != 200 || !strings.Contains(html, "inner text") || !strings.Contains(html, `id="menu-link"`) || !strings.Contains(html, "blob 50000 0") {
		t.Fatalf("status %d, the server must render the page inside the provider:\n%.3000s", status, html)
	}
	if n := strings.Count(html, big); n != 1 {
		t.Errorf("the 50 kB prop is in the HTML %d times, want exactly once", n)
	}
	if strings.Contains(html, "&quot;") {
		t.Errorf("island props must not be entity-escaped into an attribute")
	}
	// Rendered size: the document with every props payload left out. The
	// payloads may add the one 50 kB prop and a little bookkeeping.
	rendered := len(islandPropsPayloadRe.ReplaceAllString(html, ""))
	limit := (rendered + len(big)) * 11 / 10
	t.Logf("HTML %d bytes, rendered %d bytes, props %d bytes (limit %d)", len(html), rendered, len(html)-rendered, limit)
	if len(html) > limit {
		t.Errorf("HTML is %d bytes, want at most %d (rendered %d + one copy of the prop, +10%%)", len(html), limit, rendered)
	}
	if strings.Count(html, "inner text") != 1 || strings.Count(html, "menu item") != 1 {
		t.Errorf("server children must be in the HTML once (inner text %d, menu item %d)", strings.Count(html, "inner text"), strings.Count(html, "menu item"))
	}

	browser := strings.ToLower(strings.TrimSpace(os.Getenv("KRATE_BROWSER")))
	if browser == "" {
		return
	}
	if !canStopInProcessServer {
		t.Skip("cannot stop an in-process server on this platform")
	}
	if err := exec.Command("node", "-e", "require.resolve('playwright')").Run(); err != nil {
		requireE2E(t, "playwright is not installed (run pnpm install)")
	}
	script, err := filepath.Abs(filepath.Join("testdata", "browser_island_props.mjs"))
	if err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("node", script, browser, base).CombinedOutput()
	if err != nil {
		if strings.Contains(string(out), "Executable doesn't exist") || strings.Contains(string(out), "browserType.launch") {
			requireE2E(t, "%s browser is not installed: %s", browser, out)
		}
		t.Fatalf("browser checks failed: %v\n%s", err, out)
	}
}
