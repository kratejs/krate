package irtree

import (
	"strings"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/jsvalue"
)

// CollectContextDefaults scans a module's top-level declarations for
// `const X = createContext(default)` and returns each context's default value
// keyed by X. Every createContext binding is listed, so the map doubles as the
// set of known contexts (`<X value>` is a provider, `<X.Consumer>` a reader).
// A default the build cannot evaluate (it calls a function, reads a runtime
// global) is recorded as an unknown value. The builder and the SSR evaluator
// fold `X.useContext()` to the default during static rendering when no
// provider is above the reader.
func CollectContextDefaults(prog *ast.Program) map[string]jsvalue.Value {
	out := map[string]jsvalue.Value{}
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
			if v, ok := contextDefaultValue(decl.Init); ok {
				out[decl.Name] = v
			}
		}
	}
	return out
}

// isCreateContextCall reports a `createContext(...)` call (React's
// createContext is rewritten to the Krate one, which has the same name).
func isCreateContextCall(expr ast.Expr) (*ast.CallExpr, bool) {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return nil, false
	}
	id, ok := call.Callee.(*ast.Identifier)
	if !ok || id.Name != "createContext" {
		return nil, false
	}
	return call, true
}

// contextDefaultValue returns the default of a `createContext(v)` call: v
// evaluated at build time (undefined when absent, unknown when it cannot be
// evaluated).
func contextDefaultValue(expr ast.Expr) (jsvalue.Value, bool) {
	call, ok := isCreateContextCall(expr)
	if !ok {
		return jsvalue.Value{}, false
	}
	if len(call.Args) == 0 {
		return jsvalue.Undefined(), true
	}
	return evalConstValue(call.Args[0], nil, nil), true
}

// mergeContextDefaults combines defaults discovered across imported modules
// (from the annotator) with any declared in this program, this program's
// declarations taking precedence.
func mergeContextDefaults(fromAnn map[string]jsvalue.Value, prog *ast.Program) map[string]jsvalue.Value {
	out := make(map[string]jsvalue.Value)
	for name, val := range fromAnn {
		out[name] = val
	}
	for name, val := range CollectContextDefaults(prog) {
		out[name] = val
	}
	return out
}

// activeContextMarkers returns the context values folded into the current
// local-prop scope (defaults and constant provider values), keyed by context.
func (b *builder) activeContextMarkers() map[string]jsvalue.Value {
	var out map[string]jsvalue.Value
	for k, v := range b.localProps {
		ctx, ok := strings.CutPrefix(k, contextMarkerPrefix)
		if !ok {
			continue
		}
		if out == nil {
			out = make(map[string]jsvalue.Value)
		}
		out[ctx] = v
	}
	return out
}
