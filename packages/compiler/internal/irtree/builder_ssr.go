package irtree

import (
	"sort"
	"strconv"
	"strings"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/escape"
	"github.com/kratejs/krate/packages/compiler/internal/jsvalue"
)

// SSR expression helpers

// collectModuleConsts scans the module-level (top-of-file) variable
// declarations and constant-folds those with statically-evaluable initializers
// into a name -> value map (legacy string form, for the build's cross-module
// merge). Later declarations may reference earlier ones (const c = a + b).
func collectModuleConsts(prog *ast.Program) map[string]string {
	out := make(map[string]string)
	for name, v := range typedModuleConsts(collectModuleConstInits(prog), nil) {
		if s := legacyConstString(nil, v); s != "" {
			out[name] = s
		}
	}
	return out
}

// collectModuleConstInits returns the initializer expression for every
// module-level `const X = <expr>`/`let`/`var` binding. Unlike collectModuleConsts
// (which folds to a bare value string), this preserves the AST so a module const
// referenced by client code can be rendered as valid JS with generateExprJS
// (keeping strings quoted and arrays/objects as real literals).
func collectModuleConstInits(prog *ast.Program) map[string]ast.Expr {
	out := make(map[string]ast.Expr)
	if prog == nil {
		return out
	}
	for _, stmt := range prog.Body {
		var vs *ast.VarStmt
		switch s := stmt.(type) {
		case *ast.VarStmt:
			vs = s
		case *ast.ExportStmt:
			if v, ok := s.Declaration.(*ast.VarStmt); ok {
				vs = v
			}
		}
		if vs == nil {
			continue
		}
		for _, decl := range vs.Decls {
			if decl.Name != "" && decl.Init != nil {
				out[decl.Name] = decl.Init
			}
		}
	}
	return out
}

// evalValue folds expr against the current component scope (signals, props,
// locals, module constants) with cva factories and server-action URLs.
func (b *builder) evalValue(expr ast.Expr) jsvalue.Value {
	if expr == nil {
		return jsvalue.Undefined()
	}
	return b.constInterp(b.localProps).Eval(expr)
}

// constInterp returns a const-folding interpreter over props that also knows
// the builder's cva factories and server-action URLs.
func (b *builder) constInterp(props valueMap) *jsvalue.Interp {
	in := newConstInterp(b.sigMap(), props)
	ident, call := in.Hooks.Ident, in.Hooks.Call
	in.Hooks.Ident = func(name string) (jsvalue.Value, bool) {
		if u, ok := b.actionURLs[name]; ok {
			if _, shadowed := props[name]; !shadowed {
				return jsvalue.String(u), true
			}
		}
		return ident(name)
	}
	in.Hooks.Call = func(c *ast.CallExpr) (jsvalue.Value, bool) {
		if id, ok := c.Callee.(*ast.Identifier); ok {
			if spec, ok := b.cvaFactories[id.Name]; ok {
				return foldCVAValue(in, spec, c.Args), true
			}
			if id.Name == "twMerge" {
				if _, isProp := props[id.Name]; !isProp {
					return foldClassValues(in, c.Args), true
				}
			}
		}
		return call(c)
	}
	return in
}

// foldCVAValue folds a cva factory call against a statically-known selection.
func foldCVAValue(in *jsvalue.Interp, spec *CVASpec, args []ast.Expr) jsvalue.Value {
	selection := map[string]string{}
	extra := ""
	if len(args) >= 1 {
		obj := in.Eval(args[0])
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

// evalAttrConst resolves a JSX attribute value at build time, folding class
// helpers (cn/clsx/twMerge), cva variant calls and server-action URLs in
// addition to ordinary constants.
func (b *builder) evalAttrConst(expr ast.Expr) jsvalue.Value {
	if expr == nil {
		return jsvalue.Undefined()
	}
	if mem, ok := expr.(*ast.MemberExpr); ok && !mem.Computed {
		if obj, ok := mem.Object.(*ast.Identifier); ok && obj.Name == "props" {
			if prop, ok := mem.Property.(*ast.Identifier); ok {
				if raw, ok := b.localProps[prop.Name]; ok && raw.Kind() == jsvalue.KindString {
					if u := strings.Trim(raw.StringValue(), `"'`); strings.Contains(u, "/__krate/action/") {
						return jsvalue.String(u)
					}
				}
			}
		}
	}
	return b.evalValue(expr)
}

// evalAttrValue is evalAttrConst in the legacy string form expected by the
// attribute serialization and binding initials, so className folds
// consistently across render paths.
func (b *builder) evalAttrValue(expr ast.Expr) string {
	return legacyConstString(expr, b.evalAttrConst(expr))
}

// foldCVACall folds a `X({ variant: ... })` call where X is a known cva factory
// declared in this program or an imported module. Returns ok=false when X is
// not a cva factory or the selection is not statically known.
func (b *builder) foldCVACall(call *ast.CallExpr) (string, bool) {
	id, ok := call.Callee.(*ast.Identifier)
	if !ok || b.cvaFactories == nil {
		return "", false
	}
	spec, ok := b.cvaFactories[id.Name]
	if !ok {
		return "", false
	}
	v := foldCVAValue(b.constInterp(b.localProps), spec, call.Args)
	if v.IsUnknown() {
		return "", false
	}
	return v.StringValue(), true
}

func evalConst(expr ast.Expr) string {
	if expr == nil {
		return ""
	}
	// Context-free folding uses JS semantics when the value is fully known;
	// otherwise the structural fallback below keeps identifier names and
	// expression text for JS emission.
	if v := evalConstValue(expr, nil, nil); !v.ContainsUnknown() && !v.IsFunction() {
		return jsvalue.LegacyString(v)
	}
	switch e := expr.(type) {
	case *ast.Literal:
		switch e.Kind {
		case ast.StringLit:
			return e.Value
		case ast.NumberLit:
			return e.Value
		case ast.BoolLit:
			return e.Value
		case ast.NullLit:
			return "null"
		default:
			return e.Value
		}
	case *ast.Identifier:
		return e.Name
	case *ast.UnaryExpr:
		arg := evalConst(e.Arg)
		if arg == "" {
			return ""
		}
		return unaryPrefixJS(e.Op, arg)
	case *ast.BinaryExpr:
		left := evalConst(e.Left)
		right := evalConst(e.Right)
		// If either side can't be const-evaluated, return empty -
		// partial stringification produces broken JS like " || false".
		if left == "" || right == "" {
			return ""
		}
		// Numeric binary expressions (16 / 9, width * 2) must be computed
		// numerically, not string-concatenated. Otherwise a prop like
		// ratio={16/9} resolves to the literal "16 / 9", and downstream
		// arithmetic concatenates it into garbage ("116 / 9100").
		if _, lOk := parseNumeric(left); lOk {
			if _, rOk := parseNumeric(right); rOk {
				if v := evalBinaryValue(e.Op, left, right); v != "" {
					return v
				}
			}
		}
		return left + " " + e.Op + " " + right
	case *ast.ArrayExpr:
		var parts []string
		for _, el := range e.Elements {
			parts = append(parts, constExprToJS(el))
		}
		return "[" + strings.Join(parts, ",") + "]"
	case *ast.ObjectExpr:
		var parts []string
		for _, prop := range e.Properties {
			if prop.Spread {
				continue
			}
			parts = append(parts, prop.Key+`:`+constExprToJS(prop.Value))
		}
		return "{" + strings.Join(parts, ",") + "}"
	case *ast.ConditionalExpr:
		// Ternary: evaluate test, return appropriate branch
		test := evalConst(e.Test)
		if test == "" {
			// Can't evaluate test - variables or props not resolvable.
			// Return empty instead of falling through to alternate (which
			// could be null literal, producing "null" text in output).
			return ""
		}
		if test != "false" && test != "null" && test != "undefined" && test != "0" {
			return evalConst(e.Consequent)
		}
		return evalConst(e.Alternate)
	case *ast.TemplateExpr:
		var b strings.Builder
		for i, raw := range e.Raw {
			b.WriteString(escape.TemplateRaw(raw))
			if i < len(e.Parts) {
				b.WriteString(evalConst(e.Parts[i]))
			}
		}
		return b.String()
	default:
		return ""
	}
}

// constExprToJS renders a constant expression as valid JavaScript source for
// embedding in object/array literals. Unlike evalConst (which returns bare
// values - unquoted strings, raw identifiers), this re-quotes string literals
// at any nesting depth so the produced text is executable JS (e.g. a nested
// object prop like {title:"Docs"} survives as a real object, not the invalid
// {title:Docs}).
func constExprToJS(expr ast.Expr) string {
	if expr == nil {
		return "null"
	}
	switch e := expr.(type) {
	case *ast.Literal:
		switch e.Kind {
		case ast.StringLit:
			return "'" + escape.JSString(e.Value) + "'"
		case ast.NullLit:
			return "null"
		default:
			// numbers, booleans
			return e.Value
		}
	case *ast.ArrayExpr:
		var parts []string
		for _, el := range e.Elements {
			parts = append(parts, constExprToJS(el))
		}
		return "[" + strings.Join(parts, ",") + "]"
	case *ast.ObjectExpr:
		var parts []string
		for _, prop := range e.Properties {
			if prop.Spread {
				continue
			}
			parts = append(parts, jsObjectKey(prop.Key)+":"+constExprToJS(prop.Value))
		}
		return "{" + strings.Join(parts, ",") + "}"
	case *ast.UnaryExpr:
		return unaryPrefixJS(e.Op, constExprToJS(e.Arg))
	case *ast.TemplateExpr:
		// Preserve the template literal as valid JS with interpolated parts.
		var b strings.Builder
		b.WriteByte('`')
		for i, raw := range e.Raw {
			b.WriteString(raw)
			if i < len(e.Parts) {
				b.WriteString("${")
				b.WriteString(constExprToJS(e.Parts[i]))
				b.WriteByte('}')
			}
		}
		b.WriteByte('`')
		return b.String()
	case *ast.Identifier:
		// Identifier references can't be const-folded into a literal; fall
		// back to evalConst so a bare reference (e.g. a hoisted const name)
		// resolves if possible, otherwise the name itself.
		if v := evalConst(expr); v != "" {
			return v
		}
		return e.Name
	default:
		// Preserve any other expression as valid JS source rather than dropping
		// it (which previously produced `[]`/`{}` with missing elements).
		if js := generateExprJS(expr, nil); js != "" {
			return js
		}
		return evalConst(expr)
	}
}

// evalConstWithSignals evaluates a constant expression, resolving signal reads
// (name()) and bare signal identifiers to their initial values, and returns it
// in the legacy string form. Used to compute SSR initial values of dynamic
// attributes like data-state={open() ? "open" : "closed"}. props maps component
// parameter names (and folded locals) to their build-time values.
func evalConstWithSignals(expr ast.Expr, signals map[string]ast.Expr, props valueMap) string {
	if expr == nil {
		return ""
	}
	return legacyConstString(expr, evalConstValue(expr, signals, props))
}

// resolveMemberChainValue resolves a member-access chain rooted at `props`
// (e.g. props.options.accent, props.tags.length) against the props map.
// ok=false when the chain is not rooted at props or does not fully fold.
func resolveMemberChainValue(expr ast.Expr, props valueMap) (string, bool) {
	cur := expr
	depth := 0
	for {
		mem, ok := cur.(*ast.MemberExpr)
		if !ok {
			break
		}
		depth++
		cur = mem.Object
	}
	if id, ok := cur.(*ast.Identifier); !ok || id.Name != "props" || depth == 0 {
		return "", false
	}
	v := evalConstValue(expr, nil, props)
	if v.ContainsUnknown() {
		return "", false
	}
	return legacyConstString(expr, v), true
}

// operandLeaks reports whether expr depends on a value that is not known at
// build time (an unresolved identifier, an unsupported construct, ...).
func operandLeaks(expr ast.Expr, signals map[string]ast.Expr, props valueMap) bool {
	if expr == nil {
		return false
	}
	return evalConstValue(expr, signals, props).ContainsUnknown()
}

// updateTextSlotInitials walks slot children and updates TextSlot.Initial
// values from the resolved signal declarations.
func updateTextSlotInitials(children []SlotNode, signals []SignalDecl) {
	signalMap := make(map[string]string)
	for _, sig := range signals {
		if sig.Initial != "" {
			initial := sig.Initial
			if !sig.IsString {
				// Non-string initials (booleans, null) render with React child
				// semantics: `{flag()}` with flag=false shows nothing.
				if t, ok := childText(jsvalue.FromLegacy(initial)); ok {
					initial = t
				}
			}
			signalMap[sig.Name] = initial
		}
	}
	for _, child := range children {
		switch c := child.(type) {
		case *TextSlot:
			if initial, ok := signalMap[c.Signal.Name]; ok {
				c.Initial = initial
			}
		case *ComponentSlot:
			if c.Component != nil {
				updateTextSlotInitials(c.Component.Children, signals)
			}
		}
	}
}

// SSR-evaluated child components

// extractPropsAST extracts raw AST expressions from JSX attributes.
func extractPropsAST(el *ast.JSXElement) map[string]ast.Expr {
	props := make(map[string]ast.Expr)
	for _, attr := range el.Opening.Attributes {
		if attr.Spread || isShowIfAttr(attr.Name) || isReactDirectiveAttr(attr.Name) {
			continue
		}
		// A bare attribute (`<Child disabled />`) means boolean true; keep it so
		// rest props and `{...props}` forwarding preserve it.
		if attr.Value == nil {
			props[attr.Name] = &ast.Literal{Kind: ast.BoolLit, Value: "true"}
			continue
		}
		props[attr.Name] = attr.Value
	}
	return props
}

// expandSpreadAttrs replaces `{...name}` spreads that resolve to a known rest
// parameter with the call-site attributes it collected. Explicitly-written
// attributes win over spread-provided ones, matching JSX semantics. Spreads
// that cannot be resolved are left untouched (and later skipped).
// The second return value reports whether a rest spread was expanded, so the
// caller can flow call-site children through it (React includes `children` in
// the props object, so `<Comp {...props}/>` forwards them).
func (b *builder) expandSpreadAttrs(attrs []*ast.JSXAttr) ([]*ast.JSXAttr, bool) {
	var out []*ast.JSXAttr
	expanded := false
	// Track which attribute names were written explicitly so spreads don't
	// override them.
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
		rest := b.restAttrs(attr.Value)
		if rest == nil {
			out = append(out, attr)
			continue
		}
		expanded = true
		for _, name := range sortedKeysExpr(rest) {
			if explicit[name] {
				continue
			}
			out = append(out, &ast.JSXAttr{Position: attr.Position, Name: name, Value: rest[name]})
		}
	}
	return out, expanded
}

// restAttrs resolves a spread value expression to the attribute map of a known
// rest parameter, or nil.
func (b *builder) restAttrs(expr ast.Expr) map[string]ast.Expr {
	if b.restProps == nil {
		return nil
	}
	switch e := expr.(type) {
	case *ast.Identifier:
		return b.restProps[e.Name]
	case *ast.MemberExpr:
		// `{...props.rest}` style is not tracked; only top-level rest names.
		return nil
	}
	return nil
}

// sortedKeysExpr returns map keys in sorted order for deterministic output.
func sortedKeysExpr(m map[string]ast.Expr) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// resolveTagAlias resolves a JSX tag name that is bound to a local variable to
// the concrete tag it aliases, when the binding folds at build time:
//
//	const Comp = asChild ? Slot : "button";  <Comp/>
//
// Returns "" when the name is not such an alias or the condition depends on a
// runtime signal (in which case the component stays unresolved).
func (b *builder) resolveTagAlias(name string) string {
	// Only uppercase tags are component references; lowercase tags are always
	// intrinsic HTML elements and must never alias a same-named local.
	if len(name) == 0 || name[0] < 'A' || name[0] > 'Z' {
		return ""
	}
	if b.localFnBody == nil {
		return ""
	}
	for _, stmt := range b.localFnBody {
		vs, ok := stmt.(*ast.VarStmt)
		if !ok {
			continue
		}
		for _, decl := range vs.Decls {
			if decl.Name != name || decl.Init == nil {
				continue
			}
			return b.foldTagExpr(decl.Init)
		}
	}
	return ""
}

// foldTagExpr resolves a tag expression to a concrete tag name when it is a
// literal tag string or a conditional whose test folds. Slot/forwardRef-style
// wrappers fold to themselves. Returns "" when unresolvable.
func (b *builder) foldTagExpr(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.Literal:
		if e.Kind == ast.StringLit {
			return e.Value
		}
		return ""
	case *ast.Identifier:
		// A component reference (Slot, Button, ...) is already a valid tag.
		return e.Name
	case *ast.ConditionalExpr:
		testV := b.evalValue(e.Test)
		if testV.IsUnknown() {
			return ""
		}
		if testV.ToBoolean() {
			return b.foldTagExpr(e.Consequent)
		}
		return b.foldTagExpr(e.Alternate)
	case *ast.TypeAssertion:
		return b.foldTagExpr(e.Expr)
	}
	return ""
}

// paramDefaultValue returns the build-time value for an omitted parameter: its
// default expression when the pattern declares one (`asChild = false`), else
// "undefined".
func (b *builder) paramDefaultValue(fn *ast.FnDecl, name string) jsvalue.Value {
	for _, p := range fn.Params {
		if p == nil || p.Name == "" {
			continue
		}
		if p.Name == name && p.Default != nil {
			return b.evalValue(p.Default)
		}
		// Destructured member with a default: `{ asChild = false }`.
		if p.Name == "{...}" {
			if def := destructuredParamDefault(p.Pattern, name); def != nil {
				return b.evalValue(def)
			}
		}
	}
	return jsvalue.Undefined()
}

// destructuredParamDefault returns the default expression for a binding in a
// destructuring pattern (`{ asChild = false }`), or nil.
func destructuredParamDefault(pattern, name string) ast.Expr {
	p := pattern
	if len(p) >= 2 && p[0] == '{' && p[len(p)-1] == '}' {
		p = p[1 : len(p)-1]
	}
	for _, part := range splitTopLevel(p) {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		// `key: value = default` or `name = default`.
		if colon := indexTopLevel(part, ":"); colon >= 0 {
			value := strings.TrimSpace(part[colon+1:])
			if eq := strings.Index(value, "="); eq >= 0 {
				bound := strings.TrimSpace(value[:eq])
				if bound == name {
					return parseExprFragment(strings.TrimSpace(value[eq+1:]))
				}
			}
			continue
		}
		if eq := strings.Index(part, "="); eq >= 0 {
			bound := strings.TrimSpace(part[:eq])
			if bound == name {
				return parseExprFragment(strings.TrimSpace(part[eq+1:]))
			}
		}
	}
	return nil
}

// parseExprFragment parses a small expression source fragment back to an AST.
func parseExprFragment(src string) ast.Expr {
	if src == "" {
		return nil
	}
	prog := parseExprAsProgram(src)
	if prog == nil {
		return nil
	}
	for _, stmt := range prog.Body {
		if es, ok := stmt.(*ast.ExprStmt); ok {
			return es.Expression
		}
	}
	return nil
}

// restProps builds the attrs a component's rest parameter collects (those not
// bound by a named/destructured parameter), or nil when it has no rest param.
func restProps(fn *ast.FnDecl, attrs map[string]ast.Expr) map[string]ast.Expr {
	if fnRestParamName(fn) == "" {
		return nil
	}
	bound := make(map[string]bool)
	for _, n := range extractParamNames(fn) {
		bound[n] = true
	}
	rest := make(map[string]ast.Expr)
	for name, expr := range attrs {
		if !bound[name] {
			rest[name] = expr
		}
	}
	return rest
}

// restPropsFor builds the rest-parameter map (name -> collected attrs) for a
// component given its call-site attrs, or nil when it has no rest parameter.
func (b *builder) restPropsFor(restName string, fn *ast.FnDecl, attrs map[string]ast.Expr) map[string]map[string]ast.Expr {
	if restName == "" {
		return nil
	}
	rest := restProps(fn, attrs)
	if rest == nil {
		rest = map[string]ast.Expr{}
	}
	return map[string]map[string]ast.Expr{restName: rest}
}

// propBindingsToLocalProps builds the child component's local-prop scope from
// its call-site attrs: each parameter name resolves to the evaluated call-site
// value (or "undefined" when omitted). Used when walking a signal-less
// component's own return JSX so className={cn(...)} etc. fold correctly.
func (b *builder) propBindingsToLocalProps(fn *ast.FnDecl, attrs map[string]ast.Expr, parent valueMap) valueMap {
	out := make(valueMap)
	for _, name := range extractParamNames(fn) {
		if expr, ok := attrs[name]; ok {
			out[name] = b.resolveBindingValue(expr, parent)
		} else {
			out[name] = jsvalue.Undefined()
		}
	}
	return out
}

// extractParamNames returns the parameter names of a function declaration. For
// destructured object/array patterns (param.Name is "{...}"/"[...]") it returns
// the bound names contained in the pattern so call-site attributes can be
// matched to the locals they bind.
func extractParamNames(fn *ast.FnDecl) []string {
	var names []string
	for _, p := range fn.Params {
		switch p.Name {
		case "{...}":
			names = append(names, destructuredParamNames(p.Pattern)...)
		case "[...]":
			names = append(names, destructuredParamNames(p.Pattern)...)
		default:
			if p.Name != "" {
				names = append(names, p.Name)
			}
		}
	}
	return names
}

// destructuredRestName returns the trailing rest binding name of an object
// pattern (e.g. `props` in `{ a, ...props }`), or "" when there is none.
func destructuredRestName(pattern string) string {
	p := pattern
	if len(p) >= 2 && ((p[0] == '{' && p[len(p)-1] == '}') || (p[0] == '[' && p[len(p)-1] == ']')) {
		p = p[1 : len(p)-1]
	}
	for _, part := range splitTopLevel(p) {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(part, "...") {
			name := strings.TrimSpace(part[3:])
			if eq := strings.Index(name, "="); eq >= 0 {
				name = strings.TrimSpace(name[:eq])
			}
			return name
		}
	}
	return ""
}

// fnRestParamName returns the rest-parameter name of a component function
// (destructured `{...props}` or a bare `...props`), or "".
func fnRestParamName(fn *ast.FnDecl) string {
	if fn == nil {
		return ""
	}
	for _, p := range fn.Params {
		if p.IsRest && p.Name != "" {
			return p.Name
		}
		if p.Name == "{...}" {
			if name := destructuredRestName(p.Pattern); name != "" {
				return name
			}
		}
	}
	return ""
}

// destructuredParamNames extracts the binding identifiers from a destructuring
// pattern source string (e.g. "{ className, size, ...rest }" -> [className,
// size, rest]). Keys with a rename (`{ a: b }`) bind `b`. It is a lightweight
// scan over the pattern text produced by the parser.
func destructuredParamNames(pattern string) []string {
	var names []string
	p := pattern
	// Strip outer braces/brackets.
	if len(p) >= 2 && ((p[0] == '{' && p[len(p)-1] == '}') || (p[0] == '[' && p[len(p)-1] == ']')) {
		p = p[1 : len(p)-1]
	}
	for _, part := range splitTopLevel(p) {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if strings.HasPrefix(part, "...") {
			part = strings.TrimSpace(part[3:])
		}
		// `key: value` (rename) binds value; nested patterns recurse.
		if colon := indexTopLevel(part, ":"); colon >= 0 {
			value := strings.TrimSpace(part[colon+1:])
			if strings.HasPrefix(value, "{") || strings.HasPrefix(value, "[") {
				names = append(names, destructuredParamNames(value)...)
			} else if eq := strings.Index(value, "="); eq >= 0 {
				names = append(names, strings.TrimSpace(value[:eq]))
			} else {
				names = append(names, value)
			}
			continue
		}
		// Nested pattern without rename.
		if strings.HasPrefix(part, "{") || strings.HasPrefix(part, "[") {
			names = append(names, destructuredParamNames(part)...)
			continue
		}
		// Default value (`x = 1`).
		if eq := strings.Index(part, "="); eq >= 0 {
			part = strings.TrimSpace(part[:eq])
		}
		if part != "" {
			names = append(names, part)
		}
	}
	return names
}

// splitTopLevel splits a pattern body on commas that are not nested inside
// braces, brackets, or parentheses.
func splitTopLevel(s string) []string {
	var parts []string
	depth := 0
	start := 0
	for i, r := range s {
		switch r {
		case '{', '[', '(':
			depth++
		case '}', ']', ')':
			if depth > 0 {
				depth--
			}
		case ',':
			if depth == 0 {
				parts = append(parts, s[start:i])
				start = i + 1
			}
		}
	}
	parts = append(parts, s[start:])
	return parts
}

// indexTopLevel returns the index of the first `:` at nesting depth 0, or -1.
func indexTopLevel(s, target string) int {
	depth := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '{', '[', '(':
			depth++
		case '}', ']', ')':
			if depth > 0 {
				depth--
			}
		default:
			if depth == 0 && strings.HasPrefix(s[i:], target) {
				return i
			}
		}
	}
	return -1
}

// buildPropBindings maps parameter names to evaluated prop values, resolving
// each attribute expression against the current component's build-time scope
// (its local props + signals) so `items={props.tocItems}` passed from a client
// parent to a pure-prop static child still resolves to the real value (SSREval
// bindings otherwise see an empty "props"). Handles two patterns:
//  1. Destructured params: function({ items, label }) with <Comp items={...} label={...} />
//  2. Single props object: function(props) with <Comp items={...} label={...} />
//     In this case, we flatten the attrs into top-level bindings so the SSREval
//     can resolve `props.breadcrumbs` by treating "breadcrumbs" as a binding.
func (b *builder) buildPropBindings(paramNames []string, attrs map[string]ast.Expr, fn *ast.FnDecl) map[string]jsvalue.Value {
	bindings := make(map[string]jsvalue.Value)
	props := b.localProps
	if props == nil {
		props = make(valueMap)
	}
	if len(paramNames) == 1 && paramNames[0] == "props" {
		// Single props object pattern evaluate each attribute value
		// and store them as top-level bindings. The SSREval resolves
		// identifiers like "breadcrumbs" from the attrs directly.
		for name, expr := range attrs {
			bindings[name] = b.resolveBindingValue(expr, props)
		}
		return bindings
	}
	// Destructured params pattern. An omitted prop resolves to its declared
	// default (`asChild = false`) or "undefined" so fold decisions match JS.
	for _, name := range paramNames {
		if expr, ok := attrs[name]; ok {
			bindings[name] = b.resolveBindingValue(expr, props)
			continue
		}
		if fn != nil {
			bindings[name] = b.paramDefaultValue(fn, name)
		}
	}
	return bindings
}

// resolveBindingValue evaluates a call-site prop expression to its build-time
// value. It resolves through the enclosing component's local props (so a
// `props.foo` passed from a parent resolves), plus signal reads to their
// initial values, falling back to plain const evaluation.
func (b *builder) resolveBindingValue(expr ast.Expr, props valueMap) jsvalue.Value {
	if expr == nil {
		return jsvalue.Undefined()
	}
	return b.constInterp(props).Eval(expr)
}

func (b *builder) buildCallSiteChildSlots(children []ast.JSXChild, parentID string) []SlotNode {
	var result []SlotNode
	for _, child := range children {
		switch c := child.(type) {
		case *ast.JSXElementChild:
			result = append(result, b.buildSlotNodes(c.Element, parentID)...)
		case *ast.JSXFragmentChild:
			result = append(result, b.buildFragmentSlots(c.Fragment, parentID)...)
		case *ast.JSXExprContainer:
			result = append(result, b.buildExprContainerChildren(c, parentID)...)
		case *ast.JSXText:
			if c.Value != "" {
				result = append(result, &StaticHTML{HTML: c.Value})
			}
		}
	}
	return result
}

// isVoidElement returns true for HTML void elements that can be self-closing.
// Non-void elements like <div/> must be emitted as <div></div>.
func isVoidElement(tag string) bool {
	switch tag {
	case "area", "base", "br", "col", "embed", "hr", "img", "input",
		"link", "meta", "param", "source", "track", "wbr":
		return true
	}
	return false
}

// handlerBodiesReferenceProps checks if any handler body references `props.`
func handlerBodiesReferenceProps(handlers []HandlerDecl) bool {
	for _, h := range handlers {
		if strings.Contains(h.Body, "props.") {
			return true
		}
	}
	return false
}

// handlersOrLocalsReferenceProps reports whether any handler body OR any local
// function referenced by a handler accesses props. Local functions (e.g.
// handleChange) may reference props.X even though the handler body that calls
// them only contains the function name.
func handlersOrLocalsReferenceProps(handlers []HandlerDecl, body []ast.Stmt) bool {
	if handlerBodiesReferenceProps(handlers) {
		return true
	}
	locals := collectHandlerLocalFunctions(handlers, body)
	for _, fn := range locals {
		for _, stmt := range fn.Body {
			if stmtReferencesProps(stmt) {
				return true
			}
		}
	}
	return false
}

// compiledRefsProps reports whether any compiled JS string references `props`.
// Effects/memos that read props.X (e.g. controlled-component sync effects like
// `if (props.checked !== undefined) setChecked(props.checked)`) require the
// parent-scope props registration so `props` resolves at runtime.
func compiledRefsProps(js []string) bool {
	for _, s := range js {
		if codeContainsIdent(s, "props") {
			return true
		}
	}
	return false
}

// signalsReferenceProps reports whether any signal initializer that is emitted
// verbatim (RawInit) references `props`. Signal inits like
// `createSignal(props.defaultOpen !== false)` must resolve `props` at runtime,
// so the props registration must be hoisted even when no handler/effect/memo
// touches props.
func signalsReferenceProps(signals []SignalDecl) bool {
	for _, s := range signals {
		if codeContainsIdent(s.RawInit, "props") {
			return true
		}
	}
	return false
}

// buildChildPropsRegDecl generates a `__krate_props["<id>"]={...}` registration
// that must be evaluated in the PARENT component's scope. Function-valued props
// (local function references like closeNav) are emitted as the bare name so they
// resolve to the parent's live function. Const props that read the parent's own
// props (`items={props.sidebarItems}`) are substituted with the parent's resolved
// literal values so the registration never references an unbound `props`
// (a top-of-tree client component like a docs layout has no registry entry of its
// own). Signal reads (checked={checked1()}) remain live expressions.
func (b *builder) buildChildPropsRegDecl(id string, props map[string]ast.Expr) string {
	var sb strings.Builder
	sb.WriteString("__krate_props[")
	sb.WriteString(strconv.Quote(id))
	sb.WriteString("]={")
	if len(props) == 0 {
		sb.WriteByte('}')
		return sb.String()
	}
	// Keys are emitted in sorted order so the registration (and the hashed
	// hydration bundle) is byte-identical across builds.
	for i, name := range sortedKeysExpr(props) {
		expr := props[name]
		if i > 0 {
			sb.WriteByte(',')
		}
		sb.WriteString(jsObjectKey(name))
		sb.WriteByte(':')
		val := b.renderChildPropValue(expr)
		if val == "" {
			val = "undefined"
		}
		sb.WriteString(val)
	}
	sb.WriteByte('}')
	return sb.String()
}

// renderChildPropValue renders a call-site prop value to self-contained JS for a
// __krate_props registration in the current (parent) scope. `props.X` reads that
// resolve to a const value in the parent are inlined as that literal; everything
// else (function identifiers, signal reads, parent-scope locals) is emitted as-is
// since the registration runs in the parent's scope.
func (b *builder) renderChildPropValue(expr ast.Expr) string {
	if expr == nil {
		return ""
	}
	// A module-level constant (e.g. `const items = [{...}]`) has no runtime
	// binding in the hydration IIFE, so inline its value as valid JS source
	// instead of emitting the bare identifier (which would throw ReferenceError).
	// The initializer AST is rendered (not the folded string) so strings stay
	// quoted and arrays/objects stay real literals.
	if id, ok := expr.(*ast.Identifier); ok {
		if u, ok := b.actionURLs[id.Name]; ok {
			return "'" + escape.JSString(u) + "'"
		}
		if init, ok := b.moduleConstInits[id.Name]; ok && init != nil {
			if js := generateExprJS(init, b.sigMap()); js != "" {
				return js
			}
		}
	}
	resolved := resolvePropsMembers(expr, b.localProps, b.sigMap())
	if resolved != nil {
		return generateExprJS(resolved, b.sigMap())
	}
	return generateExprJS(expr, b.sigMap())
}

// resolvePropsMembers rewrites MemberExpr nodes of the form `props.X` into the
// resolved literal AST for X when X is present in the given props map. Returns
// nil (leaving the expression untouched) when no props.X member is resolvable.
func resolvePropsMembers(expr ast.Expr, props valueMap, signals map[string]ast.Expr) ast.Expr {
	// Fast path: the whole expression is props.X.
	if mem, ok := expr.(*ast.MemberExpr); ok {
		if id, ok := mem.Object.(*ast.Identifier); ok && id.Name == "props" {
			if pid, ok := mem.Property.(*ast.Identifier); ok {
				if v, ok := props[pid.Name]; ok {
					if lit := jsvalue.ToAST(v); lit != nil {
						return lit
					}
				}
			}
		}
	}
	sub := &propsMemberSubstituter{props: props, signals: signals}
	return sub.sub(expr)
}

type propsMemberSubstituter struct {
	props   valueMap
	signals map[string]ast.Expr
}

func (s *propsMemberSubstituter) sub(expr ast.Expr) ast.Expr {
	if expr == nil {
		return nil
	}
	switch e := expr.(type) {
	case *ast.MemberExpr:
		if id, ok := e.Object.(*ast.Identifier); ok && id.Name == "props" {
			if pid, ok := e.Property.(*ast.Identifier); ok {
				if v, ok := s.props[pid.Name]; ok {
					if lit := jsvalue.ToAST(v); lit != nil {
						return lit
					}
				}
			}
		}
		return &ast.MemberExpr{Position: e.Position, Object: s.sub(e.Object), Property: e.Property, Computed: e.Computed, Optional: e.Optional}
	case *ast.BinaryExpr:
		return &ast.BinaryExpr{Position: e.Position, Left: s.sub(e.Left), Op: e.Op, Right: s.sub(e.Right)}
	case *ast.ConditionalExpr:
		return &ast.ConditionalExpr{Position: e.Position, Test: s.sub(e.Test), Consequent: s.sub(e.Consequent), Alternate: s.sub(e.Alternate)}
	case *ast.UnaryExpr:
		return &ast.UnaryExpr{Position: e.Position, Op: e.Op, Arg: s.sub(e.Arg), Postfix: e.Postfix}
	case *ast.CallExpr:
		return &ast.CallExpr{Position: e.Position, Callee: s.sub(e.Callee), Args: subExprListGeneric(s, e.Args)}
	case *ast.ArrayExpr:
		return &ast.ArrayExpr{Position: e.Position, Elements: subExprListGeneric(s, e.Elements)}
	case *ast.ObjectExpr:
		out := make([]*ast.ObjectProp, len(e.Properties))
		for i, p := range e.Properties {
			out[i] = &ast.ObjectProp{Key: p.Key, Value: s.sub(p.Value), Shorthand: p.Shorthand, Spread: p.Spread, Method: p.Method}
		}
		return &ast.ObjectExpr{Position: e.Position, Properties: out}
	case *ast.TemplateExpr:
		return &ast.TemplateExpr{Position: e.Position, Parts: subExprListGeneric(s, e.Parts), Raw: e.Raw}
	default:
		return expr
	}
}

func subExprListGeneric(s *propsMemberSubstituter, list []ast.Expr) []ast.Expr {
	out := make([]ast.Expr, len(list))
	for i, e := range list {
		out[i] = s.sub(e)
	}
	return out
}
