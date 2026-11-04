package build

import (
	"strings"
	"testing"
)

const moduleValuesLib = `
export const shout = (s: string) => s.toUpperCase() + "!";
export const cfg = { name: "Krate", n: 3 };
export const nf = new Intl.NumberFormat("en-US");
export class Store { v = 1; get() { return this.v; } }
export enum Color { Red, Green }
export namespace Util { export function twice(n: number) { return n * 2; } }
export function add(a: number, b: number) { return a + b; }
`

func TestImportedModuleValuesUsedInTextReachHydration(t *testing.T) {
	files := map[string]string{
		"src/lib/helpers.ts": moduleValuesLib,
		"src/pages/index.tsx": `
import { shout, cfg, nf, Store, Color, Util, add } from "../lib/helpers";
import * as ns from "../lib/helpers";
import { createSignal } from "@krate/runtime";
export default function Home() {
  const [n, setN] = createSignal(1);
  return (
    <div>
      <p id="a">{shout("hi")}</p>
      <p id="b">{cfg.name}</p>
      <p id="c">{nf.format(1234567)}</p>
      <p id="d">{new Store().get()}</p>
      <p id="e">{Color[1]}</p>
      <p id="f">{Util.twice(4)}</p>
      <p id="g">{ns.add(1, 2)}</p>
      <button onClick={() => setN(n() + add(1, 1))}>{n()}</button>
    </div>
  );
}`,
	}
	html, js, err := buildKrateProject(t, files)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	// Pure helper folds into the server HTML.
	if !strings.Contains(html, `<p id="a">HI!</p>`) {
		t.Errorf("shout(\"hi\") was not folded for SSR:\n%s", html)
	}
	if !strings.Contains(html, `<p id="b">Krate</p>`) {
		t.Errorf("object member did not fold:\n%s", html)
	}
	// Everything else is computed on the client, so each binding must be
	// declared in the hydration scope.
	for _, want := range []string{"var Store=(class Store", "Intl.NumberFormat", "(Util2.twice = twice)", `var ns={"add":add}`} {
		if !strings.Contains(js, want) {
			t.Errorf("hydration JS misses %q:\n%s", want, js)
		}
	}
	if !strings.Contains(js, `"#stmt`) {
		t.Errorf("namespace initializer statement not replayed:\n%s", js)
	}
}

func TestSameModuleClassAndNamespaceReachHandlers(t *testing.T) {
	files := map[string]string{
		"src/pages/index.tsx": `
import { createSignal } from "@krate/runtime";
class Store { v = 2; get() { return this.v; } }
namespace Util { export function twice(n: number) { return n * 2; } }
export default function Home() {
  const [n, setN] = createSignal(0);
  return <button onClick={() => setN(new Store().get() + Util.twice(2))}>{n()}</button>;
}`,
	}
	_, js, err := buildKrateProject(t, files)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if !strings.Contains(js, "var Store=(class Store") || !strings.Contains(js, ".twice = twice") {
		t.Errorf("class and namespace not emitted for handler use:\n%s", js)
	}
}

func TestJSONImportsRenderInHTML(t *testing.T) {
	files := map[string]string{
		"src/data/site.json": `{"title":"Site T","z":1,"a":2,"posts":[{"t":"One"},{"t":"Two"}]}`,
		"src/pages/index.tsx": `
import site from "../data/site.json";
import { title } from "../data/site.json";
import * as all from "../data/site.json";
export default function Home() {
  return (
    <div>
      <h1>{site.title}</h1>
      <h2>{title}</h2>
      <h3>{all.default.title}</h3>
      <ul>{site.posts.map((p) => <li>{p.t}</li>)}</ul>
    </div>
  );
}`,
	}
	html, _, err := buildKrateProject(t, files)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	for _, want := range []string{"<h1>Site T</h1>", "<h2>Site T</h2>", "<li>One</li>", "<li>Two</li>"} {
		if !strings.Contains(html, want) {
			t.Errorf("html misses %q:\n%s", want, html)
		}
	}
}

func TestInvalidJSONImportIsLocatedError(t *testing.T) {
	files := map[string]string{
		"src/data/bad.json": "{\n  \"a\": 1,\n  \"b\": oops\n}\n",
		"src/pages/index.tsx": `
import bad from "../data/bad.json";
export default function Home() { return <p>{bad.a}</p>; }`,
	}
	_, _, err := buildKrateProject(t, files)
	if err == nil {
		t.Fatal("expected an error for invalid JSON")
	}
	msg := err.Error()
	if !strings.Contains(msg, "bad.json:3:") || !strings.Contains(msg, "invalid JSON") {
		t.Errorf("error is not located at the JSON syntax error: %s", msg)
	}
}

func TestRawAndURLImports(t *testing.T) {
	files := map[string]string{
		"src/data/note.txt": "hello raw",
		"src/data/logo.svg": "<svg xmlns='http://www.w3.org/2000/svg'/>",
		"src/pages/index.tsx": `
import note from "../data/note.txt?raw";
import logo from "../data/logo.svg?url";
export default function Home() { return <div><p>{note}</p><img src={logo} alt="" /></div>; }`,
	}
	html, _, err := buildKrateProject(t, files)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if !strings.Contains(html, "<p>hello raw</p>") {
		t.Errorf("?raw text missing:\n%s", html)
	}
	if !strings.Contains(html, `src=/assets/logo-`) && !strings.Contains(html, `src="/assets/logo-`) {
		t.Errorf("?url asset URL missing:\n%s", html)
	}
}
