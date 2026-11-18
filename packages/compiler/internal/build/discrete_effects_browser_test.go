package build

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestBrowserDiscreteEventEffects: in a Preact compat island, the effects of
// a render caused by a click run before the next event, as in React 18. The
// picker below copies react-colorful's useColorManipulation: it calls onChange
// only for changes that did not come from its color prop. When effects waited
// for the next frame, a second click before it rendered the picker with the
// new prop and the hsva state from its previous effect at once, and the picker
// reported the prop flip (#1d3fbb -> #ffd100 -> #1d3fbb) as user input, which
// lost a colour the Genesis editor autosaved.
func TestBrowserDiscreteEventEffects(t *testing.T) {
	e2eEnv(t)
	b, root := scProject(t, map[string]string{
		"src/components/repro.tsx": `'use client';
import { useEffect, useRef, useState } from 'react';
function useEventCallback(fn: any) {
  const ref = useRef(fn);
  const cb = useRef((v: string) => ref.current && ref.current(v));
  ref.current = fn;
  return cb.current;
}
function Picker({ color, onChange }: { color: string; onChange: (c: string) => void }) {
  const cb = useEventCallback(onChange);
  const [hsva, setHsva] = useState(() => ({ v: color }));
  const cache = useRef({ color, hsva });
  useEffect(() => {
    if (color !== cache.current.color) {
      const next = { v: color };
      cache.current = { hsva: next, color };
      setHsva(next);
    }
  }, [color]);
  useEffect(() => {
    if (hsva.v !== cache.current.hsva.v && hsva.v !== cache.current.color) {
      cache.current = { hsva, color: hsva.v };
      cb(hsva.v);
    }
  }, [hsva, cb]);
  return <span id="shown">{hsva.v}</span>;
}
export function Repro() {
  const [ext, setExt] = useState('#ffd100');
  const [live, setLive] = useState<string | null>(null);
  const report = (c: string) => { const w = window as any; w.__onChange = (w.__onChange || []).concat(c); };
  return <div>
    <Picker color={live ?? ext} onChange={report} />
    <button id="b1" onClick={() => setLive('#1d3fbb')}>1</button>
    <button id="b2" onClick={() => { setLive(null); queueMicrotask(() => setExt('#1d3fbb')); }}>2</button>
    <button id="reset" onClick={() => { setLive(null); setExt('#ffd100'); }}>reset</button>
  </div>;
}
`,
		"src/pages/index.tsx": `import { Repro } from '../components/repro';
export default async function Page() { await Promise.resolve(); return <main><Repro /></main>; }
`,
	})
	withPreact(t, root)
	if err := linkRuntimePackage(t, filepath.Join(root, "node_modules", "@krate")); err != nil {
		t.Fatal(err)
	}
	if out, err := captureOutput(func() error { return b.BuildAll() }); err != nil {
		t.Fatalf("BuildAll: %v\n%s", err, out)
	}
	browser := strings.ToLower(strings.TrimSpace(os.Getenv("KRATE_BROWSER")))
	if browser == "" {
		t.Skip("set KRATE_BROWSER to run")
	}
	if !canStopInProcessServer {
		t.Skip("in-process server needs a unix host")
	}
	if err := exec.Command("node", "-e", "require.resolve('playwright')").Run(); err != nil {
		requireE2E(t, "playwright is not installed (run pnpm install)")
	}
	base, err := startKrateServer(t, root, b.Cfg)
	if err != nil {
		t.Fatalf("serve: %v", err)
	}
	script, err := filepath.Abs(filepath.Join("testdata", "browser_discrete_effects.mjs"))
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
