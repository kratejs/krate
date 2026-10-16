package build

import (
	"testing"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/lexer"
	"github.com/kratejs/krate/packages/compiler/internal/parser"
)

func parseTest(t *testing.T, src string) *ast.Program {
	t.Helper()
	l := lexer.New(src)
	p := parser.New(l.Tokenize())
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	return prog
}

func TestDetectRenderModeSuspenseAutoStreaming(t *testing.T) {
	// A page that uses <Suspense> but has no explicit
	// `export const config = { streaming: true }` must still be detected as
	// streaming - the resolved fallback is swapped in per request.
	src := `import { Suspense } from '@krate/runtime/server';
export default function P() {
  return <Suspense fallback={<span>loading</span>}><section>x</section></Suspense>;
}`
	mode, _ := detectRenderMode(parseTest(t, src))
	if mode != RenderStreaming {
		t.Fatalf("expected RenderStreaming from <Suspense> usage, got %v", mode)
	}
}

func TestDetectRenderModeSuspenseNestedInExpression(t *testing.T) {
	// <Suspense> nested inside a conditional + array must still be found by the
	// AST walk (a top-level string scan would also find it, but this proves the
	// walker descends past statements/expressions).
	src := `export default function P() {
  return <div>{cond ? [<Suspense fallback={<span>load</span>}><p>inner</p></Suspense>] : null}</div>;
}`
	mode, _ := detectRenderMode(parseTest(t, src))
	if mode != RenderStreaming {
		t.Fatalf("expected RenderStreaming from nested <Suspense>, got %v", mode)
	}
}

func TestDetectRenderModeSuspenseStringNoFalsePositive(t *testing.T) {
	// A page that merely mentions "<Suspense" inside a string or a commented-out
	// block must NOT be treated as streaming - only a real JSX element counts.
	// This is the regression case for the old string-scan implementation.
	src := `const note = "Do not use <Suspense here, it is only a mention";
export default function P() {
  return <div>{"<Suspense" + " in an expr container"}</div>;
}`
	mode, _ := detectRenderMode(parseTest(t, src))
	if mode != RenderSSG {
		t.Fatalf("expected RenderSSG for string mention only, got %v", mode)
	}
}

func TestDetectRenderModeStaticWithoutSuspense(t *testing.T) {
	src := `export default function P() {
  return <div>hello</div>;
}`
	mode, _ := detectRenderMode(parseTest(t, src))
	if mode != RenderSSG {
		t.Fatalf("expected RenderSSG (no Suspense), got %v", mode)
	}
}

func TestDetectRenderModeExplicitStreamingConfig(t *testing.T) {
	src := `export const config = { streaming: true };
export default function P() { return <div>x</div>; }`
	mode, _ := detectRenderMode(parseTest(t, src))
	if mode != RenderStreaming {
		t.Fatalf("expected RenderStreaming from explicit config, got %v", mode)
	}
}

func TestDetectRenderModeExplicitSSRConfig(t *testing.T) {
	src := `export const config = { ssr: true };
export default function P() { return <div>x</div>; }`
	mode, _ := detectRenderMode(parseTest(t, src))
	if mode != RenderSSR {
		t.Fatalf("expected RenderSSR from explicit config, got %v", mode)
	}
}

func TestDetectRenderModeExplicitISRConfig(t *testing.T) {
	src := `export const config = { isr: true, revalidate: 120 };
export default function P() { return <div>x</div>; }`
	mode, revalidate := detectRenderMode(parseTest(t, src))
	if mode != RenderISR {
		t.Fatalf("expected RenderISR from explicit config, got %v", mode)
	}
	if revalidate != 120 {
		t.Fatalf("expected revalidate=120, got %d", revalidate)
	}
}

func TestDetectRenderModeISRDefaultRevalidate(t *testing.T) {
	// isr without an explicit `revalidate` must fall back to 60s.
	src := `export const config = { isr: true };
export default function P() { return <div>x</div>; }`
	mode, revalidate := detectRenderMode(parseTest(t, src))
	if mode != RenderISR {
		t.Fatalf("expected RenderISR, got %v", mode)
	}
	if revalidate != defaultISRRevalidate {
		t.Fatalf("expected default revalidate, got %d", revalidate)
	}
}

func TestDetectRenderModeISRBeatsStreaming(t *testing.T) {
	// isr has highest precedence - a page can't be both.
	src := `export const config = { streaming: true, isr: true };
export default function P() { return <div>x</div>; }`
	mode, _ := detectRenderMode(parseTest(t, src))
	if mode != RenderISR {
		t.Fatalf("expected RenderISR to beat streaming config, got %v", mode)
	}
}
