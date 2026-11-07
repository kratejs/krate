package build

import (
	"encoding/json"
	"reflect"

	"github.com/kratejs/krate/packages/compiler/ast"
)

// paramPatternBindings finds the locals a page binds to route params through
// destructuring, as JS literals, so a collection lookup keyed by them folds at
// build time like `props.params.slug` does:
//
//	function Post({ params: { slug } }) {}
//	function Post({ params }) { const { slug } = params; }
//	function Post(props) { const { slug } = props.params; }
//
// Names are matched without scoping; a local is only bound to a value when its
// name is a key of params.
func paramPatternBindings(prog *ast.Program, params map[string]string) map[string]string {
	if prog == nil || len(params) == 0 {
		return nil
	}
	out := map[string]string{}
	paramsNames := map[string]bool{"params": true}
	propsNames := map[string]bool{"props": true}

	bind := func(pat *ast.ObjectExpr) {
		for _, p := range pat.Properties {
			if p.Spread || p.Computed {
				continue
			}
			val, ok := params[p.Key]
			if !ok {
				continue
			}
			if name := patternLocal(p.Value); name != "" {
				js, _ := json.Marshal(val)
				out[name] = string(js)
			}
		}
	}
	// A pattern over the props object: `params` may be renamed or destructured.
	fromProps := func(pat *ast.ObjectExpr) {
		for _, p := range pat.Properties {
			if p.Spread || p.Computed || p.Key != "params" {
				continue
			}
			switch v := p.Value.(type) {
			case *ast.ObjectExpr:
				bind(v)
			default:
				if name := patternLocal(v); name != "" {
					paramsNames[name] = true
				}
			}
		}
	}

	var fns []*ast.ArrowFn
	var decls []*ast.FnDecl
	forEachNode(prog.Body, func(n any) {
		switch f := n.(type) {
		case *ast.ArrowFn:
			fns = append(fns, f)
		case *ast.FnDecl:
			decls = append(decls, f)
		}
	})
	first := func(ps []*ast.Param) {
		if len(ps) == 0 || ps[0] == nil {
			return
		}
		if pat, ok := ps[0].Target.(*ast.ObjectExpr); ok {
			fromProps(pat)
		} else if ps[0].Name != "" && ps[0].Pattern == "" {
			propsNames[ps[0].Name] = true
		}
	}
	for _, f := range decls {
		first(f.Params)
	}
	for _, f := range fns {
		first(f.Params)
	}

	isParamsObj := func(e ast.Expr) bool {
		switch x := e.(type) {
		case *ast.Identifier:
			return paramsNames[x.Name]
		case *ast.MemberExpr:
			if prop, ok := x.Property.(*ast.Identifier); ok && prop.Name == "params" {
				if id, ok := x.Object.(*ast.Identifier); ok {
					return propsNames[id.Name]
				}
			}
		}
		return false
	}
	// Two rounds so `const { params } = props` is known before `const { slug }
	// = params` whatever their order.
	for round := 0; round < 2; round++ {
		forEachNode(prog.Body, func(n any) {
			d, ok := n.(*ast.VarDecl)
			if !ok || d.Init == nil {
				return
			}
			pat, ok := d.Target.(*ast.ObjectExpr)
			if !ok {
				return
			}
			if isParamsObj(d.Init) {
				bind(pat)
			} else if id, ok := d.Init.(*ast.Identifier); ok && propsNames[id.Name] {
				fromProps(pat)
			}
		})
	}
	return out
}

// patternLocal returns the local name a destructuring target binds: the
// identifier itself or the left side of a `name = default`.
func patternLocal(e ast.Expr) string {
	switch v := e.(type) {
	case *ast.Identifier:
		return v.Name
	case *ast.BinaryExpr:
		if v.Op == "=" {
			if id, ok := v.Left.(*ast.Identifier); ok {
				return id.Name
			}
		}
	}
	return ""
}

// forEachNode calls fn for every pointer node under root.
func forEachNode(root any, fn func(n any)) {
	var walk func(v reflect.Value)
	walk = func(v reflect.Value) {
		switch v.Kind() {
		case reflect.Interface:
			if !v.IsNil() {
				walk(v.Elem())
			}
		case reflect.Pointer:
			if v.IsNil() {
				return
			}
			fn(v.Interface())
			walk(v.Elem())
		case reflect.Slice, reflect.Array:
			for i := 0; i < v.Len(); i++ {
				walk(v.Index(i))
			}
		case reflect.Struct:
			for i := 0; i < v.NumField(); i++ {
				if v.Type().Field(i).IsExported() {
					walk(v.Field(i))
				}
			}
		}
	}
	walk(reflect.ValueOf(root))
}
