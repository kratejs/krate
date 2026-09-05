package irtree

import (
	"strings"

	"krate-compiler/internal/ast"
	"krate-compiler/internal/escape"
)

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
		var argsJS []string
		for _, arg := range e.Args {
			argsJS = append(argsJS, generateExprJS(arg, signals))
		}
		return calleeJS + "(" + strings.Join(argsJS, ", ") + ")"
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
		if e.Op == "&&" {
			return "(" + left + "?" + right + ":'')"
		}
		return "(" + left + " " + e.Op + " " + right + ")"
	case *ast.UnaryExpr:
		arg := generateExprJS(e.Arg, signals)
		if e.Postfix {
			return arg + e.Op
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
		return ""
	}
}

// GenerateExprJS renders an AST expression to JavaScript source. Exported so
// the renderer's SSR evaluator can hand expressions to the embedded QuickJS
// engine for evaluation (real JS built-ins: Date, Math, String, Number, ...).
func GenerateExprJS(expr ast.Expr, signals map[string]ast.Expr) string {
	return generateExprJS(expr, signals)
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
	for i, p := range fn.Params {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(p.Name)
	}
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
		for _, stmt := range fn.Body {
			b.WriteString(renderStmtJS(stmt, signals))
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

// renderStmtJS renders a statement to JS source.
func renderStmtJS(stmt ast.Stmt, signals map[string]ast.Expr) string {
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
		for _, decl := range s.Decls {
			keyword := "var"
			switch s.Kind {
			case ast.VarConst:
				keyword = "const"
			case ast.VarLet:
				keyword = "let"
			}
			if decl.IsDestructuring {
				parts = append(parts, keyword+" ["+strings.Join(decl.Names, ",")+"]="+generateExprJS(decl.Init, signals))
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
		var consequentJS strings.Builder
		for _, cs := range s.Consequent {
			consequentJS.WriteString(renderStmtJS(cs, signals))
		}
		result := "if(" + test + "){" + consequentJS.String() + "}"
		if len(s.Alternate) > 0 {
			var alternateJS strings.Builder
			for _, as := range s.Alternate {
				alternateJS.WriteString(renderStmtJS(as, signals))
			}
			result += "else{" + alternateJS.String() + "}"
		}
		return result
	case *ast.BlockStmt:
		var b strings.Builder
		b.WriteByte('{')
		for _, inner := range s.Body {
			b.WriteString(renderStmtJS(inner, signals))
		}
		b.WriteByte('}')
		return b.String()
	case *ast.FnDecl:
		var b strings.Builder
		b.WriteString("function ")
		b.WriteString(s.Name)
		b.WriteString("(")
		for i, p := range s.Params {
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString(p.Name)
		}
		b.WriteString("){")
		for _, stmt := range s.Body {
			b.WriteString(renderStmtJS(stmt, signals))
		}
		b.WriteString("}")
		return b.String()
	case *ast.ForStmt:
		var b strings.Builder
		b.WriteString("for(")
		if s.Init != nil {
			if vs, ok := s.Init.(*ast.VarStmt); ok {
				b.WriteString(renderVarInitJS(vs, signals))
			} else {
				init := renderStmtJS(s.Init, signals)
				init = strings.TrimSuffix(init, ";")
				b.WriteString(init)
			}
		}
		b.WriteString(";")
		if s.Test != nil {
			b.WriteString(generateExprJS(s.Test, signals))
		}
		b.WriteString(";")
		if s.Update != nil {
			b.WriteString(generateExprJS(s.Update, signals))
		}
		b.WriteString("){")
		for _, stmt := range s.Body {
			b.WriteString(renderStmtJS(stmt, signals))
		}
		b.WriteString("}")
		return b.String()
	case *ast.WhileStmt:
		var b strings.Builder
		b.WriteString("while(")
		b.WriteString(generateExprJS(s.Test, signals))
		b.WriteString("){")
		for _, stmt := range s.Body {
			b.WriteString(renderStmtJS(stmt, signals))
		}
		b.WriteString("}")
		return b.String()
	case *ast.DoWhileStmt:
		var b strings.Builder
		b.WriteString("do{")
		for _, stmt := range s.Body {
			b.WriteString(renderStmtJS(stmt, signals))
		}
		b.WriteString("}while(")
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
		b.WriteString("switch(")
		b.WriteString(generateExprJS(s.Discriminant, signals))
		b.WriteString("){")
		for _, c := range s.Cases {
			if c.Test != nil {
				b.WriteString("case ")
				b.WriteString(generateExprJS(c.Test, signals))
				b.WriteString(":")
			} else {
				b.WriteString("default:")
			}
			for _, stmt := range c.Body {
				b.WriteString(renderStmtJS(stmt, signals))
			}
		}
		b.WriteString("}")
		return b.String()
	default:
		return ""
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
		valJS := generateExprJS(prop.Value, signals)
		if prop.Shorthand {
			parts = append(parts, prop.Key)
		} else {
			parts = append(parts, jsObjectKey(prop.Key)+":"+valJS)
		}
	}
	return "{" + strings.Join(parts, ",") + "}"
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
	// Uppercase tag names are components — reference them as function refs so
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
	var parts []string
	for _, decl := range s.Decls {
		keyword := "var"
		switch s.Kind {
		case ast.VarConst:
			keyword = "const"
		case ast.VarLet:
			keyword = "let"
		}
		if decl.IsDestructuring {
			parts = append(parts, keyword+" ["+strings.Join(decl.Names, ",")+"]="+generateExprJS(decl.Init, signals))
		} else if decl.Name != "" {
			init := ""
			if decl.Init != nil {
				init = "=" + generateExprJS(decl.Init, signals)
			}
			parts = append(parts, keyword+" "+decl.Name+init)
		}
	}
	return strings.Join(parts, ",")
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

// unescapeStringValue decodes JSON-style \uXXXX escape sequences in a string
// literal value (the lexer preserves them raw). The docs plugin JSON-encodes
// page titles/sidebar data, so a title like "Markdown \u0026 MDX" must render
// as "Markdown & MDX" rather than the literal escape sequence.
// unescapeStringValue decodes the escape sequences that can appear inside a
// JS/TS string literal token value into their literal characters. The lexer
// stores the raw source text (backslash escapes intact), so a `"double \"quote\""`
// token carries `double \"quote\"`; this turns it into `double "quote"`.
func unescapeStringValue(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != '\\' || i+1 >= len(s) {
			b.WriteByte(s[i])
			continue
		}
		c := s[i+1]
		switch c {
		case 'n':
			b.WriteByte('\n')
			i++
		case 't':
			b.WriteByte('\t')
			i++
		case 'r':
			b.WriteByte('\r')
			i++
		case 'b':
			b.WriteByte('\b')
			i++
		case 'f':
			b.WriteByte('\f')
			i++
		case 'v':
			b.WriteByte('\v')
			i++
		case '0':
			b.WriteByte(0)
			i++
		case '\\':
			b.WriteByte('\\')
			i++
		case '\'':
			b.WriteByte('\'')
			i++
		case '"':
			b.WriteByte('"')
			i++
		case '`':
			b.WriteByte('`')
			i++
		case 'u':
			if i+5 < len(s) {
				if r, ok := decodeHex4(s[i+2 : i+6]); ok {
					b.WriteRune(r)
					i += 5
					continue
				}
			}
			b.WriteByte('\\')
		default:
			b.WriteByte('\\')
		}
	}
	return b.String()
}

// decodeHex4 decodes a 4-hex-digit \uXXXX sequence to a rune.
func decodeHex4(hex string) (rune, bool) {
	if len(hex) != 4 {
		return 0, false
	}
	var v int
	for _, c := range hex {
		v <<= 4
		switch {
		case c >= '0' && c <= '9':
			v += int(c - '0')
		case c >= 'a' && c <= 'f':
			v += int(c-'a') + 10
		case c >= 'A' && c <= 'F':
			v += int(c-'A') + 10
		default:
			return 0, false
		}
	}
	return rune(v), true
}
