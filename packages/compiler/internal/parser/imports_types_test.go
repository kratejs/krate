package parser

import (
	"testing"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/lexer"
)

func parseOK(t *testing.T, src string) *ast.Program {
	t.Helper()
	p := New(lexer.New(src).Tokenize())
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors for %q: %v", src, errs)
	}
	return prog
}

// TestImportInlineTypeSpecifier covers `import { cva, type VariantProps }`,
// which shadcn/radix components use heavily. The type-only specifier is
// dropped; the value import is kept.
func TestImportInlineTypeSpecifier(t *testing.T) {
	prog := parseOK(t, `import { cva, type VariantProps } from 'class-variance-authority';`)
	if len(prog.Body) != 1 {
		t.Fatalf("expected 1 statement, got %d", len(prog.Body))
	}
	imp, ok := prog.Body[0].(*ast.ImportStmt)
	if !ok {
		t.Fatalf("expected ImportStmt, got %T", prog.Body[0])
	}
	if len(imp.Named) != 1 || imp.Named[0].Remote != "cva" {
		t.Fatalf("expected only `cva`, got %+v", imp.Named)
	}
}

func TestImportTypeOnlySpecifier(t *testing.T) {
	prog := parseOK(t, `import { type ClassValue, clsx } from 'clsx';`)
	imp := prog.Body[0].(*ast.ImportStmt)
	if len(imp.Named) != 1 || imp.Named[0].Remote != "clsx" {
		t.Fatalf("expected only `clsx`, got %+v", imp.Named)
	}
}

func TestImportTypeAsAliasSpecifier(t *testing.T) {
	prog := parseOK(t, `import { type Foo as Bar, baz } from 'x';`)
	imp := prog.Body[0].(*ast.ImportStmt)
	if len(imp.Named) != 1 || imp.Named[0].Remote != "baz" {
		t.Fatalf("expected only `baz`, got %+v", imp.Named)
	}
}

// TestInterfaceExtends covers `interface X extends A, B<C> { ... }`, whose
// heritage clause previously broke the interface-body parse.
func TestInterfaceExtends(t *testing.T) {
	src := `
		interface ButtonProps extends React.ButtonHTMLAttributes<HTMLButtonElement>, VariantProps<typeof buttonVariants> {
			asChild?: boolean;
		}
		const x = 1;
	`
	prog := parseOK(t, src)
	found := false
	for _, stmt := range prog.Body {
		if vs, ok := stmt.(*ast.VarStmt); ok {
			for _, d := range vs.Decls {
				if d.Name == "x" {
					found = true
				}
			}
		}
	}
	if !found {
		t.Fatalf("statement after an `extends` interface was not parsed; body=%d stmts", len(prog.Body))
	}
}

// TestNamedFunctionExpression covers `__name(function Foo() {...}, "Foo")`, the
// shape compiled library output (Radix UI) uses.
func TestNamedFunctionExpression(t *testing.T) {
	prog := parseOK(t, `
		var __name = (t, v) => Object.defineProperty(t, "name", { value: v });
		var Bar = __name(function Bar2(props) { return <span>{props.children}</span>; }, "Bar");
	`)
	if len(prog.Body) != 2 {
		t.Fatalf("expected 2 statements, got %d", len(prog.Body))
	}
}

func TestNamedFunctionExpressionBare(t *testing.T) {
	parseOK(t, `const f = function named(a) { return a; };`)
	parseOK(t, `const g = function (a) { return a; };`)
	parseOK(t, `const h = async function named(a) { return a; };`)
}

// TestExportStarAs covers `export * as Name from 'source'`.
func TestExportStarAs(t *testing.T) {
	prog := parseOK(t, `export * as Card from './card';`)
	if len(prog.Body) != 1 {
		t.Fatalf("expected 1 statement, got %d", len(prog.Body))
	}
	exp, ok := prog.Body[0].(*ast.ExportStmt)
	if !ok {
		t.Fatalf("expected ExportStmt, got %T", prog.Body[0])
	}
	if exp.Namespace != "Card" || !exp.StarReexport || exp.ReexportSource != "'./card'" {
		t.Fatalf("unexpected export: namespace=%q star=%v source=%q", exp.Namespace, exp.StarReexport, exp.ReexportSource)
	}
}

func TestInterfaceExtendsGeneric(t *testing.T) {
	prog := parseOK(t, `
		interface A<T> extends B<T>, C { value: T }
		const after = 2;
	`)
	if len(prog.Body) != 1 {
		t.Fatalf("expected only the const after type erasure, got %d stmts", len(prog.Body))
	}
}
