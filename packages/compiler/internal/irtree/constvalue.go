package irtree

import (
	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/jsvalue"
)

// valueMap maps names to build-time JS values (component props, folded
// locals, module constants).
type valueMap = map[string]jsvalue.Value

// evalConstValue evaluates expr to a typed JS value at build time. Signal reads
// (name() and bare signal identifiers) resolve to their initial values, props
// maps parameter/local names to their folded values, and `props.X` reads a
// call-site prop (an absent prop is a definite undefined). Anything that cannot
// be resolved yields an Unknown value.
func evalConstValue(expr ast.Expr, signals map[string]ast.Expr, props valueMap) jsvalue.Value {
	if expr == nil {
		return jsvalue.Undefined()
	}
	return newConstInterp(signals, props).Eval(expr)
}

func newConstInterp(signals map[string]ast.Expr, props valueMap) *jsvalue.Interp {
	in := jsvalue.New(jsvalue.Hooks{})
	resolving := map[string]bool{}
	signalInitial := func(name string) (jsvalue.Value, bool) {
		initial, ok := signals[name]
		if !ok {
			return jsvalue.Value{}, false
		}
		if isResourceSentinel(initial) {
			return jsvalue.Dynamic("resource " + name + " has no data during SSR"), true
		}
		if resolving[name] {
			return jsvalue.Dynamic("recursive signal initial " + name), true
		}
		resolving[name] = true
		v := in.EvalIn(jsvalue.NewScope(nil), initial)
		delete(resolving, name)
		return v, true
	}
	contextDefault := func(ctx string) (jsvalue.Value, bool) {
		v, ok := props[contextMarkerPrefix+ctx]
		return v, ok
	}
	in.Hooks = jsvalue.Hooks{
		Ident: func(name string) (jsvalue.Value, bool) {
			if name == "__krate_resource__" {
				return jsvalue.Dynamic("resource"), true
			}
			if v, ok := signalInitial(name); ok {
				return v, true
			}
			if name == "props" {
				// Props are flattened into the map; a bare `props` read is only
				// known when an object value was bound for it.
				if v, ok := props[name]; ok && v.IsObject() {
					return v, true
				}
				return jsvalue.Dynamic("props object"), true
			}
			if v, ok := props[name]; ok {
				return v, true
			}
			return jsvalue.Value{}, false
		},
		Member: func(m *ast.MemberExpr) (jsvalue.Value, bool) {
			id, ok := m.Object.(*ast.Identifier)
			if !ok || m.Computed {
				return jsvalue.Value{}, false
			}
			prop, ok := m.Property.(*ast.Identifier)
			if !ok {
				return jsvalue.Value{}, false
			}
			if _, shadowed := in.Scope.Lookup(id.Name); shadowed {
				return jsvalue.Value{}, false
			}
			if id.Name == "props" {
				if v, ok := props[prop.Name]; ok {
					return v, true
				}
				if obj, ok := props["props"]; ok && obj.IsObject() {
					return jsvalue.GetProperty(obj, prop.Name), true
				}
				if prop.Name == "children" {
					// Children resolve through the call-site slot machinery.
					return jsvalue.Dynamic("props.children"), true
				}
				// Props are closed at the call site: an absent prop is undefined.
				return jsvalue.Undefined(), true
			}
			if initial, ok := signals[id.Name]; ok && isResourceSentinel(initial) {
				switch prop.Name {
				case "loading":
					return jsvalue.Bool(true), true
				case "state":
					return jsvalue.String("unresolved"), true
				case "error":
					return jsvalue.Undefined(), true
				}
				return jsvalue.Dynamic("resource " + id.Name + "." + prop.Name), true
			}
			return jsvalue.Value{}, false
		},
		Call: func(c *ast.CallExpr) (jsvalue.Value, bool) {
			switch callee := c.Callee.(type) {
			case *ast.Identifier:
				if _, shadowed := in.Scope.Lookup(callee.Name); shadowed {
					return jsvalue.Value{}, false
				}
				if (callee.Name == "useContext" || callee.Name == "use") && len(c.Args) == 1 {
					if arg, ok := c.Args[0].(*ast.Identifier); ok {
						if v, ok := contextDefault(arg.Name); ok {
							return v, true
						}
					}
				}
				if _, isSignal := signals[callee.Name]; isSignal {
					v, _ := signalInitial(callee.Name)
					return v, true
				}
				if callee.Name == "cn" || callee.Name == "clsx" {
					if _, isProp := props[callee.Name]; !isProp {
						return foldClassValues(in, c.Args), true
					}
				}
			case *ast.MemberExpr:
				if prop, ok := callee.Property.(*ast.Identifier); ok && (prop.Name == "useContext" || prop.Name == "use") {
					if id, ok := callee.Object.(*ast.Identifier); ok {
						if v, ok := contextDefault(id.Name); ok {
							return v, true
						}
					}
				}
			}
			return jsvalue.Value{}, false
		},
	}
	return in
}

// foldClassValues folds cn/clsx arguments with clsx semantics.
func foldClassValues(in *jsvalue.Interp, args []ast.Expr) jsvalue.Value {
	var tokens []string
	for _, a := range args {
		v := in.Eval(a)
		if v.ContainsUnknown() {
			return jsvalue.Dynamic("class helper argument not known at build time")
		}
		tokens = appendClassTokens(tokens, v)
	}
	out := ""
	for i, t := range tokens {
		if i > 0 {
			out += " "
		}
		out += t
	}
	return jsvalue.String(out)
}

// appendClassTokens flattens one clsx argument: strings/numbers are tokens,
// arrays recurse, objects contribute keys with truthy values.
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

// legacyConstString renders a folded value in the historical string form the
// string-typed IR consumers expect (attribute values, slot initials, JS
// literal emission). Unknown values are "" except a bare unresolved identifier,
// which keeps its name (callers detect that case with operandLeaks).
func legacyConstString(expr ast.Expr, v jsvalue.Value) string {
	if v.IsUnknown() {
		if id, ok := expr.(*ast.Identifier); ok {
			return id.Name
		}
		return ""
	}
	if v.Kind() == jsvalue.KindNull {
		return ""
	}
	return jsvalue.LegacyString(v)
}

// childText renders a value in JSX child position as plain text (React
// semantics: null/undefined/booleans render nothing, arrays concatenate). The
// caller escapes it. ok=false for values that are not plain text (JSX,
// objects, Unknown).
func childText(v jsvalue.Value) (string, bool) {
	switch v.Kind() {
	case jsvalue.KindUndefined, jsvalue.KindNull, jsvalue.KindBool, jsvalue.KindFunction:
		return "", true
	case jsvalue.KindNumber, jsvalue.KindString, jsvalue.KindBigInt:
		return v.ToString(), true
	case jsvalue.KindArray:
		out := ""
		for _, el := range v.Elems() {
			s, ok := childText(el)
			if !ok {
				return "", false
			}
			out += s
		}
		return out, true
	}
	return "", false
}

// renderChildValue renders a known value as child HTML (escaped text). ok=false
// when the value cannot be rendered statically.
func renderChildValue(v jsvalue.Value) (string, bool) {
	if v.ContainsUnknown() {
		return "", false
	}
	html, err := jsvalue.RenderChild(v)
	if err != nil {
		return "", false
	}
	return html, true
}

// valueJSLiteral renders a folded value as a JS literal for hydration code.
func valueJSLiteral(v jsvalue.Value) string {
	if src, ok := jsvalue.JSSource(v); ok {
		return src
	}
	return "undefined"
}

// typedModuleConsts folds module-level constant initializers to typed values.
// Initializers may reference each other (evaluated lazily, cycles yield
// Unknown). Only fully-known, non-function values are kept; names that only
// have a legacy string value (no initializer) are decoded from it.
func typedModuleConsts(inits map[string]ast.Expr, legacy map[string]string) valueMap {
	out := make(valueMap, len(inits))
	state := map[string]int{} // 1 = evaluating, 2 = done
	values := map[string]jsvalue.Value{}
	in := jsvalue.New(jsvalue.Hooks{})
	resolve := func(name string) (jsvalue.Value, bool) {
		switch state[name] {
		case 1:
			return jsvalue.Dynamic("cyclic module constant " + name), true
		case 2:
			return values[name], true
		}
		init, ok := inits[name]
		if !ok || init == nil {
			if s, ok := legacy[name]; ok {
				return jsvalue.FromLegacy(s), true
			}
			return jsvalue.Value{}, false
		}
		state[name] = 1
		v := in.EvalIn(jsvalue.NewScope(nil), init)
		state[name] = 2
		values[name] = v
		return v, true
	}
	in.Hooks.Ident = resolve
	for name := range inits {
		v, _ := resolve(name)
		if v.ContainsUnknown() || v.IsFunction() || v.IsJSX() {
			continue
		}
		out[name] = v
	}
	for name, s := range legacy {
		if _, ok := out[name]; !ok {
			if _, hasInit := inits[name]; !hasInit {
				out[name] = jsvalue.FromLegacy(s)
			}
		}
	}
	return out
}

// arrayHasJSX reports whether an array literal contains a JSX element.
func arrayHasJSX(arr *ast.ArrayExpr) bool {
	for _, el := range arr.Elements {
		if hasJSXInExpr(el) {
			return true
		}
	}
	return false
}
