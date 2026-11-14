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
