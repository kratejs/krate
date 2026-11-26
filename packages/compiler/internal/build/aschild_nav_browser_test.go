package build

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestBrowserAsChildSoftNav covers a server page reached by a client-side
// navigation: a table whose rows hold a client island in one cell and a
// server Button (Radix Slot, asChild) wrapping a Link in the next. The link
// must be in the new page just as in a full load, and must navigate.
func TestBrowserAsChildSoftNav(t *testing.T) {
	t.Parallel()
	browser := strings.ToLower(strings.TrimSpace(os.Getenv("KRATE_BROWSER")))
	if browser == "" {
		t.Skip("set KRATE_BROWSER=chromium|firefox|webkit to run browser checks")
	}
	if err := exec.Command("node", "-e", "require.resolve('playwright')").Run(); err != nil {
		requireE2E(t, "playwright is not installed (run pnpm install)")
	}
	script, err := filepath.Abs(filepath.Join("testdata", "browser_aschild_nav.mjs"))
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
	ensureRuntimeDist(t)
	b, root := scProject(t, map[string]string{
		"src/components/button.tsx": `import { Slot } from 'radix-ui';
export function Button({ asChild = false, ...props }: any) {
  const Comp = asChild ? Slot.Root : 'button';
  return <Comp data-slot="button" {...props} />;
}
`,
		"src/components/grid.tsx": `'use client';
export function Grid({ className, ...props }: any) {
  return <div data-slot="grid-container"><table data-slot="grid" className={className} {...props} /></div>;
}
export function GridBody(props: any) { return <tbody data-slot="grid-body" {...props} />; }
export function GridRow(props: any) { return <tr data-slot="grid-row" {...props} />; }
export function GridCell({ className, ...props }: any) { return <td data-slot="grid-cell" className={className} {...props} />; }
`,
		"src/components/shell.tsx": `'use client';
import { useState } from 'react';
export function Shell({ children, menu }: any) {
  const [n, setN] = useState(0);
  return <div className="shell" data-n={n} onClick={() => setN(n + 1)}>{menu}{children}</div>;
}
`,
		"src/components/menu.tsx": `'use client';
import { useState } from 'react';
export function Menu({ children }: any) {
  const [open, setOpen] = useState(false);
  return <div className="menu" data-open={open ? 'yes' : 'no'} onClick={() => setOpen(!open)}>{children}</div>;
}
`,
		"src/components/providers.tsx": `'use client';
import { createContext } from 'react';
const Ctx = createContext('x');
export function Providers({ children }: any) { return <Ctx.Provider value="y">{children}</Ctx.Provider>; }
`,
		"src/components/panel.tsx": `'use client';
import { useState } from 'react';
import { usePathname } from '@krate/runtime';
export function Panel({ children }: any) {
  const [n, setN] = useState(0);
  const path = usePathname();
  return <section data-path={path} data-n={n} onClick={() => setN(n + 1)}>{children}</section>;
}
`,
		"src/components/badge.tsx": `export function Badge({ children, ...props }: any) {
  return <span data-slot="badge" {...props}>{children}</span>;
}
`,
		"src/components/age.tsx": `'use client';
import { useEffect, useState } from 'react';
export function Age({ label }: { label: string }) {
  const [ready, setReady] = useState(false);
  useEffect(() => setReady(true), []);
  return <span className="age" data-ready={ready ? 'yes' : 'no'}>{label}</span>;
}
`,
		"src/components/frame.tsx": `import { Shell } from './shell';
import { Providers } from './providers';
import { Menu } from './menu';
async function FrameBody({ children }: { children: any }) {
  await Promise.resolve();
  return <Shell menu={<div id="menu"><Menu><Link href="/">Menu</Link></Menu></div>}><header><Link href="/">Home</Link></header>{children}</Shell>;
}
export function Frame({ children }: { children: any }) {
  return <Providers><FrameBody>{children}</FrameBody></Providers>;
}
`,
		"src/pages/loading.tsx": `export default function Loading() { return <div id="loading-ui">Loading...</div>; }
`,
		"src/pages/index.tsx": `import { useState } from 'react';
export default function Home() {
  const [n, setN] = useState(0);
  return <main><button id="count" onClick={() => setN(n + 1)}>{n}</button><Link id="to-table" href="/table">Table</Link><Link id="to-b" href="/table?tab=b">Versions</Link><Link id="to-swap" href="/swap">Swap</Link><Link id="to-swap-b" href="/swap?tab=b">Swap B</Link></main>;
}
`,
		"src/pages/table.tsx": `import { Age } from '../components/age';
import { Button } from '../components/button';
import { Badge } from '../components/badge';
import { Panel } from '../components/panel';
import { Frame } from '../components/frame';
import { Grid, GridBody, GridRow, GridCell } from '../components/grid';
const sets: any = { b: [{ id: 'b1' }, { id: 'b2' }, { id: 'b3' }], c: [], d: [{ id: 'd1' }, { id: 'd2' }], e: [{ id: 'b1' }, { id: 'b2' }, { id: 'b3' }], f: [{ id: 'b3' }, { id: 'b2' }, { id: 'b1' }], g: [{ id: 'b2' }, { id: 'b3' }, { id: 'b1' }] };
export default async function TablePage({ searchParams }: any) {
  await Promise.resolve();
  const tab = searchParams?.tab || 'a';
  const rows = sets[tab] || [];
  return (
    <Frame><main>
      <nav><Link id="tab-a" href="/table">A</Link><Link id="tab-b" href="/table?tab=b">B</Link><Link id="tab-c" href="/table?tab=c">C</Link><Link id="tab-d" href="/table?tab=d">D</Link><Link id="tab-e" href="/table?tab=e">E</Link><Link id="tab-f" href="/table?tab=f">F</Link><Link id="tab-g" href="/table?tab=g">G</Link></nav>
      {tab === 'a' ? (
        <div className="card"><Grid>
          <GridBody>
            {[1, 2, 3].map((n) => (
              <GridRow key={n}>
                <GridCell>t <span className="v">v{n}</span></GridCell>
                <GridCell><Link href={'/item/' + n}>open</Link></GridCell>
                <GridCell className="when">{'a' + n}</GridCell>
                <GridCell><em>x{n}</em></GridCell>
              </GridRow>
            ))}
          </GridBody>
        </Grid></div>
      ) : !rows.length ? (
        <p id="empty">none</p>
      ) : (
        <div className="stack"><div className="card"><Panel><Grid>
          <GridBody>
            {rows.map((r: any) => (
              <GridRow key={r.id}>
                <GridCell>{r.id} <span className="v">v1</span>{r.id.endsWith('2') ? null : <Badge className="ml-2">new</Badge>}</GridCell>
                <GridCell><Link href={'/user/' + r.id}>who</Link></GridCell>
                <GridCell className="when"><Age label={r.id} /> ago</GridCell>
                <GridCell className="act">{tab === 'e' ? <Button size="sm" className="review">Review</Button> : <Button asChild size="sm"><Link href={'/item/' + r.id} className="review">Review</Link></Button>}</GridCell>
              </GridRow>
            ))}
          </GridBody>
        </Grid></Panel></div><p>pager</p></div>
      )}
    </main></Frame>
  );
}
`,
		"src/pages/swap.tsx": `import { Age } from '../components/age';
import { Button } from '../components/button';
import { Badge } from '../components/badge';
import { Grid, GridBody, GridRow, GridCell } from '../components/grid';
import { Panel } from '../components/panel';
import { Frame } from '../components/frame';
async function Rows({ tab }: any) {
  await Promise.resolve();
  return (
    <Grid>
      <GridBody>
        {tab === 'b' ? (
          <GridRow>
            <GridCell>name <span className="v">v1</span></GridCell>
            <GridCell><Age label="x" /></GridCell>
            <GridCell className="act"><Button asChild size="sm"><Link href="/item/9" className="go">Go</Link></Button></GridCell>
          </GridRow>
        ) : (
          <GridRow>
            <GridCell><Badge className="tag">{tab === 'a' ? 'version' : 'other'}</Badge></GridCell>
            <GridCell><Age label="x" /></GridCell>
          </GridRow>
        )}
      </GridBody>
    </Grid>
  );
}
async function Body({ tab }: any) {
  await Promise.resolve();
  return (
    <Panel><main>
      <nav><Link id="s-a" href="/swap">A</Link><Link id="s-b" href="/swap?tab=b">B</Link><Link id="s-c" href="/swap?tab=c">C</Link></nav>
      <div className="outer">
        {tab === 'b' ? <div className="inner"><Rows tab="b" /></div> : <Rows tab={tab} />}
      </div>
    </main></Panel>
  );
}
export default async function SwapPage({ searchParams }: any) {
  await Promise.resolve();
  return <Frame><Body tab={searchParams?.tab || 'a'} /></Frame>;
}
`,
		"src/pages/item/[id].tsx": `export default function Item({ params }: any) { return <main id="item">Item {params.id}</main>; }
`,
	})
	withPreact(t, root)
	linkRuntimeDist(t, root)
	if err := os.Symlink(radix, filepath.Join(root, "node_modules", "radix-ui")); err != nil {
		t.Skipf("cannot link radix-ui: %v", err)
	}
	e2eEnv(t)
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
	out, err := exec.Command("node", script, browser, base).CombinedOutput()
	if err != nil {
		if strings.Contains(string(out), "Executable doesn't exist") || strings.Contains(string(out), "browserType.launch") {
			requireE2E(t, "%s browser is not installed: %s", browser, out)
		}
		t.Fatalf("browser checks failed: %v\n%s", err, out)
	}
}
