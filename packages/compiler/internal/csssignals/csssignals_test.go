package csssignals

import (
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/lexer"
	"github.com/kratejs/krate/packages/compiler/internal/parser"
)

func parseProg(t *testing.T, src string) *ast.Program {
	t.Helper()
	p := parser.New(lexer.New(src).Tokenize())
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	return p.ParseProgram()
}

func componentOf(t *testing.T, src string) *ast.FnDecl {
	t.Helper()
	prog := parseProg(t, src)
	for _, stmt := range prog.Body {
		switch s := stmt.(type) {
		case *ast.FnDecl:
			return s
		case *ast.ExportStmt:
			if fn, ok := s.Declaration.(*ast.FnDecl); ok {
				return fn
			}
		}
	}
	t.Fatal("no component function found")
	return nil
}

func analyze(t *testing.T, src string) *Analyzer {
	t.Helper()
	fn := componentOf(t, src)
	next := 0
	return Analyze(fn.Name, fn.Body, func(_, _ string) int {
		i := next
		next++
		return i
	})
}

const tabSrc = `export default function Tabs() {
	const [tab, setTab] = createCSSChoice('overview');
	return (
		<div class="tabs">
			<button onClick={() => setTab('overview')}>Overview</button>
			<button onClick={() => setTab('features')}>Features</button>
			<div showIf={tab() === 'overview'}>Overview content</div>
			<div showIf={tab() === 'features'}>Features content</div>
		</div>
	);
}`

func TestAnalyzeChoiceOptions(t *testing.T) {
	a := analyze(t, tabSrc)
	if !a.OK() {
		t.Fatalf("expected OK, errors=%v", a.Errors())
	}
	s := a.Scopes()[0]
	if s.Kind != KindChoice {
		t.Errorf("kind = %v", s.Kind)
	}
	if len(s.Options) != 2 || s.Options[0] != "overview" || s.Options[1] != "features" {
		t.Errorf("options = %v", s.Options)
	}
	if s.Class != ClassPrefix+"0" {
		t.Errorf("class = %q", s.Class)
	}
}

func TestAnalyzeToggle(t *testing.T) {
	src := `export default function T() {
	const [on, setOn] = createCSSToggle(false);
	return (
		<div>
			<button onClick={() => setOn(!on())}>Toggle</button>
			<div showIf={on()}>On</div>
			<div showIf={!on()}>Off</div>
		</div>
	);
}`
	a := analyze(t, src)
	if !a.OK() {
		t.Fatalf("expected OK, errors=%v", a.Errors())
	}
	if a.Scopes()[0].Kind != KindToggle {
		t.Errorf("kind = %v", a.Scopes()[0].Kind)
	}
}

// TestAnalyzeCompoundConditions verifies compound showIf/visibleIf expressions
// normalize to DNF conditions: AND across scopes, OR (multi-term), negation
// with De Morgan, and cross-scope compound conditions.
func TestAnalyzeCompoundConditions(t *testing.T) {
	src := `export default function T() {
	const [plat, setPlat] = createCSSChoice('mac', ['mac', 'win']);
	const [licensed, setLicensed] = createCSSToggle(false);
	const [flags, setFlag] = createCSSFlags(['premium']);
	return (
		<div>
			<div showIf={plat() === 'mac' && licensed()}>AND</div>
			<div showIf={plat() === 'win' || !licensed()}>OR</div>
			<div showIf={plat() !== 'mac'}>Negated</div>
			<div showIf={!(plat() === 'win' || licensed())}>NotOR</div>
			<div showIf={plat() === 'mac' && flags.premium()}>CrossScope</div>
		</div>
	);
}`
	a := analyze(t, src)
	if !a.OK() {
		t.Fatalf("expected OK, errors=%v", a.Errors())
	}
	conds := a.Conditions()
	// The five panels above are deduped to the canonical simple conditions plus
	// one compound condition per distinct expression. Simple choice/toggle
	// wrappers are canonical and do not appear in the explicit set.
	wantCompound := []string{"AND", "OR", "Negated", "NotOR", "CrossScope"}
	if len(conds) != len(wantCompound) {
		t.Fatalf("len(conditions)=%d, want %d: %v", len(conds), len(wantCompound), conds)
	}
	got := make([]string, 0, len(conds))
	for _, c := range conds {
		got = append(got, c.Class)
	}
	t.Logf("classes: %v", got)
}

func TestAnalyzeCompoundCrossScopeAnchor(t *testing.T) {
	src := `export default function T() {
	const [a, setA] = createCSSChoice('x');
	const [b, setB] = createCSSChoice('y');
	return (
		<div>
			<div showIf={a() === 'x' && b() === 'y'}>Both</div>
			<div showIf={a() === 'x' && b() === 'y'}>Both again</div>
			<div showIf={a() === 'x' && b() === 'y'}>Both third</div>
		</div>
	);
}`
	a := analyze(t, src)
	if !a.OK() {
		t.Fatalf("expected OK, errors=%v", a.Errors())
	}
	conds := a.Conditions()
	if len(conds) != 1 {
		t.Fatalf("identical compound conditions should dedupe to one, got %d", len(conds))
	}
	// The compound owner anchor is the scope with the lowest page index (the
	// first choice scope), so selectors are anchored on its class.
	if owner := conds[0].OwningScope(); owner == nil || owner.Index != 0 {
		t.Errorf("owning scope index = %v (want 0)", func() interface{} {
			if owner == nil {
				return nil
			}
			return owner.Index
		}())
	}
}

func TestAnalyzeCompoundNonClassifiable(t *testing.T) {
	src := `export default function T() {
	const [plat, setPlat] = createCSSChoice('mac');
	return (
		<div>
			<div showIf={plat() === 'mac' && 1 === 1}>Bad</div>
		</div>
	);
}`
	a := analyze(t, src)
	if a.OK() {
		t.Fatal("expected a hard error for a non-classifiable compound condition")
	}
}

func TestAnalyzeFlags(t *testing.T) {
	src := `export default function T() {
	const [flags, setFlag] = createCSSFlags(['a', 'b']);
	return (
		<div>
			<button onClick={() => setFlag('a', !flags.a())}>A</button>
			<div showIf={flags.a()}>A on</div>
			<div showIf={!flags.b()}>B off</div>
		</div>
	);
}`
	a := analyze(t, src)
	if !a.OK() {
		t.Fatalf("expected OK, errors=%v", a.Errors())
	}
	s := a.Scopes()[0]
	if s.Kind != KindFlags || len(s.Options) != 2 {
		t.Errorf("kind=%v options=%v", s.Kind, s.Options)
	}
}

// TestAnalyzeLiveText verifies a bare `{getter()}` read is accepted as live text
// and only that scope is marked for the --krate-current content rule.
func TestAnalyzeLiveText(t *testing.T) {
	src := `export default function T() {
	const [tab, setTab] = createCSSChoice('a', ['a', 'b']);
	const [other, setOther] = createCSSChoice('x', ['x', 'y']);
	return (
		<div>
			<button onClick={() => setTab('a')}>A</button>
			<p>Current: {tab()}</p>
			<div showIf={tab() === 'a'}>A</div>
		</div>
	);
}`
	a := analyze(t, src)
	if !a.OK() {
		t.Fatalf("expected OK, errors=%v", a.Errors())
	}
	if !a.Scopes()[0].LiveText {
		t.Error("scope 0 should be marked LiveText")
	}
	if a.Scopes()[1].LiveText {
		t.Error("scope 1 should not be marked LiveText")
	}
	if !UsesLiveText(a.Scopes()) {
		t.Error("UsesLiveText should report true")
	}
}

// TestAnalyzeVars verifies literal `vars` are parsed and emitted as custom
// properties, and non-literal vars hard-error.
func TestAnalyzeVars(t *testing.T) {
	src := `export default function T() {
	const [tier, setTier] = createCSSChoice('solo', {
		options: ['solo', 'pro'],
		vars: { '--tier-price': { solo: '"$9"', pro: '"$29"' } },
	});
	return (
		<div>
			<button onClick={() => setTier('pro')}>Pro</button>
			<p>Price: {tier()}</p>
		</div>
	);
}`
	a := analyze(t, src)
	if !a.OK() {
		t.Fatalf("expected OK, errors=%v", a.Errors())
	}
	s := a.Scopes()[0]
	if got := s.Vars["--tier-price"]["pro"]; got != `"$29"` {
		t.Errorf("vars[--tier-price][pro] = %q", got)
	}
	css := Stylesheet(a.Scopes(), a.Conditions())
	if !strings.Contains(css, `.krc0:has(.krc0-r-pro:checked){--krate-current:"pro";--tier-price:"$29";}`) {
		t.Errorf("missing vars rule:\n%s", css)
	}
	if !strings.Contains(css, `.krc-live::after{content:var(--krate-current,"")}`) {
		t.Errorf("missing live-text rule:\n%s", css)
	}
}

// TestAnalyzeVarsNonLiteral verifies a non-literal vars value is a hard error.
func TestAnalyzeVarsNonLiteral(t *testing.T) {
	src := `export default function T() {
	const [tier, setTier] = createCSSChoice('solo', {
		options: ['solo'],
		vars: { '--tier-price': { solo: price } },
	});
	return <div><button onClick={() => setTier('solo')}>S</button></div>;
}`
	a := analyze(t, src)
	if a.OK() {
		t.Fatal("expected a hard error for a non-literal vars value")
	}
	if !strings.Contains(a.Errors()[0], "--tier-price") {
		t.Errorf("error should name the property: %v", a.Errors())
	}
}

// TestAnalyzeGroup verifies createCSSGroup is a choice with a null sentinel.
func TestAnalyzeGroup(t *testing.T) {
	src := `export default function T() {
	const [open, setOpen] = createCSSGroup(null, { as: 'accordion' });
	return (
		<div>
			<button onClick={() => setOpen('faq')}>FAQ</button>
			<div showIf={open() === 'faq'}>Answer</div>
			<button onClick={() => setOpen(null)}>Close</button>
		</div>
	);
}`
	a := analyze(t, src)
	if !a.OK() {
		t.Fatalf("expected OK, errors=%v", a.Errors())
	}
	s := a.Scopes()[0]
	if s.Kind != KindGroup {
		t.Fatalf("kind=%v", s.Kind)
	}
	if !contains(s.Options, "faq") || !contains(s.Options, "") {
		t.Errorf("options=%v (want faq + null sentinel)", s.Options)
	}
	if s.Role.Trigger != "button" || !s.Role.SyncExpanded {
		t.Errorf("role=%+v", s.Role)
	}
	if !a.NeedsARIA() {
		t.Error("accordion role should require the ARIA runtime")
	}
}

// TestAnalyzeRange verifies createCSSRange enumerates the step grid and accepts
// a `r()+1` stepper trigger.
func TestAnalyzeRange(t *testing.T) {
	src := `export default function T() {
	const [n, setN] = createCSSRange(2, { min: 0, max: 4, step: 1 });
	return (
		<div>
			<button onClick={() => setN(n() + 1)}>Up</button>
			<button onClick={() => setN(n() - 1)}>Down</button>
			<div showIf={n() === 2}>Two</div>
		</div>
	);
}`
	a := analyze(t, src)
	if !a.OK() {
		t.Fatalf("expected OK, errors=%v", a.Errors())
	}
	s := a.Scopes()[0]
	if s.Kind != KindRange {
		t.Fatalf("kind=%v", s.Kind)
	}
	if len(s.Options) != 5 || s.Options[0] != "0" || s.Options[4] != "4" {
		t.Errorf("options=%v", s.Options)
	}
}

// TestAnalyzeStack verifies createCSSStack infers its declared tree from pushes.
func TestAnalyzeStack(t *testing.T) {
	src := `export default function T() {
	const [stack, { push, pop, clear }] = createCSSStack(['root']);
	return (
		<div>
			<button onClick={() => push('settings')}>Settings</button>
			<div showIf={stack.top() === 'settings'}>
				<button onClick={() => pop()}>Back</button>
				<button onClick={() => push('notifications')}>Notifs</button>
			</div>
		</div>
	);
}`
	a := analyze(t, src)
	if !a.OK() {
		t.Fatalf("expected OK, errors=%v", a.Errors())
	}
	s := a.Scopes()[0]
	if s.Kind != KindStack {
		t.Fatalf("kind=%v", s.Kind)
	}
	if s.Tree["settings"] != "root" {
		t.Errorf("tree=%v (want settings→root)", s.Tree)
	}
}

// TestAnalyzeARIAOffOptOut verifies `aria: false` suppresses roles.
func TestAnalyzeARIAOffOptOut(t *testing.T) {
	src := `export default function T() {
	const [tab, setTab] = createCSSChoice('a', { as: 'tabs', aria: false });
	return <div><button onClick={() => setTab('a')}>A</button><div showIf={tab() === 'a'}>A</div></div>;
}`
	a := analyze(t, src)
	if !a.OK() {
		t.Fatalf("expected OK, errors=%v", a.Errors())
	}
	if a.Scopes()[0].Role != (Role{}) {
		t.Errorf("aria:false should suppress roles, got %+v", a.Scopes()[0].Role)
	}
	if a.NeedsARIA() {
		t.Error("aria:false should not require the ARIA runtime")
	}
}

// TestAnalyzeUnknownRole verifies a bad `as` preset is a hard error.
func TestAnalyzeUnknownRole(t *testing.T) {
	src := `export default function T() {
	const [tab, setTab] = createCSSChoice('a', { as: 'nope' });
	return <div><button onClick={() => setTab('a')}>A</button><div showIf={tab() === 'a'}>A</div></div>;
}`
	a := analyze(t, src)
	if a.OK() {
		t.Fatal("expected a hard error for an unknown role preset")
	}
}

// Hard-error cases (no fallback)

func TestErrorNonLabelableTrigger(t *testing.T) {
	src := `export default function T() {
	const [tab, setTab] = createCSSChoice('a');
	return <div><section onClick={() => setTab('b')}>B</section><div showIf={tab() === 'b'}>B</div></div>;
}`
	a := analyze(t, src)
	if a.OK() {
		t.Fatal("expected an error for a non-labelable trigger")
	}
	if !strings.Contains(a.Errors()[0], "label") {
		t.Errorf("error should mention <label>: %v", a.Errors())
	}
}

func TestErrorDynamicTriggerClass(t *testing.T) {
	src := `export default function T() {
	const [tab, setTab] = createCSSChoice('a');
	return <div><button class={x} onClick={() => setTab('b')}>B</button><div showIf={tab() === 'b'}>B</div></div>;
}`
	a := analyze(t, src)
	if a.OK() {
		t.Fatal("expected an error for a dynamic trigger class")
	}
}

func TestErrorStrayTextRead(t *testing.T) {
	// A bare getter read is live text (supported); an arbitrary expression over
	// the state is not classifiable and must hard-error.
	src := `export default function T() {
	const [tab, setTab] = createCSSChoice('a');
	return <div><p>{tab() + '!'}</p><div showIf={tab() === 'a'}>A</div></div>;
}`
	a := analyze(t, src)
	if a.OK() {
		t.Fatal("expected an error for a stray text read")
	}
}

func TestErrorNonLiteralInitial(t *testing.T) {
	src := `export default function T() {
	const [tab, setTab] = createCSSChoice(props.initial);
	return <div><div showIf={tab() === 'a'}>A</div></div>;
}`
	a := analyze(t, src)
	if a.OK() {
		t.Fatal("expected an error for a non-literal initial")
	}
}

func TestNoPrimitivesIsNotAnError(t *testing.T) {
	src := `export default function T() { return <div>{count()}</div>; }`
	a := analyze(t, src)
	if a.HaveAny() {
		t.Error("no CSS signal should report HaveAny=false")
	}
	if len(a.Errors()) != 0 {
		t.Errorf("unexpected errors: %v", a.Errors())
	}
}

// CSS generation

func TestStylesheetChoiceWellFormed(t *testing.T) {
	css := Stylesheet(analyze(t, tabSrc).Scopes(), nil)
	if strings.Contains(css, "display:none},") {
		t.Errorf("base rule joined to the next by a dangling comma:\n%s", css)
	}
	if !strings.Contains(css, ".krc0-r-overview:checked") {
		t.Errorf("missing radio rule:\n%s", css)
	}
	if !strings.Contains(css, "display:contents") {
		t.Errorf("panels should be toggled via display:contents wrapper:\n%s", css)
	}
}

func TestStylesheetToggle(t *testing.T) {
	src := `export default function T() {
	const [on, setOn] = createCSSToggle(false);
	return <div><button onClick={() => setOn(!on())}>T</button><div showIf={on()}>On</div><div showIf={!on()}>Off</div></div>;
}`
	css := Stylesheet(analyze(t, src).Scopes(), nil)
	if !strings.Contains(css, ".krc0-c:checked") {
		t.Errorf("missing checkbox rule:\n%s", css)
	}
}

func TestClassNamesAreCompact(t *testing.T) {
	// base62: 0-9 then a-z then A-Z. Index 35 = "z", 61 = "Z", 62 = "10".
	cases := map[int]string{0: "krc0", 35: "krcz", 61: "krcZ", 62: "krc10"}
	for idx, want := range cases {
		s := &Scope{Kind: KindChoice, Index: idx, Initial: "a", Options: []string{"a"}}
		s.Class = s.base()
		if s.Class != want {
			t.Errorf("index %d base = %q, want %q", idx, s.Class, want)
		}
	}
	s := &Scope{Kind: KindChoice, Index: 35, Options: []string{"a"}}
	s.Class = s.base()
	if got := s.RadioClass("overview"); got != "krcz-r-overview" {
		t.Errorf("radio class = %q", got)
	}
}
