package build

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

// react19BrowserFiles is one small app that uses the React 19 features whose
// builds used to pass and then fail in the browser: useId in child
// components, useEffectEvent, Activity, ViewTransition and custom element
// props. testdata/react19_browser.mjs drives it in a real browser against a
// production (trimmed runtime) build.
var react19BrowserFiles = map[string]string{
	"src/pages/index.tsx": `import { useState, useEffect, useEffectEvent, Activity, ViewTransition } from "react";
import Field from "../Field";
import Stateful from "../Stateful";
import Group from "../Group";
import Probe from "../Probe";

const FIXED = ["a", "b"];

export default function Page() {
  const [rows, setRows] = useState(["r1", "r2", "r 3"]);
  const [show, setShow] = useState(false);
  const [mode, setMode] = useState<"visible" | "hidden">("hidden");
  const [n, setN] = useState(0);
  const [log, setLog] = useState("");
  const [obj, setObj] = useState({ x: 1 });
  const onTick = useEffectEvent(() => setLog("n=" + n));
  useEffect(() => {
    const t = setTimeout(() => onTick(), 30);
    return () => clearTimeout(t);
  }, []);
  return (
    <main>
      <section id="ids">
        <Field label="one" /><Field label="two" />
        <Group title="g"><Field label="nested" /></Group>
        {FIXED.map((k) => <Field key={k} label={"fixed-" + k} />)}
        <Stateful label="st1" /><Stateful label="st2" />
        {show ? <Field label="cond" /> : null}
        <div id="dyn">{rows.map((r) => <Field key={r} label={r} />)}</div>
      </section>
      <button id="show" onClick={() => setShow(!show)}>show</button>
      <button id="add" onClick={() => setRows([...rows, "n" + rows.length])}>add</button>

      <section id="effect-event">
        <button id="inc" onClick={() => setN(n + 1)}>inc {n}</button>
        <button id="fire" onClick={() => onTick()}>fire</button>
        <p id="evt-log">{log}</p>
      </section>

      <section id="activity">
        <button id="tog" onClick={() => setMode(mode === "visible" ? "hidden" : "visible")}>tog</button>
        <Activity mode={mode}><Probe /><input id="act-input" defaultValue="" /></Activity>
        <Activity mode="hidden"><p id="act-static-hidden">static hidden</p></Activity>
      </section>

      <section id="vt">
        <ViewTransition name="hero" default="slow-fade"><h1 id="hero">Hero</h1></ViewTransition>
      </section>

      <section id="ce">
        <my-el id="ce-a" data={{ a: 1 }} list={[1, 2]} cb={() => 42} s="str" n={3} b={true} f={false} />
        <my-el id="ce-dyn" data={obj} />
        <button id="bump" onClick={() => setObj({ x: obj.x + 1 })}>bump</button>
      </section>
      <script src="/my-el.js"></script>
    </main>
  );
}`,
	"src/Field.tsx": `import { useId } from "react";
export default function Field({ label }: { label: string }) {
  const id = useId();
  return <div className="field"><label htmlFor={id}>{label}</label><input id={id} aria-describedby={id + "-hint"} /><span id={id + "-hint"}>hint</span></div>;
}`,
	"src/Stateful.tsx": `import { useId, useState } from "react";
export default function Stateful({ label }: { label: string }) {
  const id = useId();
  const [v, setV] = useState("");
  return <div className="field"><label htmlFor={id}>{label}</label><input id={id} value={v} onInput={(e) => setV(e.target.value)} /><em>{v}</em></div>;
}`,
	"src/Group.tsx": `import { useId } from "react";
export default function Group({ title, children }: { title: string; children?: any }) {
  const id = useId();
  return <fieldset aria-labelledby={id}><legend id={id}>{title}</legend>{children}</fieldset>;
}`,
	"src/Probe.tsx": `import { useEffect, useState } from "react";
export default function Probe() {
  const [n, setN] = useState(0);
  useEffect(() => {
    const w = window as any;
    (w.__log ||= []).push("setup");
    return () => { (w.__log ||= []).push("cleanup"); };
  }, []);
  return <button id="probe" onClick={() => setN(n + 1)}>probe {n}</button>;
}`,
	"public/my-el.js": `class MyEl extends HTMLElement {
  set data(v) { this._d = v; this.r(); }
  set list(v) { this._l = v; this.r(); }
  set cb(v) { this._c = v; this.r(); }
  connectedCallback() { this.r(); }
  r() {
    this.setAttribute("data-out", JSON.stringify({
      d: this._d, l: this._l, c: typeof this._c === "function" ? this._c() : null,
      s: this.getAttribute("s"), n: this.getAttribute("n"), b: this.getAttribute("b"), f: this.getAttribute("f"),
    }));
  }
}
customElements.define("my-el", MyEl);
`,
}

// TestReact19Browser builds react19BrowserFiles for production and drives it
// in a real browser (KRATE_BROWSER=chromium|firefox|webkit).
func TestReact19Browser(t *testing.T) {
	browser := strings.ToLower(strings.TrimSpace(os.Getenv("KRATE_BROWSER")))
	if browser == "" {
		t.Skip("set KRATE_BROWSER=chromium|firefox|webkit to run browser checks")
	}
	if _, err := exec.LookPath("node"); err != nil {
		requireE2E(t, "node is not installed")
	}
	if err := exec.Command("node", "-e", "require.resolve('playwright')").Run(); err != nil {
		requireE2E(t, "playwright is not installed (run pnpm install)")
	}
	repoRoot, err := repoRootPath()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "packages"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(repoRoot, "packages", "runtime"), filepath.Join(root, "packages", "runtime")); err != nil {
		t.Skipf("cannot link the runtime package: %v", err)
	}
	for rel, src := range react19BrowserFiles {
		full := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cfg := config.Default()
	cfg.PagesDir = filepath.Join(root, "src", "pages")
	cfg.OutDir = filepath.Join(root, "dist")
	cfg.PublicDir = filepath.Join(root, "public")
	if err := New(root, cfg).BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	script, err := filepath.Abs(filepath.Join("testdata", "react19_browser.mjs"))
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("node", script, cfg.OutDir, browser)
	cmd.Env = append(os.Environ(), "NODE_PATH="+filepath.Join(repoRoot, "node_modules"))
	out, err := cmd.CombinedOutput()
	if err != nil {
		if strings.Contains(string(out), "Executable doesn't exist") || strings.Contains(string(out), "browserType.launch") {
			requireE2E(t, "%s browser is not installed: %s", browser, out)
		}
		t.Fatalf("React 19 browser checks failed: %v\n%s", err, out)
	}
	t.Log(string(out))
}
