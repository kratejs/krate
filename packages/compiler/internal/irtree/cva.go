package irtree

import (
	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/jsvalue"
)

// CVASpec is a resolved `cva(base, config)` declaration. The compiler folds a
// `buttonVariants({ variant, size })` call to a literal class string so a
// shadcn/ui component's className is static HTML with no hydration binding.
type CVASpec struct {
	Base     string
	Variants map[string]map[string]string
	// VariantOrder is the config order of Variants; the runtime applies
	// variant classes in this order.
	VariantOrder []string
	Defaults     map[string]string
	// Compound holds compoundVariant rules as parallel key lists; the class is
	// applied when every (key, value) matches the selection.
	Compound []cvaCompound
}

type cvaCompound struct {
	Conditions map[string][]string // key -> accepted values (one of)
	Class      string
}

// CollectCVAFactories scans a module's top-level declarations for
// `const X = cva(base, config)` and returns the resolved specs keyed by X.
func CollectCVAFactories(prog *ast.Program) map[string]*CVASpec {
	out := make(map[string]*CVASpec)
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
			if decl.Name == "" || decl.Init == nil {
				continue
			}
			call, ok := decl.Init.(*ast.CallExpr)
			if !ok {
				continue
			}
			id, ok := call.Callee.(*ast.Identifier)
			if !ok || id.Name != "cva" || len(call.Args) < 1 {
				continue
			}
			base, ok := literalString(call.Args[0])
			if !ok {
				continue
			}
			if len(call.Args) >= 2 && hasComputedKey(call.Args[1]) {
				// A variant named by an expression cannot be folded; the
				// factory's calls stay runtime values.
				continue
			}
			spec := &CVASpec{Base: base, Variants: map[string]map[string]string{}, Defaults: map[string]string{}}
			if len(call.Args) >= 2 {
				parseCVAConfig(call.Args[1], spec)
			}
			out[decl.Name] = spec
		}
	}
	return out
}

// mergeCVAFactories combines factories discovered across imported modules
// (from the annotator) with any declared in this program, this program's
// declarations taking precedence.
func mergeCVAFactories(fromAnn map[string]*CVASpec, prog *ast.Program) map[string]*CVASpec {
	out := make(map[string]*CVASpec)
	for name, spec := range fromAnn {
		out[name] = spec
	}
	for name, spec := range CollectCVAFactories(prog) {
		out[name] = spec
	}
	return out
}

func parseCVAConfig(expr ast.Expr, spec *CVASpec) {
	obj, ok := expr.(*ast.ObjectExpr)
	if !ok {
		return
	}
	for _, prop := range obj.Properties {
		if prop == nil || prop.Spread {
			continue
		}
		switch prop.Key {
		case "variants":
			if vo, ok := prop.Value.(*ast.ObjectExpr); ok {
				for _, vp := range vo.Properties {
					if vp == nil || vp.Spread || vp.Key == "" {
						continue
					}
					opts, ok := vp.Value.(*ast.ObjectExpr)
					if !ok {
						continue
					}
					m := map[string]string{}
					for _, op := range opts.Properties {
						if op == nil || op.Spread || op.Key == "" {
							continue
						}
						if v, ok := literalString(op.Value); ok {
							m[op.Key] = v
						}
					}
					if _, seen := spec.Variants[vp.Key]; !seen {
						spec.VariantOrder = append(spec.VariantOrder, vp.Key)
					}
					spec.Variants[vp.Key] = m
				}
			}
		case "defaultVariants", "defaults":
			if do, ok := prop.Value.(*ast.ObjectExpr); ok {
				for _, dp := range do.Properties {
					if dp == nil || dp.Spread || dp.Key == "" {
						continue
					}
					if v, ok := literalString(dp.Value); ok {
						spec.Defaults[dp.Key] = v
					}
				}
			}
		case "compoundVariants":
			if arr, ok := prop.Value.(*ast.ArrayExpr); ok {
				for _, el := range arr.Elements {
					if c, ok := parseCompoundVariant(el); ok {
						spec.Compound = append(spec.Compound, c)
					}
				}
			}
		}
	}
}

func parseCompoundVariant(expr ast.Expr) (cvaCompound, bool) {
	obj, ok := expr.(*ast.ObjectExpr)
	if !ok {
		return cvaCompound{}, false
	}
	c := cvaCompound{Conditions: map[string][]string{}}
	for _, prop := range obj.Properties {
		if prop == nil || prop.Spread || prop.Key == "" {
			continue
		}
		if prop.Key == "class" {
			if v, ok := literalString(prop.Value); ok {
				c.Class = v
			}
			continue
		}
		if arr, ok := prop.Value.(*ast.ArrayExpr); ok {
			var vals []string
			for _, el := range arr.Elements {
				if v, ok := literalString(el); ok {
					vals = append(vals, v)
				}
			}
			c.Conditions[prop.Key] = vals
			continue
		}
		if v, ok := literalString(prop.Value); ok {
			c.Conditions[prop.Key] = []string{v}
		}
	}
	return c, true
}

// Fold mirrors the runtime cva in packages/runtime/src/class-helpers.ts: the
// base, the selected variant classes in config order, matching compound
// variants and the explicit extra class, merged with twMerge. A key present in
// selection (even with an empty value) overrides the default; only variant
// names take part in selection and compound matching.
func (s *CVASpec) Fold(selection map[string]string, extraClass string) string {
	chosen := map[string]string{}
	for _, name := range s.VariantOrder {
		if v, ok := selection[name]; ok {
			chosen[name] = v
		} else if d, ok := s.Defaults[name]; ok {
			chosen[name] = d
		}
	}
	parts := []string{s.Base}
	for _, name := range s.VariantOrder {
		if v := chosen[name]; v != "" {
			if cls, ok := s.Variants[name][v]; ok {
				parts = append(parts, cls)
			}
		}
	}
	for _, c := range s.Compound {
		matched := true
		for key, accepted := range c.Conditions {
			got, have := chosen[key]
			if !have {
				got, have = s.Defaults[key]
			}
			if !have || !contains(accepted, got) {
				matched = false
				break
			}
		}
		if matched && c.Class != "" {
			parts = append(parts, c.Class)
		}
	}
	if extraClass != "" {
		parts = append(parts, extraClass)
	}
	return mergeTailwindClasses(parts)
}

// FoldValue folds a call's props object like the runtime cva: null and
// undefined props fall back to defaults, `class` wins over `className`, and
// only string class overrides count. An unknown value is returned as is.
func (s *CVASpec) FoldValue(props jsvalue.Value) jsvalue.Value {
	selection := map[string]string{}
	var class, className *jsvalue.Value
	for _, k := range props.Keys() {
		v, _ := props.Get(k)
		if v.IsUnknown() {
			return v
		}
		if v.IsNullish() {
			continue
		}
		switch k {
		case "class":
			class = &v
		case "className":
			className = &v
		default:
			selection[k] = v.ToString()
		}
	}
	extra := ""
	if class == nil {
		class = className
	}
	if class != nil && class.Kind() == jsvalue.KindString {
		extra = class.StringValue()
	}
	return jsvalue.String(s.Fold(selection, extra))
}

func contains(list []string, v string) bool {
	for _, item := range list {
		if item == v {
			return true
		}
	}
	return false
}

// literalString resolves a literal string/number value.
func literalString(expr ast.Expr) (string, bool) {
	lit, ok := expr.(*ast.Literal)
	if !ok {
		return "", false
	}
	switch lit.Kind {
	case ast.StringLit, ast.NumberLit:
		return lit.Value, true
	}
	return "", false
}

// hasComputedKey reports whether any object literal under expr has a
// `[expr]: value` key.
func hasComputedKey(expr ast.Expr) bool {
	found := false
	WalkAST(expr, func(n any) bool {
		if p, ok := n.(*ast.ObjectProp); ok && p.Computed {
			found = true
		}
		return !found
	})
	return found
}
