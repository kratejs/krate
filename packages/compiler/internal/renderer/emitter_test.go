package renderer

import (
	"strings"
	"testing"

	"krate-compiler/internal/annotator"
	"krate-compiler/internal/ast"
	"krate-compiler/internal/config"
	"krate-compiler/internal/irtree"
)

func annotateWithRuntime(prog *ast.Program, raw string) *irtree.Annotations {
	return annotator.Annotate(prog, &config.Config{RuntimeComponents: []string{"RuntimeWidget"}}, "page.tsx", raw)
}

// ─── emitSSREvaluated ───────────────────────────────────────────────────────

func TestEmitSSREvaluatedWithPropBindings(t *testing.T) {
	src := `function Card(props) {
  var title = props.title || "Untitled";
  return <div class="card"><h2>{title}</h2><p>{props.body}</p></div>;
}
export default function Page() {
  return <div><Card title="Hello" body="World" /></div>;
}`
	result, _ := fullPipeline(t, src)
	if !strings.Contains(result.HTML, `<h2>Hello</h2>`) {
		t.Errorf("expected prop binding title=Hello in HTML, got:\n%s", result.HTML)
	}
	if !strings.Contains(result.HTML, `<p>World</p>`) {
		t.Errorf("expected prop binding body=World in HTML, got:\n%s", result.HTML)
	}
}

func TestEmitSSREvaluatedWithChildrenBinding(t *testing.T) {
	src := `function Wrapper(props) {
  return <div class="wrap">{props.children}</div>;
}
export default function Page() {
  return <div><Wrapper><span>inner</span></Wrapper></div>;
}`
	result, _ := fullPipeline(t, src)
	if !strings.Contains(result.HTML, `<div class="wrap"><span>inner</span></div>`) {
		t.Errorf("expected children injected into wrapper, got:\n%s", result.HTML)
	}
}

func TestEmitSSREvaluatedChildrenEscaped(t *testing.T) {
	src := "function Wrapper(props) {\n  return <div>{props.children}</div>;\n}\nexport default function Page() {\n  return <div><Wrapper>{`a < b`}</Wrapper></div>;\n}"
	result, _ := fullPipeline(t, src)
	if !strings.Contains(result.HTML, "a &lt; b") {
		t.Errorf("expected escaped children, got:\n%s", result.HTML)
	}
	if strings.Contains(result.HTML, "a < b") && !strings.Contains(result.HTML, "&lt;") {
		t.Errorf("children leaked raw < into HTML:\n%s", result.HTML)
	}
}

func TestEmitSSREvaluatedDateNow(t *testing.T) {
	src := `function ServerTime(props) {
  return <div>built {Date.now()}</div>;
}
export default function Page() {
  return <ServerTime />;
}`
	// ServerTime is signal-less → SSR-evaluated. Without the QuickJS hook,
	// Date.now() is an unsupported built-in → "".
	result, _ := fullPipeline(t, src)
	if strings.Contains(result.HTML, "built 1") && !strings.Contains(result.HTML, "Date.now") {
		t.Log("Date.now evaluated")
	}

	// With the hook wired (as the build does with QuickJS), the global is
	// delegated and evaluated to a real value.
	prog := parseProg(t, src)
	ann := annotator.Annotate(prog, &config.Config{}, "page.tsx", src)
	tree := irtree.Build(prog, ann)
	emitter := NewEmitter()
	emitter.EvalJS = func(code string) (string, error) {
		if code == "Date.now()" {
			return "1710000000000", nil
		}
		return "0", nil
	}
	result2 := emitter.Emit(tree)
	if !strings.Contains(result2.HTML, "built 1710000000000") {
		t.Errorf("expected delegated Date.now() value baked into HTML, got:\n%s", result2.HTML)
	}
}

func TestEmitSSREvaluatedDelegatesBuiltin(t *testing.T) {
	src := `function Widget(props) {
  return <div>round {Math.round(3.7)} max {Math.max(1, 5, 3)}</div>;
}
export default function Page() {
  return <Widget />;
}`
	prog := parseProg(t, src)
	ann := annotator.Annotate(prog, &config.Config{}, "page.tsx", src)
	tree := irtree.Build(prog, ann)
	var seen []string
	emitter := NewEmitter()
	emitter.EvalJS = func(code string) (string, error) {
		seen = append(seen, code)
		switch {
		case strings.Contains(code, "Math.round"):
			return "4", nil
		case strings.Contains(code, "Math.max"):
			return "5", nil
		}
		return "0", nil
	}
	result := emitter.Emit(tree)
	if !strings.Contains(result.HTML, "round 4") || !strings.Contains(result.HTML, "max 5") {
		t.Errorf("expected delegated built-in results, got:\n%s", result.HTML)
	}
	if len(seen) == 0 {
		t.Error("expected built-in expressions to be delegated to the JS evaluator")
	}
}

// ─── Meta slots ─────────────────────────────────────────────────────────────

func TestEmitMultipleMetaSlots(t *testing.T) {
	src := `export default function Page() {
  return <div>
    <Head><title>My Title</title><meta name="description" content="desc"/></Head>
    <Style>{` + "`" + `.a { color: red; }` + "`" + `}</Style>
    <Script>{` + "`" + `console.log('x')` + "`" + `}</Script>
  </div>;
}`
	result, _ := fullPipeline(t, src)
	if !strings.Contains(result.HeadHTML, `<title>My Title</title>`) {
		t.Errorf("expected title in HeadHTML, got %q", result.HeadHTML)
	}
	if !strings.Contains(result.HeadHTML, `content="desc"`) {
		t.Errorf("expected meta in HeadHTML, got %q", result.HeadHTML)
	}
	if !strings.Contains(result.StyleHTML, `.a { color: red; }`) {
		t.Errorf("expected style preserved raw, got %q", result.StyleHTML)
	}
	if !strings.Contains(result.ScriptHTML, `console.log('x')`) {
		t.Errorf("expected inline script preserved raw, got %q", result.ScriptHTML)
	}
}

func TestEmitMetaViaSSREvaluatedWrapper(t *testing.T) {
	// A signal-less wrapper's <Head>/<Script>/<Style> must still route to meta
	// output (collected through SSREval during emitSSREvaluated).
	src := `function DocShell(props) {
  return <html><Head><title>{props.title}</title></Head><body>{props.children}</body></html>;
}
export default function Page() {
  return <DocShell title="Doc"><div>content</div></DocShell>;
}`
	result, _ := fullPipeline(t, src)
	if !strings.Contains(result.HeadHTML, `Doc`) {
		t.Errorf("expected title prop resolved into HeadHTML, got %q", result.HeadHTML)
	}
	if !strings.Contains(result.HTML, "content") {
		t.Errorf("expected body content in HTML, got:\n%s", result.HTML)
	}
}

// ─── emitClient edge cases ──────────────────────────────────────────────────

func TestEmitClientCollectsChildComponentSignatures(t *testing.T) {
	src := `function Counter() {
  const [c, setC] = createSignal(0);
  return <button onClick={() => setC(c + 1)}>{c()}</button>;
}
export default function Page() {
  return <div><Counter /></div>;
}`
	result, _ := fullPipeline(t, src)
	// Page + Counter both produce client signatures.
	var counterSigs int
	for _, sig := range result.Signatures {
		if sig.Tier == irtree.TierClient && strings.Contains(string(sig.ComponentID), "Counter") {
			counterSigs++
		}
	}
	if counterSigs == 0 {
		t.Errorf("expected a Counter client signature, got %d signatures total", len(result.Signatures))
	}
}

func TestEmitClientCallSiteChildrenWithMarkers(t *testing.T) {
	src := `function Panel(props) {
  return <div class="panel">{props.children}</div>;
}
function Toggle() {
  const [on, setOn] = createSignal(false);
  return <button onClick={() => setOn(!on())}>{on() ? "On" : "Off"}</button>;
}
export default function Page() {
  return <div><Panel><Toggle /></Panel></div>;
}`
	result, _ := fullPipeline(t, src)
	// The Toggle is interactive and must retain its data-k markers inside the
	// SSR-evaluated Panel (children injection through the tree path).
	if !strings.Contains(result.HTML, `data-k="k:`) {
		t.Errorf("expected hydration data-k markers for child of SSR wrapper, got:\n%s", result.HTML)
	}
}

func TestEmitExprSlotEscapesInitial(t *testing.T) {
	src := "export default function Page() {\n  const [code, setCode] = createSignal(`return <h1>x</h1>`);\n  return <code>{code()}</code>;\n}"
	result, _ := fullPipeline(t, src)
	if !strings.Contains(result.HTML, "&lt;h1&gt;") {
		t.Errorf("expected escaped expr-slot initial, got:\n%s", result.HTML)
	}
	if strings.Contains(result.HTML, "<h1>x</h1>") {
		t.Errorf("expr slot initial leaked raw markup:\n%s", result.HTML)
	}
}

// ─── Runtime component emission ─────────────────────────────────────────────

func TestEmitRuntimeResolvedPropsScript(t *testing.T) {
	src := `function RuntimeWidget(props) { return <div>{props.label}</div>; }
export default function Page() {
  return <div><RuntimeWidget label="hello" /><RuntimeWidget label="world" /></div>;
}`
	prog := parseProg(t, src)
	ann := annotateWithRuntime(prog, src)
	tree := irtree.Build(prog, ann)
	emitter := NewEmitter()
	result := emitter.Emit(tree)
	if strings.Count(result.HTML, "krate-id") != 2 {
		t.Errorf("expected 2 krate-id placeholders, got:\n%s", result.HTML)
	}
	if !strings.Contains(result.RuntimeHTML, `"label":"hello"`) || !strings.Contains(result.RuntimeHTML, `"label":"world"`) {
		t.Errorf("expected resolved props in runtime script, got %q", result.RuntimeHTML)
	}
}

// TestParallelEmitMatchesSequential proves the goroutine-based slot emitter is
// byte-for-byte equivalent to the sequential path, including runtime component
// id re-keying and orphan/metadata routing.
func TestParallelEmitMatchesSequential(t *testing.T) {
	src := `function RuntimeWidget(props) { return <div>{props.label}</div>; }
function Counter() {
  const [c, setC] = createSignal(0);
  return <button onClick={() => setC(c + 1)}>{c()}</button>;
}
export default function Page() {
  return <div>
    <Counter />
    <Head><title>T</title></Head>
    <RuntimeWidget label="a" />
    <RuntimeWidget label="b" />
    <Counter />
  </div>;
}`
	prog := parseProg(t, src)
	ann := annotateWithRuntime(prog, src)
	tree := irtree.Build(prog, ann)

	run := func(parallel bool) *EmitResult {
		old := parallelMinChildren
		defer func() { parallelMinChildren = old }()
		if parallel {
			parallelMinChildren = 2
		} else {
			parallelMinChildren = 1 << 30
		}
		emitter := NewEmitter()
		return emitter.Emit(tree)
	}

	seq := run(false)
	par := run(true)

	if seq.HTML != par.HTML {
		t.Errorf("HTML mismatch (parallel != sequential):\n--- seq ---\n%s\n--- par ---\n%s", seq.HTML, par.HTML)
	}
	if seq.RuntimeHTML != par.RuntimeHTML {
		t.Errorf("RuntimeHTML mismatch:\n--- seq ---\n%s\n--- par ---\n%s", seq.RuntimeHTML, par.RuntimeHTML)
	}
	if seq.HeadHTML != par.HeadHTML {
		t.Errorf("HeadHTML mismatch: seq=%q par=%q", seq.HeadHTML, par.HeadHTML)
	}
	if len(seq.Signatures) != len(par.Signatures) {
		t.Fatalf("signature count mismatch: seq=%d par=%d", len(seq.Signatures), len(par.Signatures))
	}
	for i := range seq.Signatures {
		if !sigEqual(seq.Signatures[i], par.Signatures[i]) {
			t.Errorf("signature %d mismatch:\n--- seq ---\n%+v\n--- par ---\n%+v", i, seq.Signatures[i], par.Signatures[i])
		}
	}
}

func sigEqual(a, b irtree.ComponentSignature) bool {
	if a.ComponentID != b.ComponentID || a.Tier != b.Tier ||
		len(a.Signals) != len(b.Signals) || len(a.SlotBindings) != len(b.SlotBindings) ||
		len(a.AttrBindings) != len(b.AttrBindings) {
		return false
	}
	for i := range a.SlotBindings {
		if !slotBindingEqual(a.SlotBindings[i], b.SlotBindings[i]) {
			return false
		}
	}
	return true
}

func slotBindingEqual(a, b irtree.SlotBinding) bool {
	if a.SlotID != b.SlotID || a.Type != b.Type || a.ExprJS != b.ExprJS ||
		len(a.Signals) != len(b.Signals) {
		return false
	}
	for i := range a.Signals {
		if a.Signals[i] != b.Signals[i] {
			return false
		}
	}
	return true
}

// ─── Suspense slot emission ─────────────────────────────────────────────────

func TestEmitSuspenseSlot(t *testing.T) {
	fb := []irtree.SlotNode{&irtree.StaticHTML{HTML: `<div class="loading">Loading...</div>`}}
	slot := &irtree.SuspenseSlot{
		ID:       "page.suspense.0",
		Fallback: fb,
		StreamID: "0",
	}
	tree := &irtree.ComponentTree{
		Root: &irtree.ComponentNode{
			Name:     "Page",
			Tier:     irtree.TierClient,
			Children: []irtree.SlotNode{slot},
		},
		Functions: map[string]*ast.FnDecl{},
	}
	emitter := NewEmitter()
	result := emitter.Emit(tree)
	if !strings.Contains(result.HTML, `<!--suspense:0-->`) {
		t.Errorf("expected suspense open marker, got:\n%s", result.HTML)
	}
	if !strings.Contains(result.HTML, `<div class="loading">Loading...</div>`) {
		t.Errorf("expected fallback HTML, got:\n%s", result.HTML)
	}
	if !strings.Contains(result.HTML, `<!--/suspense:0-->`) {
		t.Errorf("expected suspense close marker, got:\n%s", result.HTML)
	}
}
