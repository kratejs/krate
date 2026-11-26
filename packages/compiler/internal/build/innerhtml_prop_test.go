package build

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestInnerHTMLPropSetsContent: <el innerHTML={...}> sets raw content in the
// static page and in a component the SSR evaluator renders, like
// dangerouslySetInnerHTML.
func TestInnerHTMLPropSetsContent(t *testing.T) {
	t.Parallel()
	b, root := scProject(t, map[string]string{
		"src/components/Box.tsx": `export default function Box(props) { return <section innerHTML={props.html} />; }`,
		"src/pages/index.tsx": `import Box from "../components/Box";
const note = "<em>raw</em>";
export default function Home() {
  return <main><div id="a" innerHTML={note} /><Box html="<u>boxed</u>" /><p id="t" textContent={"a < b"} /></main>;
}`,
	})
	if err := b.BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	html := readDistPage(t, root, "index.html")
	for _, want := range []string{"<em>raw</em>", "<u>boxed</u>", "a &lt; b"} {
		if !strings.Contains(html, want) {
			t.Errorf("missing %q:\n%s", want, html)
		}
	}
	if strings.Contains(html, "innerHTML=") || strings.Contains(html, "textContent=") {
		t.Errorf("content prop leaked as an attribute:\n%s", html)
	}
}

func TestInnerHTMLPropWithChildrenFailsBuild(t *testing.T) {
	t.Parallel()
	b, _ := scProject(t, map[string]string{
		"src/pages/index.tsx": `export default function Home() { return <div innerHTML="<b>x</b>">child</div>; }`,
	})
	err := b.BuildAll()
	if err == nil || !strings.Contains(err.Error(), "KR8032") {
		t.Fatalf("want KR8032 error, got %v", err)
	}
}

// TestInnerHTMLPropReactiveHydrates: a value that reads state is rebound in
// the browser, like dangerouslySetInnerHTML.
func TestInnerHTMLPropReactiveHydrates(t *testing.T) {
	t.Parallel()
	root := runtimeScriptProject(t, map[string]string{
		"src/pages/index.tsx": `import { useState } from "react";
export default function Home() {
  const [html, setHtml] = useState("<b>one</b>");
  return <main><div id="c" innerHTML={html} /><button onClick={() => setHtml("<i>two</i>")}>go</button></main>;
}`,
	})
	page := readDistPage(t, root, "index.html")
	if !strings.Contains(page, "<b>one</b>") {
		t.Fatalf("initial content missing:\n%s", page)
	}
	var js strings.Builder
	files, _ := filepath.Glob(filepath.Join(root, "dist", "*.js"))
	more, _ := filepath.Glob(filepath.Join(root, "dist", "*", "*.js"))
	for _, f := range append(files, more...) {
		data, _ := os.ReadFile(f)
		js.Write(data)
	}
	if !strings.Contains(page+js.String(), "kbindInnerHTML(") {
		t.Fatalf("no innerHTML binding:\n%s", page)
	}
}

// TestJSXInModuleDataMappedIntoComponentProp: JSX held in a module constant's
// array, handed through a mapped component prop, renders where the component
// maps it into list items.
func TestJSXInModuleDataMappedIntoComponentProp(t *testing.T) {
	t.Parallel()
	b, root := scProject(t, map[string]string{
		"src/lib/steps.tsx": `export const STEPS = [
  { title: "One", facts: [<>text <strong>bold</strong> more</>, <Mark />, "plain"] },
  { title: "Two", facts: [<>second</>] },
];
function Mark() { return <i>mark</i>; }`,
		"src/components/Facts.tsx": `export function Facts({ items, className }) {
  return <ul className={className}>{items.map((f, i) => <li key={i} style={{ "--delay": ` + "`${i * 80}ms`" + ` }}>{f}</li>)}</ul>;
}`,
		"src/components/Step.tsx": `import { Facts } from "./Facts";
export function Step({ title, facts }) { return <section><h2>{title}</h2><Facts items={facts} className="x" /></section>; }`,
		"src/pages/index.tsx": `import { STEPS } from "../lib/steps";
import { Step } from "../components/Step";
export default function Home() { return <main>{STEPS.map((s) => <Step key={s.title} title={s.title} facts={s.facts} />)}</main>; }`,
	})
	if err := b.BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	html := readDistPage(t, root, "index.html")
	for _, want := range []string{"text <strong>bold</strong> more</li>", "<i>mark</i></li>", ">plain</li>", ">second</li>"} {
		if !strings.Contains(html, want) {
			t.Errorf("missing %q:\n%s", want, html)
		}
	}
}
