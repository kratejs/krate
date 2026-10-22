// Package astprint renders an *ast.Program back to TypeScript/JSX source.
// The Krate parser drops type-only constructs (interfaces, type aliases and
// all type annotations) because they carry no runtime meaning for hydration.
// This printer therefore only round-trips the runtime subset of a module.
// Callers that edit user source (e.g. the MCP `edit_ast` tool) must first
// verify the source is in that subset (see CycleLossy); otherwise printing
// would silently drop type information.
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

// PrintExpr renders a single expression as source (for example, re-emitting a
// JSX subtree as the body of an external React island). Unsupported nodes
// render as an empty string rather than panicking.
func PrintExpr(expr ast.Expr) string {
	pr := &printer{}
	return pr.expr(expr, precLowest)
}

type printer struct {
	b strings.Builder
	// noIn parenthesizes `in` expressions (inside a for-statement head).
	noIn bool
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
		p.line(depth, p.varDecl(v)+";")
	case *ast.FnDecl:
		p.fnDecl(v, depth, "")
	case *ast.ClassDecl:
		p.classDecl(v, depth, "")
	case *ast.ReturnStmt:
		if v.Value == nil {
			p.line(depth, "return;")
		} else {
			p.line(depth, "return "+p.expr(v.Value, precLowest)+";")
		}
	case *ast.ExprStmt:
		p.line(depth, p.exprStmt(v.Expression)+";")
	case *ast.IfStmt:
		p.ifStmt(v, depth)
	case *ast.BlockStmt:
		p.block(v.Body, depth)
	case *ast.ForStmt:
		p.forStmt(v, depth)
	case *ast.ForInStmt:
		p.forInStmt(v, depth)
	case *ast.WhileStmt:
		p.line(depth, "while ("+p.expr(v.Test, precLowest)+") {")
		p.body(v.Body, depth+1)
		p.line(depth, "}")
	case *ast.DoWhileStmt:
		p.line(depth, "do {")
		p.body(v.Body, depth+1)
		p.line(depth, "} while ("+p.expr(v.Test, precLowest)+");")
	case *ast.SwitchStmt:
		p.switchStmt(v, depth)
	case *ast.TryStmt:
		p.tryStmt(v, depth)
	case *ast.ThrowStmt:
		p.line(depth, "throw "+p.expr(v.Value, precLowest)+";")
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
	case *ast.LabeledStmt:
		inner := &printer{}
		inner.stmt(v.Body, depth)
		body := strings.TrimLeft(inner.b.String(), " ")
		if body == "" {
			body = ";\n"
		}
		p.indent(depth)
		p.b.WriteString(v.Label + ": " + body)
	case *ast.EmptyStmt:
		p.line(depth, ";")
	case *ast.WithStmt:
		p.line(depth, "with ("+p.expr(v.Object, precLowest)+") {")
		p.body(v.Body, depth+1)
		p.line(depth, "}")
	case *ast.DebuggerStmt:
		p.line(depth, "debugger;")
	}
}

// exprStmt renders an expression statement, parenthesizing expressions whose
// leading token would otherwise parse as a declaration or block.
func (p *printer) exprStmt(e ast.Expr) string {
	s := p.expr(e, precLowest)
	if strings.HasPrefix(s, "{") || strings.HasPrefix(s, "function") || strings.HasPrefix(s, "class") ||
		strings.HasPrefix(s, "async function") || strings.HasPrefix(s, "let [") || strings.HasPrefix(s, "let[") {
		return "(" + s + ")"
	}
	return s
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

func fnHead(async, gen bool, name string) string {
	s := ""
	if async {
		s = "async "
	}
	s += "function"
	if gen {
		s += "*"
	}
	if name != "" {
		s += " " + name
	}
	return s
}

func (p *printer) fnDecl(f *ast.FnDecl, depth int, prefix string) {
	p.line(depth, prefix+fnHead(f.Async, f.Generator, f.Name)+"("+p.params(f.Params)+") {")
	p.body(f.Body, depth+1)
	p.line(depth, "}")
}

func (p *printer) classDecl(c *ast.ClassDecl, depth int, prefix string) {
	p.indent(depth)
	p.b.WriteString(prefix)
	p.b.WriteString(p.classText(c, depth))
	p.b.WriteString("\n")
}

// classText renders `class Name extends X { ... }` with members indented one
// level below depth; the closing brace is not followed by a newline.
func (p *printer) classText(c *ast.ClassDecl, depth int) string {
	var b strings.Builder
	b.WriteString("class")
	if c.Name != "" {
		b.WriteString(" " + c.Name)
	}
	if c.SuperClass != nil {
		b.WriteString(" extends " + p.expr(c.SuperClass, precLHS))
	}
	if len(c.Members) == 0 {
		b.WriteString(" {}")
		return b.String()
	}
	b.WriteString(" {\n")
	for _, m := range c.Members {
		if m == nil {
			continue
		}
		mp := &printer{}
		mp.classMember(m, depth+1)
		b.WriteString(mp.b.String())
	}
	for i := 0; i < depth; i++ {
		b.WriteString("  ")
	}
	b.WriteString("}")
	return b.String()
}

func (p *printer) memberKey(m *ast.ClassMember) string {
	switch {
	case m.Computed:
		return "[" + p.expr(m.KeyExpr, precAssign) + "]"
	case m.KeyQuoted:
		return quoteString(m.Name)
	}
	return m.Name
}

func (p *printer) classMember(m *ast.ClassMember, depth int) {
	prefix := ""
	if m.Static {
		prefix = "static "
	}
	switch m.Kind {
	case ast.ClassStaticBlock:
		p.line(depth, "static {")
		p.body(m.Body, depth+1)
		p.line(depth, "}")
		return
	case ast.ClassField, ast.ClassAccessor:
		if m.Kind == ast.ClassAccessor {
			prefix += "accessor "
		}
		s := prefix + p.memberKey(m)
		if m.Init != nil {
			s += " = " + p.expr(m.Init, precAssign)
		}
		p.line(depth, s+";")
		return
	}
	switch {
	case m.Kind == ast.ClassGetter:
		prefix += "get "
	case m.Kind == ast.ClassSetter:
		prefix += "set "
	default:
		if m.Async {
			prefix += "async "
		}
		if m.Generator {
			prefix += "*"
		}
	}
	head := prefix + p.memberKey(m) + "(" + p.params(m.Params) + ") {"
	if len(m.Body) == 0 {
		p.line(depth, head+"}")
		return
	}
	p.line(depth, head)
	p.body(m.Body, depth+1)
	p.line(depth, "}")
}

func (p *printer) export(e *ast.ExportStmt, depth int) {
	attrs := printAttributes(e.Attributes)
	switch {
	case e.StarReexport:
		s := "export *"
		if e.Namespace != "" {
			s += " as " + printModuleName(e.Namespace)
		}
		p.line(depth, s+" from "+e.ReexportSource+attrs+";")
	case e.ReexportSource != "":
		if len(e.ReexportNamed) > 0 || e.Local == "" {
			p.line(depth, "export "+printSpecifiers(e.ReexportNamed)+" from "+e.ReexportSource+attrs+";")
		} else {
			p.line(depth, "export { "+e.Local+" } from "+e.ReexportSource+attrs+";")
		}
	case e.Default && e.Declaration == nil && e.Local != "":
		p.line(depth, "export default "+e.Local+";")
	case e.Default && e.Declaration != nil:
		switch d := e.Declaration.(type) {
		case *ast.ExprStmt:
			s := p.expr(d.Expression, precAssign)
			if strings.HasPrefix(s, "function") || strings.HasPrefix(s, "class") || strings.HasPrefix(s, "async function") {
				s = "(" + s + ")"
			}
			p.line(depth, "export default "+s+";")
		case *ast.FnDecl:
			p.fnDecl(d, depth, "export default ")
		case *ast.ClassDecl:
			p.classDecl(d, depth, "export default ")
		default:
			p.stmt(d, depth)
		}
	case e.Declaration != nil:
		switch d := e.Declaration.(type) {
		case *ast.FnDecl:
			p.fnDecl(d, depth, "export ")
		case *ast.ClassDecl:
			p.classDecl(d, depth, "export ")
		case *ast.VarStmt:
			p.line(depth, "export "+p.varDecl(d)+";")
		case *ast.ExprStmt:
			p.line(depth, "export "+p.expr(d.Expression, precLowest)+";")
		default:
			p.stmt(d, depth)
		}
	case len(e.Named) > 0:
		p.line(depth, "export "+printSpecifiers(e.Named)+";")
	case e.Local != "":
		p.line(depth, "export { "+e.Local+" };")
	default:
		p.line(depth, "export {};")
	}
}

func printSpecifiers(specs []ast.NamedExport) string {
	if len(specs) == 0 {
		return "{}"
	}
	parts := make([]string, 0, len(specs))
	for _, s := range specs {
		if s.Remote == "" || s.Remote == s.Local {
			parts = append(parts, printModuleName(s.Local))
		} else {
			parts = append(parts, printModuleName(s.Local)+" as "+printModuleName(s.Remote))
		}
	}
	return "{ " + strings.Join(parts, ", ") + " }"
}

// printModuleName renders an import/export specifier name, quoting names
// that are not identifier-like (`"a-b"`).
func printModuleName(s string) string {
	if isJSIdent(s) {
		return s
	}
	return quoteString(s)
}

func printAttributes(attrs []ast.ImportAttribute) string {
	if len(attrs) == 0 {
		return ""
	}
	parts := make([]string, 0, len(attrs))
	for _, a := range attrs {
		parts = append(parts, printModuleName(a.Key)+": "+quoteString(a.Value))
	}
	if len(parts) == 0 {
		return " with {}"
	}
	return " with { " + strings.Join(parts, ", ") + " }"
}

func (p *printer) ifStmt(f *ast.IfStmt, depth int) {
	p.line(depth, "if ("+p.expr(f.Test, precLowest)+") {")
	p.body(f.Consequent, depth+1)
	for len(f.Alternate) == 1 {
		next, ok := f.Alternate[0].(*ast.IfStmt)
		if !ok {
			break
		}
		p.line(depth, "} else if ("+p.expr(next.Test, precLowest)+") {")
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
	p.noIn = true
	switch v := f.Init.(type) {
	case *ast.VarStmt:
		init = p.varDecl(v)
	case *ast.ExprStmt:
		init = p.expr(v.Expression, precLowest)
		if strings.HasPrefix(init, "let") {
			init = "(" + init + ")"
		}
	}
	p.noIn = false
	test := ""
	if f.Test != nil {
		test = p.expr(f.Test, precLowest)
	}
	update := ""
	if f.Update != nil {
		update = p.expr(f.Update, precLowest)
	}
	p.line(depth, "for ("+init+"; "+test+"; "+update+") {")
	p.body(f.Body, depth+1)
	p.line(depth, "}")
}

func (p *printer) forInStmt(f *ast.ForInStmt, depth int) {
	op := " in "
	right := p.expr(f.Right, precLowest)
	if f.IsForOf {
		op = " of "
		right = p.expr(f.Right, precAssign)
	}
	head := "for "
	if f.Await {
		head = "for await "
	}
	left := p.expr(f.Left, precLHS)
	if f.Keyword != "" {
		left = f.Keyword + " " + left
	} else if strings.HasPrefix(left, "let") || (f.IsForOf && strings.HasPrefix(left, "async")) {
		left = "(" + left + ")"
	}
	p.line(depth, head+"("+left+op+right+") {")
	p.body(f.Body, depth+1)
	p.line(depth, "}")
}

func (p *printer) switchStmt(s *ast.SwitchStmt, depth int) {
	p.line(depth, "switch ("+p.expr(s.Discriminant, precLowest)+") {")
	for _, c := range s.Cases {
		if c.Test == nil {
			p.line(depth+1, "default:")
		} else {
			p.line(depth+1, "case "+p.expr(c.Test, precLowest)+":")
		}
		p.body(c.Body, depth+2)
	}
	p.line(depth, "}")
}

func (p *printer) tryStmt(t *ast.TryStmt, depth int) {
	p.line(depth, "try {")
	p.body(t.Body, depth+1)
	if t.Catch != nil {
		switch {
		case t.Catch.ParamTarget != nil:
			p.line(depth, "} catch ("+p.expr(t.Catch.ParamTarget, precAssign)+") {")
		case t.Catch.Param != "":
			p.line(depth, "} catch ("+t.Catch.Param+") {")
		default:
			p.line(depth, "} catch {")
		}
		p.body(t.Catch.Body, depth+1)
	}
	// An empty finally is printed only when it is needed for validity.
	if len(t.Finally) > 0 || t.Catch == nil {
		p.line(depth, "} finally {")
		p.body(t.Finally, depth+1)
	}
	p.line(depth, "}")
}

// helpers for statement text

func printImport(i *ast.ImportStmt) string {
	attrs := printAttributes(i.Attributes)
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
				names = append(names, printModuleName(n.Remote)+" as "+n.Local)
			} else if n.Local != "" {
				names = append(names, n.Local)
			}
		}
		parts = append(parts, "{ "+strings.Join(names, ", ")+" }")
	}
	if len(parts) == 0 {
		return "import " + i.Source + attrs + ";"
	}
	return "import " + strings.Join(parts, ", ") + " from " + i.Source + attrs + ";"
}

func varKeyword(k ast.VarKind) string {
	switch k {
	case ast.VarLet:
		return "let"
	case ast.VarVar:
		return "var"
	case ast.VarUsing:
		return "using"
	case ast.VarAwaitUsing:
		return "await using"
	}
	return "const"
}

func (p *printer) varDecl(v *ast.VarStmt) string {
	decls := make([]string, 0, len(v.Decls))
	for _, d := range v.Decls {
		name := d.Name
		switch {
		case d.Target != nil:
			name = p.expr(d.Target, precAssign)
		case d.IsDestructuring && d.Pattern != "":
			name = d.Pattern
		}
		if d.Init != nil {
			name += " = " + p.expr(d.Init, precAssign)
		}
		decls = append(decls, name)
	}
	return varKeyword(v.Kind) + " " + strings.Join(decls, ", ")
}

func (p *printer) params(params []*ast.Param) string {
	out := make([]string, 0, len(params))
	for _, prm := range params {
		out = append(out, p.param(prm))
	}
	return strings.Join(out, ", ")
}

func (p *printer) param(prm *ast.Param) string {
	if prm == nil {
		return ""
	}
	name := prm.Name
	switch {
	case prm.Target != nil:
		name = p.expr(prm.Target, precAssign)
	case prm.Pattern != "":
		name = prm.Pattern
	}
	if prm.IsRest {
		name = "..." + name
	}
	if prm.Default != nil {
		name += " = " + p.expr(prm.Default, precAssign)
	}
	return name
}

// printParams renders a parameter list (kept for callers in this package).
func printParams(params []*ast.Param) string { return (&printer{}).params(params) }
