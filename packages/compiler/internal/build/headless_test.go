package build

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

// TestHydrationRunsHeadless builds a page (inside examples/ so the workspace
// runtime resolves) and executes its runtime + hydration bundles in happy-dom.
// It catches ReferenceErrors in emitted client code that a static build
// assertion cannot see.
func TestHydrationRunsHeadless(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node is not installed")
	}
	repoRoot, err := filepath.Abs(filepath.Join("..", "..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	examplesDir := filepath.Join(repoRoot, "examples")
	if _, err := os.Stat(filepath.Join(examplesDir, "node_modules", "@krate", "runtime")); err != nil {
		t.Skipf("examples workspace runtime is not installed: %v", err)
	}

	root, err := os.MkdirTemp(examplesDir, ".krate-headless-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(root)

	pagesDir := filepath.Join(root, "src", "pages")
	if err := os.MkdirAll(pagesDir, 0755); err != nil {
		t.Fatal(err)
	}
	page := `
		import { createSignal, createMemo } from '@krate/runtime';
		import { forwardRef, useRef, useImperativeHandle } from 'react';
		const FRUITS = ['Apple', 'Apricot', 'Banana'];
		const FancyInput = forwardRef((props: any, ref: any) => {
			const inputRef = useRef(null);
			useImperativeHandle(ref, () => ({
				focus() { if (inputRef.current) inputRef.current.focus(); },
			}));
			return <input ref={inputRef} placeholder="x" />;
		});
		export default function Page() {
			const [q, setQ] = createSignal('a');
			const [n, setN] = createSignal(0);
			const api = useRef(null);
			const filtered = createMemo(() => FRUITS.filter((f) => f.toLowerCase().includes(q().toLowerCase())));
			return (
				<div>
					<button onClick={() => setQ('A')}>set</button>
					<button id="inc" onClick={() => setN(n() + 1)}>inc</button>
					<button id="noop">noop</button>
					<span id="count">{n()}</span>
					<button onClick={() => api.current && api.current.focus()}>focus</button>
					<FancyInput ref={api} />
					<ul>{filtered().map((f) => <li>{f}</li>)}</ul>
				</div>
			);
		}
	`
	if err := os.WriteFile(filepath.Join(pagesDir, "index.tsx"), []byte(page), 0644); err != nil {
		t.Fatal(err)
	}

	cfg := config.Default()
	cfg.PagesDir = pagesDir
	cfg.OutDir = filepath.Join(root, "dist")
	if err := New(root, cfg).BuildAll(); err != nil {
		t.Fatalf("build: %v", err)
	}

	runtimeFiles, _ := filepath.Glob(filepath.Join(cfg.OutDir, "chunks", "runtime.*.js"))
	if len(runtimeFiles) == 0 {
		t.Fatal("runtime chunk not emitted")
	}
	pageFiles, err := filepath.Glob(filepath.Join(cfg.OutDir, "index.*.js"))
	if err != nil || len(pageFiles) == 0 {
		t.Fatalf("page bundle not found: %v", err)
	}

	script := `
import { Window } from 'happy-dom';
import fs from 'node:fs';

const win = new Window({ url: 'http://localhost/' });
win.document.write(fs.readFileSync(process.env.KRATE_HTML, 'utf8'));
win.__KRATE_CFG__ = { basePath: '', viewTransitions: 'auto' };

const errors = [];
win.addEventListener('error', (event) => {
  errors.push(String((event.error && event.error.stack) || event.error || event.message));
});

win.eval(fs.readFileSync(process.env.KRATE_RUNTIME, 'utf8'));
win.eval(fs.readFileSync(process.env.KRATE_PAGE, 'utf8'));

await new Promise((resolve) => setTimeout(resolve, 100));

if (errors.length) {
  console.error(errors.join('\n'));
  process.exit(1);
}
const text = win.document.body.textContent || '';
if (!text.includes('Apple')) {
  console.error('expected Apple in rendered text, got: ' + text);
  process.exit(1);
}
win.document.getElementById("inc").click();
win.document.getElementById("inc").click();
win.document.getElementById("noop").click();
await new Promise((resolve) => setTimeout(resolve, 50));
const countText = win.document.getElementById("count").textContent.trim();
if (countText !== "2") { console.error("counter not updated: " + countText); process.exit(1); }
console.log("OK");
process.exit(0);
`
	cmd := exec.Command("node", "--input-type=module", "-e", script)
	cmd.Dir = filepath.Join(repoRoot, "packages", "runtime")
	cmd.Env = append(os.Environ(),
		"KRATE_HTML="+filepath.Join(cfg.OutDir, "index.html"),
		"KRATE_RUNTIME="+runtimeFiles[0],
		"KRATE_PAGE="+pageFiles[0],
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("headless hydration failed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "OK") {
		t.Fatalf("unexpected headless output: %s", out)
	}
}
