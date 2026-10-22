package annotator

import (
	"strings"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/diag"
	irtree "github.com/kratejs/krate/packages/compiler/internal/irtree"
)

// classIssue explains why a React class component cannot be lowered: the
// offending member, where it is, and what to do instead.
type classIssue struct {
	member string
	pos    ast.Pos
	reason string
	hint   string
}

// unsupportedLifecycle maps class members Krate does not lower to the reason
// and the actionable replacement shown in the diagnostic.
var unsupportedLifecycle = map[string][2]string{
	"shouldComponentUpdate": {
		"shouldComponentUpdate is not supported",
		"Krate re-renders through fine-grained signals, so render skipping has no equivalent; remove shouldComponentUpdate.",
	},
	"getSnapshotBeforeUpdate": {
		"getSnapshotBeforeUpdate is not supported",
		"read the DOM inside componentDidUpdate instead, or convert the component to a function component with createEffect.",
	},
	"getDerivedStateFromProps": {
		"static getDerivedStateFromProps is not supported",
		"derive the value during render from this.props instead of copying props into state.",
	},
	"getDerivedStateFromError": {
		"static getDerivedStateFromError (error boundary) is not supported",
		"error boundaries are not lowered; handle the failure in the component that throws (try/catch) or use a function component.",
	},
	"componentDidCatch": {
		"componentDidCatch (error boundary) is not supported",
		"error boundaries are not lowered; handle the failure in the component that throws (try/catch) or use a function component.",
	},
}

// classLoweringIssue reports the first construct that keeps a React class
// component from being lowered, or nil when synthesizeClassComponent can
// lower it. It does not mutate the class.
func classLoweringIssue(class *ast.ClassDecl) *classIssue {
	if class == nil || class.Name == "" || !isReactClassBase(class.SuperClass) {
		return nil
	}
	allowed := map[string]bool{"props": true, "state": true, "setState": true, "forceUpdate": true}
	var render *ast.ClassMember
	var bodies []*ast.ClassMember
	for _, m := range class.Members {
		if m == nil {
			continue
		}
		if m.Private {
			name := "#" + strings.TrimPrefix(m.Name, "#")
			return &classIssue{member: name, pos: m.Position,
				reason: "private member " + name + " is not supported",
				hint:   "rename it to a regular field or method (drop the #), or move it to a module-level helper."}
		}
		if pair, ok := unsupportedLifecycle[m.Name]; ok && m.Kind == ast.ClassMethod {
			return &classIssue{member: m.Name, pos: m.Position, reason: pair[0], hint: pair[1]}
		}
		if m.Static {
			if m.Kind == ast.ClassField && (m.Name == "defaultProps" || m.Name == "propTypes" || m.Name == "displayName") {
				continue
			}
			return &classIssue{member: m.Name, pos: m.Position,
				reason: "static member " + m.Name + " is not supported",
				hint:   "move it to a module-level constant or function next to the component."}
		}
		switch m.Kind {
		case ast.ClassMethod:
			switch m.Name {
			case "render":
				render = m
				bodies = append(bodies, m)
			case "componentDidMount", "componentDidUpdate", "componentWillUnmount":
				bodies = append(bodies, m)
			case "constructor":
			default:
				allowed[m.Name] = true
			}
		case ast.ClassField:
			if m.Name != "" {
				allowed[m.Name] = true
			}
		}
	}
	for _, m := range class.Members {
		if m != nil && m.Kind == ast.ClassConstructor {
			collectThisAssignments(m.Body, allowed)
		}
	}
	if render == nil {
		return &classIssue{pos: class.Position, reason: "it has no render() method",
			hint: "add a render() method returning JSX, or convert it to a function component."}
	}
	for _, m := range bodies {
		if mem := findUnsupportedThis(m.Body, allowed); mem != nil {
			name := ""
			if id, ok := mem.Property.(*ast.Identifier); ok {
				name = id.Name
			}
			pos := mem.Position
			if pos.Line == 0 {
				pos = m.Position
			}
			return &classIssue{member: "this." + name, pos: pos,
				reason: "this." + name + " is not a field, method, props, state or setState",
				hint:   "declare " + name + " as a class field or method, or read it from this.props/this.state."}
		}
	}
	return nil
}

// collectClassRejections records, for every React class component in body
// that cannot be lowered, a warning diagnostic positioned at the offending
// member. The builder reports it where the component is used.
func collectClassRejections(body []ast.Stmt, ann *irtree.Annotations, file, raw string) {
	for _, stmt := range body {
		var class *ast.ClassDecl
		switch s := stmt.(type) {
		case *ast.ClassDecl:
			class = s
		case *ast.ExportStmt:
			if c, ok := s.Declaration.(*ast.ClassDecl); ok {
				class = c
			}
		}
		issue := classLoweringIssue(class)
		if issue == nil {
			continue
		}
		if ann.ClassRejections == nil {
			ann.ClassRejections = make(map[string]diag.Diagnostic)
		}
		if _, exists := ann.ClassRejections[class.Name]; exists {
			continue
		}
		pos := issue.pos
		if pos.Line == 0 {
			pos = class.Position
		}
		d := diag.New(file, pos.Line, pos.Col,
			"class component <"+class.Name+"> is not lowered: "+issue.reason,
			issue.hint, lineOf(raw, pos.Line))
		d.Severity = diag.SeverityWarning
		ann.ClassRejections[class.Name] = d
	}
}

// lineOf returns the 1-based line of raw, or "".
func lineOf(raw string, line int) string {
	if raw == "" || line <= 0 {
		return ""
	}
	lines := strings.SplitN(raw, "\n", line+1)
	if len(lines) < line {
		return ""
	}
	return strings.TrimRight(lines[line-1], "\r")
}

// findUnsupportedThis returns the first `this.<name>` member read in stmts
// whose name is not in allowed.
func findUnsupportedThis(stmts []ast.Stmt, allowed map[string]bool) *ast.MemberExpr {
	var found *ast.MemberExpr
	var walkStmts func([]ast.Stmt)
	var walk func(ast.Expr)
	walkChildren := func(children []ast.JSXChild) {
		for _, c := range children {
			switch ch := c.(type) {
			case *ast.JSXExprContainer:
				walk(ch.Expression)
			case *ast.JSXElementChild:
				walk(ch.Element)
			case *ast.JSXFragmentChild:
				walk(ch.Fragment)
			}
		}
	}
	walk = func(e ast.Expr) {
		if e == nil || found != nil {
			return
		}
		switch x := e.(type) {
		case *ast.MemberExpr:
			if _, ok := x.Object.(*ast.ThisExpr); ok && !x.Computed {
				if p, ok := x.Property.(*ast.Identifier); ok && !allowed[p.Name] {
					found = x
				}
				return
			}
			walk(x.Object)
			if x.Computed {
				walk(x.Property)
			}
		case *ast.CallExpr:
			walk(x.Callee)
			for _, a := range x.Args {
				walk(a)
			}
		case *ast.NewExpr:
			walk(x.Callee)
			for _, a := range x.Args {
				walk(a)
			}
		case *ast.ArrowFn:
			walkStmts(x.Body)
		case *ast.BinaryExpr:
			walk(x.Left)
			walk(x.Right)
		case *ast.UnaryExpr:
			walk(x.Arg)
		case *ast.AwaitExpr:
			walk(x.Arg)
		case *ast.ConditionalExpr:
			walk(x.Test)
			walk(x.Consequent)
			walk(x.Alternate)
		case *ast.TypeAssertion:
			walk(x.Expr)
		case *ast.ArrayExpr:
			for _, el := range x.Elements {
				walk(el)
			}
		case *ast.ObjectExpr:
			for _, p := range x.Properties {
				if p != nil {
					walk(p.Value)
				}
			}
		case *ast.TemplateExpr:
			for _, p := range x.Parts {
				walk(p)
			}
		case *ast.JSXElement:
			if x.Opening != nil {
				for _, a := range x.Opening.Attributes {
					if a != nil {
						walk(a.Value)
					}
				}
			}
			walkChildren(x.Children)
		case *ast.JSXFragment:
			walkChildren(x.Children)
		}
	}
	walkStmts = func(stmts []ast.Stmt) {
		for _, stmt := range stmts {
			if found != nil {
				return
			}
			switch s := stmt.(type) {
			case *ast.ExprStmt:
				walk(s.Expression)
			case *ast.ReturnStmt:
				walk(s.Value)
			case *ast.ThrowStmt:
				walk(s.Value)
			case *ast.VarStmt:
				for _, d := range s.Decls {
					if d != nil {
						walk(d.Init)
					}
				}
			case *ast.IfStmt:
				walk(s.Test)
				walkStmts(s.Consequent)
				walkStmts(s.Alternate)
			case *ast.BlockStmt:
				walkStmts(s.Body)
			case *ast.ForStmt:
				if s.Init != nil {
					walkStmts([]ast.Stmt{s.Init})
				}
				walk(s.Test)
				walk(s.Update)
				walkStmts(s.Body)
			case *ast.ForInStmt:
				walk(s.Right)
				walkStmts(s.Body)
			case *ast.WhileStmt:
				walk(s.Test)
				walkStmts(s.Body)
			case *ast.DoWhileStmt:
				walkStmts(s.Body)
				walk(s.Test)
			case *ast.SwitchStmt:
				walk(s.Discriminant)
				for _, c := range s.Cases {
					if c != nil {
						walk(c.Test)
						walkStmts(c.Body)
					}
				}
			case *ast.TryStmt:
				walkStmts(s.Body)
				if s.Catch != nil {
					walkStmts(s.Catch.Body)
				}
				walkStmts(s.Finally)
			case *ast.FnDecl:
				walkStmts(s.Body)
			}
		}
	}
	walkStmts(stmts)
	return found
}
