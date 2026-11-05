package build

import (
	"strings"
	"testing"
)

// Object keys written as `[expr]` fold at build time; the static page shows
// the key's value, not the expression's spelling.
func TestComputedObjectKeysFold(t *testing.T) {
	cases := map[string]struct{ src, want string }{
		"string constant and template": {
			"const K = 'color';\nconst o = { [K]: 'red', [`b${1}`]: 'x' };\nexport default function P() { return <p data-a={o.color} data-b={o.b1}>{Object.keys(o).join(',')}|{JSON.stringify(o)}</p>; }\n",
			"<p data-a=red data-b=x>color,b1|{&quot;color&quot;:&quot;red&quot;,&quot;b1&quot;:&quot;x&quot;}</p>",
		},
		"local object": {
			"export default function P() { const K = 'color'; const o = { [K]: 'red' }; return <p data-a={o.color}>{Object.keys(o).join(',')}</p>; }\n",
			"<p data-a=red>color</p>",
		},
		"member of a const object": {
			"const Kind = { A: 'a', B: 'b' } as const;\nconst m = { [Kind.A]: 1, [Kind.B]: 2 };\nexport default function P() { return <p>{m.a}-{m.b}</p>; }\n",
			"<p>1-2</p>",
		},
		"TypeScript enum member": {
			"enum Kind { A = 'a', B = 'b' }\nconst m = { [Kind.A]: 1, [Kind.B]: 2 };\nexport default function P() { return <p>{m.a}-{m.b}</p>; }\n",
			"<p>1-2</p>",
		},
		"number keys": {
			"const i = 3;\nconst o = { [i]: 'x', [i + 1]: 'y' };\nexport default function P() { return <p>{Object.keys(o).join(',')}{o[3]}</p>; }\n",
			"<p>3,4x</p>",
		},
		"later key wins": {
			"const K = 'a';\nconst o = { a: 1, [K]: 2 };\nexport default function P() { return <p>{o.a}</p>; }\n",
			"<p>2</p>",
		},
		"method with a computed name": {
			"const K = 'm';\nconst o = { [K]() { return 5; } };\nexport default function P() { return <p>{o.m()}</p>; }\n",
			"<p>5</p>",
		},
		"built by reduce and fromEntries": {
			"const items = [{id:'a',n:1},{id:'b',n:2}];\nconst byId = Object.fromEntries(items.map(i => [i.id, i.n]));\nconst byId2 = items.reduce((acc, i) => ({ ...acc, [i.id]: i.n }), {});\nexport default function P() { return <p>{byId.a}{byId2.b}|{Object.keys(byId2).join(',')}</p>; }\n",
			"<p>12|a,b</p>",
		},
		"passed to a component": {
			"function C({ o }) { return <p>{Object.keys(o).join(',')}:{o.color}</p>; }\nconst K = 'color';\nexport default function P() { return <main><C o={{ [K]: 'red' }} /></main>; }\n",
			"<p>color:red</p>",
		},
		"style object": {
			"const K = 'color';\nexport default function P() { return <p style={{ [K]: 'blue' }}>x</p>; }\n",
			"<p style=color:blue>x</p>",
		},
		"class object helper": {
			"function cx(o) { return Object.keys(o).filter(k => o[k]).join(' '); }\nconst cls = 'on';\nexport default function P() { return <p class={cx({ [cls]: true, off: false })}>x</p>; }\n",
			"<p class=on>x</p>",
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			html, _ := buildPlain(t, map[string]string{"src/pages/index.tsx": c.src})
			if !strings.Contains(html, c.want) {
				t.Errorf("want %s in:\n%s", c.want, rootOf(t, html))
			}
		})
	}
}

// A key the build cannot compute leaves the object to the runtime; the page
// must not show the expression's spelling as the key.
func TestComputedObjectKeyUnknownIsDeferred(t *testing.T) {
	html, _ := buildPlain(t, map[string]string{"src/pages/index.tsx": "export default function P() { const k = globalThis.foo; const o = { [k]: 'x' }; return <p>{JSON.stringify(o)}</p>; }\n"})
	if strings.Contains(html, "&quot;k&quot;") || strings.Contains(html, `"k"`) {
		t.Errorf("the key's spelling was baked in as a key:\n%s", rootOf(t, html))
	}
	if !strings.Contains(html, "<p><!--k:") {
		t.Errorf("the value should be a runtime slot:\n%s", rootOf(t, html))
	}
}

// Static and hydrated output agree for computed keys, including a key that
// reads a signal.
func TestComputedObjectKeysBrowser(t *testing.T) {
	page := dynTagSig + `const K = "color";
const o = { [K]: "red", [` + "`n${1}`" + `]: "x" };
export default function Page() {
	const [k, setK] = createSignal("a");
	return (
		<main>
			<p class="keys" onClick={() => setK("b")}>{Object.keys(o).join(",")}|{Object.keys({ [k()]: 1 }).join(",")}</p>
			<p class="style" style={{ [K]: "blue" }}>s</p>
		</main>
	);
}
`
	evalFixBrowser(t, map[string]string{"src/pages/index.tsx": page}, []evalFixCheck{
		{Path: "/", Selector: "p.keys", Hydrated: "color,n1|a", Click: "p.keys", AfterClick: strp("color,n1|b")},
	})
}
