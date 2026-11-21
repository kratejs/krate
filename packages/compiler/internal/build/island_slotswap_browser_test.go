package build

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// islandSlotSwapFiles mirrors an admin section of a real site: server layouts
// render "use client" wrappers around {children}, each its own island with a
// server-children slot. Providers (root), ShellFrame (renders <main> around
// its slot and reads usePathname, so it re-renders after every navigation)
// and, in the bot layout, BotWorkspaceProvider (a context provider whose
// props are the same on every bot page, so navigations keep it mounted).
// Each bot page puts other islands in the provider's slot, so the slot
// changes shape; a cookie changes the provider's props and page B's shape on
// a refresh.
var islandSlotSwapFiles = map[string]string{
	"src/components/track.ts": `export function track(name: string) {
  const w = window as any;
  w.__mounts = w.__mounts || {};
  w.__mounts[name] = (w.__mounts[name] || 0) + 1;
  w["__" + name] = "mounted";
  return () => { w["__" + name] = "unmounted"; };
}
`,
	"src/components/providers.tsx": `"use client";
export function Providers({ children }: { children: any }) { return <>{children}<section id="toaster" /></>; }
`,
	"src/components/shell-frame.tsx": `"use client";
import { useState } from "react";
import { usePathname } from "@krate/runtime";
export function ShellFrame({ children }: { children: any }) {
  const [n, setN] = useState(0);
  const path = usePathname();
  return <div class="frame"><button id="bump" onClick={() => setN(n + 1)}>bump {n}</button><nav data-path={path}><a id="nav-a" href="/ws/bot/a/" data-krate-link>a</a><a id="nav-m" href="/ws/bot/m/" data-krate-link>m</a><a id="nav-c" href="/ws/c/" data-krate-link>c</a></nav><main id="main">{children}</main></div>;
}
`,
	"src/components/ws-context.tsx": `"use client";
import { createContext, useEffect } from "react";
import { track } from "./track";
export const BotWs = createContext("none");
export function BotWorkspaceProvider({ value, children }: { value: string; children: any }) {
  useEffect(() => track("bwp"), []);
  return <BotWs.Provider value={value}>{children}</BotWs.Provider>;
}
`,
	"src/components/modules-list.tsx": `"use client";
import { useContext, useEffect, useState } from "react";
import { createPortal } from "react-dom";
import { BotWs } from "./ws-context";
import { track } from "./track";
export function ModulesList() {
  const ws = useContext(BotWs);
  const [open, setOpen] = useState(false);
  useEffect(() => { setOpen(true); return track("x"); }, []);
  return <ul id="x">{ws}{open ? createPortal(<div id="x-portal">portal</div>, document.body) : null}</ul>;
}
`,
	"src/components/brand-form.tsx": `"use client";
import { useContext, useEffect } from "react";
import { BotWs } from "./ws-context";
import { track } from "./track";
export function BrandForm() {
  const ws = useContext(BotWs);
  useEffect(() => track("y"), []);
  return <form id="y">{ws}</form>;
}
`,
	"src/components/save-bar.tsx": `"use client";
import { useContext, useEffect, useState } from "react";
import { BotWs } from "./ws-context";
import { track } from "./track";
import { save } from "../actions/save";
export function SaveBar() {
  const ws = useContext(BotWs);
  const [note, setNote] = useState("");
  useEffect(() => track("z"), []);
  return <div id="z">{ws}<button id="save" onClick={async () => { await save(); setNote("saved"); }}>save</button>{note}</div>;
}
`,
	"src/actions/save.ts": `"use server";
import { revalidatePath } from "@krate/runtime/server";
export async function save() { revalidatePath("/ws/bot/elsewhere"); return true; }
`,
	"src/components/frame.tsx": `import { ShellFrame } from "./shell-frame";
export async function Frame({ children }: { children: any }) {
  await Promise.resolve();
  return <ShellFrame>{children}</ShellFrame>;
}
`,
	"src/components/workspace.tsx": `import { cookies } from "@krate/runtime/server";
import { BotWorkspaceProvider } from "./ws-context";
export async function Workspace({ children }: { children: any }) {
  const v = (await cookies()).get("v")?.value ?? "0";
  return <BotWorkspaceProvider value={"genesis-test@" + v}>{children}</BotWorkspaceProvider>;
}
`,
	"src/pages/ws/_layout.tsx": `import { Providers } from "../../components/providers";
import { Frame } from "../../components/frame";
export default async function Layout({ children }: { children: any }) {
  await Promise.resolve();
  return <Providers><Frame>{children}</Frame></Providers>;
}`,
	"src/pages/ws/bot/_layout.tsx": `import { Workspace } from "../../../components/workspace";
export default async function BotLayout({ children }: { children: any }) {
  await Promise.resolve();
  return <Workspace>{children}</Workspace>;
}`,
	"src/pages/ws/bot/a.tsx": `import { ModulesList } from "../../../components/modules-list";
export default async function A() {
  await Promise.resolve();
  return <><div class="bar">bar</div><header>Modules page</header><ModulesList /><a id="to-b" data-krate-link href="/ws/bot/b/">brand</a></>;
}`,
	"src/pages/ws/bot/b.tsx": `import { cookies } from "@krate/runtime/server";
import { BrandForm } from "../../../components/brand-form";
import { SaveBar } from "../../../components/save-bar";
export default async function B() {
  const v = (await cookies()).get("v")?.value ?? "0";
  if (v === "2") return <><div class="bar">bar</div><header>Brand page saved</header><SaveBar /></>;
  if (v === "1") return <><div class="bar">bar</div><header>Brand page</header><BrandForm /><SaveBar /><a id="to-a" data-krate-link href="/ws/bot/a/">modules</a></>;
  return <><div class="bar">bar</div><header>Brand page</header><BrandForm /><a id="to-a" data-krate-link href="/ws/bot/a/">modules</a></>;
}`,
	"src/components/module-rows.tsx": `"use client";
import { useContext, useEffect } from "react";
import { BotWs } from "./ws-context";
import { track } from "./track";
export function ModuleRows() {
  const ws = useContext(BotWs);
  useEffect(() => track("w"), []);
  return <ol id="w">{ws}</ol>;
}
`,
	"src/components/module-detail.tsx": `"use client";
import { useContext, useEffect } from "react";
import { BotWs } from "./ws-context";
import { track } from "./track";
export function ModuleDetail({ name }: { name: string }) {
  const ws = useContext(BotWs);
  useEffect(() => track("v"), []);
  return <article id="v">{name} {ws}</article>;
}
`,
	// A list page and a detail page under the same provider: an in-page link
	// from one to the other, and back.
	"src/pages/ws/bot/m/index.tsx": `import { ModuleRows } from "../../../../components/module-rows";
export default async function List() {
  await Promise.resolve();
  return <><header>Module list</header><ModuleRows /><a id="to-detail" data-krate-link href="/ws/bot/m/base/">base</a></>;
}`,
	"src/pages/ws/bot/m/base.tsx": `import { ModuleDetail } from "../../../../components/module-detail";
export default async function Detail() {
  await Promise.resolve();
  return <><nav><a id="to-list" data-krate-link href="/ws/bot/m/">all modules</a></nav><section><header>Module detail</header><ModuleDetail name="base" /></section></>;
}`,
	"src/pages/ws/c.tsx": `export default async function C() {
  await Promise.resolve();
  return <section id="c-page"><header>Settings page</header></section>;
}`,
	// A compiled page with state gives the site its runtime chunk (the router).
	"src/pages/index.tsx": `import { useState } from "react";
export default function Home() {
  const [n, setN] = useState(0);
  return <button id="count" onClick={() => setN(n + 1)}>{n}</button>;
}`,
}

// TestBrowserKeptProviderSlotChangesShape walks the section above with
// client navigations, back/forward, router.refresh() and a revalidating
// server action, and after every step checks the islands: the kept wrappers
// stay mounted (ShellFrame's counter survives), the provider's range holds
// exactly the current page's content and nothing follows it, islands of the
// content that went are unmounted (effect cleanup ran, portal gone), and
// every mounted island's markers are well formed in the document with no
// server-children markers left inside.
func TestBrowserKeptProviderSlotChangesShape(t *testing.T) {
	t.Parallel()
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
	b, root := scProject(t, islandSlotSwapFiles)
	withPreact(t, root)
	linkRuntimeDist(t, root)
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
	script, err := filepath.Abs(filepath.Join("testdata", "island_slotswap.mjs"))
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
	type view struct {
		Main, Range, After, Bump string
		Status                   map[string]string
		Portals                  int
		SameBump, HasBWP         bool
		Problems                 []string
	}
	var res struct {
		Steps  map[string]view
		Order  []string
		Errors []string
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &res); err != nil {
		t.Fatalf("parsing browser output: %v\n%s", err, out)
	}
	type want struct {
		has, not  []string
		mounted   []string
		unmounted []string
		portals   int
		bwp       bool
	}
	wants := map[string]want{
		"a":       {[]string{"Modules page", "genesis-test@0"}, []string{"Brand page"}, []string{"x", "bwp"}, nil, 1, true},
		"b":       {[]string{"Brand page", "genesis-test@0"}, []string{"Modules page"}, []string{"y", "bwp"}, []string{"x"}, 0, true},
		"back":    {[]string{"Modules page"}, []string{"Brand page"}, []string{"x", "bwp"}, []string{"y"}, 1, true},
		"forward": {[]string{"Brand page"}, []string{"Modules page"}, []string{"y", "bwp"}, []string{"x"}, 0, true},
		// router.refresh(): the provider's props change and its slot changes shape.
		"refresh": {[]string{"Brand page", "genesis-test@1", "save"}, []string{"Modules page"}, []string{"y", "z", "bwp"}, []string{"x"}, 0, true},
		// The refresh after a server action that revalidated: again both.
		"action": {[]string{"Brand page saved", "genesis-test@2"}, []string{"Modules page", "modules"}, []string{"z", "bwp"}, []string{"x", "y"}, 0, true},
		// List -> detail -> list under the same provider (props unchanged).
		"list":    {[]string{"Module list", "genesis-test@2"}, []string{"Brand page", "Module detail"}, []string{"w", "bwp"}, []string{"x", "y", "z"}, 0, true},
		"detail":  {[]string{"Module detail", "base genesis-test@2"}, []string{"Module list"}, []string{"v", "bwp"}, []string{"w"}, 0, true},
		"list2":   {[]string{"Module list"}, []string{"Module detail"}, []string{"w", "bwp"}, []string{"v"}, 0, true},
		"detail2": {[]string{"Module detail"}, []string{"Module list"}, []string{"v", "bwp"}, []string{"w"}, 0, true},
		// Leaving the bot section: the provider goes, ShellFrame stays.
		"c": {[]string{"Settings page"}, []string{"Module detail"}, nil, []string{"v", "w", "bwp"}, 0, false},
	}
	for _, name := range res.Order {
		v := res.Steps[name]
		w, ok := wants[name]
		if !ok {
			continue
		}
		text := v.Main
		if w.bwp {
			text = v.Range
			if v.After != "" {
				t.Errorf("%s: nothing may follow the provider's range in <main>: %q", name, v.After)
			}
		}
		if v.HasBWP != w.bwp {
			t.Errorf("%s: provider mounted = %v, want %v", name, v.HasBWP, w.bwp)
		}
		for _, s := range w.has {
			if !strings.Contains(text, s) {
				t.Errorf("%s: %q missing from %q", name, s, text)
			}
		}
		for _, s := range w.not {
			if strings.Contains(text, s) {
				t.Errorf("%s: %q must be gone from %q", name, s, text)
			}
		}
		for _, k := range w.mounted {
			if v.Status[k] != "mounted" {
				t.Errorf("%s: island %s must be mounted (status %v)", name, k, v.Status)
			}
		}
		for _, k := range w.unmounted {
			if v.Status[k] != "unmounted" && v.Status[k] != "" {
				t.Errorf("%s: island %s must be unmounted (status %v)", name, k, v.Status)
			}
		}
		if v.Portals != w.portals {
			t.Errorf("%s: %d portals, want %d", name, v.Portals, w.portals)
		}
		if !v.SameBump || v.Bump != "bump 2" {
			t.Errorf("%s: ShellFrame must stay mounted with its state: same=%v %q", name, v.SameBump, v.Bump)
		}
		if len(v.Problems) > 0 {
			t.Errorf("%s: island invariants: %v", name, v.Problems)
		}
	}
	if len(res.Order) != len(wants) {
		t.Errorf("steps run: %v\n%s", res.Order, out)
	}
	if len(res.Errors) > 0 {
		t.Errorf("page errors: %v", res.Errors)
	}
}
