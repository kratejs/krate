package build

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// pageJS returns the text of the hydration scripts of a built page.
func builtPageJS(t *testing.T, outDir, route string) string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(outDir, route, "*.js"))
	if err != nil {
		t.Fatal(err)
	}
	var sb strings.Builder
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		sb.Write(data)
	}
	return sb.String()
}

func builtPageHTML(t *testing.T, outDir, route string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(outDir, route, "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// A hook imported from another module that creates state gives the component
// that calls it client state: it hydrates, the hook ships with it, and the
// server HTML holds the hook's initial values.
func TestImportedStateHookHydrates(t *testing.T) {
	t.Parallel()
	files := map[string]string{
		"src/lib/hooks.ts": `import { createSignal } from "@krate/runtime";
export function useToggle(initial = false) {
  const [on, setOn] = createSignal(initial);
  return { on, toggle: () => setOn(!on()) };
}
export function useFlag(initial = false) {
  const [on, setOn] = createSignal(initial);
  return [on, () => setOn(!on())] as const;
}
export function useOpenToggle() {
  return useToggle(true);
}
`,
		"src/components/panel.tsx": `import { useToggle, useFlag, useOpenToggle } from "../lib/hooks";
export function Panel() {
  const { on, toggle } = useToggle();
  return <button id="obj" onClick={toggle}>{on() ? "open" : "closed"}</button>;
}
export function FlagPanel() {
  const [on, toggle] = useFlag();
  return <button id="arr" onClick={toggle}>{on() ? "open" : "closed"}</button>;
}
export function NestedPanel() {
  const { on, toggle } = useOpenToggle();
  return <button id="nested" class={on() ? "a" : "b"} onClick={toggle}>{on() ? "open" : "closed"}</button>;
}
export function Status() {
  const { on } = useToggle(true);
  return <p id="status">{on() ? "on" : "off"}</p>;
}
export function Note(props: { text: string }) {
  return <p id="note">{props.text}</p>;
}
`,
		"src/pages/hooks.tsx": `import { Panel, FlagPanel, NestedPanel, Status } from "../components/panel";
export default function Page() {
  return <main><Panel /><FlagPanel /><NestedPanel /><Status /></main>;
}
`,
		"src/pages/plain.tsx": `import { Note } from "../components/panel";
export default function Page() {
  return <main><Note text="hello" /></main>;
}
`,
	}
	b, root := scProject(t, files)
	b.Cfg.Minify = false
	out, err := captureBuild(b)
	if err != nil {
		t.Fatalf("BuildAll failed: %v\n%s", err, out)
	}
	if strings.Contains(out, "KR8352") {
		t.Fatalf("unexpected KR8352:\n%s", out)
	}
	outDir := filepath.Join(root, "dist")
	html := builtPageHTML(t, outDir, "hooks")
	// Defaults give closed, an argument and a nested hook give open and on.
	for text, n := range map[string]int{"closed<!--/k:": 2, "open<!--/k:": 1, "on<!--/k:": 1} {
		if got := strings.Count(html, text); got != n {
			t.Errorf("page HTML has %d x %q, want %d:\n%s", got, text, n, html)
		}
	}
	js := builtPageJS(t, outDir, "hooks")
	for _, want := range []string{"createSignal(", "function useToggle", "function useFlag", "function useOpenToggle", "toggle"} {
		if !strings.Contains(js, want) {
			t.Errorf("page script lacks %q:\n%s", want, js)
		}
	}

	// A component with no state anywhere still renders at build time and ships
	// no script.
	if plain := builtPageJS(t, outDir, "plain"); strings.Contains(plain, "kbind") {
		t.Errorf("a plain stateless component must ship no bindings:\n%s", plain)
	}
	if got := builtPageHTML(t, outDir, "plain"); !strings.Contains(got, "hello") {
		t.Errorf("plain page lost its text:\n%s", got)
	}
}

// A React-source component whose hook comes from another module hydrates
// instead of failing the build with KR8352.
func TestReactSourceImportedHookHydrates(t *testing.T) {
	t.Parallel()
	files := map[string]string{
		"src/lib/hooks.ts": `import { useState } from "react";
export function useToggle(initial = false) {
  const [on, setOn] = useState(initial);
  return [on, () => setOn(!on)] as const;
}
`,
		"src/components/show.tsx": `import { useToggle } from "../lib/hooks";
export function Show() {
  const [on] = useToggle();
  return <p id="show">{on ? "open" : "closed"}</p>;
}
`,
		"src/pages/show.tsx": `import { Show } from "../components/show";
export default function Page() { return <main><Show /></main>; }
`,
	}
	b, root := scProject(t, files)
	b.Cfg.Minify = false
	out, err := captureBuild(b)
	if err != nil {
		t.Fatalf("BuildAll failed: %v\n%s", err, out)
	}
	js := builtPageJS(t, filepath.Join(root, "dist"), "show")
	if !strings.Contains(js, "useToggle") || !strings.Contains(js, "createSignal(") {
		t.Fatalf("the hook must ship with the component:\n%s", js)
	}
}

// A stateless component of a "use client" module is built as a client
// component: what the build cannot evaluate is bound in the browser instead
// of failing the build, and what it can evaluate stays plain HTML.
func TestUseClientStatelessComponentFallsBackToTheBrowser(t *testing.T) {
	t.Parallel()
	files := map[string]string{
		"src/components/widgets.tsx": `"use client";
import { Inner } from "./inner";
export function Width(props: { label: string }) {
  const w = window.innerWidth;
  return <p id="width">{props.label}: {typeof w}</p>;
}
export function Wide() {
  return <p id="wide">{window.innerWidth > 0 ? "wide" : "none"}</p>;
}
export function Fixed(props: { text: string }) {
  return <p id="fixed">{props.text}</p>;
}
export function Wrapper() {
  return <section id="wrapper"><Inner /></section>;
}
`,
		"src/components/inner.tsx": `export function Inner() {
  return <b id="inner">{typeof document}</b>;
}
`,
		"src/pages/client.tsx": `import { Width, Wide, Fixed, Wrapper } from "../components/widgets";
export default function Page() {
  return <main><Width label="w" /><Wide /><Fixed text="static text" /><Wrapper /></main>;
}
`,
		// The same code without the directive is evaluated at build time.
		"src/pages/server.tsx": `import { Wide } from "../components/nodirective";
export default function Page() { return <main><Wide /></main>; }
`,
		"src/components/nodirective.tsx": `export function Wide() {
  return <p id="wide">{window.innerWidth > 0 ? "wide" : "none"}</p>;
}
`,
	}
	b, _ := scProject(t, files)
	b.Cfg.Minify = false
	out, err := captureBuild(b)
	if err == nil {
		t.Fatalf("a component without the directive must still fail the build:\n%s", out)
	}
	if !strings.Contains(out, "server.tsx") || strings.Contains(out, "client.tsx") {
		t.Fatalf("only the page without the directive may fail:\n%s", out)
	}

	// Without that page the rest builds.
	rest := map[string]string{}
	for k, v := range files {
		if k != "src/pages/server.tsx" {
			rest[k] = v
		}
	}
	b2, root2 := scProject(t, rest)
	b2.Cfg.Minify = false
	out, err = captureBuild(b2)
	if err != nil {
		t.Fatalf("BuildAll failed: %v\n%s", err, out)
	}
	if strings.Contains(out, "KR8352") || strings.Contains(out, "KR8351") {
		t.Fatalf("unexpected build-time evaluation diagnostic:\n%s", out)
	}
	outDir := filepath.Join(root2, "dist")
	html := builtPageHTML(t, outDir, "client")
	if !strings.Contains(html, "static text") {
		t.Errorf("a component the build can evaluate keeps its HTML:\n%s", html)
	}
	js := builtPageJS(t, outDir, "client")
	for _, want := range []string{"window.innerWidth", "typeof document"} {
		if !strings.Contains(js, want) {
			t.Errorf("page script lacks %q:\n%s\nHTML:\n%s", want, js, html)
		}
	}
	if strings.Contains(js, "static text") {
		t.Errorf("a fully evaluated component ships no script:\n%s", js)
	}
}
