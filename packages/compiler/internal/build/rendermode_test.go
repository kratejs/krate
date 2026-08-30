package build

import (
	"testing"

	"krate-compiler/internal/lexer"
	"krate-compiler/internal/parser"
)

func TestDetectRenderModeSuspenseAutoStreaming(t *testing.T) {
	// A page that uses <Suspense> but has no explicit
	// `export const config = { streaming: true }` must still be detected as
	// streaming — the resolved fallback is swapped in per request.
	src := `import { Suspense } from '@krate/runtime/server';
export default function P() {
  return <Suspense fallback={<span>loading</span>}><section>x</section></Suspense>;
}`
	l := lexer.New(src)
	p := parser.New(l.Tokenize())
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	mode, _ := detectRenderMode(prog, src)
	if mode != RenderStreaming {
		t.Fatalf("expected RenderStreaming from <Suspense> usage, got %v", mode)
	}
}

func TestDetectRenderModeStaticWithoutSuspense(t *testing.T) {
	src := `export default function P() {
  return <div>hello</div>;
}`
	l := lexer.New(src)
	p := parser.New(l.Tokenize())
	prog := p.ParseProgram()
	mode, _ := detectRenderMode(prog, src)
	if mode != RenderSSG {
		t.Fatalf("expected RenderSSG (no Suspense), got %v", mode)
	}
}

func TestDetectRenderModeExplicitStreamingConfig(t *testing.T) {
	src := `export const config = { streaming: true };
export default function P() { return <div>x</div>; }`
	l := lexer.New(src)
	p := parser.New(l.Tokenize())
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	mode, _ := detectRenderMode(prog, src)
	if mode != RenderStreaming {
		t.Fatalf("expected RenderStreaming from explicit config, got %v", mode)
	}
}
