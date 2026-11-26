package build

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/irtree"
)

func TestIslandImportsForReferencedValues(t *testing.T) {
	imports := []irtree.ExternalImport{
		{Source: "/p/Widget.tsx", Local: "Widget", Default: true},
		{Source: "/p/rows.ts", Local: "loadRows"},
		{Source: "/p/rows.ts", Local: "LIMIT"},
		{Source: "/p/rows.ts", Local: "ident"},
		{Source: "/p/rows.ts", Local: "inString"},
		{Source: "/p/rows.ts", Local: "unused"},
		{Source: "/p/ns.ts", Local: "cfg", Namespace: true},
	}
	subtree := `<Widget rows={loadRows()} limit={LIMIT * 2} label="inString" obj={{ k: ident }} n={cfg.unused}>it's {"x"}</Widget>`
	var got []string
	for _, imp := range islandImportsFor(subtree, imports) {
		got = append(got, imp.Local)
	}
	want := "Widget,loadRows,LIMIT,ident,cfg"
	if strings.Join(got, ",") != want {
		t.Fatalf("imports = %v, want %s", got, want)
	}
	// A subtree that does not parse still keeps tag imports.
	got = nil
	for _, imp := range islandImportsFor(`<Widget a={`, imports) {
		got = append(got, imp.Local)
	}
	if strings.Join(got, ",") != "Widget" {
		t.Fatalf("fallback imports = %v", got)
	}
}

// TestIslandPropsUseImportedValues: a compat-path island whose props call an
// imported function and read an imported constant gets those imports.
func TestIslandPropsUseImportedValues(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("node"); err != nil {
		requireE2E(t, "node is not installed")
	}
	repo, err := repoRootPath()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(repo, "packages", "runtime", "dist", "krate-runtime.js")); err != nil {
		requireE2E(t, "the runtime package is not built: %v", err)
	}
	root := t.TempDir()
	cfg := setupIslandProject(t, root, true)
	e2eEnv(t)
	if err := os.MkdirAll(filepath.Join(root, "node_modules", "@krate"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := linkRuntimePackage(t, filepath.Join(root, "node_modules", "@krate")); err != nil {
		t.Skipf("cannot link the runtime package: %v", err)
	}
	writeProjectFile(t, root, "node_modules/countlib/package.json", `{"name":"countlib","version":"1.0.0","main":"index.js","peerDependencies":{"react":"^19.0.0"}}`)
	writeProjectFile(t, root, "node_modules/countlib/index.js", `import { useState } from 'react';
export function useOnce(fn) {
	const [v] = useState(fn);
	return v;
}
`)
	writeProjectFile(t, root, "src/lib/rows.ts", `export type Rows = { base: number };
export const MARK = "mark-7";
export function makeRows(): Rows { return { base: 41 }; }
`)
	writeProjectFile(t, root, "src/components/Widget.tsx", `import { useOnce } from 'countlib';
import type { Rows } from '../lib/rows';
export default function Widget(props: { rows: Rows; mark: string }) {
	const v = useOnce(() => props.rows.base + 1);
	return <p id="w">{v}{props.mark}</p>;
}
`)
	writeProjectFile(t, root, "src/pages/index.tsx", `import Widget from '../components/Widget';
import { makeRows, MARK } from '../lib/rows';
import type { Rows } from '../lib/rows';
export default function Page() {
	return <main><h1>home</h1><Widget rows={makeRows()} mark={MARK} /></main>;
}
`)
	out, err := captureBuild(New(root, cfg))
	if err != nil {
		t.Fatalf("BuildAll: %v\n%s", err, out)
	}
	chunks, _ := filepath.Glob(filepath.Join(cfg.OutDir, "chunks", "*.js"))
	found := false
	for _, c := range chunks {
		text := readBuilt(t, c)
		if !strings.Contains(text, "makeRows") && !strings.Contains(text, "mark-7") {
			continue
		}
		found = true
		if !strings.Contains(text, "mark-7") || !strings.Contains(text, "base:41") && !strings.Contains(text, "base: 41") {
			t.Errorf("%s calls makeRows or reads MARK without bundling them:\n%s", filepath.Base(c), text)
		}
	}
	if !found {
		t.Fatalf("no island chunk references the imported values (%d chunks)", len(chunks))
	}
}
