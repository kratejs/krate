package bundler

import (
	"reflect"
	"strings"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/diag"
)

// React APIs removed in React 19 (KR8001) and legacy context (KR8005).

// removedDOMAPIs are react-dom exports React 19 removed, with the hint for
// each.
var removedDOMAPIs = map[string]string{
	"findDOMNode":            "attach a ref to the element instead (`ref={nodeRef}`, then read nodeRef.current)",
	"render":                 "Krate mounts pages itself; render the component from a page instead of calling ReactDOM.render",
	"hydrate":                "Krate hydrates pages itself; remove the ReactDOM.hydrate call",
	"unmountComponentAtNode": "Krate unmounts pages on navigation; render the content conditionally instead",
}

// removedReactAPIs are react exports React 19 removed.
var removedReactAPIs = map[string]string{
	"createFactory": "write JSX (or createElement(type, props)) instead of a factory",
}

// legacyContextMembers are the class members of the legacy context API.
var legacyContextMembers = map[string]bool{
	"contextTypes":      true,
	"childContextTypes": true,
	"getChildContext":   true,
}

// legacyImports records the local bindings of removed APIs imported by name.
func legacyImports(prog *ast.Program) map[string]string {
	out := map[string]string{}
	for _, stmt := range prog.Body {
		imp, ok := stmt.(*ast.ImportStmt)
		if !ok {
			continue
		}
		var table map[string]string
		switch strings.Trim(imp.Source, "\"'") {
		case "react-dom":
			table = removedDOMAPIs
		case "react":
			table = removedReactAPIs
		default:
			continue
		}
		for _, named := range imp.Named {
			if _, removed := table[named.Remote]; !removed {
				continue
			}
			local := named.Local
			if local == "" {
				local = named.Remote
			}
			out[local] = named.Remote
		}
	}
	return out
}

// checkLegacyAPIs reports calls of APIs removed in React 19 and classes that
// use legacy context. It runs before the rewrite, on the source as written.
// imported holds the removed APIs imported by name (legacyImports, taken
// before the React imports are dropped).
func (c *reactCtx) checkLegacyAPIs(prog *ast.Program, imported map[string]string) {
	reportedClass := map[*ast.ClassDecl]bool{}
	walkNodes(reflect.ValueOf(prog.Body), func(n any) {
		switch e := n.(type) {
		case *ast.CallExpr:
			if api, ok := c.removedCallee(e.Callee, imported); ok {
				hint := removedDOMAPIs[api]
				if hint == "" {
					hint = removedReactAPIs[api]
				}
				c.warn(e.Position, diag.Code("KR8001"), api+" was removed in React 19 and has no Krate equivalent", hint)
			}
		case *ast.ClassDecl:
			for _, m := range e.Members {
				if m == nil || reportedClass[e] || !legacyContextMembers[m.Name] {
					continue
				}
				if (m.Name == "getChildContext") == m.Static {
					continue
				}
				reportedClass[e] = true
				name := e.Name
				if name == "" {
					name = "class"
				}
				c.warn(m.Position, diag.Code("KR8005"),
					"<"+name+"> uses legacy context ("+m.Name+"), which React 19 removed; this.context stays empty",
					"create a context with createContext, provide it with <Ctx value={...}> and read it with static contextType = Ctx or useContext(Ctx)")
			}
		}
	})
}

// removedCallee resolves a callee to a removed API name: a named import, or
// a member of the React / ReactDOM namespace.
func (c *reactCtx) removedCallee(callee ast.Expr, imported map[string]string) (string, bool) {
	switch e := callee.(type) {
	case *ast.Identifier:
		api, ok := imported[e.Name]
		return api, ok
	case *ast.MemberExpr:
		obj, ok := e.Object.(*ast.Identifier)
		prop, ok2 := e.Property.(*ast.Identifier)
		if !ok || !ok2 || e.Computed {
			return "", false
		}
		if c.domAlias != "" && obj.Name == c.domAlias {
			if _, removed := removedDOMAPIs[prop.Name]; removed {
				return prop.Name, true
			}
		}
		if c.alias != "" && obj.Name == c.alias {
			if _, removed := removedReactAPIs[prop.Name]; removed {
				return prop.Name, true
			}
		}
	}
	return "", false
}

// walkNodes calls visit for every AST node pointer reachable from v.
func walkNodes(v reflect.Value, visit func(any)) {
	switch v.Kind() {
	case reflect.Interface:
		if !v.IsNil() {
			walkNodes(v.Elem(), visit)
		}
	case reflect.Pointer:
		if v.IsNil() || v.Elem().Kind() != reflect.Struct {
			return
		}
		visit(v.Interface())
		walkNodes(v.Elem(), visit)
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			if v.Type().Field(i).IsExported() {
				walkNodes(v.Field(i), visit)
			}
		}
	case reflect.Slice:
		for i := 0; i < v.Len(); i++ {
			walkNodes(v.Index(i), visit)
		}
	}
}
