package annotator

import "github.com/kratejs/krate/packages/compiler/ast"

// classStateName / classSetterName are the synthetic bindings a lowered class
// component uses for `this.state` and `this.setState`.
const (
	classStateName  = "__krate_class_state"
	classSetterName = "__krate_class_setState"
)

// synthesizeClassComponent lowers a React class component into an ordinary
// Krate function component for the supported subset:
//
//   - `state` (field or constructor assignment) becomes a signal;
//   - `this.state.X` / `this.props.X` / `this.setState(...)` are rewritten;
//   - componentDidMount / componentDidUpdate / componentWillUnmount become
//     onMount / a state-diff effect / onCleanup;
//   - helper methods, arrow handlers, instance fields, and forceUpdate become
//     ordinary locals;
//   - render() becomes the component body.
//
// It returns ok=false for classes it cannot lower faithfully (derived state,
// shouldComponentUpdate, snapshots, error boundaries, static members), leaving
// the class to the specific build diagnostic.
func synthesizeClassComponent(class *ast.ClassDecl) (*ast.FnDecl, bool) {
	if class == nil || class.Name == "" || !isReactClassBase(class.SuperClass) {
		return nil, false
	}

	members, ok := collectClassMembers(class)
	if !ok {
		return nil, false
	}

	var render, didMount, didUpdate, willUnmount *ast.ClassMember
	var stateExpr ast.Expr
	for _, m := range class.Members {
		if m == nil {
			continue
		}
		if m.Kind == ast.ClassMethod {
			switch m.Name {
			case "render":
				render = m
			case "componentDidMount":
				didMount = m
			case "componentWillUnmount":
				willUnmount = m
			case "componentDidUpdate":
				didUpdate = m
			case "constructor":
				// Constructor state assignments are handled below.
			case "shouldComponentUpdate", "getSnapshotBeforeUpdate",
				"getDerivedStateFromProps", "getDerivedStateFromError", "componentDidCatch":
				return nil, false
			}
			continue
		}
		if m.Kind == ast.ClassField && m.Name == "state" {
			stateExpr = m.Init
		}
	}

	for _, m := range class.Members {
		if m == nil {
			continue
		}
		switch m.Kind {
		case ast.ClassMethod:
			replaceClassMemberRefs(m.Body, members)
		case ast.ClassConstructor:
			replaceClassMemberRefs(m.Body, members)
		case ast.ClassField:
			if m.Init != nil {
				m.Init = replaceClassMemberRefsExpr(m.Init, members)
			}
		}
	}

	if render == nil {
		return nil, false
	}
	// Reject this.<other> access anywhere in the lowered bodies (methods that
	// would not exist on the synthesized component).
	if classUsesUnsupportedThis(render.Body) || classUsesUnsupportedThis(lifecycleBody(didMount)) || classUsesUnsupportedThis(lifecycleBody(didUpdate)) || classUsesUnsupportedThis(lifecycleBody(willUnmount)) {
		return nil, false
	}

	// Constructor `this.state = expr` fallback when no state field is present.
	if stateExpr == nil {
		stateExpr = constructorStateInit(class)
	}
	if stateExpr == nil {
		stateExpr = &ast.ObjectExpr{}
	}

	body := make([]ast.Stmt, 0, 4)
	body = append(body, &ast.VarStmt{
		Kind: ast.VarConst,
		Decls: []*ast.VarDecl{{
			IsDestructuring: true,
			Names:           []string{classStateName, classSetterName},
			Init: &ast.CallExpr{
				Callee: &ast.Identifier{Name: "createSignal"},
				Args:   []ast.Expr{stateExpr},
			},
		}},
	})
	body = append(body, classMemberStatements(class, members)...)
	if didMount != nil {
		body = append(body, &ast.ExprStmt{Expression: &ast.CallExpr{
			Callee: &ast.Identifier{Name: "onMount"},
			Args:   []ast.Expr{&ast.ArrowFn{Body: lowerClassStmts(didMount.Body)}},
		}})
	}
	body = append(body, classDidUpdateStatements(didUpdate)...)
	if willUnmount != nil {
		body = append(body, &ast.ExprStmt{Expression: &ast.CallExpr{
			Callee: &ast.Identifier{Name: "onCleanup"},
			Args:   []ast.Expr{&ast.ArrowFn{Body: lowerClassStmts(willUnmount.Body)}},
		}})
	}
	body = append(body, lowerClassStmts(render.Body)...)

	return &ast.FnDecl{
		Position: class.Position,
		Name:     class.Name,
		Params:   []*ast.Param{{Name: "props"}},
		Body:     body,
	}, true
}

func lifecycleBody(m *ast.ClassMember) []ast.Stmt {
	if m == nil {
		return nil
	}
	return m.Body
}

// constructorStateInit extracts a simple `this.state = <object>` assignment
// from a constructor body.
func constructorStateInit(class *ast.ClassDecl) ast.Expr {
	for _, m := range class.Members {
		if m == nil || m.Kind != ast.ClassConstructor {
			continue
		}
		for _, stmt := range m.Body {
			es, ok := stmt.(*ast.ExprStmt)
			if !ok {
				continue
			}
			assign, ok := es.Expression.(*ast.BinaryExpr)
			if !ok || assign.Op != "=" {
				continue
			}
			mem, ok := assign.Left.(*ast.MemberExpr)
			if !ok || mem.Computed {
				continue
			}
			if _, ok := mem.Object.(*ast.ThisExpr); !ok {
				continue
			}
			if p, ok := mem.Property.(*ast.Identifier); ok && p.Name == "state" {
				return assign.Right
			}
		}
	}
	return nil
}

// classUsesUnsupportedThis reports whether statements read `this.<x>` for any x
// other than props/state/setState.
func classUsesUnsupportedThis(stmts []ast.Stmt) bool {
	for _, stmt := range stmts {
		if stmt == nil {
			continue
		}
		if exprUsesUnsupportedThis(stmtExpr(stmt)) {
			return true
		}
		switch s := stmt.(type) {
		case *ast.IfStmt:
			if classUsesUnsupportedThis(s.Consequent) || classUsesUnsupportedThis(s.Alternate) {
				return true
			}
		case *ast.BlockStmt:
			if classUsesUnsupportedThis(s.Body) {
				return true
			}
		case *ast.ForStmt:
			if classUsesUnsupportedThis(s.Body) {
				return true
			}
		case *ast.ForInStmt:
			if classUsesUnsupportedThis(s.Body) {
				return true
			}
		case *ast.WhileStmt:
			if classUsesUnsupportedThis(s.Body) {
				return true
			}
		case *ast.DoWhileStmt:
			if classUsesUnsupportedThis(s.Body) {
				return true
			}
		case *ast.SwitchStmt:
			for _, c := range s.Cases {
				if classUsesUnsupportedThis(c.Body) {
					return true
				}
			}
		case *ast.TryStmt:
			if classUsesUnsupportedThis(s.Body) || (s.Catch != nil && classUsesUnsupportedThis(s.Catch.Body)) || classUsesUnsupportedThis(s.Finally) {
				return true
			}
		}
	}
	return false
}

func stmtExpr(stmt ast.Stmt) ast.Expr {
	switch s := stmt.(type) {
	case *ast.ExprStmt:
		return s.Expression
	case *ast.ReturnStmt:
		return s.Value
	case *ast.VarStmt:
		var out ast.Expr
		for _, d := range s.Decls {
			if d != nil && d.Init != nil {
				out = d.Init
			}
		}
		return out
	}
	return nil
}

func exprUsesUnsupportedThis(expr ast.Expr) bool {
	if expr == nil {
		return false
	}
	switch e := expr.(type) {
	case *ast.MemberExpr:
		if _, ok := e.Object.(*ast.ThisExpr); ok && !e.Computed {
			if p, ok := e.Property.(*ast.Identifier); ok {
				switch p.Name {
				case "props", "state", "setState":
					return false
				}
			}
			return true
		}
		if exprUsesUnsupportedThis(e.Object) {
			return true
		}
		if e.Computed {
			return exprUsesUnsupportedThis(e.Property)
		}
		return false
	case *ast.CallExpr:
		if exprUsesUnsupportedThis(e.Callee) {
			return true
		}
		for _, a := range e.Args {
			if exprUsesUnsupportedThis(a) {
				return true
			}
		}
	case *ast.ArrowFn:
		return classUsesUnsupportedThis(e.Body)
	case *ast.BinaryExpr:
		return exprUsesUnsupportedThis(e.Left) || exprUsesUnsupportedThis(e.Right)
	case *ast.UnaryExpr:
		return exprUsesUnsupportedThis(e.Arg)
	case *ast.ConditionalExpr:
		return exprUsesUnsupportedThis(e.Test) || exprUsesUnsupportedThis(e.Consequent) || exprUsesUnsupportedThis(e.Alternate)
	case *ast.ArrayExpr:
		for _, el := range e.Elements {
			if exprUsesUnsupportedThis(el) {
				return true
			}
		}
	case *ast.ObjectExpr:
		for _, p := range e.Properties {
			if p != nil && exprUsesUnsupportedThis(p.Value) {
				return true
			}
		}
	case *ast.TemplateExpr:
		for _, p := range e.Parts {
			if exprUsesUnsupportedThis(p) {
				return true
			}
		}
	case *ast.JSXElement:
		if e.Opening != nil {
			for _, a := range e.Opening.Attributes {
				if a != nil && a.Value != nil && exprUsesUnsupportedThis(a.Value) {
					return true
				}
			}
		}
		for _, c := range e.Children {
			switch ch := c.(type) {
			case *ast.JSXExprContainer:
				if exprUsesUnsupportedThis(ch.Expression) {
					return true
				}
			case *ast.JSXElementChild:
				if exprUsesUnsupportedThis(ch.Element) {
					return true
				}
			case *ast.JSXFragmentChild:
				if exprUsesUnsupportedThis(ch.Fragment) {
					return true
				}
			}
		}
	case *ast.JSXFragment:
		for _, c := range e.Children {
			switch ch := c.(type) {
			case *ast.JSXExprContainer:
				if exprUsesUnsupportedThis(ch.Expression) {
					return true
				}
			case *ast.JSXElementChild:
				if exprUsesUnsupportedThis(ch.Element) {
					return true
				}
			case *ast.JSXFragmentChild:
				if exprUsesUnsupportedThis(ch.Fragment) {
					return true
				}
			}
		}
	}
	return false
}

// lowerClassStmts rewrites this.* reads in a class method body.
func lowerClassStmts(stmts []ast.Stmt) []ast.Stmt {
	for _, stmt := range stmts {
		if stmt == nil {
			continue
		}
		switch s := stmt.(type) {
		case *ast.ExprStmt:
			s.Expression = lowerClassExpr(s.Expression)
		case *ast.ReturnStmt:
			s.Value = lowerClassExpr(s.Value)
		case *ast.VarStmt:
			for _, d := range s.Decls {
				if d != nil {
					d.Init = lowerClassExpr(d.Init)
				}
			}
		case *ast.IfStmt:
			s.Test = lowerClassExpr(s.Test)
			s.Consequent = lowerClassStmts(s.Consequent)
			s.Alternate = lowerClassStmts(s.Alternate)
		case *ast.BlockStmt:
			s.Body = lowerClassStmts(s.Body)
		case *ast.ForStmt:
			if s.Init != nil {
				lowerClassStmts([]ast.Stmt{s.Init})
			}
			s.Test = lowerClassExpr(s.Test)
			s.Update = lowerClassExpr(s.Update)
			s.Body = lowerClassStmts(s.Body)
		case *ast.ForInStmt:
			s.Left = lowerClassExpr(s.Left)
			s.Right = lowerClassExpr(s.Right)
			s.Body = lowerClassStmts(s.Body)
		case *ast.WhileStmt:
			s.Test = lowerClassExpr(s.Test)
			s.Body = lowerClassStmts(s.Body)
		case *ast.DoWhileStmt:
			s.Body = lowerClassStmts(s.Body)
			s.Test = lowerClassExpr(s.Test)
		case *ast.SwitchStmt:
			s.Discriminant = lowerClassExpr(s.Discriminant)
			for _, c := range s.Cases {
				c.Test = lowerClassExpr(c.Test)
				c.Body = lowerClassStmts(c.Body)
			}
		case *ast.TryStmt:
			s.Body = lowerClassStmts(s.Body)
			if s.Catch != nil {
				s.Catch.Body = lowerClassStmts(s.Catch.Body)
			}
			s.Finally = lowerClassStmts(s.Finally)
		}
	}
	return stmts
}

// lowerClassExpr rewrites this.state / this.props / this.setState reads.
func lowerClassExpr(expr ast.Expr) ast.Expr {
	switch e := expr.(type) {
	case *ast.MemberExpr:
		if _, ok := e.Object.(*ast.ThisExpr); ok && !e.Computed {
			if p, ok := e.Property.(*ast.Identifier); ok {
				switch p.Name {
				case "props":
					return &ast.Identifier{Name: "props"}
				case "state":
					return &ast.CallExpr{Callee: &ast.Identifier{Name: classStateName}}
				case "setState":
					return &ast.Identifier{Name: classSetterName}
				}
			}
			return e
		}
		if inner, ok := e.Object.(*ast.MemberExpr); ok && !inner.Computed {
			if _, ok := inner.Object.(*ast.ThisExpr); ok {
				if p, ok := inner.Property.(*ast.Identifier); ok {
					switch p.Name {
					case "state":
						return &ast.MemberExpr{
							Position: e.Position,
							Object:   &ast.CallExpr{Callee: &ast.Identifier{Name: classStateName}},
							Property: e.Property,
							Computed: e.Computed,
						}
					case "props":
						return &ast.MemberExpr{
							Position: e.Position,
							Object:   &ast.Identifier{Name: "props"},
							Property: e.Property,
							Computed: e.Computed,
						}
					}
				}
			}
		}
		e.Object = lowerClassExpr(e.Object)
		if e.Computed {
			e.Property = lowerClassExpr(e.Property)
		}
	case *ast.CallExpr:
		if isThisSetStateCall(e) && len(e.Args) == 1 {
			return lowerSetStateCall(e)
		}
		e.Callee = lowerClassExpr(e.Callee)
		for i := range e.Args {
			e.Args[i] = lowerClassExpr(e.Args[i])
		}
	case *ast.BinaryExpr:
		e.Left = lowerClassExpr(e.Left)
		e.Right = lowerClassExpr(e.Right)
	case *ast.UnaryExpr:
		e.Arg = lowerClassExpr(e.Arg)
	case *ast.ConditionalExpr:
		e.Test = lowerClassExpr(e.Test)
		e.Consequent = lowerClassExpr(e.Consequent)
		e.Alternate = lowerClassExpr(e.Alternate)
	case *ast.ArrayExpr:
		for i := range e.Elements {
			e.Elements[i] = lowerClassExpr(e.Elements[i])
		}
	case *ast.ObjectExpr:
		for _, p := range e.Properties {
			if p != nil {
				p.Value = lowerClassExpr(p.Value)
			}
		}
	case *ast.TemplateExpr:
		for i := range e.Parts {
			e.Parts[i] = lowerClassExpr(e.Parts[i])
		}
	case *ast.ArrowFn:
		e.Body = lowerClassStmts(e.Body)
	case *ast.JSXElement:
		if e.Opening != nil {
			for _, a := range e.Opening.Attributes {
				if a != nil {
					a.Value = lowerClassExpr(a.Value)
				}
			}
		}
		for _, c := range e.Children {
			switch ch := c.(type) {
			case *ast.JSXExprContainer:
				ch.Expression = lowerClassExpr(ch.Expression)
			case *ast.JSXElementChild:
				ch.Element = lowerClassExpr(ch.Element).(*ast.JSXElement)
			case *ast.JSXFragmentChild:
				ch.Fragment = lowerClassExpr(ch.Fragment).(*ast.JSXFragment)
			}
		}
	case *ast.JSXFragment:
		for _, c := range e.Children {
			switch ch := c.(type) {
			case *ast.JSXExprContainer:
				ch.Expression = lowerClassExpr(ch.Expression)
			case *ast.JSXElementChild:
				ch.Element = lowerClassExpr(ch.Element).(*ast.JSXElement)
			case *ast.JSXFragmentChild:
				ch.Fragment = lowerClassExpr(ch.Fragment).(*ast.JSXFragment)
			}
		}
	}
	return expr
}

func isThisSetStateCall(call *ast.CallExpr) bool {
	mem, ok := call.Callee.(*ast.MemberExpr)
	if !ok || mem.Computed {
		return false
	}
	if _, ok := mem.Object.(*ast.ThisExpr); !ok {
		return false
	}
	p, ok := mem.Property.(*ast.Identifier)
	return ok && p.Name == "setState"
}

// lowerSetStateCall lowers `this.setState(x)` to a Krate signal update. React
// merges partial state, so an object argument is merged with the current state
// and a function updater receives the previous state.
func lowerSetStateCall(call *ast.CallExpr) ast.Expr {
	raw := call.Args[0]
	arg := lowerClassExpr(raw)
	stateGet := &ast.CallExpr{Callee: &ast.Identifier{Name: classStateName}}
	if _, ok := raw.(*ast.ArrowFn); ok {
		prev := &ast.Identifier{Name: "prev"}
		next := mergeObject(prev, &ast.CallExpr{Callee: arg, Args: []ast.Expr{prev}})
		fn := &ast.ArrowFn{
			Params: []*ast.Param{{Name: "prev"}},
			Body:   []ast.Stmt{&ast.ReturnStmt{Value: next}},
		}
		return &ast.CallExpr{Callee: &ast.Identifier{Name: classSetterName}, Args: []ast.Expr{fn}}
	}
	return &ast.CallExpr{Callee: &ast.Identifier{Name: classSetterName}, Args: []ast.Expr{mergeObject(stateGet, arg)}}
}

// mergeObject builds `Object.assign({}, a, b)`.
func mergeObject(a, b ast.Expr) ast.Expr {
	return &ast.CallExpr{
		Callee: &ast.MemberExpr{
			Object:   &ast.Identifier{Name: "Object"},
			Property: &ast.Identifier{Name: "assign"},
		},
		Args: []ast.Expr{&ast.ObjectExpr{}, a, b},
	}
}
