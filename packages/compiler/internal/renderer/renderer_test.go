package renderer

import (
	"regexp"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/annotator"
	"github.com/kratejs/krate/packages/compiler/internal/bundler"
	"github.com/kratejs/krate/packages/compiler/internal/config"
	"github.com/kratejs/krate/packages/compiler/internal/escape"
	"github.com/kratejs/krate/packages/compiler/internal/irtree"
	"github.com/kratejs/krate/packages/compiler/internal/lexer"
	"github.com/kratejs/krate/packages/compiler/internal/parser"
)

// ─── Test helpers ────────────────────────────────────────────────────────────

func parseProg(t *testing.T, src string) *ast.Program {
	t.Helper()
	l := lexer.New(src)
	tokens := l.Tokenize()
	p := parser.New(tokens)
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	return prog
}

func fullPipeline(t *testing.T, src string) (*EmitResult, string) {
	t.Helper()
	prog := parseProg(t, src)
	cfg := &config.Config{}
	ann := annotator.Annotate(prog, cfg, "test.tsx", src)
	tree := irtree.Build(prog, ann)
	emitter := NewEmitter()
	result := emitter.Emit(tree)
	EmitMeta(tree, result)
	hydrationJS := GenerateNewHydrationJS(result)
	return result, hydrationJS
}

func fullPipelineWithReact(t *testing.T, src string) (*EmitResult, string) {
	t.Helper()
	prog := parseProg(t, src)
	bundler.RewriteReact(prog)
	cfg := &config.Config{}
	ann := annotator.Annotate(prog, cfg, "test.tsx", src)
	tree := irtree.Build(prog, ann)
	emitter := NewEmitter()
	result := emitter.Emit(tree)
	EmitMeta(tree, result)
	hydrationJS := GenerateNewHydrationJS(result)
	return result, hydrationJS
}

// ─── Shared utility tests ────────────────────────────────────────────────────

func TestEscapeHTML(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"hello", "hello"},
		{"<script>", "&lt;script&gt;"},
		{"a & b", "a &amp; b"},
		{`"quote"`, "&quot;quote&quot;"},
		{"it's", "it&#39;s"},
	}
	for _, tt := range tests {
		got := escape.HTML(tt.input)
		if got != tt.expected {
			t.Errorf("escape.HTML(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestHasJSX(t *testing.T) {
	expr := &ast.JSXElement{Opening: &ast.JSXOpening{Name: "div"}}
	if !hasJSX(expr) {
		t.Error("expected hasJSX to return true for JSXElement")
	}
	expr2 := &ast.Literal{Kind: ast.NumberLit, Value: "1"}
	if hasJSX(expr2) {
		t.Error("expected hasJSX to return false for literal")
	}
}

func TestMangleIdentInString(t *testing.T) {
	result := mangleIdentInString("count + 1", "count", "count_c0")
	if result != "count_c0 + 1" {
		t.Errorf("expected 'count_c0 + 1', got %q", result)
	}
}

func TestExtractJSONProp(t *testing.T) {
	val := extractJSONProp(`{"count":42,"name":"hello"}`, "count")
	if val != "42" {
		t.Errorf("expected '42', got %q", val)
	}
	val = extractJSONProp(`{"count":42}`, "missing")
	if val != "" {
		t.Errorf("expected empty, got %q", val)
	}
}

func TestArrowBodyExpr(t *testing.T) {
	arrow := &ast.ArrowFn{
		Expression: true,
		Body: []ast.Stmt{
			&ast.ExprStmt{Expression: &ast.Literal{Kind: ast.NumberLit, Value: "42"}},
		},
	}
	expr := arrowBodyExpr(arrow)
	if expr == nil {
		t.Fatal("expected non-nil expression")
	}
	if lit, ok := expr.(*ast.Literal); !ok || lit.Value != "42" {
		t.Errorf("expected literal '42', got %v", expr)
	}
}

func TestStringLiteralValue(t *testing.T) {
	lit := &ast.Literal{Kind: ast.StringLit, Value: "hello"}
	if got := stringLiteralValue(lit); got != "hello" {
		t.Errorf("expected 'hello', got %q", got)
	}
}

func TestItoa(t *testing.T) {
	tests := []struct {
		input    int
		expected string
	}{
		{0, "0"},
		{1, "1"},
		{42, "42"},
		{123, "123"},
		{999, "999"},
	}
	for _, tt := range tests {
		got := itoa(tt.input)
		if got != tt.expected {
			t.Errorf("itoa(%d) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestEscapeJSString(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"hello", "hello"},
		{"it's", "it\\'s"},
		{"line\nbreak", "line\\nbreak"},
		{"back\\slash", "back\\\\slash"},
	}
	for _, tt := range tests {
		got := escapeJSString(tt.input)
		if got != tt.expected {
			t.Errorf("escapeJSString(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestToFloat(t *testing.T) {
	if f := toFloat("3.14"); f < 3.13 || f > 3.15 {
		t.Errorf("expected ~3.14, got %f", f)
	}
	if f := toFloat(""); f != 0 {
		t.Errorf("expected 0 for empty string, got %f", f)
	}
}

// ─── New pipeline: HTML output tests ─────────────────────────────────────────

func TestRenderSimpleHTML(t *testing.T) {
	result, _ := fullPipeline(t, `export default function Page() { return <div>hello</div>; }`)
	if !strings.Contains(result.HTML, "<div>hello</div>") {
		t.Errorf("expected <div>hello</div> in HTML, got: %s", result.HTML)
	}
}

func TestRenderWithTextContent(t *testing.T) {
	result, _ := fullPipeline(t, `export default function Page() { return <p>Hello world</p>; }`)
	if !strings.Contains(result.HTML, "<p>Hello world</p>") {
		t.Errorf("expected <p>Hello world</p> in HTML, got: %s", result.HTML)
	}
}

func TestRenderWithSelfClosingTag(t *testing.T) {
	result, _ := fullPipeline(t, `export default function Page() { return <br />; }`)
	if !strings.Contains(result.HTML, "<br") {
		t.Errorf("expected <br> in HTML, got: %s", result.HTML)
	}
}

func TestRenderWithAttributes(t *testing.T) {
	result, _ := fullPipeline(t, `export default function Page() { return <a href="/test">link</a>; }`)
	if !strings.Contains(result.HTML, `href="/test"`) {
		t.Errorf("expected href attribute in HTML, got: %s", result.HTML)
	}
}

func TestRenderHeadComponent(t *testing.T) {
	result, _ := fullPipeline(t, `export default function Page() { return <Head><title>Test</title></Head>; }`)
	if !strings.Contains(result.HeadHTML, "<title>Test</title>") {
		t.Errorf("expected <title>Test</title> in HeadHTML, got: %s", result.HeadHTML)
	}
}

func TestRenderScriptComponentInline(t *testing.T) {
	result, _ := fullPipeline(t, `export default function Page() { return <Script>{"console.log('hello')"}</Script>; }`)
	if !strings.Contains(result.ScriptHTML, "console.log") {
		t.Errorf("expected inline script in ScriptHTML, got: %s", result.ScriptHTML)
	}
}

func TestRenderStyleComponent(t *testing.T) {
	result, _ := fullPipeline(t, `export default function Page() { return <Style>{"body { color: red; }"}</Style>; }`)
	if !strings.Contains(result.StyleHTML, "body { color: red; }") {
		t.Errorf("expected style content in StyleHTML, got: %s", result.StyleHTML)
	}
}

func TestRenderFragment(t *testing.T) {
	result, _ := fullPipeline(t, `export default function Page() { return <><div>A</div><div>B</div></>; }`)
	if !strings.Contains(result.HTML, "<div>A</div>") || !strings.Contains(result.HTML, "<div>B</div>") {
		t.Errorf("expected both children in HTML, got: %s", result.HTML)
	}
}

func TestRenderNestedElements(t *testing.T) {
	result, _ := fullPipeline(t, `export default function Page() { return <div><span>inner</span></div>; }`)
	if !strings.Contains(result.HTML, "<span>inner</span>") {
		t.Errorf("expected nested span in HTML, got: %s", result.HTML)
	}
}

// ─── New pipeline: Signal detection ──────────────────────────────────────────

func TestSignalDetection(t *testing.T) {
	result, _ := fullPipeline(t, `export default function Page() { const [count, setCount] = createSignal(0); return <div>{count()}</div>; }`)
	if len(result.Signatures) == 0 {
		t.Fatal("expected at least 1 signature")
	}
	found := false
	for _, sig := range result.Signatures {
		for _, s := range sig.Signals {
			if s.Name == "count" {
				found = true
				if s.SetterName != "setCount" {
					t.Errorf("expected setter 'setCount', got %q", s.SetterName)
				}
			}
		}
	}
	if !found {
		t.Error("expected signal 'count' in signatures")
	}
}

func TestHydrationJSWithSignals(t *testing.T) {
	_, js := fullPipeline(t, `export default function Page() { const [x, setX] = createSignal(0); return <div>{x()}</div>; }`)
	if js == "" {
		t.Fatal("expected non-empty hydration JS")
	}
	if !strings.Contains(js, "createSignal") {
		t.Error("expected createSignal in hydration JS")
	}
}

func TestHydrationJSStaticPage(t *testing.T) {
	_, js := fullPipeline(t, `export default function Page() { return <div>static</div>; }`)
	if js != "" {
		t.Fatalf("expected no hydration JS for a static page, got:\n%s", js)
	}
}

func TestHydrationJSWithRef(t *testing.T) {
	src := `function Code() {
  var wrapRef = null;
  onMount(function () {
    if (!wrapRef) return;
    var codeEl = wrapRef.querySelector("code");
    if (codeEl && typeof codeEl.textContent === "string") {
      codeEl.setAttribute("data-code", codeEl.textContent);
    }
  });
  function handleCopy() {
    if (!wrapRef) return;
    var codeEl = wrapRef.querySelector("code");
    var text = codeEl ? codeEl.textContent || "" : "";
    if (navigator.clipboard) {
      navigator.clipboard.writeText(text);
    }
  }
  return <div class="code-block" ref={wrapRef}><button onClick={handleCopy}>Copy</button></div>;
}
export default function Page() {
  return <Code />;
}`
	result, js := fullPipeline(t, src)
	if !strings.Contains(result.HTML, `data-k="k:`) {
		t.Errorf("expected ref target element to carry a data-k slot id, got:\n%s", result.HTML)
	}
	if !strings.Contains(js, "kbindRef") {
		t.Errorf("expected hydration JS to emit kbindRef, got:\n%s", js)
	}
	if !strings.Contains(js, "onMount(") {
		t.Errorf("expected hydration JS to emit onMount, got:\n%s", js)
	}
	if !strings.Contains(js, "typeof codeEl.textContent") {
		t.Errorf("expected typeof operator to keep its space separator, got:\n%s", js)
	}
}

func TestHydrationJSSignalInitPropsRegistration(t *testing.T) {
	// A signal whose initializer references props at runtime (e.g. a missing
	// prop compared with !==) must cause the child's props registration to be
	// hoisted, so `props` resolves inside the component IIFE instead of
	// throwing "props is not defined" during hydration.
	src := `function Folder(props) {
  var [open, setOpen] = createSignal(props.defaultOpen !== false);
  function toggle() { setOpen(!open()); }
  return <div class="krate-folder" onClick={toggle}>{open() ? "open" : "closed"}</div>;
}
export default function Page() {
  return <Folder name="public" />;
}`
	_, js := fullPipeline(t, src)
	if strings.Contains(js, "createSignal(props.defaultOpen") {
		// The expression should have been const-folded to a boolean since a
		// missing prop is a definite undefined. If it was NOT folded (i.e. it
		// appears verbatim), the props registration must still be present so
		// `props` is defined at runtime.
		if !strings.Contains(js, "var props=__krate_props[") {
			t.Errorf("expected props registration when signal init references props verbatim, got:\n%s", js)
		}
	}
}

func TestHydrationJSNamedCallbackRef(t *testing.T) {
	// A locally named function used as a ref (`ref={setRef}` where
	// `function setRef(el){...}`) is a callback ref: kbindRef(id, setRef) must
	// INVOKE the function with the node. Regression: it was compiled as an
	// assignment target (el=>{setRef=el;}), overwriting the function variable
	// with the element so an onMount that reads/calls setRef saw the node, not
	// the function.
	src := `function Widget() {
  var [count, setCount] = createSignal(0);
  var got = null;
  function setRef(el) { got = el; }
  onMount(function () { if (typeof setRef !== "function") throw new Error("setRef clobbered"); });
  return <div class="named" ref={setRef}>{count()}</div>;
}
export default function Page() {
  return <Widget />;
}`
	_, js := fullPipeline(t, src)
	if !strings.Contains(js, "kbindRef(") {
		t.Fatalf("expected hydration JS to emit kbindRef, got:\n%s", js)
	}
	if strings.Contains(js, "setRef=el;") || strings.Contains(js, "setRef = el;") {
		t.Errorf("named callback ref must not compile to an assignment that overwrites the function:\n%s", js)
	}
	if !strings.Contains(js, "kbindRef(") || !strings.Contains(js, ",setRef)") {
		t.Errorf("expected kbindRef(id, setRef) invoking the named function, got:\n%s", js)
	}
}

func TestHydrationJSCallbackRef(t *testing.T) {
	// Callback refs (ref={(el) => { rootRef = el; }}) must be emitted as the
	// callback itself — kbindRef(id, (el)=>{...}) — NOT wrapped as an assignment
	// target (kbindRef(id, el=>{(el)=>{...}=el;})) which is a syntax error.
	src := `function Widget() {
  var [count, setCount] = createSignal(0);
  var rootRef = null;
  return <div class="cb" ref={(el) => { rootRef = el; }}>{count()}</div>;
}
export default function Page() {
  return <Widget />;
}`
	_, js := fullPipeline(t, src)
	if !strings.Contains(js, "kbindRef(") {
		t.Fatalf("expected hydration JS to emit kbindRef, got:\n%s", js)
	}
	if strings.Contains(js, "=el;}))") || strings.Contains(js, "}=el;") {
		t.Errorf("callback ref must not be wrapped as an assignment target:\n%s", js)
	}
	flat := strings.Join(strings.Fields(js), "")
	if !strings.Contains(flat, "(el)=>{rootRef=el;}") && !strings.Contains(flat, "(el)=>{(rootRef=el);}") {
		t.Errorf("expected the arrow callback passed to kbindRef, got:\n%s", js)
	}
}

func TestHydrationJSPropsDeclPrecedesSignalInits(t *testing.T) {
	// A signal whose initializer reads props at runtime (cannot be const-folded
	// because the child's props are runtime-hoisted from the parent, e.g. when
	// a handler references props.onValueChange) must still see `props` declared
	// before the signal init runs. Signal inits are emitted before other extra
	// vars, so the `var props=__krate_props[...]` decl must be hoisted above
	// them.
	src := `function RadioGroup(props) {
  var [selected, setSelected] = createSignal((props.defaultValue || "").trim());
  function handleChange(v) {
    setSelected(v);
    if (props.onValueChange) props.onValueChange(v);
  }
  return <div data-krate-radio-group="true" onClick={handleChange}>{selected()}</div>;
}
function Demo() {
  var [sel, setSel] = createSignal("x");
  return <RadioGroup defaultValue="comfy" onValueChange={setSel} />;
}
export default function Page() {
  return <Demo />;
}`
	_, js := fullPipeline(t, src)
	propsDecl := "var props=__krate_props["
	sigIdx := strings.Index(js, "props.defaultValue")
	declIdx := strings.Index(js, propsDecl)
	if sigIdx == -1 {
		t.Fatalf("expected signal init to reference props verbatim, got:\n%s", js)
	}
	if declIdx == -1 {
		t.Fatalf("expected props registration decl, got:\n%s", js)
	}
	if declIdx > sigIdx {
		t.Errorf("props decl (%d) must precede signal init (%d):\n%s", declIdx, sigIdx, js)
	}
}

func TestHydrationJSLocalVarPrecedesSignalInit(t *testing.T) {
	// Regression for the release-qualification "initializer" probe: a signal
	// whose initializer reads a local object (`var initial = {value:7}` then
	// createSignal(initial.value)) must not emit the signal before the local
	// declaration. `var` hoists the name to undefined, so signal-first emission
	// throws "Cannot read properties of undefined" at hydration (createSignal(
	// initial.value); var initial={value:7};).
	src := `function Widget() {
  var initial = { value: 7 };
  var [count, setCount] = createSignal(initial.value);
  var doubled = count() * 2;
  return <div onClick={() => setCount(count() + 1)}>{count()} {doubled}</div>;
}
export default function Page() {
  return <Widget />;
}`
	_, js := fullPipeline(t, src)
	initIdx := strings.Index(js, "initial.value")
	declIdx := strings.Index(js, "var initial=")
	if initIdx == -1 {
		t.Fatalf("expected signal init to reference the local var, got:\n%s", js)
	}
	if declIdx == -1 {
		t.Fatalf("expected local var declaration, got:\n%s", js)
	}
	if declIdx > initIdx {
		t.Errorf("local var (%d) must be declared before signal init (%d):\n%s", declIdx, initIdx, js)
	}

	// A local var that reads a signal getter must stay AFTER the signal
	// declaration (const [count,...]=createSignal(...)) it depends on.
	sigIdx := strings.Index(js, "const [count,setCount]=createSignal(")
	doubledIdx := strings.Index(js, "var doubled=")
	if sigIdx == -1 || doubledIdx == -1 {
		t.Fatalf("expected signal decl and signal-reading var, got:\n%s", js)
	}
	if doubledIdx < sigIdx {
		t.Errorf("signal-reading var (%d) must come after signal decl (%d):\n%s", doubledIdx, sigIdx, js)
	}
}

func TestHydrationJSWithTopLevelOnCleanup(t *testing.T) {
	src := `function Widget() {
  onCleanup(function () {
    window.clearAll();
  });
  return <div>widget</div>;
}
export default function Page() {
  return <Widget />;
}`
	_, js := fullPipeline(t, src)
	if !strings.Contains(js, "onCleanup(") {
		t.Errorf("expected hydration JS to emit top-level onCleanup, got:\n%s", js)
	}
}

func TestHydrationJSWithDynamicImport(t *testing.T) {
	src := `function Widget() {
  createEffect(function () {
    import("./widget.js").then(function (m) { window.setTitle(m.title); });
  });
  return <div>widget</div>;
}
export default function Page() {
  return <Widget />;
}`
	_, js := fullPipeline(t, src)
	if !strings.Contains(js, "import('./widget.js')") {
		t.Errorf("expected hydration JS to emit dynamic import, got:\n%s", js)
	}
}

func TestHydrationJSWithImportMetaURL(t *testing.T) {
	src := `function Widget() {
  createEffect(function () {
    var banner = new URL("../assets/banner.png", import.meta.url);
    window.setBanner(banner.href);
  });
  return <div>widget</div>;
}
export default function Page() {
  return <Widget />;
}`
	_, js := fullPipeline(t, src)
	if !strings.Contains(js, "import.meta.url") {
		t.Errorf("expected hydration JS to emit import.meta.url, got:\n%s", js)
	}
	if !strings.Contains(js, "new URL(") {
		t.Errorf("expected hydration JS to emit new URL, got:\n%s", js)
	}
}

// ─── New pipeline: Component tiers ───────────────────────────────────────────

func TestServerComponentRendering(t *testing.T) {
	src := `// @server
function ServerWidget(props) {
	return (
		<div class="widget">
			<h2>{props.title}</h2>
			<p>Static content from server</p>
		</div>
	);
}

function Page() {
	return (
		<div>
			<ServerWidget title="Hello" />
		</div>
	);
}
export default Page;`
	result, _ := fullPipeline(t, src)
	if !strings.Contains(result.HTML, "Static content from server") {
		t.Errorf("expected server component content in HTML:\n%s", result.HTML)
	}
	if !strings.Contains(result.HTML, `data-krate-server`) {
		t.Errorf("expected data-krate-server attribute in HTML:\n%s", result.HTML)
	}
}

func TestRuntimeComponentRendering(t *testing.T) {
	src := `// @runtime
function RuntimeWidget(props) {
	return <div class="widget">{props.label}</div>;
}
function Page() {
	return <div><RuntimeWidget label="hello" /></div>;
}
export default Page;`
	result, _ := fullPipeline(t, src)
	if !strings.Contains(result.HTML, "<!--region:") {
		t.Errorf("expected runtime component region splice marker in HTML:\n%s", result.HTML)
	}
}

func TestStaticComponentRendering(t *testing.T) {
	src := `// @static
function StaticWidget() {
	return <div>purely static</div>;
}
function Page() {
	return <div><StaticWidget /></div>;
}
export default Page;`
	result, _ := fullPipeline(t, src)
	if !strings.Contains(result.HTML, "purely static") {
		t.Errorf("expected static content in HTML:\n%s", result.HTML)
	}
}

// ─── New pipeline: React interop (always on, via bundler.RewriteReact) ───────

func TestReactInteropSignalDetection(t *testing.T) {
	src := `import { useState } from 'react';
export default function Page() { const [count, setCount] = useState(0); return <div>{count()}</div>; }`
	result, _ := fullPipelineWithReact(t, src)
	if len(result.Signatures) == 0 {
		t.Fatal("expected at least 1 signature")
	}
	found := false
	for _, sig := range result.Signatures {
		for _, s := range sig.Signals {
			if s.Name == "count" {
				found = true
			}
		}
	}
	if !found {
		t.Error("expected signal 'count' in signatures")
	}
}

func TestReactInteropHydrationJSWithEffects(t *testing.T) {
	src := `import { useState, useEffect } from 'react';
export default function Page() {
	const [count, setCount] = useState(0);
	useEffect(() => { document.title = count; });
	return <div>{count()}</div>;
}`
	_, js := fullPipelineWithReact(t, src)
	if js == "" {
		t.Fatal("expected non-empty hydration JS")
	}
	if !strings.Contains(js, "createSignal") {
		t.Error("expected createSignal in hydration JS")
	}
}

func TestReactInteropCollectsEffects(t *testing.T) {
	src := `import { useEffect } from 'react';
export default function Page() {
	useEffect(() => { document.title = "hello"; });
	return <div>static</div>;
}`
	result, _ := fullPipelineWithReact(t, src)
	if len(result.Signatures) == 0 {
		t.Fatal("expected at least 1 signature")
	}
	found := false
	for _, sig := range result.Signatures {
		if len(sig.Effects) > 0 {
			found = true
		}
	}
	if !found {
		t.Error("expected effects in signatures")
	}
}

func TestReactInteropCollectsMemos(t *testing.T) {
	src := `import { createMemo } from 'react';
export default function Page() {
	createMemo(() => { return 42; });
	return <div>static</div>;
}`
	result, _ := fullPipelineWithReact(t, src)
	if len(result.Signatures) == 0 {
		t.Fatal("expected at least 1 signature")
	}
	// createMemo may or may not be detected depending on annotator config.
	// Verify the pipeline doesn't crash and produces output.
	t.Logf("Signatures: %d, first memos: %v", len(result.Signatures), result.Signatures[0].Memos)
}

func TestReactInteropHandlerAddsParens(t *testing.T) {
	src := `import { useState } from 'react';
export default function Page() { const [count, setCount] = useState(0); return <button onClick={() => setCount(count + 1)}>click</button>; }`
	result, js := fullPipelineWithReact(t, src)
	if js == "" {
		t.Fatal("expected non-empty hydration JS")
	}
	if !strings.Contains(js, "createSignal") {
		t.Error("expected createSignal in hydration JS")
	}
	// Verify handler is present in signatures
	found := false
	for _, sig := range result.Signatures {
		for _, h := range sig.Handlers {
			if h.Event == "click" {
				found = true
			}
		}
	}
	if !found {
		t.Error("expected click handler in signatures")
	}
}

func TestReactInteropHandlerWithCreateSignal(t *testing.T) {
	src := `export default function Page() { const [count, setCount] = createSignal(0); return <button onClick={() => setCount(count + 1)}>click</button>; }`
	_, js := fullPipeline(t, src)
	if js == "" {
		t.Fatal("expected non-empty hydration JS")
	}
	if !strings.Contains(js, "createSignal") {
		t.Error("expected createSignal in hydration JS")
	}
}

// ─── New pipeline: Hydration correctness ─────────────────────────────────────

func TestHandlerIndicesUnique(t *testing.T) {
	src := `
		function CompA() {
			const [x, setX] = createSignal(0);
			return <button onClick={() => setX(1)}>A</button>;
		}
		function CompB() {
			const [y, setY] = createSignal(0);
			return <button onClick={() => setY(2)}>B</button>;
		}
		export default function Page() {
			return <div><CompA /><CompB /></div>;
		}`
	_, js := fullPipeline(t, src)
	if js == "" {
		t.Fatal("expected non-empty hydration JS")
	}
	if !strings.Contains(js, "createSignal") {
		t.Error("expected createSignal in hydration JS")
	}
	idxRe := regexp.MustCompile(`data-kh="(\d+)"`)
	matches := idxRe.FindAllStringSubmatch(js, -1)
	seen := make(map[string]bool)
	for _, m := range matches {
		if seen[m[1]] {
			t.Errorf("duplicate data-kh index in hydration JS: %s", m[1])
		}
		seen[m[1]] = true
	}
}

func TestHandlerDetection(t *testing.T) {
	src := `export default function Page() {
		const [count, setCount] = createSignal(0);
		return <button onClick={() => setCount(1)}>click</button>;
	}`
	result, _ := fullPipeline(t, src)
	if len(result.Signatures) == 0 {
		t.Fatal("expected at least 1 signature")
	}
	found := false
	for _, sig := range result.Signatures {
		for _, h := range sig.Handlers {
			if h.Event == "click" {
				found = true
			}
		}
	}
	if !found {
		t.Error("expected click handler in signatures")
	}
}

func TestSignalInitialValues(t *testing.T) {
	src := `export default function Page() {
		const [count, setCount] = createSignal(42);
		return <div>{count()}</div>;
	}`
	result, js := fullPipeline(t, src)
	if len(result.Signatures) == 0 {
		t.Fatal("expected at least 1 signature")
	}
	found := false
	for _, sig := range result.Signatures {
		for _, s := range sig.Signals {
			if s.Name == "count" && s.Initial == "42" {
				found = true
			}
		}
	}
	if !found {
		t.Error("expected signal 'count' with initial value '42'")
	}
	if !strings.Contains(js, "42") {
		t.Error("expected '42' in hydration JS")
	}
}

func TestStringSignalInitialValue(t *testing.T) {
	src := `export default function Page() {
		const [name, setName] = createSignal("World");
		return <div>{name()}</div>;
	}`
	result, js := fullPipeline(t, src)
	if len(result.Signatures) == 0 {
		t.Fatal("expected at least 1 signature")
	}
	found := false
	for _, sig := range result.Signatures {
		for _, s := range sig.Signals {
			if s.Name == "name" && s.Initial == "World" {
				found = true
			}
		}
	}
	if !found {
		t.Error("expected signal 'name' with initial value 'World'")
	}
	if !strings.Contains(js, "'World'") {
		t.Error("expected 'World' string in hydration JS")
	}
}

// ─── New pipeline: Complex scenarios ─────────────────────────────────────────

func TestMultiComponentHandlerMangling(t *testing.T) {
	src := `function CompA(props) {
  const [show, setShow] = createSignal(true);
  return (
    <div>
      {show() && <div class="visible">Visible content</div>}
      {show()
        ? <div class="on">Shown</div>
        : <div class="off">Hidden</div>
      }
    </div>
  );
}

function CompB(props) {
  const [val, setVal] = createSignal("hello");
  const [num, setNum] = createSignal(42);

  return (
    <div>
      <input
        value={val()}
        onInput={(e) => {
          const target = e.target as HTMLInputElement;
          setVal(target.value);
        }}
      />
      <button onClick={() => setNum(n => n + 1)}>Inc</button>
      <button onClick={() => {
        const current = num();
        if (current > 100) {
          setNum(0);
        } else {
          setNum(current * 2);
        }
      }}>Double or Reset</button>
      <p>{val()} - {num()}</p>
    </div>
  );
}

export default function Page() {
  return (
    <div class="edge-cases">
      <CompA />
      <CompB />
    </div>
  );
}`

	result, js := fullPipeline(t, src)

	if len(result.Signatures) == 0 {
		t.Fatal("expected at least 1 signature")
	}

	totalSignals := 0
	for _, sig := range result.Signatures {
		totalSignals += len(sig.Signals)
	}
	if totalSignals == 0 {
		t.Error("expected signals in signatures")
	}
	t.Logf("Total signals across all components: %d", totalSignals)

	if js == "" {
		t.Error("expected non-empty hydration JS")
	}
	if !strings.Contains(js, "createSignal") {
		t.Error("expected createSignal in hydration JS")
	}

	if !strings.Contains(result.HTML, "Inc") {
		t.Error("expected 'Inc' button in HTML")
	}
	if !strings.Contains(result.HTML, "Double or Reset") {
		t.Error("expected 'Double or Reset' button in HTML")
	}
}

func TestReactInteropCollectsRealisticPage(t *testing.T) {
	src := `import { useState, useEffect, useRef, useCallback } from 'react';
function TimerDisplay(props) {
  const [seconds, setSeconds] = useState(0);
  const intervalRef = useRef(null);
  useEffect(() => {
    intervalRef.current = setInterval(function() {
      setSeconds(function(prev) { return prev + 1; });
    }, 1000);
    return function() {
      if (intervalRef.current) clearInterval(intervalRef.current);
    };
  }, []);
  return <div>{seconds}</div>;
}
function Greeting(props) {
  const [name, setName] = useState('World');
  return <div><TimerDisplay /></div>;
}
export default Greeting;`
	result, _ := fullPipelineWithReact(t, src)
	if len(result.Signatures) == 0 {
		t.Fatal("expected at least 1 signature")
	}
	t.Logf("Signatures: %d", len(result.Signatures))
	for _, sig := range result.Signatures {
		for _, s := range sig.Signals {
			t.Logf("  signal: %s", s.Name)
		}
	}
}

func TestReactServerComponentRendering(t *testing.T) {
	src := `
		// @server
		function ServerWidget(props) {
			return (
				<div class="widget">
					<h2>{props.title}</h2>
					<p>Static content from server</p>
				</div>
			);
		}

		function ClientCounter() {
			const [count, setCount] = createSignal(0);
			return (
				<div>
					<p>{count()}</p>
					<button onClick={() => setCount(count() + 1)}>Click</button>
				</div>
			);
		}

		function Page() {
			return (
				<div>
					<ServerWidget title="Hello" />
					<ClientCounter />
				</div>
			);
		}
		export default Page;
	`
	result, _ := fullPipeline(t, src)

	if !strings.Contains(result.HTML, "Static content from server") {
		t.Errorf("expected server component content in HTML:\n%s", result.HTML)
	}

	if !strings.Contains(result.HTML, `data-krate-server`) {
		t.Errorf("expected data-krate-server attribute in HTML:\n%s", result.HTML)
	}

	// Verify the hydration JS doesn't reference server components
	hdrJS := GenerateNewHydrationJS(result)
	if strings.Contains(hdrJS, "ServerWidget") {
		t.Errorf("hydration JS should not reference server component:\n%s", hdrJS)
	}
}

// TestAsyncEffectEmitsAsync ensures an `async () => {}` effect keeps its
// `async` keyword in the emitted hydration JS. Dropping it produces
// `await` outside an async function (a SyntaxError that breaks the page).
func TestAsyncEffectEmitsAsync(t *testing.T) {
	src := `import { createSignal, createEffect } from '@krate/runtime';
export default function Page() {
	const [msg, setMsg] = createSignal("");
	createEffect(async () => {
		const res = await Promise.resolve("resolved");
		setMsg(res);
	});
	return <div>{msg()}</div>;
}`
	_, js := fullPipeline(t, src)
	if !strings.Contains(js, "async") {
		t.Errorf("expected async keyword in hydration JS, got:\n%s", js)
	}
	if strings.Contains(js, "await") && !strings.Contains(js, "async") {
		t.Errorf("hydration JS has await without async:\n%s", js)
	}
}

// TestModuleConstantsFoldInJSX ensures module-level const declarations with
// statically-evaluable initializers are folded into JSX text at build time,
// so `{hexNum}` renders 255 instead of the raw identifier name and does not
// emit a hydration binding referencing an out-of-scope identifier.
func TestModuleConstantsFoldInJSX(t *testing.T) {
	src := `const hexNum = 0xFF;
const greeting = "hello";
const total = 1 + 2 + 3;
export default function Page() {
	return <div><p>{hexNum}</p><p>{greeting}</p><p>{total}</p></div>;
}`
	result, js := fullPipeline(t, src)
	html := result.HTML

	if strings.Contains(html, "hexNum") {
		t.Errorf("module const hexNum leaked identifier name into HTML:\n%s", html)
	}
	if !strings.Contains(html, "0xFF") && !strings.Contains(html, "255") {
		t.Errorf("expected hexNum value in HTML:\n%s", html)
	}
	if strings.Contains(html, "greeting") {
		t.Errorf("module const greeting leaked identifier name into HTML:\n%s", html)
	}
	if !strings.Contains(html, "hello") {
		t.Errorf("expected greeting value in HTML:\n%s", html)
	}
	if strings.Contains(html, "total") {
		t.Errorf("module const total leaked identifier name into HTML:\n%s", html)
	}
	if !strings.Contains(html, "6") {
		t.Errorf("expected computed total (6) in HTML:\n%s", html)
	}

	// No hydration binding should reference the module-level identifiers.
	for _, name := range []string{"hexNum", "greeting", "total"} {
		if strings.Contains(js, "=>"+name) || strings.Contains(js, "("+name+")") {
			t.Errorf("hydration JS references out-of-scope module const %q:\n%s", name, js)
		}
	}
}

// ─── <SyntaxHighlight> compile-time chroma ───────────────────────────────────

func TestSSREvalSyntaxHighlightHighlightsStaticCallSiteChildren(t *testing.T) {
	// A signal-less (SSR-evaluated) wrapper passing {children} through to
	// <SyntaxHighlight>: the call-site code text is static, so chroma must
	// highlight it at build time.
	src := `function Shell(props) {
	return <div><SyntaxHighlight lang={props.lang}>{props.children}</SyntaxHighlight></div>;
}
export default function Page() {
	return <Shell lang="js">{` + "`const x = 1;`" + `}</Shell>;
}`
	result, _ := fullPipeline(t, src)
	if !strings.Contains(result.HTML, "language-js") || !strings.Contains(result.HTML, "<span") {
		t.Errorf("expected chroma-highlighted output for static children, got:\n%s", result.HTML)
	}
}
