package irtree

import (
	"testing"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/lexer"
	"github.com/kratejs/krate/packages/compiler/internal/parser"
)

func parseModule(t *testing.T, src string) *ast.Program {
	t.Helper()
	p := parser.New(lexer.New(src).Tokenize())
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse: %v", errs)
	}
	return prog
}

func TestCollectCVAFactories(t *testing.T) {
	prog := parseModule(t, `
		const buttonVariants = cva("base", {
			variants: {
				variant: { default: "bg-blue", destructive: "bg-red" },
				size: { default: "h-9", lg: "h-10" },
			},
			defaultVariants: { variant: "default", size: "default" },
		});
	`)
	specs := CollectCVAFactories(prog)
	spec, ok := specs["buttonVariants"]
	if !ok {
		t.Fatal("buttonVariants not collected")
	}
	if spec.Base != "base" {
		t.Errorf("base = %q", spec.Base)
	}
	if spec.Variants["variant"]["destructive"] != "bg-red" {
		t.Errorf("variant.destructive = %q", spec.Variants["variant"]["destructive"])
	}
	if spec.Defaults["size"] != "default" {
		t.Errorf("defaults.size = %q", spec.Defaults["size"])
	}
}

func TestCVASpecFold(t *testing.T) {
	prog := parseModule(t, `
		const b = cva("base", {
			variants: {
				variant: { default: "bg-blue", destructive: "bg-red" },
				size: { default: "h-9", lg: "h-10" },
			},
			defaultVariants: { variant: "default", size: "default" },
		});
	`)
	spec := CollectCVAFactories(prog)["b"]

	// All defaults (variant groups are applied in sorted name order).
	if got := spec.Fold(map[string]string{}, ""); got != "base h-9 bg-blue" {
		t.Errorf("defaults fold = %q", got)
	}
	// Explicit selection overrides defaults.
	if got := spec.Fold(map[string]string{"variant": "destructive", "size": "lg"}, ""); got != "base h-10 bg-red" {
		t.Errorf("selection fold = %q", got)
	}
	// Extra class appended last.
	if got := spec.Fold(map[string]string{}, "custom"); got != "base h-9 bg-blue custom" {
		t.Errorf("extra class fold = %q", got)
	}
}

func TestCVASpecCompoundVariants(t *testing.T) {
	prog := parseModule(t, `
		const b = cva("base", {
			variants: {
				variant: { default: "bg-blue", destructive: "bg-red" },
				size: { default: "h-9", lg: "h-10" },
			},
			compoundVariants: [
				{ variant: "destructive", size: "lg", class: "font-bold" },
			],
			defaultVariants: { variant: "default", size: "default" },
		});
	`)
	spec := CollectCVAFactories(prog)["b"]

	if got := spec.Fold(map[string]string{"variant": "destructive", "size": "lg"}, ""); got != "base h-10 bg-red font-bold" {
		t.Errorf("compound match fold = %q", got)
	}
	if got := spec.Fold(map[string]string{"variant": "destructive", "size": "default"}, ""); got != "base h-9 bg-red" {
		t.Errorf("compound non-match fold = %q", got)
	}
}

func TestDestructuredParamNames(t *testing.T) {
	got := destructuredParamNames("{ className, variant, size, asChild = false, ...props }")
	want := map[string]bool{"className": true, "variant": true, "size": true, "asChild": true, "props": true}
	if len(got) != len(want) {
		t.Fatalf("names = %v, want %v", got, want)
	}
	for _, n := range got {
		if !want[n] {
			t.Errorf("unexpected name %q in %v", n, got)
		}
	}
}

func TestDestructuredParamDefault(t *testing.T) {
	def := destructuredParamDefault("{ a, b = false, c = 'x' }", "b")
	if def == nil {
		t.Fatal("expected a default for b")
	}
	lit, ok := def.(*ast.Literal)
	if !ok || lit.Value != "false" {
		t.Errorf("b default = %v", def)
	}
	if destructuredParamDefault("{ a, b }", "a") != nil {
		t.Error("a should have no default")
	}
}

func TestFnRestParamName(t *testing.T) {
	prog := parseModule(t, `function B({ a, ...rest }) { return <div/> }`)
	fn := prog.Body[0].(*ast.FnDecl)
	if got := fnRestParamName(fn); got != "rest" {
		t.Errorf("rest name = %q", got)
	}
}
