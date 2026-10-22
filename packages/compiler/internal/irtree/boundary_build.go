package irtree

import "github.com/kratejs/krate/packages/compiler/ast"

// Build-time error boundary fallback.
//
// When a component inside a lowered error boundary throws while the page is
// built (renderer BuildThrow), the build re-runs the IR with that boundary
// instance in BuildOptions.BoundaryThrows. The instance is then built from a
// copy of the lowered class whose initial state includes
// getDerivedStateFromError(error) and whose runtime spec carries the error
// (componentDidCatch runs at hydration), with its call-site children dropped:
// they cannot render, and React would have unmounted them.

// Names emitted by the class lowering (annotator/class_lower.go).
const (
	loweredClassState   = "__krate_class_state"
	loweredClassRuntime = "__krateClass"
	loweredBuildError   = "__krate_class_build_error"
)

// boundaryFallback returns the fallback build of fn for instance id, or ok=false.
func (b *builder) boundaryFallback(fn *ast.FnDecl, id SlotID) (*ast.FnDecl, bool) {
	t, ok := b.boundaryThrows[id]
	if !ok || fn == nil {
		return nil, false
	}
	errObj := &ast.ObjectExpr{Properties: []*ast.ObjectProp{
		{Key: "name", Value: &ast.Literal{Kind: ast.StringLit, Value: errorName(t.Name)}},
		{Key: "message", Value: &ast.Literal{Kind: ast.StringLit, Value: t.Message}},
	}}
	var derive ast.Expr
	body := make([]ast.Stmt, len(fn.Body))
	copy(body, fn.Body)
	// The spec comes after the state signal; find getDerivedStateFromError first.
	for i, stmt := range body {
		if spec, call := classSpecOf(stmt); spec != nil {
			for _, p := range spec.Properties {
				if p != nil && p.Key == "getDerivedStateFromError" {
					derive = p.Value
				}
			}
			specCopy := *spec
			specCopy.Properties = append(append([]*ast.ObjectProp(nil), spec.Properties...),
				&ast.ObjectProp{Key: "buildError", Value: errObj})
			callCopy := *call
			callCopy.Args = append(append([]ast.Expr(nil), call.Args[:3]...), &specCopy)
			body[i] = replaceVarInit(stmt, &callCopy)
		}
	}
	if derive != nil {
		for i, stmt := range body {
			init, ok := classStateInit(stmt)
			if !ok {
				continue
			}
			// The error is a local so the initializer is emitted as an
			// expression the client evaluates (like getDerivedStateFromProps).
			next := &ast.CallExpr{
				Callee: &ast.MemberExpr{Object: &ast.Identifier{Name: "Object"}, Property: &ast.Identifier{Name: "assign"}},
				Args:   []ast.Expr{&ast.ObjectExpr{}, init, &ast.CallExpr{Callee: derive, Args: []ast.Expr{&ast.Identifier{Name: loweredBuildError}}}},
			}
			decl := &ast.VarStmt{Kind: ast.VarConst, Decls: []*ast.VarDecl{{Name: loweredBuildError, Init: errObj}}}
			body[i] = replaceVarInit(stmt, &ast.CallExpr{Callee: &ast.Identifier{Name: "createSignal"}, Args: []ast.Expr{next}})
			body = append(body[:i], append([]ast.Stmt{decl}, body[i:]...)...)
			break
		}
	}
	out := *fn
	out.Body = body
	return &out, true
}

func errorName(name string) string {
	if name == "" {
		return "Error"
	}
	return name
}

// classSpecOf matches `var __krate_this = __krateClass(props, s, set, {spec})`.
func classSpecOf(stmt ast.Stmt) (*ast.ObjectExpr, *ast.CallExpr) {
	vs, ok := stmt.(*ast.VarStmt)
	if !ok || len(vs.Decls) != 1 || vs.Decls[0] == nil {
		return nil, nil
	}
	call, ok := vs.Decls[0].Init.(*ast.CallExpr)
	if !ok || len(call.Args) != 4 {
		return nil, nil
	}
	if id, ok := call.Callee.(*ast.Identifier); !ok || id.Name != loweredClassRuntime {
		return nil, nil
	}
	spec, ok := call.Args[3].(*ast.ObjectExpr)
	if !ok {
		return nil, nil
	}
	return spec, call
}

// classStateInit returns X for `const [__krate_class_state, ...] = createSignal(X)`.
func classStateInit(stmt ast.Stmt) (ast.Expr, bool) {
	vs, ok := stmt.(*ast.VarStmt)
	if !ok || len(vs.Decls) != 1 || vs.Decls[0] == nil {
		return nil, false
	}
	d := vs.Decls[0]
	if len(d.Names) == 0 || d.Names[0] != loweredClassState {
		return nil, false
	}
	call, ok := d.Init.(*ast.CallExpr)
	if !ok || len(call.Args) != 1 {
		return nil, false
	}
	return call.Args[0], true
}

func replaceVarInit(stmt ast.Stmt, init ast.Expr) ast.Stmt {
	vs := stmt.(*ast.VarStmt)
	c := *vs
	d := *vs.Decls[0]
	d.Init = init
	c.Decls = []*ast.VarDecl{&d}
	return &c
}
