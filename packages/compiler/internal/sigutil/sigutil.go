// Package sigutil centralizes detection of reactive declarations
// (createSignal / createResource destructuring) across the compiler. The
// annotator and the IR builder previously walked function bodies with their own
// copies of this traversal; both now consume this single implementation.
package sigutil

import "github.com/kratejs/krate/packages/compiler/ast"

// CSSKind identifies which zero-JS CSS primitive a declaration uses.
type CSSKind int

const (
	// CSSKindNone is a non-CSS reactive declaration (createSignal/createResource).
	CSSKindNone CSSKind = iota
	// CSSKindChoice is `createCSSChoice(initial, options?)` - a radio group.
	CSSKindChoice
	// CSSKindToggle is `createCSSToggle(initial)` - a single checkbox.
	CSSKindToggle
	// CSSKindFlags is `createCSSFlags([...])` - independent checkboxes.
	CSSKindFlags
	// CSSKindGroup is `createCSSGroup(initial|null, opts?)` - an optional radio
	// group: a choice with an explicit "closed" (null) sentinel option.
	CSSKindGroup
	// CSSKindRange is `createCSSRange(initial, {min,max,step})` - a discrete
	// radio chain over integer values.
	CSSKindRange
	// CSSKindStack is `createCSSStack([...])` - a declared navigation tree with
	// push/pop/clear methods (a stack of nested radio levels).
	CSSKindStack
)

// Decl is a single detected reactive declaration.
type Decl struct {
	Name       string
	Setter     string   // createSignal's second destructured name
	Initial    ast.Expr // initial value expression (may be nil for resources)
	IsResource bool
	// Factory names the reactive factory the declaration was built with
	// (createSignal, createReducer, ...). Empty for resources/CSS primitives.
	Factory string
	// Args holds the factory call's arguments so lowering passes can emit the
	// original call (e.g. createReducer's reducer function) instead of a plain
	// createSignal.
	Args []ast.Expr
	// CSSKind marks a zero-JS CSS declaration (choice/toggle/flags/group/range/
	// stack). These are compiled to hidden inputs + `:has()` CSS, never emitted
	// as JS signals.
	CSSKind CSSKind
	// Options holds the option universe: the explicit array for a choice, the
	// flag names for flags, or ["on"] for a toggle. Empty for non-CSS decls.
	Options []string
	// As is the requested ARIA role preset (`{ as: 'tabs' }`); "" means the
	// kind's default.
	As string
	// Aria is the ARIA mode: "", "structural", "full" (opt-in tiny runtime), or
	// "off". Raw attribute overrides live in AriaAttrs.
	Aria string
	// AriaAttrs holds raw `aria: { ... }` attribute overrides, keyed by
	// attribute name (e.g. "role", "aria-controls").
	AriaAttrs map[string]string
	// Label is an optional accessible label for the scope container.
	Label string
	// Min/Max/Step are the literal numeric bounds for createCSSRange.
	Min, Max, Step string
	// Tree maps each stack node to its parent ("" for the root) and Nodes lists
	// every node in declaration order (root first) for createCSSStack.
	Tree  map[string]string
	Nodes []string
	Root  string
	// Names holds every destructured name in order. For a stack the second
	// element is the method object's destructured names ([getter, push, pop, ...]).
	Names []string
	// RestName is the trailing rest binding name in a destructuring pattern.
	RestName string
	// Vars maps a CSS custom property name to a per-option value
	// (`{ '--x': { a: '"1"', b: '"2"' } }`). Emitted as inheritable custom
	// properties so state can drive live text/theme with zero JS.
	Vars map[string]map[string]string
	// VarErr is set when `vars` was present but not all-literal, naming the
	// offending property so analysis can raise a precise hard error.
	VarErr string
}

// Find walks a statement list for reactive declarations. When recurse is true,
// control-flow bodies and nested blocks are walked too (the annotator's
// scoping); when false only top-level statements are inspected (the IR
// builder's scoping - signals declared in blocks are out of hydration scope).
func Find(body []ast.Stmt, recurse bool) []Decl {
	var out []Decl
	var walk func([]ast.Stmt)
	walk = func(stmts []ast.Stmt) {
		for _, stmt := range stmts {
			switch s := stmt.(type) {
			case *ast.VarStmt:
				for _, decl := range s.Decls {
					if !decl.IsDestructuring || decl.Init == nil {
						continue
					}
					call, ok := decl.Init.(*ast.CallExpr)
					if !ok {
						continue
					}
					id, ok := call.Callee.(*ast.Identifier)
					if !ok {
						continue
					}
					switch id.Name {
					case "createSignal":
						if len(decl.Names) >= 2 && len(call.Args) >= 1 {
							out = append(out, Decl{Name: decl.Names[0], Setter: decl.Names[1], Initial: call.Args[0], Factory: "createSignal", Args: call.Args})
						}
					case "createReducer":
						// Reducer state getter: the initial value is the second
						// argument (reducer, initial) - the third for the
						// optional init function form.
						if len(decl.Names) >= 2 && len(call.Args) >= 2 {
							out = append(out, Decl{Name: decl.Names[0], Setter: decl.Names[1], Initial: call.Args[1], Factory: "createReducer", Args: call.Args})
						}
					case "createResource":
						if len(decl.Names) >= 1 && len(call.Args) >= 1 {
							out = append(out, Decl{Name: decl.Names[0], Initial: call.Args[0], IsResource: true})
						}
					case "createCSSChoice":
						if len(decl.Names) >= 2 && len(call.Args) >= 1 {
							d := Decl{
								Name:     decl.Names[0],
								Setter:   decl.Names[1],
								Initial:  call.Args[0],
								CSSKind:  CSSKindChoice,
								Names:    append([]string(nil), decl.Names...),
								RestName: decl.RestName,
							}
							if len(call.Args) >= 2 {
								d.Options, d.As, d.Aria, d.AriaAttrs, d.Label = parseCSSOptions(call.Args[1], true)
							}
							d.Vars, d.VarErr = literalVarsInArgs(call.Args[1:])
							out = append(out, d)
						}
					case "createCSSToggle":
						if len(decl.Names) >= 2 && len(call.Args) >= 1 {
							d := Decl{
								Name:     decl.Names[0],
								Setter:   decl.Names[1],
								Initial:  call.Args[0],
								CSSKind:  CSSKindToggle,
								Options:  []string{"on"},
								Names:    append([]string(nil), decl.Names...),
								RestName: decl.RestName,
							}
							if len(call.Args) >= 2 {
								_, d.As, d.Aria, d.AriaAttrs, d.Label = parseCSSOptions(call.Args[1], false)
							}
							d.Vars, d.VarErr = literalVarsInArgs(call.Args[1:])
							out = append(out, d)
						}
					case "createCSSFlags":
						if len(decl.Names) >= 2 && len(call.Args) >= 1 {
							d := Decl{
								Name:     decl.Names[0],
								Setter:   decl.Names[1],
								Initial:  nil,
								CSSKind:  CSSKindFlags,
								Names:    append([]string(nil), decl.Names...),
								RestName: decl.RestName,
							}
							d.Options, d.As, d.Aria, d.AriaAttrs, d.Label = parseCSSOptions(call.Args[0], true)
							if len(call.Args) >= 2 {
								_, d.As, d.Aria, d.AriaAttrs, d.Label = parseCSSOptions(call.Args[1], false)
							}
							d.Vars, d.VarErr = literalVarsInArgs(call.Args)
							out = append(out, d)
						}
					case "createCSSGroup":
						if len(decl.Names) >= 2 && len(call.Args) >= 1 {
							d := Decl{
								Name:     decl.Names[0],
								Setter:   decl.Names[1],
								Initial:  call.Args[0],
								CSSKind:  CSSKindGroup,
								Names:    append([]string(nil), decl.Names...),
								RestName: decl.RestName,
							}
							if len(call.Args) >= 2 {
								d.Options, d.As, d.Aria, d.AriaAttrs, d.Label = parseCSSOptions(call.Args[1], true)
							}
							d.Vars, d.VarErr = literalVarsInArgs(call.Args[1:])
							out = append(out, d)
						}
					case "createCSSRange":
						if len(decl.Names) >= 2 && len(call.Args) >= 1 {
							d := Decl{
								Name:     decl.Names[0],
								Setter:   decl.Names[1],
								Initial:  call.Args[0],
								CSSKind:  CSSKindRange,
								Names:    append([]string(nil), decl.Names...),
								RestName: decl.RestName,
							}
							if len(call.Args) >= 2 {
								_, d.As, d.Aria, d.AriaAttrs, d.Label = parseCSSOptions(call.Args[1], false)
								d.Min, d.Max, d.Step = literalNumProp(call.Args[1], "min"), literalNumProp(call.Args[1], "max"), literalNumProp(call.Args[1], "step")
							}
							d.Vars, d.VarErr = literalVarsInArgs(call.Args[1:])
							out = append(out, d)
						}
					case "createCSSStack":
						if len(decl.Names) >= 2 && len(call.Args) >= 1 {
							d := Decl{
								Name:     decl.Names[0],
								Setter:   decl.Names[1],
								Initial:  nil,
								CSSKind:  CSSKindStack,
								Options:  literalStringOptions(call.Args[0]),
								Names:    append([]string(nil), decl.Names...),
								RestName: decl.RestName,
							}
							if len(call.Args) >= 2 {
								_, d.As, d.Aria, d.AriaAttrs, d.Label = parseCSSOptions(call.Args[1], false)
							}
							d.Vars, d.VarErr = literalVarsInArgs(call.Args[1:])
							out = append(out, d)
						}
					}
				}
			case *ast.BlockStmt:
				if recurse {
					walk(s.Body)
				}
			case *ast.ForStmt:
				if recurse {
					walk(s.Body)
				}
			case *ast.WhileStmt:
				if recurse {
					walk(s.Body)
				}
			case *ast.DoWhileStmt:
				if recurse {
					walk(s.Body)
				}
			case *ast.SwitchStmt:
				if recurse {
					for _, c := range s.Cases {
						walk(c.Body)
					}
				}
			case *ast.TryStmt:
				if recurse {
					walk(s.Body)
					if s.Catch != nil {
						walk(s.Catch.Body)
					}
					walk(s.Finally)
				}
			case *ast.IfStmt:
				if recurse {
					walk(s.Consequent)
					walk(s.Alternate)
				}
			}
		}
	}
	walk(body)
	return out
}

// literalStringOptions extracts the string/number values of a literal array
// expression (createCSSChoice's optional explicit options list). Returns nil
// when the argument is not a literal array.
func literalStringOptions(expr ast.Expr) []string {
	arr, ok := expr.(*ast.ArrayExpr)
	if !ok {
		return nil
	}
	var out []string
	for _, el := range arr.Elements {
		lit, ok := el.(*ast.Literal)
		if !ok {
			return nil
		}
		switch lit.Kind {
		case ast.StringLit, ast.NumberLit:
			out = append(out, lit.Value)
		default:
			return nil
		}
	}
	return out
}

// parseCSSOptions reads a primitive's trailing options argument, which may be
// either the legacy literal options array or an options object
// (`{ as, aria, label, options }`). When allowArray is true a bare array is
// read as the option universe. It returns the options plus the ARIA
// configuration (mode, raw overrides) and label.
func parseCSSOptions(expr ast.Expr, allowArray bool) (options []string, as, aria string, attrs map[string]string, label string) {
	if arr, ok := expr.(*ast.ArrayExpr); ok {
		if allowArray {
			options = literalStringOptions(arr)
		}
		return
	}
	obj, ok := expr.(*ast.ObjectExpr)
	if !ok {
		return
	}
	for _, p := range obj.Properties {
		if p == nil || p.Key == "" {
			continue
		}
		switch p.Key {
		case "options":
			if arr, ok := p.Value.(*ast.ArrayExpr); ok {
				options = literalStringOptions(arr)
			}
		case "as":
			if v, ok := literalStringValue(p.Value); ok {
				as = v
			}
		case "aria":
			switch v := p.Value.(type) {
			case *ast.Literal:
				if v.Kind == ast.BoolLit {
					if v.Value == "false" {
						aria = "off"
					}
				} else if s, ok := literalStringValue(p.Value); ok {
					aria = s
				}
			case *ast.ObjectExpr:
				aria = "full"
				attrs = literalObjectAttrs(v)
			}
		case "label":
			if v, ok := literalStringValue(p.Value); ok {
				label = v
			}
		}
	}
	return
}

// literalNumProp reads a literal string/number property from an object.
func literalNumProp(expr ast.Expr, key string) string {
	obj, ok := expr.(*ast.ObjectExpr)
	if !ok {
		return ""
	}
	for _, p := range obj.Properties {
		if p != nil && p.Key == key {
			if v, ok := literalStringValue(p.Value); ok {
				return v
			}
		}
	}
	return ""
}

// literalObjectAttrs reads a `{ key: "value" }` object into a string map.
func literalObjectAttrs(obj *ast.ObjectExpr) map[string]string {
	out := make(map[string]string, len(obj.Properties))
	for _, p := range obj.Properties {
		if p == nil || p.Key == "" {
			continue
		}
		if v, ok := literalStringValue(p.Value); ok {
			out[p.Key] = v
			continue
		}
		if b, ok := p.Value.(*ast.Literal); ok && b.Kind == ast.BoolLit {
			out[p.Key] = b.Value
		}
	}
	return out
}

// literalStringValue resolves a literal to its raw string value.
func literalStringValue(expr ast.Expr) (string, bool) {
	lit, ok := expr.(*ast.Literal)
	if !ok {
		return "", false
	}
	switch lit.Kind {
	case ast.StringLit, ast.NumberLit, ast.BoolLit:
		return lit.Value, true
	}
	return "", false
}

// literalVarsProp reads the `vars` property of an options object:
// `{ '--prop': { option: 'value' } }`. Values must be string literals; the
// first non-literal property (or non-object value) is reported via err, naming
// the property, so the analyzer can hard-error with a precise message. Returns
// (nil, "") when there is no `vars` (or the argument is not an options object).
func literalVarsProp(expr ast.Expr) (vars map[string]map[string]string, err string) {
	obj, ok := expr.(*ast.ObjectExpr)
	if !ok {
		return nil, ""
	}
	var varsExpr ast.Expr
	for _, p := range obj.Properties {
		if p != nil && p.Key == "vars" {
			varsExpr = p.Value
			break
		}
	}
	if varsExpr == nil {
		return nil, ""
	}
	varsObj, ok := varsExpr.(*ast.ObjectExpr)
	if !ok {
		return nil, "vars must be an object of { '--property': { option: value } }"
	}
	vars = make(map[string]map[string]string, len(varsObj.Properties))
	for _, prop := range varsObj.Properties {
		if prop == nil || prop.Key == "" {
			continue
		}
		optObj, ok := prop.Value.(*ast.ObjectExpr)
		if !ok {
			return nil, "vars property " + prop.Key + " must map options to string literals"
		}
		vals := make(map[string]string, len(optObj.Properties))
		for _, ov := range optObj.Properties {
			if ov == nil {
				continue
			}
			v, ok := literalStringValue(ov.Value)
			if !ok {
				return nil, "vars property " + prop.Key + " option " + ov.Key + " must be a string literal"
			}
			vals[ov.Key] = v
		}
		vars[prop.Key] = vals
	}
	return vars, ""
}

// literalVarsInArgs scans the trailing option arguments of a factory call for a
// `vars` object and returns it (plus any error). The first `vars` found wins.
func literalVarsInArgs(args []ast.Expr) (map[string]map[string]string, string) {
	for _, a := range args {
		if v, err := literalVarsProp(a); v != nil || err != "" {
			return v, err
		}
	}
	return nil, ""
}
