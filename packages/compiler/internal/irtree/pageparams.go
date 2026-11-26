package irtree

import (
	"sort"
	"strings"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/jsvalue"
)

// PageParamBindings binds entryFn's first parameter (plain props, rest, or
// destructuring such as `{ params }`, `{ params: { slug } }`, `{ params: p }`,
// or `{ slug }`) against the page's dynamic-route params.
func PageParamBindings(entryFn *ast.FnDecl, params map[string]string) map[string]jsvalue.Value {
	if entryFn == nil || len(params) == 0 {
		return nil
	}
	names := make([]string, 0, len(params))
	for k := range params {
		names = append(names, k)
	}
	sort.Strings(names)
	paramsObj := jsvalue.NewObject()
	for _, k := range names {
		paramsObj.Set(k, jsvalue.String(params[k]))
	}
	propsObj := jsvalue.NewObject()
	propsObj.Set("params", paramsObj)

	out := make(map[string]jsvalue.Value)
	if plain := plainPropsParamName(entryFn); plain != "" {
		out[plain] = propsObj
		out["params"] = paramsObj
		return out
	}
	if len(entryFn.Params) == 0 || entryFn.Params[0] == nil {
		return nil
	}
	p := entryFn.Params[0]
	restName := fnRestParamName(entryFn)
	bindObj := propsObj
	if restName == "" {
		// Also expose each route param directly so legacy `{ slug }` pages bind it.
		bindObj = jsvalue.NewObject()
		for _, k := range names {
			bindObj.Set(k, jsvalue.String(params[k]))
		}
		bindObj.Set("params", paramsObj)
	}
	scope := jsvalue.NewScope(nil)
	in := newConstInterp(nil, nil)
	if r := in.BindPattern(scope, p.Name, p.Pattern, bindObj); !r.IsUnsupported() {
		for _, n := range scope.Names() {
			if v, ok := scope.Lookup(n); ok {
				out[n] = v
			}
		}
	}
	if restName != "" {
		if _, exists := out["params"]; !exists {
			out["params"] = paramsObj
		}
	}
	return out
}

// seedPageParams gives the entry page the `params` prop a router would pass
// it, binding the entry function's first parameter into localProps so locals
// derived from route params fold during IR construction. A page that wraps its
// content (`<Post {...props} />`) also keeps the forwarded `params` in
// restProps.
func (b *builder) seedPageParams(entryFn *ast.FnDecl, params map[string]string) {
	if entryFn == nil || len(params) == 0 {
		return
	}
	bound := PageParamBindings(entryFn, params)
	if len(bound) > 0 {
		if b.localProps == nil {
			b.localProps = make(valueMap, len(bound))
		}
		b.pageParamValues = make(valueMap, len(bound))
		for k, v := range bound {
			b.localProps[k] = v
			b.pageParamValues[k] = v
		}
	}
	if plain := plainPropsParamName(entryFn); plain != "" {
		b.restProps = map[string]map[string]ast.Expr{plain: {"params": pageParamsExpr(params)}}
		return
	}
	if restName := fnRestParamName(entryFn); restName != "" {
		if rest := restProps(entryFn, map[string]ast.Expr{"params": pageParamsExpr(params)}); len(rest) > 0 {
			b.restProps = map[string]map[string]ast.Expr{restName: rest}
		}
	}
}

// pageParamHydrationDecls returns declarations that expose the page's route
// params to a client-tier entry page whose hydration code reads them. Concrete
// generateStaticParams pages embed literal values; dynamic-route templates read
// window.__KRATE_PARAMS__ injected at request time.
func (b *builder) pageParamHydrationDecls(node *ComponentNode, fn *ast.FnDecl) []string {
	if len(b.pageParams) == 0 || fn == nil || len(fn.Params) == 0 || fn.Params[0] == nil {
		return nil
	}
	declared := declaredScopeNames(node)
	paramSet := paramBindingSet(fn)
	if !b.pageParamsDynamic {
		names := make([]string, 0, len(b.pageParamValues))
		for k := range b.pageParamValues {
			names = append(names, k)
		}
		sort.Strings(names)
		var decls []string
		for _, name := range names {
			if !paramSet[name] || declared[name] || !hydrationReadsAny(node, []string{name}) {
				continue
			}
			decls = append(decls, "var "+name+"="+valueJSLiteral(b.pageParamValues[name]))
		}
		return decls
	}
	if plain := plainPropsParamName(fn); plain != "" {
		if paramSet[plain] && !declared[plain] && hydrationReadsAny(node, []string{plain}) {
			return []string{"var " + plain + "={params:window.__KRATE_PARAMS__||{}}"}
		}
		return nil
	}
	var needed []string
	for _, n := range extractParamNames(fn) {
		if paramSet[n] && !declared[n] {
			needed = append(needed, n)
		}
	}
	if len(needed) == 0 || !hydrationReadsAny(node, needed) {
		return nil
	}
	p := fn.Params[0]
	if isPlainParamsPattern(p.Pattern) && len(needed) == 1 && needed[0] == "params" {
		return []string{"var params=window.__KRATE_PARAMS__||{}"}
	}
	pattern := p.Pattern
	if p.Target != nil {
		pattern = renderPattern(p.Target, nil)
	}
	if pattern == "" {
		return nil
	}
	if fnRestParamName(fn) != "" {
		return []string{"var " + pattern + "={params:window.__KRATE_PARAMS__||{}}"}
	}
	return []string{"var " + pattern + "=Object.assign({},window.__KRATE_PARAMS__||{},{params:window.__KRATE_PARAMS__||{}})"}
}

// isPlainParamsPattern reports whether pattern is `{ params }` (with any whitespace).
func isPlainParamsPattern(pattern string) bool {
	s := strings.TrimSpace(pattern)
	if len(s) < 2 || s[0] != '{' || s[len(s)-1] != '}' {
		return false
	}
	return strings.TrimSpace(s[1:len(s)-1]) == "params"
}

// declaredScopeNames collects every variable/function name already declared in
// node's PreSignalVars and ExtraVars (including destructuring patterns).
func declaredScopeNames(node *ComponentNode) map[string]bool {
	declared := map[string]bool{}
	for _, list := range [][]string{node.PreSignalVars, node.ExtraVars} {
		for _, v := range list {
			if name := hoistedDeclName(v); name != "" {
				declared[name] = true
				continue
			}
			if lhs, _, ok := splitVarDecl(v); ok {
				lhs = strings.TrimSpace(lhs)
				if len(lhs) >= 2 && (lhs[0] == '{' || lhs[0] == '[') {
					for _, n := range destructuredParamNames(lhs) {
						declared[n] = true
					}
				}
			}
		}
	}
	return declared
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
