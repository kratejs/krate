package build

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

// buildLoopSite builds files without Tailwind and returns the output dir and
// the build log.
func buildLoopSite(t *testing.T, files map[string]string) (out, logged string) {
	t.Helper()
	_, out, logged = buildFiles(t, files, func(c *config.Config) {
		c.Tailwind.Enabled = false
		c.Tailwind.Strict = false
	})
	return out, logged
}

func readLoopOut(t *testing.T, out, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(out, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// loopSiteData is a module constant several tests read through a prop.
const loopSiteData = `const site = [
  { dir: "a", title: "A1", href: "/a1" },
  { dir: "b", title: "B1", href: "/b1" },
  { dir: "a", title: "A2", href: "/a2" },
];
`

// Statements that build up a value in a component body fold at build time,
// in components with state (hydrated, folded by the IR builder) and without
// (rendered by the build-time evaluator).
func TestLocalLoopBuildUp(t *testing.T) {
	t.Parallel()
	nav := `function Nav({ pages }) {
  const groups = [];
  for (const p of pages) { let g = groups.find((x) => x.dir === p.dir); if (!g) { g = { dir: p.dir, items: [] }; groups.push(g); } g.items.push({ title: p.title, href: p.href }); }
  return <nav>{groups.map((g) => <section><h2>{g.dir}</h2>{g.items.map((i) => <a href={i.href}>{i.title}</a>)}</section>)}</nav>;
}
`
	const wantNav = `<nav><section><h2>a</h2><a href=/a1>A1</a><a href=/a2>A2</a></section><section><h2>b</h2><a href=/b1>B1</a></section></nav>`
	hydrated := func(body string) string {
		return `import { createSignal } from "@krate/runtime";
` + loopSiteData + body + `
export default function Page() {
  const [n, setN] = createSignal(0);
  return <main><button onClick={() => setN(n() + 1)}>{n()}</button><Widget pages={site} /></main>;
}`
	}
	stateless := func(body string) string {
		return loopSiteData + body + `
export default function Page() { return <main><Widget pages={site} /></main>; }`
	}
	cases := []struct {
		name string
		body string
		want []string
	}{
		{
			name: "grouping with find and an alias",
			body: strings.Replace(nav, "Nav", "Widget", 1),
			want: []string{wantNav},
		},
		{
			name: "switch grouping",
			body: `function Widget({ pages }) {
  const a = [], b = [];
  for (const p of pages) {
    switch (p.dir) {
      case "a": a.push(p.title); break;
      case "b": b.push(p.title); break;
      default: a.push("?");
    }
  }
  return <p>{a.join("+")}|{b.join("+")}</p>;
}`,
			want: []string{`<p>A1+A2|B1</p>`},
		},
		{
			name: "break and continue",
			body: `function Widget({ pages }) {
  const seen = [];
  let skipped = 0;
  for (const p of pages) {
    if (p.dir === "b") { skipped += 1; continue; }
    seen.push(p.title);
    if (seen.length === 1) break;
  }
  return <p>{seen.join(",")}|{skipped}</p>;
}`,
			want: []string{`<p>A1|0</p>`},
		},
		{
			name: "labelled break out of nested loops",
			body: `function Widget({ pages }) {
  const out = [];
  outer: for (const p of pages) {
    for (let i = 0; i < 3; i++) {
      if (i === 2) continue outer;
      if (p.dir === "b") break outer;
      out.push(p.title + i);
    }
  }
  return <p>{out.join(",")}</p>;
}`,
			want: []string{`<p>A10,A11</p>`},
		},
		{
			name: "nested object mutation through an alias",
			body: `function Widget({ pages }) {
  const byDir = { a: { count: 0, titles: [] }, b: { count: 0, titles: [] } };
  for (const p of pages) {
    const entry = byDir[p.dir];
    entry.count += 1;
    entry.titles.push(p.title);
  }
  const first = byDir.a;
  first.label = "dir a";
  return <p>{byDir.a.count}:{byDir.a.titles.join("+")}:{byDir.b.count}:{byDir.a.label}</p>;
}`,
			want: []string{`<p>2:A1+A2:1:dir a</p>`},
		},
		{
			name: "loop pushing JSX",
			body: `function Widget({ pages }) {
  const rows = [];
  for (const p of pages) {
    if (p.dir === "a") rows.push(<li key={p.href}><a href={p.href}>{p.title}</a></li>);
  }
  return <ul>{rows}</ul>;
}`,
			want: []string{`<ul><li><a href=/a1>A1</a></li><li><a href=/a2>A2</a></li></ul>`},
		},
		{
			name: "array methods and index assignment",
			body: `function Widget({ pages }) {
  const list = pages.map((p) => p.title);
  list.reverse();
  list.unshift("first");
  list.splice(2, 1, "x", "y");
  const last = list.pop();
  list[0] = "F";
  list.sort();
  const head = list.shift();
  return <p>{head}|{last}|{list.join(",")}</p>;
}`,
			want: []string{`<p>A2|A1|F,x,y</p>`},
		},
		{
			name: "Map and Set building",
			body: `function Widget({ pages }) {
  const dirs = new Set();
  const counts = new Map();
  for (const p of pages) {
    dirs.add(p.dir);
    counts.set(p.dir, (counts.get(p.dir) || 0) + 1);
  }
  let text = "";
  for (const [k, v] of counts) text += k + "=" + v + ";";
  return <p>{dirs.size}|{text}</p>;
}`,
			want: []string{`<p>2|a=2;b=1;</p>`},
		},
		{
			name: "while loop with let reassignment and string append",
			body: `function Widget({ pages }) {
  let i = 0;
  let s = "";
  while (i < pages.length) { s += pages[i].dir; i++; }
  let n = 0;
  do { n += 2; } while (n < 5);
  return <p>{s}|{n}</p>;
}`,
			want: []string{`<p>aba|6</p>`},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name+"/hydrated", func(t *testing.T) {
			t.Parallel()
			out, logged := buildLoopSite(t, map[string]string{"src/pages/index.tsx": hydrated(tc.body)})
			html := readLoopOut(t, out, "index.html")
			for _, w := range tc.want {
				if !strings.Contains(html, w) {
					t.Errorf("html misses %s:\n%s\nlog:\n%s", w, html, logged)
				}
			}
			if strings.Contains(logged, "KR8356") {
				t.Errorf("unexpected fold warning:\n%s", logged)
			}
		})
		t.Run(tc.name+"/stateless", func(t *testing.T) {
			t.Parallel()
			out, logged := buildLoopSite(t, map[string]string{"src/pages/index.tsx": stateless(tc.body)})
			html := readLoopOut(t, out, "index.html")
			for _, w := range tc.want {
				if !strings.Contains(html, w) {
					t.Errorf("html misses %s:\n%s\nlog:\n%s", w, html, logged)
				}
			}
		})
	}
}

// A page that changes a module constant must not change what another page
// (or another instance) renders: the value is copied when it is changed.
func TestLocalLoopModuleConstantIsolation(t *testing.T) {
	t.Parallel()
	files := map[string]string{
		"src/shared.ts": `export const ITEMS = [{ n: "b" }, { n: "a" }];
export const TAGS = ["x"];
`,
		"src/widgets.tsx": `import { createSignal } from "@krate/runtime";
import { ITEMS, TAGS } from "./shared";
export function Plain({ tag }) {
  const tags = TAGS;
  tags.push(tag);
  const sorted = ITEMS;
  sorted.sort((a, b) => (a.n < b.n ? -1 : 1));
  for (const item of sorted) item.n = item.n + tag;
  return <p class="plain">{tags.join(",")}|{sorted.map((i) => i.n).join(",")}</p>;
}
export function Live({ tag }) {
  const [c] = createSignal(0);
  const tags = TAGS;
  tags.push(tag);
  for (const item of ITEMS) item.n = item.n + tag;
  return <p class="live">{c()}{tags.join(",")}|{ITEMS.map((i) => i.n).join(",")}</p>;
}
`,
		"src/pages/index.tsx": `import { Plain, Live } from "../widgets";
import { ITEMS, TAGS } from "../shared";
export default function Page() {
  return <main><Plain tag="p1" /><Live tag="l1" /><p class="raw">{TAGS.join(",")}|{ITEMS.map((i) => i.n).join(",")}</p></main>;
}`,
		"src/pages/other.tsx": `import { Plain, Live } from "../widgets";
import { ITEMS, TAGS } from "../shared";
export default function Page() {
  return <main><Plain tag="p2" /><Live tag="l2" /><p class="raw">{TAGS.join(",")}|{ITEMS.map((i) => i.n).join(",")}</p></main>;
}`,
	}
	out, logged := buildLoopSite(t, files)
	first := readLoopOut(t, out, "index.html")
	second := readLoopOut(t, out, "other/index.html")
	for _, w := range []string{`<p class=plain>x,p1|ap1,bp1</p>`, `>x,l1|bl1,al1</p>`, `<p class=raw>x|b,a</p>`} {
		if !strings.Contains(first, w) {
			t.Errorf("first page misses %s:\n%s\nlog:\n%s", w, first, logged)
		}
	}
	for _, w := range []string{`<p class=plain>x,p2|ap2,bp2</p>`, `>x,l2|bl2,al2</p>`, `<p class=raw>x|b,a</p>`} {
		if !strings.Contains(second, w) {
			t.Errorf("second page misses %s:\n%s\nlog:\n%s", w, second, logged)
		}
	}
}

// When a statement cannot be run, the build says which one and why, unless
// the value only the browser knows is the reason.
func TestLocalLoopGiveUpIsReported(t *testing.T) {
	t.Parallel()
	page := func(body string) map[string]string {
		return map[string]string{"src/pages/index.tsx": `import { createSignal } from "@krate/runtime";
export default function Page() {
  const [n, setN] = createSignal(0);
` + body + `
  return <main><button onClick={() => setN(n() + 1)}>{n()}</button><p>{out.join(",")}</p></main>;
}`}
	}
	t.Run("loop limit", func(t *testing.T) {
		t.Parallel()
		_, logged := buildLoopSite(t, page(`  const out = [];
  for (let i = 0; i < 200000; i++) { if (i === 0) out.push("x"); }`))
		for _, w := range []string{"KR8356", "for loop", "loop iteration limit exceeded", "index.tsx:5"} {
			if !strings.Contains(logged, w) {
				t.Errorf("log misses %q:\n%s", w, logged)
			}
		}
	})
	t.Run("unsupported construct", func(t *testing.T) {
		t.Parallel()
		_, logged := buildLoopSite(t, page(`  const out = [];
  const tag = (s) => s[0];
  for (const w of ["a", "b"]) { out.push(tag`+"`x`"+`); }`))
		if !strings.Contains(logged, "KR8356") || !strings.Contains(logged, "tagged template") {
			t.Errorf("expected a KR8356 warning naming the construct:\n%s", logged)
		}
	})
	t.Run("a value from the browser is not reported", func(t *testing.T) {
		t.Parallel()
		_, logged := buildLoopSite(t, page(`  const out = [];
  for (let i = 0; i < n(); i++) { out.push("x"); }`))
		if strings.Contains(logged, "KR8356") {
			t.Errorf("a loop over a signal must not warn:\n%s", logged)
		}
	})
}

// A return inside a loop is evaluated in a component without state.
func TestLocalLoopEarlyReturnStateless(t *testing.T) {
	t.Parallel()
	out, logged := buildLoopSite(t, map[string]string{"src/pages/index.tsx": `function Pick({ ids }) {
  for (const id of ids) {
    if (id > 1) return <b>big {id}</b>;
  }
  return <i>none</i>;
}
export default function Page() { return <main><Pick ids={[0, 1, 2, 3]} /><Pick ids={[0]} /></main>; }`})
	html := readLoopOut(t, out, "index.html")
	if !strings.Contains(html, "<main><b>big 2</b><i>none</i></main>") {
		t.Errorf("html:\n%s\nlog:\n%s", html, logged)
	}
}
