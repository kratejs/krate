package build

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestRouterHooksInClientComponentsReturnValues: a "use client" nav and a
// filters component under a server-rendered layout, one of them importing a
// React package, read usePathname() as a string and useSearchParams() as a
// URLSearchParams, and follow client-side navigation.
func TestRouterHooksInClientComponentsReturnValues(t *testing.T) {
	t.Parallel()
	browser := strings.ToLower(strings.TrimSpace(os.Getenv("KRATE_BROWSER")))
	if browser == "" {
		t.Skip("set KRATE_BROWSER=chromium|firefox|webkit to run the router hooks browser test")
	}
	if !canStopInProcessServer {
		t.Skip("in-process server needs a unix host")
	}
	ensureRuntimeDist(t)
	if err := exec.Command("node", "-e", "require.resolve('playwright')").Run(); err != nil {
		requireE2E(t, "playwright is not installed (run pnpm install)")
	}
	b, root := scProject(t, map[string]string{
		"src/components/Nav.tsx": `"use client";
import { usePathname } from "@krate/runtime";
import { Group } from "fake-group";
import { useState } from "react";
const isStore = (p: string) => p === "/store" || p.startsWith("/store/");
const isTemplates = (p: string) => p === "/templates" || p.startsWith("/templates/");
export function Nav() {
  const pathname = usePathname();
  const [ready] = useState(true);
  const items = [
    { id: "nav-store", href: "/store/", active: isStore(pathname) },
    { id: "nav-templates", href: "/templates/?q=abc", active: isTemplates(pathname) },
  ];
  return <Group id="nav">{items.map((it) => <a key={it.id} id={it.id} href={it.href} aria-current={it.active && ready ? "page" : undefined}>{it.id}</a>)}</Group>;
}`,
		"src/components/StoreFilters.tsx": `"use client";
import { useRouter, useSearchParams } from "@krate/runtime";
import { useState } from "react";
export default function StoreFilters() {
  const params = useSearchParams();
  const router = useRouter();
  const [n] = useState(0);
  return <section id="filters" data-router={typeof router.push + n}><span id="q">{params.get("q") ?? "none"}</span></section>;
}`,
		"src/pages/_layout.tsx": `import { Nav } from "../components/Nav";
export default async function Layout({ children }: { children: any }) {
  await Promise.resolve();
  return <div><Nav />{children}</div>;
}`,
		"src/pages/index.tsx": `export default async function Home() { await Promise.resolve(); return <main>home</main>; }`,
		"src/pages/store.tsx": `export default async function Store() { await Promise.resolve(); return <main>store</main>; }`,
		"src/pages/templates.tsx": `import StoreFilters from "../components/StoreFilters";
export default async function Templates() { await Promise.resolve(); return <main><StoreFilters /></main>; }`,
		"node_modules/fake-group/package.json": `{"name":"fake-group","version":"1.0.0","main":"index.js","peerDependencies":{"react":"*"}}`,
		"node_modules/fake-group/index.js": `import React from "react";
export function Group(props) { return React.createElement("nav", { id: props.id }, props.children); }`,
	})
	withPreact(t, root)
	if err := linkRuntimePackage(t, filepath.Join(root, "node_modules", "@krate")); err != nil {
		t.Fatal(err)
	}
	if err := b.BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	base, err := startKrateServer(t, root, b.Cfg)
	if err != nil {
		t.Fatalf("serve: %v", err)
	}
	script, err := filepath.Abs(filepath.Join("testdata", "client_nav_hooks.mjs"))
	if err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("node", script, base, browser).CombinedOutput()
	if err != nil {
		if strings.Contains(string(out), "Executable doesn't exist") {
			requireE2E(t, "%s browser is not installed: %s", browser, out)
		}
		t.Fatalf("browser script failed: %v\n%s", err, out)
	}
	var res struct {
		Home, Store, Templates, Q1, Q2, Router string
		Errors                                 []string
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &res); err != nil {
		t.Fatalf("parsing browser output: %v\n%s", err, out)
	}
	if res.Home != "" || res.Store != "nav-store" || res.Templates != "nav-templates" || res.Q1 != "abc" || res.Q2 != "direct" ||
		res.Router != "function0" || len(res.Errors) > 0 {
		t.Fatalf("unexpected browser result: %+v", res)
	}
}
