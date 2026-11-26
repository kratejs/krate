package build

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

// Data modules that hold JSX next to plain fields: the plain fields feed the
// document head and the page body at build time, the JSX renders where it is
// read, and an island gets its initial rows.
func TestImportedDataWithJSXFeedsHeadAndBody(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	files := map[string]string{
		"src/data/entries.tsx": `export const entries = [
  { id: "a", name: "Alpha", icon: <svg viewBox="0 0 1 1" /> },
  { id: "b", name: "Beta", icon: <svg viewBox="0 0 2 2" /> },
];`,
		"src/pages/[id].tsx": `import { useState } from 'react';
import { entries } from '../data/entries';
function Picker(props: { rows: { id: string; name: string }[] }) {
  const [n, setN] = useState(0);
  return <div><button onClick={() => setN(n + 1)}>{n}</button><ul>{props.rows.map((r) => <li key={r.id}>{r.name}</li>)}</ul></div>;
}
export function generateStaticParams() { return [{ id: 'a' }, { id: 'b' }]; }
export default function Page({ params }: { params: { id: string } }) {
  const entry = entries.find((e) => e.id === params.id)!;
  return <main><title>{entries.find((e) => e.id === params.id)!.name}</title><h1>{entry.name}</h1>{entry.icon}<Picker rows={entries} /></main>;
}`,
	}
	for rel, content := range files {
		writeFileRel(t, root, rel, content)
	}
	cfg := config.Default()
	cfg.PagesDir = filepath.Join(root, "src", "pages")
	cfg.OutDir = filepath.Join(root, "dist")
	cfg.Minify = false
	out, err := captureBuild(New(root, cfg))
	if err != nil {
		t.Fatalf("BuildAll: %v\n%s", err, out)
	}
	for _, tc := range []struct{ id, name, box string }{{"a", "Alpha", "0 0 1 1"}, {"b", "Beta", "0 0 2 2"}} {
		html := readOut(t, cfg.OutDir, filepath.Join(tc.id, "index.html"))
		for _, want := range []string{
			"<title>" + tc.name + "</title>",
			"<h1>" + tc.name + "</h1>",
			`<svg viewBox="` + tc.box + `"></svg>`,
			"<li>Alpha</li>", "<li>Beta</li>",
		} {
			if !strings.Contains(html, want) {
				t.Errorf("%s: missing %s in:\n%s", tc.id, want, html)
			}
		}
	}
}
