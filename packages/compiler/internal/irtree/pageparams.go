package irtree

import (
	"sort"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/jsvalue"
)

// seedPageParams gives the entry page the `params` prop a router would pass
// it. A page that wraps its content (`<Post {...props} />`) forwards that
// prop, so the child binds `params` like the page itself does. Pages that
// destructure `{ params }` are already served by the root's eval bindings.
func (b *builder) seedPageParams(entryFn *ast.FnDecl, params map[string]string) {
	if entryFn == nil || len(params) == 0 {
		return
	}
	name := fnRestParamName(entryFn)
	if name == "" {
		name = plainPropsParamName(entryFn)
	}
	if name == "" {
		return
	}
	// The forwarded value is read back as `props.params` in the page's own
	// scope, so the value is bound there as well.
	obj := jsvalue.NewObject()
	for key, val := range params {
		obj.Set(key, jsvalue.String(val))
	}
	if b.localProps == nil {
		b.localProps = make(valueMap)
	}
	b.localProps["params"] = obj
	b.restProps = map[string]map[string]ast.Expr{name: {"params": pageParamsExpr(params)}}
}

// pageParamsExpr is the object literal {name: "value", ...}, keys sorted.
func pageParamsExpr(params map[string]string) ast.Expr {
	names := make([]string, 0, len(params))
	for name := range params {
		names = append(names, name)
	}
	sort.Strings(names)
	obj := &ast.ObjectExpr{}
	for _, name := range names {
		obj.Properties = append(obj.Properties, &ast.ObjectProp{
			Key:   name,
			Value: &ast.Literal{Kind: ast.StringLit, Value: params[name]},
		})
	}
	return obj
}
