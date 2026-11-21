package build

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// syncStoreFiles is an app whose client components read a localStorage
// store with useSyncExternalStore(subscribe, getSnapshot, getServerSnapshot),
// as the Genesis builder remembers its 2D/3D/split layout. The server renders
// getServerSnapshot ("a"); a browser with "b" saved must show "b" once the
// page hydrates, as React does, in components that change structure (Mode)
// and in ones that change only attributes (Switch).
var syncStoreFiles = map[string]string{
	"src/store.ts": `const listeners = new Set<() => void>();
export function subscribe(cb: () => void) {
  listeners.add(cb);
  addEventListener('storage', cb);
  return () => { listeners.delete(cb); removeEventListener('storage', cb); };
}
export const read = () => localStorage.getItem('mode') ?? 'a';
export const serverRead = () => 'a';
export function setMode(v: string) { localStorage.setItem('mode', v); listeners.forEach((l) => l()); }
`,
	"src/components/Mode.tsx": `'use client';
import { useSyncExternalStore } from 'react';
import { subscribe, read, serverRead } from '../store';
export default function Mode() {
  const v = useSyncExternalStore(subscribe, read, serverRead);
  return v === 'b' ? <section id="mode" data-mode="b"><b>B</b></section> : <div id="mode" data-mode="a"><i>A</i></div>;
}
`,
	"src/components/Switch.tsx": `'use client';
import { useSyncExternalStore } from 'react';
import { subscribe, read, serverRead, setMode } from '../store';
export default function Switch() {
  const v = useSyncExternalStore(subscribe, read, serverRead);
  return <div role="radiogroup">
    <button id="sw-a" role="radio" aria-checked={v === 'a'} onClick={() => setMode('a')}>A</button>
    <button id="sw-b" role="radio" aria-checked={v === 'b'} onClick={() => setMode('b')}>B</button>
  </div>;
}
`,
	"src/components/Pair.tsx": `'use client';
import { useEffect, useState, useSyncExternalStore } from 'react';
import { subscribe, read, serverRead } from '../store';
function Reader({ id }: { id: string }) {
  return <span id={id}>{useSyncExternalStore(subscribe, read, serverRead)}</span>;
}
export default function Pair() {
  const [late, setLate] = useState(false);
  useEffect(() => setLate(true), []);
  return <p><Reader id="early" />{late ? <Reader id="late" /> : null}</p>;
}
`,
	"src/components/NoServer.tsx": `'use client';
import { useSyncExternalStore } from 'react';
import { subscribe } from '../store';
const read = () => (globalThis as any).__noServer ?? 'a';
export default function NoServer() {
  return <span id="noserver">{useSyncExternalStore(subscribe, read)}</span>;
}
`,
	"src/components/Ids.tsx": `'use client';
import { useId, useState } from 'react';
export default function Ids() {
  const id = useId();
  const [n, setN] = useState(0);
  return <div><label id="lbl" htmlFor={id}>name</label><input id="in" data-id={id} /><button id="rerender" onClick={() => setN(n + 1)}>{n}</button></div>;
}
`,
	"src/components/Hooked.tsx": `'use client';
import { useMemo, useSyncExternalStore } from 'react';
import { subscribe, read, serverRead } from '../store';
export default function Hooked() {
  const m = useSyncExternalStore(subscribe, read, serverRead);
  return <span id="hooked" data-hooked={m}>{m}{useMemo(() => '!' + m, [m])}</span>;
}
`,
	"src/components/RealMode.tsx": `'use client';
import { useSyncExternalStore } from 'react';
import { Canvas, Rect } from 'fake-canvas';
import { subscribe, read, serverRead } from '../store';
export default function RealMode() {
  const v = useSyncExternalStore(subscribe, read, serverRead);
  return <div>
    {v === 'b' ? <section id="real" data-mode="b">B</section> : <div id="real" data-mode="a">A</div>}
    <Canvas id="cv" width={10} height={10}><Rect x={0} y={0} w={5} h={5} fill="red" /></Canvas>
  </div>;
}
`,
	// Client components inside an async server component: client boundaries.
	"src/pages/index.tsx": `import Mode from '../components/Mode';
import Switch from '../components/Switch';
import Pair from '../components/Pair';
import NoServer from '../components/NoServer';
import Ids from '../components/Ids';
import Hooked from '../components/Hooked';
export default async function Page() {
  await Promise.resolve();
  return <main><Switch /><Mode /><Pair /><NoServer /><Ids /><Hooked /></main>;
}
`,
	// The same components on a synchronous page are compiled to Krate's own
	// runtime, which lowers a hook called inline in JSX like a declared one.
	"src/pages/compiled.tsx": `import Mode from '../components/Mode';
import Switch from '../components/Switch';
import Pair from '../components/Pair';
import NoServer from '../components/NoServer';
import Ids from '../components/Ids';
import Hooked from '../components/Hooked';
export default function Page() {
  return <main><Switch /><Mode /><Pair /><NoServer /><Ids /><Hooked /></main>;
}
`,
	// The same components from a package on a synchronous page: Preact
	// islands, mounted by the island entry rather than the client-boundary
	// loader. (Project "use client" components on a synchronous page are
	// compiled to Krate's own runtime instead.)
	"node_modules/store-widgets/package.json": `{"name":"store-widgets","version":"1.0.0","type":"module","main":"index.js"}`,
	"node_modules/store-widgets/index.js": `import { jsx, jsxs } from 'react/jsx-runtime';
import { useEffect, useId, useState, useSyncExternalStore } from 'react';
const listeners = new Set();
function subscribe(cb) { listeners.add(cb); return () => listeners.delete(cb); }
const read = () => localStorage.getItem('mode') ?? 'a';
const serverRead = () => 'a';
function setMode(v) { localStorage.setItem('mode', v); listeners.forEach((l) => l()); }
export function Mode() {
  const v = useSyncExternalStore(subscribe, read, serverRead);
  return v === 'b' ? jsx('section', { id: 'mode', 'data-mode': 'b', children: jsx('b', { children: 'B' }) }) : jsx('div', { id: 'mode', 'data-mode': 'a', children: jsx('i', { children: 'A' }) });
}
export function Switch() {
  const v = useSyncExternalStore(subscribe, read, serverRead);
  return jsxs('div', { role: 'radiogroup', children: [
    jsx('button', { id: 'sw-a', role: 'radio', 'aria-checked': v === 'a', onClick: () => setMode('a'), children: 'A' }),
    jsx('button', { id: 'sw-b', role: 'radio', 'aria-checked': v === 'b', onClick: () => setMode('b'), children: 'B' }),
  ] });
}
function Reader({ id }) { return jsx('span', { id, children: useSyncExternalStore(subscribe, read, serverRead) }); }
export function Pair() {
  const [late, setLate] = useState(false);
  useEffect(() => setLate(true), []);
  return jsxs('p', { children: [jsx(Reader, { id: 'early' }), late ? jsx(Reader, { id: 'late' }) : null] });
}
export function NoServer() {
  return jsx('span', { id: 'noserver', children: useSyncExternalStore(subscribe, () => globalThis.__noServer ?? 'a') });
}
export function Ids() {
  const id = useId();
  const [n, setN] = useState(0);
  return jsxs('div', { children: [jsx('label', { id: 'lbl', htmlFor: id, children: 'name' }), jsx('input', { id: 'in', 'data-id': id }), jsx('button', { id: 'rerender', onClick: () => setN(n + 1), children: n })] });
}
`,
	"src/pages/islands.tsx": `import { Mode, Switch, Pair, NoServer, Ids } from 'store-widgets';
export default function Page() {
  return <main><Switch /><Mode /><Pair /><NoServer /><Ids /></main>;
}
`,
	"src/pages/real.tsx": `import RealMode from '../components/RealMode';
export default function Page() { return <main><RealMode /></main>; }
`,
}

// TestUseClientIslandBrowserSyncExternalStore loads each page with "b" saved:
// Preact client boundaries, Preact package islands, the same components
// compiled to Krate's runtime, and a real-React island. On the Preact pages
// every frame after hydration must show "b". It also checks that a reader
// mounted after hydration agrees, that a store without getServerSnapshot
// still reads getSnapshot while hydrating, that useId keeps the server's ids,
// that hooks called inline in JSX work on Krate's runtime, and that the real-
// React island behaves as React does.
func TestUseClientIslandBrowserSyncExternalStore(t *testing.T) {
	t.Parallel()
	e2eEnv(t)
	b, root := scProject(t, syncStoreFiles)
	withPreact(t, root)
	if err := linkRuntimePackage(t, filepath.Join(root, "node_modules", "@krate")); err != nil {
		t.Fatal(err)
	}
	realReactFixture(t, root)
	if out, err := captureBuild(b); err != nil {
		t.Fatalf("BuildAll: %v\n%s", err, out)
	}
	// The shared Preact modules every Preact island and client component
	// imports carry the hydration mark and the hook.
	for name, want := range map[string]string{"preact-preact.*.js": "__krateHydrating", "preact-preact-compat.*.js": "__krateHydrating"} {
		if src := readBuilt(t, mustGlobOne(t, filepath.Join(b.Cfg.OutDir, "chunks", name))); !strings.Contains(src, want) {
			t.Fatalf("%s lacks %s", name, want)
		}
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
	script, err := filepath.Abs(filepath.Join("testdata", "browser_sync_external_store.mjs"))
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
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	var res map[string]any
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &res); err != nil {
		t.Fatalf("parsing browser output: %v\n%s", err, out)
	}
	t.Logf("%s", lines[len(lines)-1])
}
