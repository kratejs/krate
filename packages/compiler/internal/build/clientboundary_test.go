package build

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/diag"
)

const counterClientSrc = `"use client";
import { useState } from "react";
export default function Counter({ start, label, children }: any) {
  const [n, setN] = useState(start);
  return <div class="counter"><button id="inc" onClick={() => setN(n + 1)}>{label}: {n}</button>{children}</div>;
}
export const LIMIT = 10;
`

// withPreact copies the examples' Preact fixture into a test project.
func withPreact(t *testing.T, root string) {
	t.Helper()
	src := filepath.Join("..", "..", "..", "..", "examples", "node_modules", "preact")
	if _, err := os.Stat(src); err != nil {
		requireE2E(t, "preact fixture not installed: %v", err)
	}
	if err := copyTree(t, src, filepath.Join(root, "node_modules", "preact")); err != nil {
		t.Fatalf("copying preact: %v", err)
	}
}

func buildErr(t *testing.T, files map[string]string) error {
	t.Helper()
	b, _ := scProject(t, files)
	return b.BuildAll()
}

func wantCode(t *testing.T, err error, code diag.Code, contains string) {
	t.Helper()
	if err == nil || !strings.Contains(err.Error(), string(code)) || !strings.Contains(err.Error(), contains) {
		t.Fatalf("expected [%s] mentioning %q, got %v", code, contains, err)
	}
}

func TestUseClientInServerPageBuildsBoundary(t *testing.T) {
	b, root := scProject(t, map[string]string{
		"src/components/Counter.tsx": counterClientSrc,
		"src/pages/index.tsx": `import Counter from "../components/Counter";
export default async function Home() {
  const start = await Promise.resolve(2);
  return <main><Counter start={start} label="n"><b>child</b></Counter></main>;
}`,
	})
	withPreact(t, root)
	if err := b.BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	shell, _ := os.ReadFile(filepath.Join(root, "dist", "index.html"))
	if !strings.Contains(string(shell), `island-loader.`) || !strings.Contains(string(shell), "importmap") {
		t.Errorf("server page shell needs the island loader and the Preact import map:\n%s", shell)
	}
	// The boundary chunk is an entry of the island graph.
	if err := b.writeIslandBundles(); err != nil {
		t.Fatal(err)
	}
	chunks, _ := filepath.Glob(filepath.Join(root, "dist", "chunks", "island_*.js"))
	if len(chunks) != 1 {
		t.Fatalf("expected one client chunk, got %v", chunks)
	}
	chunk, _ := os.ReadFile(chunks[0])
	if !strings.Contains(string(chunk), "mount") || !strings.Contains(string(chunk), "preact") {
		t.Errorf("client chunk should export mount and hydrate with preact:\n%s", chunk)
	}
	impls, _ := filepath.Glob(filepath.Join(root, "dist", ".krate", "server-bundles", "client", "*.ssr.mjs"))
	if len(impls) != 1 {
		t.Fatalf("expected one client SSR module, got %v", impls)
	}
	bundle, _ := os.ReadFile(filepath.Join(root, "dist", ".krate", "server-bundles", "index.server.mjs"))
	for _, want := range []string{"clientBoundary", `"default"`, `"LIMIT"`, "./client/"} {
		if !strings.Contains(string(bundle), want) {
			t.Errorf("page bundle missing %q:\n%s", want, bundle)
		}
	}
}

func TestServerOnlyModuleInClientBoundaryIsAnError(t *testing.T) {
	b, projRoot := scProject(t, map[string]string{
		"src/db.ts": `import "server-only";
export const secret = "s3cret";`,
		"src/components/Leak.tsx": `"use client";
import { secret } from "../db";
export default function Leak() { return <p>{secret}</p>; }`,
		"src/pages/index.tsx": `import Leak from "../components/Leak";
export default async function Home() { await Promise.resolve(); return <main><Leak /></main>; }`,
	})
	withPreact(t, projRoot)
	wantCode(t, b.BuildAll(), diag.CodeServerOnlyInClient, `db.ts imports "server-only"`)
}

// TestUseClientIslandBrowser serves an async page through the real Go server
// and SSR sidecar and clicks the client counter it renders in a browser.
// Requires KRATE_BROWSER (like TestBrowserConformance).
func TestUseClientIslandBrowser(t *testing.T) {
	browser := strings.ToLower(strings.TrimSpace(os.Getenv("KRATE_BROWSER")))
	if browser == "" {
		t.Skip("set KRATE_BROWSER=chromium|firefox|webkit to run the client island browser test")
	}
	if !canStopInProcessServer {
		t.Skip("in-process server needs a unix host")
	}
	ensureRuntimeDist(t)
	if _, err := exec.LookPath("node"); err != nil {
		requireE2E(t, "node is not installed")
	}
	if err := exec.Command("node", "-e", "require.resolve('playwright')").Run(); err != nil {
		requireE2E(t, "playwright is not installed (run pnpm install)")
	}
	b, root := scProject(t, map[string]string{
		"src/components/Counter.tsx": counterClientSrc,
		"src/pages/index.tsx": `import Counter from "../components/Counter";
async function Note() { await Promise.resolve(); return <em>server note</em>; }
export default async function Home() {
  const start = await Promise.resolve(5);
  return <main><Counter start={start} label="Count"><Note /></Counter></main>;
}`,
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
	script, err := filepath.Abs(filepath.Join("testdata", "client_island_click.mjs"))
	if err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("node", script, base+"/", browser).CombinedOutput()
	if err != nil {
		if strings.Contains(string(out), "Executable doesn't exist") {
			requireE2E(t, "%s browser is not installed: %s", browser, out)
		}
		t.Fatalf("browser script failed: %v\n%s", err, out)
	}
	var res struct {
		Before, After, Children string
		Errors                  []string
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &res); err != nil {
		t.Fatalf("parsing browser output: %v\n%s", err, out)
	}
	if res.Before != "Count: 5" || res.After != "Count: 7" || res.Children != "server note" || len(res.Errors) > 0 {
		t.Fatalf("unexpected browser result: %+v", res)
	}
}

func TestClientModuleStarReexportExpandsRuntimeExports(t *testing.T) {
	files := map[string]string{
		"src/components/viewer/types.ts":   "export interface ViewerProps { label: string }\nexport type Mode = 'a' | 'b';\n",
		"src/components/viewer/helpers.ts": "export function fmt(s: string) { return s.toUpperCase(); }\nexport const MAX = 3;\nexport default 1;\n",
		"src/components/Button.tsx":        "export function Button({ text }: { text: string }) { return <i class=\"btn\">{text}</i>; }\n",
		"src/components/viewer/viewer.tsx": `import { useState } from "react";
import { Button } from "../Button";
import type { ViewerProps } from "./types";
export function Viewer({ label }: ViewerProps) {
  const [n, setN] = useState(0);
  return <button id="v" onClick={() => setN(n + 1)}>{label}: {n}<Button text="b" /></button>;
}
`,
		"src/components/viewer/index.tsx": `"use client";
export * from './types';
export * from './helpers';
export { Viewer } from './viewer';
`,
		"src/pages/index.tsx": `import { Viewer } from "../components/viewer";
import { Button } from "../components/Button";
export default async function Home() {
  const label = await Promise.resolve("count");
  return <main><Viewer label={label} /><Button text={label} /></main>;
}`,
	}
	b, root := scProject(t, files)
	withPreact(t, root)
	if err := b.BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	shell, _ := os.ReadFile(filepath.Join(root, "dist", "index.html"))
	if !strings.Contains(string(shell), "island-loader.") {
		t.Errorf("page shell should load the island loader:\n%s", shell)
	}
	if err := b.writeIslandBundles(); err != nil {
		t.Fatal(err)
	}
	// viewer.tsx has no directive: a "use client" module imports it, so it is
	// client code and hydrates in its own island chunk.
	chunks, _ := filepath.Glob(filepath.Join(root, "dist", "chunks", "island_*.js"))
	hydrates := false
	for _, c := range chunks {
		if src, _ := os.ReadFile(c); strings.Contains(string(src), "Viewer") && strings.Contains(string(src), "mount") {
			hydrates = true
		}
	}
	// The page's direct <Button> stays a server component: only the barrel and
	// viewer.tsx are boundaries, Button has no chunk of its own.
	if len(chunks) != 2 {
		t.Errorf("expected two island chunks (barrel and Viewer), got %v", chunks)
	}
	if !hydrates {
		t.Fatalf("no island chunk contains the Viewer component: %v", chunks)
	}
	bundle, _ := os.ReadFile(filepath.Join(root, "dist", ".krate", "server-bundles", "index.server.mjs"))
	for _, want := range []string{`"Viewer"`, `"fmt"`, `"MAX"`} {
		if !strings.Contains(string(bundle), want) {
			t.Errorf("page bundle missing expanded export %s:\n%s", want, bundle)
		}
	}

	// A cycle of star re-exports cannot be expanded: the hint names the source
	// once, with a single pair of quotes.
	err := buildErr(t, map[string]string{
		"src/components/a.tsx": "\"use client\";\nexport * from './b';\nexport function A() { return <i>a</i>; }\n",
		"src/components/b.tsx": "export * from './a';\nexport const B = 1;\n",
		"src/pages/index.tsx": `import { A } from "../components/a";
export default async function Home() { await 1; return <main><A /></main>; }`,
	})
	wantCode(t, err, diag.CodeClientBoundaryExports, `export { A, B } from "./a"`)
}

// TestServerOnlyCodeStaysOutOfClientBundles builds a page whose server tree
// uses Node-only code (a native package, node:crypto) and whose client
// component comes through a barrel that also re-exports a server helper and a
// lazy server component. Like Next.js, the browser bundles hold only what the
// client component imports; server-only modules neither enter them nor
// warn about process.env.
func TestServerOnlyCodeStaysOutOfClientBundles(t *testing.T) {
	b, root := scProject(t, map[string]string{
		"node_modules/nativepkg/package.json": `{"name":"nativepkg","version":"1.0.0","main":"index.js"}`,
		"node_modules/nativepkg/index.js": `const fs = require("node:fs");
let addon = "";
try { addon = require("./addon.node"); } catch (e) { addon = "noaddon"; }
module.exports = { draw: function () { return "drawn-" + typeof fs.readFileSync + addon; } };`,
		"node_modules/nativepkg/addon.node": "not a real binary",
		"src/server/lib.ts": `import { createHash } from "node:crypto";
import { draw } from "nativepkg";
export const SECRET = process.env.SECRET;
export function hashIt(s: string) { return createHash("sha1").update(s).digest("hex").slice(0, 8) + draw(); }`,
		"src/components/Counter.tsx": `"use client";
import { useState } from "react";
import { Label } from "./ui";
export function Counter({ start }: any) {
  const [n, setN] = useState(start);
  return <button id="inc" onClick={() => setN(n + 1)}><Label>Count: </Label>{n}{process.env.NEXT_PUBLIC_X}</button>;
}`,
		"src/components/ui/Label.tsx": `export function Label({ children }: any) { return <b>{children}</b>; }`,
		"src/components/ui/util.ts": `import { lazy } from "react";
import { hashIt } from "../../server/lib";
export const Draw = lazy(() => import("../../draw-sources"));
export function serverUtil() { return hashIt("a"); }`,
		"src/components/ui/index.ts": `export { Label } from "./Label";
export { serverUtil, Draw } from "./util";`,
		"src/components/index.ts": `export { Counter } from "./Counter";
export { serverUtil, Draw } from "./ui";`,
		"src/draw-sources.tsx": `import { draw } from "nativepkg";
export default function DrawSources() { return <p id="ds">{draw()}</p>; }`,
		"src/pages/index.tsx": `import { Counter, Draw, serverUtil } from "../components";
import { SECRET } from "../server/lib";
export default async function Home() {
  await Promise.resolve();
  return <main><i id="h">{serverUtil()}{String(SECRET)}</i><Counter start={5} /><Draw /></main>;
}`,
	})
	withPreact(t, root)
	if err := linkRuntimePackage(t, filepath.Join(root, "node_modules", "@krate")); err != nil {
		t.Fatal(err)
	}
	out, err := captureOutput(b.BuildAll)
	if err != nil {
		t.Fatalf("BuildAll: %v\n%s", err, out)
	}
	if !strings.Contains(out, "components/Counter.tsx") || !strings.Contains(out, "process.env.NEXT_PUBLIC_X") {
		t.Errorf("the client component's environment read should warn (KR8310):\n%s", out)
	}
	if strings.Contains(out, "server/lib.ts") {
		t.Errorf("a server-only module's environment read must not warn:\n%s", out)
	}
	var js []string
	_ = filepath.WalkDir(filepath.Join(root, "dist"), func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(p, ".js") && !strings.Contains(filepath.ToSlash(p), "/.krate/") {
			js = append(js, p)
		}
		return nil
	})
	var counterChunks int
	for _, p := range js {
		data, _ := os.ReadFile(p)
		for _, bad := range []string{"nativepkg", "node:", "addon.node", "createHash", "readFileSync", "hashIt", "drawn-"} {
			if strings.Contains(string(data), bad) {
				t.Errorf("%s holds server-only code (%q)", filepath.Base(p), bad)
			}
		}
		if strings.Contains(string(data), "Count: ") {
			counterChunks++
		}
	}
	if counterChunks != 1 {
		t.Errorf("expected the client component in one browser chunk, found %d among %v", counterChunks, js)
	}
	if _, err := exec.LookPath("node"); err != nil {
		requireE2E(t, "node is not installed")
		return
	}
	ensureRuntimeDist(t)
	base, err := startKrateServer(t, root, b.Cfg)
	if err != nil {
		t.Fatalf("serve: %v", err)
	}
	resp, err := http.Get(base + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	for _, want := range []string{`id="ds"`, "drawn-function", "<krate-island", "<b>Count: </b>5"} {
		if !strings.Contains(string(body), want) {
			t.Errorf("rendered page misses %q:\n%s", want, body)
		}
	}
}
