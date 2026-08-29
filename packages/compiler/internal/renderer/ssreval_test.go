package renderer

import (
	"testing"

	"krate-compiler/internal/ast"
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
