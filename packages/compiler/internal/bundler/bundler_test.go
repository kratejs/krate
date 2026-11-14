package bundler

import (
	"testing"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/lexer"
	"github.com/kratejs/krate/packages/compiler/internal/parser"
)

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

func TestRewriteReactRenamesUseState(t *testing.T) {
	src := `
		import { useState } from 'react';
		export default function Page() {
			const [count, setCount] = useState(0);
			return <div>{count()}</div>;
		}
	`
	prog := parseProg(t, src)

	// Verify import exists before rewrite
	foundImport := false
	for _, stmt := range prog.Body {
		if imp, ok := stmt.(*ast.ImportStmt); ok {
			if imp.Source == "'react'" {
				foundImport = true
			}
		}
	}
	if !foundImport {
		t.Fatal("expected react import before rewrite")
	}

	RewriteReact(prog)

	// Verify import is removed
	for _, stmt := range prog.Body {
		if imp, ok := stmt.(*ast.ImportStmt); ok {
			if imp.Source == "'react'" {
				t.Error("react import should be removed after rewrite")
			}
		}
	}

	// Verify useState is renamed to createSignal
	foundCreateSignal := false
	for _, stmt := range prog.Body {
		if exp, ok := stmt.(*ast.ExportStmt); ok {
			if fn, ok := exp.Declaration.(*ast.FnDecl); ok {
				for _, bodyStmt := range fn.Body {
					if v, ok := bodyStmt.(*ast.VarStmt); ok {
						for _, decl := range v.Decls {
							if call, ok := decl.Init.(*ast.CallExpr); ok {
								if id, ok := call.Callee.(*ast.Identifier); ok {
									if id.Name == "createSignal" {
										foundCreateSignal = true
									}
									if id.Name == "useState" {
										t.Error("useState should be renamed to createSignal")
									}
								}
							}
						}
					}
				}
			}
		}
	}
	if !foundCreateSignal {
		t.Error("expected createSignal after rewrite")
	}
}
