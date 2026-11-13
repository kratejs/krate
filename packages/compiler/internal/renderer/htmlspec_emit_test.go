package renderer

import (
	"strings"
	"testing"
)

// pageBody renders a page whose root returns `jsx` (IR tree path).
func pageBody(t *testing.T, jsx string) string {
	t.Helper()
	res, _ := fullPipeline(t, "export default function Page() {\n  return "+jsx+";\n}")
	return res.HTML
}

// evalBody renders `jsx` inside a prop-driven (SSREval) component so both
// serializer paths are covered by the same expectations.
func evalBody(t *testing.T, jsx string) string {
	t.Helper()
	src := "function Box(props) {\n  return " + jsx + ";\n}\nexport default function Page() {\n  return <section><Box v=\"1\" /></section>;\n}"
	res, _ := fullPipeline(t, src)
	h := strings.TrimPrefix(res.HTML, "<section>")
	return strings.TrimSuffix(h, "</section>")
}

func TestSerializerExactHTML(t *testing.T) {
	cases := []struct {
		name, jsx, want string
	}{
		{"boolean true bare", `<input disabled={true} />`, `<input disabled />`},
		{"boolean bare", `<input disabled />`, `<input disabled />`},
		{"boolean false omitted", `<input disabled={false} />`, `<input />`},
		{"boolean string false is truthy", `<input disabled="false" />`, `<input disabled />`},
		{"readOnly alias", `<input readOnly />`, `<input readonly />`},
		{"defaultChecked", `<input type="checkbox" defaultChecked />`, `<input type="checkbox" checked />`},
		{"defaultValue on input", `<input defaultValue="x" />`, `<input value="x" />`},
		{"hidden until-found", `<div hidden="until-found">x</div>`, `<div hidden="until-found">x</div>`},
		{"translate enumerated", `<p translate="no">x</p>`, `<p translate="no">x</p>`},
		{"draggable booleanish", `<div draggable={true} spellCheck={false}>x</div>`, `<div draggable="true" spellcheck="false">x</div>`},
		{"contentEditable bare", `<div contentEditable>x</div>`, `<div contenteditable="true">x</div>`},
		{"aria booleans stringify", `<b aria-hidden={true} aria-expanded={false}>x</b>`, `<b aria-hidden="true" aria-expanded="false">x</b>`},
		{"null and false omitted", `<div title={null} id={undefined} lang={false}>x</div>`, `<div>x</div>`},
		{"svg names", `<svg viewBox="0 0 1 1" xmlnsXlink="http://www.w3.org/1999/xlink"><use xlinkHref="#a" strokeWidth={2} markerHeight="3" xmlSpace="preserve" /></svg>`,
			`<svg viewBox="0 0 1 1" xmlns:xlink="http://www.w3.org/1999/xlink"><use xlink:href="#a" stroke-width="2" markerHeight="3" xml:space="preserve"></use></svg>`},
		{"style object", `<div style={{fontSize: 12, marginTop: 0, lineHeight: 1.5, "--x": 1, WebkitTransition: "all", msTransform: "none", color: null}}>x</div>`,
			`<div style="font-size:12px;margin-top:0;line-height:1.5;--x:1;-webkit-transition:all;-ms-transform:none">x</div>`},
		{"empty style omitted", `<div style={{}}>x</div>`, `<div>x</div>`},
		{"textarea value", `<textarea value="hi" readOnly />`, `<textarea readonly>hi</textarea>`},
		{"textarea defaultValue escaped", `<textarea defaultValue="a<b" />`, `<textarea>a&lt;b</textarea>`},
		{"select value", `<select value="b"><option value="a">A</option><option value="b">B</option></select>`,
			`<select><option value="a">A</option><option value="b" selected>B</option></select>`},
		{"select multiple defaultValue", `<select multiple defaultValue={["a", "c"]}><option value="a">A</option><option value="b">B</option><option value="c">C</option></select>`,
			`<select multiple><option value="a" selected>A</option><option value="b">B</option><option value="c" selected>C</option></select>`},
		{"option text value", `<select defaultValue="Two"><option>One</option><option>Two</option></select>`,
			`<select><option>One</option><option selected>Two</option></select>`},
		{"javascript href", `<a href="javascript:alert(1)">x</a>`,
			`<a href="javascript:throw new Error(&#39;React has blocked a javascript: URL as a security precaution.&#39;)">x</a>`},
		{"obfuscated javascript src", "<iframe src={\" \\tJaVa\\nScRiPt:alert(1)\"}></iframe>",
			`<iframe src="javascript:throw new Error(&#39;React has blocked a javascript: URL as a security precaution.&#39;)"></iframe>`},
		{"empty src omitted", `<img src="" alt="" />`, `<img alt="" />`},
		{"text is escaped", `<p>Use &lt;script&gt;alert(1)&lt;/script&gt;</p>`, `<p>Use &lt;script&gt;alert(1)&lt;/script&gt;</p>`},
		{"pre leading newline", `<pre>{"\nx"}</pre>`, "<pre>\n\nx</pre>"},
		{"void with closing tag", `<p>a<br></br>b</p>`, `<p>a<br />b</p>`},
		{"inline svg style stays", `<svg><style>{".a{fill:red}"}</style></svg>`, `<svg><style>.a{fill:red}</style></svg>`},
		{"script closing neutralized", "<div><script>{\"var s='</script><b>'\"}</script></div>", `<div><script>var s='<\/script><b>'</script></div>`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := pageBody(t, tc.jsx); got != tc.want {
				t.Errorf("IR path:\n got: %s\nwant: %s", got, tc.want)
			}
			if got := evalBody(t, tc.jsx); got != tc.want {
				t.Errorf("SSREval path:\n got: %s\nwant: %s", got, tc.want)
			}
		})
	}
}
