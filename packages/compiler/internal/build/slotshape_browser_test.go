package build

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestBrowserSlotDOMShape: server children of a client component sit directly
// in the element the component renders them in, as in React, with no wrapper
// element, so child-combinator CSS matches them: shadcn's Button
// (has-[>svg]:px-4) with a server icon, and a list (space-y-*, > :not(:last-child))
// of server items. The shape holds after hydration, after the component
// renders nodes before and after the slot, and after it hides and shows the
// slot, and a nested island in the slot keeps its state. Slots next to each
// other keep their order and focus when the component re-renders, and a slot
// the component moves takes its nodes along.
func TestBrowserSlotDOMShape(t *testing.T) {
	t.Parallel()
	e2eEnv(t)
	b, root := scProject(t, map[string]string{
		"src/components/button.tsx": `'use client';
import { useState } from 'react';
export function Button({ children }: any) {
  const [busy, setBusy] = useState(false);
  return <button id="btn" className="btn" onClick={() => setBusy(!busy)}>{busy && <b id="lead">!</b>}{children}{busy && <i id="spin">*</i>}</button>;
}
`,
		"src/components/list.tsx": `'use client';
import { useState } from 'react';
export function List({ children }: any) {
  const [show, setShow] = useState(true);
  return <div><button id="toggle" onClick={() => setShow(!show)}>toggle</button>{show ? <ul id="list" className="list">{children}</ul> : <p id="hidden">hidden</p>}</div>;
}
export function Counter() {
  const [n, setN] = useState(0);
  return <button id="count" onClick={() => setN(n + 1)}>count {n}</button>;
}
`,
		"src/components/shell.tsx": `'use client';
import { useState } from 'react';
export function Shell({ left, right, children }: any) {
  const [n, setN] = useState(0);
  const [flip, setFlip] = useState(false);
  const x = <b key="x" id="x">x</b>, y = <b key="y" id="y">y</b>;
  return <div>
    <button id="bump" onClick={() => setN(n + 1)}>bump {n}</button>
    <button id="flip" onClick={() => setFlip(!flip)}>flip</button>
    <div id="pair">{n > 1 && <i id="head">head</i>}{left}{right}{n > 0 && <i id="tail">tail</i>}</div>
    <div id="order">{flip ? [children, x, y] : [x, y, children]}</div>
  </div>;
}
`,
		"src/pages/index.tsx": `import { Button } from '../components/button';
import { List, Counter } from '../components/list';
import { Shell } from '../components/shell';
const css = '.btn{padding-left:24px}.btn:has(> svg){padding-left:16px}.list>:not(:last-child){margin-bottom:8px}.list>li{color:rgb(1, 2, 3)}';
async function Item() { await Promise.resolve(); return <li id="item"><Counter /></li>; }
export default async function Page() {
  await Promise.resolve();
  return <main><style dangerouslySetInnerHTML={{ __html: css }} /><Button><svg className="icon" width="10" height="10"></svg> Sign in</Button><List><li>one</li><li>two</li><Item /></List><Shell left={<input id="in" />} right={<em id="r">r</em>}><u id="kid">kid</u></Shell></main>;
}
`,
	})
	withPreact(t, root)
	if err := linkRuntimePackage(t, filepath.Join(root, "node_modules", "@krate")); err != nil {
		t.Fatal(err)
	}
	if out, err := captureBuild(b); err != nil {
		t.Fatalf("BuildAll: %v\n%s", err, out)
	}
	base, err := startKrateServer(t, root, b.Cfg)
	if err != nil {
		t.Fatalf("serve: %v", err)
	}
	status, _, html := getBody(t, base+"/")
	if status != 200 {
		t.Fatalf("status %d:\n%s", status, html)
	}
	if strings.Contains(html, "krate-children") || strings.Contains(html, "krate-raw") {
		t.Errorf("server children must not be wrapped in an element:\n%s", html)
	}
	for _, re := range []string{
		`<button[^>]*class="btn"[^>]*><!--krate-slot:([0-9a-z]+)--><svg class="icon"[^>]*></svg> Sign in<!--/krate-slot:([0-9a-z]+)--></button>`,
		`<ul[^>]*class="list"[^>]*><!--krate-slot:[0-9a-z]+--><li>one</li><li>two</li><li id="item">`,
	} {
		if !regexp.MustCompile(re).MatchString(html) {
			t.Errorf("server HTML does not match %s:\n%s", re, html)
		}
	}

	browser := strings.ToLower(strings.TrimSpace(os.Getenv("KRATE_BROWSER")))
	if browser == "" {
		return
	}
	if !canStopInProcessServer {
		t.Skip("in-process server needs a unix host")
	}
	if err := exec.Command("node", "-e", "require.resolve('playwright')").Run(); err != nil {
		requireE2E(t, "playwright is not installed (run pnpm install)")
	}
	script, err := filepath.Abs(filepath.Join("testdata", "browser_slot_shape.mjs"))
	if err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("node", script, browser, base).CombinedOutput()
	if err != nil {
		if strings.Contains(string(out), "Executable doesn't exist") || strings.Contains(string(out), "browserType.launch") {
			requireE2E(t, "%s browser is not installed: %s", browser, out)
		}
		t.Fatalf("browser checks failed: %v\n%s", err, out)
	}
}
