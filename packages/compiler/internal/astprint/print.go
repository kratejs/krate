// Package astprint renders an *ast.Program back to TypeScript/JSX source.
// The Krate parser is intentionally lossy: it drops type-only constructs
// (interfaces, type aliases, enums, classes) and all type annotations, because
// they carry no runtime meaning for hydration. This printer therefore only
// round-trips the runtime subset of a module. Callers that edit user source
// (e.g. the MCP `edit_ast` tool) must first verify the source is in that subset
// with LossyReason; otherwise printing would silently drop type information.
package astprint

import (
	"strings"

	"github.com/kratejs/krate/packages/compiler/ast"
)

// Print renders a program as source. Unsupported nodes render as an empty
// string rather than panicking; parse the output before trusting it.
func Print(p *ast.Program) string {
	if p == nil {
		return ""
	}
	pr := &printer{}
	for _, s := range p.Body {
		pr.stmt(s, 0)
	}
	return pr.b.String()
}

type printer struct {
	b strings.Builder
}

func (p *printer) indent(n int) {
	for i := 0; i < n; i++ {
		p.b.WriteString("  ")
	}
}

func (p *printer) line(n int, s string) {
	p.indent(n)
	p.b.WriteString(s)
	p.b.WriteString("\n")
}

// statements

func (p *printer) stmt(s ast.Stmt, depth int) {
	switch v := s.(type) {
	case nil:
		return
	case *ast.ImportStmt:
		p.line(depth, printImport(v))
	case *ast.ExportStmt:
		p.export(v, depth)
	case *ast.VarStmt:
		p.line(depth, printVar(v))
	case *ast.FnDecl:
		p.fnDecl(v, depth)
	case *ast.ReturnStmt:
		if v.Value == nil {
			p.line(depth, "return;")
		} else {
			p.line(depth, "return "+p.expr(v.Value)+";")
		}
	case *ast.ExprStmt:
		e := p.expr(v.Expression)
		// A leading object literal would parse as a block statement.
		if _, ok := v.Expression.(*ast.ObjectExpr); ok {
			e = "(" + e + ")"
		}
		p.line(depth, e+";")
	case *ast.IfStmt:
		p.ifStmt(v, depth)
	case *ast.BlockStmt:
		p.block(v.Body, depth)
	case *ast.ForStmt:
		p.forStmt(v, depth)
	case *ast.ForInStmt:
		p.forInStmt(v, depth)
	case *ast.WhileStmt:
		p.line(depth, "while ("+p.expr(v.Test)+") {")
		p.body(v.Body, depth+1)
		p.line(depth, "}")
	case *ast.DoWhileStmt:
		p.line(depth, "do {")
		p.body(v.Body, depth+1)
		p.line(depth, "} while ("+p.expr(v.Test)+");")
	case *ast.SwitchStmt:
		p.switchStmt(v, depth)
	case *ast.TryStmt:
		p.tryStmt(v, depth)
	case *ast.ThrowStmt:
		p.line(depth, "throw "+p.expr(v.Value)+";")
	case *ast.BreakStmt:
		if v.Label != "" {
			p.line(depth, "break "+v.Label+";")
		} else {
			p.line(depth, "break;")
		}
	case *ast.ContinueStmt:
		if v.Label != "" {
			p.line(depth, "continue "+v.Label+";")
		} else {
			p.line(depth, "continue;")
		}
	}
}

func (p *printer) body(stmts []ast.Stmt, depth int) {
	for _, s := range stmts {
		p.stmt(s, depth)
	}
}

func (p *printer) block(stmts []ast.Stmt, depth int) {
	p.line(depth, "{")
	p.body(stmts, depth+1)
	p.line(depth, "}")
}

func (p *printer) fnDecl(f *ast.FnDecl, depth int) {
	var b strings.Builder
	if f.Async {
		b.WriteString("async ")
	}
	b.WriteString("function")
	if f.Name != "" {
		b.WriteString(" " + f.Name)
	}
	b.WriteString("(" + printParams(f.Params) + ") {")
	p.line(depth, b.String())
	p.body(f.Body, depth+1)
	p.line(depth, "}")
}

func (p *printer) export(e *ast.ExportStmt, depth int) {
	switch {
	case e.StarReexport:
		p.line(depth, "export * from "+e.ReexportSource+";")
	case e.ReexportSource != "":
		p.line(depth, "export { "+e.Local+" } from "+e.ReexportSource+";")
	case e.Default && e.Local != "" && e.Declaration == nil:
		p.line(depth, "export default "+e.Local+";")
	case e.Default && e.Declaration != nil:
		if es, ok := e.Declaration.(*ast.ExprStmt); ok {
			p.line(depth, "export default "+p.expr(es.Expression)+";")
			return
		}
		// `export default function ...` - print the declaration with a prefix.
		p.exported(e.Declaration, depth, "export default ")
	case e.Declaration != nil:
		p.exported(e.Declaration, depth, "export ")
	case e.Local != "":
		p.line(depth, "export { "+e.Local+" };")
	}
}

// exported prints a declaration with an `export ` prefix. Function and variable
// declarations are rendered inline rather than delegating to stmt, so the
// prefix lands on the same line.
func (p *printer) exported(s ast.Stmt, depth int, prefix string) {
	switch v := s.(type) {
	case *ast.FnDecl:
		var b strings.Builder
		b.WriteString(prefix)
		if v.Async {
			b.WriteString("async ")
		}
		b.WriteString("function")
		if v.Name != "" {
			b.WriteString(" " + v.Name)
		}
		b.WriteString("(" + printParams(v.Params) + ") {")
		p.line(depth, b.String())
		p.body(v.Body, depth+1)
		p.line(depth, "}")
	case *ast.VarStmt:
		p.line(depth, prefix+printVar(v))
	case *ast.ExprStmt:
		p.line(depth, prefix+p.expr(v.Expression))
	default:
		p.stmt(s, depth)
	}
}

func (p *printer) ifStmt(f *ast.IfStmt, depth int) {
	p.line(depth, "if ("+p.expr(f.Test)+") {")
	p.body(f.Consequent, depth+1)
	if len(f.Alternate) == 0 {
		p.line(depth, "}")
		return
	}
	// `else if` chain reads best on one line.
	if len(f.Alternate) == 1 {
		if inner, ok := f.Alternate[0].(*ast.IfStmt); ok {
			p.indent(depth)
			p.b.WriteString("} else if (")
			p.b.WriteString(p.expr(inner.Test))
			p.b.WriteString(") {\n")
			p.body(inner.Consequent, depth+1)
			p.closeIfChain(inner, depth)
			return
		}
	}
	p.line(depth, "} else {")
	p.body(f.Alternate, depth+1)
	p.line(depth, "}")
}

// closeIfChain renders the tail of a nested else-if chain that was opened by
// ifStmt.
func (p *printer) closeIfChain(f *ast.IfStmt, depth int) {
	for len(f.Alternate) == 1 {
		next, ok := f.Alternate[0].(*ast.IfStmt)
		if !ok {
			break
		}
		p.indent(depth)
		p.b.WriteString("} else if (")
		p.b.WriteString(p.expr(next.Test))
		p.b.WriteString(") {\n")
		p.body(next.Consequent, depth+1)
		f = next
	}
	if len(f.Alternate) > 0 {
		p.line(depth, "} else {")
		p.body(f.Alternate, depth+1)
	}
	p.line(depth, "}")
}

func (p *printer) forStmt(f *ast.ForStmt, depth int) {
	init := ""
	if f.Init != nil {
		init = strings.TrimSuffix(printStmtInline(f.Init), ";")
	}
	test := ""
	if f.Test != nil {
		test = p.expr(f.Test)
	}
	update := ""
	if f.Update != nil {
		update = p.expr(f.Update)
	}
	p.line(depth, "for ("+init+"; "+test+"; "+update+") {")
	p.body(f.Body, depth+1)
	p.line(depth, "}")
}

func (p *printer) forInStmt(f *ast.ForInStmt, depth int) {
	op := " in "
	if f.IsForOf {
		op = " of "
	}
	p.line(depth, "for ("+p.expr(f.Left)+op+p.expr(f.Right)+") {")
	p.body(f.Body, depth+1)
	p.line(depth, "}")
}

func (p *printer) switchStmt(s *ast.SwitchStmt, depth int) {
	p.line(depth, "switch ("+p.expr(s.Discriminant)+") {")
	for _, c := range s.Cases {
		if c.Test == nil {
			p.line(depth, "default:")
		} else {
			p.line(depth, "case "+p.expr(c.Test)+":")
		}
		p.body(c.Body, depth+1)
	}
	p.line(depth, "}")
}

func (p *printer) tryStmt(t *ast.TryStmt, depth int) {
	p.line(depth, "try {")
	p.body(t.Body, depth+1)
	if t.Catch != nil {
		if t.Catch.Param != "" {
			p.line(depth, "} catch ("+t.Catch.Param+") {")
		} else {
			p.line(depth, "} catch {")
		}
		p.body(t.Catch.Body, depth+1)
	}
	if len(t.Finally) > 0 {
		p.line(depth, "} finally {")
		p.body(t.Finally, depth+1)
	}
	p.line(depth, "}")
}

// helpers for statement text

func printImport(i *ast.ImportStmt) string {
	var parts []string
	if i.Default != "" {
		parts = append(parts, i.Default)
	}
	if i.Namespace != "" {
		parts = append(parts, "* as "+i.Namespace)
	}
	if len(i.Named) > 0 {
		var names []string
		for _, n := range i.Named {
			if n.Remote != "" && n.Remote != n.Local {
				names = append(names, n.Remote+" as "+n.Local)
			} else if n.Local != "" {
				names = append(names, n.Local)
			}
		}
		if len(names) > 0 {
			parts = append(parts, "{ "+strings.Join(names, ", ")+" }")
		}
	}
	if len(parts) == 0 {
		return "import " + i.Source + ";"
	}
	return "import " + strings.Join(parts, ", ") + " from " + i.Source + ";"
}

func printVar(v *ast.VarStmt) string {
	kind := "const"
	switch v.Kind {
	case ast.VarLet:
		kind = "let"
	case ast.VarVar:
		kind = "var"
	}
	var decls []string
	for _, d := range v.Decls {
		decls = append(decls, printVarDecl(d))
	}
	return kind + " " + strings.Join(decls, ", ") + ";"
}

func printVarDecl(d *ast.VarDecl) string {
	name := d.Name
	if d.IsDestructuring && d.Pattern != "" {
		name = d.Pattern
	}
	if d.Init != nil {
		return name + " = " + (&printer{}).expr(d.Init)
	}
	return name
}

func printParams(params []*ast.Param) string {
	var out []string
	for _, prm := range params {
		out = append(out, printParam(prm))
	}
	return strings.Join(out, ", ")
}

func printParam(prm *ast.Param) string {
	if prm == nil {
		return ""
	}
	name := prm.Name
	if name == "{...}" && prm.Pattern != "" {
		name = prm.Pattern
	}
	if prm.IsRest {
		name = "..." + name
	}
	if prm.Default != nil {
		name += " = " + (&printer{}).expr(prm.Default)
	}
	return name
}

// printStmtInline renders a statement without a trailing newline (for `for`
// initializers).
func printStmtInline(s ast.Stmt) string {
	p := &printer{}
	p.stmt(s, 0)
	return strings.TrimRight(p.b.String(), "\n")
}
