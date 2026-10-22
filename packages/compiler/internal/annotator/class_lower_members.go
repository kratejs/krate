package annotator

import (
	"strings"

	"github.com/kratejs/krate/packages/compiler/ast"
)

// classMemberLocal is the local binding used for a lowered class method,
// arrow field, or instance field. Class lowering turns `this.foo` into this
// identifier so rendered JSX and handlers close over ordinary locals.
func classMemberLocal(name string) string {
	var b strings.Builder
	b.WriteString("__krate_member_")
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	return b.String()
}

// collectClassMembers returns the instance member names that can be lowered,
// and reports whether the class has only supported members.
func collectClassMembers(class *ast.ClassDecl) (map[string]bool, bool) {
	members := map[string]bool{"forceUpdate": true}
	for _, m := range class.Members {
		if m == nil {
			continue
		}
		if m.Private {
			return nil, false
		}
		if m.Static {
			// defaultProps/propTypes/displayName are metadata; static lifecycle
			// methods change React semantics and are not lowered.
			if m.Kind == ast.ClassField && (m.Name == "defaultProps" || m.Name == "propTypes" || m.Name == "displayName") {
				continue
			}
			return nil, false
		}
		switch m.Kind {
		case ast.ClassMethod:
			switch m.Name {
			case "render", "componentDidMount", "componentWillUnmount", "constructor":
			case "shouldComponentUpdate", "getSnapshotBeforeUpdate",
				"getDerivedStateFromProps", "getDerivedStateFromError", "componentDidCatch":
				return nil, false
			default:
				members[m.Name] = true
			}
		case ast.ClassField:
			if m.Name != "state" && m.Name != "" {
				members[m.Name] = true
			}
		case ast.ClassConstructor:
		default:
			// Getters, setters, accessors and static blocks are not lowered.
			return nil, false
		}
	}
	// Constructor assignments create instance fields even when they have no
	// class field declaration.
	for _, m := range class.Members {
		if m == nil || m.Kind != ast.ClassConstructor {
			continue
		}
		collectThisAssignments(m.Body, members)
	}
	return members, true
}

func collectThisAssignments(stmts []ast.Stmt, members map[string]bool) {
	for _, stmt := range stmts {
		if stmt == nil {
			continue
		}
		switch s := stmt.(type) {
		case *ast.ExprStmt:
			if assign, ok := s.Expression.(*ast.BinaryExpr); ok && assign.Op == "=" {
				if mem, ok := assign.Left.(*ast.MemberExpr); ok && !mem.Computed {
					if _, ok := mem.Object.(*ast.ThisExpr); ok {
						if p, ok := mem.Property.(*ast.Identifier); ok && p.Name != "state" {
							members[p.Name] = true
						}
					}
				}
			}
		case *ast.IfStmt:
			collectThisAssignments(s.Consequent, members)
			collectThisAssignments(s.Alternate, members)
		case *ast.BlockStmt:
			collectThisAssignments(s.Body, members)
		case *ast.ForStmt:
			collectThisAssignments(s.Body, members)
		case *ast.WhileStmt:
			collectThisAssignments(s.Body, members)
		case *ast.DoWhileStmt:
			collectThisAssignments(s.Body, members)
		case *ast.SwitchStmt:
			for _, c := range s.Cases {
				collectThisAssignments(c.Body, members)
			}
		case *ast.TryStmt:
			collectThisAssignments(s.Body, members)
			if s.Catch != nil {
				collectThisAssignments(s.Catch.Body, members)
			}
			collectThisAssignments(s.Finally, members)
		}
	}
}

// replaceClassMemberRefs rewrites `this.<member>` to the local binding before
// the ordinary class lowering pass. It never touches state/props/setState.
func replaceClassMemberRefs(stmts []ast.Stmt, members map[string]bool) {
	for _, stmt := range stmts {
		if stmt == nil {
			continue
		}
		switch s := stmt.(type) {
		case *ast.ExprStmt:
			s.Expression = replaceClassMemberRefsExpr(s.Expression, members)
		case *ast.ReturnStmt:
			s.Value = replaceClassMemberRefsExpr(s.Value, members)
		case *ast.VarStmt:
			for _, d := range s.Decls {
				if d != nil {
					d.Init = replaceClassMemberRefsExpr(d.Init, members)
				}
			}
		case *ast.IfStmt:
			s.Test = replaceClassMemberRefsExpr(s.Test, members)
			replaceClassMemberRefs(s.Consequent, members)
			replaceClassMemberRefs(s.Alternate, members)
		case *ast.BlockStmt:
			replaceClassMemberRefs(s.Body, members)
		case *ast.ForStmt:
			if s.Init != nil {
				replaceClassMemberRefs([]ast.Stmt{s.Init}, members)
			}
			s.Test = replaceClassMemberRefsExpr(s.Test, members)
			s.Update = replaceClassMemberRefsExpr(s.Update, members)
			replaceClassMemberRefs(s.Body, members)
		case *ast.ForInStmt:
			s.Left = replaceClassMemberRefsExpr(s.Left, members)
			s.Right = replaceClassMemberRefsExpr(s.Right, members)
			replaceClassMemberRefs(s.Body, members)
		case *ast.WhileStmt:
			s.Test = replaceClassMemberRefsExpr(s.Test, members)
			replaceClassMemberRefs(s.Body, members)
		case *ast.DoWhileStmt:
			replaceClassMemberRefs(s.Body, members)
			s.Test = replaceClassMemberRefsExpr(s.Test, members)
		case *ast.SwitchStmt:
			s.Discriminant = replaceClassMemberRefsExpr(s.Discriminant, members)
			for _, c := range s.Cases {
				c.Test = replaceClassMemberRefsExpr(c.Test, members)
				replaceClassMemberRefs(c.Body, members)
			}
		case *ast.TryStmt:
			replaceClassMemberRefs(s.Body, members)
			if s.Catch != nil {
				replaceClassMemberRefs(s.Catch.Body, members)
			}
			replaceClassMemberRefs(s.Finally, members)
		}
	}
}

func replaceClassMemberRefsExpr(expr ast.Expr, members map[string]bool) ast.Expr {
	switch e := expr.(type) {
	case *ast.MemberExpr:
		if _, ok := e.Object.(*ast.ThisExpr); ok && !e.Computed {
			if p, ok := e.Property.(*ast.Identifier); ok && members[p.Name] && p.Name != "state" && p.Name != "props" && p.Name != "setState" {
				return &ast.Identifier{Name: classMemberLocal(p.Name)}
			}
			return e
		}
		e.Object = replaceClassMemberRefsExpr(e.Object, members)
		if e.Computed {
			e.Property = replaceClassMemberRefsExpr(e.Property, members)
		}
	case *ast.CallExpr:
		e.Callee = replaceClassMemberRefsExpr(e.Callee, members)
		for i := range e.Args {
			e.Args[i] = replaceClassMemberRefsExpr(e.Args[i], members)
		}
	case *ast.BinaryExpr:
		e.Left = replaceClassMemberRefsExpr(e.Left, members)
		e.Right = replaceClassMemberRefsExpr(e.Right, members)
	case *ast.UnaryExpr:
		e.Arg = replaceClassMemberRefsExpr(e.Arg, members)
	case *ast.ConditionalExpr:
		e.Test = replaceClassMemberRefsExpr(e.Test, members)
		e.Consequent = replaceClassMemberRefsExpr(e.Consequent, members)
		e.Alternate = replaceClassMemberRefsExpr(e.Alternate, members)
	case *ast.ArrayExpr:
		for i := range e.Elements {
			e.Elements[i] = replaceClassMemberRefsExpr(e.Elements[i], members)
		}
	case *ast.ObjectExpr:
		for _, p := range e.Properties {
			if p != nil {
				p.Value = replaceClassMemberRefsExpr(p.Value, members)
			}
		}
	case *ast.TemplateExpr:
		for i := range e.Parts {
			e.Parts[i] = replaceClassMemberRefsExpr(e.Parts[i], members)
		}
	case *ast.ArrowFn:
		replaceClassMemberRefs(e.Body, members)
	case *ast.JSXElement:
		if e.Opening != nil {
			for _, a := range e.Opening.Attributes {
				if a != nil {
					a.Value = replaceClassMemberRefsExpr(a.Value, members)
				}
			}
		}
		for _, c := range e.Children {
			switch ch := c.(type) {
			case *ast.JSXExprContainer:
				ch.Expression = replaceClassMemberRefsExpr(ch.Expression, members)
			case *ast.JSXElementChild:
				ch.Element = replaceClassMemberRefsExpr(ch.Element, members).(*ast.JSXElement)
			case *ast.JSXFragmentChild:
				ch.Fragment = replaceClassMemberRefsExpr(ch.Fragment, members).(*ast.JSXFragment)
			}
		}
	case *ast.JSXFragment:
		for _, c := range e.Children {
			switch ch := c.(type) {
			case *ast.JSXExprContainer:
				ch.Expression = replaceClassMemberRefsExpr(ch.Expression, members)
			case *ast.JSXElementChild:
				ch.Element = replaceClassMemberRefsExpr(ch.Element, members).(*ast.JSXElement)
			case *ast.JSXFragmentChild:
				ch.Fragment = replaceClassMemberRefsExpr(ch.Fragment, members).(*ast.JSXFragment)
			}
		}
	}
	return expr
}

// classMemberStatements lowers helper methods, arrow/instance fields, and
// constructor assignments to ordinary locals. It runs after the state signal
// is declared so field initializers can read this.state/this.props.
func classMemberStatements(class *ast.ClassDecl, members map[string]bool) []ast.Stmt {
	var out []ast.Stmt
	declared := map[string]bool{"forceUpdate": true}
	// Methods are available on the prototype in React, so declare them before
	// field initializers that may call them.
	for _, m := range class.Members {
		if m == nil || m.Kind != ast.ClassMethod || m.Static {
			continue
		}
		switch m.Name {
		case "render", "constructor", "componentDidMount", "componentWillUnmount":
			continue
		}
		local := classMemberLocal(m.Name)
		declared[m.Name] = true
		out = append(out, &ast.VarStmt{Kind: ast.VarLet, Decls: []*ast.VarDecl{{
			Name: local,
			Init: &ast.ArrowFn{Position: m.Position, Params: m.Params, Async: m.Async, Body: lowerClassStmts(m.Body)},
		}}})
	}
	out = append(out, &ast.VarStmt{Kind: ast.VarLet, Decls: []*ast.VarDecl{{
		Name: classMemberLocal("forceUpdate"),
		Init: &ast.ArrowFn{Body: []ast.Stmt{&ast.ExprStmt{Expression: &ast.CallExpr{
			Callee: &ast.Identifier{Name: classSetterName},
			Args: []ast.Expr{&ast.CallExpr{
				Callee: &ast.MemberExpr{Object: &ast.Identifier{Name: "Object"}, Property: &ast.Identifier{Name: "assign"}},
				Args:   []ast.Expr{&ast.ObjectExpr{}, &ast.CallExpr{Callee: &ast.Identifier{Name: classStateName}}},
			}},
		}}}},
	}}})
	for _, m := range class.Members {
		if m == nil || m.Kind != ast.ClassField || m.Static || m.Name == "state" || m.Name == "" {
			continue
		}
		declared[m.Name] = true
		decl := &ast.VarDecl{Name: classMemberLocal(m.Name)}
		if m.Init != nil {
			decl.Init = lowerClassExpr(m.Init)
		}
		out = append(out, &ast.VarStmt{Kind: ast.VarLet, Decls: []*ast.VarDecl{decl}})
	}
	for name := range members {
		if declared[name] || name == "state" {
			continue
		}
		out = append(out, &ast.VarStmt{Kind: ast.VarLet, Decls: []*ast.VarDecl{{Name: classMemberLocal(name)}}})
	}
	for _, m := range class.Members {
		if m == nil || m.Kind != ast.ClassConstructor {
			continue
		}
		for _, stmt := range m.Body {
			if constructorStmtHandled(stmt) {
				continue
			}
			out = append(out, lowerClassStmts([]ast.Stmt{stmt})...)
		}
	}
	return out
}

func constructorStmtHandled(stmt ast.Stmt) bool {
	es, ok := stmt.(*ast.ExprStmt)
	if !ok {
		return false
	}
	if call, ok := es.Expression.(*ast.CallExpr); ok {
		if id, ok := call.Callee.(*ast.Identifier); ok && id.Name == "super" {
			return true
		}
	}
	assign, ok := es.Expression.(*ast.BinaryExpr)
	if !ok || assign.Op != "=" {
		return false
	}
	if mem, ok := assign.Left.(*ast.MemberExpr); ok && !mem.Computed {
		if _, ok := mem.Object.(*ast.ThisExpr); ok {
			if p, ok := mem.Property.(*ast.Identifier); ok && p.Name == "state" {
				return true
			}
		}
	}
	// this.handler = this.handler.bind(this) is unnecessary once handlers are
	// ordinary local functions.
	if left, ok := assign.Left.(*ast.Identifier); ok {
		if call, ok := assign.Right.(*ast.CallExpr); ok {
			if mem, ok := call.Callee.(*ast.MemberExpr); ok && !mem.Computed {
				if id, ok := mem.Object.(*ast.Identifier); ok && id.Name == left.Name {
					if p, ok := mem.Property.(*ast.Identifier); ok && p.Name == "bind" {
						return true
					}
				}
			}
		}
	}
	return false
}

// classDidUpdateStatements lowers componentDidUpdate to an effect that diffs
// the state signal. Props do not update in the run-once model, so the current
// props are passed as prevProps.
func classDidUpdateStatements(m *ast.ClassMember) []ast.Stmt {
	if m == nil {
		return nil
	}
	prev := "__krate_class_prev_state"
	cur := &ast.CallExpr{Callee: &ast.Identifier{Name: classStateName}}
	effect := &ast.CallExpr{
		Callee: &ast.Identifier{Name: "createEffect"},
		Args: []ast.Expr{&ast.ArrowFn{Body: []ast.Stmt{
			&ast.IfStmt{
				Test:       &ast.BinaryExpr{Left: cur, Op: "===", Right: &ast.Identifier{Name: prev}},
				Consequent: []ast.Stmt{&ast.ReturnStmt{}},
			},
			&ast.VarStmt{Kind: ast.VarLet, Decls: []*ast.VarDecl{{Name: "__krate_class_prev", Init: &ast.Identifier{Name: prev}}}},
			&ast.ExprStmt{Expression: &ast.BinaryExpr{Left: &ast.Identifier{Name: prev}, Op: "=", Right: &ast.CallExpr{Callee: &ast.Identifier{Name: classStateName}}}},
			&ast.ExprStmt{Expression: &ast.CallExpr{
				Callee: &ast.ArrowFn{Position: m.Position, Params: m.Params, Async: m.Async, Body: lowerClassStmts(m.Body)},
				Args:   []ast.Expr{&ast.Identifier{Name: "props"}, &ast.Identifier{Name: "__krate_class_prev"}},
			}},
		}}},
	}
	return []ast.Stmt{
		&ast.VarStmt{Kind: ast.VarLet, Decls: []*ast.VarDecl{{Name: prev, Init: &ast.CallExpr{Callee: &ast.Identifier{Name: classStateName}}}}},
		&ast.ExprStmt{Expression: effect},
	}
}
