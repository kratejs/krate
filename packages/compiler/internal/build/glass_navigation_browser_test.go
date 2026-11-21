package build

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// glassNavigationFiles is a layout whose client component is a frosted glass
// sidebar (backdrop-filter over a gradient), kept mounted while client-side
// navigations change the page under it, as in the Genesis dashboard.
var glassNavigationFiles = map[string]string{
	"src/styles/glass.css": `html, body { margin: 0; height: 100%; }
body { background: linear-gradient(135deg, #ff5f6d 0%, #ffc371 35%, #47cacc 70%, #3a1c71 100%) fixed; font: 16px sans-serif; }
.shell { display: flex; min-height: 100vh; }
#glass { position: sticky; top: 12px; margin: 12px; width: 240px; height: calc(100vh - 24px); border-radius: 16px; background: rgba(20, 20, 30, 0.35); backdrop-filter: blur(24px) saturate(1.8); -webkit-backdrop-filter: blur(24px) saturate(1.8); padding: 12px; box-sizing: border-box; }
#glass a { display: block; color: #fff; padding: 8px; }
.page { padding: 24px; color: #111; }
.stripe { height: 80px; margin: 12px 0; background: rgba(255, 255, 255, .5); }
`,
	"src/components/Nav.tsx": `'use client';
import { useState } from 'react';
export default function Nav() {
  const [n, setN] = useState(0);
  return <aside id="glass"><button id="count" onClick={() => setN(n + 1)}>{n}</button><Link href="/" id="to-a">Page A</Link><Link href="/b/" id="to-b">Page B</Link></aside>;
}
`,
	"src/pages/_layout.tsx": `import '../styles/glass.css';
import Nav from '../components/Nav';
export default function Layout({ children }: { children: any }) { return <div className="shell"><Nav /><main>{children}</main></div>; }
`,
	"src/pages/index.tsx": `export default function A() { return <div className="page"><h1>Page A</h1>{[1, 2, 3].map((i) => <div key={i} className="stripe" />)}</div>; }
`,
	"src/pages/b.tsx": `export default function B() { return <div className="page"><h1>Page B</h1>{[1, 2, 3, 4, 5].map((i) => <p key={i}>Line {i} of page B.</p>)}</div>; }
`,
}

// TestUseClientIslandBrowserGlassNavigation navigates between two pages of an
// app configured as `krate migrate` configures one from Next.js
// (viewTransitions: "off": Next.js has no View Transition unless
// experimental.viewTransition is on), client-side and as full document
// loads, and checks in every frame of a CDP screencast that the glass sidebar
// keeps its brightness. Chromium composites a backdrop-filter surface from the
// transition's snapshot: it dimmed for a frame on the Genesis dashboard, and a
// cross-document transition (the @view-transition rule) turns it white for
// several frames in headless Chromium. Chromium only: the frames come from the
// DevTools protocol.
func TestUseClientIslandBrowserGlassNavigation(t *testing.T) {
	t.Parallel()
	e2eEnv(t)
	b, root := scProject(t, glassNavigationFiles)
	b.Cfg.ViewTransitions = "off"
	withPreact(t, root)
	if err := linkRuntimePackage(t, filepath.Join(root, "node_modules", "@krate")); err != nil {
		t.Fatal(err)
	}
	if out, err := captureBuild(b); err != nil {
		t.Fatalf("BuildAll: %v\n%s", err, out)
	}
	browser := strings.ToLower(strings.TrimSpace(os.Getenv("KRATE_BROWSER")))
	if browser != "chromium" {
		t.Skip("set KRATE_BROWSER=chromium to run (the frames come from the DevTools protocol)")
	}
	if err := exec.Command("node", "-e", "require.resolve('playwright')").Run(); err != nil {
		requireE2E(t, "playwright is not installed (run pnpm install)")
	}
	base, err := startKrateServer(t, root, b.Cfg)
	if err != nil {
		t.Fatalf("serve: %v", err)
	}
	script, err := filepath.Abs(filepath.Join("testdata", "browser_glass_navigation.mjs"))
	if err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("node", script, base).CombinedOutput()
	if err != nil {
		if strings.Contains(string(out), "Executable doesn't exist") || strings.Contains(string(out), "browserType.launch") {
			requireE2E(t, "chromium is not installed: %s", out)
		}
		t.Fatalf("browser checks failed: %v\n%s", err, out)
	}
	t.Logf("%s", out)
}
