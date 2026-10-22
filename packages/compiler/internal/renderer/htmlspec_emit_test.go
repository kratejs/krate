package renderer

import (
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/annotator"
	"github.com/kratejs/krate/packages/compiler/internal/config"
	"github.com/kratejs/krate/packages/compiler/internal/irtree"
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

func TestSerializerTextXSS(t *testing.T) {
	html := pageBody(t, `<p>Use &lt;script&gt;alert(1)&lt;/script&gt;</p>`)
	if strings.Contains(html, "<script>") {
		t.Fatalf("decoded entity text emitted a live <script>: %s", html)
	}
}

func markupErrors(t *testing.T, src string) []error {
	t.Helper()
	prog := parseProg(t, src)
	ann := annotator.Annotate(prog, &config.Config{}, "page.tsx", src)
	tree := irtree.Build(prog, ann)
	errs := append([]error(nil), tree.Errors...)
	errs = append(errs, NewEmitter().Emit(tree).Errors...)
	return errs
}

func TestSerializerNameDiagnostics(t *testing.T) {
	cases := []struct{ src, want string }{
		{`export default function Page() { return <div><br>x</br></div>; }`, "<br> is a void element and cannot have children"},
		{`export default function Page() { return <div><img>x</img></div>; }`, "<img> is a void element"},
	}
	for _, tc := range cases {
		errs := markupErrors(t, tc.src)
		found := false
		for _, e := range errs {
			if strings.Contains(e.Error(), tc.want) {
				found = true
			}
		}
		if !found {
			t.Errorf("%s: expected diagnostic %q, got %v", tc.src, tc.want, errs)
		}
	}
	// A signal-less component evaluated through SSREval reports too.
	errs := markupErrors(t, `function Box(props) { return <hr>{props.x}</hr>; }
export default function Page() { return <main><Box x="1" /></main>; }`)
	if len(errs) == 0 || !strings.Contains(errs[0].Error(), "<hr> is a void element") {
		t.Errorf("expected void-children diagnostic from SSREval path, got %v", errs)
	}
}

func TestMarkerEscaping(t *testing.T) {
	s := &irtree.ListSlot{ID: "L", Items: []*irtree.ListItem{{Key: "a-->b<script>x</script>"}}}
	out := NewEmitter().emitListSlot(s)
	if strings.Contains(out.HTML, "<script>") || strings.Count(out.HTML, "-->") != 4 {
		t.Fatalf("list key broke out of its comment marker: %s", out.HTML)
	}
	want := `<!--k:L--><!--k:L:a--%3Eb%3Cscript%3Ex%3C/script%3E--><!--/k:L:a--%3Eb%3Cscript%3Ex%3C/script%3E--><!--/k:L-->`
	if out.HTML != want {
		t.Errorf("got  %s\nwant %s", out.HTML, want)
	}
	sus := NewEmitter().emitSuspenseSlot(&irtree.SuspenseSlot{StreamID: "s-->x"})
	if sus.HTML != `<!--suspense:s--%3Ex--><!--/suspense:s--%3Ex-->` {
		t.Errorf("suspense stream id not encoded: %s", sus.HTML)
	}
	srv := NewEmitter().emitServer(&irtree.ComponentNode{Name: `X"><script>`})
	if strings.Contains(srv.HTML, "<script>") {
		t.Errorf("server component name not escaped: %s", srv.HTML)
	}
}

const condPage = `export default function Page() {
  const [on, setOn] = createSignal(true);
  return <div>
    <button onClick={() => setOn(!on())}>t</button>
    <table><tbody>{on() ? <tr><td>yes</td></tr> : <tr><td>no</td></tr>}</tbody></table>
    <ul>{on() ? <li>a</li> : <li>b</li>}</ul>
    <select>{on() ? <option>x</option> : <option>y</option>}</select>
    <p>{on() ? <b>bold</b> : <i>it</i>}</p>
  </div>;
}`

func TestConditionalCommentMarkers(t *testing.T) {
	res, js := fullPipeline(t, condPage)
	html := res.HTML
	if strings.Contains(html, "data-krate-cond-w") || strings.Contains(html, "display:none") {
		t.Fatalf("conditional still renders wrapper divs:\n%s", html)
	}
	for _, want := range []string{
		`<tbody><!--k:`, `--><tr><td>yes</td></tr><!--/k:`, `--></tbody>`,
		`<ul><!--k:`, `--><li>a</li><!--/k:`,
		`<select><!--k:`, `--><option>x</option><!--/k:`,
		`<p><!--k:`, `--><b>bold</b><!--/k:`,
		`data-kb="a"><tr><td>no</td></tr></template>`,
		`data-kb="a"><li>b</li></template>`,
		`data-kb="a"><option>y</option></template>`,
		`data-kb="a"><i>it</i></template>`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("missing %q in:\n%s", want, html)
		}
	}
	// The inactive branches are hoisted after the page content.
	if idx := strings.Index(html, "<template"); idx < strings.LastIndex(html, "</div>") {
		t.Errorf("templates must be hoisted after the page content:\n%s", html)
	}
	if strings.Count(js, "kbindCond(") != 4 {
		t.Errorf("expected 4 kbindCond bindings, got:\n%s", js)
	}
}

func TestConditionalInitialFalseHoldsConsequent(t *testing.T) {
	res, _ := fullPipeline(t, `export default function Page() {
  const [on, setOn] = createSignal(false);
  return <div onClick={() => setOn(true)}>{on() ? <b>yes</b> : <i>no</i>}</div>;
}`)
	if !strings.Contains(res.HTML, `--><i>no</i><!--/k:`) || !strings.Contains(res.HTML, `data-kb="c"><b>yes</b></template>`) {
		t.Errorf("unexpected output:\n%s", res.HTML)
	}
}

// TestEmitIsPureUnderParallelReuse emits the same tree from several goroutines
// (each forcing the parallel sibling path) twice over and requires identical
// output. Run with -race: emit must never write to the shared IR.
func TestEmitIsPureUnderParallelReuse(t *testing.T) {
	src := `function RuntimeWidget(props) { return <div>{props.label}</div>; }
function Counter() {
  const [c, setC] = createSignal(0);
  return <button onClick={() => setC(c() + 1)}>{c() > 1 ? <b>many</b> : <i>few</i>}</button>;
}
export default function Page() {
  return <div>
    <Counter /><Counter /><Counter /><Counter />
    <RuntimeWidget label="a" />
    <RuntimeWidget label="b" />
    <Counter /><Counter /><Counter />
  </div>;
}`
	prog := parseProg(t, src)
	tree := irtree.Build(prog, annotateWithRuntime(prog, src))
	old := parallelMinChildren
	parallelMinChildren = 2
	defer func() { parallelMinChildren = old }()

	first := NewEmitter().Emit(tree).HTML
	var wg sync.WaitGroup
	outs := make([]string, 8)
	for i := range outs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			outs[i] = NewEmitter().Emit(tree).HTML
		}(i)
	}
	wg.Wait()
	for i, o := range outs {
		if o != first {
			t.Fatalf("emit %d differs:\n%s\n---\n%s", i, o, first)
		}
	}
}

func TestGoPropsJSONIsValidJSON(t *testing.T) {
	got := goPropsJSON(map[string]string{"b": `{"x": 1}`, "a": `not json`, "c": `"s"`}, `<p class="q">"</p>`)
	var decoded map[string]any
	if err := json.Unmarshal([]byte(got), &decoded); err != nil {
		t.Fatalf("invalid JSON %s: %v", got, err)
	}
	if decoded["a"] != "not json" || decoded["c"] != "s" || decoded["children"] != `<p class="q">"</p>` {
		t.Errorf("unexpected decode: %v", decoded)
	}
	if b, ok := decoded["b"].(map[string]any); !ok || b["x"] != float64(1) {
		t.Errorf("object prop not kept as JSON: %v", decoded["b"])
	}
	if !strings.HasPrefix(got, `{"a":`) {
		t.Errorf("keys not sorted: %s", got)
	}
}

func TestExtractJSONPropNested(t *testing.T) {
	cases := []struct{ obj, key, want string }{
		{`{"a":{"count":1},"count":2}`, "count", "2"},
		{`{"s":"count:9","count":3}`, "count", "3"},
		{`{a:{b:1},b:'two'}`, "b", "two"},
		{`{label:'x, y',n:[1,2]}`, "n", "[1,2]"},
		{`{"name":"hi \"there\""}`, "name", `hi "there"`},
		{`{"a":1}`, "missing", ""},
	}
	for _, c := range cases {
		if got := extractJSONProp(c.obj, c.key); got != c.want {
			t.Errorf("extractJSONProp(%s, %s) = %q, want %q", c.obj, c.key, got, c.want)
		}
	}
}

func TestMangleIdentLinear(t *testing.T) {
	if got := mangleIdentInString(`count + "count" + 'count' + `+"`${count}`", "count", "c0"); got != `c0 + "count" + 'count' + `+"`${c0}`" {
		t.Errorf("got %s", got)
	}
	big := strings.Repeat(`x + "s" + `, 20000)
	if got := mangleIdentInString(big, "x", "y"); strings.Count(got, "y") != 20000 {
		t.Errorf("unexpected replacement count")
	}
}
