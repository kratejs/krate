package build

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

// buildProject writes files (paths relative to a temp root) and builds the
// project, returning the page HTML, hydration JS, and any build error.
func buildKrateProject(t *testing.T, files map[string]string) (string, string, error) {
	t.Helper()
	root := t.TempDir()
	for rel, content := range files {
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	cfg := config.Default()
	cfg.PagesDir = filepath.Join(root, "src", "pages")
	cfg.OutDir = filepath.Join(root, "dist")
	cfg.Minify = false
	if err := New(root, cfg).BuildAll(); err != nil {
		return "", "", err
	}
	htmlBytes, err := os.ReadFile(filepath.Join(cfg.OutDir, "index.html"))
	if err != nil {
		return "", "", err
	}
	jsBytes := []byte{}
	if jsFiles, _ := filepath.Glob(filepath.Join(cfg.OutDir, "index.*.js")); len(jsFiles) > 0 {
		jsBytes, _ = os.ReadFile(jsFiles[0])
	}
	return string(htmlBytes), string(jsBytes), nil
}

// TestKrateModernOperators verifies exponentiation, shifts, and bitwise
// operators parse and survive codegen in Krate-native handlers.
func TestKrateModernOperators(t *testing.T) {
	page := `
		import { createSignal } from '@krate/runtime';
		export default function Page() {
			const [n, setN] = createSignal(3);
			return (
				<div>
					<button onClick={() => setN(n() ** 2)}>pow</button>
					<button onClick={() => setN(n() << 1)}>left</button>
					<button onClick={() => setN(n() >> 1)}>right</button>
					<button onClick={() => setN(n() >>> 1)}>unsigned</button>
					<button onClick={() => setN(n() & 1)}>and</button>
					<button onClick={() => setN(n() | 4)}>or</button>
					<button onClick={() => setN(n() ^ 2)}>xor</button>
					<p>{n()}</p>
				</div>
			);
		}
	`
	html, js, err := buildKrateProject(t, map[string]string{"src/pages/index.tsx": page})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if !strings.Contains(html, ">3<") {
		t.Errorf("expected the signal initial in SSR, got:\n%s", html)
	}
	for _, op := range []string{"**", "<<", ">>", ">>>", "&", "|", "^"} {
		if !strings.Contains(js, op) {
			t.Errorf("handler JS is missing operator %q:\n%s", op, js)
		}
	}
}

// TestKrateLogicalAssignments verifies ??=, &&=, ||=, and **= parse and are
// emitted from a handler body.
func TestKrateLogicalAssignments(t *testing.T) {
	page := `
		export default function Page() {
			let a = null, b = 2, c = null, d = 1;
			const mutate = () => {
				a ??= 1;
				b **= 2;
				c ||= 3;
				d &&= 4;
			};
			return <button onClick={mutate}>go</button>;
		}
	`
	_, js, err := buildKrateProject(t, map[string]string{"src/pages/index.tsx": page})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	for _, op := range []string{"??=", "**=", "||=", "&&="} {
		if !strings.Contains(js, op) {
			t.Errorf("handler JS is missing %q:\n%s", op, js)
		}
	}
}

// TestKrateSpreadCallAndExportAlias verifies spread call arguments and an
// `export { a as b }` alias resolve in a Krate project.
func TestKrateSpreadCallAndExportAlias(t *testing.T) {
	files := map[string]string{
		"src/lib/values.ts": `
			const internal = 'HELLO';
			export { internal as upper };
		`,
		"src/pages/index.tsx": `
			import { upper as shout } from '../lib/values';
			export default function Page() {
				const parts = ['a', 'b'];
				const log = () => console.log(...parts);
				return <button onClick={log}>{shout}</button>;
			}
		`,
	}
	html, js, err := buildKrateProject(t, files)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if !strings.Contains(html, "HELLO") {
		t.Errorf("export alias did not resolve to its value:\n%s", html)
	}
	if !strings.Contains(js, "...parts") {
		t.Errorf("spread call argument was not preserved:\n%s", js)
	}
}

// TestKrateThisParameter verifies a TypeScript `this` parameter is accepted and
// erased from the emitted handler.
func TestKrateThisParameter(t *testing.T) {
	page := `
		function label(this: any, value: string) {
			return value;
		}
		export default function Page() {
			return <button onClick={() => console.log(label('ok'))}>x</button>;
		}
	`
	_, js, err := buildKrateProject(t, map[string]string{"src/pages/index.tsx": page})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if strings.Contains(js, "this:") {
		t.Errorf("this parameter leaked into JS:\n%s", js)
	}
	if !strings.Contains(js, "label(") {
		t.Errorf("expected the helper to be emitted:\n%s", js)
	}
}

// TestKrateDefaultImportComponent verifies a default-imported local component
// resolves and renders in a Krate page.
func TestKrateDefaultImportComponent(t *testing.T) {
	files := map[string]string{
		"src/components/ui/button.tsx": `
			export default function Button() {
				return <button>Test</button>;
			}
		`,
		"src/pages/index.tsx": `
			import Button from '../components/ui/button';
			export default function Page() {
				return <div><Button /></div>;
			}
		`,
	}
	html, _, err := buildKrateProject(t, files)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if !strings.Contains(html, "<button>Test</button>") {
		t.Fatalf("default-imported component did not render:\n%s", html)
	}
}
