package irtree

import (
	"fmt"
	"strings"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/escape"
)

// indentUnit is one level of source indentation. The generated JS is emitted
// readable (newlines + indentation) and minified by esbuild for production, so
// devtools and view-source show sane code without shipping the extra bytes.
const indentUnit = "  "

func indent(n int) string {
	if n <= 0 {
		return ""
	}
	return strings.Repeat(indentUnit, n)
}

// generateExprJS converts an AST expression to a JavaScript source string.
// Used for handler bodies, effect expressions, and complex slot expressions.
func generateExprJS(expr ast.Expr, signals map[string]ast.Expr) string {
	if expr == nil {
		return ""
	}
	switch e := expr.(type) {
	case *ast.Literal:
		switch e.Kind {
		case ast.StringLit:
			return "'" + escape.JSString(e.Value) + "'"
		case ast.NullLit:
			// Preserve undefined vs null distinction (both parse to NullLit;
			// Value carries the original spelling).
			if e.Value == "undefined" {
				return "undefined"
			}
			return "null"
		case ast.BoolLit:
			if e.Value == "true" {
				return "true"
			}
			return "false"
		default:
			return e.Value
		}
	case *ast.Identifier:
		return e.Name
	case *ast.CallExpr:
		calleeJS := generateExprJS(e.Callee, signals)
		opt := ""
		if e.Optional {
			opt = "?."
		}
		var argsJS []string
		for _, arg := range e.Args {
			argsJS = append(argsJS, generateExprJS(arg, signals))
		}
		return calleeJS + opt + "(" + strings.Join(argsJS, ", ") + ")"
	case *ast.MemberExpr:
		objJS := generateExprJS(e.Object, signals)
		if e.Computed {
			propJS := generateExprJS(e.Property, signals)
			if e.Optional {
				return objJS + "?.[" + propJS + "]"
			}
			return objJS + "[" + propJS + "]"
		}
		propName := ""
		if id, ok := e.Property.(*ast.Identifier); ok {
			propName = id.Name
		} else {
			propName = generateExprJS(e.Property, signals)
		}
		if e.Optional {
			return objJS + "?." + propName
		}
		return objJS + "." + propName
	case *ast.BinaryExpr:
		left := generateExprJS(e.Left, signals)
		right := generateExprJS(e.Right, signals)
		// Preserve real JS semantics for `&&` (falsy left operand is returned
		// unchanged rather than collapsed to ''). The runtime and SSR renderer
		// both skip false/null/undefined, so conditional JSX still renders
		// nothing when the guard fails - matching React.
		return "(" + left + " " + e.Op + " " + right + ")"
	case *ast.UnaryExpr:
		arg := generateExprJS(e.Arg, signals)
		if e.Postfix {
			return arg + e.Op
		}
		if e.Op == "typeof" {
			// typeof is a keyword; concatenating without a space yields an
			// identifier (`typeofx`) instead of the typeof operator.
			return "typeof " + arg
		}
		return e.Op + arg
	case *ast.ConditionalExpr:
		test := generateExprJS(e.Test, signals)
		consequent := generateExprJS(e.Consequent, signals)
		alternate := generateExprJS(e.Alternate, signals)
		return "(" + test + "?" + consequent + ":" + alternate + ")"
	case *ast.ArrowFn:
		return renderArrowFn(e, signals)
	case *ast.ArrayExpr:
		var elems []string
		for _, el := range e.Elements {
			elems = append(elems, generateExprJS(el, signals))
		}
		return "[" + strings.Join(elems, ",") + "]"
	case *ast.ObjectExpr:
		return generateObjectExpr(e, signals)
	case *ast.TemplateExpr:
		return generateTemplateExpr(e, signals)
	case *ast.NewExpr:
		calleeJS := generateExprJS(e.Callee, signals)
		var argsJS []string
		for _, arg := range e.Args {
			argsJS = append(argsJS, generateExprJS(arg, signals))
		}
		return "new " + calleeJS + "(" + strings.Join(argsJS, ", ") + ")"
	case *ast.TypeAssertion:
		return generateExprJS(e.Expr, signals)
	case *ast.AwaitExpr:
		return "await " + generateExprJS(e.Arg, signals)
	case *ast.DynamicImport:
		return "import(" + generateExprJS(e.Arg, signals) + ")"
	case *ast.ImportMetaExpr:
		return "import.meta"
	case *ast.ThisExpr:
		return "this"
	case *ast.JSXElement:
		return generateJSXJS(e, signals)
	case *ast.JSXFragment:
		return generateJSXFragmentJS(e, signals)
	default:
		// Unknown node: emit a visible placeholder rather than silently
		// dropping the expression. The build validator (codegenIssues) fails
		// the build for unsupported nodes, so this is a defensive fallback.
		return "/*krate:unsupported:" + fmt.Sprintf("%T", expr) + "*/undefined"
	}
}

// GenerateExprJS renders an AST expression to JavaScript source. Exported so
// the renderer's SSR evaluator can hand expressions to the embedded QuickJS
// engine for evaluation (real JS built-ins: Date, Math, String, Number, ...).
func GenerateExprJS(expr ast.Expr, signals map[string]ast.Expr) string {
	return generateExprJS(expr, signals)
}

// renderParam renders a function parameter including rest and default forms.
func renderParam(p *ast.Param) string {
	if p == nil {
		return ""
	}
	s := p.Name
	if p.Pattern != "" {
		s = p.Pattern
	}
	if p.IsRest {
		s = "..." + s
	}
	if p.Default != nil {
		s += "=" + generateExprJS(p.Default, nil)
	}
	return s
}

func renderParams(params []*ast.Param) string {
	var parts []string
	for _, p := range params {
		parts = append(parts, renderParam(p))
	}
	return strings.Join(parts, ", ")
}

// renderArrowFn renders an ArrowFn AST node to a JS function expression string.
func renderArrowFn(fn *ast.ArrowFn, signals map[string]ast.Expr) string {
	if fn == nil {
		return "()=>{}"
	}
	var b strings.Builder
	if fn.Async {
		b.WriteString("async ")
	}
	b.WriteByte('(')
	b.WriteString(renderParams(fn.Params))
	b.WriteString(")=>")
	if fn.Expression {
		// Expression body: return the expression
		bodyExpr := arrowBodyExpr(fn)
		if bodyExpr != nil {
			b.WriteString(generateExprJS(bodyExpr, signals))
		} else {
			b.WriteString("{}")
		}
	} else {
		// Block body
		b.WriteByte('{')
		if len(fn.Body) > 0 {
			b.WriteByte('\n')
			b.WriteString(renderStmtBlock(fn.Body, signals, 1))
			b.WriteByte('\n')
		}
		b.WriteByte('}')
	}
	return b.String()
}

// RenderComponentFnJS renders a component function declaration (its param list
// and body) to a JS function string suitable for inclusion in the hydration
// scope, where the runtime can invoke it via h(ComponentName, props) when
// re-rendering dynamic lists. JSX in the body compiles to h() calls.
func RenderComponentFnJS(fn *ast.FnDecl) string {
	if fn == nil {
		return ""
	}
	return renderStmtJS(fn, nil)
}

// renderStmtJS renders a top-level statement to JS source.
func renderStmtJS(stmt ast.Stmt, signals map[string]ast.Expr) string {
	return renderStmt(stmt, signals, 0)
}

// renderStmtBlock renders a list of statements each on its own line, indented
// to the given level, without a trailing newline.
func renderStmtBlock(stmts []ast.Stmt, signals map[string]ast.Expr, level int) string {
	var b strings.Builder
	for i, s := range stmts {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(indent(level))
		b.WriteString(renderStmt(s, signals, level))
	}
	return b.String()
}

// renderStmt renders a statement to JS source. The returned string does not
// include leading indentation for the statement itself; nested blocks are
// indented relative to level.
func renderStmt(stmt ast.Stmt, signals map[string]ast.Expr, level int) string {
	switch s := stmt.(type) {
	case *ast.ReturnStmt:
		if s.Value != nil {
			return "return " + generateExprJS(s.Value, signals) + ";"
		}
		return "return;"
	case *ast.ExprStmt:
		return generateExprJS(s.Expression, signals) + ";"
	case *ast.VarStmt:
		var parts []string
		keyword := varKeyword(s.Kind)
		for _, decl := range s.Decls {
			if decl.IsDestructuring {
				pattern := decl.Pattern
				if pattern == "" {
					pattern = "[" + strings.Join(decl.Names, ",") + "]"
				}
				parts = append(parts, keyword+" "+pattern+"="+generateExprJS(decl.Init, signals))
			} else if decl.Name != "" {
				init := ""
				if decl.Init != nil {
					init = "=" + generateExprJS(decl.Init, signals)
				}
				parts = append(parts, keyword+" "+decl.Name+init)
			}
		}
		return strings.Join(parts, ";") + ";"
	case *ast.IfStmt:
		test := generateExprJS(s.Test, signals)
		var b strings.Builder
		b.WriteString("if (")
		b.WriteString(test)
		b.WriteString(") {")
		if len(s.Consequent) > 0 {
			b.WriteByte('\n')
			b.WriteString(renderStmtBlock(s.Consequent, signals, level+1))
			b.WriteByte('\n')
			b.WriteString(indent(level))
		}
		b.WriteByte('}')
		if len(s.Alternate) > 0 {
			// Render `else if` chains without an extra nesting level.
			if len(s.Alternate) == 1 {
				if elif, ok := s.Alternate[0].(*ast.IfStmt); ok {
					b.WriteString(" else ")
					b.WriteString(renderStmt(elif, signals, level))
					return b.String()
				}
			}
			b.WriteString(" else {")
			b.WriteByte('\n')
			b.WriteString(renderStmtBlock(s.Alternate, signals, level+1))
			b.WriteByte('\n')
			b.WriteString(indent(level))
			b.WriteByte('}')
		}
		return b.String()
	case *ast.BlockStmt:
		if len(s.Body) == 0 {
			return "{}"
		}
		var b strings.Builder
		b.WriteString("{\n")
		b.WriteString(renderStmtBlock(s.Body, signals, level+1))
		b.WriteByte('\n')
		b.WriteString(indent(level))
		b.WriteByte('}')
		return b.String()
	case *ast.FnDecl:
		var b strings.Builder
		if s.Async {
			b.WriteString("async ")
		}
		b.WriteString("function ")
		b.WriteString(s.Name)
		b.WriteString("(")
		b.WriteString(renderParams(s.Params))
		b.WriteString(") {")
		if len(s.Body) > 0 {
			b.WriteByte('\n')
			b.WriteString(renderStmtBlock(s.Body, signals, level+1))
			b.WriteByte('\n')
			b.WriteString(indent(level))
		}
		b.WriteByte('}')
		return b.String()
	case *ast.ForStmt:
		var b strings.Builder
		b.WriteString("for (")
		if s.Init != nil {
			if vs, ok := s.Init.(*ast.VarStmt); ok {
				b.WriteString(renderVarInitJS(vs, signals))
			} else {
				init := renderStmt(s.Init, signals, level)
				init = strings.TrimSuffix(init, ";")
				b.WriteString(init)
			}
		}
		b.WriteString("; ")
		if s.Test != nil {
			b.WriteString(generateExprJS(s.Test, signals))
		}
		b.WriteString("; ")
		if s.Update != nil {
			b.WriteString(generateExprJS(s.Update, signals))
		}
		b.WriteString(") {")
		if len(s.Body) > 0 {
			b.WriteByte('\n')
			b.WriteString(renderStmtBlock(s.Body, signals, level+1))
			b.WriteByte('\n')
			b.WriteString(indent(level))
		}
		b.WriteByte('}')
		return b.String()
	case *ast.ForInStmt:
		op := "in"
		if s.IsForOf {
			op = "of"
		}
		left := generateExprJS(s.Left, signals)
		var b strings.Builder
		b.WriteString("for (")
		if s.Keyword != "" {
			b.WriteString(s.Keyword)
			b.WriteByte(' ')
		}
		b.WriteString(left)
		b.WriteByte(' ')
		b.WriteString(op)
		b.WriteByte(' ')
		b.WriteString(generateExprJS(s.Right, signals))
		b.WriteString(") {")
		if len(s.Body) > 0 {
			b.WriteByte('\n')
			b.WriteString(renderStmtBlock(s.Body, signals, level+1))
			b.WriteByte('\n')
			b.WriteString(indent(level))
		}
		b.WriteByte('}')
		return b.String()
	case *ast.WhileStmt:
		var b strings.Builder
		b.WriteString("while (")
		b.WriteString(generateExprJS(s.Test, signals))
		b.WriteString(") {")
		if len(s.Body) > 0 {
			b.WriteByte('\n')
			b.WriteString(renderStmtBlock(s.Body, signals, level+1))
			b.WriteByte('\n')
			b.WriteString(indent(level))
		}
		b.WriteByte('}')
		return b.String()
	case *ast.DoWhileStmt:
		var b strings.Builder
		b.WriteString("do {")
		if len(s.Body) > 0 {
			b.WriteByte('\n')
			b.WriteString(renderStmtBlock(s.Body, signals, level+1))
			b.WriteByte('\n')
			b.WriteString(indent(level))
		}
		b.WriteString("} while (")
		b.WriteString(generateExprJS(s.Test, signals))
		b.WriteString(");")
		return b.String()
	case *ast.BreakStmt:
		if s.Label != "" {
			return "break " + s.Label + ";"
		}
		return "break;"
	case *ast.ContinueStmt:
		if s.Label != "" {
			return "continue " + s.Label + ";"
		}
		return "continue;"
	case *ast.SwitchStmt:
		var b strings.Builder
		b.WriteString("switch (")
		b.WriteString(generateExprJS(s.Discriminant, signals))
		b.WriteString(") {\n")
		for _, c := range s.Cases {
			if c.Test != nil {
				b.WriteString(indent(level + 1))
				b.WriteString("case ")
				b.WriteString(generateExprJS(c.Test, signals))
				b.WriteString(":\n")
			} else {
				b.WriteString(indent(level + 1))
				b.WriteString("default:\n")
			}
			if len(c.Body) > 0 {
				b.WriteString(renderStmtBlock(c.Body, signals, level+2))
				b.WriteByte('\n')
			}
		}
		b.WriteString(indent(level))
		b.WriteByte('}')
		return b.String()
	case *ast.TryStmt:
		var b strings.Builder
		b.WriteString("try {")
		if len(s.Body) > 0 {
			b.WriteByte('\n')
			b.WriteString(renderStmtBlock(s.Body, signals, level+1))
			b.WriteByte('\n')
			b.WriteString(indent(level))
		}
		b.WriteByte('}')
		if s.Catch != nil {
			b.WriteString(" catch (")
			b.WriteString(s.Catch.Param)
			b.WriteString(") {")
			if len(s.Catch.Body) > 0 {
				b.WriteByte('\n')
				b.WriteString(renderStmtBlock(s.Catch.Body, signals, level+1))
				b.WriteByte('\n')
				b.WriteString(indent(level))
			}
			b.WriteByte('}')
		}
		if len(s.Finally) > 0 {
			b.WriteString(" finally {")
			b.WriteByte('\n')
			b.WriteString(renderStmtBlock(s.Finally, signals, level+1))
			b.WriteByte('\n')
			b.WriteString(indent(level))
			b.WriteByte('}')
		}
		return b.String()
	case *ast.ThrowStmt:
		return "throw " + generateExprJS(s.Value, signals) + ";"
	default:
		return "/*krate:unsupported:" + fmt.Sprintf("%T", stmt) + "*/;"
	}
}

func varKeyword(kind ast.VarKind) string {
	switch kind {
	case ast.VarConst:
		return "const"
	case ast.VarLet:
		return "let"
	default:
		return "var"
	}
}

// generateObjectExpr renders an ObjectExpr to JS source.
func generateObjectExpr(obj *ast.ObjectExpr, signals map[string]ast.Expr) string {
	var parts []string
	for _, prop := range obj.Properties {
		if prop.Spread {
			parts = append(parts, "..."+generateExprJS(prop.Value, signals))
			continue
		}
		if prop.Method {
			parts = append(parts, generateMethod(prop.Key, prop.Value, signals))
			continue
		}
		valJS := generateExprJS(prop.Value, signals)
		if prop.Shorthand {
			parts = append(parts, prop.Key)
		} else {
			parts = append(parts, jsObjectKey(prop.Key)+":"+valJS)
		}
	}
	return "{" + strings.Join(parts, ",") + "}"
}

// generateMethod renders an object-literal method shorthand (e.g.
// `{ onSubmit() { ... } }`) from its function value.
func generateMethod(key string, value ast.Expr, signals map[string]ast.Expr) string {
	async := false
	var params []*ast.Param
	var body []ast.Stmt
	switch fn := value.(type) {
	case *ast.ArrowFn:
		async = fn.Async
		params = fn.Params
		if fn.Expression {
			if e := arrowBodyExpr(fn); e != nil {
				body = []ast.Stmt{&ast.ReturnStmt{Value: e}}
			}
		} else {
			body = fn.Body
		}
	default:
		return jsObjectKey(key) + ":" + generateExprJS(value, signals)
	}
	var b strings.Builder
	if async {
		b.WriteString("async ")
	}
	b.WriteString(jsObjectKey(key))
	b.WriteByte('(')
	b.WriteString(renderParams(params))
	b.WriteString(") {")
	if len(body) > 0 {
		b.WriteByte('\n')
		b.WriteString(renderStmtBlock(body, signals, 1))
		b.WriteByte('\n')
	}
	b.WriteByte('}')
	return b.String()
}

// generateTemplateExpr renders a TemplateExpr to JS source.
func generateTemplateExpr(t *ast.TemplateExpr, signals map[string]ast.Expr) string {
	var b strings.Builder
	b.WriteByte('`')
	for i, raw := range t.Raw {
		b.WriteString(raw)
		if i < len(t.Parts) {
			b.WriteString("${")
			b.WriteString(generateExprJS(t.Parts[i], signals))
			b.WriteByte('}')
		}
	}
	b.WriteByte('`')
	return b.String()
}

// arrowBodyExpr extracts the body expression from an arrow function.
func arrowBodyExpr(arrow *ast.ArrowFn) ast.Expr {
	if arrow == nil {
		return nil
	}
	if arrow.Expression {
		for _, s := range arrow.Body {
			if ret, ok := s.(*ast.ReturnStmt); ok && ret.Value != nil {
				return ret.Value
			}
			if exprStmt, ok := s.(*ast.ExprStmt); ok {
				return exprStmt.Expression
			}
		}
	}
	for _, s := range arrow.Body {
		if ret, ok := s.(*ast.ReturnStmt); ok && ret.Value != nil {
			return ret.Value
		}
	}
	return nil
}

// generateJSXJS converts a JSXElement AST node to a JavaScript h() call.
func generateJSXJS(el *ast.JSXElement, signals map[string]ast.Expr) string {
	var b strings.Builder
	name := el.Opening.Name
	// The built-in <Link> lowers to an <a> carrying the SPA/prefetch data
	// attributes - in the client codegen as well as the server/IR path. Without
	// this, a client component using <Link> would emit `h(Link, ...)` and throw
	// "Link is not defined" at hydration.
	if name == "Link" {
		return generateLinkJSX(el, signals)
	}
	// Uppercase tag names are components - reference them as function refs so
	// the runtime `h()` invokes the component rather than creating a DOM
	// element with a bogus tag like <Toast>.
	if len(name) > 0 && name[0] >= 'A' && name[0] <= 'Z' {
		b.WriteString("h(")
		b.WriteString(name)
	} else {
		b.WriteString("h('")
		b.WriteString(name)
		b.WriteString("'")
	}
	b.WriteString(",")

	// Props object
	if len(el.Opening.Attributes) > 0 {
		b.WriteString("{")
		for i, attr := range el.Opening.Attributes {
			if i > 0 {
				b.WriteString(",")
			}
			if attr.Spread {
				b.WriteString("...")
				b.WriteString(generateExprJS(attr.Value, signals))
			} else {
				propName := attr.Name
				if propName == "className" {
					propName = "class"
				}
				b.WriteString(jsObjectKey(propName))
				b.WriteString(":")
				if attr.Value != nil {
					b.WriteString(generateExprJS(attr.Value, signals))
				} else {
					b.WriteString("true")
				}
			}
		}
		b.WriteString("}")
	} else {
		b.WriteString("null")
	}

	// Children
	for _, child := range el.Children {
		b.WriteString(",")
		switch c := child.(type) {
		case *ast.JSXText:
			b.WriteString("'")
			b.WriteString(escape.JSString(c.Value))
			b.WriteString("'")
		case *ast.JSXExprContainer:
			b.WriteString(generateExprJS(c.Expression, signals))
		case *ast.JSXElementChild:
			b.WriteString(generateJSXJS(c.Element, signals))
		case *ast.JSXFragmentChild:
			b.WriteString(generateJSXFragmentJS(c.Fragment, signals))
		}
	}
	b.WriteString(")")
	return b.String()
}

// generateLinkJSX lowers the built-in <Link> to an <a> with the SPA navigation
// and prefetch data attributes the runtime router looks for. The router filters
// non-local links at runtime (origin/scheme), so `data-krate-link` is always
// emitted for internal handling; `external` opts out explicitly.
func generateLinkJSX(el *ast.JSXElement, signals map[string]ast.Expr) string {
	prefetch := "true"
	replace := ""
	external := false
	scrollFalse := false
	var props []string
	for _, attr := range el.Opening.Attributes {
		if attr.Spread {
			props = append(props, "..."+generateExprJS(attr.Value, signals))
			continue
		}
		var val string
		if attr.Value != nil {
			val = generateExprJS(attr.Value, signals)
		}
		switch attr.Name {
		case "prefetch":
			if attr.Value != nil {
				prefetch = val
			}
		case "replace":
			if attr.Value == nil {
				replace = "true"
			} else {
				replace = val
			}
		case "external":
			external = true
		case "scroll":
			if lit, ok := attr.Value.(*ast.Literal); ok && lit.Value == "false" {
				scrollFalse = true
			} else if attr.Value != nil {
				props = append(props, "'data-krate-scroll':("+val+")===false?'false':undefined")
			}
		default:
			key := attr.Name
			if key == "className" {
				key = "class"
			}
			if attr.Value == nil {
				props = append(props, jsObjectKey(key)+":true")
			} else {
				props = append(props, jsObjectKey(key)+":"+val)
			}
		}
	}
	if external {
		props = append(props, "'data-krate-external':true")
	} else {
		props = append(props, "'data-krate-link':true")
		props = append(props, "'data-prefetch':("+prefetch+")")
		if replace != "" {
			props = append(props, "'data-krate-replace':("+replace+")")
		}
		if scrollFalse {
			props = append(props, "'data-krate-scroll':'false'")
		}
	}

	var b strings.Builder
	b.WriteString("h('a',{")
	b.WriteString(strings.Join(props, ","))
	b.WriteString("}")
	for _, child := range el.Children {
		b.WriteString(",")
		switch c := child.(type) {
		case *ast.JSXText:
			b.WriteString("'")
			b.WriteString(escape.JSString(c.Value))
			b.WriteString("'")
		case *ast.JSXExprContainer:
			b.WriteString(generateExprJS(c.Expression, signals))
		case *ast.JSXElementChild:
			b.WriteString(generateJSXJS(c.Element, signals))
		case *ast.JSXFragmentChild:
			b.WriteString(generateJSXFragmentJS(c.Fragment, signals))
		}
	}
	b.WriteString(")")
	return b.String()
}

// generateJSXFragmentJS converts a JSXFragment to a JS array expression.
func generateJSXFragmentJS(frag *ast.JSXFragment, signals map[string]ast.Expr) string {
	if len(frag.Children) == 0 {
		return "[]"
	}
	var b strings.Builder
	b.WriteString("[")
	for i, child := range frag.Children {
		if i > 0 {
			b.WriteString(",")
		}
		switch c := child.(type) {
		case *ast.JSXText:
			b.WriteString("'")
			b.WriteString(escape.JSString(c.Value))
			b.WriteString("'")
		case *ast.JSXExprContainer:
			b.WriteString(generateExprJS(c.Expression, signals))
		case *ast.JSXElementChild:
			b.WriteString(generateJSXJS(c.Element, signals))
		case *ast.JSXFragmentChild:
			b.WriteString(generateJSXFragmentJS(c.Fragment, signals))
		}
	}
	b.WriteString("]")
	return b.String()
}

// renderVarInitJS renders a VarStmt for use as a for-loop init (no trailing semicolon).
func renderVarInitJS(s *ast.VarStmt, signals map[string]ast.Expr) string {
	keyword := varKeyword(s.Kind)
	var parts []string
	for _, decl := range s.Decls {
		if decl.IsDestructuring {
			pattern := decl.Pattern
			if pattern == "" {
				pattern = "[" + strings.Join(decl.Names, ",") + "]"
			}
			parts = append(parts, keyword+" "+pattern+"="+generateExprJS(decl.Init, signals))
		} else if decl.Name != "" {
			init := ""
			if decl.Init != nil {
				init = "=" + generateExprJS(decl.Init, signals)
			}
			parts = append(parts, keyword+" "+decl.Name+init)
		}
	}
	return strings.Join(parts, ", ")
}

// jsObjectKey renders an object-literal key, quoting it when it is not a
// valid JS identifier (e.g. "data-otp-index").
func jsObjectKey(key string) string {
	if isJSIdentifier(key) {
		return key
	}
	return "'" + escape.JSString(key) + "'"
}

func isJSIdentifier(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		if i == 0 {
			if !(r == '_' || r == '$' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')) {
				return false
			}
		} else {
			if !(r == '_' || r == '$' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')) {
				return false
			}
		}
	}
	return true
}

// codegenIssues validates that every construct reachable from a function body
// is supported by the JS code generator. It guarantees no node is silently
// dropped: an unsupported statement or expression becomes a hard build error
// instead of vanishing from the emitted hydration/SSR code.
func codegenIssues(fn *ast.FnDecl) []error {
	if fn == nil {
		return nil
	}
	var issues []error
	where := fn.Name
	if where == "" {
		where = "<anonymous>"
	}
	report := func(pos ast.Pos, kind string, node interface{}) {
		issues = append(issues, fmt.Errorf(
			"%s:%d:%d: unsupported %s %T in client code (Krate bug — please report)",
			where, pos.Line, pos.Col, kind, node))
	}

	var walkExpr func(ast.Expr)
	var walkStmt func(ast.Stmt)
	var walkJSXChild func(ast.JSXChild)

	walkExpr = func(e ast.Expr) {
		if e == nil {
			return
		}
		switch v := e.(type) {
		case *ast.Identifier, *ast.Literal, *ast.ThisExpr, *ast.ImportMetaExpr:
		case *ast.CallExpr:
			walkExpr(v.Callee)
			for _, a := range v.Args {
				walkExpr(a)
			}
		case *ast.MemberExpr:
			walkExpr(v.Object)
			walkExpr(v.Property)
		case *ast.BinaryExpr:
			walkExpr(v.Left)
			walkExpr(v.Right)
		case *ast.UnaryExpr:
			walkExpr(v.Arg)
		case *ast.ConditionalExpr:
			walkExpr(v.Test)
			walkExpr(v.Consequent)
			walkExpr(v.Alternate)
		case *ast.TypeAssertion:
			walkExpr(v.Expr)
		case *ast.ArrowFn:
			for _, s := range v.Body {
				walkStmt(s)
			}
		case *ast.ObjectExpr:
			for _, p := range v.Properties {
				walkExpr(p.Value)
			}
		case *ast.ArrayExpr:
			for _, el := range v.Elements {
				walkExpr(el)
			}
		case *ast.TemplateExpr:
			for _, p := range v.Parts {
				walkExpr(p)
			}
		case *ast.JSXElement:
			for _, a := range v.Opening.Attributes {
				walkExpr(a.Value)
			}
			for _, c := range v.Children {
				walkJSXChild(c)
			}
		case *ast.JSXFragment:
			for _, c := range v.Children {
				walkJSXChild(c)
			}
		case *ast.NewExpr:
			walkExpr(v.Callee)
			for _, a := range v.Args {
				walkExpr(a)
			}
		case *ast.AwaitExpr:
			walkExpr(v.Arg)
		case *ast.DynamicImport:
			walkExpr(v.Arg)
		default:
			report(e.Pos(), "expression", e)
		}
	}

	walkJSXChild = func(c ast.JSXChild) {
		switch v := c.(type) {
		case *ast.JSXText:
		case *ast.JSXExprContainer:
			walkExpr(v.Expression)
		case *ast.JSXElementChild:
			walkExpr(v.Element)
		case *ast.JSXFragmentChild:
			walkExpr(v.Fragment)
		}
	}

	walkStmt = func(s ast.Stmt) {
		switch v := s.(type) {
		case *ast.ReturnStmt:
			walkExpr(v.Value)
		case *ast.ExprStmt:
			walkExpr(v.Expression)
		case *ast.VarStmt:
			for _, d := range v.Decls {
				walkExpr(d.Init)
			}
		case *ast.IfStmt:
			walkExpr(v.Test)
			for _, c := range v.Consequent {
				walkStmt(c)
			}
			for _, a := range v.Alternate {
				walkStmt(a)
			}
		case *ast.BlockStmt:
			for _, c := range v.Body {
				walkStmt(c)
			}
		case *ast.FnDecl:
			for _, c := range v.Body {
				walkStmt(c)
			}
		case *ast.ForStmt:
			walkStmt(v.Init)
			walkExpr(v.Test)
			walkExpr(v.Update)
			for _, c := range v.Body {
				walkStmt(c)
			}
		case *ast.ForInStmt:
			walkExpr(v.Left)
			walkExpr(v.Right)
			for _, c := range v.Body {
				walkStmt(c)
			}
		case *ast.WhileStmt:
			walkExpr(v.Test)
			for _, c := range v.Body {
				walkStmt(c)
			}
		case *ast.DoWhileStmt:
			walkExpr(v.Test)
			for _, c := range v.Body {
				walkStmt(c)
			}
		case *ast.SwitchStmt:
			walkExpr(v.Discriminant)
			for _, c := range v.Cases {
				walkExpr(c.Test)
				for _, b := range c.Body {
					walkStmt(b)
				}
			}
		case *ast.TryStmt:
			for _, c := range v.Body {
				walkStmt(c)
			}
			if v.Catch != nil {
				for _, c := range v.Catch.Body {
					walkStmt(c)
				}
			}
			for _, c := range v.Finally {
				walkStmt(c)
			}
		case *ast.ThrowStmt:
			walkExpr(v.Value)
		case *ast.BreakStmt, *ast.ContinueStmt:
		default:
			report(s.Pos(), "statement", s)
		}
	}

	for _, s := range fn.Body {
		walkStmt(s)
	}
	return issues
}

// String literal values are decoded once at parse time (see
// parser.decodeStringToken -> escape.UnescapeJSString), so consumers here read
// the literal runtime value directly.
