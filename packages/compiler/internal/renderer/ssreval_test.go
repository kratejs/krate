package renderer

import (
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/ast"
)

// TestSSREvalUnsupportedExpressionErrors verifies that expression constructs
// the SSR evaluator cannot handle produce a diagnostic instead of silently
// rendering empty input.
func TestSSREvalUnsupportedExpressionErrors(t *testing.T) {
	eval := NewSSREval(nil)

	// `this` in a server-component expression can't be statically evaluated.
	eval.Eval(&ast.ThisExpr{})
	if errs := eval.Errors(); len(errs) == 0 {
		t.Error("expected ThisExpr to produce an unsupported-expression error, got none")
	}
}

// TestEmitUnsupportedExpressionFailsBuild verifies that a page whose
// SSR-evaluated component uses an unsupported expression surfaces an error on
// EmitResult.Errors — the build must fail rather than ship empty output.
func TestEmitUnsupportedExpressionFailsBuild(t *testing.T) {
	// `{this}` appears in the return JSX of a signal-less (SSR-evaluated)
	// component. The evaluator cannot resolve `this` at compile time.
	src := `function Bad(props) {
  return <div>{this}</div>;
}
export default function Page() {
  return <Bad />;
}`
	result, _ := fullPipeline(t, src)
	if len(result.Errors) == 0 {
		t.Fatalf("expected EmitResult.Errors to be non-empty for unsupported expression, got HTML:\n%s", result.HTML)
	}
	t.Logf("OK   %v", result.Errors[0])
}

// TestShowIfSignalLessComponentStatic verifies the SSR-eval path (signal-less,
// prop-driven components) honors showIf: truthy includes the element, the
// attribute never leaks, and the injected child still hydrates.
func TestShowIfSignalLessComponentStatic(t *testing.T) {
	src := `function Banner(props) {
  return <div><p class="banner" showIf={props.x}>B</p></div>;
}
export default function Page() {
  return <Banner x={true} />;
}`
	result, _ := fullPipeline(t, src)
	if strings.Contains(result.HTML, "showIf") {
		t.Errorf("showIf must not leak into HTML, got: %s", result.HTML)
	}
	if !strings.Contains(result.HTML, `class="banner"`) {
		t.Errorf("expected showIf element to render, got: %s", result.HTML)
	}
}

// TestShowIfSignalLessComponentFalseElides verifies a false showIf on the
// SSR-eval path renders nothing.
func TestShowIfSignalLessComponentFalseElides(t *testing.T) {
	src := `function Banner(props) {
  return <div><p class="banner" showIf={props.x}>B</p></div>;
}
export default function Page() {
  return <Banner x={false} />;
}`
	result, _ := fullPipeline(t, src)
	if strings.Contains(result.HTML, `class="banner"`) {
		t.Errorf("expected false showIf to elide the element, got: %s", result.HTML)
	}
}

// TestStringSliceFoldsAtSSR verifies `str.slice(a, b)` folds to the sliced
// substring in emitted HTML. The SSR const evaluators previously ignored
// `.slice`, so the text rendered empty (e.g. the docs "Last updated" line).
func TestStringSliceFoldsAtSSR(t *testing.T) {
	src := `function Meta(props) {
  return <p class="updated">{props.updated.slice(0, 10)}</p>;
}
export default function Page() {
  return <Meta updated="2026-09-12T19:48:40+01:00" />;
}`
	result, _ := fullPipeline(t, src)
	if !strings.Contains(result.HTML, "2026-09-12") {
		t.Fatalf("expected sliced date in HTML, got:\n%s", result.HTML)
	}
	if strings.Contains(result.HTML, "T19:48") {
		t.Fatalf("slice must trim the timestamp, got:\n%s", result.HTML)
	}
}
