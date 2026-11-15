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

const flightPropsClientSrc = `"use client";
import { useEffect, useState } from "react";
function describe(p: any) {
  return JSON.stringify([
    p.row.createdAt instanceof Date, p.row.createdAt.toISOString(), String(p.row.bad.getTime()),
    typeof p.row.big, String(p.row.big), p.tags.get("a").has(2), p.tags instanceof Map, p.tags.get("a") instanceof Set,
    "gone" in p.row && p.row.gone === undefined, Object.is(p.row.nan, NaN), Object.is(p.row.neg, -0), p.row.inf,
    p.row.fake.__krateT, p.row.list[1] === undefined, p.row.bytes instanceof Uint8Array && p.row.bytes[2], p.sym === Symbol.for("s"),
  ]);
}
export default function Row(p: any) {
  const [seen, setSeen] = useState("");
  useEffect(() => setSeen(describe(p)), []);
  return <div><p id="ssr">{describe(p)}</p><p id="client" data-ready={seen ? "1" : undefined}>{seen}</p></div>;
}
`

// TestClientPropsCarryFlightTypes sends a Drizzle-like row (Date, BigInt, Map,
// Set, undefined, NaN, ...) from an async server page to a client component
// and checks the server render and, with KRATE_BROWSER set, the hydrated client.
func TestClientPropsCarryFlightTypes(t *testing.T) {
	if !canStopInProcessServer {
		t.Skip("in-process server needs a unix host")
	}
	ensureRuntimeDist(t)
	if _, err := exec.LookPath("node"); err != nil {
		requireE2E(t, "node is not installed")
	}
	b, root := scProject(t, map[string]string{
		"src/components/Row.tsx": flightPropsClientSrc,
		"src/pages/index.tsx": `import Row from "../components/Row";
export default async function Home() {
  const row = await Promise.resolve({
    createdAt: new Date("2024-01-02T03:04:05Z"), bad: new Date("nope"), big: 12345678901234567890n,
    gone: undefined, nan: NaN, neg: -0, inf: Infinity, list: [1, undefined, 3],
    fake: { __krateT: "D", v: 5 }, bytes: new Uint8Array([7, 8, 9]),
  });
  const tags = new Map([["a", new Set([1, 2])]]);
  return <main><Row row={row} tags={tags} sym={Symbol.for("s")} /></main>;
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
	const want = `[true,"2024-01-02T03:04:05.000Z","NaN","bigint","12345678901234567890",true,true,true,true,true,true,null,"D",true,9,true]`
	resp, err := http.Get(base + "/")
	if err != nil {
		t.Fatal(err)
	}
	html, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status %d:\n%s", resp.StatusCode, html)
	}
	// JSON.stringify turns Infinity into null, which is what the want shows.
	if !strings.Contains(string(html), `<p id="ssr">`+strings.ReplaceAll(want, `"`, "&quot;")+`</p>`) &&
		!strings.Contains(string(html), `<p id="ssr">`+want+`</p>`) {
		t.Fatalf("server render lacks the revived values %s:\n%s", want, html)
	}
	browser := strings.ToLower(strings.TrimSpace(os.Getenv("KRATE_BROWSER")))
	if browser == "" {
		return
	}
	if err := exec.Command("node", "-e", "require.resolve('playwright')").Run(); err != nil {
		requireE2E(t, "playwright is not installed (run pnpm install)")
	}
	script, err := filepath.Abs(filepath.Join("testdata", "client_flight_props.mjs"))
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
		Client, SsrLike string
		Errors          []string
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &res); err != nil {
		t.Fatalf("parsing browser output: %v\n%s", err, out)
	}
	if res.Client != want || res.SsrLike != want || len(res.Errors) > 0 {
		t.Fatalf("hydrated client saw %+v, want %s", res, want)
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

// TestUseClientPageUnderAsyncLayoutIsABoundary builds a "use client" page
// under an async layout: the server tree renders it as a client boundary (its
// initial state is in the HTML, its chunk exists) instead of evaluating its
// hooks, and with KRATE_BROWSER it hydrates.
func TestUseClientPageUnderAsyncLayoutIsABoundary(t *testing.T) {
	b, root := scProject(t, map[string]string{
		"src/pages/(app)/_layout.tsx": `export default async function Layout({ children }: { children: any }) {
  await Promise.resolve();
  return <div id="shell">{children}</div>;
}`,
		"src/pages/(app)/settings.tsx": `"use client";
import { useState, useEffect, useRef } from "react";
export default function Settings() {
  const [n, setN] = useState(5);
  const ref = useRef(null);
  useEffect(() => { document.title = "settings"; }, []);
  return <main ref={ref}><button id="inc" onClick={() => setN(n + 1)}>Count: {n}</button></main>;
}`,
	})
	withPreact(t, root)
	e2eEnv(t)
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
	status, _, html := getBody(t, base+"/settings/")
	if status != 200 || !strings.Contains(html, `<div id="shell">`) || !strings.Contains(html, "<krate-island") || !regexp.MustCompile(`Count: (<!--[^>]*-->)*5`).MatchString(html) {
		t.Fatalf("status %d, expected the layout around a server-rendered island with its initial state:\n%s", status, html)
	}
	chunks, _ := filepath.Glob(filepath.Join(b.Cfg.OutDir, "chunks", "*"))
	if len(chunks) == 0 {
		t.Fatalf("expected an island chunk under %s", b.Cfg.OutDir)
	}
	browser := strings.ToLower(strings.TrimSpace(os.Getenv("KRATE_BROWSER")))
	if browser == "" {
		return
	}
	script, err := filepath.Abs(filepath.Join("testdata", "client_page_click.mjs"))
	if err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("node", script, base+"/settings/", browser).CombinedOutput()
	if err != nil {
		if strings.Contains(string(out), "Executable doesn't exist") {
			requireE2E(t, "%s browser is not installed: %s", browser, out)
		}
		t.Fatalf("browser script failed: %v\n%s", err, out)
	}
	var res struct {
		Before, After string
		Errors        []string
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &res); err != nil {
		t.Fatalf("parsing browser output: %v\n%s", err, out)
	}
	if res.Before != "Count: 5" || res.After != "Count: 6" || len(res.Errors) > 0 {
		t.Fatalf("unexpected browser result: %+v", res)
	}
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
	// Diagnostics print native paths; compare with forward slashes.
	out = strings.ReplaceAll(out, `\`, "/")
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

// TestUseServerBodyStaysOutOfClientChunks: a "use client" Preact component
// imports a side-effect-free "use server" module directly, through a barrel,
// from a static page and from an async page. The browser gets the action
// reference only; the action's body, its secret and node:crypto stay out of
// every emitted file.
func TestUseServerBodyStaysOutOfClientChunks(t *testing.T) {
	client := func(imp string) string {
		return `"use client";
import { useState } from "react";
import { saveDraft } from "` + imp + `";
export function Save() {
  const [id, setId] = useState("");
  return <button id="save" onClick={async () => setId(await saveDraft("hello"))}>save {id}</button>;
}
`
	}
	b, root := scProject(t, map[string]string{
		"src/server/actions.ts": `'use server';
import { createHash } from "node:crypto";
export async function saveDraft(text: string) {
  return createHash("sha256").update(text + "TOPSECRET-4711").digest("hex").slice(0, 8);
}
`,
		"src/components/index.ts":    `export { saveDraft } from "../server/actions";` + "\n",
		"src/components/Direct.tsx":  client("../server/actions"),
		"src/components/Barrel.tsx":  client("./index"),
		"src/pages/direct.tsx":       "import { Save } from \"../components/Direct\";\nexport default function P() { return <main><Save /></main>; }\n",
		"src/pages/barrel.tsx":       "import { Save } from \"../components/Barrel\";\nexport default function P() { return <main><Save /></main>; }\n",
		"src/pages/direct-async.tsx": "import { Save } from \"../components/Direct\";\nexport default async function P() { await Promise.resolve(); return <main><Save /></main>; }\n",
		"src/pages/barrel-async.tsx": "import { Save } from \"../components/Barrel\";\nexport default async function P() { await Promise.resolve(); return <main><Save /></main>; }\n",
	})
	withPreact(t, root)
	if err := linkRuntimePackage(t, filepath.Join(root, "node_modules", "@krate")); err != nil {
		t.Fatal(err)
	}
	out, err := captureOutput(b.BuildAll)
	if err != nil {
		t.Fatalf("BuildAll: %v\n%s", err, out)
	}
	var withAction int
	_ = filepath.WalkDir(filepath.Join(root, "dist"), func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !(strings.HasSuffix(p, ".js") || strings.HasSuffix(p, ".html")) || strings.Contains(filepath.ToSlash(p), "/.krate/") {
			return nil
		}
		data, _ := os.ReadFile(p)
		for _, bad := range []string{"TOPSECRET", "createHash", "node:"} {
			if strings.Contains(string(data), bad) {
				t.Errorf("%s holds server-only code (%q)", p, bad)
			}
		}
		if strings.Contains(string(data), "/__krate/action/") || strings.Contains(string(data), "__krateAction") {
			withAction++
		}
		return nil
	})
	if withAction < 4 {
		t.Errorf("every page must reference the action, only %d files do", withAction)
	}
}

// TestJSXPropsReachClientComponent: a server component passes JSX in props
// other than children (a slot, an array of elements, a client component in a
// slot) to a client component. The slots render on the server, are in the SSR
// HTML, and survive hydration in a browser (KRATE_BROWSER).
func TestJSXPropsReachClientComponent(t *testing.T) {
	e2eEnv(t)
	b, root := scProject(t, map[string]string{
		"src/components/Shell.tsx": `"use client";
import { useState } from "react";
export default function Shell({ userMenu, tools, children }: any) {
  const [open, setOpen] = useState(false);
  return <div id="shell"><button id="toggle" onClick={() => setOpen(!open)}>{open ? "open" : "closed"}</button><div id="menu">{userMenu}</div><ul id="tools">{tools}</ul><div id="body">{children}</div></div>;
}`,
		"src/components/Like.tsx": `"use client";
import { useState } from "react";
export default function Like() {
  const [n, setN] = useState(0);
  return <li><button id="like" onClick={() => setN(n + 1)}>likes {n}</button></li>;
}`,
		"src/pages/index.tsx": `import Shell from "../components/Shell";
import Like from "../components/Like";
async function UserMenu({ name }: { name: string }) { await Promise.resolve(); return <span id="who">{name}</span>; }
export default async function Home() {
  const name = await Promise.resolve("Ada");
  return <Shell userMenu={<UserMenu name={name} />} tools={[<li key="a">tool a</li>, <Like key="b" />]}><p>page body</p></Shell>;
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
	resp, err := http.Get(base + "/")
	if err != nil {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	html := string(data)
	if resp.StatusCode != 200 || !strings.Contains(html, `<span id="who">Ada</span>`) || !strings.Contains(html, "tool a") || !strings.Contains(html, "likes 0") || !strings.Contains(html, "page body") {
		t.Fatalf("status %d, the slots must be in the server HTML:\n%s", resp.StatusCode, html)
	}
	browser := strings.ToLower(strings.TrimSpace(os.Getenv("KRATE_BROWSER")))
	if browser == "" {
		return
	}
	if !canStopInProcessServer {
		t.Skip("in-process server needs a unix host")
	}
	if err := exec.Command("node", "-e", "require.resolve('playwright')").Run(); err != nil {
		requireE2E(t, "playwright is not installed (run pnpm install)")
	}
	script, err := filepath.Abs(filepath.Join("testdata", "client_island_slots.mjs"))
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
		Who, Tools, Body string
		Errors           []string
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &res); err != nil {
		t.Fatalf("parsing browser output: %v\n%s", err, out)
	}
	if res.Who != "Ada" || !strings.Contains(res.Tools, "tool a") || res.Body != "page body" || len(res.Errors) > 0 {
		t.Fatalf("unexpected browser result: %+v", res)
	}
}

// TestReactDOMResourceHints: react-dom's preload, preconnect, preinit and
// preloadModule work in a server component and in a client component that the
// server renders, and put their tags (once each) in the head of the response.
func TestReactDOMResourceHints(t *testing.T) {
	e2eEnv(t)
	b, root := scProject(t, map[string]string{
		"src/server/hints.ts": `import "server-only";
import { preload, preconnect, preinit, preloadModule, prefetchDNS } from "react-dom";
export function hintModel(url: string) {
  preload(url, { as: "fetch", crossOrigin: "anonymous", fetchPriority: "high" });
  preload(url, { as: "fetch", crossOrigin: "anonymous", fetchPriority: "high" });
  preconnect("https://cdn.example.com", { crossOrigin: "anonymous" });
  prefetchDNS("https://dns.example.com");
  preinit("/app.css", { as: "style" });
  preloadModule("/mod.js");
}`,
		"src/components/wasm-hint.ts": `import { preload } from "react-dom";
export function hintWasm() { preload("/m.wasm", { as: "fetch", crossOrigin: "anonymous", type: "application/wasm" }); }`,
		"src/components/Grid.tsx": `"use client";
import { useState } from "react";
import { hintWasm } from "./wasm-hint";
export default function Grid() {
  hintWasm();
  const [n, setN] = useState(0);
  return <button id="g" onClick={() => setN(n + 1)}>grid {n}</button>;
}`,
		"src/pages/index.tsx": `import Grid from "../components/Grid";
import { hintModel } from "../server/hints";
export default async function Home() {
  await Promise.resolve();
  hintModel("/model.glb");
  return <main><Grid /></main>;
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
	status, _, html := getWith(t, base+"/", nil)
	if status != 200 {
		t.Fatalf("status %d:\n%.800s", status, html)
	}
	head := html[:strings.Index(html, "</head>")]
	for _, want := range []string{
		`<link rel="preload" href="/model.glb" as="fetch" crossorigin="" fetchpriority="high">`,
		`<link rel="preconnect" href="https://cdn.example.com" crossorigin="">`,
		`<link rel="dns-prefetch" href="https://dns.example.com">`,
		`<link rel="stylesheet" href="/app.css">`,
		`<link rel="modulepreload" href="/mod.js">`,
		`<link rel="preload" href="/m.wasm" as="fetch" crossorigin="" type="application/wasm">`,
	} {
		if strings.Count(head, want) != 1 {
			t.Errorf("head must hold %s once:\n%s", want, head)
		}
	}
}

// TestUseClientReact19HooksBrowser runs a "use client" component that calls
// React 19 hooks preact/compat lacks, through a namespace import and named
// imports, against a real server action: SSR renders the initial state, a
// click applies an optimistic update that settles after the action, and a
// form bound to useActionState reports its pending status through
// useFormStatus. Requires KRATE_BROWSER.
func TestUseClientReact19HooksBrowser(t *testing.T) {
	browser := strings.ToLower(strings.TrimSpace(os.Getenv("KRATE_BROWSER")))
	if browser == "" {
		t.Skip("set KRATE_BROWSER=chromium|firefox|webkit to run the client hooks browser test")
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
		"src/actions.ts": `"use server";
let n = 0;
export async function bump() { await new Promise((r) => setTimeout(r, 400)); return ++n; }`,
		"src/components/Panel.tsx": `"use client";
import * as React from "react";
import { use, useOptimistic } from "react";
import { useFormStatus } from "react-dom";
import { bump } from "../actions";
const Ctx = React.createContext("ctx-default");
function Submit() {
  const { pending } = useFormStatus();
  return <button id="submit" disabled={pending}>{pending ? "saving" : "save"}</button>;
}
export default function Panel({ count }: { count: number }) {
  const [opt, addOpt] = React.useOptimistic(count, (s: number, d: number) => s + d);
  const [named] = useOptimistic(count);
  const [state, formAction] = React.useActionState(async (prev: number) => { await bump(); return prev + 1; }, 0);
  const ctx = React.use(Ctx);
  return (
    <div>
      <button id="go" onClick={() => React.startTransition(async () => { addOpt(1); await bump(); })}>go</button>
      <p id="opt">opt {opt}</p>
      <p id="named">named {named} {typeof use}</p>
      <p id="ctx">{ctx}</p>
      <form action={formAction}><Submit /></form>
      <p id="state">state {state}</p>
    </div>
  );
}`,
		"src/pages/index.tsx": `import Panel from "../components/Panel";
export default async function Home() {
  const count = await Promise.resolve(5);
  return <main><Panel count={count} /></main>;
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
	resp, err := http.Get(base + "/")
	if err != nil {
		t.Fatal(err)
	}
	html, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	for _, want := range []string{"opt 5", "named 5 function", "ctx-default", "state 0", ">save<"} {
		if !strings.Contains(string(html), want) {
			t.Errorf("server HTML missing %q:\n%s", want, html)
		}
	}
	script, err := filepath.Abs(filepath.Join("testdata", "client_react19_hooks.mjs"))
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
		Before, During, Settled, Status, State string
		Errors                                 []string
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &res); err != nil {
		t.Fatalf("parsing browser output: %v\n%s", err, out)
	}
	if res.Before != "opt 5" || res.During != "opt 6" || res.Settled != "opt 5" || res.Status != "saving" || res.State != "state 1" || len(res.Errors) > 0 {
		t.Fatalf("unexpected browser result: %+v", res)
	}
}
