package irtree_test

import (
	"strings"
	"testing"
)

func TestLocalExecBuildUp(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string
	}{
		{
			name: "grouping through find and an alias",
			src: `export default function App() {
	const pages = [{ d: "a", t: "A1" }, { d: "b", t: "B1" }, { d: "a", t: "A2" }];
	const groups = [];
	for (const p of pages) {
		let g = groups.find((x) => x.d === p.d);
		if (!g) { g = { d: p.d, items: [] }; groups.push(g); }
		g.items.push({ t: p.t });
	}
	return <nav>{groups.map((g) => <section><h2>{g.d}</h2>{g.items.map((i) => <a>{i.t}</a>)}</section>)}</nav>;
}`,
			want: `<nav><section><h2>a</h2><a>A1</a><a>A2</a></section><section><h2>b</h2><a>B1</a></section></nav>`,
		},
		{
			name: "pushing objects and arrays",
			src: `export default function App() {
	const rows = [];
	for (let i = 0; i < 3; i++) rows.push({ n: i, pair: [i, i * 2] });
	return <p>{rows.map((r) => r.n + ":" + r.pair.join("/")).join(",")}</p>;
}`,
			want: `<p>0:0/0,1:1/2,2:2/4</p>`,
		},
		{
			name: "switch inside a loop",
			src: `export default function App() {
	const out = [];
	for (const k of [1, 2, 3, 4]) {
		switch (k % 3) {
			case 0: out.push("zero");
			case 1: out.push("one"); break;
			default: out.push("other");
		}
	}
	return <p>{out.join(",")}</p>;
}`,
			want: `<p>one,other,zero,one,one</p>`,
		},
		{
			name: "break continue and labels",
			src: `export default function App() {
	const out = [];
	rows: for (let r = 0; r < 4; r++) {
		for (let c = 0; c < 4; c++) {
			if (c === 1) continue;
			if (c === 3) continue rows;
			if (r === 2) break rows;
			out.push(r + "" + c);
		}
	}
	return <p>{out.join(",")}</p>;
}`,
			want: `<p>00,02,10,12</p>`,
		},
		{
			name: "mutation through an alias reaches the array element",
			src: `export default function App() {
	const list = [{ n: 1 }, { n: 2 }];
	const second = list[1];
	second.n = 20;
	for (const item of list) item.n += 1;
	return <p>{list.map((i) => i.n).join(",")}|{second.n}</p>;
}`,
			want: `<p>2,21|21</p>`,
		},
		{
			name: "array methods that change the array",
			src: `export default function App() {
	const a = [3, 1, 2];
	a.sort();
	a.push(9);
	a.unshift(0);
	a.splice(1, 1);
	a.reverse();
	const first = a.shift();
	const last = a.pop();
	a[1] = "x";
	return <p>{first}|{last}|{a.join(",")}</p>;
}`,
			want: `<p>9|0|3,x</p>`,
		},
		{
			name: "string and number accumulation with let",
			src: `export default function App() {
	let s = "";
	let n = 0;
	for (const w of ["a", "b", "c"]) { s += w; n += s.length; }
	return <p>{s}:{n}</p>;
}`,
			want: `<p>abc:6</p>`,
		},
		{
			name: "Map and Set",
			src: `export default function App() {
	const seen = new Set();
	const byLen = new Map();
	for (const w of ["aa", "b", "cc", "aa"]) {
		seen.add(w);
		byLen.set(w.length, [...(byLen.get(w.length) || []), w]);
	}
	return <p>{seen.size}|{[...byLen.entries()].map(([k, v]) => k + "=" + v.join("+")).join(";")}</p>;
}`,
			want: `<p>3|2=aa+cc+aa;1=b</p>`,
		},
		{
			name: "loop pushing JSX",
			src: `export default function App() {
	const items = [];
	for (const w of ["x", "y"]) items.push(<li key={w}>{w}</li>);
	return <ul>{items}</ul>;
}`,
			want: `<ul><li>x</li><li>y</li></ul>`,
		},
		{
			name: "try and finally in a loop",
			src: `export default function App() {
	const out = [];
	for (const v of [1, 2]) { try { if (v === 2) throw new Error("no"); out.push(v); } catch (e) { out.push(e.message); } finally { out.push("f"); } }
	return <p>{out.join(",")}</p>;
}`,
			want: `<p>1,f,no,f</p>`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tree := annotateAndBuild(t, tc.src)
			if got := renderStaticHTML(tree.Root.Children); got != tc.want {
				t.Fatalf("HTML = %q, want %q", got, tc.want)
			}
		})
	}
}

// A component that changes a module constant sees the change itself, and no
// other component or page does.
func TestLocalExecModuleConstantIsCopied(t *testing.T) {
	src := `const TAGS = ["x"];
const ITEMS = [{ n: "b" }, { n: "a" }];
function Child({ tag }) {
	const tags = TAGS;
	tags.push(tag);
	for (const it of ITEMS) it.n += tag;
	return <p>{TAGS.join(",")}|{ITEMS.map((i) => i.n).join(",")}</p>;
}
export default function App() {
	return <div><Child tag="1" /><Child tag="2" /><p>{TAGS.join(",")}|{ITEMS.map((i) => i.n).join(",")}</p></div>;
}`
	tree := annotateAndBuild(t, src)
	want := `<div><p>x,1|b1,a1</p><p>x,2|b2,a2</p><p>x|b,a</p></div>`
	if got := renderStaticHTML(tree.Root.Children); got != want {
		t.Fatalf("HTML = %q, want %q", got, want)
	}
}

func TestLocalExecReportsWhyFoldingStopped(t *testing.T) {
	warns := func(src string) string {
		tree := annotateAndBuild(t, src)
		var b strings.Builder
		for _, w := range tree.Warnings {
			b.WriteString(string(w.Code) + " " + w.Message + "\n")
		}
		return b.String()
	}
	t.Run("loop limit", func(t *testing.T) {
		got := warns(`export default function App() {
	const out = [];
	for (let i = 0; i < 150000; i++) { if (i === 5) out.push(i); }
	return <p>{out.join(",")}</p>;
}`)
		if !strings.Contains(got, "KR8356") || !strings.Contains(got, "for loop") || !strings.Contains(got, "loop iteration limit exceeded") {
			t.Fatalf("warnings = %q", got)
		}
	})
	t.Run("signal read is silent", func(t *testing.T) {
		got := warns(`export default function App() {
	const [n] = createSignal(2);
	const out = [];
	for (let i = 0; i < n(); i++) out.push(i);
	return <p>{out.join(",")}</p>;
}`)
		if strings.Contains(got, "KR8356") {
			t.Fatalf("a loop over a signal must not warn: %q", got)
		}
	})
}
