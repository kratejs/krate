package astprint

import (
	"fmt"
	"strings"

	"github.com/kratejs/krate/packages/compiler/ast"
)

// Expression precedence levels (higher binds tighter). A child is wrapped in
// parentheses when its level is below the minimum its position requires.
const (
	precLowest = iota
	precSeq
	precAssign
	precCond
	precNullish
	precOr
	precAnd
	precBitOr
	precBitXor
	precBitAnd
	precEq
	precRel
	precShift
	precAdd
	precMul
	precExp
	precUnary
	precPostfix
	precLHS
	precPrimary
)

func binaryPrec(op string) int {
	switch op {
	case ",":
		return precSeq
	case "=", "+=", "-=", "*=", "/=", "%=", "**=", "<<=", ">>=", ">>>=", "&=", "|=", "^=", "&&=", "||=", "??=":
		return precAssign
	case "??":
		return precNullish
	case "||":
		return precOr
	case "&&":
		return precAnd
	case "|":
		return precBitOr
	case "^":
		return precBitXor
	case "&":
		return precBitAnd
	case "==", "!=", "===", "!==":
		return precEq
	case "<", ">", "<=", ">=", "in", "instanceof":
		return precRel
	case "<<", ">>", ">>>":
		return precShift
	case "+", "-":
		return precAdd
	case "*", "/", "%":
		return precMul
	case "**":
		return precExp
	}
	return precLowest
}

// exprPrecOf returns the precedence level of an expression node.
func exprPrecOf(e ast.Expr) int {
	switch v := e.(type) {
	case *ast.BinaryExpr:
		return binaryPrec(v.Op)
	case *ast.ConditionalExpr:
		return precCond
	case *ast.ArrowFn:
		if v.Function {
			return precPrimary
		}
		return precAssign
	case *ast.UnaryExpr:
		switch {
		case v.Op == "yield" || v.Op == "yield*" || v.Op == "...":
			return precAssign
		case v.Postfix:
			return precPostfix
		}
		return precUnary
	case *ast.AwaitExpr:
		return precUnary
	case *ast.TypeAssertion:
		if v.Expr == nil {
			return precPrimary
		}
		return exprPrecOf(v.Expr)
	case *ast.NewExpr, *ast.CallExpr, *ast.MemberExpr, *ast.TaggedTemplateExpr, *ast.DynamicImport:
		return precLHS
	}
	return precPrimary
}

// expr renders e, parenthesizing it when its precedence is below minPrec.
func (p *printer) expr(e ast.Expr, minPrec int) string {
	if e == nil {
		return ""
	}
	if ta, ok := e.(*ast.TypeAssertion); ok {
		return p.expr(ta.Expr, minPrec)
	}
	s := p.exprRaw(e)
	if exprPrecOf(e) < minPrec {
		return "(" + s + ")"
	}
	return s
}

func (p *printer) exprRaw(e ast.Expr) string {
	switch v := e.(type) {
	case *ast.Identifier:
		return v.Name
	case *ast.Literal:
		return printLiteral(v)
	case *ast.ThisExpr:
		return "this"
	case *ast.ImportMetaExpr:
		return "import.meta"
	case *ast.MetaProperty:
		return v.Meta + "." + v.Property
	case *ast.AwaitExpr:
		return "await " + p.expr(v.Arg, precUnary)
	case *ast.DynamicImport:
		s := "import(" + p.expr(v.Arg, precAssign)
		if v.Options != nil {
			s += ", " + p.expr(v.Options, precAssign)
		}
		return s + ")"
	case *ast.ParenExpr:
		return "(" + p.expr(v.Expr, precLowest) + ")"
	case *ast.UnaryExpr:
		return p.unary(v)
	case *ast.BinaryExpr:
		return p.binary(v)
	case *ast.ConditionalExpr:
		return p.expr(v.Test, precNullish) + " ? " + p.expr(v.Consequent, precAssign) + " : " + p.expr(v.Alternate, precAssign)
	case *ast.ArrowFn:
		return p.arrow(v)
	case *ast.ObjectExpr:
		return p.object(v)
	case *ast.ArrayExpr:
		return p.array(v)
	case *ast.TemplateExpr:
		return p.template(v)
	case *ast.TaggedTemplateExpr:
		return p.expr(v.Tag, precLHS) + p.template(v.Quasi)
	case *ast.MemberExpr:
		return p.member(v)
	case *ast.CallExpr:
		return p.call(v)
	case *ast.NewExpr:
		return p.newExpr(v)
	case *ast.ClassExpr:
		if v.Class == nil {
			return "class {}"
		}
		return p.classText(v.Class, 0)
	case *ast.JSXElement:
		return p.jsxElement(v)
	case *ast.JSXFragment:
		return p.jsxFragment(v)
	case *ast.TypeAssertion:
		return p.expr(v.Expr, precLowest)
	}
	return ""
}

func printLiteral(l *ast.Literal) string {
	switch l.Kind {
	case ast.StringLit:
		return quoteString(l.Value)
	case ast.NullLit:
		if l.Value == "" {
			return "null"
		}
		return l.Value
	}
	return l.Value
}

func (p *printer) unary(u *ast.UnaryExpr) string {
	switch u.Op {
	case "...":
		return "..." + p.expr(u.Arg, precAssign)
	case "yield", "yield*":
		if u.Arg == nil {
			return u.Op
		}
		return u.Op + " " + p.expr(u.Arg, precAssign)
	}
	if u.Postfix {
		return p.expr(u.Arg, precLHS) + u.Op
	}
	arg := p.expr(u.Arg, precUnary)
	switch u.Op {
	case "typeof", "void", "delete":
		return u.Op + " " + arg
	}
	// Keep `- -x` and `+ +x` (and `- --x`) from fusing into `--x`.
	if (u.Op == "-" || u.Op == "+") && strings.HasPrefix(arg, u.Op) {
		return u.Op + " " + arg
	}
	return u.Op + arg
}

func (p *printer) binary(b *ast.BinaryExpr) string {
	prec := binaryPrec(b.Op)
	var left, right string
	switch {
	case prec == precAssign:
		left = p.expr(b.Left, precLHS)
		if _, isPattern := b.Left.(*ast.ObjectExpr); isPattern {
			left = p.exprRaw(b.Left)
		} else if _, isPattern := b.Left.(*ast.ArrayExpr); isPattern {
			left = p.exprRaw(b.Left)
		}
		right = p.expr(b.Right, precAssign)
	case b.Op == "**":
		left = p.expr(b.Left, precExp+1)
		switch b.Left.(type) {
		case *ast.UnaryExpr, *ast.AwaitExpr:
			if !strings.HasPrefix(left, "(") || exprPrecOf(b.Left) >= precExp+1 {
				left = "(" + p.exprRaw(b.Left) + ")"
			}
		}
		right = p.expr(b.Right, precExp)
	case b.Op == ",":
		left = p.expr(b.Left, precSeq)
		right = p.expr(b.Right, precAssign)
	default:
		left = p.expr(b.Left, prec)
		right = p.expr(b.Right, prec+1)
		if b.Op == "in" && p.noIn {
			return "(" + left + " in " + right + ")"
		}
		// `??` cannot mix with unparenthesized `||`/`&&`.
		if b.Op == "??" || b.Op == "||" || b.Op == "&&" {
			left = p.wrapNullishMix(b.Op, b.Left, left)
			right = p.wrapNullishMix(b.Op, b.Right, right)
		}
	}
	return left + " " + b.Op + " " + right
}

func (p *printer) wrapNullishMix(op string, child ast.Expr, s string) string {
	be, ok := child.(*ast.BinaryExpr)
	if !ok || strings.HasPrefix(s, "(") && strings.HasSuffix(s, ")") && exprPrecOf(child) < binaryPrec(op) {
		return s
	}
	mixed := (op == "??" && (be.Op == "||" || be.Op == "&&")) || (op != "??" && be.Op == "??")
	if mixed {
		return "(" + s + ")"
	}
	return s
}

func (p *printer) arrow(a *ast.ArrowFn) string {
	if a.Function {
		return fnHead(a.Async, a.Generator, a.Name) + "(" + p.params(a.Params) + ") " + p.fnBody(a.Body)
	}
	params := "(" + p.params(a.Params) + ")"
	if a.Async {
		params = "async " + params
	}
	if a.Expression && len(a.Body) == 1 {
		var body ast.Expr
		switch s := a.Body[0].(type) {
		case *ast.ExprStmt:
			body = s.Expression
		case *ast.ReturnStmt:
			body = s.Value
		}
		if body != nil {
			text := p.expr(body, precAssign)
			if strings.HasPrefix(text, "{") {
				text = "(" + text + ")"
			}
			return params + " => " + text
		}
	}
	return params + " => " + p.fnBody(a.Body)
}

// fnBody renders `{ ... }` for a function body.
func (p *printer) fnBody(stmts []ast.Stmt) string {
	if len(stmts) == 0 {
		return "{}"
	}
	body := &printer{}
	body.body(stmts, 1)
	return "{\n" + body.b.String() + "}"
}

func (p *printer) propKey(prop *ast.ObjectProp) string {
	switch {
	case prop.Computed:
		return "[" + p.expr(prop.KeyExpr, precAssign) + "]"
	case prop.KeyQuoted:
		return quoteString(prop.Key)
	case prop.KeyNumeric:
		return prop.Key
	}
	return printObjectKey(prop.Key)
}

func (p *printer) object(o *ast.ObjectExpr) string {
	if len(o.Properties) == 0 {
		return "{}"
	}
	parts := make([]string, 0, len(o.Properties))
	for _, prop := range o.Properties {
		if prop == nil {
			continue
		}
		if prop.Spread {
			parts = append(parts, "..."+p.expr(prop.Value, precAssign))
			continue
		}
		key := p.propKey(prop)
		if prop.Method {
			fn, _ := prop.Value.(*ast.ArrowFn)
			prefix := ""
			switch {
			case prop.Getter:
				prefix = "get "
			case prop.Setter:
				prefix = "set "
			case fn != nil:
				if fn.Async {
					prefix = "async "
				}
				if fn.Generator {
					prefix += "*"
				}
			}
			if fn == nil {
				parts = append(parts, key+": "+p.expr(prop.Value, precAssign))
				continue
			}
			body := fn.Body
			if fn.Expression && len(body) == 1 {
				if es, ok := body[0].(*ast.ExprStmt); ok {
					body = []ast.Stmt{&ast.ReturnStmt{Value: es.Expression}}
				}
			}
			parts = append(parts, prefix+key+"("+p.params(fn.Params)+") "+p.fnBody(body))
			continue
		}
		if prop.Shorthand {
			parts = append(parts, p.expr(prop.Value, precAssign))
			continue
		}
		parts = append(parts, key+": "+p.expr(prop.Value, precAssign))
	}
	return "{ " + strings.Join(parts, ", ") + " }"
}

func (p *printer) array(a *ast.ArrayExpr) string {
	parts := make([]string, 0, len(a.Elements))
	for _, el := range a.Elements {
		parts = append(parts, p.expr(el, precAssign))
	}
	s := strings.Join(parts, ", ")
	// A trailing hole needs an extra comma: `[a, ,]` has length 2.
	if n := len(a.Elements); n > 0 && a.Elements[n-1] == nil {
		s += ","
	}
	return "[" + s + "]"
}

func (p *printer) template(t *ast.TemplateExpr) string {
	if t == nil {
		return "``"
	}
	var b strings.Builder
	b.WriteString("`")
	for i, raw := range t.Raw {
		b.WriteString(raw)
		if i < len(t.Parts) {
			b.WriteString("${")
			b.WriteString(p.expr(t.Parts[i], precLowest))
			b.WriteString("}")
		}
	}
	for i := len(t.Raw); i < len(t.Parts); i++ {
		b.WriteString("${" + p.expr(t.Parts[i], precLowest) + "}")
	}
	b.WriteString("`")
	return b.String()
}

// lhsObject renders the object of a member access or the callee of a call.
func (p *printer) lhsObject(e ast.Expr) string {
	s := p.expr(e, precLHS)
	if lit, ok := e.(*ast.Literal); ok && lit.Kind == ast.NumberLit && isPlainInteger(lit.Value) {
		return "(" + s + ")"
	}
	switch v := e.(type) {
	case *ast.ArrowFn:
		if v.Function {
			return "(" + s + ")"
		}
	case *ast.ClassExpr:
		return "(" + s + ")"
	}
	return s
}

func isPlainInteger(v string) bool {
	if v == "" {
		return false
	}
	for i := 0; i < len(v); i++ {
		c := v[i]
		if !(c >= '0' && c <= '9' || c == '_') {
			return false
		}
	}
	return true
}

func (p *printer) member(m *ast.MemberExpr) string {
	obj := p.lhsObject(m.Object)
	if m.Computed {
		if m.Optional {
			return obj + "?.[" + p.expr(m.Property, precLowest) + "]"
		}
		return obj + "[" + p.expr(m.Property, precLowest) + "]"
	}
	prop := ""
	if id, ok := m.Property.(*ast.Identifier); ok {
		prop = id.Name
	} else {
		prop = p.expr(m.Property, precPrimary)
	}
	if m.Optional {
		return obj + "?." + prop
	}
	return obj + "." + prop
}

func (p *printer) args(args []ast.Expr) string {
	parts := make([]string, 0, len(args))
	for _, a := range args {
		parts = append(parts, p.expr(a, precAssign))
	}
	return strings.Join(parts, ", ")
}

func (p *printer) call(c *ast.CallExpr) string {
	callee := p.lhsObject(c.Callee)
	if c.Optional {
		return callee + "?.(" + p.args(c.Args) + ")"
	}
	return callee + "(" + p.args(c.Args) + ")"
}

// containsCall reports whether a `new` callee contains a call or optional
// chain on its member spine, which would change what `new` binds to.
func containsCall(e ast.Expr) bool {
	for {
		switch v := e.(type) {
		case *ast.CallExpr:
			return true
		case *ast.MemberExpr:
			if v.Optional {
				return true
			}
			e = v.Object
		case *ast.TaggedTemplateExpr:
			e = v.Tag
		case *ast.TypeAssertion:
			e = v.Expr
		default:
			return false
		}
	}
}

func (p *printer) newExpr(n *ast.NewExpr) string {
	callee := p.expr(n.Callee, precLHS)
	if containsCall(n.Callee) && !strings.HasPrefix(callee, "(") {
		callee = "(" + callee + ")"
	}
	return "new " + callee + "(" + p.args(n.Args) + ")"
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
		return printJSXText(v.Value)
	case *ast.JSXExprContainer:
		if v.Expression == nil {
			return "{}"
		}
		return "{" + p.expr(v.Expression, precAssign) + "}"
	case *ast.JSXElementChild:
		return p.jsxElement(v.Element)
	case *ast.JSXFragmentChild:
		return p.jsxFragment(v.Fragment)
	}
	return ""
}

// printJSXText re-emits a decoded JSX text child. Text containing characters
// that are special in JSX (or that the whitespace transform would alter) is
// printed as a string expression container.
func printJSXText(s string) string {
	if s == "" {
		return ""
	}
	if strings.ContainsAny(s, "{}<>&\n\r\t") || strings.Contains(s, "  ") {
		return "{" + quoteString(s) + "}"
	}
	return s
}

func printJSXAttr(attr *ast.JSXAttr, p *printer) string {
	if attr == nil {
		return ""
	}
	if attr.Spread {
		return "{..." + p.expr(attr.Value, precAssign) + "}"
	}
	if attr.Name == "" {
		return "{" + p.expr(attr.Value, precAssign) + "}"
	}
	switch v := attr.Value.(type) {
	case *ast.Literal:
		if v.Kind == ast.BoolLit && v.Value == "true" && v.Position == attr.Position {
			return attr.Name
		}
		if v.Kind == ast.StringLit {
			switch {
			case !strings.ContainsAny(v.Value, "\"&"):
				return attr.Name + `="` + v.Value + `"`
			case !strings.ContainsAny(v.Value, "'&"):
				return attr.Name + `='` + v.Value + `'`
			}
		}
	case *ast.JSXElement, *ast.JSXFragment:
		return attr.Name + "=" + p.expr(attr.Value, precPrimary)
	}
	return attr.Name + "={" + p.expr(attr.Value, precAssign) + "}"
}

// quoteString re-emits a string literal value. The parser decodes escape
// sequences at parse time, so the value is the literal runtime string; it is
// wrapped in double quotes with every character escaped that would otherwise
// change meaning or break the literal.
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
		case 0x2028, 0x2029:
			fmt.Fprintf(&b, `\u%04x`, r)
		default:
			if r < 0x20 || r == 0x7f {
				fmt.Fprintf(&b, `\x%02x`, r)
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}

func printObjectKey(key string) string {
	if isJSIdent(key) {
		return key
	}
	return quoteString(key)
}

func isJSIdent(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		if r == '_' || r == '$' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r > 0x7f {
			continue
		}
		if i > 0 && r >= '0' && r <= '9' {
			continue
		}
		return false
	}
	return true
}
