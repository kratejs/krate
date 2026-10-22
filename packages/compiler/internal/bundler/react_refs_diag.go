package bundler

import (
	"reflect"
	"strings"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/diag"
)

// refDiagnostics reports ref patterns Krate cannot honour, on the source AST
// before lowering, so every component shape (function, arrow, class) is seen:
// string refs (KR5003) and ref.current read during render (KR5008).
func (c *reactCtx) refDiagnostics(prog *ast.Program) {
	// Cheap source gates keep the reflective walk off modules that cannot
	// contain either pattern (most of node_modules).
	stringRefs := strings.Contains(c.src, `ref="`) || strings.Contains(c.src, `ref='`)
	renderReads := strings.Contains(c.src, "useRef") && strings.Contains(c.src, ".current")
	// Third-party packages cannot act on these hints (their diagnostics are
	// dropped), so skip the walk there.
	if (!stringRefs && !renderReads) || strings.Contains(strings.ReplaceAll(c.file, "\\", "/"), "node_modules/") {
		return
	}
	walkAST(reflect.ValueOf(prog), func(n any) bool {
		switch x := n.(type) {
		case *ast.JSXAttr:
			if x.Name == "ref" {
				if lit, ok := x.Value.(*ast.Literal); ok && lit.Kind == ast.StringLit {
					c.warn(x.Position, diag.CodeStringRef,
						"string ref \""+lit.Value+"\" is not supported (removed in React 19) and is ignored",
						"create a ref with useRef(null) (or createRef() in a class) and pass it as ref={myRef}, or pass a callback ref={(node) => ...}")
				}
			}
		case *ast.FnDecl:
			if renderReads {
				c.renderRefReads(x.Body)
			}
		case *ast.ArrowFn:
			if renderReads {
				c.renderRefReads(x.Body)
			}
		}
		return true
	})
}

// renderRefReads reports `r.current` read in the returned JSX of a function
// body that declares r with useRef. Reads inside nested functions (handlers,
// effects, callback refs) are fine.
func (c *reactCtx) renderRefReads(body []ast.Stmt) {
	refs := map[string]bool{}
	var returns []ast.Expr
	for _, stmt := range body {
		switch s := stmt.(type) {
		case *ast.VarStmt:
			for _, d := range s.Decls {
				if d == nil || d.Name == "" {
					continue
				}
				if call, ok := d.Init.(*ast.CallExpr); ok && isUseRefCallee(call.Callee) {
					refs[d.Name] = true
				}
			}
		case *ast.ReturnStmt:
			if s.Value != nil {
				returns = append(returns, s.Value)
			}
		}
	}
	if len(refs) == 0 {
		return
	}
	reported := map[string]bool{}
	for _, ret := range returns {
		walkAST(reflect.ValueOf(ret), func(n any) bool {
			switch x := n.(type) {
			case *ast.ArrowFn, *ast.FnDecl, *ast.ClassDecl, *ast.ClassExpr:
				return false
			case *ast.JSXAttr:
				// ref={r} passes the ref object; it does not read it.
				if x.Name == "ref" {
					return false
				}
			case *ast.MemberExpr:
				obj, ok := x.Object.(*ast.Identifier)
				prop, ok2 := x.Property.(*ast.Identifier)
				if ok && ok2 && !x.Computed && refs[obj.Name] && prop.Name == "current" && !reported[obj.Name] {
					reported[obj.Name] = true
					c.warn(x.Position, diag.CodeRefReadInRender,
						"`"+obj.Name+".current` is read during render; Krate renders it once and never refreshes it",
						"keep values the UI shows in state (useState), or read the ref in an effect or event handler")
				}
			}
			return true
		})
	}
}

func isUseRefCallee(e ast.Expr) bool {
	switch x := e.(type) {
	case *ast.Identifier:
		return x.Name == "useRef"
	case *ast.MemberExpr:
		p, ok := x.Property.(*ast.Identifier)
		return ok && p.Name == "useRef"
	}
	return false
}

var (
	astNodeType = reflect.TypeOf((*ast.Node)(nil)).Elem()
	jsxAttrType = reflect.TypeOf((*ast.JSXAttr)(nil))
)

// walkAST visits every AST node reachable from v depth-first. visit returns
// false to skip a node's children.
func walkAST(v reflect.Value, visit func(any) bool) {
	switch v.Kind() {
	case reflect.Interface:
		if !v.IsNil() {
			walkAST(v.Elem(), visit)
		}
	case reflect.Pointer:
		if v.IsNil() {
			return
		}
		if v.Type().Implements(astNodeType) || v.Type() == jsxAttrType {
			if !visit(v.Interface()) {
				return
			}
		}
		walkAST(v.Elem(), visit)
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			if v.Type().Field(i).IsExported() {
				walkAST(v.Field(i), visit)
			}
		}
	case reflect.Slice:
		for i := 0; i < v.Len(); i++ {
			walkAST(v.Index(i), visit)
		}
	}
}
