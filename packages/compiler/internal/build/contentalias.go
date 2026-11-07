package build

import (
	"reflect"

	"github.com/kratejs/krate/packages/compiler/ast"
)

// aliasEntry is `const posts = getCollection("blog")...` whose uses are folded
// in place (`posts.find(...)` becomes the matching entry). The declaration is
// inlined only if something still reads the name afterwards.
type aliasEntry struct {
	decl *ast.VarDecl
	init ast.Expr
}

// aliasScope holds the names bound in one function or in the module. A nil
// entry is a name that is bound there but is not an alias; it hides an alias of
// an outer scope.
type aliasScope map[string]*aliasEntry

// buildAliasScope collects the bindings of a function body (nested functions
// are their own scopes). A name bound more than once, or by a parameter, is
// never an alias.
func buildAliasScope(params []*ast.Param, body []ast.Stmt) aliasScope {
	counts := map[string]int{}
	cands := map[string]*aliasEntry{}
	for _, p := range params {
		if p == nil {
			continue
		}
		if p.Name != "" {
			counts[p.Name]++
		}
		for _, w := range identWordRe.FindAllString(p.Pattern, -1) {
			counts[w]++
		}
	}
	var walk func(stmts []ast.Stmt)
	walk = func(stmts []ast.Stmt) {
		for _, st := range stmts {
			switch s := st.(type) {
			case *ast.VarStmt:
				for _, d := range s.Decls {
					if d.IsDestructuring || d.Target != nil {
						for _, n := range d.Names {
							counts[n]++
						}
						for _, w := range identWordRe.FindAllString(d.Pattern, -1) {
							counts[w]++
						}
						continue
					}
					if d.Name == "" {
						continue
					}
					counts[d.Name]++
					if s.Kind == ast.VarConst && d.Init != nil && rootCollectionName(d.Init) != "" {
						cands[d.Name] = &aliasEntry{decl: d, init: d.Init}
					}
				}
			case *ast.FnDecl:
				counts[s.Name]++
			case *ast.ExportStmt:
				if s.Declaration != nil {
					walk([]ast.Stmt{s.Declaration})
				}
			case *ast.IfStmt:
				walk(s.Consequent)
				walk(s.Alternate)
			case *ast.BlockStmt:
				walk(s.Body)
			case *ast.ForStmt:
				walk(s.Body)
			}
		}
	}
	walk(body)
	scope := aliasScope{}
	for name := range counts {
		scope[name] = nil
	}
	for name, e := range cands {
		if counts[name] == 1 {
			scope[name] = e
		}
	}
	return scope
}

// pushAliasScope makes the aliases of a function body visible while it is
// walked.
func (ic *inlineCtx) pushAliasScope(params []*ast.Param, body []ast.Stmt) aliasScope {
	scope := buildAliasScope(params, body)
	for _, e := range scope {
		if e != nil {
			ic.deferred[e.decl] = true
		}
	}
	ic.scopes = append(ic.scopes, scope)
	return scope
}

// popAliasScope settles the aliases of a finished scope: a declaration nothing
// reads any more is emptied, one that is still read is inlined as before.
func (ic *inlineCtx) popAliasScope(scope aliasScope, root any, bindings map[string]string) {
	ic.scopes = ic.scopes[:len(ic.scopes)-1]
	for name, e := range scope {
		if e == nil {
			continue
		}
		delete(ic.deferred, e.decl)
		if countIdentifier(root, name) == 0 {
			e.decl.Init = &ast.ArrayExpr{}
			continue
		}
		// What is left reads the collection in the browser (the fold failed).
		if ic.depth == 0 {
			ic.sites = append(ic.sites, declPos(e.init))
		}
		ic.depth++
		e.decl.Init = inlineContentExpr(e.decl.Init, ic, bindings)
		ic.depth--
	}
}

func declPos(e ast.Expr) ast.Pos {
	if n, ok := e.(interface{ Pos() ast.Pos }); ok {
		return n.Pos()
	}
	return ast.Pos{}
}

func (ic *inlineCtx) lookupAlias(name string) *aliasEntry {
	for i := len(ic.scopes) - 1; i >= 0; i-- {
		if e, ok := ic.scopes[i][name]; ok {
			return e
		}
	}
	return nil
}

// expandAlias returns expr with its alias root replaced by the alias's chain,
// or nil when expr is not a chain on an alias. The result shares nodes with the
// declaration and is only used to render JavaScript, never put in the tree.
func (ic *inlineCtx) expandAlias(expr ast.Expr) ast.Expr {
	switch e := expr.(type) {
	case *ast.Identifier:
		if ent := ic.lookupAlias(e.Name); ent != nil {
			return ent.init
		}
	case *ast.MemberExpr:
		if o := ic.expandAlias(e.Object); o != nil {
			cp := *e
			cp.Object = o
			return &cp
		}
	case *ast.CallExpr:
		if m, ok := e.Callee.(*ast.MemberExpr); ok {
			if o := ic.expandAlias(m.Object); o != nil {
				mc := *m
				mc.Object = o
				cp := *e
				cp.Callee = &mc
				return &cp
			}
		}
	}
	return nil
}

// foldAliased folds a chain on an alias (`posts.find(...)`) the way a chain
// on getCollection(...) is folded. It returns nil when expr is not such a
// chain or cannot be folded at build time.
func (ic *inlineCtx) foldAliased(expr ast.Expr, bindings map[string]string) ast.Expr {
	if len(ic.scopes) == 0 {
		return nil
	}
	if call, ok := expr.(*ast.CallExpr); ok {
		if mem, ok := call.Callee.(*ast.MemberExpr); ok {
			if prop, ok := mem.Property.(*ast.Identifier); ok && prop.Name == "map" {
				if exp := ic.expandAlias(mem.Object); exp != nil {
					if arr := foldCollectionChain(exp, ic.collections, bindings); arr != nil {
						mem.Object = arr
						return call
					}
				}
			}
		}
	}
	if exp := ic.expandAlias(expr); exp != nil {
		return foldCollectionChain(exp, ic.collections, bindings)
	}
	return nil
}

// countIdentifier counts reads of name under root.
func countIdentifier(root any, name string) int {
	n := 0
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
			if id, ok := v.Interface().(*ast.Identifier); ok && id.Name == name {
				n++
			}
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
	return n
}

// helperEntry is a module-level function that only returns a collection chain,
// e.g. `function getPost(slug) { return getCollection("blog").find(...) }`.
// A call with build-time arguments folds to the entry; the function is emptied
// when nothing calls it any more, so its chain does not ship.
type helperEntry struct {
	params []string
	ret    ast.Expr
	// setBody drops the function body once it is unreferenced.
	setBody func()
}

// collectHelpers finds the foldable helpers of the module. Exported functions
// stay, as another module may call them.
func collectHelpers(prog *ast.Program) map[string]*helperEntry {
	counts := map[string]int{}
	cands := map[string]*helperEntry{}
	simple := func(ps []*ast.Param) ([]string, bool) {
		var names []string
		for _, p := range ps {
			if p == nil || p.Name == "" || p.Pattern != "" || p.Target != nil || p.IsRest || p.Default != nil {
				return nil, false
			}
			names = append(names, p.Name)
		}
		return names, true
	}
	retOf := func(body []ast.Stmt) ast.Expr {
		if len(body) != 1 {
			return nil
		}
		if rs, ok := body[0].(*ast.ReturnStmt); ok && rs.Value != nil && rootCollectionName(rs.Value) != "" {
			return rs.Value
		}
		return nil
	}
	for _, st := range prog.Body {
		switch s := st.(type) {
		case *ast.FnDecl:
			counts[s.Name]++
			if names, ok := simple(s.Params); ok && !s.Async && !s.Generator {
				if r := retOf(s.Body); r != nil {
					fn := s
					cands[s.Name] = &helperEntry{params: names, ret: cloneNode(r).(ast.Expr), setBody: func() { fn.Body = nil }}
				}
			}
		case *ast.ExportStmt:
			switch d := s.Declaration.(type) {
			case *ast.FnDecl:
				counts[d.Name] += 2
			case *ast.VarStmt:
				for _, vd := range d.Decls {
					counts[vd.Name] += 2
				}
			}
		case *ast.VarStmt:
			for _, d := range s.Decls {
				counts[d.Name]++
				if arrow, ok := d.Init.(*ast.ArrowFn); ok && s.Kind == ast.VarConst && !arrow.Async && !arrow.Generator {
					if names, ok := simple(arrow.Params); ok {
						if r := retOf(arrow.Body); r != nil {
							a := arrow
							cands[d.Name] = &helperEntry{params: names, ret: cloneNode(r).(ast.Expr), setBody: func() { a.Body = nil }}
						}
					}
				}
			}
		}
	}
	for name := range cands {
		if counts[name] != 1 {
			delete(cands, name)
		}
	}
	return cands
}

// foldHelperCall folds `getPost("hello")` by evaluating the helper's chain with
// its parameters bound to the arguments. It returns nil unless every argument
// is a build-time literal and the chain folds.
func (ic *inlineCtx) foldHelperCall(call *ast.CallExpr, bindings map[string]string) ast.Expr {
	id, ok := call.Callee.(*ast.Identifier)
	if !ok {
		return nil
	}
	h := ic.helpers[id.Name]
	if h == nil || len(h.params) != len(call.Args) || ic.lookupAlias(id.Name) != nil {
		return nil
	}
	local := make(map[string]string, len(bindings)+len(h.params))
	for k, v := range bindings {
		local[k] = v
	}
	for i, arg := range call.Args {
		js, ok := literalJS(arg)
		if !ok {
			if aid, isID := arg.(*ast.Identifier); isID {
				js, ok = bindings[aid.Name]
			}
		}
		if !ok {
			return nil
		}
		local[h.params[i]] = js
	}
	// Each call folds a fresh copy: folding binds the parameters in place.
	return foldCollectionChain(cloneNode(h.ret).(ast.Expr), ic.collections, local)
}

// settleHelpers empties the helpers nothing reads any more.
func (ic *inlineCtx) settleHelpers(prog *ast.Program) {
	for name, h := range ic.helpers {
		if countIdentifier(prog.Body, name) == 0 {
			h.setBody()
		}
	}
}

// cloneNode deep-copies an AST subtree.
func cloneNode(n any) any {
	return cloneValue(reflect.ValueOf(n)).Interface()
}

func cloneValue(v reflect.Value) reflect.Value {
	switch v.Kind() {
	case reflect.Interface:
		if v.IsNil() {
			return v
		}
		out := reflect.New(v.Type()).Elem()
		out.Set(cloneValue(v.Elem()))
		return out
	case reflect.Pointer:
		if v.IsNil() {
			return v
		}
		out := reflect.New(v.Type().Elem())
		out.Elem().Set(cloneValue(v.Elem()))
		return out
	case reflect.Slice:
		if v.IsNil() {
			return v
		}
		out := reflect.MakeSlice(v.Type(), v.Len(), v.Len())
		for i := 0; i < v.Len(); i++ {
			out.Index(i).Set(cloneValue(v.Index(i)))
		}
		return out
	case reflect.Struct:
		out := reflect.New(v.Type()).Elem()
		out.Set(v)
		for i := 0; i < v.NumField(); i++ {
			if v.Type().Field(i).IsExported() {
				out.Field(i).Set(cloneValue(v.Field(i)))
			}
		}
		return out
	}
	return v
}
