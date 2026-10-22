package build

import (
	"regexp"
	"strings"
	"testing"
)

var rootDivRe = regexp.MustCompile(`(?s)<div id="root">(.*)</div>`)

func rootHTML(html string) string {
	if m := rootDivRe.FindStringSubmatch(html); m != nil {
		return m[1]
	}
	return html
}

// TestSpreadPropsOnComponentRoot covers `{...props}` and `{...rest}` spreads on
// a component's root element, for build-time evaluated and client components,
// with React's override order.
func TestSpreadPropsOnComponentRoot(t *testing.T) {
	cases := []struct {
		name, page, want string
	}{
		{
			"whole props object",
			`function Box(props: any) { return <div {...props}>x</div>; }
			export default function P() {
				const p = { id: "i", className: "c", "data-x": "1" };
				return <Box {...p} title="t" />;
			}`,
			`<div class="c" data-x="1" id="i" title="t">x</div>`,
		},
		{
			"bare boolean attribute",
			`function Btn(props: any) { return <button {...props}>x</button>; }
			export default function P() { return <main><Btn id="a" disabled /></main>; }`,
			`<main><button disabled id="a">x</button></main>`,
		},
		{
			"rest after destructuring",
			`function Btn({ label, ...rest }: any) { return <button {...rest}>{label}</button>; }
			export default function P() { return <main><Btn label="L" id="a" className="c" /></main>; }`,
			`<main><button class="c" id="a">L</button></main>`,
		},
		{
			"spread before and after an explicit attribute",
			`function A(props: any) { return <div id="x" {...props}>a</div>; }
			function B(props: any) { return <div {...props} id="x">b</div>; }
			export default function P() { return <main><A id="y" /><B id="y" /></main>; }`,
			`<main><div id="y">a</div><div id="x">b</div></main>`,
		},
		{
			"spread forwards children",
			`function Box(props: any) { return <section {...props} />; }
			export default function P() { return <main><Box id="q"><b>kid</b></Box></main>; }`,
			`<main><section id="q"><b>kid</b></section></main>`,
		},
		{
			"local object spread overridden by a later attribute",
			`export default function P() {
				const p = { id: "i", title: "t", hidden: false };
				return <main><div {...p} title="u">x</div></main>;
			}`,
			`<main><div id="i" title="u">x</div></main>`,
		},
		{
			"client component whole props",
			`function Btn(props: any) {
				const [n, setN] = createSignal(0);
				return <button {...props} onClick={() => setN(n() + 1)}>{n()}</button>;
			}
			export default function P() { return <main><Btn id="a" disabled title="t" /></main>; }`,
			`<main><button data-k="k:2" disabled id="a" title="t"><!--k:3-->0<!--/k:3--></button></main>`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			html, _ := buildReactPage(t, tc.page)
			if got := rootHTML(html); got != tc.want {
				t.Fatalf("got  %s\nwant %s", got, tc.want)
			}
		})
	}
}

// TestSpreadPropsForwardedValuesReadProps checks that values forwarded through
// a spread are read from the component's props registry at hydration, never
// through the parent's bindings (which are out of scope in the child).
func TestSpreadPropsForwardedValuesReadProps(t *testing.T) {
	page := `
		function Btn(props: any) {
			const [n, setN] = createSignal(0);
			return <button {...props}>{n()}</button>;
		}
		export default function P() {
			const [c, setC] = createSignal(0);
			function inc() { setC(c() + 1); }
			return <main><Btn id="a" onClick={inc} title={String(c())} /><i>{c()}</i></main>;
		}
	`
	html, js := buildReactPage(t, page)
	if !strings.Contains(html, `title="0"`) {
		t.Fatalf("forwarded title missing from SSR:\n%s", rootHTML(html))
	}
	child := js[strings.Index(js, "// component 1.Btn"):]
	if !strings.Contains(child, `var props=__krate_props["1.Btn.Btn_c0"];`) {
		t.Fatalf("child does not bind its props registry:\n%s", js)
	}
	if !strings.Contains(child, `kbindHandler("2","__krate_click_2",props.onClick);`) {
		t.Fatalf("forwarded handler not read from props:\n%s", js)
	}
	if strings.Contains(child, "inc") || strings.Contains(child, "c()") {
		t.Fatalf("child references parent bindings:\n%s", child)
	}
}

// TestSpreadMutatedConstObjectNotFlattened checks that a local object changed
// before render is not flattened from its literal.
func TestSpreadMutatedConstObjectNotFlattened(t *testing.T) {
	page := `
		export default function P() {
			const p: any = { id: "i" };
			p.title = "t";
			return <main><div {...p}>x</div></main>;
		}
	`
	html, _ := buildReactPage(t, page)
	if got := rootHTML(html); got != `<main><div id="i" title="t">x</div></main>` {
		t.Fatalf("got %s", got)
	}
}
