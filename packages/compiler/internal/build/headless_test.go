package build

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

// runHeadless builds page (inside examples/ so the workspace runtime
// resolves), executes its runtime + hydration bundles in happy-dom and then
// runs check, an ES module body with `win` (the window) and `fail(msg)` in
// scope. It catches ReferenceErrors and wrong client behaviour in emitted code
// that a static build assertion cannot see.
func runHeadless(t *testing.T, page, check string) {
	t.Helper()
	if _, err := exec.LookPath("node"); err != nil {
		skipOrFail(t, "node is not installed")
	}
	repoRoot, err := filepath.Abs(filepath.Join("..", "..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	examplesDir := filepath.Join(repoRoot, "examples")
	if _, err := os.Stat(filepath.Join(examplesDir, "node_modules", "@krate", "runtime")); err != nil {
		skipOrFail(t, "examples workspace runtime is not installed: "+err.Error())
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
const fail = (msg) => { console.error(msg); process.exit(1); };

win.eval(fs.readFileSync(process.env.KRATE_RUNTIME, 'utf8'));
win.eval(fs.readFileSync(process.env.KRATE_PAGE, 'utf8'));

await new Promise((resolve) => setTimeout(resolve, 100));

if (errors.length) {
  fail(errors.join('\n'));
}
` + check + `
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

func skipOrFail(t *testing.T, reason string) {
	t.Helper()
	if os.Getenv("KRATE_REQUIRE_E2E") == "1" {
		t.Fatal(reason)
	}
	t.Skip(reason)
}

func TestHydrationRunsHeadless(t *testing.T) {
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
	runHeadless(t, page, `
const text = win.document.body.textContent || '';
if (!text.includes('Apple')) fail('expected Apple in rendered text, got: ' + text);
win.document.getElementById("inc").click();
win.document.getElementById("inc").click();
win.document.getElementById("noop").click();
await new Promise((resolve) => setTimeout(resolve, 50));
const countText = win.document.getElementById("count").textContent.trim();
if (countText !== "2") fail("counter not updated: " + countText);
`)
}

// TestHydrationSpreadForwardsHandler checks that a handler forwarded through
// `{...props}` on a client component's root element is bound from the props
// registry and runs.
func TestHydrationSpreadForwardsHandler(t *testing.T) {
	page := `
		import { useState } from 'react';
		function Btn(props: any) {
			const [n, setN] = useState(0);
			return <button {...props}>{n}</button>;
		}
		export default function Index() {
			const [c, setC] = useState(0);
			function inc() { setC(c + 1); }
			return <main><Btn id="b" title="t" onClick={inc} /><i id="c">{c}</i></main>;
		}
	`
	runHeadless(t, page, `
const b = win.document.getElementById("b");
if (!b || b.getAttribute("title") !== "t") fail("spread attributes missing: " + win.document.body.innerHTML);
b.click();
await new Promise((resolve) => setTimeout(resolve, 50));
const c = win.document.getElementById("c").textContent.trim();
if (c !== "1") fail("forwarded handler did not run: " + c);
`)
}

// TestHydrationReplaysBodyStatements checks that component-body statements
// which are not declarations (mutations, updates, dependency effects) run in
// the hydration scope in source order, so the browser shows what React shows.
func TestHydrationReplaysBodyStatements(t *testing.T) {
	page := `
		import { useState, useEffect } from 'react';
		export default function Index() {
			const cache = new Map<string, number>();
			cache.set("k", 1);
			const list = [1, 2];
			list.push(3);
			let n = 1;
			n++;
			const [c, setC] = useState(0);
			useEffect(() => { document.getElementById("eff")!.textContent = "ran" + c; }, [c]);
			return (
				<main>
					<p id="kw">{cache.has("k") ? "yes" : "no"}</p>
					<p id="len">{list.length}</p>
					<p id="n">{n}</p>
					<p id="eff"></p>
					<button id="b" onClick={() => setC(c + 1)}>+</button>
				</main>
			);
		}
	`
	runHeadless(t, page, `
const text = (id) => win.document.getElementById(id).textContent.trim();
if (text("kw") !== "yes") fail("kw: " + text("kw"));
if (text("len") !== "3") fail("len: " + text("len"));
if (text("n") !== "2") fail("n: " + text("n"));
if (text("eff") !== "ran0") fail("effect with deps did not run: " + text("eff"));
win.document.getElementById("b").click();
await new Promise((resolve) => setTimeout(resolve, 50));
if (text("eff") !== "ran1") fail("effect with deps did not rerun: " + text("eff"));
`)
}
