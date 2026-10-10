package astprint

import (
	"strings"

	"github.com/kratejs/krate/packages/compiler/ast"
)

// expr renders an expression. Parenthesization is applied conservatively: a
// nested expression is wrapped whenever its node kind could otherwise reparse
// with different grouping. The output is intended to be re-parsed, not to be
// minimally parenthesized.
func (p *printer) expr(e ast.Expr) string {
	switch v := e.(type) {
	case nil:
		return ""
	case *ast.Identifier:
		return v.Name
	case *ast.Literal:
		return printLiteral(v)
	case *ast.ThisExpr:
		return "this"
	case *ast.ImportMetaExpr:
		return "import.meta"
	case *ast.AwaitExpr:
		return "await " + p.expr(v.Arg)
	case *ast.DynamicImport:
		return "import(" + p.expr(v.Arg) + ")"
	case *ast.TypeAssertion:
		// Runtime expression only; the type is compile-time and dropped.
		return p.expr(v.Expr)
	case *ast.UnaryExpr:
		return p.unary(v)
	case *ast.BinaryExpr:
		return p.binary(v)
	case *ast.ConditionalExpr:
		return p.expr(v.Test) + " ? " + p.expr(v.Consequent) + " : " + p.expr(v.Alternate)
	case *ast.ArrowFn:
		return p.arrow(v)
	case *ast.ObjectExpr:
		return p.object(v)
	case *ast.ArrayExpr:
		return p.array(v)
	case *ast.TemplateExpr:
		return p.template(v)
	case *ast.MemberExpr:
		return p.member(v)
	case *ast.CallExpr:
		return p.call(v)
	case *ast.NewExpr:
		return p.newExpr(v)
	case *ast.JSXElement:
		return p.jsxElement(v)
	case *ast.JSXFragment:
		return p.jsxFragment(v)
	default:
		return ""
	}
}

func printLiteral(l *ast.Literal) string {
	switch l.Kind {
	case ast.StringLit:
		return quoteString(l.Value)
	case ast.NumberLit:
		return l.Value
	case ast.BoolLit:
		return l.Value
	case ast.NullLit:
		if l.Value == "" {
			return "null"
		}
		return l.Value
	case ast.RegexpLit:
		return l.Value
	default:
		return l.Value
	}
}

func (p *printer) unary(u *ast.UnaryExpr) string {
	arg := p.expr(u.Arg)
	switch u.Op {
	case "typeof", "void", "delete", "yield":
		s := u.Op + " " + arg
		if u.Postfix {
			return "(" + s + ")"
		}
		return s
	}
	if u.Postfix {
		return arg + u.Op
	}
	return u.Op + arg
}

// binaryPrec mirrors the parser's precedence table so nested binary
// expressions reparse with the same grouping.
func binaryPrec(op string) int {
	switch op {
	case "=", "+=", "-=", "*=", "/=", "%=":
		return 1
	case "??":
		return 2
	case "||":
		return 3
	case "&&":
		return 4
	case "|":
		return 5
	case "^":
		return 6
	case "&":
		return 7
	case "==", "!=", "===", "!==":
		return 8
	case "<", ">", "<=", ">=", "in", "instanceof":
		return 9
	case "<<", ">>", ">>>":
		return 10
	case "+", "-":
		return 11
	case "*", "/", "%":
		return 12
	default:
		return 0
	}
}

func (p *printer) binary(b *ast.BinaryExpr) string {
	prec := binaryPrec(b.Op)
	left := p.exprPrec(b.Left, prec, false)
	right := p.exprPrec(b.Right, prec, true)
	return left + " " + b.Op + " " + right
}

// exprPrec renders e, wrapping in parens when e's own precedence is lower than
// the parent's. Right-hand operands at equal precedence are wrapped too, which
// is conservative but always correct.
func (p *printer) exprPrec(e ast.Expr, parentPrec int, right bool) string {
	s := p.expr(e)
	be, ok := e.(*ast.BinaryExpr)
	if !ok {
		return s
	}
	childPrec := binaryPrec(be.Op)
	if childPrec < parentPrec || (right && childPrec == parentPrec) {
		return "(" + s + ")"
	}
	return s
}

func (p *printer) arrow(a *ast.ArrowFn) string {
	params := printParams(a.Params)
	if a.Async {
		params = "async (" + params + ")"
	}
	if a.Expression && len(a.Body) == 1 {
		if es, ok := a.Body[0].(*ast.ExprStmt); ok {
			return params + " => " + p.expr(es.Expression)
		}
		if rs, ok := a.Body[0].(*ast.ReturnStmt); ok {
			return params + " => " + p.expr(rs.Value)
		}
	}
	body := &printer{}
	body.body(a.Body, 0)
	return params + " => {\n" + body.b.String() + "}"
}

func (p *printer) object(o *ast.ObjectExpr) string {
	if len(o.Properties) == 0 {
		return "{}"
	}
	var parts []string
	for _, prop := range o.Properties {
		if prop.Spread {
			parts = append(parts, "..."+p.expr(prop.Value))
			continue
		}
		key := prop.Key
		if prop.Method {
			parts = append(parts, key+"("+p.arrowParams(prop.Value)+") "+p.arrowBlock(prop.Value))
			continue
		}
		if prop.Shorthand {
			parts = append(parts, key)
			continue
		}
		parts = append(parts, key+": "+p.expr(prop.Value))
	}
	return "{ " + strings.Join(parts, ", ") + " }"
}

func (p *printer) arrowParams(e ast.Expr) string {
	if a, ok := e.(*ast.ArrowFn); ok {
		return printParams(a.Params)
	}
	return ""
}

func (p *printer) arrowBlock(e ast.Expr) string {
	a, ok := e.(*ast.ArrowFn)
	if !ok {
		return "{}"
	}
	if a.Expression && len(a.Body) == 1 {
		if es, ok := a.Body[0].(*ast.ExprStmt); ok {
			return "{ return " + p.expr(es.Expression) + "; }"
		}
	}
	body := &printer{}
	body.body(a.Body, 0)
	return "{\n" + body.b.String() + "}"
}

func (p *printer) array(a *ast.ArrayExpr) string {
	var parts []string
	for _, el := range a.Elements {
		parts = append(parts, p.expr(el))
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

func (p *printer) template(t *ast.TemplateExpr) string {
	var b strings.Builder
	b.WriteString("`")
	if len(t.Parts) == 0 {
		b.WriteString(strings.Join(t.Raw, ""))
		b.WriteString("`")
		return b.String()
	}
	for i, part := range t.Parts {
		if i < len(t.Raw) {
			b.WriteString(t.Raw[i])
		}
		b.WriteString("${")
		b.WriteString(p.expr(part))
		b.WriteString("}")
	}
	if len(t.Raw) > len(t.Parts) {
		b.WriteString(t.Raw[len(t.Parts)])
	}
	b.WriteString("`")
	return b.String()
}

func (p *printer) member(m *ast.MemberExpr) string {
	obj := p.expr(m.Object)
	// Wrap low-precedence objects (binary/conditional/arrow) so `.` binds right.
	switch m.Object.(type) {
	case *ast.BinaryExpr, *ast.ConditionalExpr, *ast.ArrowFn, *ast.NewExpr:
		obj = "(" + obj + ")"
	}
	if m.Computed {
		if m.Optional {
			return obj + "?.[" + p.expr(m.Property) + "]"
		}
		return obj + "[" + p.expr(m.Property) + "]"
	}
	if m.Optional {
		return obj + "?." + p.expr(m.Property)
	}
	return obj + "." + p.expr(m.Property)
}

func (p *printer) call(c *ast.CallExpr) string {
	callee := p.expr(c.Callee)
	switch c.Callee.(type) {
	case *ast.ArrowFn, *ast.BinaryExpr, *ast.ConditionalExpr:
		callee = "(" + callee + ")"
	}
	var args []string
	for _, a := range c.Args {
		args = append(args, p.expr(a))
	}
	if c.Optional {
		return callee + "?.(" + strings.Join(args, ", ") + ")"
	}
	return callee + "(" + strings.Join(args, ", ") + ")"
}

func (p *printer) newExpr(n *ast.NewExpr) string {
	var args []string
	for _, a := range n.Args {
		args = append(args, p.expr(a))
	}
	return "new " + p.expr(n.Callee) + "(" + strings.Join(args, ", ") + ")"
}

// JSX

func (p *printer) jsxElement(el *ast.JSXElement) string {
	if el == nil || el.Opening == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString("<")
	b.WriteString(el.Opening.Name)
	for _, attr := range el.Opening.Attributes {
		b.WriteString(" ")
		b.WriteString(printJSXAttr(attr, p))
	}
	if el.Opening.SelfClosing {
		b.WriteString(" />")
		return b.String()
	}
	b.WriteString(">")
	for _, c := range el.Children {
		b.WriteString(p.jsxChild(c))
	}
	b.WriteString("</" + el.Opening.Name + ">")
	return b.String()
}

func (p *printer) jsxFragment(f *ast.JSXFragment) string {
	var b strings.Builder
	b.WriteString("<>")
	for _, c := range f.Children {
		b.WriteString(p.jsxChild(c))
	}
	b.WriteString("</>")
	return b.String()
}

func (p *printer) jsxChild(c ast.JSXChild) string {
	switch v := c.(type) {
	case *ast.JSXText:
		return v.Value
	case *ast.JSXExprContainer:
		if v.Expression == nil {
			return "{}"
		}
		// A bare object literal in `{...}` must be wrapped, matching JSX rules.
		e := p.expr(v.Expression)
		if _, ok := v.Expression.(*ast.ObjectExpr); ok {
			e = "(" + e + ")"
		}
		return "{" + e + "}"
	case *ast.JSXElementChild:
		return p.jsxElement(v.Element)
	case *ast.JSXFragmentChild:
		return p.jsxFragment(v.Fragment)
	default:
		return ""
	}
}

func printJSXAttr(attr *ast.JSXAttr, p *printer) string {
	if attr == nil {
		return ""
	}
	if attr.Spread {
		return "{..." + p.expr(attr.Value) + "}"
	}
	if attr.Name == "" {
		return "{" + p.expr(attr.Value) + "}"
	}
	lit, isLit := attr.Value.(*ast.Literal)
	if isLit && lit.Kind == ast.BoolLit && lit.Value == "true" {
		return attr.Name
	}
	if isLit && lit.Kind == ast.StringLit && isSafeJSXString(lit.Value) {
		return attr.Name + `="` + lit.Value + `"`
	}
	return attr.Name + "={" + p.expr(attr.Value) + "}"
}

// isSafeJSXString reports whether a string value can be emitted as an unquoted
// JSX attribute without re-escaping (no quotes/braces/whitespace runs).
func isSafeJSXString(s string) bool {
	if s == "" {
		return true
	}
	return !strings.ContainsAny(s, `"'<>{}\n\t`) && s == strings.TrimSpace(s)
}

// quoteString re-emits a string literal value. The parser decodes escape
// sequences at parse time (see parser.decodeStringToken), so the value is the
// literal runtime string. We wrap it in double quotes and escape every
// character that would otherwise change meaning or break the literal, so the
// emitted source reparses to the same string.
func quoteString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '\\':
			b.WriteString(`\\`)
		case '"':
			b.WriteString(`\"`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}
