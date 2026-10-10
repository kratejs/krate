package irtree

import (
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/escape"
)

// SSR expression helpers

// collectModuleConsts scans the module-level (top-of-file) variable
// declarations and constant-folds those with statically-evaluable initializers
// into a name -> value map. This lets JSX text like {hexNum} resolve to 255 at
// build time instead of leaking the identifier name as literal text. Later
// declarations may reference earlier ones (e.g. const c = a + b).
func collectModuleConsts(prog *ast.Program) map[string]string {
	consts := make(map[string]string)
	if prog == nil {
		return consts
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
			if decl.Name == "" || decl.Init == nil {
				continue
			}
			if val := evalConstWithSignals(decl.Init, nil, consts); val != "" {
				consts[decl.Name] = val
			}
		}
	}
	return consts
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

// evalConst evaluates a constant expression to a string value.
// evalAttrValue resolves a JSX attribute value at build time, folding class
// helpers (cn/clsx) and cva variant calls in addition to ordinary constants.
// It is the single entry point for static attribute emission and binding
// initials so className folds consistently across render paths.
func (b *builder) evalAttrValue(expr ast.Expr) string {
	if expr == nil {
		return ""
	}
	if call, ok := expr.(*ast.CallExpr); ok {
		if v, ok := b.foldCVACall(call); ok {
			return v
		}
	}
	return evalConstWithSignals(expr, b.sigMap(), b.localProps)
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
	selection := map[string]string{}
	extra := ""
	if len(call.Args) >= 1 {
		obj, ok := call.Args[0].(*ast.ObjectExpr)
		if !ok {
			return "", false
		}
		for _, prop := range obj.Properties {
			if prop == nil || prop.Spread || prop.Key == "" {
				continue
			}
			v := evalConstWithSignals(prop.Value, b.sigMap(), b.localProps)
			if prop.Value != nil && operandLeaks(prop.Value, b.sigMap(), b.localProps) {
				return "", false
			}
			if v == "undefined" || v == "null" || v == "" {
				continue
			}
			if prop.Key == "class" || prop.Key == "className" {
				extra = v
				continue
			}
			selection[prop.Key] = v
		}
	}
	return spec.Fold(selection, extra), true
}

// constArrayItems parses a const-folded array value into its element strings.
// Values arrive either as the internal \x1f-joined form (arrays built in
// evaluated code) or as a JS array-literal source string (arrays from props /
// consts, e.g. `['a','b']`). String elements are unquoted so `arr.join(", ")`
// yields the element text rather than its quoted source form.
func constArrayItems(v string) ([]string, bool) {
	if v == "" {
		return nil, true
	}
	if strings.Contains(v, "\x1f") {
		return strings.Split(v, "\x1f"), true
	}
	if !strings.HasPrefix(strings.TrimSpace(v), "[") {
		return nil, false
	}
	arr, ok := constSourceToAST(v).(*ast.ArrayExpr)
	if !ok {
		return nil, false
	}
	out := make([]string, 0, len(arr.Elements))
	for _, el := range arr.Elements {
		out = append(out, evalConst(el))
	}
	return out, true
}

// sliceConstBounds resolves JS Array/String.prototype.slice arguments
// (negative offsets and an omitted end) to a clamped [start, end) range.
func sliceConstBounds(start, end int, hasEnd bool, n int) (int, int) {
	if start < 0 {
		start += n
		if start < 0 {
			start = 0
		}
	}
	if start > n {
		start = n
	}
	if !hasEnd {
		end = n
	}
	if end < 0 {
		end += n
		if end < 0 {
			end = 0
		}
	}
	if end > n {
		end = n
	}
	if end < start {
		end = start
	}
	return start, end
}

func evalConst(expr ast.Expr) string {
	if expr == nil {
		return ""
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
		return e.Op + arg
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
			b.WriteString(raw)
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
		return e.Op + constExprToJS(e.Arg)
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
// (name()) and bare signal identifiers to their initial values. Used to compute
// the SSR initial value of dynamic attributes like data-state={open() ? "open" : "closed"}.
// props maps component parameter names to pre-resolved call-site values so
// props.X reads and bare param identifiers resolve.
func evalConstWithSignals(expr ast.Expr, signals map[string]ast.Expr, props map[string]string) string {
	if expr == nil {
		return ""
	}
	switch e := expr.(type) {
	case *ast.Literal:
		if e.Kind == ast.NullLit {
			return ""
		}
		if e.Kind == ast.StringLit {
			return e.Value
		}
		return e.Value
	case *ast.CallExpr:
		// Array.prototype.join: fold `arr.join(sep)` to a string when `arr` is a
		// statically-known array (an array literal, a prop binding such as
		// `props.authors`, or a signal initial). Without this the join only
		// resolves during hydration, so a client component's build-time static
		// HTML renders "" and diverges from the hydrated output.
		if mem, ok := e.Callee.(*ast.MemberExpr); ok {
			if prop, ok := mem.Property.(*ast.Identifier); ok && prop.Name == "join" {
				arr := evalConstWithSignals(mem.Object, signals, props)
				sep := ","
				if len(e.Args) == 1 {
					if s := evalConstWithSignals(e.Args[0], signals, props); s != "" {
						sep = s
					}
				}
				if items, ok := constArrayItems(arr); ok {
					return strings.Join(items, sep)
				}
			}
			// String/Array.prototype.slice: fold `x.slice(a, b?)` so static text
			// such as `{lastUpdated.slice(0, 10)}` renders at build time instead
			// of collapsing to "" (it never re-renders during hydration).
			if prop, ok := mem.Property.(*ast.Identifier); ok && prop.Name == "slice" {
				obj := evalConstWithSignals(mem.Object, signals, props)
				start, end, hasEnd := 0, 0, false
				if len(e.Args) >= 1 {
					if n, err := strconv.Atoi(strings.TrimSpace(evalConstWithSignals(e.Args[0], signals, props))); err == nil {
						start = n
					}
				}
				if len(e.Args) >= 2 {
					if n, err := strconv.Atoi(strings.TrimSpace(evalConstWithSignals(e.Args[1], signals, props))); err == nil {
						end = n
						hasEnd = true
					}
				}
				if items, ok := constArrayItems(obj); ok {
					s, en := sliceConstBounds(start, end, hasEnd, len(items))
					return strings.Join(items[s:en], "\x1f")
				}
				runes := []rune(obj)
				s, en := sliceConstBounds(start, end, hasEnd, len(runes))
				return string(runes[s:en])
			}
		}
		// `Ctx.useContext()` folds to the context default (no Provider exists
		// during SSG). The default was seeded under a reserved key above.
		if mem, ok := e.Callee.(*ast.MemberExpr); ok {
			if prop, ok := mem.Property.(*ast.Identifier); ok && prop.Name == "useContext" {
				if id, ok := mem.Object.(*ast.Identifier); ok {
					if v, found := props[contextMarkerPrefix+id.Name]; found {
						return v
					}
				}
			}
		}
		if id, ok := e.Callee.(*ast.Identifier); ok {
			// `useContext(Ctx)` (the @krate/runtime form) folds to the default.
			if id.Name == "useContext" && len(e.Args) == 1 {
				if arg, ok := e.Args[0].(*ast.Identifier); ok {
					if v, found := props[contextMarkerPrefix+arg.Name]; found {
						return v
					}
				}
			}
			if initial, ok := signals[id.Name]; ok {
				if isResourceSentinel(initial) {
					return "" // resource getter: no data resolved during SSR
				}
				return evalConstWithSignals(initial, signals, props)
			}
			if id.Name == "String" && len(e.Args) == 1 {
				return evalConstWithSignals(e.Args[0], signals, props)
			}
			// Pure class helpers (cn/clsx) fold to a literal class string when
			// every argument is statically known, so className becomes static
			// HTML with no hydration binding.
			if id.Name == "cn" || id.Name == "clsx" {
				if v, ok := foldClassCall(e.Args, signals, props); ok {
					return v
				}
			}
		}
		return ""
	case *ast.Identifier:
		if e.Name == "__krate_resource__" {
			return "" // resource sentinel: no data resolved during SSR
		}
		if initial, ok := signals[e.Name]; ok {
			return evalConstWithSignals(initial, signals, props)
		}
		if v, ok := props[e.Name]; ok {
			return v
		}
		return e.Name
	case *ast.ConditionalExpr:
		test := evalConstWithSignals(e.Test, signals, props)
		if test == "false" || test == "null" || test == "undefined" || test == "0" || test == "" {
			return evalConstWithSignals(e.Alternate, signals, props)
		}
		return evalConstWithSignals(e.Consequent, signals, props)
	case *ast.BinaryExpr:
		switch e.Op {
		case "||":
			left := evalConstWithSignals(e.Left, signals, props)
			if isTruthyValue(left) {
				return left
			}
			return evalConstWithSignals(e.Right, signals, props)
		case "&&":
			left := evalConstWithSignals(e.Left, signals, props)
			if !isTruthyValue(left) {
				return left
			}
			return evalConstWithSignals(e.Right, signals, props)
		case "==", "===", "!=", "!==":
			lVal := evalConstWithSignals(e.Left, signals, props)
			rVal := evalConstWithSignals(e.Right, signals, props)
			// A literal operand is known even when its resolved value is "".
			// A non-empty resolved value is also known. Only treat truly
			// unresolvable operands (unresolved identifier, call, etc.) as unknown.
			_, lLit := e.Left.(*ast.Literal)
			_, rLit := e.Right.(*ast.Literal)
			if !lLit && lVal == "" {
				return ""
			}
			if !rLit && rVal == "" {
				return ""
			}
			switch e.Op {
			case "==", "===":
				return strconv.FormatBool(lVal == rVal)
			default:
				return strconv.FormatBool(lVal != rVal)
			}
		default:
			left := evalConstWithSignals(e.Left, signals, props)
			right := evalConstWithSignals(e.Right, signals, props)
			if operandLeaks(e.Left, signals, props) || operandLeaks(e.Right, signals, props) {
				return ""
			}
			return evalBinaryValue(e.Op, left, right)
		}
	case *ast.UnaryExpr:
		arg := evalConstWithSignals(e.Arg, signals, props)
		if e.Op == "!" {
			// Logical negation must fold to a real boolean; returning "!true"
			// would stringify to a truthy value and invert the guard at SSR.
			// Callers only trust this value when testFullyKnown is true, so an
			// empty arg here means a resolved falsy operand (null/""), not an
			// unresolved leak.
			return strconv.FormatBool(!isTruthyValue(arg))
		}
		if arg == "" {
			return ""
		}
		return e.Op + arg
	case *ast.MemberExpr:
		if v, ok := resolveMemberChainValue(e, props); ok {
			return v
		}
		// Member reads on an object literal (e.g. a list item substituted from a
		// content-collection entry: post.data.title) fold through the literal.
		if v, ok := resolveMemberOnLiteral(e, signals, props); ok {
			return v
		}
		if id, ok := e.Object.(*ast.Identifier); ok && id.Name == "props" {
			if prop, ok := e.Property.(*ast.Identifier); ok {
				if v, ok := props[prop.Name]; ok {
					return v
				}
				// children resolve through call-site slot machinery, never here.
				if prop.Name == "children" {
					return ""
				}
				// An absent prop is a definite undefined at runtime. Return the
				// "undefined" token (not "") so comparisons fold correctly
				// (undefined !== false === true) and fallbacks via || still
				// pick the default (undefined is falsy).
				return "undefined"
			}
		} else if id, ok := e.Object.(*ast.Identifier); ok {
			if initial, ok := signals[id.Name]; ok && isResourceSentinel(initial) {
				if prop, ok := e.Property.(*ast.Identifier); ok {
					switch prop.Name {
					case "loading":
						return "true"
					case "state":
						return "unresolved"
					case "error":
						return ""
					}
				}
				return ""
			}
		}
		return ""
	case *ast.TemplateExpr:
		var buf strings.Builder
		for i, raw := range e.Raw {
			buf.WriteString(raw)
			if i < len(e.Parts) {
				buf.WriteString(evalConstWithSignals(e.Parts[i], signals, props))
			}
		}
		return buf.String()
	default:
		return evalConst(expr)
	}
}

// foldClassCall evaluates a `cn(...)`/`clsx(...)` call to a literal class
// string when every argument is a statically-known class value: a string, a
// number, a boolean/null, a template, a `&&`/ternary guard whose test folds, an
// array of class values, or an object of `{class: bool}` toggles. Returns
// ok=false when any argument cannot be resolved, leaving the call to runtime.
func foldClassCall(args []ast.Expr, signals map[string]ast.Expr, props map[string]string) (string, bool) {
	var tokens []string
	for _, arg := range args {
		vals, ok := foldClassValue(arg, signals, props)
		if !ok {
			return "", false
		}
		tokens = append(tokens, vals...)
	}
	return strings.Join(tokens, " "), true
}

// foldClassValue flattens one argument to zero or more class tokens. The
// boolean=true return means "statically known"; an empty slice is a valid
// known-empty result (e.g. a falsy guard).
func foldClassValue(expr ast.Expr, signals map[string]ast.Expr, props map[string]string) ([]string, bool) {
	switch e := expr.(type) {
	case *ast.Literal:
		switch e.Kind {
		case ast.StringLit:
			if e.Value == "" {
				return nil, true
			}
			return []string{e.Value}, true
		case ast.NumberLit:
			return []string{e.Value}, true
		default:
			// booleans/null contribute nothing.
			return nil, true
		}
	case *ast.TemplateExpr:
		v := evalConstWithSignals(expr, signals, props)
		if operandLeaks(expr, signals, props) {
			return nil, false
		}
		if v == "" {
			return nil, true
		}
		return []string{v}, true
	case *ast.ArrayExpr:
		var out []string
		for _, el := range e.Elements {
			if el == nil {
				continue
			}
			toks, ok := foldClassValue(el, signals, props)
			if !ok {
				return nil, false
			}
			out = append(out, toks...)
		}
		return out, true
	case *ast.ObjectExpr:
		var out []string
		for _, prop := range e.Properties {
			if prop == nil || prop.Spread || prop.Key == "" {
				return nil, false
			}
			v := evalConstWithSignals(prop.Value, signals, props)
			if operandLeaks(prop.Value, signals, props) {
				return nil, false
			}
			if isTruthyValue(v) {
				out = append(out, prop.Key)
			}
		}
		return out, true
	case *ast.BinaryExpr:
		if e.Op == "&&" {
			left := evalConstWithSignals(e.Left, signals, props)
			if operandLeaks(e.Left, signals, props) {
				return nil, false
			}
			if !isTruthyValue(left) {
				return nil, true
			}
			return foldClassValue(e.Right, signals, props)
		}
		// Other binary operators are not class values.
		return nil, false
	case *ast.ConditionalExpr:
		test := evalConstWithSignals(e.Test, signals, props)
		if operandLeaks(e.Test, signals, props) {
			return nil, false
		}
		if isTruthyValue(test) {
			return foldClassValue(e.Consequent, signals, props)
		}
		return foldClassValue(e.Alternate, signals, props)
	case *ast.Identifier:
		// A bare local/prop that resolves to a string literal.
		if v, ok := props[e.Name]; ok {
			if v == "" || v == "undefined" || v == "null" || v == "false" {
				return nil, true
			}
			return []string{v}, true
		}
		return nil, false
	default:
		return nil, false
	}
}

// resolveMemberChainValue resolves a member-access chain rooted at `props`
// (e.g. props.options.accent, props.options?.dark) against the props map.
// Each intermediate hop must resolve to a literal object whose property value
// can be const-folded (so `props.theme.colors.primary` works when the theme
// object is a call-site literal). Returns (value, true) when fully resolvable;
// (value, false) when the root or any hop cannot be determined.
func resolveMemberChainValue(expr ast.Expr, props map[string]string) (string, bool) {
	var names []string
	cur := expr
	for {
		mem, ok := cur.(*ast.MemberExpr)
		if !ok {
			break
		}
		pid, ok := mem.Property.(*ast.Identifier)
		if !ok {
			return "", false
		}
		names = append(names, pid.Name)
		cur = mem.Object
	}
	if len(names) == 0 {
		return "", false
	}
	id, ok := cur.(*ast.Identifier)
	if !ok || id.Name != "props" {
		return "", false
	}
	v, ok := props[names[len(names)-1]]
	if !ok {
		return "", false
	}
	// Hop through each outer member in reverse (innermost to outermost).
	for i := len(names) - 2; i >= 0; i-- {
		name := names[i]
		lit := constSourceToAST(v)
		// `.length` on a resolved array/string literal (e.g. props.tags.length,
		// props.hero.actions.length) folds to a number so guards like
		// `{props.x && props.x.length > 0 && ...}` resolve at build time.
		if name == "length" {
			if n, ok := constLength(lit); ok {
				v = strconv.Itoa(n)
				continue
			}
			return "", false
		}
		obj, ok := lit.(*ast.ObjectExpr)
		if !ok {
			return "", false
		}
		var found bool
		for _, p := range obj.Properties {
			if p.Spread || p.Key != name || p.Value == nil {
				continue
			}
			v = evalConst(p.Value)
			found = true
			break
		}
		if !found {
			return "", false
		}
	}
	return v, true
}

// resolveMemberOnLiteral folds a member chain against an object/array literal
// expression. It handles reads like `post.data.title` where `post` is an
// object literal (e.g. a list item substituted from a content-collection
// entry), plus `.length` on array literals. Returns (value, true) when the
// whole chain resolves.
func resolveMemberOnLiteral(expr *ast.MemberExpr, signals map[string]ast.Expr, props map[string]string) (string, bool) {
	// Collect the property chain (innermost first) and the base expression.
	var names []string
	var cur ast.Expr = expr
	for {
		mem, ok := cur.(*ast.MemberExpr)
		if !ok {
			break
		}
		pid, ok := mem.Property.(*ast.Identifier)
		if !ok {
			return "", false
		}
		names = append(names, pid.Name)
		cur = mem.Object
	}
	if len(names) == 0 {
		return "", false
	}

	// Resolve the base: an object/array literal, or a signal/local binding
	// whose value is one.
	base := cur
	switch b := cur.(type) {
	case *ast.Identifier:
		if initial, ok := signals[b.Name]; ok {
			base = initial
		} else if v, ok := props[b.Name]; ok {
			if lit := constSourceToAST(v); lit != nil {
				base = lit
			} else {
				return "", false
			}
		} else {
			return "", false
		}
	case *ast.ObjectExpr, *ast.ArrayExpr:
		// use as-is
	default:
		return "", false
	}

	// Walk outer?inner is not needed; names are innermost-first, so reverse to
	// resolve outermost property first.
	for i := len(names) - 1; i >= 0; i-- {
		name := names[i]
		if name == "length" {
			if n, ok := constLength(base); ok {
				base = &ast.Literal{Kind: ast.NumberLit, Value: strconv.Itoa(n)}
				continue
			}
			return "", false
		}
		obj, ok := base.(*ast.ObjectExpr)
		if !ok {
			return "", false
		}
		var found bool
		for _, p := range obj.Properties {
			if p.Spread || p.Key != name || p.Value == nil {
				continue
			}
			base = p.Value
			found = true
			break
		}
		if !found {
			return "", false
		}
	}
	return evalConstWithSignals(base, signals, props), true
}
func operandLeaks(expr ast.Expr, signals map[string]ast.Expr, props map[string]string) bool {
	switch e := expr.(type) {
	case *ast.Identifier:
		if e.Name == "props" {
			return true
		}
		if _, ok := signals[e.Name]; ok {
			return false
		}
		_, ok := props[e.Name]
		return !ok
	case *ast.MemberExpr:
		if id, ok := e.Object.(*ast.Identifier); ok && id.Name == "props" {
			if _, ok := e.Property.(*ast.Identifier); ok {
				return false
			}
		}
		if _, ok := resolveMemberChainValue(e, props); ok {
			return false
		}
		return true
	case *ast.CallExpr:
		// Context reads fold to their default and never leak.
		if mem, ok := e.Callee.(*ast.MemberExpr); ok {
			if prop, ok := mem.Property.(*ast.Identifier); ok && prop.Name == "useContext" {
				if id, ok := mem.Object.(*ast.Identifier); ok {
					if _, found := props[contextMarkerPrefix+id.Name]; found {
						return false
					}
				}
			}
		}
		if id, ok := e.Callee.(*ast.Identifier); ok {
			if id.Name == "useContext" && len(e.Args) == 1 {
				if arg, ok := e.Args[0].(*ast.Identifier); ok {
					if _, found := props[contextMarkerPrefix+arg.Name]; found {
						return false
					}
				}
			}
			if _, ok := signals[id.Name]; ok {
				return false
			}
			// Pure class helpers do not leak when every argument is itself
			// resolvable (e.g. cn("base", className)), so class folding can
			// proceed for prop-driven components.
			if id.Name == "cn" || id.Name == "clsx" {
				for _, arg := range e.Args {
					if operandLeaks(arg, signals, props) {
						return true
					}
				}
				return false
			}
		}
		return true
	case *ast.BinaryExpr:
		return operandLeaks(e.Left, signals, props) || operandLeaks(e.Right, signals, props)
	case *ast.UnaryExpr:
		return operandLeaks(e.Arg, signals, props)
	case *ast.ConditionalExpr:
		if operandLeaks(e.Test, signals, props) {
			return true
		}
		tv := evalConstWithSignals(e.Test, signals, props)
		if isTruthyValue(tv) {
			return operandLeaks(e.Consequent, signals, props)
		}
		return operandLeaks(e.Alternate, signals, props)
	case *ast.TemplateExpr:
		for _, p := range e.Parts {
			if operandLeaks(p, signals, props) {
				return true
			}
		}
		return false
	case *ast.TypeAssertion:
		return operandLeaks(e.Expr, signals, props)
	default:
		return false
	}
}

// resolveSignalReadExpr resolves a signal-read expression to its initial value.
// Handles CallExpr{name()} and Identifier{name} where name is a known signal.
func resolveSignalReadExpr(expr ast.Expr, signals map[string]ast.Expr) string {
	if expr == nil || signals == nil {
		return ""
	}
	switch e := expr.(type) {
	case *ast.CallExpr:
		if id, ok := e.Callee.(*ast.Identifier); ok {
			if initial, ok := signals[id.Name]; ok {
				return evalConst(initial)
			}
		}
	case *ast.Identifier:
		if initial, ok := signals[e.Name]; ok {
			return evalConst(initial)
		}
	}
	return ""
}

// updateTextSlotInitials walks slot children and updates TextSlot.Initial
// values from the resolved signal declarations.
func updateTextSlotInitials(children []SlotNode, signals []SignalDecl) {
	signalMap := make(map[string]string)
	for _, sig := range signals {
		if sig.Initial != "" {
			signalMap[sig.Name] = sig.Initial
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
		test := evalConstWithSignals(e.Test, b.sigMap(), b.localProps)
		if operandLeaks(e.Test, b.sigMap(), b.localProps) {
			return ""
		}
		if isTruthyValue(test) {
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
func (b *builder) paramDefaultValue(fn *ast.FnDecl, name string) string {
	for _, p := range fn.Params {
		if p == nil || p.Name == "" {
			continue
		}
		if p.Name == name && p.Default != nil {
			if v := evalConstWithSignals(p.Default, b.sigMap(), b.localProps); v != "" {
				return v
			}
			return "undefined"
		}
		// Destructured member with a default: `{ asChild = false }`.
		if p.Name == "{...}" {
			if def := destructuredParamDefault(p.Pattern, name); def != nil {
				if v := evalConstWithSignals(def, b.sigMap(), b.localProps); v != "" {
					return v
				}
				return "undefined"
			}
		}
	}
	return "undefined"
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
func (b *builder) propBindingsToLocalProps(fn *ast.FnDecl, attrs map[string]ast.Expr, parent map[string]string) map[string]string {
	out := make(map[string]string)
	for _, name := range extractParamNames(fn) {
		if expr, ok := attrs[name]; ok {
			out[name] = evalConstWithSignals(expr, b.sigMap(), parent)
		} else {
			out[name] = "undefined"
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
func (b *builder) buildPropBindings(paramNames []string, attrs map[string]ast.Expr, fn *ast.FnDecl) map[string]string {
	bindings := make(map[string]string)
	props := b.localProps
	if props == nil {
		props = make(map[string]string)
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
func (b *builder) resolveBindingValue(expr ast.Expr, props map[string]string) string {
	if expr == nil {
		return ""
	}
	if v := evalConstWithSignals(expr, b.sigMap(), props); v != "" {
		return v
	}
	if v := evalConst(expr); v != "" {
		return v
	}
	return resolveSignalReadExpr(expr, b.sigMap())
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

// propsIdentRe matches a standalone `props` identifier in compiled JS.
var propsIdentRe = regexp.MustCompile(`\bprops\b`)

// compiledRefsProps reports whether any compiled JS string references `props`.
// Effects/memos that read props.X (e.g. controlled-component sync effects like
// `if (props.checked !== undefined) setChecked(props.checked)`) require the
// parent-scope props registration so `props` resolves at runtime.
func compiledRefsProps(js []string) bool {
	for _, s := range js {
		if propsIdentRe.MatchString(s) {
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
		if propsIdentRe.MatchString(s.RawInit) {
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
	first := true
	for name, expr := range props {
		if !first {
			sb.WriteByte(',')
		}
		first = false
		sb.WriteString(name)
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
func resolvePropsMembers(expr ast.Expr, props map[string]string, signals map[string]ast.Expr) ast.Expr {
	// Fast path: the whole expression is props.X.
	if mem, ok := expr.(*ast.MemberExpr); ok {
		if id, ok := mem.Object.(*ast.Identifier); ok && id.Name == "props" {
			if pid, ok := mem.Property.(*ast.Identifier); ok {
				if v, ok := props[pid.Name]; ok {
					if lit := constSourceToAST(v); lit != nil {
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
	props   map[string]string
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
					if lit := constSourceToAST(v); lit != nil {
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

// constSourceToAST converts a resolved const value string back to an AST literal
// so it can be re-emitted as self-contained JS. Array/object/numeric/boolean
// sources (valid JS produced by evalConst) are parsed back; a bare string token
// becomes a string literal. Returns nil when the value is empty.
func constSourceToAST(v string) ast.Expr {
	if v == "" {
		return nil
	}
	switch v {
	case "true", "false", "null", "undefined":
		return &ast.Literal{Kind: ast.BoolLit, Value: v}
	}
	if v[0] == '[' || v[0] == '{' {
		if expr := singleExprFromProgram(parseExprAsProgram(v)); expr != nil {
			return expr
		}
		// A bare object literal parses as a block statement, not an expression.
		// Wrap in parens so the parser returns the ObjectExpr itself.
		if expr := singleExprFromProgram(parseExprAsProgram("(" + v + ")")); expr != nil {
			return expr
		}
		return nil
	}
	// A bare scalar (string/number). Number-like values stay numeric tokens.
	if isNumericLiteral(v) {
		return &ast.Literal{Kind: ast.NumberLit, Value: v}
	}
	return &ast.Literal{Kind: ast.StringLit, Value: v}
}

// constLength returns the `.length` of a resolved const expression: the number
// of elements for an array literal, or the rune count for a string literal.
// Returns ok=false for anything else (objects, numbers, unresolved values).
func constLength(expr ast.Expr) (int, bool) {
	switch e := expr.(type) {
	case *ast.ArrayExpr:
		return len(e.Elements), true
	case *ast.Literal:
		if e.Kind == ast.StringLit {
			return len([]rune(e.Value)), true
		}
	}
	return 0, false
}

// singleExprFromProgram returns the single expression statement of a program,
// or nil when the program is empty or has no leading expression statement.
func singleExprFromProgram(prog *ast.Program) ast.Expr {
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
