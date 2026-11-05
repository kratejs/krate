package build

import (
	"strings"
	"testing"
)

// TestAliasedHelperImportFoldsStatically covers `import { plain as pp }`: the
// helper folds into the server HTML under its local name.
func TestAliasedHelperImportFoldsStatically(t *testing.T) {
	html, _, err := buildKrateProject(t, map[string]string{
		"src/lib/x.ts": `export function plain() { return "plain-value"; }
export default function fallback() { return "default-value"; }`,
		"src/pages/index.tsx": `
			import dflt, { plain as pp } from "../lib/x";
			export default function Page() {
				return <div><span id="a">{pp()}</span><span id="b">{dflt()}</span></div>;
			}
		`,
	})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	for _, want := range []string{">plain-value</span>", ">default-value</span>"} {
		if !strings.Contains(html, want) {
			t.Errorf("aliased helper did not render %q:\n%s", want, html)
		}
	}
}

// TestAliasedHelperImportHydrates covers an aliased helper that depends on a
// signal: the hydration script must declare the helper under its local name,
// or the browser throws `pp is not defined`.
func TestAliasedHelperImportHydrates(t *testing.T) {
	_, js, err := buildKrateProject(t, map[string]string{
		"src/lib/x.ts": `export function plain(n: number) { return "n=" + n; }`,
		"src/pages/index.tsx": `
			import { createSignal } from '@krate/runtime';
			import { plain as pp } from "../lib/x";
			export default function Page() {
				const [n, setN] = createSignal(1);
				return <button onClick={() => setN(n() + 1)}>{pp(n())}</button>;
			}
		`,
	})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if !strings.Contains(js, "function pp(") {
		t.Errorf("hydration script does not declare the aliased helper pp:\n%s", js)
	}
}
