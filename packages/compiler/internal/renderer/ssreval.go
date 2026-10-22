package renderer

import (
	"fmt"
	"sort"
	"strings"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/diag"
	"github.com/kratejs/krate/packages/compiler/internal/escape"
	"github.com/kratejs/krate/packages/compiler/internal/irtree"
	"github.com/kratejs/krate/packages/compiler/internal/jsvalue"
	"github.com/kratejs/krate/packages/compiler/internal/syntaxhighlight"
)

// SSREval renders signal-less (prop-driven) components to HTML at build time.
// It has NO signal tracking, NO handler collection, NO path tracking.
//
// Expressions evaluate to typed JS values (internal/jsvalue), so truthiness,
// operators and child rendering follow JS/React semantics. Only JSX values are
// ever written as raw markup; strings and numbers are always HTML-escaped.
type SSREval struct {
	functions map[string]*ast.FnDecl
	depth     int

	in *jsvalue.Interp
	// module holds module-level bindings (constants evaluated lazily from
	// moduleInits, and module functions). Component frames are child scopes
	// of module, so a component never sees its caller's locals.
	module      *jsvalue.Scope
	moduleInits map[string]ast.Expr
	evaluating  map[string]bool
	// frame is the scope of the root component being evaluated.
	frame *jsvalue.Scope

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
	// element whose `name` attribute is an expression. The hook receives the
	// evaluated name and the element's forwarded attributes, and returns the
	// compiled SVG HTML. Return handled=false when the name could not be
	// resolved so the default path applies.
	iconEmit func(name string, attrs []*ast.JSXAttr) (html string, handled bool)

	// childrenRawText holds the unescaped text of the current component frame's
	// call-site children (text + evaluated expressions, no HTML escaping). It
	// lets compile-time processors like <SyntaxHighlight> chroma-highlighting
	// operate on the original code instead of already-escaped HTML.
	childrenRawText string

	// evalJS is an optional hook (wired by the build to the embedded QuickJS
	// engine) that evaluates a self-contained JS expression with full built-ins
	// (Date, Intl, ...). Calls to global built-ins the Go evaluator does not
	// implement are delegated to it, producing a genuine JS-engine value.
	evalJS func(code string) (string, error)

	// errs collects diagnostics raised when an expression cannot be evaluated
	// at build time. Instead of silently emitting empty/wrong output, the
	// emitter surfaces these so the build fails with a clear message.
	errs []error

	// cvaFactories maps module-level `const X = cva(...)` bindings to their
	// resolved specs so `X({ variant })` calls fold to a static class string.
	cvaFactories map[string]*irtree.CVASpec

	// restProps maps a rest-parameter name (e.g. `props` in `{ a, ...props }`)
	// to the call-site attrs it collects, so `{...props}` spreads on intrinsic
	// elements expand during SSREval.
	restProps map[string]map[string]ast.Expr

	// tagAliases maps a local `const X = <tag expr>` binding to its tag
	// expression, so `<X/>` resolves to the concrete tag when it folds.
	tagAliases map[string]ast.Expr

	// contextDefaults maps a module-level `const X = createContext(v)` binding to
	// its literal default v, so `X.useContext()` folds at build time.
	contextDefaults map[string]jsvalue.Value
	// contextStack holds the provider values in scope per context, innermost
	// last: the instance's build-time providers (SetContextValues) plus
	// providers met while evaluating its JSX.
	contextStack map[string][]jsvalue.Value

	// codeTheme is the chroma theme used to highlight <Code>/<SyntaxHighlight>.
	codeTheme string

	// goComponents maps imported Go component names to their refs, and goEnqueue
	// records a build-time render request, returning a splice marker.
	goComponents map[string]irtree.GoComponentRef
	goEnqueue    func(callID, name, propsJSON string) string

	// nativeGuard prevents re-entering the built-in delegation hook while a
	// call is evaluated natively first.
	nativeGuard map[ast.Expr]bool

	// selectStack holds the selected option values of enclosing <select>
	// elements (nil when the select has no value/defaultValue).
	selectStack [][]string
}

// maxEvalDepth bounds nested component evaluation (a recursion guard, not a
// size limit: real component trees are far shallower).
const maxEvalDepth = 256

// NewSSREval creates a new SSR evaluator.
func NewSSREval(functions map[string]*ast.FnDecl) *SSREval {
	e := &SSREval{
		functions:   functions,
		evaluating:  map[string]bool{},
		nativeGuard: map[ast.Expr]bool{},
		tagAliases:  map[string]ast.Expr{},
	}
	e.module = jsvalue.NewScope(nil)
	e.frame = jsvalue.NewScope(e.module)
	e.in = &jsvalue.Interp{Scope: e.frame}
	e.in.Hooks = jsvalue.Hooks{
		Ident: e.resolveIdent,
		Call:  e.interceptCall,
		New:   e.interceptNew,
		JSX: func(expr ast.Expr) jsvalue.Value {
			switch x := expr.(type) {
			case *ast.JSXElement:
				return jsvalue.JSX(e.evalJSX(x))
			case *ast.JSXFragment:
				return jsvalue.JSX(e.evalFragment(x))
			}
			return jsvalue.Unsupported("JSX")
		},
	}
	return e
}

// SetGoComponents installs the Go-component table + enqueue hook.
func (e *SSREval) SetGoComponents(comps map[string]irtree.GoComponentRef, enqueue func(callID, name, propsJSON string) string) {
	e.goComponents = comps
	e.goEnqueue = enqueue
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
func (e *SSREval) SetContextDefaults(defaults map[string]jsvalue.Value) {
	e.contextDefaults = defaults
}

// SetContextValues installs the provider values in scope where the evaluated
// component instance is rendered.
func (e *SSREval) SetContextValues(values map[string]jsvalue.Value) {
	for ctx, v := range values {
		e.pushContext(ctx, v)
	}
}

func (e *SSREval) pushContext(ctx string, v jsvalue.Value) {
	if e.contextStack == nil {
		e.contextStack = make(map[string][]jsvalue.Value)
	}
	e.contextStack[ctx] = append(e.contextStack[ctx], v)
}

func (e *SSREval) popContext(ctx string) {
	if s := e.contextStack[ctx]; len(s) > 0 {
		e.contextStack[ctx] = s[:len(s)-1]
	}
}

// contextValue resolves a context read: the nearest provider value, else the
// createContext default.
func (e *SSREval) contextValue(ctx string) (jsvalue.Value, bool) {
	if s := e.contextStack[ctx]; len(s) > 0 {
		return s[len(s)-1], true
	}
	def, ok := e.contextDefaults[ctx]
	return def, ok
}

// isContextName reports whether name is a module-level createContext binding.
func (e *SSREval) isContextName(name string) bool {
	if _, ok := e.contextDefaults[name]; !ok {
		return false
	}
	_, fn := e.functions[name]
	return !fn
}

// evalContextElement renders `<Ctx.Provider value>`, React 19 `<Ctx value>`
// and `<Ctx.Consumer>{value => ...}</Ctx.Consumer>`.
func (e *SSREval) evalContextElement(el *ast.JSXElement, name string) (string, bool) {
	ctx, member, _ := strings.Cut(name, ".")
	if !e.isContextName(ctx) {
		return "", false
	}
	switch member {
	case "", "Provider":
		value := jsvalue.Undefined()
		for _, attr := range el.Opening.Attributes {
			if attr != nil && !attr.Spread && attr.Name == "value" && attr.Value != nil {
				value = e.value(attr.Value)
			}
		}
		e.pushContext(ctx, value)
		defer e.popContext(ctx)
		return e.renderChildren(el.Children), true
	case "Consumer":
		cur, _ := e.contextValue(ctx)
		var out strings.Builder
		for _, child := range el.Children {
			c, ok := child.(*ast.JSXExprContainer)
			if !ok || c.Expression == nil {
				continue
			}
			fn := e.value(c.Expression)
			if !fn.IsFunction() {
				e.reportUnknown(c.Expression, fn, "<"+name+"> child")
				continue
			}
			out.WriteString(e.renderValue(c.Expression, fn.Call(cur)))
		}
		return out.String(), true
	}
	return "", false
}

// SetRestProps installs the rest-parameter attribute map for spread expansion.
func (e *SSREval) SetRestProps(rest map[string]map[string]ast.Expr) {
	e.restProps = rest
}

// SetModuleConsts installs module-level `const X = <expr>` initializers. They
// are evaluated lazily, on first reference, in the module scope.
func (e *SSREval) SetModuleConsts(inits map[string]ast.Expr) {
	e.moduleInits = inits
}

// SetEvalJS installs the expression-evaluation hook backed by the embedded
// QuickJS runtime. See the evalJS field docs.
func (e *SSREval) SetEvalJS(fn func(code string) (string, error)) {
	e.evalJS = fn
}

// SetBindings binds the root component's props. Each name is bound directly
// (destructured parameters) and the whole set is also exposed as a `props`
// object (a single `props` parameter), matching how the IR builder flattens
// call-site attributes.
func (e *SSREval) SetBindings(bindings map[string]jsvalue.Value) {
	names := make([]string, 0, len(bindings))
	for k := range bindings {
		names = append(names, k)
	}
	sort.Strings(names)
	props := jsvalue.NewObject()
	if existing, ok := e.frame.Lookup("props"); ok && existing.IsObject() {
		props = existing
	}
	for _, k := range names {
		e.frame.Define(k, bindings[k])
		if k != "props" {
			props.Set(k, bindings[k])
		}
	}
	if v, ok := bindings["props"]; ok && v.IsObject() {
		return
	}
	e.frame.Define("props", props)
}

// ResolveJSXProps re-evaluates call-site props whose value is JSX (which the
// IR const folder cannot represent) and binds the rendered markup.
func (e *SSREval) ResolveJSXProps(attrs map[string]ast.Expr) {
	names := make([]string, 0, len(attrs))
	for name, expr := range attrs {
		if expr != nil && hasJSX(expr) {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return
	}
	sort.Strings(names)
	bindings := make(map[string]jsvalue.Value, len(names))
	for _, name := range names {
		if cur, ok := e.frame.Lookup(name); ok && !cur.IsUnknown() {
			continue
		}
		bindings[name] = e.value(attrs[name])
	}
	e.SetBindings(bindings)
}

// SetChildrenHTML binds the root component's call-site children, already
// rendered to HTML by the emitter's slot pipeline.
func (e *SSREval) SetChildrenHTML(html string) {
	children := jsvalue.JSX(html)
	e.frame.Define("children", children)
	if props, ok := e.frame.Lookup("props"); ok && props.IsObject() {
		props.Set("children", children)
	}
}

// Errors returns the diagnostics collected during evaluation (may be empty).
func (e *SSREval) Errors() []error {
	return e.errs
}

// addDiag records a source-located diagnostic.
func (e *SSREval) addDiag(pos ast.Pos, msg, hint string) {
	e.errs = append(e.errs, diag.Diagnostic{Line: pos.Line, Col: pos.Col, Message: msg, Hint: hint})
}

// reportUnknown records why a value needed for output is not known at build
// time. Signal-less components are rendered entirely at build time, so there is
// no runtime fallback: an unknown value must fail the build, never render as a
// silent guess.
func (e *SSREval) reportUnknown(expr ast.Expr, v jsvalue.Value, context string) {
	pos := ast.Pos{}
	src := ""
	if expr != nil {
		pos = expr.Pos()
		src = irtree.GenerateExprJS(expr, nil)
	}
	if v.IsUnsupported() {
		e.addDiag(pos, "unsupported expression: "+v.Reason()+" (in "+context+" `"+src+"`)",
			"this component is rendered at build time; rewrite the expression with supported constructs or move it into a component with client state")
		return
	}
	e.addDiag(pos, "cannot evaluate `"+src+"` at build time: "+v.Reason()+" (in "+context+")",
		"signal-less components are rendered to static HTML at build time, so every value they render must be a prop, a local, or a module constant")
}

// Value evaluation

// value evaluates an expression in the current scope.
func (e *SSREval) value(expr ast.Expr) jsvalue.Value {
	if expr == nil {
		return jsvalue.Undefined()
	}
	return e.in.Eval(expr)
}

// Eval evaluates an expression and returns it rendered as HTML in JSX child
// position (text escaped, JSX raw, null/undefined/booleans empty).
func (e *SSREval) Eval(expr ast.Expr) string {
	return e.renderChildExpr(expr)
}

// renderChildExpr renders the value of a {expr} container in child position.
func (e *SSREval) renderChildExpr(expr ast.Expr) string {
	if expr == nil {
		return ""
	}
	return e.renderValue(expr, e.value(expr))
}

// renderValue renders an evaluated value in child position, reporting values
// that cannot be rendered.
func (e *SSREval) renderValue(expr ast.Expr, v jsvalue.Value) string {
	html, err := jsvalue.RenderChild(v)
	if err != nil {
		if uerr, ok := err.(*jsvalue.UnknownChildError); ok {
			e.reportUnknown(expr, uerr.Value, "JSX child")
		} else {
			pos := ast.Pos{}
			src := ""
			if expr != nil {
				pos = expr.Pos()
				src = irtree.GenerateExprJS(expr, nil)
			}
			e.addDiag(pos, "objects are not valid as a JSX child (found `"+src+"`)",
				"render a property of the object, or map it to elements")
		}
	}
	return html
}

// resolveIdent resolves identifiers not bound in the current scope chain:
// module constants, module functions, and the browser globals that are
// undefined during server rendering.
func (e *SSREval) resolveIdent(name string) (jsvalue.Value, bool) {
	if v, ok := e.module.Lookup(name); ok {
		return v, true
	}
	if init, ok := e.moduleInits[name]; ok && init != nil && !e.evaluating[name] {
		e.evaluating[name] = true
		v := e.in.EvalIn(jsvalue.NewScope(e.module), init)
		delete(e.evaluating, name)
		e.module.Define(name, v)
		return v, true
	}
	if fn := e.functions[name]; fn != nil {
		saved := e.in.Scope
		e.in.Scope = e.module
		v := e.in.Closure(fn.Name, fn.Params, fn.Body)
		e.in.Scope = saved
		e.module.Define(name, v)
		return v, true
	}
	switch name {
	case "window", "document", "navigator", "location", "localStorage", "sessionStorage", "self":
		// Server rendering has no browser globals: `typeof window` is
		// "undefined", exactly as under react-dom/server.
		return jsvalue.Undefined(), true
	}
	return jsvalue.Value{}, false
}

// interceptCall handles calls with build-time-specific meaning: class helpers,
// cva factories, context reads, and delegation of JS built-ins.
func (e *SSREval) interceptCall(call *ast.CallExpr) (jsvalue.Value, bool) {
	switch callee := call.Callee.(type) {
	case *ast.Identifier:
		if e.isBound(callee.Name) {
			break
		}
		if callee.Name == "cn" || callee.Name == "clsx" {
			if v, ok := e.foldClassCall(call.Args); ok {
				return jsvalue.String(v), true
			}
			return jsvalue.Dynamic(callee.Name + "(...) with arguments not known at build time"), true
		}
		if spec, ok := e.cvaFactories[callee.Name]; ok {
			return e.evalCVACall(spec, call.Args), true
		}
		if (callee.Name == "useContext" || callee.Name == "use") && len(call.Args) == 1 {
			if arg, ok := call.Args[0].(*ast.Identifier); ok {
				if def, found := e.contextValue(arg.Name); found {
					return def, true
				}
			}
		}
	case *ast.MemberExpr:
		if prop, ok := callee.Property.(*ast.Identifier); ok && (prop.Name == "useContext" || prop.Name == "use") {
			if id, ok := callee.Object.(*ast.Identifier); ok {
				if def, found := e.contextValue(id.Name); found {
					return def, true
				}
			}
		}
	}
	return e.maybeDelegate(call, call.Callee)
}

func (e *SSREval) interceptNew(n *ast.NewExpr) (jsvalue.Value, bool) {
	return e.maybeDelegate(n, n.Callee)
}

func (e *SSREval) isBound(name string) bool {
	_, ok := e.in.Scope.Lookup(name)
	return ok
}

// nativeGlobals are the built-ins jsvalue implements itself. Their calls run
// natively and only fall back to the JS engine for members jsvalue lacks.
var nativeGlobals = map[string]bool{
	"Math": true, "JSON": true, "String": true, "Number": true, "Boolean": true,
	"Object": true, "Array": true, "parseInt": true, "parseFloat": true,
	"isNaN": true, "isFinite": true,
}

// maybeDelegate evaluates a call/new rooted at a JS global built-in through the
// embedded JS engine when the Go evaluator does not implement it natively.
func (e *SSREval) maybeDelegate(expr ast.Expr, callee ast.Expr) (jsvalue.Value, bool) {
	root := globalRoot(callee)
	if !globalBuiltins[root] || e.isBound(root) || e.nativeGuard[expr] {
		return jsvalue.Value{}, false
	}
	if nativeGlobals[root] {
		e.nativeGuard[expr] = true
		v := e.in.Eval(expr)
		delete(e.nativeGuard, expr)
		if !v.IsUnsupported() {
			return v, true
		}
	}
	return e.delegateJS(expr), true
}

// globalBuiltins are ECMAScript/host globals. Calls rooted at those jsvalue
// does not implement are delegated to the embedded JS engine.
var globalBuiltins = map[string]bool{
	"Date": true, "Math": true, "String": true, "Number": true, "Boolean": true,
	"Array": true, "Object": true, "RegExp": true, "Symbol": true, "BigInt": true,
	"JSON": true, "Intl": true, "Promise": true, "parseInt": true, "parseFloat": true,
	"isNaN": true, "isFinite": true, "encodeURIComponent": true, "decodeURIComponent": true,
	"encodeURI": true, "decodeURI": true, "escape": true, "unescape": true,
	"globalThis": true, "console": true, "structuredClone": true,
}

// globalRoot returns the root identifier of an expression chain (e.g. "Date"
// for Date.now(), "Math" for Math.round(x), "Date" for new Date(x).getTime()).
func globalRoot(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.Identifier:
		return t.Name
	case *ast.MemberExpr:
		return globalRoot(t.Object)
	case *ast.CallExpr:
		return globalRoot(t.Callee)
	case *ast.NewExpr:
		return globalRoot(t.Callee)
	}
	return ""
}

// delegateJS evaluates expr in the embedded JS engine. Free variables are
// passed in as literal arguments of a wrapping function, and the result comes
// back as typed JSON, so the value keeps its JS type.
func (e *SSREval) delegateJS(expr ast.Expr) jsvalue.Value {
	if e.evalJS == nil {
		return jsvalue.Unsupported("`" + irtree.GenerateExprJS(expr, nil) + "` needs the JS engine, which is not available here")
	}
	var params, args []string
	for _, name := range freeIdentifiers(expr) {
		if globalBuiltins[name] {
			continue
		}
		v := e.value(&ast.Identifier{Name: name})
		if v.IsUnknown() {
			return v
		}
		src, ok := jsvalue.JSSource(v)
		if !ok {
			return jsvalue.Unsupported("value of " + name + " cannot be passed to the JS engine")
		}
		params = append(params, name)
		args = append(args, src)
	}
	code := irtree.GenerateExprJS(expr, nil)
	if code == "" {
		return jsvalue.Unsupported("expression cannot be compiled for the JS engine")
	}
	wrapped := "((" + strings.Join(params, ",") + ")=>{const v=(" + code + ");" +
		"const t=v===null?'null':Array.isArray(v)?'array':typeof v;" +
		"return JSON.stringify([t,(t==='object'&&Object.getPrototypeOf(v)!==Object.prototype)?String(v):(t==='undefined'||t==='function'||t==='symbol'||t==='bigint')?String(v):v]);})(" +
		strings.Join(args, ",") + ")"
	out, err := e.evalJS(wrapped)
	if err != nil {
		return jsvalue.Unsupported("JS engine error: " + err.Error())
	}
	pair, ok := jsvalue.FromJSON(out)
	if !ok || len(pair.Elems()) != 2 {
		return jsvalue.Unsupported("JS engine returned an unexpected result")
	}
	kind, payload := pair.Elems()[0].StringValue(), pair.Elems()[1]
	switch kind {
	case "undefined":
		return jsvalue.Undefined()
	case "function", "symbol", "bigint":
		return jsvalue.Unsupported(kind + " values from the JS engine are not supported")
	}
	// A non-plain object (Date, Map, ...) arrives as its string form.
	return payload
}

// freeIdentifiers lists identifiers referenced by expr outside of member
// property positions and arrow parameter lists.
func freeIdentifiers(expr ast.Expr) []string {
	seen := map[string]bool{}
	var out []string
	var walk func(x ast.Expr, bound map[string]bool)
	walk = func(x ast.Expr, bound map[string]bool) {
		switch t := x.(type) {
		case *ast.Identifier:
			if !bound[t.Name] && !seen[t.Name] && t.Name != "undefined" && t.Name != "NaN" && t.Name != "Infinity" {
				seen[t.Name] = true
				out = append(out, t.Name)
			}
		case *ast.MemberExpr:
			walk(t.Object, bound)
			if t.Computed {
				walk(t.Property, bound)
			}
		case *ast.CallExpr:
			walk(t.Callee, bound)
			for _, a := range t.Args {
				walk(a, bound)
			}
		case *ast.NewExpr:
			walk(t.Callee, bound)
			for _, a := range t.Args {
				walk(a, bound)
			}
		case *ast.BinaryExpr:
			walk(t.Left, bound)
			walk(t.Right, bound)
		case *ast.UnaryExpr:
			walk(t.Arg, bound)
		case *ast.ConditionalExpr:
			walk(t.Test, bound)
			walk(t.Consequent, bound)
			walk(t.Alternate, bound)
		case *ast.TemplateExpr:
			for _, p := range t.Parts {
				walk(p, bound)
			}
		case *ast.ArrayExpr:
			for _, el := range t.Elements {
				walk(el, bound)
			}
		case *ast.ObjectExpr:
			for _, p := range t.Properties {
				if p.Value != nil {
					walk(p.Value, bound)
				} else if p.Shorthand {
					walk(&ast.Identifier{Name: p.Key}, bound)
				}
			}
		case *ast.TypeAssertion:
			walk(t.Expr, bound)
		case *ast.ArrowFn:
			inner := map[string]bool{}
			for k := range bound {
				inner[k] = true
			}
			for _, p := range t.Params {
				inner[p.Name] = true
			}
			for _, s := range t.Body {
				switch st := s.(type) {
				case *ast.ReturnStmt:
					walk(st.Value, inner)
				case *ast.ExprStmt:
					walk(st.Expression, inner)
				}
			}
		}
	}
	walk(expr, map[string]bool{})
	return out
}

// Class helpers

// foldClassCall folds cn/clsx arguments with clsx semantics. ok=false when any
// argument is not statically known.
func (e *SSREval) foldClassCall(args []ast.Expr) (string, bool) {
	var tokens []string
	for _, a := range args {
		v := e.value(a)
		if v.ContainsUnknown() {
			return "", false
		}
		tokens = appendClassTokens(tokens, v)
	}
	return strings.Join(tokens, " "), true
}

// appendClassTokens flattens a clsx argument: strings and numbers are tokens,
// arrays recurse, objects contribute keys with truthy values, falsy values
// contribute nothing.
func appendClassTokens(tokens []string, v jsvalue.Value) []string {
	if !v.ToBoolean() {
		return tokens
	}
	switch v.Kind() {
	case jsvalue.KindString, jsvalue.KindNumber:
		return append(tokens, v.ToString())
	case jsvalue.KindArray:
		for _, el := range v.Elems() {
			tokens = appendClassTokens(tokens, el)
		}
	case jsvalue.KindObject:
		for _, k := range v.Keys() {
			if p, _ := v.Get(k); p.ToBoolean() {
				tokens = append(tokens, k)
			}
		}
	}
	return tokens
}

// evalCVACall resolves a cva factory call to its class string using the
// current bindings for the variant selection.
func (e *SSREval) evalCVACall(spec *irtree.CVASpec, args []ast.Expr) jsvalue.Value {
	selection := map[string]string{}
	extra := ""
	if len(args) >= 1 {
		obj := e.value(args[0])
		if obj.IsUnknown() {
			return obj
		}
		for _, k := range obj.Keys() {
			v, _ := obj.Get(k)
			if v.IsUnknown() {
				return v
			}
			if v.IsNullish() || (v.Kind() == jsvalue.KindString && v.StringValue() == "") {
				continue
			}
			if k == "class" || k == "className" {
				extra = v.ToString()
				continue
			}
			selection[k] = v.ToString()
		}
	}
	return jsvalue.String(spec.Fold(selection, extra))
}

// Statement evaluation

// EvalComponentBody runs a component function body in the current frame and
// renders its return value. Unknown control flow or return values are
// reported as diagnostics.
func (e *SSREval) EvalComponentBody(fn *ast.FnDecl) string {
	if fn == nil {
		return ""
	}
	html, _ := e.evalBranchReturns(fn.Body)
	return html
}

// evalBranchReturns runs a component body and renders its return value.
// ok=false when the body completes without returning.
func (e *SSREval) evalBranchReturns(stmts []ast.Stmt) (string, bool) {
	e.collectTagAliases(stmts)
	v, returned := e.in.ExecBlock(stmts)
	if !returned {
		return "", false
	}
	var at ast.Expr = &ast.Identifier{Name: "return"}
	if ret := findReturnStmtIn(stmts); ret != nil && ret.Value != nil {
		at = ret.Value
	}
	return e.renderValue(at, v), true
}

// collectTagAliases records `const X = <tag expr>` locals so `<X/>` resolves.
func (e *SSREval) collectTagAliases(body []ast.Stmt) {
	for _, stmt := range body {
		vs, ok := stmt.(*ast.VarStmt)
		if !ok {
			continue
		}
		for _, d := range vs.Decls {
			if d.Name != "" && d.Init != nil && isTagExpr(d.Init) {
				e.tagAliases[d.Name] = d.Init
			}
		}
	}
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
		test := e.value(t.Test)
		truthy, known := test.Truthy()
		if !known {
			return ""
		}
		if truthy {
			return e.foldTagExpr(t.Consequent)
		}
		return e.foldTagExpr(t.Alternate)
	case *ast.TypeAssertion:
		return e.foldTagExpr(t.Expr)
	}
	return ""
}

// Slot

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
		if raw := e.childrenHTML(); raw != "" {
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
	opening.Attributes = expandSlotSpread(merged)
	cloned.Opening = &opening
	return e.evalJSX(&cloned), true
}

// childrenHTML returns the current frame's rendered children markup.
func (e *SSREval) childrenHTML() string {
	if v, ok := e.in.Scope.Lookup("children"); ok && v.IsJSX() {
		return v.HTML()
	}
	if props, ok := e.in.Scope.Lookup("props"); ok && props.IsObject() {
		if v, ok := props.Get("children"); ok && v.IsJSX() {
			return v.HTML()
		}
	}
	return ""
}

// mergeSlotChildrenHTML merges the Slot's attributes into the opening tag of
// already-rendered children HTML (the forwarded `children` binding case). It
// injects the class into the first element.
func (e *SSREval) mergeSlotChildrenHTML(slot *ast.JSXElement, html string) (string, bool) {
	start := strings.IndexByte(html, '<')
	if start < 0 || start+1 >= len(html) {
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
			if v := e.value(a.Value); !v.IsNullish() && !v.IsUnknown() {
				cls = v.ToString()
			}
		}
	}
	if cls == "" {
		return html, true
	}
	// Merge into an existing class attribute if present, else add one.
	openTag := html[start : end+1]
	if idx := indexClassAttr(openTag); idx >= 0 {
		insertAt := start + idx
		return html[:insertAt] + escape.HTML(cls) + " " + html[insertAt:], true
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
func expandSlotSpread(attrs []*ast.JSXAttr) []*ast.JSXAttr {
	out := make([]*ast.JSXAttr, 0, len(attrs))
	for _, a := range attrs {
		if a.Spread {
			continue
		}
		out = append(out, a)
	}
	return out
}

// expandSSRSpreadAttrs expands `{...expr}` spreads on an intrinsic element with
// React's ordering (a later attribute or spread overrides an earlier one). A
// known rest parameter expands to the call-site attrs it collects; any other
// value that evaluates to an object (the whole `props` object, a local object)
// expands to one attribute per own key, each reading `expr[key]` so normal
// attribute evaluation serializes it. A nullish spread contributes nothing;
// an unresolved one is kept and skipped by the emitter. The second result
// reports whether a spread forwards `children`.
func (e *SSREval) expandSSRSpreadAttrs(attrs []*ast.JSXAttr) ([]*ast.JSXAttr, bool) {
	return irtree.MergeSpreadAttrs(attrs, func(attr *ast.JSXAttr) irtree.SpreadExpansion {
		if id, ok := attr.Value.(*ast.Identifier); ok && e.restProps != nil {
			if rest, ok := e.restProps[id.Name]; ok {
				exp := irtree.SpreadExpansion{Resolved: true, ForwardsChildren: true}
				names := make([]string, 0, len(rest))
				for name := range rest {
					names = append(names, name)
				}
				sort.Strings(names)
				for _, name := range names {
					exp.Attrs = append(exp.Attrs, &ast.JSXAttr{Position: attr.Position, Name: name, Value: rest[name]})
				}
				return exp
			}
		}
		v := e.value(attr.Value)
		if v.IsNullish() {
			return irtree.SpreadExpansion{Resolved: true}
		}
		if !v.IsObject() {
			return irtree.SpreadExpansion{}
		}
		exp := irtree.SpreadExpansion{Resolved: true}
		for _, k := range v.Keys() {
			if k == "children" {
				exp.ForwardsChildren = true
				continue
			}
			exp.Attrs = append(exp.Attrs, &ast.JSXAttr{Position: attr.Position, Name: k, Value: &ast.MemberExpr{
				Position: attr.Position,
				Object:   attr.Value,
				Property: &ast.Literal{Kind: ast.StringLit, Value: k},
				Computed: true,
			}})
		}
		return exp
	})
}

// JSX

// resolveIconName evaluates the `name` attribute of an <Icon> element.
func (e *SSREval) resolveIconName(el *ast.JSXElement) string {
	for _, attr := range el.Opening.Attributes {
		if attr.Spread || attr.Name != "name" {
			continue
		}
		if lit, ok := attr.Value.(*ast.Literal); ok {
			return lit.Value
		}
		v := e.value(attr.Value)
		if v.Kind() == jsvalue.KindString {
			return v.StringValue()
		}
		return ""
	}
	return ""
}

// tryEvalSyntaxHighlight renders <SyntaxHighlight> as compile-time chroma
// HTML. A {children} passthrough resolves from the current frame's raw
// call-site text so chroma sees the original code. ok=false when any part of
// the content isn't statically known.
func (e *SSREval) tryEvalSyntaxHighlight(el *ast.JSXElement) (string, bool) {
	lang := e.langAttr(el)
	var code strings.Builder
	for _, child := range el.Children {
		switch c := child.(type) {
		case *ast.JSXText:
			code.WriteString(c.Value)
		case *ast.JSXExprContainer:
			if isChildrenRef(c.Expression) {
				if e.childrenRawText == "" {
					return "", false
				}
				code.WriteString(e.childrenRawText)
				continue
			}
			v := e.value(c.Expression)
			if v.ContainsUnknown() || v.IsJSX() {
				return "", false
			}
			code.WriteString(jsvalue.RenderRawChild(v))
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

func (e *SSREval) langAttr(el *ast.JSXElement) string {
	for _, attr := range el.Opening.Attributes {
		if attr.Spread || attr.Value == nil || attr.Name != "lang" {
			continue
		}
		if v := e.value(attr.Value); !v.IsNullish() && !v.IsUnknown() {
			return v.ToString()
		}
	}
	return ""
}

// evalPlainCodeBlock renders a <SyntaxHighlight> whose content isn't
// statically known as a plain code block through the standard child rules.
func (e *SSREval) evalPlainCodeBlock(el *ast.JSXElement) string {
	lang := e.langAttr(el)
	var b strings.Builder
	b.WriteString("<pre class=\"chroma\"><code class=\"language-" + escape.HTML(lang) + "\">")
	b.WriteString(e.renderChildren(el.Children))
	b.WriteString("</code></pre>")
	return b.String()
}

// evalLink renders the built-in <Link> as an <a> with the SPA/prefetch data
// attributes the client router consumes.
func (e *SSREval) evalLink(el *ast.JSXElement) string {
	prefetch := true
	replace := false
	external := false
	scrollFalse := false
	isFalse := func(v jsvalue.Value) bool {
		return (v.Kind() == jsvalue.KindBool && !v.BoolValue()) || (v.Kind() == jsvalue.KindString && v.StringValue() == "false")
	}
	var out []*ast.JSXAttr
	for _, a := range el.Opening.Attributes {
		if a.Spread {
			out = append(out, a)
			continue
		}
		switch a.Name {
		case "prefetch":
			if a.Value != nil {
				prefetch = !isFalse(e.value(a.Value))
			}
		case "replace":
			if a.Value == nil {
				replace = true
			} else {
				v := e.value(a.Value)
				replace = v.ToBoolean() && !isFalse(v)
			}
		case "external":
			external = true
		case "scroll":
			if a.Value != nil && isFalse(e.value(a.Value)) {
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
		if prefetch {
			out = append(out, &ast.JSXAttr{Name: "data-prefetch", Value: nil})
		}
		if replace {
			out = append(out, &ast.JSXAttr{Name: "data-krate-replace", Value: nil})
		}
		if scrollFalse {
			out = append(out, &ast.JSXAttr{Name: "data-krate-scroll", Value: &ast.Literal{Kind: ast.StringLit, Value: "false"}})
		}
	}
	clone := &ast.JSXElement{
		Position: el.Position,
		Opening:  &ast.JSXOpening{Name: "a", Attributes: out, SelfClosing: el.Opening.SelfClosing},
		Children: el.Children,
		Closing:  &ast.JSXClosing{Name: "a"},
	}
	return e.evalJSX(clone)
}

// attrValue evaluates an attribute expression for the htmlspec serializer.
// A value not known at build time is reported and treated as absent: a
// build-time rendered component has no runtime to fill it in later.
func (e *SSREval) attrValue(expr ast.Expr) jsvalue.Value {
	v := e.value(expr)
	if v.ContainsUnknown() {
		e.reportUnknown(expr, firstUnknownOf(v), "attribute")
		return jsvalue.Undefined()
	}
	return v
}

// firstUnknownOf returns the first Unknown inside v (v itself if Unknown).
func firstUnknownOf(v jsvalue.Value) jsvalue.Value {
	if v.IsUnknown() {
		return v
	}
	for _, el := range v.Elems() {
		if el.ContainsUnknown() {
			return firstUnknownOf(el)
		}
	}
	for _, k := range v.Keys() {
		if p, _ := v.Get(k); p.ContainsUnknown() {
			return firstUnknownOf(p)
		}
	}
	return v
}

func (e *SSREval) evalJSX(el *ast.JSXElement) string {
	// `showIf`/`visibleIf` sugar: {test && <el/>}. Signal-less components reach
	// this path, so the test is evaluated statically against the bindings; a
	// truthy test renders the stripped element, a falsy test renders nothing.
	if test, stripped, ok := irtree.ShowIfExpr(el); ok {
		v := e.value(test)
		truthy, known := v.Truthy()
		if !known {
			e.reportUnknown(test, v, "showIf")
			return ""
		}
		if truthy {
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
	case "Script":
		e.ScriptHTML += e.renderMetaElement(el, "script")
		return ""
	case "Style":
		e.StyleHTML += e.renderMetaElement(el, "style")
		return ""
	}

	// <Icon> with a dynamic `name` attribute resolves through the bindings and
	// the emitter's iconEmit hook compiles it to SVG markup.
	if name == "Icon" && e.iconEmit != nil {
		if iconName := e.resolveIconName(el); iconName != "" {
			if html, handled := e.iconEmit(iconName, el.Opening.Attributes); handled {
				return html
			}
		}
	}

	if name == "SyntaxHighlight" {
		if html, ok := e.tryEvalSyntaxHighlight(el); ok {
			return html
		}
		return e.evalPlainCodeBlock(el)
	}

	// Built-in <Link> lowers to an <a> with the SPA/prefetch data attributes.
	if name == "Link" {
		return e.evalLink(el)
	}

	// <Slot> (the `asChild` primitive): render its single child element with
	// the Slot's own attributes merged onto it, instead of wrapping.
	if name == "Slot" {
		if html, ok := e.evalSlot(el); ok {
			return html
		}
		return e.renderChildren(el.Children)
	}

	if html, ok := e.evalContextElement(el, name); ok {
		return html
	}

	// Uppercase = component - resolve and evaluate recursively.
	if len(name) > 0 && name[0] >= 'A' && name[0] <= 'Z' {
		if e.interactiveEmit != nil {
			if html, handled := e.interactiveEmit(el); handled {
				return html
			}
		}
		if ref, ok := e.goComponents[name]; ok && e.goEnqueue != nil {
			return e.evalGoComponent(el, ref)
		}
		if fn := e.functions[name]; fn != nil {
			return e.evalComponentFn(fn, el)
		}
		// A component held in a local or prop (e.g. `const Comp = props.as`).
		if v, ok := e.in.Scope.Lookup(name); ok && v.IsFunction() {
			return e.evalComponentValue(v, el)
		}
		e.addDiag(el.Position, "unknown component <"+name+"> in a build-time rendered component",
			"import or define "+name+" so it can be rendered; an unresolved component is not silently dropped")
		return ""
	}

	return e.renderElement(el, name)
}

// renderChildren renders JSX children in child position.
func (e *SSREval) renderChildren(children []ast.JSXChild) string {
	var b strings.Builder
	for _, child := range children {
		switch c := child.(type) {
		case *ast.JSXText:
			b.WriteString(escape.Text(c.Value))
		case *ast.JSXExprContainer:
			b.WriteString(e.renderChildExpr(c.Expression))
		case *ast.JSXElementChild:
			b.WriteString(e.evalJSX(c.Element))
		case *ast.JSXFragmentChild:
			b.WriteString(e.evalFragment(c.Fragment))
		}
	}
	return b.String()
}

// renderMetaElement renders a <Script>/<Style> element as a complete tag
// (opening tag with attributes + raw children + closing tag). The children are
// written raw so inline JS / CSS is never HTML-escaped.
func (e *SSREval) renderMetaElement(el *ast.JSXElement, tag string) string {
	var b strings.Builder
	b.WriteByte('<')
	b.WriteString(tag)
	for _, attr := range el.Opening.Attributes {
		if attr.Spread || attr.Name == "dangerouslySetInnerHTML" {
			continue
		}
		b.WriteString(irtree.RenderAttr(tag, attr, e.attrValue, nil))
	}
	b.WriteByte('>')
	b.WriteString(e.renderElementChildren(tag, el.Children))
	b.WriteString("</")
	b.WriteString(tag)
	b.WriteByte('>')
	return b.String()
}

// evalInnerHTMLValue extracts the __html string from a
// dangerouslySetInnerHTML={{__html: expr}} attribute value.
func (e *SSREval) evalInnerHTMLValue(expr ast.Expr) (string, bool) {
	v := e.value(expr)
	if !v.IsObject() {
		if v.IsUnknown() {
			e.reportUnknown(expr, v, "dangerouslySetInnerHTML")
		}
		return "", false
	}
	h, ok := v.Get("__html")
	if !ok {
		return "", false
	}
	if h.IsUnknown() {
		e.reportUnknown(expr, h, "dangerouslySetInnerHTML")
		return "", false
	}
	if h.IsNullish() {
		return "", true
	}
	return h.ToString(), true
}

// evalChildren evaluates JSX children to raw content for Head/Script/Style
// capture (text is not escaped: inline scripts and styles stay intact).
func (e *SSREval) evalChildren(children []ast.JSXChild) string {
	var b strings.Builder
	for _, child := range children {
		switch c := child.(type) {
		case *ast.JSXText:
			b.WriteString(escape.Text(c.Value))
		case *ast.JSXExprContainer:
			if c.Expression == nil {
				continue
			}
			v := e.value(c.Expression)
			if v.ContainsUnknown() {
				e.reportUnknown(c.Expression, firstUnknownOf(v), "head/script/style content")
				continue
			}
			b.WriteString(jsvalue.RenderRawChild(v))
		case *ast.JSXElementChild:
			b.WriteString(e.evalJSX(c.Element))
		case *ast.JSXFragmentChild:
			b.WriteString(e.evalFragment(c.Fragment))
		}
	}
	return b.String()
}

// evalGoComponent enqueues a build-time render for an imported Go component: it
// serializes props to JSON, renders children to HTML, and returns a splice
// marker the build replaces with the host's output.
func (e *SSREval) evalGoComponent(el *ast.JSXElement, ref irtree.GoComponentRef) string {
	props := map[string]string{}
	for _, attr := range el.Opening.Attributes {
		if attr == nil || attr.Spread || attr.Name == "" {
			continue
		}
		if attr.Value == nil {
			props[attr.Name] = "true"
			continue
		}
		v := e.value(attr.Value)
		js, ok := jsvalue.JSONStringify(v)
		if !ok {
			if v.ContainsUnknown() {
				e.reportUnknown(attr.Value, firstUnknownOf(v), "Go component prop "+attr.Name)
			}
			js = "null"
		}
		props[attr.Name] = js
	}
	childHTML := e.renderChildren(el.Children)
	callID := irtree.GoCallID(el.Position.Line, el.Position.Col, ref.Func)
	return e.goEnqueue(callID, ref.Func, goPropsJSON(props, childHTML))
}

// callSiteProps evaluates a component element's attributes (in the caller's
// scope) into a props object, plus its rendered children.
func (e *SSREval) callSiteProps(el *ast.JSXElement) (props jsvalue.Value, childrenRaw string, hasChildren bool) {
	props = jsvalue.NewObject()
	for _, attr := range el.Opening.Attributes {
		if attr == nil {
			continue
		}
		if attr.Spread {
			src := e.value(attr.Value)
			switch {
			case src.IsObject():
				for _, k := range src.Keys() {
					v, _ := src.Get(k)
					props.Set(k, v)
				}
			case src.IsNullish():
			default:
				e.reportUnknown(attr.Value, src, "spread props")
			}
			continue
		}
		if attr.Name == "key" || attr.Name == "ref" {
			continue
		}
		if attr.Value == nil {
			props.Set(attr.Name, jsvalue.Bool(true))
			continue
		}
		props.Set(attr.Name, e.value(attr.Value))
	}

	var html, raw strings.Builder
	var items []jsvalue.Value
	for _, child := range el.Children {
		switch c := child.(type) {
		case *ast.JSXText:
			if c.Value == "" {
				continue
			}
			html.WriteString(escape.Text(c.Value))
			raw.WriteString(c.Value)
			hasChildren = true
		case *ast.JSXExprContainer:
			if c.Expression == nil {
				continue
			}
			v := e.value(c.Expression)
			switch {
			case v.IsJSX():
				raw.WriteString(v.HTML())
			case !v.ContainsUnknown():
				raw.WriteString(jsvalue.RenderRawChild(v))
			}
			items = append(items, v)
			if v.IsFunction() {
				// A render-prop child is passed through, not rendered.
				hasChildren = true
				continue
			}
			html.WriteString(e.renderValue(c.Expression, v))
			hasChildren = true
		case *ast.JSXElementChild:
			h := e.evalJSX(c.Element)
			html.WriteString(h)
			raw.WriteString(h)
			hasChildren = true
		case *ast.JSXFragmentChild:
			h := e.evalFragment(c.Fragment)
			html.WriteString(h)
			raw.WriteString(h)
			hasChildren = true
		}
	}
	if hasChildren {
		// A single non-JSX expression child keeps its value (a string or a
		// render callback passed as children); otherwise children are the
		// rendered markup.
		if len(el.Children) == 1 && len(items) == 1 && !items[0].IsJSX() && !items[0].ContainsUnknown() {
			props.Set("children", items[0])
		} else {
			props.Set("children", jsvalue.JSX(html.String()))
		}
	}
	return props, raw.String(), hasChildren
}

// evalComponentFn evaluates a component function with props extracted from JSX
// attributes. The body runs in a fresh scope under the module scope.
func (e *SSREval) evalComponentFn(fn *ast.FnDecl, el *ast.JSXElement) string {
	e.depth++
	defer func() { e.depth-- }()
	if e.depth > maxEvalDepth {
		e.addDiag(el.Position, fmt.Sprintf("component <%s> nests deeper than %d levels during build-time rendering", el.Opening.Name, maxEvalDepth),
			"check for a component that renders itself without a terminating condition")
		return ""
	}
	props, raw, hasChildren := e.callSiteProps(el)

	scope := jsvalue.NewScope(e.module)
	savedScope := e.in.Scope
	e.in.Scope = scope
	for i, p := range fn.Params {
		if p == nil {
			continue
		}
		arg := jsvalue.Undefined()
		if i == 0 {
			arg = props
		}
		if arg.Kind() == jsvalue.KindUndefined && p.Default != nil {
			arg = e.in.Eval(p.Default)
		}
		if r := e.in.BindPattern(scope, p.Name, p.Pattern, arg); r.IsUnsupported() {
			e.in.Scope = savedScope
			e.addDiag(el.Position, "unsupported expression: "+r.Reason()+" in the parameters of <"+el.Opening.Name+">", "simplify the parameter pattern")
			return ""
		}
	}
	// Expose `children` for `{children}` reads in a `props` component.
	if hasChildren {
		if _, ok := scope.Lookup("children"); !ok {
			if ch, ok := props.Get("children"); ok {
				scope.Define("children", ch)
			}
		}
	}

	savedRaw := e.childrenRawText
	if hasChildren {
		e.childrenRawText = raw
	} else {
		e.childrenRawText = ""
	}
	savedAliases := e.tagAliases
	e.tagAliases = map[string]ast.Expr{}
	defer func() {
		e.in.Scope = savedScope
		e.childrenRawText = savedRaw
		e.tagAliases = savedAliases
	}()
	return e.EvalComponentBody(fn)
}

// evalComponentValue renders a component held in a variable (a closure).
func (e *SSREval) evalComponentValue(fn jsvalue.Value, el *ast.JSXElement) string {
	e.depth++
	defer func() { e.depth-- }()
	if e.depth > maxEvalDepth {
		e.addDiag(el.Position, fmt.Sprintf("component <%s> nests deeper than %d levels during build-time rendering", el.Opening.Name, maxEvalDepth),
			"check for a component that renders itself without a terminating condition")
		return ""
	}
	props, _, _ := e.callSiteProps(el)
	return e.renderValue(el, fn.Call(props))
}

func (e *SSREval) evalFragment(frag *ast.JSXFragment) string {
	return e.renderChildren(frag.Children)
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
