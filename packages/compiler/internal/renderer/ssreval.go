package renderer

import (
	"strconv"
	"strings"

	"krate-compiler/internal/ast"
	"krate-compiler/internal/escape"
	"krate-compiler/internal/irtree"
)

// SSREval is a lightweight SSR evaluator that produces HTML output only.
// It has NO signal tracking, NO handler collection, NO path tracking.
// It replaces the old evalExpr/renderExpr triple-return monster.
type SSREval struct {
	bindings  map[string]string
	arrays    map[string][]string
	functions map[string]*ast.FnDecl
	depth     int

	// Meta content captured from <Head>/<Script>/<Style> elements encountered
	// during evaluation. Signal-less components that go through the SSREval
	// path (e.g. a docs layout wrapper) still inject their <Head> stylesheet
	// links and <Script> tags via these fields; the emitter routes them into
	// the page's HeadHTML/ScriptHTML/StyleHTML.
	HeadHTML   string
	ScriptHTML string
	StyleHTML  string

	// interactiveEmit is an optional hook set by the emitter. When an
	// uppercase component JSX element is encountered during evaluation, the
	// hook is given a chance to render it through the tree emit path (which
	// preserves data-k/data-kh hydration markers and emits a component
	// signature) instead of as flat static HTML. Return handled=true when the
	// component was emitted through the tree path.
	interactiveEmit func(el *ast.JSXElement) (html string, handled bool)

	// iconEmit is an optional hook set by the emitter to resolve a <Icon>
	// element whose `name` attribute is an expression (e.g. `name={icon}`
	// where `icon` is a component-local variable). The hook receives the
	// evaluated name and the element's forwarded attributes, and returns the
	// compiled SVG HTML. Return handled=false when the name could not be
	// resolved so the default "unknown component" path applies.
	iconEmit func(name string, attrs []*ast.JSXAttr) (html string, handled bool)

	// childrenIsHTML is set when the "children" binding has already been
	// rendered to (escaped) HTML by the emitter's slot pipeline. A `{children}`
	// container must then inject it raw instead of escaping again.
	childrenIsHTML bool

	// evalJS is an optional hook (wired by the build to the embedded QuickJS
	// engine) that evaluates a self-contained JS expression with full built-ins
	// (Date, Math, String, Number, ...). When set, calls to global built-ins the
	// Go evaluator can't handle statically (e.g. Date.now()) are delegated to
	// it, producing a genuine JS-engine value at SSR/compile time.
	evalJS func(code string) (string, error)
}

// SetEvalJS installs the expression-evaluation hook backed by the embedded
// QuickJS runtime. See the evalJS field docs.
func (e *SSREval) SetEvalJS(fn func(code string) (string, error)) {
	e.evalJS = fn
}

const maxEvalDepth = 10

// NewSSREval creates a new SSR evaluator.
func NewSSREval(functions map[string]*ast.FnDecl) *SSREval {
	return &SSREval{
		bindings:  make(map[string]string),
		arrays:    make(map[string][]string),
		functions: functions,
	}
}

// SetBindings sets the variable bindings for evaluation.
func (e *SSREval) SetBindings(bindings map[string]string) {
	e.bindings = bindings
}

// BindLocalVars evaluates top-level const/let/var declarations in a function
// body and binds them so return-statement evaluation resolves locals like
// `var className = "..." + side`. Must run before Eval on the return value.
// Also evaluates for-loops that build arrays via `.push(<JSX/>)` so
// components like OTPField render their statically-built element lists.
func (e *SSREval) BindLocalVars(body []ast.Stmt) {
	for _, stmt := range body {
		switch s := stmt.(type) {
		case *ast.VarStmt:
			for _, decl := range s.Decls {
				if decl.Name != "" && decl.Init != nil {
					if arr, ok := decl.Init.(*ast.ArrayExpr); ok && hasJSX(arr) {
						var elems []string
						for _, el := range arr.Elements {
							elems = append(elems, e.eval(el))
						}
						e.arrays[decl.Name] = elems
						continue
					}
					e.bindings[decl.Name] = e.eval(decl.Init)
				}
			}
		case *ast.ForStmt:
			e.evalForLoop(s)
		case *ast.ExportStmt:
			if vs, ok := s.Declaration.(*ast.VarStmt); ok {
				for _, decl := range vs.Decls {
					if decl.Name != "" && decl.Init != nil {
						if arr, ok := decl.Init.(*ast.ArrayExpr); ok && hasJSX(arr) {
							var elems []string
							for _, el := range arr.Elements {
								elems = append(elems, e.eval(el))
							}
							e.arrays[decl.Name] = elems
							continue
						}
						e.bindings[decl.Name] = e.eval(decl.Init)
					}
				}
			}
		}
	}
}

// evalForLoop statically evaluates a `for` loop whose body pushes JSX onto an
// array binding (e.g. `var inputs = []; for (var i = 0; i < n; i++) { inputs.push(<input/>) }`).
func (e *SSREval) evalForLoop(stmt *ast.ForStmt) {
	if vs, ok := stmt.Init.(*ast.VarStmt); ok {
		for _, decl := range vs.Decls {
			if decl.Name != "" {
				if decl.Init != nil {
					e.bindings[decl.Name] = e.eval(decl.Init)
				} else {
					e.bindings[decl.Name] = ""
				}
			}
		}
	}
	for iter := 0; iter < 1000; iter++ {
		test := e.eval(stmt.Test)
		if !isSSRTruthy(test) {
			return
		}
		for _, s := range stmt.Body {
			if es, ok := s.(*ast.ExprStmt); ok {
				if call, ok := es.Expression.(*ast.CallExpr); ok {
					if mem, ok := call.Callee.(*ast.MemberExpr); ok {
						if objID, ok := mem.Object.(*ast.Identifier); ok {
							if propID, ok := mem.Property.(*ast.Identifier); ok && propID.Name == "push" && len(call.Args) == 1 {
								if _, isArr := e.arrays[objID.Name]; isArr {
									e.arrays[objID.Name] = append(e.arrays[objID.Name], e.eval(call.Args[0]))
								}
							}
						}
					}
				}
			}
		}
		if upd, ok := stmt.Update.(*ast.UnaryExpr); ok && (upd.Op == "++" || upd.Op == "--") {
			if id, ok := upd.Arg.(*ast.Identifier); ok {
				cur := toFloat(e.bindings[id.Name])
				if upd.Op == "++" {
					cur++
				} else {
					cur--
				}
				e.bindings[id.Name] = trimFloatStr(cur)
			}
		}
	}
}

func isSSRTruthy(v string) bool {
	return v != "" && v != "false" && v != "null" && v != "undefined" && v != "0"
}

func trimFloatStr(f float64) string {
	if f == float64(int64(f)) {
		return itoa(int(f))
	}
	return strconv.FormatFloat(f, 'f', -1, 64)
}

// Eval evaluates an expression and returns its HTML string value.
func (e *SSREval) Eval(expr ast.Expr) string {
	if expr == nil {
		return ""
	}
	switch ex := expr.(type) {
	case *ast.Literal:
		if ex.Kind == ast.NullLit {
			return ""
		}
		return stringLiteralValue(ex)
	case *ast.Identifier:
		if v, ok := e.bindings[ex.Name]; ok {
			return v
		}
		if arr, ok := e.arrays[ex.Name]; ok {
			return strings.Join(arr, "")
		}
		return ""
	case *ast.BinaryExpr:
		return e.evalBinaryExpr(ex)
	case *ast.UnaryExpr:
		return e.evalUnaryExpr(ex)
	case *ast.ConditionalExpr:
		return e.evalConditional(ex)
	case *ast.MemberExpr:
		return e.evalMemberExpr(ex)
	case *ast.CallExpr:
		return e.evalCallExpr(ex)
	case *ast.TemplateExpr:
		return e.evalTemplateExpr(ex)
	case *ast.JSXElement:
		return e.evalJSX(ex)
	case *ast.JSXFragment:
		return e.evalFragment(ex)
	case *ast.TypeAssertion:
		return e.eval(ex.Expr)
	case *ast.ArrowFn:
		body := arrowBodyExpr(ex)
		if body != nil {
			return e.eval(body)
		}
		return ""
	case *ast.ArrayExpr:
		return e.evalArrayExpr(ex)
	case *ast.ObjectExpr:
		return e.evalObjectExpr(ex)
	case *ast.NewExpr:
		// `new Date(...)` etc. — delegated to QuickJS so real constructors run.
		if root := globalRoot(ex.Callee); globalBuiltins[root] {
			return e.delegateJS(ex)
		}
		return ""
	default:
		return ""
	}
}

func (e *SSREval) eval(expr ast.Expr) string {
	return e.Eval(expr)
}

// ─── Binary ────────────────────────────────────────────────────────────────

func (e *SSREval) evalBinaryExpr(expr *ast.BinaryExpr) string {
	left := e.eval(expr.Left)
	right := e.eval(expr.Right)

	switch expr.Op {
	case "+":
		if isNumericStr(left) && isNumericStr(right) {
			return trimFloatStr(toFloat(left) + toFloat(right))
		}
		return left + right
	case "-":
		if isNumericStr(left) && isNumericStr(right) {
			return trimFloatStr(toFloat(left) - toFloat(right))
		}
		return left + right // simplified: just concat for SSR
	case "*":
		if isNumericStr(left) && isNumericStr(right) {
			return trimFloatStr(toFloat(left) * toFloat(right))
		}
		return left + right
	case "/":
		if isNumericStr(left) && isNumericStr(right) {
			return trimFloatStr(toFloat(left) / toFloat(right))
		}
		return left + right
	case "%":
		if isNumericStr(left) && isNumericStr(right) {
			return trimFloatStr(float64(int(toFloat(left)) % int(toFloat(right))))
		}
		return left + right
	case "<", ">", "<=", ">=":
		if isNumericStr(left) && isNumericStr(right) {
			l, r := toFloat(left), toFloat(right)
			switch expr.Op {
			case "<":
				return boolStr(l < r)
			case ">":
				return boolStr(l > r)
			case "<=":
				return boolStr(l <= r)
			case ">=":
				return boolStr(l >= r)
			}
		}
		return left + " " + expr.Op + " " + right
	case "==", "===":
		if isNumericStr(left) && isNumericStr(right) {
			return boolStr(toFloat(left) == toFloat(right))
		}
		if left == right {
			return "true"
		}
		return "false"
	case "!=", "!==":
		if isNumericStr(left) && isNumericStr(right) {
			return boolStr(toFloat(left) != toFloat(right))
		}
		if left != right {
			return "true"
		}
		return "false"
	case "&&":
		if isSSRTruthy(left) {
			return right
		}
		// Short-circuit: a falsy left operand produces no output (false && x
		// renders nothing, not the string "false"). This matches how the
		// renderer handles conditional children in JSX.
		return ""
	case "||":
		if isSSRTruthy(left) {
			return left
		}
		return right
	case "??":
		if left != "" && left != "null" && left != "undefined" {
			return left
		}
		return right
	default:
		return left + " " + expr.Op + " " + right
	}
}

func isNumericStr(s string) bool {
	if s == "" {
		return false
	}
	_, err := strconv.ParseFloat(s, 64)
	return err == nil
}

func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// ─── Unary ─────────────────────────────────────────────────────────────────

func (e *SSREval) evalUnaryExpr(expr *ast.UnaryExpr) string {
	if expr.Op == "!" {
		val := e.eval(expr.Arg)
		if val == "" || val == "false" || val == "null" || val == "undefined" || val == "0" {
			return "true"
		}
		return "false"
	}
	if expr.Op == "typeof" {
		return "string"
	}
	return e.eval(expr.Arg)
}

// ─── Conditional ───────────────────────────────────────────────────────────

func (e *SSREval) evalConditional(expr *ast.ConditionalExpr) string {
	test := e.eval(expr.Test)
	if test != "" && test != "false" && test != "null" && test != "undefined" && test != "0" {
		return e.eval(expr.Consequent)
	}
	return e.eval(expr.Alternate)
}

// ─── Member ────────────────────────────────────────────────────────────────

func (e *SSREval) evalMemberExpr(expr *ast.MemberExpr) string {
	prop := ""
	if id, ok := expr.Property.(*ast.Identifier); ok {
		prop = id.Name
	}

	// Direct binding lookup: props.breadcrumbs → bindings["breadcrumbs"]
	// Only applies when the object is `props` — a bare `item.url` must NOT
	// resolve from a top-level "url" binding (which could be a leftover prop
	// from another component) but from the `item` object binding instead.
	if prop != "" {
		if id, ok := expr.Object.(*ast.Identifier); ok && id.Name == "props" {
			if v, ok := e.bindings[prop]; ok {
				return v
			}
		}
	}

	// Identifier-based lookups
	if id, ok := expr.Object.(*ast.Identifier); ok {
		// JSON.stringify
		if id.Name == "JSON" && prop == "stringify" {
			return e.eval(expr.Property)
		}
		// Try to extract property from binding value
		if v, found := e.bindings[id.Name]; found {
			if prop == "length" && v != "" {
				// Array length: count \x1f-separated items
				parts := strings.Split(v, "\x1f")
				return itoa(len(parts))
			}
			if prop != "" {
				if val := extractJSONProp(v, prop); val != "" {
					return val
				}
				// Property is missing from the object — return "" so ternary
				// tests like `item.indexURL ? ... : ...` correctly pick the
				// alternate branch instead of treating the whole object as the
				// property value.
				if strings.HasPrefix(v, "{") {
					return ""
				}
				return v
			}
			return v
		}
	}

	obj := e.eval(expr.Object)
	if obj == "" {
		return ""
	}
	// .length on evaluated value
	if prop == "length" && obj != "" {
		parts := strings.Split(obj, "\x1f")
		return itoa(len(parts))
	}
	// Try to extract property from evaluated object string
	if prop != "" {
		if val := extractJSONProp(obj, prop); val != "" {
			return val
		}
	}
	return ""
}

// extractJSONProp is defined in eval.go and shared across the package.

// ─── Call ──────────────────────────────────────────────────────────────────

func (e *SSREval) evalCallExpr(expr *ast.CallExpr) string {
	// JSON.stringify
	if mem, ok := expr.Callee.(*ast.MemberExpr); ok {
		if id, ok := mem.Object.(*ast.Identifier); ok && id.Name == "JSON" && len(expr.Args) == 1 {
			if prop, ok := mem.Property.(*ast.Identifier); ok && prop.Name == "stringify" {
				return e.eval(expr.Args[0])
			}
		}
		// .map()
		if prop, ok := mem.Property.(*ast.Identifier); ok && prop.Name == "map" && len(expr.Args) == 1 {
			return e.evalArrayMap(mem.Object, expr.Args[0])
		}
		// .join()
		if prop, ok := mem.Property.(*ast.Identifier); ok && prop.Name == "join" {
			arr := e.eval(mem.Object)
			sep := ", "
			if len(expr.Args) == 1 {
				sep = e.eval(expr.Args[0])
			}
			return strings.ReplaceAll(arr, "\x1f", sep)
		}
		// .filter()
		if prop, ok := mem.Property.(*ast.Identifier); ok && prop.Name == "filter" && len(expr.Args) == 1 {
			return e.eval(mem.Object) // simplified: return unfiltered
		}
		// .length
		if prop, ok := mem.Property.(*ast.Identifier); ok && prop.Name == "length" {
			arr := e.eval(mem.Object)
			return itoa(len(strings.Split(arr, "\x1f")))
		}
		// .toString()
		if prop, ok := mem.Property.(*ast.Identifier); ok && prop.Name == "toString" {
			return e.eval(mem.Object)
		}
		// .toUpperCase()
		if prop, ok := mem.Property.(*ast.Identifier); ok && prop.Name == "toUpperCase" {
			return strings.ToUpper(e.eval(mem.Object))
		}
		// .toLowerCase()
		if prop, ok := mem.Property.(*ast.Identifier); ok && prop.Name == "toLowerCase" {
			return strings.ToLower(e.eval(mem.Object))
		}
	}
	// IIFE
	if arrow, ok := expr.Callee.(*ast.ArrowFn); ok {
		body := arrowBodyExpr(arrow)
		if body != nil {
			return e.eval(body)
		}
	}
	// Calls to global built-ins (Date.now(), Math.round(), String(x), ...) are
	// delegated to the embedded QuickJS engine, which has the real built-ins.
	if root := globalRoot(expr.Callee); globalBuiltins[root] {
		return e.delegateJS(expr)
	}
	return ""
}

// ─── Template ──────────────────────────────────────────────────────────────

func (e *SSREval) evalTemplateExpr(expr *ast.TemplateExpr) string {
	var b strings.Builder
	for i, raw := range expr.Raw {
		b.WriteString(raw)
		if i < len(expr.Parts) {
			b.WriteString(e.eval(expr.Parts[i]))
		}
	}
	return b.String()
}

// ─── JSX ───────────────────────────────────────────────────────────────────

// resolveIconName evaluates the `name` attribute of an <Icon> element through
// the eval bindings (props + local vars). Returns "" when the name is a
// literal that is empty, or when the expression cannot be resolved.
func (e *SSREval) resolveIconName(el *ast.JSXElement) string {
	for _, attr := range el.Opening.Attributes {
		if attr.Spread || attr.Name != "name" {
			continue
		}
		if lit, ok := attr.Value.(*ast.Literal); ok {
			return lit.Value
		}
		return e.eval(attr.Value)
	}
	return ""
}

func (e *SSREval) evalJSX(el *ast.JSXElement) string {
	name := el.Opening.Name

	// Special components: capture their content into meta fields so signal-less
	// wrappers (layouts, doc shells) still inject <Head>/<Script>/<Style>.
	switch name {
	case "Head", "head":
		e.HeadHTML += e.evalChildren(el.Children)
		return ""
	case "Script", "script":
		e.ScriptHTML += e.renderMetaElement(el, "script")
		return ""
	case "Style", "style":
		e.StyleHTML += e.renderMetaElement(el, "style")
		return ""
	}

	// <Icon> with a dynamic `name` attribute: resolve the expression through
	// the eval bindings (component props + local vars) and let the emitter's
	// iconEmit hook compile it to the SVG markup. This covers components like
	// LinkCard that render <Icon name={icon}/> where icon = props.icon || "".
	if name == "Icon" && e.iconEmit != nil {
		if iconName := e.resolveIconName(el); iconName != "" {
			if html, handled := e.iconEmit(iconName, el.Opening.Attributes); handled {
				return html
			}
		}
	}

	// Uppercase = component — resolve and evaluate recursively
	if len(name) > 0 && name[0] >= 'A' && name[0] <= 'Z' {
		if e.interactiveEmit != nil {
			if html, handled := e.interactiveEmit(el); handled {
				return html
			}
		}
		if e.functions != nil {
			if fn := e.functions[name]; fn != nil {
				return e.evalComponentFn(fn, el)
			}
		}
		// Unknown component — skip
		return ""
	}

	var b strings.Builder
	b.WriteByte('<')
	b.WriteString(name)

	// dangerouslySetInnerHTML={{__html: "..."}} injects raw, pre-rendered HTML
	// (e.g. build-time markdown). It is never emitted as an attribute.
	var rawInnerHTML string
	hasRawInnerHTML := false
	for _, attr := range el.Opening.Attributes {
		if attr.Spread || attr.Name != "dangerouslySetInnerHTML" || attr.Value == nil {
			continue
		}
		if raw, ok := e.evalInnerHTMLValue(attr.Value); ok {
			rawInnerHTML = raw
			hasRawInnerHTML = true
		}
	}

	for _, attr := range el.Opening.Attributes {
		if attr.Spread || attr.Name == "dangerouslySetInnerHTML" {
			continue
		}
		val := ""
		if attr.Value != nil {
			val = e.eval(attr.Value)
		} else {
			// Bare attribute like <input disabled /> — boolean true
			val = "true"
		}
		// Boolean attributes must not be emitted with empty/"false" values:
		// their mere presence (even ="") makes them truthy in HTML.
		if isBooleanAttr(attr.Name) {
			if val == "" || val == "false" || val == "null" || val == "undefined" || val == "0" {
				continue
			}
			b.WriteByte(' ')
			b.WriteString(ast.HTMLAttrName(attr.Name))
			if val != "true" {
				b.WriteString(`="`)
				b.WriteString(escape.HTML(val))
				b.WriteByte('"')
			}
			continue
		}
		b.WriteByte(' ')
		b.WriteString(ast.HTMLAttrName(attr.Name))
		if attr.Value != nil {
			b.WriteString(`="`)
			b.WriteString(escape.HTML(val))
			b.WriteByte('"')
		}
	}

	if el.Opening.SelfClosing && !hasRawInnerHTML {
		if isVoidElement(el.Opening.Name) {
			b.WriteString(" />")
		} else {
			b.WriteString("></")
			b.WriteString(el.Opening.Name)
			b.WriteByte('>')
		}
		return b.String()
	}

	b.WriteByte('>')
	if hasRawInnerHTML {
		// The dangerouslySetInnerHTML value is pre-rendered markup — inject
		// it verbatim, ignoring children.
		b.WriteString(rawInnerHTML)
	} else {
		for _, child := range el.Children {
			switch c := child.(type) {
			case *ast.JSXText:
				b.WriteString(c.Value)
			case *ast.JSXExprContainer:
				b.WriteString(e.escapeContainerValue(c.Expression))
			case *ast.JSXElementChild:
				b.WriteString(e.evalJSX(c.Element))
			case *ast.JSXFragmentChild:
				b.WriteString(e.evalFragment(c.Fragment))
			}
		}
	}
	b.WriteString("</")
	b.WriteString(name)
	b.WriteByte('>')
	return b.String()
}

// renderMetaElement renders a <Script>/<Style> element as a complete tag
// (opening tag with attributes + raw children + closing tag) for capture into
// the page's ScriptHTML/StyleHTML. The children are written raw so inline JS /
// CSS is never HTML-escaped.
func (e *SSREval) renderMetaElement(el *ast.JSXElement, tag string) string {
	var b strings.Builder
	b.WriteByte('<')
	b.WriteString(tag)
	for _, attr := range el.Opening.Attributes {
		if attr.Spread || attr.Value == nil || attr.Name == "dangerouslySetInnerHTML" {
			continue
		}
		val := e.eval(attr.Value)
		if isBooleanAttr(attr.Name) {
			if val == "" || val == "false" || val == "null" || val == "undefined" || val == "0" {
				continue
			}
			b.WriteByte(' ')
			b.WriteString(ast.HTMLAttrName(attr.Name))
			if val != "true" {
				b.WriteString(`="`)
				b.WriteString(escape.HTML(val))
				b.WriteByte('"')
			}
			continue
		}
		b.WriteByte(' ')
		b.WriteString(ast.HTMLAttrName(attr.Name))
		b.WriteString(`="`)
		b.WriteString(escape.HTML(val))
		b.WriteByte('"')
	}
	b.WriteByte('>')
	b.WriteString(e.evalChildren(el.Children))
	b.WriteString("</")
	b.WriteString(tag)
	b.WriteByte('>')
	return b.String()
}

// evalInnerHTMLValue extracts the __html string from a
// dangerouslySetInnerHTML={{__html: expr}} attribute value. Returns ok=false
// when the value cannot be statically resolved.
func (e *SSREval) evalInnerHTMLValue(expr ast.Expr) (string, bool) {
	obj, ok := expr.(*ast.ObjectExpr)
	if !ok {
		return "", false
	}
	for _, prop := range obj.Properties {
		if prop.Spread || prop.Key != "__html" || prop.Value == nil {
			continue
		}
		return e.eval(prop.Value), true
	}
	return "", false
}

// evalChildren evaluates JSX children to a string (used for Head/Script/Style
// content capture in signal-less components).
func (e *SSREval) evalChildren(children []ast.JSXChild) string {
	var b strings.Builder
	for _, child := range children {
		switch c := child.(type) {
		case *ast.JSXText:
			b.WriteString(c.Value)
		case *ast.JSXExprContainer:
			b.WriteString(stripArraySep(e.eval(c.Expression)))
		case *ast.JSXElementChild:
			b.WriteString(e.evalJSX(c.Element))
		case *ast.JSXFragmentChild:
			b.WriteString(e.evalFragment(c.Fragment))
		}
	}
	return b.String()
}

// evalComponentFn evaluates a component function with props extracted from JSX attributes.
func (e *SSREval) evalComponentFn(fn *ast.FnDecl, el *ast.JSXElement) string {
	e.depth++
	defer func() { e.depth-- }()
	if e.depth > maxEvalDepth {
		return ""
	}
	// Extract prop bindings from JSX attributes
	savedBindings := make(map[string]string)
	for k, v := range e.bindings {
		savedBindings[k] = v
	}

	// Map function params to attribute values
	if len(fn.Params) == 1 && fn.Params[0].Name == "props" {
		// Single props object: set each attr as a binding
		for _, attr := range el.Opening.Attributes {
			if attr.Spread || attr.Value == nil {
				continue
			}
			val := e.eval(attr.Value)
			e.bindings[attr.Name] = val
		}
	} else {
		// Destructured params
		for _, attr := range el.Opening.Attributes {
			if attr.Spread || attr.Value == nil {
				continue
			}
			for _, param := range fn.Params {
				if param.Name == "{...}" {
					// Destructured param — map attr name directly
					val := e.eval(attr.Value)
					e.bindings[attr.Name] = val
				} else if param.Name == attr.Name {
					e.bindings[param.Name] = e.eval(attr.Value)
				}
			}
		}
	}

	// Also pass {children} content from the JSX call site
	var childrenContent strings.Builder
	for _, child := range el.Children {
		switch c := child.(type) {
		case *ast.JSXText:
			childrenContent.WriteString(c.Value)
		case *ast.JSXExprContainer:
			childrenContent.WriteString(e.escapeContainerValue(c.Expression))
		case *ast.JSXElementChild:
			childrenContent.WriteString(e.evalJSX(c.Element))
		case *ast.JSXFragmentChild:
			childrenContent.WriteString(e.evalFragment(c.Fragment))
		}
	}
	if childrenContent.Len() > 0 {
		e.bindings["children"] = childrenContent.String()
	}
	// childrenContent was built through escapeContainerValue (text escaped,
	// elements kept raw), so a `{children}` container in the component's own
	// return JSX must inject it raw rather than escaping it a second time.
	preRenderedChildren := childrenContent.Len() > 0

	// Save/restore childrenIsHTML across this component call so a nested
	// component with its own pre-rendered children doesn't leak the flag.
	savedChildrenIsHTML := e.childrenIsHTML
	if preRenderedChildren {
		e.childrenIsHTML = true
	}
	defer func() { e.childrenIsHTML = savedChildrenIsHTML }()

	e.BindLocalVars(fn.Body)

	ret := findReturnStmtIn(fn.Body)
	if ret == nil || ret.Value == nil {
		e.bindings = savedBindings
		return ""
	}

	// Handle if-return patterns: if (!hasPrev && !hasNext) return <span />;
	for _, stmt := range fn.Body {
		if ifStmt, ok := stmt.(*ast.IfStmt); ok {
			condVal := e.eval(ifStmt.Test)
			if condVal != "" && condVal != "false" && condVal != "null" && condVal != "undefined" && condVal != "0" {
				// Condition is truthy → execute consequent (the early return)
				for _, consequent := range ifStmt.Consequent {
					if retStmt, ok := consequent.(*ast.ReturnStmt); ok && retStmt.Value != nil {
						result := e.eval(retStmt.Value)
						e.bindings = savedBindings
						return result
					}
				}
			}
			// Condition is falsy → skip consequent, fall through to main return
		}
	}

	result := e.eval(ret.Value)

	e.bindings = savedBindings
	return result
}

func (e *SSREval) evalFragment(frag *ast.JSXFragment) string {
	var b strings.Builder
	for _, child := range frag.Children {
		switch c := child.(type) {
		case *ast.JSXText:
			b.WriteString(c.Value)
		case *ast.JSXExprContainer:
			b.WriteString(e.escapeContainerValue(c.Expression))
		case *ast.JSXElementChild:
			b.WriteString(e.evalJSX(c.Element))
		case *ast.JSXFragmentChild:
			b.WriteString(e.evalFragment(c.Fragment))
		}
	}
	return b.String()
}

// ─── JSX text escaping ──────────────────────────────────────────────────────

// escapeContainerValue returns the evaluated value of a JSXExprContainer in a
// text position. Element-producing expressions (JSX, .map() of JSX, ternaries
// with JSX branches) are left raw so their markup survives; text-producing
// expressions (template literals, strings, concatenations) are HTML-escaped so
// `<Code>{`return <h1>x</h1>`}</Code>` can't leak real markup.
func (e *SSREval) escapeContainerValue(expr ast.Expr) string {
	v := e.eval(expr)
	if e.childrenIsHTML && isChildrenRef(expr) {
		// The children binding was already rendered (and escaped) by the
		// emitter's slot pipeline — injecting it raw is correct.
		return stripArraySep(v)
	}
	if e.isHTMLProducing(expr) {
		return stripArraySep(v)
	}
	return escape.HTML(v)
}

// isChildrenRef reports whether expr references the {children}/{props.children}
// placeholder.
func isChildrenRef(expr ast.Expr) bool {
	switch t := expr.(type) {
	case *ast.Identifier:
		return t.Name == "children"
	case *ast.MemberExpr:
		if id, ok := t.Object.(*ast.Identifier); ok && id.Name == "props" {
			if pid, ok := t.Property.(*ast.Identifier); ok && pid.Name == "children" {
				return true
			}
		}
	}
	return false
}

// isHTMLProducing reports whether evaluating expr can produce HTML elements
// (as opposed to plain text). Used to decide whether a JSX text position must
// escape its value.
func (e *SSREval) isHTMLProducing(expr ast.Expr) bool {
	if expr == nil {
		return false
	}
	switch t := expr.(type) {
	case *ast.JSXElement, *ast.JSXFragment:
		return true
	case *ast.ArrayExpr:
		for _, el := range t.Elements {
			if e.isHTMLProducing(el) {
				return true
			}
		}
		return false
	case *ast.ConditionalExpr:
		return e.isHTMLProducing(t.Consequent) || e.isHTMLProducing(t.Alternate)
	case *ast.BinaryExpr:
		// `left && <el/>`, `left || <el/>` — the right operand can be markup.
		return e.isHTMLProducing(t.Right)
	case *ast.CallExpr:
		// `.map()` produces markup only when the callback body produces markup
		// (e.g. items.map(i => <li>)); items.map(i => i.name) is text.
		if mem, ok := t.Callee.(*ast.MemberExpr); ok {
			if prop, ok := mem.Property.(*ast.Identifier); ok && prop.Name == "map" && len(t.Args) == 1 {
				if arrow, ok := t.Args[0].(*ast.ArrowFn); ok {
					if body := arrowBodyExpr(arrow); body != nil {
						return e.isHTMLProducing(body)
					}
				}
			}
		}
		return false
	case *ast.TemplateExpr:
		for _, p := range t.Parts {
			if e.isHTMLProducing(p) {
				return true
			}
		}
		return false
	case *ast.TypeAssertion:
		return e.isHTMLProducing(t.Expr)
	}
	return false
}

// ─── Built-in delegation to QuickJS ─────────────────────────────────────────

// globalBuiltins are ECMAScript globals that the Go SSR evaluator does not
// implement. Calls/constructors rooted at these names are delegated to the
// embedded QuickJS engine, which implements them with real JS semantics.
var globalBuiltins = map[string]bool{
	"Date": true, "Math": true, "String": true, "Number": true, "Boolean": true,
	"Array": true, "Object": true, "RegExp": true, "Symbol": true, "BigInt": true,
	"JSON": true, "Intl": true, "Promise": true, "parseInt": true, "parseFloat": true,
	"isNaN": true, "isFinite": true, "encodeURIComponent": true, "decodeURIComponent": true,
	"encodeURI": true, "decodeURI": true, "escape": true, "unescape": true,
	"globalThis": true, "window": true, "document": true, "navigator": true,
	"location": true, "console": true, "fetch": true, "structuredClone": true,
	"setTimeout": true, "clearTimeout": true, "setInterval": true, "clearInterval": true,
}

// globalRoot returns the root identifier of an expression chain (e.g. "Date"
// for Date.now(), "Math" for Math.round(x), "parseInt" for parseInt(s)).
func globalRoot(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.Identifier:
		return t.Name
	case *ast.MemberExpr:
		return globalRoot(t.Object)
	}
	return ""
}

// delegateJS hands a self-contained expression to the QuickJS evaluation hook
// and returns its string value. Returns "" when no hook is installed or the
// expression can't be evaluated (e.g. it references an undefined identifier).
func (e *SSREval) delegateJS(expr ast.Expr) string {
	if e.evalJS == nil {
		return ""
	}
	code := irtree.GenerateExprJS(expr, nil)
	if code == "" {
		return ""
	}
	v, err := e.evalJS(code)
	if err != nil {
		return ""
	}
	return v
}

// ─── Array ─────────────────────────────────────────────────────────────────

func (e *SSREval) evalArrayExpr(expr *ast.ArrayExpr) string {
	var parts []string
	for _, el := range expr.Elements {
		parts = append(parts, e.eval(el))
	}
	return strings.Join(parts, "\x1f")
}

func (e *SSREval) evalArrayMap(arrExpr ast.Expr, callback ast.Expr) string {
	arrow, ok := callback.(*ast.ArrowFn)
	if !ok {
		return ""
	}
	arrVal := e.eval(arrExpr)
	if arrVal == "" {
		return ""
	}
	// Split by separator (used by array rendering). Prop bindings arrive as
	// JS-array-literal strings (e.g. `[{title:'Welcome',url:'/docs/'}]`) from
	// buildPropBindings/evalConst, while code-built arrays use \x1f. Handle both.
	var items []string
	if strings.HasPrefix(arrVal, "[") {
		items = splitJSArrayLiteral(arrVal)
	} else {
		items = strings.Split(arrVal, "\x1f")
	}
	bodyExpr := arrowBodyExpr(arrow)
	if bodyExpr == nil {
		return ""
	}
	var results []string
	for i, item := range items {
		// Create bindings for the arrow params
		savedBindings := make(map[string]string)
		for k, v := range e.bindings {
			savedBindings[k] = v
		}
		if len(arrow.Params) >= 1 {
			e.bindings[arrow.Params[0].Name] = item
		}
		if len(arrow.Params) >= 2 {
			e.bindings[arrow.Params[1].Name] = itoa(i)
		}
		result := e.eval(bodyExpr)
		results = append(results, result)
		e.bindings = savedBindings
	}
	return strings.Join(results, "\x1f")
}

// splitJSArrayLiteral splits a JS array-literal string into its top-level
// elements, respecting nested braces/brackets and quoted strings. Used when a
// .map() source comes from a prop binding that was serialized by evalConst
// (e.g. sidebarItems={[{...},{...}]}).
func splitJSArrayLiteral(s string) []string {
	s = strings.TrimSpace(s)
	if len(s) < 2 || s[0] != '[' || s[len(s)-1] != ']' {
		// Not a well-formed array literal — fall back to whole string.
		if s == "" {
			return nil
		}
		return []string{s}
	}
	var items []string
	depth := 0
	inStr := byte(0)
	start := 1 // skip '['
	for i := 1; i < len(s)-1; i++ {
		ch := s[i]
		if inStr != 0 {
			if ch == '\\' {
				i++
				continue
			}
			if ch == inStr {
				inStr = 0
			}
			continue
		}
		switch ch {
		case '\'', '"', '`':
			inStr = ch
		case '{', '[':
			depth++
		case '}', ']':
			if depth > 0 {
				depth--
			}
		case ',':
			if depth == 0 {
				items = append(items, strings.TrimSpace(s[start:i]))
				start = i + 1
			}
		}
	}
	if start < len(s)-1 {
		items = append(items, strings.TrimSpace(s[start:len(s)-1]))
	}
	return items
}

// ─── Object ────────────────────────────────────────────────────────────────

func (e *SSREval) evalObjectExpr(expr *ast.ObjectExpr) string {
	var parts []string
	for _, prop := range expr.Properties {
		if prop.Spread {
			continue
		}
		val := e.eval(prop.Value)
		parts = append(parts, prop.Key+`:`+val)
	}
	return "{" + strings.Join(parts, ",") + "}"
}

// ─── Helpers ───────────────────────────────────────────────────────────────

// stringLiteralValue is already defined in eval.go.
