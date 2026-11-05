package sigutil

import (
	"reflect"
	"testing"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/lexer"
	"github.com/kratejs/krate/packages/compiler/internal/parser"
)

func body(t *testing.T, src string) []ast.Stmt {
	t.Helper()
	p := parser.New(lexer.New(src).Tokenize())
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse %q: %v", src, errs[0])
	}
	return prog.Body
}

func find(t *testing.T, src string, recurse bool) []Decl {
	t.Helper()
	return Find(body(t, src), recurse)
}

func names(ds []Decl) []string {
	var out []string
	for _, d := range ds {
		out = append(out, d.Name)
	}
	return out
}

func TestFindSignalsReducersAndResources(t *testing.T) {
	ds := find(t, `
const [count, setCount] = createSignal(0);
const [state, dispatch] = createReducer(reduce, { n: 1 });
const [data] = createResource(fetchIt);
`, false)
	if len(ds) != 3 {
		t.Fatalf("decls = %+v", ds)
	}
	if d := ds[0]; d.Name != "count" || d.Setter != "setCount" || d.Factory != "createSignal" || d.IsResource || d.CSSKind != CSSKindNone || len(d.Args) != 1 {
		t.Errorf("signal = %+v", d)
	}
	// The reducer's initial state is its second argument; the call is kept
	// whole so lowering can emit createReducer with the original reducer.
	if d := ds[1]; d.Factory != "createReducer" || d.Setter != "dispatch" || len(d.Args) != 2 {
		t.Errorf("reducer = %+v", d)
	} else if lit, ok := d.Initial.(*ast.ObjectExpr); !ok || lit == nil {
		t.Errorf("reducer initial = %T, want the object literal", d.Initial)
	}
	if d := ds[2]; d.Name != "data" || !d.IsResource || d.Setter != "" {
		t.Errorf("resource = %+v", d)
	}
}

func TestFindIgnoresDeclarationsItCannotLower(t *testing.T) {
	ds := find(t, `
const [a] = createSignal(0);
const [b, setB] = createSignal();
const [c, setC] = createReducer(reduce);
const [] = createResource(f);
const x = createSignal(1);
const [d, setD] = React.createSignal(1);
const [e, setE] = notASignal(1);
const [g, setG] = createSignal;
let h;
`, false)
	if len(ds) != 0 {
		t.Fatalf("expected nothing, got %v", names(ds))
	}
}

func TestFindRecursesOnlyWhenAsked(t *testing.T) {
	src := `
const [top, setTop] = createSignal(0);
if (cond) { const [inIf, setIf] = createSignal(1); } else { const [inElse, setElse] = createSignal(2); }
for (let i = 0; i < 2; i++) { const [inFor, setFor] = createSignal(3); }
while (x) { const [inWhile, setWhile] = createSignal(4); }
do { const [inDo, setDo] = createSignal(5); } while (x);
{ const [inBlock, setBlock] = createSignal(6); }
try { const [inTry, setTry] = createSignal(7); } catch (e) { const [inCatch, setCatch] = createSignal(8); } finally { const [inFinally, setFinally] = createSignal(9); }
switch (k) { case 1: const [inCase, setCase] = createSignal(10); }
`
	if got := names(find(t, src, false)); !reflect.DeepEqual(got, []string{"top"}) {
		t.Errorf("IR scope (no recursion) = %v, want only top-level declarations", got)
	}
	want := []string{"top", "inIf", "inElse", "inFor", "inWhile", "inDo", "inBlock", "inTry", "inCatch", "inFinally", "inCase"}
	if got := names(find(t, src, true)); !reflect.DeepEqual(got, want) {
		t.Errorf("annotator scope = %v, want %v", got, want)
	}
}

func TestFindCSSChoice(t *testing.T) {
	ds := find(t, `
const [plan, setPlan, ...rest] = createCSSChoice('free', { options: ['free', 'pro'], as: 'tabs', aria: 'structural', label: 'Plan', name: 'plan', disabled: ['pro'], anchor: 'bottom-start', transition: true });
const [legacy] = createCSSChoice('a', ['a', 'b', 3]);
const handle = createCSSChoice('x', ['x', 'y']);
`, false)
	if len(ds) != 3 {
		t.Fatalf("decls = %+v", ds)
	}
	d := ds[0]
	if d.CSSKind != CSSKindChoice || d.Name != "plan" || d.Setter != "setPlan" || d.RestName != "rest" {
		t.Errorf("choice = %+v", d)
	}
	if !reflect.DeepEqual(d.Options, []string{"free", "pro"}) || d.As != "tabs" || d.Aria != "structural" || d.Label != "Plan" ||
		d.FormName != "plan" || !reflect.DeepEqual(d.Disabled, []string{"pro"}) || d.Anchor != "bottom-start" || !d.Transition {
		t.Errorf("options not read: %+v", d)
	}
	// Names lists every destructured binding in order, the rest element included.
	if !reflect.DeepEqual(d.Names, []string{"plan", "setPlan", "rest"}) {
		t.Errorf("Names = %v", d.Names)
	}
	// A bare array is the option universe; numbers count as options.
	if !reflect.DeepEqual(ds[1].Options, []string{"a", "b", "3"}) {
		t.Errorf("legacy array options = %v", ds[1].Options)
	}
	// Handle form: no destructuring, no setter.
	if ds[2].Name != "handle" || ds[2].Setter != "" || !reflect.DeepEqual(ds[2].Names, []string{"handle"}) {
		t.Errorf("handle form = %+v", ds[2])
	}
}

func TestFindCSSToggleFlagsGroupRangeStack(t *testing.T) {
	ds := find(t, `
const [open, setOpen] = createCSSToggle(false, { label: 'Open' });
const [flags, setFlags] = createCSSFlags(['a', 'b'], { as: 'listbox' });
const [group, setGroup] = createCSSGroup(null, ['x', 'y']);
const [level] = createCSSRange(2, { min: 1, max: 5, step: 1 });
const [nav, push, pop] = createCSSStack(['home', 'docs', 'api']);
const [notEvenCalled] = createCSSChoice;
`, false)
	if len(ds) != 5 {
		t.Fatalf("decls = %v", names(ds))
	}
	by := map[string]Decl{}
	for _, d := range ds {
		by[d.Name] = d
	}
	if d := by["open"]; d.CSSKind != CSSKindToggle || !reflect.DeepEqual(d.Options, []string{"on"}) || d.Label != "Open" {
		t.Errorf("toggle = %+v", d)
	}
	if d := by["flags"]; d.CSSKind != CSSKindFlags || !reflect.DeepEqual(d.Options, []string{"a", "b"}) || d.As != "listbox" || d.Initial != nil {
		t.Errorf("flags = %+v", d)
	}
	if d := by["group"]; d.CSSKind != CSSKindGroup || !reflect.DeepEqual(d.Options, []string{"x", "y"}) {
		t.Errorf("group = %+v", d)
	}
	if d := by["level"]; d.CSSKind != CSSKindRange || d.Min != "1" || d.Max != "5" || d.Step != "1" {
		t.Errorf("range = %+v", d)
	}
	if d := by["nav"]; d.CSSKind != CSSKindStack || !reflect.DeepEqual(d.Options, []string{"home", "docs", "api"}) || !reflect.DeepEqual(d.Names, []string{"nav", "push", "pop"}) || d.Initial != nil {
		t.Errorf("stack = %+v", d)
	}
}

func TestFindAriaConfiguration(t *testing.T) {
	ds := find(t, `
const [a] = createCSSChoice('x', { aria: false });
const [b] = createCSSChoice('x', { aria: true });
const [c] = createCSSChoice('x', { aria: { role: 'tablist', 'aria-orientation': 'vertical', 'aria-multiselectable': true } });
const [d] = createCSSChoice('x', { aria: 'full' });
`, false)
	if got := ds[0].Aria; got != "off" {
		t.Errorf("aria:false = %q, want off", got)
	}
	if got := ds[1].Aria; got != "" {
		t.Errorf("aria:true = %q, want the default", got)
	}
	c := ds[2]
	if c.Aria != "full" || c.AriaAttrs["role"] != "tablist" || c.AriaAttrs["aria-orientation"] != "vertical" || c.AriaAttrs["aria-multiselectable"] != "true" {
		t.Errorf("aria object = %q %v", c.Aria, c.AriaAttrs)
	}
	if ds[3].Aria != "full" {
		t.Errorf("aria:'full' = %q", ds[3].Aria)
	}
}

func TestFindVars(t *testing.T) {
	ds := find(t, `
const [ok] = createCSSChoice('a', { options: ['a', 'b'], vars: { '--accent': { a: 'red', b: 'blue' } } });
const [none] = createCSSChoice('a', ['a']);
const [dyn] = createCSSChoice('a', { vars: { '--x': { a: color } } });
const [notObj] = createCSSChoice('a', { vars: 'nope' });
const [badProp] = createCSSChoice('a', { vars: { '--y': 'red' } });
`, false)
	if want := map[string]map[string]string{"--accent": {"a": "red", "b": "blue"}}; !reflect.DeepEqual(ds[0].Vars, want) || ds[0].VarErr != "" {
		t.Errorf("literal vars = %v / %q", ds[0].Vars, ds[0].VarErr)
	}
	if ds[1].Vars != nil || ds[1].VarErr != "" {
		t.Errorf("no vars = %v / %q", ds[1].Vars, ds[1].VarErr)
	}
	// Non-literal vars are reported by property so analysis can fail precisely.
	if ds[2].VarErr != "vars property --x option a must be a string literal" {
		t.Errorf("dynamic value err = %q", ds[2].VarErr)
	}
	if ds[3].VarErr != "vars must be an object of { '--property': { option: value } }" {
		t.Errorf("non-object err = %q", ds[3].VarErr)
	}
	if ds[4].VarErr != "vars property --y must map options to string literals" {
		t.Errorf("bad property err = %q", ds[4].VarErr)
	}
}

func TestFindNonLiteralOptionsYieldNoOptions(t *testing.T) {
	ds := find(t, `
const opts = ['a', 'b'];
const [a] = createCSSChoice('a', opts);
const [b] = createCSSChoice('a', ['a', dynamic]);
const [c] = createCSSChoice('a', { options: ['a', true] });
`, false)
	for _, d := range ds {
		if len(d.Options) != 0 {
			t.Errorf("%s: options = %v, want none (the caller must infer or reject)", d.Name, d.Options)
		}
	}
}
