package build

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// portalEffectOrderFiles is a client component whose probes record, in a
// passive effect, the value their insertion effect was handed, rendered before
// and after a portal. compat's createPortal renders (and commits) its content
// while its parent's render is still running.
var portalEffectOrderFiles = map[string]string{
	"src/components/Order.tsx": `'use client';
import { useEffect, useInsertionEffect, useRef, useState } from 'react';
import { createPortal } from 'react-dom';
function Probe({ v, name }: { v: number; name: string }) {
  const handed = useRef(v);
  useInsertionEffect(() => { handed.current = v; });
  useEffect(() => {
    const w = window as any;
    (w.__seen ||= []).push(name + ':' + v + '/' + handed.current);
  });
  return <i id={name}>{v}</i>;
}
export default function Order() {
  const [v, setV] = useState(0);
  const [host, setHost] = useState<HTMLElement | null>(null);
  useEffect(() => setHost(document.body), []);
  return <div>
    <button id="click" onClick={() => setV((x) => x + 1)}>click</button>
    <button id="later" onClick={() => setTimeout(() => setV((x) => x + 1), 0)}>later</button>
    <Probe v={v} name="before" />
    {host ? createPortal(<b id="in-portal">{v}</b>, host) : null}
    <Probe v={v} name="after" />
  </div>;
}
`,
	"src/pages/index.tsx": `import Order from '../components/Order';
export default async function Page() { await Promise.resolve(); return <main><Order /></main>; }
`,
}

// TestUseClientIslandBrowserPortalEffectOrder: in a Preact client component,
// a component's passive effects run after its insertion and layout effects
// even when a portal in the same render commits first, for a render a click
// causes (flushed with the event, as React does) and for one a timer causes
// (flushed before the next render). Krate's effect-order shims took the
// portal's commit for the end of the render and ran the effects of the
// components rendered before it early: motion's useEffect then animated with
// the props its useInsertionEffect had not handed over yet, and the Genesis
// sidebar labels stayed hidden after a Radix tooltip.
func TestUseClientIslandBrowserPortalEffectOrder(t *testing.T) {
	t.Parallel()
	e2eEnv(t)
	b, root := scProject(t, portalEffectOrderFiles)
	withPreact(t, root)
	if err := linkRuntimePackage(t, filepath.Join(root, "node_modules", "@krate")); err != nil {
		t.Fatal(err)
	}
	if out, err := captureBuild(b); err != nil {
		t.Fatalf("BuildAll: %v\n%s", err, out)
	}
	browser := strings.ToLower(strings.TrimSpace(os.Getenv("KRATE_BROWSER")))
	if browser == "" {
		t.Skip("set KRATE_BROWSER to run")
	}
	if err := exec.Command("node", "-e", "require.resolve('playwright')").Run(); err != nil {
		requireE2E(t, "playwright is not installed (run pnpm install)")
	}
	base, err := startKrateServer(t, root, b.Cfg)
	if err != nil {
		t.Fatalf("serve: %v", err)
	}
	script, err := filepath.Abs(filepath.Join("testdata", "browser_portal_effect_order.mjs"))
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
