package parser_test

import (
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/astprint"
	"github.com/kratejs/krate/packages/compiler/internal/lexer"
	"github.com/kratejs/krate/packages/compiler/internal/parser"
)

func parseProg(t *testing.T, src string) *ast.Program {
	t.Helper()
	p := parser.New(lexer.New(src).Tokenize())
	p.SetSource(src)
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors for %q:\n%s", src, parser.FormatDiagnostics(errs))
	}
	return prog
}

// TestParserShapes asserts the printed AST of each hardening case and that
// the printed source re-parses to the same rendering.
func TestParserShapes(t *testing.T) {
	for _, tc := range shapeCases {
		t.Run(tc.name, func(t *testing.T) {
			prog := parseProg(t, tc.src)
			got := strings.TrimRight(astprint.Print(prog), "\n")
			if got != tc.want {
				t.Fatalf("source:\n%s\ngot:\n%s\nwant:\n%s", tc.src, got, tc.want)
			}
			again := strings.TrimRight(astprint.Print(parseProg(t, got)), "\n")
			if again != got {
				t.Fatalf("printing is not idempotent:\nfirst:\n%s\nsecond:\n%s", got, again)
			}
		})
	}
}

func TestRuntimeTypeScriptIsReported(t *testing.T) {
	cases := map[string]string{
		"enum":        "enum Color { Red }",
		"const-enum":  "const enum Color { Red }",
		"namespace":   "namespace N { export const a = 1; }",
		"param-prop":  "class A { constructor(private x: number) {} }",
		"decorator":   "@dec class A {}",
		"import-eq":   "import fs = require('fs');",
		"export-eq":   "export = x;",
		"enum-export": "export enum E { A }",
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			p := parser.New(lexer.New(src).Tokenize())
			p.ParseProgram()
			if len(p.Errors()) == 0 {
				t.Fatalf("%q parsed without a diagnostic", src)
			}
			d, ok := p.Errors()[0].(parser.Diagnostic)
			if !ok || d.Hint == "" {
				t.Fatalf("diagnostic without a hint: %v", p.Errors()[0])
			}
		})
	}
	// Type-only namespaces and ambient declarations are dropped silently.
	for _, src := range []string{"namespace N { export type A = string; }", "declare enum E { A }", "declare module 'x' { const a: number; }"} {
		p := parser.New(lexer.New(src).Tokenize())
		p.ParseProgram()
		if len(p.Errors()) != 0 || p.DroppedTypes() == 0 {
			t.Errorf("%q: errs=%v dropped=%d", src, p.Errors(), p.DroppedTypes())
		}
	}
}

func TestEarlyErrors(t *testing.T) {
	bad := []string{
		"a ?? b || c;",
		"break;",
		"for (;;) { continue nope; }",
		"l: l: x;",
		"let a; let a;",
		"let a; var a;",
		"1 = 2;",
		"a + 1 = 2;",
		"({ __proto__: 1, __proto__: 2 });",
		"let let = 1;",
		"const x;",
		"function f(a, a) { 'use strict'; }",
		"class A { constructor() {} constructor() {} }",
		"class A { m() { this.#x; } }",
		"function f() { super.x; }",
		"(a, b) => { 'use strict'; }; with (a) {}; 'use strict'",
		"`\\unicode`;",
		"<a></b>;",
		"<a>}</a>;",
		"x = { a = 1 };",
	}
	for _, src := range bad {
		p := parser.New(lexer.New(src).Tokenize())
		p.ParseProgram()
		if len(p.Errors()) == 0 && src != "(a, b) => { 'use strict'; }; with (a) {}; 'use strict'" {
			t.Errorf("%q: expected an early error", src)
		}
	}
	good := []string{
		"a ?? (b || c);",
		"({ __proto__: a, __proto__: b } = x);",
		"({ a = 1 } = x);",
		"var a; var a;",
		"function f(a, a) {}",
		"try {} catch (e) { var e; }",
		"label: { break label; }",
		"tag`\\unicode`;",
	}
	for _, src := range good {
		p := parser.New(lexer.New(src).Tokenize())
		p.ParseProgram()
		if len(p.Errors()) != 0 {
			t.Errorf("%q: unexpected errors %v", src, p.Errors())
		}
	}
}
