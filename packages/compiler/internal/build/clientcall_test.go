package build

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/diag"
)

const clientShellSrc = `"use client";
import { useState } from "react";
export function parseUi(raw: string | undefined) {
  const [mode, collapsed] = (raw ?? "").split("|");
  return { mode: mode || "light", collapsed: collapsed === "1" };
}
export const LIMIT = 3;
export function Shell({ mode }: { mode: string }) {
  const [n, setN] = useState(0);
  return <button id="shell" onClick={() => setN(n + 1)}>{mode} {n}</button>;
}
`

// TestServerCallOfClientExportIsABuildError: a server component that calls a
// function exported by a "use client" module gets a diagnostic at the call,
// instead of the island wrapper running the region renderer on its argument.
func TestServerCallOfClientExportIsABuildError(t *testing.T) {
	t.Parallel()
	b, root := scProject(t, map[string]string{
		"src/components/shell.tsx": clientShellSrc,
		"src/pages/admin/_layout.tsx": `import { parseUi, Shell } from "../../components/shell";
export default async function Layout({ children }: { children: any }) {
  await Promise.resolve();
  const ui = parseUi("dark|1");
  return <div><Shell mode={ui.mode} />{children}</div>;
}`,
		"src/pages/admin/index.tsx": `export default async function Admin() { await Promise.resolve(); return <main>admin</main>; }`,
	})
	withPreact(t, root)
	err := b.BuildAll()
	wantCode(t, err, diag.CodeClientExportCalled, "`parseUi` is exported from a client module (shell.tsx)")
	for _, want := range []string{"_layout.tsx:4:", `move parseUi into a module without "use client"`} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("diagnostic lacks %q:\n%v", want, err)
		}
	}
}

// TestClientExportsStayUsableOnTheServer: what the diagnostic does not cover
// keeps working. Client components render, a client export that is not a
// function (a constant) reads normally, a local of the same name shadows the
// import, and a client function passed along without a call is no error.
func TestClientExportsStayUsableOnTheServer(t *testing.T) {
	t.Parallel()
	ensureRuntimeDist(t)
	b, root := scProject(t, map[string]string{
		"src/components/shell.tsx": clientShellSrc,
		"src/pages/index.tsx": `import { Shell, LIMIT, parseUi } from "../components/shell";
export default async function Home() {
  await Promise.resolve();
  const parse = (x: string) => x.toUpperCase();
  const label = (() => { const parseUi = (x: string) => "local-" + x; return parseUi("a"); })();
  return <main><p id="limit">limit {LIMIT + 1}</p><p id="label">{label}{parse("b")}</p><Shell mode="m" /><span data-f={typeof parseUi} /></main>;
}`,
	})
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
	status, html := get(t, base+"/")
	if status != 200 || !strings.Contains(html, "limit 4") || !strings.Contains(html, "local-aB") || !strings.Contains(html, `id="shell"`) {
		t.Fatalf("page: %d\n%s", status, html)
	}
}

// TestRuntimeCallOfClientExportFailsOneRequest: a call the build cannot see
// (through a namespace import) fails when the page renders, naming the
// export, and answers that one request with a 500 while every other page keeps
// answering.
func TestRuntimeCallOfClientExportFailsOneRequest(t *testing.T) {
	t.Parallel()
	base := serveFaultyApp(t, map[string]string{
		"src/components/shell.tsx": clientShellSrc,
		"src/pages/admin/_layout.tsx": `import * as shell from "../../components/shell";
export default async function Layout({ children }: { children: any }) {
  await Promise.resolve();
  const ui = shell.parseUi("dark|1");
  return <div>{ui.mode}{children}</div>;
}`,
		"src/pages/admin/index.tsx": `export default async function Admin() { await Promise.resolve(); return <main>admin</main>; }`,
	})
	healthyWhileFailing(t, base, "/admin/", 500, "")
}
