package irtree

import (
	"sort"
	"strings"

	"github.com/kratejs/krate/packages/compiler/ast"
)

// CVASpec is a resolved `cva(base, config)` declaration. The compiler folds a
// `buttonVariants({ variant, size })` call to a literal class string so a
// shadcn/ui component's className is static HTML with no hydration binding.
type CVASpec struct {
	Base     string
	Variants map[string]map[string]string
	Defaults map[string]string
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
		if prop.Key == "class" || prop.Key == "className" {
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

// Fold resolves a variant selection to a class string: the base, the selected
// variant classes, matching compound variants, and any explicit extra class
// (the `class`/`className` entry in selection).
func (s *CVASpec) Fold(selection map[string]string, extraClass string) string {
	parts := []string{s.Base}
	// Deterministic variant order.
	names := make([]string, 0, len(s.Variants))
	for name := range s.Variants {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		chosen := selection[name]
		if chosen == "" {
			chosen = s.Defaults[name]
		}
		if chosen != "" {
			if cls, ok := s.Variants[name][chosen]; ok {
				parts = append(parts, cls)
			}
		}
	}
	for _, c := range s.Compound {
		matched := true
		for key, accepted := range c.Conditions {
			got := selection[key]
			if got == "" {
				got = s.Defaults[key]
			}
			if !contains(accepted, got) {
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
	return strings.Join(parts, " ")
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
