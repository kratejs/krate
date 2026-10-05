package renderer

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/escape"
	"github.com/kratejs/krate/packages/compiler/internal/irtree"
	"github.com/kratejs/krate/packages/compiler/internal/syntaxhighlight"
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

	// childrenRawText holds the unescaped text of the current component frame's
	// call-site children (text + evaluated expressions, no HTML escaping). It
	// lets compile-time processors like <SyntaxHighlight> chroma-highlighting
	// operate on the original code instead of already-escaped HTML.
	childrenRawText string

	// evalJS is an optional hook (wired by the build to the embedded QuickJS
	// engine) that evaluates a self-contained JS expression with full built-ins
	// (Date, Math, String, Number, ...). When set, calls to global built-ins the
	// Go evaluator can't handle statically (e.g. Date.now()) are delegated to
	// it, producing a genuine JS-engine value at SSR/compile time.
	evalJS func(code string) (string, error)

	// errs collects diagnostics raised when an expression construct that Krate
	// does not support is encountered during evaluation. Instead of silently
	// emitting empty/wrong output, the emitter surfaces these so the build
	// fails with a clear message.
	errs []error

	// cvaFactories maps module-level `const X = cva(...)` bindings to their
	// resolved specs so `X({ variant })` calls in a prop-driven component fold
	// to a static class string during SSR.
	cvaFactories map[string]*irtree.CVASpec

	// restProps maps a rest-parameter name (e.g. `props` in `{ a, ...props }`)
	// to the call-site attrs it collects, so `{...props}` spreads on intrinsic
	// elements expand during SSREval.
	restProps map[string]map[string]ast.Expr

	// tagAliases maps a local `const X = <tag expr>` binding to its tag
	// expression, so `<X/>` resolves to the concrete tag when it folds.
	tagAliases map[string]ast.Expr

	// jsxBindings marks local names bound to a JSX value (`const chip = <span/>`)
	// so `{chip}` is injected as markup instead of being HTML-escaped.
	jsxBindings map[string]bool

	// contextDefaults maps a module-level `const X = createContext(v)` binding to
	// its literal default v, so `X.useContext()` folds at build time.
	contextDefaults map[string]string

	// codeTheme is the chroma theme used to highlight <Code>/<SyntaxHighlight>
	// during SSREval, so inline highlighted blocks match the generated
	// stylesheet. Empty selects the default theme.
	codeTheme string
}

// SetCodeTheme installs the chroma theme for compile-time code highlighting.
func (e *SSREval) SetCodeTheme(theme string) {
	e.codeTheme = theme
}

// SetCVAFactories installs the module-wide cva factory table.
func (e *SSREval) SetCVAFactories(factories map[string]*irtree.CVASpec) {
	e.cvaFactories = factories
}

// SetContextDefaults installs the module-wide context-default table.
func (e *SSREval) SetContextDefaults(defaults map[string]string) {
	e.contextDefaults = defaults
}

// SetRestProps installs the rest-parameter attribute map for spread expansion.
func (e *SSREval) SetRestProps(rest map[string]map[string]ast.Expr) {
	e.restProps = rest
}

// expandSSRSpreadAttrs expands `{...name}` spreads resolving to a known rest
// parameter; explicit attributes win over spread-provided ones.
func (e *SSREval) expandSSRSpreadAttrs(attrs []*ast.JSXAttr) (out []*ast.JSXAttr, expanded bool) {
	if e.restProps == nil {
		return attrs, false
	}
	explicit := make(map[string]bool)
	for _, attr := range attrs {
		if !attr.Spread {
			explicit[attr.Name] = true
		}
	}
	for _, attr := range attrs {
		if !attr.Spread || attr.Value == nil {
			out = append(out, attr)
			continue
		}
		id, ok := attr.Value.(*ast.Identifier)
		if !ok {
			out = append(out, attr)
			continue
		}
		rest, ok := e.restProps[id.Name]
		if !ok {
			out = append(out, attr)
			continue
		}
		expanded = true
		names := make([]string, 0, len(rest))
		for name := range rest {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			if explicit[name] {
				continue
			}
			out = append(out, &ast.JSXAttr{Position: attr.Position, Name: name, Value: rest[name]})
		}
	}
	return out, expanded
}

// addErr records an unsupported-construct diagnostic during evaluation.
func (e *SSREval) addErr(format string, args ...interface{}) {
	e.errs = append(e.errs, fmt.Errorf("unsupported expression: "+format, args...))
}

// Errors returns the diagnostics collected during evaluation (may be empty).
func (e *SSREval) Errors() []error {
	return e.errs
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
		bindings:    make(map[string]string),
		arrays:      make(map[string][]string),
		jsxBindings: make(map[string]bool),
		functions:   functions,
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
	if e.tagAliases == nil {
		e.tagAliases = make(map[string]ast.Expr)
	}
	for _, stmt := range body {
		switch s := stmt.(type) {
		case *ast.VarStmt:
			for _, decl := range s.Decls {
				if decl.Name != "" && decl.Init != nil {
					// Array literals are registered as arrays (not scalar
					// bindings). An EMPTY array is registered too so a following
					// `for (...) { name.push(<JSX/>) }` can append to it — the
					// common `var items = []; for (...) items.push(<X/>)` pattern.
					if arr, ok := decl.Init.(*ast.ArrayExpr); ok && (hasJSX(arr) || len(arr.Elements) == 0) {
						var elems []string
						for _, el := range arr.Elements {
							elems = append(elems, e.eval(el))
						}
						e.arrays[decl.Name] = elems
						continue
					}
					// Record tag aliases (`const Comp = cond ? Slot : "button"`)
					// so `<Comp/>` can resolve to the concrete tag.
					if isTagExpr(decl.Init) {
						e.tagAliases[decl.Name] = decl.Init
					}
					// A local bound to JSX (`const chip = <span/>`) must render as
					// markup when read, not as escaped text.
					if e.isHTMLProducing(decl.Init) {
						e.jsxBindings[decl.Name] = true
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
						if arr, ok := decl.Init.(*ast.ArrayExpr); ok && (hasJSX(arr) || len(arr.Elements) == 0) {
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

// evalSlot renders `<Slot attrs>{child}</Slot>` by merging the Slot's
// attributes onto the single child element (the shadcn/radix `asChild`
// contract). Returns ok=false when there is not exactly one element child.
func (e *SSREval) evalSlot(el *ast.JSXElement) (string, bool) {
	var child *ast.JSXElement
	for _, c := range el.Children {
		switch ch := c.(type) {
		case *ast.JSXElementChild:
			if child != nil {
				return "", false
			}
			child = ch.Element
		case *ast.JSXText:
			if strings.TrimSpace(ch.Value) != "" {
				return "", false
			}
		case *ast.JSXExprContainer:
			// `{children}` / `{props.children}` forwards the call-site child,
			// which arrives via the frame's `children` binding.
			if !isChildrenRef(ch.Expression) {
				return "", false
			}
		}
	}
	if child == nil {
		// The child may have arrived via the frame's `children` binding (e.g.
		// a forwarded rest spread or `{children}`). Merge the Slot's attributes
		// into the rendered child HTML.
		if raw, ok := e.bindings["children"]; ok && raw != "" {
			if merged, ok := e.mergeSlotChildrenHTML(el, raw); ok {
				return merged, true
			}
		}
		return "", false
	}
	// Merge the Slot's explicit attributes under the child's own (child wins
	// for duplicates, matching cloneElement where later props override).
	merged := make([]*ast.JSXAttr, 0, len(el.Opening.Attributes)+len(child.Opening.Attributes))
	childAttrs := make(map[string]bool, len(child.Opening.Attributes))
	for _, a := range child.Opening.Attributes {
		if !a.Spread {
			childAttrs[a.Name] = true
		}
	}
	for _, a := range el.Opening.Attributes {
		if a.Spread {
			continue
		}
		if a.Name == "key" || a.Name == "ref" || isSlotOnlyAttr(a.Name) {
			continue
		}
		if childAttrs[a.Name] {
			continue
		}
		if a.Name == "class" || a.Name == "className" {
			merged = append(merged, mergeClassAttr(child, a))
			continue
		}
		merged = append(merged, a)
	}
	merged = append(merged, child.Opening.Attributes...)

	cloned := *child
	opening := *child.Opening
	opening.Attributes = e.expandSlotSpread(merged)
	cloned.Opening = &opening
	return e.evalJSX(&cloned), true
}

// mergeSlotChildrenHTML merges the Slot's attributes into the opening tag of
// already-rendered children HTML (the forwarded `children` binding case). It
// injects the class and any other simple attributes into the first element.
func (e *SSREval) mergeSlotChildrenHTML(slot *ast.JSXElement, html string) (string, bool) {
	start := strings.IndexByte(html, '<')
	if start < 0 {
		return "", false
	}
	// Skip closing tags / comments / doctype.
	if start+1 >= len(html) {
		return "", false
	}
	switch html[start+1] {
	case '/', '!':
		return "", false
	}
	end := strings.IndexByte(html[start:], '>')
	if end < 0 {
		return "", false
	}
	end += start

	cls := ""
	for _, a := range slot.Opening.Attributes {
		if a.Spread || isSlotOnlyAttr(a.Name) {
			continue
		}
		if a.Name == "class" || a.Name == "className" {
			cls = e.eval(a.Value)
		}
	}
	if cls == "" {
		return html, true
	}
	// Merge into an existing class attribute if present, else add one.
	openTag := html[start : end+1]
	if idx := indexClassAttr(openTag); idx >= 0 {
		// Insert the Slot's classes after `class="`.
		insertAt := start + idx
		return html[:insertAt] + cls + " " + html[insertAt:], true
	}
	return html[:end] + ` class="` + escape.HTML(cls) + `"` + html[end:], true
}

// indexClassAttr returns the offset just after the opening quote of a
// class="..." attribute in an opening tag, or -1.
func indexClassAttr(tag string) int {
	for _, key := range []string{` class="`, ` class='`} {
		if i := strings.Index(tag, key); i >= 0 {
			return i + len(key)
		}
	}
	return -1
}

// isSlotOnlyAttr reports attributes consumed by Slot itself, not forwarded.
func isSlotOnlyAttr(name string) bool {
	switch name {
	case "asChild", "children", "suppressHydrationWarning":
		return true
	}
	return false
}

// mergeClassAttr combines a Slot-provided class with the child's existing class
// attribute value (Slot class first, then child class), as a `a + " " + b`
// binary expression that the evaluator folds.
func mergeClassAttr(child *ast.JSXElement, slotAttr *ast.JSXAttr) *ast.JSXAttr {
	for _, ca := range child.Opening.Attributes {
		if ca.Name != "class" && ca.Name != "className" {
			continue
		}
		combined := &ast.BinaryExpr{
			Op: "+",
			Left: &ast.BinaryExpr{
				Op:    "+",
				Left:  slotAttr.Value,
				Right: &ast.Literal{Kind: ast.StringLit, Value: " "},
			},
			Right: ca.Value,
		}
		return &ast.JSXAttr{Position: slotAttr.Position, Name: "class", Value: combined}
	}
	return slotAttr
}

// expandSlotSpread drops unresolved spreads (Slot spread handling is limited to
// explicit attributes in this lowering).
func (e *SSREval) expandSlotSpread(attrs []*ast.JSXAttr) []*ast.JSXAttr {
	out := make([]*ast.JSXAttr, 0, len(attrs))
	for _, a := range attrs {
		if a.Spread {
			continue
		}
		out = append(out, a)
	}
	return out
}

// isTagExpr reports whether an initializer could be a JSX tag alias: a string
// literal, a component identifier, or a conditional/type-assertion of those.
func isTagExpr(expr ast.Expr) bool {
	switch e := expr.(type) {
	case *ast.Literal:
		return e.Kind == ast.StringLit
	case *ast.Identifier:
		return len(e.Name) > 0
	case *ast.ConditionalExpr:
		return isTagExpr(e.Consequent) || isTagExpr(e.Alternate)
	case *ast.TypeAssertion:
		return isTagExpr(e.Expr)
	}
	return false
}

// resolveTagAlias resolves a JSX tag bound to a local `const X = ...` to its
// concrete tag name when the binding folds against the current bindings.
func (e *SSREval) resolveTagAlias(name string) string {
	// Only uppercase tags are component references; lowercase tags are always
	// intrinsic and must never alias a same-named local.
	if len(name) == 0 || name[0] < 'A' || name[0] > 'Z' {
		return ""
	}
	if e.tagAliases == nil {
		return ""
	}
	expr, ok := e.tagAliases[name]
	if !ok {
		return ""
	}
	return e.foldTagExpr(expr)
}

func (e *SSREval) foldTagExpr(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.Literal:
		if t.Kind == ast.StringLit {
			return t.Value
		}
		return ""
	case *ast.Identifier:
		return t.Name
	case *ast.ConditionalExpr:
		if !e.isStaticExpr(t.Test) {
			return ""
		}
		if isSSRTruthy(e.eval(t.Test)) {
			return e.foldTagExpr(t.Consequent)
		}
		return e.foldTagExpr(t.Alternate)
	case *ast.TypeAssertion:
		return e.foldTagExpr(t.Expr)
	}
	return ""
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
		// An expression construct Krate's SSR evaluator does not support. Error
		// instead of silently rendering empty output.
		e.addErr("%s expression nodes are not supported", strings.TrimPrefix(fmt.Sprintf("%T", ex), "*ast."))
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
			r := toFloat(right)
			if r == 0 {
				// Match JS: x/0 is +/-Infinity (x=0 gives NaN).
				if toFloat(left) == 0 {
					return "NaN"
				}
				if left != "" && left[0] == '-' {
					return "-Infinity"
				}
				return "Infinity"
			}
			return trimFloatStr(toFloat(left) / r)
		}
		return left + right
	case "%":
		if isNumericStr(left) && isNumericStr(right) {
			r := toFloat(right)
			if r == 0 {
				return "NaN" // JS x % 0 is NaN; never divide by zero in Go.
			}
			return trimFloatStr(math.Mod(toFloat(left), r))
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
				// Property is missing (or the binding is not an object): return
				// "" so `item.name` on a plain string (and `item.x ? … : …`)
				// resolves to undefined/empty instead of rendering the whole
				// binding value.
				return ""
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
		// `X.useContext()` → the context default (no Provider at build time).
		if prop, ok := mem.Property.(*ast.Identifier); ok && prop.Name == "useContext" {
			if id, ok := mem.Object.(*ast.Identifier); ok && e.contextDefaults != nil {
				if def, found := e.contextDefaults[id.Name]; found {
					return def
				}
			}
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
	// Class helpers (cn/clsx) fold to a literal class string when every
	// argument is statically known, so a prop-driven component's
	// `className={cn("base", className)}` renders real classes at build time.
	if id, ok := expr.Callee.(*ast.Identifier); ok && (id.Name == "cn" || id.Name == "clsx") {
		if v, ok := e.evalClassCall(expr.Args); ok {
			return v
		}
	}
	// A known cva factory call (`buttonVariants({ variant, size })`) folds to
	// its class string when the selection is statically known.
	if id, ok := expr.Callee.(*ast.Identifier); ok && e.cvaFactories != nil {
		if spec, ok := e.cvaFactories[id.Name]; ok {
			return e.evalCVACall(spec, expr.Args)
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

// evalCVACall resolves a cva factory call to its class string using the current
// bindings for the variant selection.
func (e *SSREval) evalCVACall(spec *irtree.CVASpec, args []ast.Expr) string {
	selection := map[string]string{}
	extra := ""
	if len(args) >= 1 {
		if obj, ok := args[0].(*ast.ObjectExpr); ok {
			for _, prop := range obj.Properties {
				if prop == nil || prop.Spread || prop.Key == "" || !e.isStaticExpr(prop.Value) {
					continue
				}
				v := e.eval(prop.Value)
				if v == "" || v == "undefined" || v == "null" {
					continue
				}
				if prop.Key == "class" || prop.Key == "className" {
					extra = v
					continue
				}
				selection[prop.Key] = v
			}
		}
	}
	return spec.Fold(selection, extra)
}

// evalClassCall folds cn/clsx arguments against the current SSREval bindings.
// It returns ok=false when any argument cannot be statically resolved.
func (e *SSREval) evalClassCall(args []ast.Expr) (string, bool) {
	var tokens []string
	for _, arg := range args {
		toks, ok := e.evalClassValue(arg)
		if !ok {
			return "", false
		}
		tokens = append(tokens, toks...)
	}
	return strings.Join(tokens, " "), true
}

func (e *SSREval) evalClassValue(expr ast.Expr) ([]string, bool) {
	switch ex := expr.(type) {
	case *ast.Literal:
		switch ex.Kind {
		case ast.StringLit:
			if ex.Value == "" {
				return nil, true
			}
			return []string{ex.Value}, true
		case ast.NumberLit:
			return []string{ex.Value}, true
		default:
			return nil, true
		}
	case *ast.Identifier:
		if isChildrenRef(expr) {
			return nil, false
		}
		if v, ok := e.bindings[ex.Name]; ok {
			if v == "" || v == "undefined" || v == "null" || v == "false" {
				return nil, true
			}
			return []string{v}, true
		}
		if _, ok := e.arrays[ex.Name]; ok {
			return nil, false
		}
		// An unbound identifier is not statically known.
		return nil, false
	case *ast.CallExpr:
		// Nested class helpers behave like their result string.
		if id, ok := ex.Callee.(*ast.Identifier); ok && (id.Name == "cn" || id.Name == "clsx") {
			v, ok := e.evalClassCall(ex.Args)
			if !ok {
				return nil, false
			}
			if v == "" {
				return nil, true
			}
			return []string{v}, true
		}
		if id, ok := ex.Callee.(*ast.Identifier); ok && e.cvaFactories != nil {
			if spec, ok := e.cvaFactories[id.Name]; ok {
				v := e.evalCVACall(spec, ex.Args)
				if v == "" {
					return nil, true
				}
				return []string{v}, true
			}
		}
		return nil, false
	case *ast.TemplateExpr:
		for _, part := range ex.Parts {
			if !e.isStaticExpr(part) {
				return nil, false
			}
		}
		v := e.eval(expr)
		if v == "" {
			return nil, true
		}
		return []string{v}, true
	case *ast.ArrayExpr:
		var out []string
		for _, el := range ex.Elements {
			if el == nil {
				continue
			}
			toks, ok := e.evalClassValue(el)
			if !ok {
				return nil, false
			}
			out = append(out, toks...)
		}
		return out, true
	case *ast.ObjectExpr:
		var out []string
		for _, prop := range ex.Properties {
			if prop == nil || prop.Spread || prop.Key == "" {
				return nil, false
			}
			if !e.isStaticExpr(prop.Value) {
				return nil, false
			}
			v := e.eval(prop.Value)
			if isSSRTruthy(v) {
				out = append(out, prop.Key)
			}
		}
		return out, true
	case *ast.BinaryExpr:
		if ex.Op == "&&" {
			if !e.isStaticExpr(ex.Left) {
				return nil, false
			}
			if !isSSRTruthy(e.eval(ex.Left)) {
				return nil, true
			}
			return e.evalClassValue(ex.Right)
		}
		return nil, false
	case *ast.ConditionalExpr:
		if !e.isStaticExpr(ex.Test) {
			return nil, false
		}
		if isSSRTruthy(e.eval(ex.Test)) {
			return e.evalClassValue(ex.Consequent)
		}
		return e.evalClassValue(ex.Alternate)
	default:
		return nil, false
	}
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

// isStaticExpr reports whether expr evaluates to a compile-time-known value:
// literals, identifiers/props resolved through the current bindings, and pure
// operator combinations of those. Calls, JSX, and unbound identifiers are not.
func (e *SSREval) isStaticExpr(expr ast.Expr) bool {
	switch t := expr.(type) {
	case *ast.Literal:
		return true
	case *ast.Identifier:
		if isChildrenRef(expr) {
			return e.childrenRawText != ""
		}
		if _, bound := e.bindings[t.Name]; bound {
			return true
		}
		_, isArr := e.arrays[t.Name]
		return isArr
	case *ast.MemberExpr:
		if id, ok := t.Object.(*ast.Identifier); ok && id.Name == "props" {
			if prop, ok := t.Property.(*ast.Identifier); ok {
				_, bound := e.bindings[prop.Name]
				return bound
			}
		}
		return false
	case *ast.TemplateExpr:
		for _, p := range t.Parts {
			if !e.isStaticExpr(p) {
				return false
			}
		}
		return true
	case *ast.BinaryExpr:
		return e.isStaticExpr(t.Left) && e.isStaticExpr(t.Right)
	case *ast.UnaryExpr:
		return e.isStaticExpr(t.Arg)
	case *ast.ConditionalExpr:
		return e.isStaticExpr(t.Test) && e.isStaticExpr(t.Consequent) && e.isStaticExpr(t.Alternate)
	case *ast.TypeAssertion:
		return e.isStaticExpr(t.Expr)
	}
	return false
}

// tryEvalSyntaxHighlight renders <SyntaxHighlight> as compile-time chroma
// HTML. A {children} passthrough resolves from the current frame's raw
// call-site text so chroma sees the original code. ok=false when any part of
// the content isn't statically known — the caller then renders a plain code
// block through the standard children rules instead of highlighting
// placeholder values.
func (e *SSREval) tryEvalSyntaxHighlight(el *ast.JSXElement) (string, bool) {
	lang := ""
	for _, attr := range el.Opening.Attributes {
		if attr.Spread || attr.Value == nil || attr.Name != "lang" {
			continue
		}
		lang = e.eval(attr.Value)
	}

	var code strings.Builder
	for _, child := range el.Children {
		switch c := child.(type) {
		case *ast.JSXText:
			code.WriteString(c.Value)
		case *ast.JSXExprContainer:
			if !e.isStaticExpr(c.Expression) {
				return "", false
			}
			if isChildrenRef(c.Expression) {
				code.WriteString(e.childrenRawText)
				continue
			}
			code.WriteString(e.eval(c.Expression))
		default:
			return "", false
		}
	}

	codeStr := strings.TrimSpace(code.String())
	normalizedLang := syntaxhighlight.NormalizeLanguage(lang)
	if normalizedLang == "" {
		return "<pre><code>" + escape.HTML(codeStr) + "</code></pre>", true
	}
	highlighted := syntaxhighlight.HighlightTheme(codeStr, normalizedLang, e.codeTheme)
	return "<pre class=\"chroma\"><code class=\"language-" + escape.HTML(lang) + "\">" + highlighted + "</code></pre>", true
}

// evalPlainCodeBlock renders a <SyntaxHighlight> whose content isn't
// statically known as a plain code block. Children go through the standard
// JSX text rules (text escaped, rendered markup injected raw), mirroring the
// tree builder's dynamic fallback.
func (e *SSREval) evalPlainCodeBlock(el *ast.JSXElement) string {
	lang := ""
	for _, attr := range el.Opening.Attributes {
		if attr.Spread || attr.Value == nil || attr.Name != "lang" {
			continue
		}
		lang = e.eval(attr.Value)
	}

	var b strings.Builder
	b.WriteString("<pre class=\"chroma\"><code class=\"language-" + escape.HTML(lang) + "\">")
	for _, child := range el.Children {
		switch c := child.(type) {
		case *ast.JSXText:
			b.WriteString(escape.HTML(c.Value))
		case *ast.JSXExprContainer:
			b.WriteString(e.escapeContainerValue(c.Expression))
		}
	}
	b.WriteString("</code></pre>")
	return b.String()
}

// evalLink renders the built-in <Link> as an <a> with the SPA/prefetch data
// attributes the client router consumes. The generic element path then renders
// it (attributes evaluated against the current bindings).
func (e *SSREval) evalLink(el *ast.JSXElement) string {
	prefetch := "true"
	replace := ""
	external := false
	scrollFalse := false
	var out []*ast.JSXAttr
	for _, a := range el.Opening.Attributes {
		if a.Spread {
			out = append(out, a)
			continue
		}
		switch a.Name {
		case "prefetch":
			if a.Value != nil {
				prefetch = e.eval(a.Value)
			}
		case "replace":
			if a.Value == nil {
				replace = "true"
			} else {
				replace = e.eval(a.Value)
			}
		case "external":
			external = true
		case "scroll":
			if lit, ok := a.Value.(*ast.Literal); ok && lit.Value == "false" {
				scrollFalse = true
			}
		case "className":
			out = append(out, &ast.JSXAttr{Name: "class", Value: a.Value})
		default:
			out = append(out, a)
		}
	}
	if external {
		out = append(out, &ast.JSXAttr{Name: "data-krate-external", Value: nil})
	} else {
		out = append(out, &ast.JSXAttr{Name: "data-krate-link", Value: nil})
		if prefetch != "false" {
			out = append(out, &ast.JSXAttr{Name: "data-prefetch", Value: nil})
		}
		if replace == "true" {
			out = append(out, &ast.JSXAttr{Name: "data-krate-replace", Value: nil})
		}
		if scrollFalse {
			out = append(out, &ast.JSXAttr{Name: "data-krate-scroll", Value: &ast.Literal{Kind: ast.StringLit, Value: "false"}})
		}
	}
	clone := &ast.JSXElement{
		Opening:  &ast.JSXOpening{Name: "a", Attributes: out, SelfClosing: el.Opening.SelfClosing},
		Children: el.Children,
		Closing:  &ast.JSXClosing{Name: "a"},
	}
	return e.evalJSX(clone)
}

func (e *SSREval) evalJSX(el *ast.JSXElement) string {
	// `showIf`/`visibleIf` sugar: {test && <el/>}. Signal-less components reach
	// this path, so the test must be evaluated statically against the bindings
	// (props/locals); a truthy test renders the stripped element, a falsy test
	// renders nothing. A signal-referencing test would have promoted the
	// component to the client tier and never arrive here.
	if test, stripped, ok := irtree.ShowIfExpr(el); ok {
		if isSSRTruthy(e.eval(test)) {
			return e.evalJSX(stripped)
		}
		return ""
	}

	name := el.Opening.Name

	// A tag bound to a local `const X = cond ? Tag : "tag"` resolves through the
	// eval bindings when the condition folds (the shadcn `asChild` pattern).
	if alias := e.resolveTagAlias(name); alias != "" && alias != name {
		cloned := *el
		opening := *el.Opening
		opening.Name = alias
		cloned.Opening = &opening
		return e.evalJSX(&cloned)
	}

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

	// <SyntaxHighlight lang="...">code</SyntaxHighlight> — compile-time chroma
	// highlighting when the content is statically known; a plain code block
	// through the standard children rules otherwise.
	if name == "SyntaxHighlight" {
		if html, ok := e.tryEvalSyntaxHighlight(el); ok {
			return html
		}
		return e.evalPlainCodeBlock(el)
	}

	// Built-in <Link>: lower to an <a> carrying the SPA/prefetch data
	// attributes so signal-less components (theme chrome, breadcrumbs, prev/
	// next) render real anchors instead of an unknown component.
	if name == "Link" {
		return e.evalLink(el)
	}

	// <Slot> (the `asChild` primitive): render its single child element with
	// the Slot's own attributes merged onto it, instead of wrapping.
	if name == "Slot" {
		if html, ok := e.evalSlot(el); ok {
			return html
		}
		// No single element child — render children alone.
		return e.evalChildren(el.Children)
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
	attrs, expanded := e.expandSSRSpreadAttrs(el.Opening.Attributes)
	var rawInnerHTML string
	hasRawInnerHTML := false
	for _, attr := range attrs {
		if attr.Spread || attr.Name != "dangerouslySetInnerHTML" || attr.Value == nil {
			continue
		}
		if raw, ok := e.evalInnerHTMLValue(attr.Value); ok {
			rawInnerHTML = raw
			hasRawInnerHTML = true
		}
	}

	for _, attr := range attrs {
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
		// A prop that resolved to undefined/null is omitted entirely (React
		// drops undefined attributes).
		if val == "undefined" || val == "null" {
			continue
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
		// A self-closing element may still receive children through a forwarded
		// rest spread (`<Comp {...props}/>`), since React's props object carries
		// `children`. Inject the frame's children when a spread was expanded and
		// they are present.
		if expanded {
			if ch, ok := e.bindings["children"]; ok && ch != "" {
				b.WriteByte('>')
				b.WriteString(ch)
				b.WriteString("</")
				b.WriteString(name)
				b.WriteByte('>')
				return b.String()
			}
		}
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
		children := el.Children
		// React's props object carries `children`, so `<Comp {...props}/>`
		// forwards the call-site children. When this element has no explicit
		// children but a rest spread was expanded, inject the component
		// frame's children binding.
		if len(children) == 0 && expanded {
			if ch, ok := e.bindings["children"]; ok && ch != "" {
				b.WriteString(ch)
				b.WriteString("</")
				b.WriteString(name)
				b.WriteByte('>')
				return b.String()
			}
		}
		for _, child := range children {
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
	var childrenRaw strings.Builder
	for _, child := range el.Children {
		switch c := child.(type) {
		case *ast.JSXText:
			childrenContent.WriteString(c.Value)
			childrenRaw.WriteString(c.Value)
		case *ast.JSXExprContainer:
			v := e.eval(c.Expression)
			// Raw variant first: no HTML escaping, so compile-time processors
			// (<SyntaxHighlight>) see the original code text.
			if e.childrenIsHTML && isChildrenRef(c.Expression) {
				childrenRaw.WriteString(stripArraySep(v))
			} else {
				childrenRaw.WriteString(v)
			}
			childrenContent.WriteString(e.escapeContainerValueEvaluated(c.Expression, v))
		case *ast.JSXElementChild:
			childrenContent.WriteString(e.evalJSX(c.Element))
			childrenRaw.WriteString(e.evalJSX(c.Element))
		case *ast.JSXFragmentChild:
			childrenContent.WriteString(e.evalFragment(c.Fragment))
			childrenRaw.WriteString(e.evalFragment(c.Fragment))
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

	// Same scoping for the raw children text: it belongs to this component
	// frame only and must not leak into nested component calls.
	savedChildrenRaw := e.childrenRawText
	if preRenderedChildren {
		e.childrenRawText = childrenRaw.String()
	} else {
		e.childrenRawText = ""
	}
	defer func() { e.childrenRawText = savedChildrenRaw }()

	e.BindLocalVars(fn.Body)

	// Resolve if/else-if/else chains where each branch ends in a return (e.g.
	// <AsideIcon> whose body is `if ... return <svg> ... else if ... else`).
	// Such bodies have no top-level return statement, so they'd previously be
	// dropped by the no-top-level-return guard below.
	if v, ok := e.evalBranchReturns(fn.Body); ok {
		e.bindings = savedBindings
		return v
	}

	// Handle if-return patterns with a following main return:
	//   if (!hasPrev && !hasNext) return <span />;
	//   return <div>...</div>;
	ret := findReturnStmtIn(fn.Body)
	if ret == nil || ret.Value == nil {
		e.bindings = savedBindings
		return ""
	}

	result := e.eval(ret.Value)

	e.bindings = savedBindings
	return result
}

// evalBranchReturns resolves a return value from an if/else-if/else chain whose
// branches each end in a return, preferring the first truthy branch and falling
// back to the else branch. This supports signal-less components whose function
// body has no top-level return statement (e.g. an SVG helper that returns from
// each if/else branch). Returns ok=false when no branch resolves to a return.
// Block-bodied ifs (if (c) { return <x/>; }) are unwrapped so the return inside
// the braces is found.
func (e *SSREval) evalBranchReturns(stmts []ast.Stmt) (string, bool) {
	for _, stmt := range stmts {
		switch s := stmt.(type) {
		case *ast.BlockStmt:
			if v, ok := e.evalBranchReturns(s.Body); ok {
				return v, true
			}
		case *ast.IfStmt:
			condVal := e.eval(s.Test)
			truthy := condVal != "" && condVal != "false" && condVal != "null" && condVal != "undefined" && condVal != "0"
			if truthy {
				if v, ok := e.returnFromConsequent(s.Consequent); ok {
					return v, true
				}
				if v, ok := e.evalBranchReturns(s.Consequent); ok {
					return v, true
				}
			} else if v, ok := e.evalBranchReturns(s.Alternate); ok {
				return v, true
			}
		case *ast.ReturnStmt:
			if s.Value != nil {
				return e.eval(s.Value), true
			}
		}
	}
	return "", false
}

// returnFromConsequent extracts the direct return value from an if-consequent
// statement list (unwrapping a wrapping block), if it ends in a return.
func (e *SSREval) returnFromConsequent(consequent []ast.Stmt) (string, bool) {
	for _, stmt := range consequent {
		switch s := stmt.(type) {
		case *ast.BlockStmt:
			if v, ok := e.returnFromConsequent(s.Body); ok {
				return v, true
			}
		case *ast.ReturnStmt:
			if s.Value != nil {
				return e.eval(s.Value), true
			}
		}
	}
	return "", false
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
	return e.escapeContainerValueEvaluated(expr, e.eval(expr))
}

// escapeContainerValueEvaluated is escapeContainerValue with a pre-evaluated
// value so callers that also need the raw value avoid evaluating twice.
func (e *SSREval) escapeContainerValueEvaluated(expr ast.Expr, v string) string {
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
	case *ast.Identifier:
		// An identifier bound to a JSX-built array (e.g. `var items = [];
		// for (...) items.push(<li/>)`) or to a JSX value (`const chip = <b/>`)
		// evaluates to HTML markup.
		if _, isArr := e.arrays[t.Name]; isArr {
			return true
		}
		return e.jsxBindings[t.Name]
	case *ast.MemberExpr:
		// A prop/local member whose resolved value is markup (e.g.
		// `{props.body}` where body={<b/>}) is HTML, not text.
		return strings.HasPrefix(strings.TrimSpace(e.eval(t)), "<")
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
		key := prop.Key
		// Quote keys that are not valid identifiers (e.g. "a-b").
		if !isIdentString(key) {
			key = strconv.Quote(key)
		}
		val := e.eval(prop.Value)
		parts = append(parts, key+`:`+val)
	}
	return "{" + strings.Join(parts, ",") + "}"
}

// isIdentString reports whether s is a valid JS identifier.
func isIdentString(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if i == 0 {
			if !(ch == '_' || ch == '$' || (ch >= 'A' && ch <= 'Z') || (ch >= 'a' && ch <= 'z')) {
				return false
			}
		} else if !(ch == '_' || ch == '$' || (ch >= 'A' && ch <= 'Z') || (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9')) {
			return false
		}
	}
	return true
}

// ─── Helpers ───────────────────────────────────────────────────────────────

// stringLiteralValue is already defined in eval.go.
