package bundler

import (
	"strings"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/diag"
)

// checkInlineUseServer rejects a `"use server"` directive at the top of a
// function body. React allows inline server functions that close over
// component scope; Krate supports file-level action modules only.
func (b *Bundler) checkInlineUseServer(prog *ast.Program, path, src string) {
	if prog == nil || !strings.Contains(src, "use server") {
		return
	}
	pos, ok := findInlineUseServer(prog.Body)
	if !ok {
		return
	}
	d := diag.New(path, pos.Line, pos.Col,
		`inline "use server" functions are not supported`,
		`move the function into a module whose first statement is "use server", export it, and import it here`,
		sourceLineOf(src, pos.Line)).WithCode(diag.CodeInlineUseServer)
	_ = b.setActionError(d)
}

func isUseServerDirective(stmts []ast.Stmt) (ast.Pos, bool) {
	if len(stmts) == 0 {
		return ast.Pos{}, false
	}
	es, ok := stmts[0].(*ast.ExprStmt)
	if !ok {
		return ast.Pos{}, false
	}
	lit, ok := es.Expression.(*ast.Literal)
	if !ok || lit.Kind != ast.StringLit || strings.Trim(strings.TrimSpace(lit.Value), `"'`) != "use server" {
		return ast.Pos{}, false
	}
	pos := es.Position
	if pos.Line == 0 {
		pos = lit.Position
	}
	return pos, true
}

// inlineUseServerFinder walks function bodies (declarations, arrows and
// function expressions, class members) for a leading "use server" directive.
type inlineUseServerFinder struct {
	pos   ast.Pos
	found bool
}

func findInlineUseServer(stmts []ast.Stmt) (ast.Pos, bool) {
	f := &inlineUseServerFinder{}
	f.stmts(stmts)
	return f.pos, f.found
}

func (f *inlineUseServerFinder) fnBody(stmts []ast.Stmt) {
	if f.found {
		return
	}
	if p, ok := isUseServerDirective(stmts); ok {
		f.pos, f.found = p, true
		return
	}
	f.stmts(stmts)
}

func (f *inlineUseServerFinder) stmts(list []ast.Stmt) {
	for _, s := range list {
		if f.found {
			return
		}
		f.stmt(s)
	}
}

func (f *inlineUseServerFinder) members(members []*ast.ClassMember) {
	for _, m := range members {
		if m != nil {
			f.fnBody(m.Body)
			f.expr(m.Init)
		}
	}
}

func (f *inlineUseServerFinder) stmt(s ast.Stmt) {
	if f.found || s == nil {
		return
	}
	switch x := s.(type) {
	case *ast.FnDecl:
		f.fnBody(x.Body)
	case *ast.ClassDecl:
		f.members(x.Members)
	case *ast.VarStmt:
		for _, d := range x.Decls {
			if d != nil {
				f.expr(d.Init)
			}
		}
	case *ast.ExprStmt:
		f.expr(x.Expression)
	case *ast.ReturnStmt:
		f.expr(x.Value)
	case *ast.ExportStmt:
		if x.Declaration != nil {
			f.stmt(x.Declaration)
		}
	case *ast.IfStmt:
		f.expr(x.Test)
		f.stmts(x.Consequent)
		f.stmts(x.Alternate)
	case *ast.BlockStmt:
		f.stmts(x.Body)
	case *ast.ForStmt:
		f.stmts(x.Body)
	case *ast.ForInStmt:
		f.stmts(x.Body)
	case *ast.WhileStmt:
		f.stmts(x.Body)
	case *ast.DoWhileStmt:
		f.stmts(x.Body)
	case *ast.TryStmt:
		f.stmts(x.Body)
		if x.Catch != nil {
			f.stmts(x.Catch.Body)
		}
		f.stmts(x.Finally)
	case *ast.SwitchStmt:
		for _, c := range x.Cases {
			if c != nil {
				f.stmts(c.Body)
			}
		}
	}
}

func (f *inlineUseServerFinder) exprs(list []ast.Expr) {
	for _, e := range list {
		f.expr(e)
	}
}

func (f *inlineUseServerFinder) expr(e ast.Expr) {
	if f.found || e == nil {
		return
	}
	switch x := e.(type) {
	case *ast.ArrowFn:
		for _, p := range x.Params {
			if p != nil {
				f.expr(p.Default)
			}
		}
		f.fnBody(x.Body)
	case *ast.CallExpr:
		f.expr(x.Callee)
		f.exprs(x.Args)
	case *ast.NewExpr:
		f.expr(x.Callee)
		f.exprs(x.Args)
	case *ast.MemberExpr:
		f.expr(x.Object)
	case *ast.BinaryExpr:
		f.expr(x.Left)
		f.expr(x.Right)
	case *ast.UnaryExpr:
		f.expr(x.Arg)
	case *ast.ConditionalExpr:
		f.expr(x.Test)
		f.expr(x.Consequent)
		f.expr(x.Alternate)
	case *ast.ParenExpr:
		f.expr(x.Expr)
	case *ast.TypeAssertion:
		f.expr(x.Expr)
	case *ast.AwaitExpr:
		f.expr(x.Arg)
	case *ast.ObjectExpr:
		for _, p := range x.Properties {
			if p != nil {
				f.expr(p.Value)
			}
		}
	case *ast.ArrayExpr:
		f.exprs(x.Elements)
	case *ast.TemplateExpr:
		f.exprs(x.Parts)
	case *ast.JSXElement:
		if x.Opening != nil {
			for _, a := range x.Opening.Attributes {
				if a != nil {
					f.expr(a.Value)
				}
			}
		}
		f.children(x.Children)
	case *ast.JSXFragment:
		f.children(x.Children)
	case *ast.ClassExpr:
		if x.Class != nil {
			f.members(x.Class.Members)
		}
	}
}

func (f *inlineUseServerFinder) children(children []ast.JSXChild) {
	for _, ch := range children {
		switch c := ch.(type) {
		case *ast.JSXExprContainer:
			f.expr(c.Expression)
		case *ast.JSXElementChild:
			if c.Element != nil {
				f.expr(c.Element)
			}
		case *ast.JSXFragmentChild:
			if c.Fragment != nil {
				f.expr(c.Fragment)
			}
		}
	}
}
