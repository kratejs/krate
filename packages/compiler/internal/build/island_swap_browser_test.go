package build

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// islandSwapFiles is an app of server-rendered pages with islands only (no
// page scripts) whose islands open a dialog in a portal and call server
// actions that revalidate, plus a search page whose island drives the URL.
// ShareButton runs on Preact with Radix's Dialog; RealShare and RealFilters
// import a react-reconciler library, so they run on real React.
var islandSwapFiles = map[string]string{
	"src/actions/share.ts": `"use server";
import { revalidatePath } from "@krate/runtime/server";
export async function createShare(id: number) { revalidatePath("/shares"); return "/s/" + id; }
export async function revokeShare(id: number) { revalidatePath("/shares"); return id; }
export async function touchOrder(id: number) { revalidatePath("/order"); revalidatePath("/realorder"); return id; }
`,
	"src/components/share-button.tsx": `"use client";
import { useState } from "react";
import { Dialog } from "radix-ui";
import { createShare, revokeShare, touchOrder } from "../actions/share";
export function ShareButton({ orderId }: { orderId: number }) {
  const [open, setOpen] = useState(false);
  const [link, setLink] = useState<string | null>(null);
  const [note, setNote] = useState("");
  return (
    <div className="share">
      <button id="share" onClick={async () => { setLink(await createShare(orderId)); setOpen(true); }}>Share</button>
      <output id="note">{note}</output>
      <Dialog.Root open={open} onOpenChange={setOpen}>
        <Dialog.Portal>
          <Dialog.Content aria-describedby={undefined}>
            <Dialog.Title>Share link</Dialog.Title>
            <p id="share-url">{link}</p>
            <button id="revoke" onClick={async () => { await revokeShare(orderId); setLink(null); setNote("Link revoked"); setOpen(false); }}>Revoke</button>
            <button id="touch" onClick={async () => { await touchOrder(orderId); setNote("Touched"); setOpen(false); }}>Touch</button>
          </Dialog.Content>
        </Dialog.Portal>
      </Dialog.Root>
    </div>
  );
}
`,
	"src/components/real-share.tsx": `"use client";
import { useState } from "react";
import { createPortal } from "react-dom";
import { Canvas, Rect } from "fake-canvas";
import { createShare, revokeShare, touchOrder } from "../actions/share";
export function RealShare({ orderId }: { orderId: number }) {
  const [open, setOpen] = useState(false);
  const [link, setLink] = useState<string | null>(null);
  const [note, setNote] = useState("");
  return (
    <div className="share">
      <Canvas id="cv" width={4} height={4}><Rect x={0} y={0} w={1} h={1} fill="red" /></Canvas>
      <button id="share" onClick={async () => { setLink(await createShare(orderId)); setOpen(true); }}>Share</button>
      <output id="note">{note}</output>
      {open ? createPortal(
        <div role="dialog">
          <p id="share-url">{link}</p>
          <button id="revoke" onClick={async () => { await revokeShare(orderId); setLink(null); setNote("Link revoked"); setOpen(false); }}>Revoke</button>
          <button id="touch" onClick={async () => { await touchOrder(orderId); setNote("Touched"); setOpen(false); }}>Touch</button>
        </div>, document.body) : null}
    </div>
  );
}
`,
	"src/components/filters.tsx": `"use client";
import { useEffect, useState } from "react";
import { usePathname, useRouter, useSearchParams } from "@krate/runtime";
export function Filters() {
  const router = useRouter();
  const pathname = usePathname();
  const params = useSearchParams();
  const [value, setValue] = useState(params.get("q") ?? "");
  useEffect(() => { (window as any).__mounts = ((window as any).__mounts || 0) + 1; }, []);
  return (
    <div className="filters">
      <input id="search" aria-label="Search" value={value} onChange={(e: any) => setValue(e.currentTarget.value)} onKeyDown={(e: any) => {
        if (e.key === "Enter") router.replace(pathname + "?q=" + encodeURIComponent(value), { scroll: false });
        if (e.key === "Escape") { setValue(""); router.replace(pathname, { scroll: false }); }
      }} />
      <output id="seen">{params.get("q") ?? ""}</output>
    </div>
  );
}
`,
	"src/components/real-filters.tsx": `"use client";
import { useEffect, useState } from "react";
import { Canvas } from "fake-canvas";
import { usePathname, useRouter, useSearchParams } from "@krate/runtime";
export function RealFilters() {
  const router = useRouter();
  const pathname = usePathname();
  const params = useSearchParams();
  const [value, setValue] = useState(params.get("q") ?? "");
  useEffect(() => { (window as any).__mounts = ((window as any).__mounts || 0) + 1; }, []);
  return (
    <div className="filters">
      <Canvas width={1} height={1} />
      <input id="search" aria-label="Search" value={value} onChange={(e: any) => setValue(e.currentTarget.value)} onKeyDown={(e: any) => {
        if (e.key === "Enter") router.replace(pathname + "?q=" + encodeURIComponent(value), { scroll: false });
        if (e.key === "Escape") { setValue(""); router.replace(pathname, { scroll: false }); }
      }} />
      <output id="seen">{params.get("q") ?? ""}</output>
    </div>
  );
}
`,
	"src/pages/order.tsx": `import { ShareButton } from "../components/share-button";
export default async function Order({ searchParams }: any) {
  await searchParams;
  return <main><h1>Order</h1><p id="stamp">{String(Date.now() + Math.random())}</p><div class="bar"><ShareButton orderId={1} /></div><a id="to-other" data-krate-link href="/other/">other</a></main>;
}`,
	"src/pages/realorder.tsx": `import { RealShare } from "../components/real-share";
export default async function Order({ searchParams }: any) {
  await searchParams;
  return <main><h1>Order</h1><p id="stamp">{String(Date.now() + Math.random())}</p><div class="bar"><RealShare orderId={1} /></div><a id="to-other" data-krate-link href="/other/">other</a></main>;
}`,
	"src/pages/search.tsx": `import { Filters } from "../components/filters";
export default async function Search({ searchParams }: any) {
  const { q } = await searchParams;
  return <main><p id="server-q">{q ?? ""}</p><section><Filters /></section><ul><li>{q || "all"}</li></ul></main>;
}`,
	"src/pages/realsearch.tsx": `import { RealFilters } from "../components/real-filters";
export default async function Search({ searchParams }: any) {
  const { q } = await searchParams;
  return <main><p id="server-q">{q ?? ""}</p><section><RealFilters /></section><ul><li>{q || "all"}</li></ul></main>;
}`,
	"src/components/providers.tsx": `"use client";
import { useEffect, useState } from "react";
export function Providers({ children }: { children: any }) {
  const [n] = useState(1);
  useEffect(() => { (window as any).__providers = ((window as any).__providers || 0) + 1; }, []);
  return <>{children}<section id="toaster" data-n={n} /></>;
}
`,
	"src/components/shell.tsx": `import { Filters } from "./filters";
export async function Shell({ q }: { q?: string }) {
  await Promise.resolve();
  return <div class="shell"><main><p id="server-q">{q ?? ""}</p><section><Filters /></section><ul>{(q ? [q] : ["a", "b"]).map((x) => <li>{x}</li>)}</ul></main></div>;
}
`,
	"src/pages/wrapped.tsx": `import { Providers } from "../components/providers";
import { Shell } from "../components/shell";
export default async function Wrapped({ searchParams }: any) {
  const { q } = await searchParams;
  return <Providers><Shell q={q} /></Providers>;
}`,
	"src/pages/other.tsx": `export default async function Other() { await Promise.resolve(); return <main id="other-page">other</main>; }`,
	"src/pages/index.tsx": `import { useState } from "react";
export default function Home() {
  const [n, setN] = useState(0);
  return <button id="count" onClick={() => setN(n + 1)}>{n}</button>;
}`,
}

// TestIslandLoaderShipsIslandSwap: the island loader chunk carries the
// runtime's island-preserving swap, and the runtime chunk does not.
func TestIslandLoaderShipsIslandSwap(t *testing.T) {
	ensureRuntimeDist(t)
	b, root := scProject(t, islandRouterFiles)
	b.Cfg.Minify = true
	b.Cfg.MinifyJS = true
	withPreact(t, root)
	linkRuntimeDist(t, root)
	if err := linkRuntimePackage(t, filepath.Join(root, "node_modules", "@krate")); err != nil {
		t.Fatal(err)
	}
	if err := b.BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	chunks, err := filepath.Glob(filepath.Join(root, "dist", "chunks", "*.js"))
	if err != nil {
		t.Fatal(err)
	}
	var loader, runtime string
	for _, c := range chunks {
		data, err := os.ReadFile(c)
		if err != nil {
			t.Fatal(err)
		}
		switch base := filepath.Base(c); {
		case strings.HasPrefix(base, "island-loader."):
			loader = string(data)
		case strings.HasPrefix(base, "runtime."):
			runtime = string(data)
		}
	}
	if !strings.Contains(loader, "__krateIslandSwap") || !strings.Contains(loader, "__krateIslands") {
		t.Errorf("the island loader chunk must install the island swap:\n%.400s", loader)
	}
	if runtime == "" || !strings.Contains(runtime, "__krateIslandSwap") || strings.Contains(runtime, "krate-slot:") {
		t.Errorf("the runtime chunk must use the island swap only through the global (found the router: %v)", strings.Contains(runtime, "__krateIslandSwap"))
	}
}

// TestBrowserIslandSwapKeepsIslands runs router swaps over mounted islands in
// a browser: the refresh after a server action that revalidates (another
// page, or the current one) keeps the island mounted (same nodes, state, the
// dialog closes through its own root, no second copy); router.replace to the
// same page keeps a focused input focused and re-renders the island with the
// new search params; a navigation that drops the island unmounts it, so its
// portal goes too. Preact (Radix Dialog) and real-React islands.
func TestBrowserIslandSwapKeepsIslands(t *testing.T) {
	browser := strings.ToLower(strings.TrimSpace(os.Getenv("KRATE_BROWSER")))
	if browser == "" {
		t.Skip("set KRATE_BROWSER=chromium|firefox|webkit to run browser checks")
	}
	if !canStopInProcessServer {
		t.Skip("in-process server needs a unix host")
	}
	ensureRuntimeDist(t)
	if err := exec.Command("node", "-e", "require.resolve('playwright')").Run(); err != nil {
		requireE2E(t, "playwright is not installed (run pnpm install)")
	}
	radix, err := filepath.Abs(filepath.Join("..", "..", "..", "..", "examples", "node_modules", "radix-ui"))
	if err == nil {
		radix, err = filepath.EvalSymlinks(radix)
	}
	if err != nil {
		requireE2E(t, "radix-ui is not installed for the examples (run pnpm install): %v", err)
	}
	repo, err := repoRootPath()
	if err != nil {
		t.Fatal(err)
	}
	b, root := scProject(t, islandSwapFiles)
	withPreact(t, root)
	realReactFixtureFrom(t, root, repo)
	if err := os.Symlink(radix, filepath.Join(root, "node_modules", "radix-ui")); err != nil {
		t.Skipf("cannot link radix-ui: %v", err)
	}
	linkRuntimeDist(t, root)
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
	script, err := filepath.Abs(filepath.Join("testdata", "island_swap.mjs"))
	if err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("node", script, base, browser).CombinedOutput()
	if err != nil {
		if strings.Contains(string(out), "Executable doesn't exist") || strings.Contains(string(out), "browserType.launch") {
			requireE2E(t, "%s browser is not installed: %s", browser, out)
		}
		t.Fatalf("browser script failed: %v\n%s", err, out)
	}
	type step struct {
		Same, Refreshed   bool
		Dialogs, Buttons  int
		Note, URL, Active string
	}
	type share struct {
		Create, Revoke, Touch step
		Leave                 struct{ Dialogs, Guards int }
	}
	type search struct {
		Same, Focused, Cleared bool
		Seen, Server, Value    string
		ClearSeen              string
		Mounts, Blurs          int
	}
	var res struct {
		Preact, Real                           share
		PreactSearch, RealSearch, NestedSearch search
		Providers                              int
		Slot                                   bool
		Errors                                 []string
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &res); err != nil {
		t.Fatalf("parsing browser output: %v\n%s", err, out)
	}
	t.Logf("%+v", res)
	for name, s := range map[string]share{"preact": res.Preact, "real-react": res.Real} {
		if !s.Create.Refreshed || !s.Create.Same || s.Create.Dialogs != 1 || s.Create.Buttons != 1 || s.Create.URL != "/s/1" {
			t.Errorf("%s: after the action that revalidates another page the island must stay mounted with its dialog open (one copy): %+v", name, s.Create)
		}
		if !s.Revoke.Refreshed || !s.Revoke.Same || s.Revoke.Dialogs != 0 || s.Revoke.Buttons != 1 || s.Revoke.Note != "Link revoked" {
			t.Errorf("%s: revoke must close the dialog through the island's own state: %+v", name, s.Revoke)
		}
		if !s.Touch.Refreshed || !s.Touch.Same || s.Touch.Dialogs != 0 || s.Touch.Note != "Touched" {
			t.Errorf("%s: an action that revalidates the current page must keep the island mounted: %+v", name, s.Touch)
		}
		if s.Leave.Dialogs != 0 || s.Leave.Guards != 0 {
			t.Errorf("%s: leaving the page must unmount the island and its open dialog: %+v", name, s.Leave)
		}
	}
	if !res.Slot || res.Providers != 1 {
		t.Errorf("the island around the page (server children holding the search island) must stay mounted: slot %v, mounts %d", res.Slot, res.Providers)
	}
	for name, s := range map[string]search{"preact": res.PreactSearch, "real-react": res.RealSearch, "nested in server children": res.NestedSearch} {
		if !s.Same || !s.Focused || s.Seen != "cvx" || s.Server != "cvx" || s.Value != "cvx" || s.Mounts != 1 || s.Blurs != 0 {
			t.Errorf("%s: router.replace to the same page must keep the island's focused input and show the new search params: %+v", name, s)
		}
		if !s.Cleared || s.ClearSeen != "" {
			t.Errorf("%s: clearing the search must keep the input and show the cleared params: %+v", name, s)
		}
	}
	if len(res.Errors) > 0 {
		t.Errorf("page errors: %v", res.Errors)
	}
}
