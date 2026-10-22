package renderer

import (
	"regexp"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/ast"
)

// findFn returns the named top-level function declaration.
func findFn(prog *ast.Program, name string) *ast.FnDecl {
	for _, stmt := range prog.Body {
		switch s := stmt.(type) {
		case *ast.FnDecl:
			if s.Name == name {
				return s
			}
		case *ast.ExportStmt:
			if fn, ok := s.Declaration.(*ast.FnDecl); ok && fn.Name == name {
				return fn
			}
		}
	}
	return nil
}

// typedLocals are the bindings every typed-evaluation case can reference.
const typedLocals = `const count = 0; const q = "0"; const f = "false"; const nl = "null"; const nan = 0 / 0;
const items = [{n:"a",on:true,v:3},{n:"b",on:false,v:1},{n:"c",on:true,v:2}];
const name = "bob"; const n = null; const obj = {a:{b:"deep"}};`

// typedCases pairs a JSX child expression with the exact HTML React renders
// inside <div>...</div>.
var typedCases = []struct {
	name, expr, want string
}{
	{"false", `{false}`, ``},
	{"true", `{true}`, ``},
	{"null", `{null}`, ``},
	{"undefined", `{undefined}`, ``},
	{"zero and element", `{count && <b>x</b>}`, `0`},
	{"NaN and element", `{nan && <b>x</b>}`, `NaN`},
	{"string zero is truthy", `{q && <b>q</b>}`, `<b>q</b>`},
	{"string false is truthy", `{f && <b>f</b>}`, `<b>f</b>`},
	{"string null is truthy", `{nl && <b>n</b>}`, `<b>n</b>`},
	{"string concat", `{"5" + "3"}`, `53`},
	{"number plus string", `{5 + "3"}`, `53`},
	{"modulo is float", `{7 % 2.5}`, `2`},
	{"minus on strings is numeric", `{"10" - "4"}`, `6`},
	{"minus on non-number is NaN", `{"a" - 1}`, `NaN`},
	{"unary minus", `{-(5)}`, `-5`},
	{"negative zero", `{-count}`, `0`},
	{"negative arithmetic", `{-5 + 2}`, `-3`},
	{"typeof number", `{typeof count}`, `number`},
	{"typeof string", `{typeof q}`, `string`},
	{"typeof null", `{typeof n}`, `object`},
	{"NaN", `{0 / 0}`, `NaN`},
	{"Infinity", `{1 / 0}`, `Infinity`},
	{"exponent", `{1e21}`, `1e+21`},
	{"large integer", `{1e20}`, `100000000000000000000`},
	{"float precision", `{0.1 + 0.2}`, `0.30000000000000004`},
	{"small exponent", `{1e-7}`, `1e-7`},
	{"filter then map", `<ul>{items.filter(i => i.on).map(i => <li>{i.n}</li>)}</ul>`, `<ul><li>a</li><li>c</li></ul>`},
	{"find", `{items.find(i => i.v === 1).n}`, `b`},
	{"some every", `{String(items.some(i => !i.on))}-{String(items.every(i => i.on))}`, `true-false`},
	{"includes indexOf", `{items.map(i => i.n).includes("c") ? items.map(i => i.n).indexOf("c") : -1}`, `2`},
	{"join", `{items.map(i => i.n).join("-")}`, `a-b-c`},
	{"slice concat", `{items.slice(1).concat([{n:"d"}]).map(i => i.n).join("")}`, `bcd`},
	{"reverse", `{items.map(i => i.n).reverse().join("")}`, `cba`},
	{"length", `{items.length}`, `3`},
	{"object keys values entries", `{Object.keys(obj).join()}|{Object.values({x: 1, y: 2}).join()}|{Object.entries({k: "v"})[0].join("=")}`, `a|1,2|k=v`},
	{"string methods", `{name.toUpperCase()}|{"  hi ".trim()}|{"a,b".split(",").length}|{name.slice(1)}|{String(name.startsWith("b"))}|{String(name.endsWith("x"))}|{String(name.includes("o"))}|{"a-b".replace("-", "+")}|{"5".padStart(3, "0")}|{name.toLowerCase()}`, `BOB|hi|2|ob|true|false|true|a+b|005|bob`},
	{"template literal", "{`x${count}y${n}`}", `x0ynull`},
	{"optional chaining", `{obj?.a?.b}|{n?.x}|{obj.missing?.x.y}`, `deep||`},
	{"nullish coalescing", `{n ?? "dflt"}|{count ?? "dflt"}|{count || "or"}`, `dflt|0|or`},
	{"ternary", `{q ? "y" : "n"}|{count ? "y" : "n"}`, `y|n`},
	{"string comparison", `{"10" < "9" ? "y" : "n"}`, `y`},
	{"mixed comparison", `{"10" < 9 ? "y" : "n"}`, `n`},
	{"strict vs loose equality", `{String("1" == 1)}|{String("1" === 1)}|{String(null == undefined)}|{String(nan == nan)}`, `true|false|true|false`},
	{"array child", `{[1, 2, 3]}`, `123`},
	{"array with nullish", `{["a", null, false, "b"]}`, `ab`},
	{"strings are escaped", `{"<i>x</i>"}`, `&lt;i&gt;x&lt;/i&gt;`},
}

var commentRE = regexp.MustCompile(`<!--[^>]*-->`)

func divBody(t *testing.T, html string) string {
	t.Helper()
	html = commentRE.ReplaceAllString(html, "")
	start := strings.Index(html, "<div>")
	end := strings.LastIndex(html, "</div>")
	if start < 0 || end < start {
		t.Fatalf("no <div> in output: %s", html)
	}
	return html[start+len("<div>") : end]
}

// TestTypedValuesInPropComponent covers the SSREval path: a signal-less,
// prop-driven component rendered at build time.
func TestTypedValuesInPropComponent(t *testing.T) {
	for _, c := range typedCases {
		t.Run(c.name, func(t *testing.T) {
			src := "function C(props) {\n" + typedLocals + "\n  return <div>" + c.expr + "</div>;\n}\n" +
				`export default function Page() { return <C x="1" />; }`
			result, _ := fullPipeline(t, src)
			if len(result.Errors) > 0 {
				t.Fatalf("unexpected errors: %v", result.Errors)
			}
			if got := divBody(t, result.HTML); got != c.want {
				t.Errorf("%s\n got: %q\nwant: %q", c.expr, got, c.want)
			}
		})
	}
}

// TestTypedValuesInPageRoot covers the IR const-folding path: expressions in
// a page root built through the slot tree (hydration markers are stripped).
func TestTypedValuesInPageRoot(t *testing.T) {
	for _, c := range typedCases {
		t.Run(c.name, func(t *testing.T) {
			src := typedLocals + "\nexport default function Page() {\n  return <div>" + c.expr + "</div>;\n}"
			result, _ := fullPipeline(t, src)
			if len(result.Errors) > 0 {
				t.Fatalf("unexpected errors: %v", result.Errors)
			}
			if got := divBody(t, result.HTML); got != c.want {
				t.Errorf("%s\n got: %q\nwant: %q", c.expr, got, c.want)
			}
		})
	}
}

// TestPropStringIsNeverMarkup is the stored-XSS regression: a string prop that
// looks like markup must be escaped, because only JSX values are markup.
func TestPropStringIsNeverMarkup(t *testing.T) {
	cases := []string{
		`function Card(props) { return <h1>{props.title}</h1>; }
export default function Page() { return <Card title={"<img src=x onerror=alert(1)>"} />; }`,
		`function Card({ title }) { return <h1>{title}</h1>; }
export default function Page() { return <Card title="<img src=x onerror=alert(1)>" />; }`,
		`function Inner(props) { return <h1>{props.t}</h1>; }
function Card(props) { return <div><Inner t={props.title} /></div>; }
export default function Page() { return <Card title={"<img src=x onerror=alert(1)>"} />; }`,
	}
	for _, src := range cases {
		result, _ := fullPipeline(t, src)
		if strings.Contains(result.HTML, "<img") {
			t.Errorf("prop string injected as raw markup:\n%s", result.HTML)
		}
		if !strings.Contains(result.HTML, "&lt;img src=x onerror=alert(1)&gt;") {
			t.Errorf("expected escaped text, got:\n%s", result.HTML)
		}
	}
}

// TestPropJSXIsMarkup verifies a JSX value passed as a prop still renders as
// markup (the type, not the text, decides).
func TestPropJSXIsMarkup(t *testing.T) {
	src := `function Card(props) { return <h1>{props.icon}{props.label}</h1>; }
export default function Page() { return <Card icon={<i>i</i>} label="<b>" />; }`
	result, _ := fullPipeline(t, src)
	if !strings.Contains(result.HTML, "<h1><i>i</i>&lt;b&gt;</h1>") {
		t.Errorf("got:\n%s", result.HTML)
	}
}

// TestTypedPropValues verifies typed call-site props: the number 0 and the
// string "0" differ in truthiness.
func TestTypedPropValues(t *testing.T) {
	src := `function Card(props) { return <h1>{props.c && <b>x</b>}|{props.s && <i>s</i>}|{props.b && "no"}</h1>; }
export default function Page() { return <Card c={0} s="0" b={false} />; }`
	result, _ := fullPipeline(t, src)
	if !strings.Contains(result.HTML, "<h1>0|<i>s</i>|</h1>") {
		t.Errorf("got:\n%s", result.HTML)
	}
}

// TestSSREvalModuleConstsAndHelpers verifies module constants and module helper
// functions resolve inside build-time rendered components.
func TestSSREvalModuleConstsAndHelpers(t *testing.T) {
	src := `const SITE = "Krate"; const NUMS = [1, 2];
function fmt(x) { return x + "!"; }
function Card(props) { return <h1>{SITE}-{props.t}-{NUMS.length}-{fmt(props.t)}</h1>; }
export default function Page() { return <Card t="a" />; }`
	result, _ := fullPipeline(t, src)
	if len(result.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}
	if !strings.Contains(result.HTML, "<h1>Krate-a-2-a!</h1>") {
		t.Errorf("got:\n%s", result.HTML)
	}
}

// TestSSREvalDestructuredDefaults verifies destructuring defaults apply.
func TestSSREvalDestructuredDefaults(t *testing.T) {
	src := `function Badge({ label = "none", tone = "info" }) { return <span class={tone}>{label}</span>; }
export default function Page() { return <div><Badge /><Badge label="x" tone="warn" /></div>; }`
	result, _ := fullPipeline(t, src)
	if !strings.Contains(result.HTML, `<span class="info">none</span><span class="warn">x</span>`) {
		t.Errorf("got:\n%s", result.HTML)
	}
}

// TestSSREvalUnknownValueIsDiagnosed verifies a value that cannot be known at
// build time fails the build instead of rendering a silent guess.
func TestSSREvalUnknownValueIsDiagnosed(t *testing.T) {
	src := `function Card(props) { return <h1>{someRuntimeGlobal.title}</h1>; }
export default function Page() { return <Card t="a" />; }`
	result, _ := fullPipeline(t, src)
	if len(result.Errors) == 0 {
		t.Fatalf("expected a diagnostic, got HTML:\n%s", result.HTML)
	}
	if !strings.Contains(result.Errors[0].Error(), "someRuntimeGlobal") {
		t.Errorf("diagnostic should name the unresolved identifier: %v", result.Errors[0])
	}
}

// TestSSREvalUnsupportedConstructIsDiagnosed verifies unsupported constructs
// (regular expressions here) are reported.
func TestSSREvalUnsupportedConstructIsDiagnosed(t *testing.T) {
	src := `function Card(props) { return <h1>{props.t.replace(/a/g, "b")}</h1>; }
export default function Page() { return <Card t="a" />; }`
	result, _ := fullPipeline(t, src)
	if len(result.Errors) == 0 || !strings.Contains(result.Errors[0].Error(), "unsupported expression") {
		t.Fatalf("expected an unsupported-expression diagnostic, got %v / %s", result.Errors, result.HTML)
	}
}

// TestSSREvalUnknownComponentIsDiagnosed verifies an unresolvable component
// is reported instead of silently rendering nothing.
func TestSSREvalUnknownComponentIsDiagnosed(t *testing.T) {
	eval := NewSSREval(nil)
	fn := findFn(parseProg(t, `function F() { return <div><Missing /></div>; }`), "F")
	eval.EvalComponentBody(fn)
	if len(eval.Errors()) == 0 || !strings.Contains(eval.Errors()[0].Error(), "Missing") {
		t.Fatalf("expected an unknown-component diagnostic, got %v", eval.Errors())
	}
}

// TestSSREvalDeepNestingIsDiagnosed verifies the recursion guard reports
// instead of silently truncating output.
func TestSSREvalDeepNestingIsDiagnosed(t *testing.T) {
	prog := parseProg(t, `function R(props) { return <div><R /></div>; }`)
	fn := findFn(prog, "R")
	eval := NewSSREval(map[string]*ast.FnDecl{"R": fn})
	eval.EvalComponentBody(fn)
	if len(eval.Errors()) == 0 || !strings.Contains(eval.Errors()[0].Error(), "nests deeper") {
		t.Fatalf("expected a nesting-depth diagnostic, got %v", eval.Errors())
	}
}

// TestSSREvalDeepButFiniteNesting verifies realistic nesting (well past the old
// limit of 10) renders completely.
func TestSSREvalDeepButFiniteNesting(t *testing.T) {
	prog := parseProg(t, `function R(props) { return props.n > 0 ? <i><R n={props.n - 1} /></i> : <b>end</b>; }`)
	fn := findFn(prog, "R")
	eval := NewSSREval(map[string]*ast.FnDecl{"R": fn})
	el := parseProg(t, `function W() { return <R n={30} />; }`)
	html := eval.EvalComponentBody(findFn(el, "W"))
	if len(eval.Errors()) > 0 {
		t.Fatalf("unexpected errors: %v", eval.Errors())
	}
	if strings.Count(html, "<i>") != 30 || !strings.Contains(html, "<b>end</b>") {
		t.Errorf("got %s", html)
	}
}
