package build

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// islandRouterFiles is a server-rendered app whose /templates page has
// islands only: no page script, so nothing but the runtime chunk can start
// the SPA router. The counter page gives the site a runtime chunk (a site
// whose pages are all island-only ships none).
var islandRouterFiles = map[string]string{
	"src/components/Filters.tsx": `"use client";
import { usePathname, useRouter, useSearchParams } from "@krate/runtime";
export default function Filters() {
  const router = useRouter();
  const pathname = usePathname();
  const params = useSearchParams();
  return <input id="search" defaultValue={params.get("q") ?? ""} onKeyDown={(e: any) => {
    if (e.key === "Escape") router.replace(pathname, { scroll: false });
  }} />;
}`,
	"src/pages/templates.tsx": `import Filters from "../components/Filters";
export default async function Templates() {
  await Promise.resolve();
  return <main><Filters /><a id="to-store" data-krate-link href="/store/">store</a></main>;
}`,
	"src/pages/store.tsx": `export default async function Store() { await Promise.resolve(); return <main id="store-page">store</main>; }`,
	"src/pages/index.tsx": `import { useState } from "react";
export default function Home() {
  const [n, setN] = useState(0);
  return <button id="count" onClick={() => setN(n + 1)}>{n}</button>;
}`,
}

// TestIslandOnlyPageRuntimeStartsRouter: a page with islands and no page
// script loads the runtime chunk, and the chunk starts the router itself
// (once: only when no router registered yet) after the document ran.
func TestIslandOnlyPageRuntimeStartsRouter(t *testing.T) {
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
	base, err := startKrateServer(t, root, b.Cfg)
	if err != nil {
		t.Fatalf("serve: %v", err)
	}
	resp, err := http.Get(base + "/templates/")
	if err != nil {
		t.Fatal(err)
	}
	html, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	page := string(html)
	if !strings.Contains(page, "<!--krate-island:") || !strings.Contains(page, "island-loader.") {
		t.Fatalf("templates is not an island page:\n%s", page)
	}
	srcs := regexp.MustCompile(`<script[^>]*\ssrc="?([^"\s>]+)`).FindAllStringSubmatch(page, -1)
	var runtime string
	for _, m := range srcs {
		switch {
		case strings.Contains(m[1], "chunks/runtime."):
			runtime = m[1]
		case strings.Contains(m[1], "chunks/island-loader."):
		default:
			t.Errorf("unexpected script %s: the page should load no page script", m[1])
		}
	}
	if runtime == "" {
		t.Fatalf("no runtime chunk on the island page:\n%s", page)
	}
	js, err := os.ReadFile(filepath.Join(root, "dist", filepath.FromSlash(strings.TrimPrefix(runtime, "/"))))
	if err != nil {
		t.Fatal(err)
	}
	code := string(js)
	for _, want := range []string{`Symbol.for("krate.router")`, "initRouter()", "DOMContentLoaded"} {
		if !strings.Contains(code, want) {
			t.Errorf("runtime chunk lacks the router start (%s)", want)
		}
	}
}

// TestIslandOnlyPageRouterReplaceIsSoftNavigation runs the page in a browser:
// the island's router.replace() goes through the page router (no document
// load, focus kept), and links are intercepted.
func TestIslandOnlyPageRouterReplaceIsSoftNavigation(t *testing.T) {
	browser := strings.ToLower(strings.TrimSpace(os.Getenv("KRATE_BROWSER")))
	if browser == "" {
		t.Skip("set KRATE_BROWSER=chromium|firefox|webkit to run the island router browser test")
	}
	if !canStopInProcessServer {
		t.Skip("in-process server needs a unix host")
	}
	ensureRuntimeDist(t)
	if err := exec.Command("node", "-e", "require.resolve('playwright')").Run(); err != nil {
		requireE2E(t, "playwright is not installed (run pnpm install)")
	}
	b, root := scProject(t, islandRouterFiles)
	withPreact(t, root)
	linkRuntimeDist(t, root)
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
	script, err := filepath.Abs(filepath.Join("testdata", "island_router.mjs"))
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
		Started    bool
		Documents  int
		Marker     string
		NavStarts  int
		Search     string
		Focused    string
		LinkMarker string
		Errors     []string
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &res); err != nil {
		t.Fatalf("parsing browser output: %v\n%s", err, out)
	}
	if !res.Started || res.Documents != 1 || res.Marker != "kept" || res.NavStarts < 1 || res.Search != "" || res.Focused != "search" ||
		res.LinkMarker != "kept" || len(res.Errors) > 0 {
		t.Fatalf("unexpected browser result: %+v", res)
	}
}

// linkRuntimeDist makes the built runtime (split features and ES modules
// included) visible at root/packages/runtime/dist, where the build looks for
// it, so the site gets the runtime chunk a real build writes.
func linkRuntimeDist(t *testing.T, root string) {
	t.Helper()
	repo, err := repoRootPath()
	if err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(repo, "packages", "runtime", "dist")
	if _, err := os.Stat(filepath.Join(src, "krate-features.json")); err != nil {
		requireE2E(t, "built runtime missing (run pnpm --filter @krate/runtime build): %v", err)
		t.Skip("built runtime missing")
	}
	dst := filepath.Join(root, "packages", "runtime", "dist")
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(src, dst); err != nil {
		t.Skipf("no symlinks: %v", err)
	}
}
