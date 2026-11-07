package build

import (
	"regexp"
	"strings"
	"testing"
)

const nestedRow = "function Row({ item: { id, tags: [first = 'none', ...rest] = [] }, idx }) { return <li data-id={id}>{first}:{rest.length}:{idx}</li>; }\n"

// A renamed or nested parameter pattern binds its names by destructuring the
// props, so the names inside it are the call site's values, not undefined and
// not a build failure.
func TestNestedParameterPatternsRender(t *testing.T) {
	const imp = "import { createSignal } from '@krate/runtime';\n"
	cases := map[string]struct{ src, want string }{
		"object and array inside": {nestedRow + "export default function P() { return <ul><Row item={{ id: 1, tags: ['x', 'y', 'z'] }} idx={0} /></ul>; }\n",
			`<li data-id=1>x:2:0</li>`},
		"default for the array": {nestedRow + "export default function P() { return <ul><Row item={{ id: 2 }} idx={0} /></ul>; }\n",
			`<li data-id=2>none:0:0</li>`},
		"in a list": {nestedRow + "export default function P() { const rows = [{ id: 1, tags: ['x'] }, { id: 2 }]; return <ul>{rows.map((r, i) => <Row key={r.id} item={r} idx={i} />)}</ul>; }\n",
			`<li data-id=1>x:0:0</li><li data-id=2>none:0:1</li>`},
		"in a list with a signal": {nestedRow + "export default function P() { const rows = [{ id: 1, tags: ['x'] }, { id: 2 }]; const [n] = createSignal(0); return <ul>{rows.map((r, i) => <Row key={r.id} item={r} idx={i + n()} />)}</ul>; }\n",
			`<li data-id=1>x:0:0</li><li data-id=2>none:0:1</li>`},
		"object rest and array rest": {"function Row({ pair: [a, [b, ...c] = [], ...d], opts: { x = 1, ...o } = {}, n }) { const [k, setK] = createSignal(n); return <li onClick={() => setK(k() + 1)}>{a}{b}{c.length}{d.length}{x}{Object.keys(o).length}{k()}</li>; }\n" +
			"export default function P() { return <ul><Row pair={[1, [2, 3, 4], 5, 6]} opts={{ x: 9, y: 1 }} n={0} /><Row pair={[7]} n={1} /></ul>; }\n",
			`1222910`},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			html, _ := buildPlain(t, map[string]string{"src/pages/index.tsx": imp + c.src})
			compact := regexp.MustCompile(`<!--[^>]*-->| data-k[a-z-]*=[^ >]*`).ReplaceAllString(rootOf(t, html), "")
			if !strings.Contains(compact, c.want) {
				t.Errorf("want %s in:\n%s", c.want, compact)
			}
		})
	}
}

// A signal passed through a pattern the build keeps destructured would show
// its first value for good; the build says so.
func TestDestructuredLivePropWarns(t *testing.T) {
	_, logged := buildPlain(t, map[string]string{"src/pages/index.tsx": "import { createSignal } from '@krate/runtime';\n" + nestedRow +
		"export default function P() { const [n, setN] = createSignal(0); return <ul><Row item={{ id: 1 }} idx={n()} /><button onClick={() => setN(1)}>+</button></ul>; }\n"})
	if !strings.Contains(logged, "signal-derived prop(s) idx through a destructured parameter") {
		t.Errorf("want the destructured-parameter warning, got:\n%s", logged)
	}
}

func TestNestedParameterPatternHydrates(t *testing.T) {
	page := `import { createSignal } from "@krate/runtime";
function Row({ item: { id, tags: [first, ...rest] = [] } }) { const [c, setC] = createSignal(0); return <li class="out" data-id={id} onClick={() => setC(c() + 1)}>{first}:{rest.length}:{c()}</li>; }
export default function Page() { return <ul><Row item={{ id: 1, tags: ["x", "y", "z"] }} /></ul>; }
`
	evalFixBrowser(t, map[string]string{"src/pages/index.tsx": page}, []evalFixCheck{
		{Path: "/", Selector: "li.out", Hydrated: "x:2:0", Click: "li.out", AfterClick: strp("x:2:1")},
	})
}
