package bundler

import (
	"strings"
	"testing"

	"krate-compiler/internal/ast"
	"krate-compiler/internal/lexer"
	"krate-compiler/internal/parser"
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

func TestRewriteReactRenamesUseEffect(t *testing.T) {
	src := `
		import { useEffect, useState } from 'react';
		export default function Page() {
			const [count, setCount] = useState(0);
			useEffect(() => { document.title = count; });
			return <div>{count()}</div>;
		}
	`
	prog := parseProg(t, src)
	RewriteReact(prog)

	foundEffect := false
	for _, stmt := range prog.Body {
		if exp, ok := stmt.(*ast.ExportStmt); ok {
			if fn, ok := exp.Declaration.(*ast.FnDecl); ok {
				for _, bodyStmt := range fn.Body {
					if es, ok := bodyStmt.(*ast.ExprStmt); ok {
						if call, ok := es.Expression.(*ast.CallExpr); ok {
							if id, ok := call.Callee.(*ast.Identifier); ok {
								if id.Name == "createEffect" {
									foundEffect = true
								}
								if id.Name == "useEffect" {
									t.Error("useEffect should be renamed to createEffect")
								}
							}
						}
					}
				}
			}
		}
	}
	if !foundEffect {
		t.Error("expected createEffect after rewrite")
	}
}

func TestRewriteReactTransformsUseRef(t *testing.T) {
	src := `
		import { useRef } from 'react';
		export default function Page() {
			const ref = useRef(0);
			return <div>{ref.current}</div>;
		}
	`
	prog := parseProg(t, src)
	RewriteReact(prog)

	foundObj := false
	for _, stmt := range prog.Body {
		if exp, ok := stmt.(*ast.ExportStmt); ok {
			if fn, ok := exp.Declaration.(*ast.FnDecl); ok {
				for _, bodyStmt := range fn.Body {
					if v, ok := bodyStmt.(*ast.VarStmt); ok {
						for _, decl := range v.Decls {
							if obj, ok := decl.Init.(*ast.ObjectExpr); ok {
								foundObj = true
								if len(obj.Properties) != 1 {
									t.Errorf("expected 1 property on object, got %d", len(obj.Properties))
								}
								if obj.Properties[0].Key != "current" {
									t.Errorf("expected key 'current', got %q", obj.Properties[0].Key)
								}
							}
							if call, ok := decl.Init.(*ast.CallExpr); ok {
								if id, ok := call.Callee.(*ast.Identifier); ok {
									if id.Name == "useRef" {
										t.Error("useRef should be transformed, not left as call")
									}
								}
							}
						}
					}
				}
			}
		}
	}
	if !foundObj {
		t.Error("expected object expression after useRef rewrite")
	}
}

func TestRewriteReactTransformsUseCallback(t *testing.T) {
	src := `
		import { useCallback } from 'react';
		export default function Page() {
			const cb = useCallback(() => { return 42; }, []);
			return <div>test</div>;
		}
	`
	prog := parseProg(t, src)
	RewriteReact(prog)

	foundArrow := false
	for _, stmt := range prog.Body {
		if exp, ok := stmt.(*ast.ExportStmt); ok {
			if fn, ok := exp.Declaration.(*ast.FnDecl); ok {
				for _, bodyStmt := range fn.Body {
					if v, ok := bodyStmt.(*ast.VarStmt); ok {
						for _, decl := range v.Decls {
							if _, ok := decl.Init.(*ast.ArrowFn); ok {
								foundArrow = true
							}
							if call, ok := decl.Init.(*ast.CallExpr); ok {
								if id, ok := call.Callee.(*ast.Identifier); ok {
									if id.Name == "useCallback" {
										t.Error("useCallback should be transformed")
									}
								}
							}
						}
					}
				}
			}
		}
	}
	if !foundArrow {
		t.Error("expected arrow function after useCallback rewrite")
	}
}

func TestRewriteReactRenamesUseEffectInNestedFn(t *testing.T) {
	src := `
		import { useEffect, useState } from 'react';
		function TimerDisplay() {
			const [seconds, setSeconds] = useState(0);
			useEffect(() => { clearInterval(); });
		}
		export default function Page() {
			return <TimerDisplay/>;
		}
	`
	prog := parseProg(t, src)
	RewriteReact(prog)

	// Verify the useEffect inside TimerDisplay was renamed to createEffect
	foundEffect := false
	for _, stmt := range prog.Body {
		if fn, ok := stmt.(*ast.FnDecl); ok && fn.Name == "TimerDisplay" {
			for _, bodyStmt := range fn.Body {
				if es, ok := bodyStmt.(*ast.ExprStmt); ok {
					if call, ok := es.Expression.(*ast.CallExpr); ok {
						if id, ok := call.Callee.(*ast.Identifier); ok {
							if id.Name == "createEffect" {
								foundEffect = true
							}
							if id.Name == "useEffect" {
								t.Error("useEffect should be renamed to createEffect inside TimerDisplay")
							}
						}
					}
				}
			}
		}
	}
	if !foundEffect {
		t.Error("expected createEffect inside TimerDisplay after rewrite")
	}

	// Also check that the import was removed
	for _, stmt := range prog.Body {
		if imp, ok := stmt.(*ast.ImportStmt); ok {
			if imp.Source == "'react'" {
				t.Error("react import should be removed")
			}
		}
	}
}

func TestRewriteReactNoReactImport(t *testing.T) {
	src := `
		import { createSignal } from './runtime.js';
		export default function Page() {
			const [x, setX] = createSignal(0);
			return <div>{x()}</div>;
		}
	`
	prog := parseProg(t, src)
	RewriteReact(prog)

	// Verify the import is preserved (not from 'react')
	foundImport := false
	for _, stmt := range prog.Body {
		if imp, ok := stmt.(*ast.ImportStmt); ok {
			if strings.Contains(imp.Source, "runtime") {
				foundImport = true
			}
			if strings.Contains(imp.Source, "react") {
				t.Error("no react import should be present")
			}
		}
	}
	if !foundImport {
		t.Error("expected non-react import to be preserved")
	}
}
