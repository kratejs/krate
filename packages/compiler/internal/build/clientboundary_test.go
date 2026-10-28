package build

import (
	"encoding/json"
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
	if !strings.Contains(string(shell), `krate-island[data-src]`) || !strings.Contains(string(shell), "importmap") {
		t.Errorf("server page shell needs the island loader and the Preact import map:\n%s", shell)
	}
	chunks, _ := filepath.Glob(filepath.Join(root, "dist", "chunks", "client.*.js"))
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

// An ordinary page (no server components) that imports a "use client" module
// compiles exactly as before: Krate hydration, no island marker.
func TestUseClientInOrdinaryPageUnchanged(t *testing.T) {
	b, root := scProject(t, map[string]string{
		"src/components/Counter.tsx": counterClientSrc,
		"src/pages/index.tsx": `import Counter from "../components/Counter";
export default function Home() { return <main><Counter start={1} label="n" /></main>; }`,
	})
	if err := b.BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	shell, _ := os.ReadFile(filepath.Join(root, "dist", "index.html"))
	s := string(shell)
	if strings.Contains(s, "krate-island") || !strings.Contains(s, "id=inc>n: <!--k:") {
		t.Errorf("ordinary page should render the counter through the Krate pipeline:\n%s", s)
	}
	if !strings.Contains(s, ".js></script>") {
		t.Errorf("ordinary page should keep its hydration script:\n%s", s)
	}
}

func TestServerTreeRejectsHooks(t *testing.T) {
	err := buildErr(t, map[string]string{
		"src/Widget.tsx": `import { useState } from "react";
export function Widget() { const [v] = useState(1); return <p>{v}</p>; }`,
		"src/pages/index.tsx": `import { Widget } from "../Widget";
export default async function Home() { await Promise.resolve(); return <main><Widget /></main>; }`,
	})
	wantCode(t, err, diag.CodeClientAPIInServer, "useState() in Widget")
	if !strings.Contains(err.Error(), "Widget.tsx") {
		t.Errorf("diagnostic should point at the component's module: %v", err)
	}
}

func TestServerTreeRejectsEventHandlers(t *testing.T) {
	err := buildErr(t, map[string]string{
		"src/pages/index.tsx": `export default async function Home() {
  await Promise.resolve();
  return <main><button onClick={() => console.log(1)}>go</button></main>;
}`,
	})
	wantCode(t, err, diag.CodeClientAPIInServer, "event handler onClick on <button>")
}

func TestServerTreeChecksOnlyServerSubtrees(t *testing.T) {
	// The sync page keeps its interactive button (normal Krate hydration);
	// only the async child's subtree is a server tree.
	b, _ := scProject(t, map[string]string{
		"src/pages/index.tsx": `import { createSignal } from "@krate/runtime";
async function Stats() { const n = await Promise.resolve(3); return <p>{n}</p>; }
export default function Home() {
  const [c, setC] = createSignal(0);
  return <main><button onClick={() => setC(c() + 1)}>{c()}</button><Stats /></main>;
}`,
	})
	if err := b.BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
}

func TestServerToClientFunctionPropIsAnError(t *testing.T) {
	err := buildErr(t, map[string]string{
		"src/components/Counter.tsx": counterClientSrc,
		"src/pages/index.tsx": `import Counter from "../components/Counter";
export default async function Home() {
  await Promise.resolve();
  return <main><Counter start={1} label="n" onPick={() => 1} /></main>;
}`,
	})
	wantCode(t, err, diag.CodeClientPropNotSerializable, "prop `onPick`")
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

func TestServerOnlyImportAcceptedInServerTree(t *testing.T) {
	b, root := scProject(t, map[string]string{
		"src/db.ts": `import "server-only";
export async function load() { return "row"; }`,
		"src/pages/index.tsx": `import { load } from "../db";
export default async function Home() { const r = await load(); return <main>{r}</main>; }`,
	})
	if err := b.BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	bundle, _ := os.ReadFile(filepath.Join(root, "dist", ".krate", "server-bundles", "index.server.mjs"))
	if strings.Contains(string(bundle), `from "server-only"`) || strings.Contains(string(bundle), `import "server-only"`) {
		t.Errorf("server-only marker should be resolved away, not left as a runtime import:\n%s", bundle)
	}
}

func TestAsyncClientComponentIsAnError(t *testing.T) {
	err := buildErr(t, map[string]string{
		"src/components/Bad.tsx": `"use client";
export default async function Bad() { return <p>x</p>; }`,
		"src/pages/index.tsx": `import Bad from "../components/Bad";
export default async function Home() { await Promise.resolve(); return <main><Bad /></main>; }`,
	})
	wantCode(t, err, diag.CodeAsyncClientComponent, "client component Bad is async")
}

func TestUseClientExportStarIsAnError(t *testing.T) {
	err := buildErr(t, map[string]string{
		"src/components/impl.tsx": `export function A() { return <p>a</p>; }`,
		"src/components/All.tsx": `"use client";
export * from "./impl";`,
		"src/pages/index.tsx": `import { A } from "../components/All";
export default async function Home() { await Promise.resolve(); return <main><A /></main>; }`,
	})
	wantCode(t, err, diag.CodeClientBoundaryExports, "export *")
}

// TestUseClientIslandSidecarE2E renders an async page with a client component
// through the real SSR sidecar: the island carries the Preact server render,
// the serialized props and the server-rendered children.
func TestUseClientIslandSidecarE2E(t *testing.T) {
	ensureRuntimeDist(t)
	node, err := exec.LookPath("node")
	if err != nil {
		requireE2E(t, "node not available: %v", err)
	}
	repoRoot, err := repoRootPath()
	if err != nil {
		t.Fatal(err)
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
	if err := b.BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	if err := linkRuntimePackage(t, filepath.Join(root, "node_modules", "@krate")); err != nil {
		t.Fatal(err)
	}
	base, stop := startSidecar(t, node, repoRoot, root, b.Cfg.OutDir)
	defer stop()
	frames := postSCRegions(t, base, map[string]any{
		"route": "/", "url": "/", "method": "GET", "params": map[string]string{},
		"regions": []map[string]string{{"id": "page", "kind": "page"}},
	})
	if len(frames) != 1 || frames[0].Status != 200 {
		t.Fatalf("frames: %+v", frames)
	}
	html := frames[0].HTML
	for _, want := range []string{
		`<krate-island style="display:contents" data-module="`,
		`data-export="default"`,
		`&quot;start&quot;:5`,
		`<button id="inc">Count: 5</button>`,
		`<krate-children style="display:contents"><em>server note</em></krate-children>`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("island HTML missing %q:\n%s", want, html)
		}
	}
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
