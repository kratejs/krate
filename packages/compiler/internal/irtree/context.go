package irtree

import "github.com/kratejs/krate/packages/compiler/ast"

// CollectContextDefaults scans a module's top-level declarations for
// `const X = createContext(default)` and returns each context's default value
// keyed by X. The SSR evaluator uses this to fold `X.useContext()` to the
// default during static rendering (there is no Provider at build time), so a
// page that reads a context default renders real text instead of leaking the
// identifier.
func CollectContextDefaults(prog *ast.Program) map[string]string {
	out := map[string]string{}
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

// contextDefaultValue returns the default of a `createContext(v)` call when v
// is a string literal (or number, rendered as its text).
func contextDefaultValue(expr ast.Expr) (string, bool) {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return "", false
	}
	id, ok := call.Callee.(*ast.Identifier)
	if !ok || id.Name != "createContext" || len(call.Args) < 1 {
		return "", false
	}
	if s, ok := literalString(call.Args[0]); ok {
		return s, true
	}
	return "", false
}

// mergeContextDefaults combines defaults discovered across imported modules
// (from the annotator) with any declared in this program, this program's
// declarations taking precedence.
func mergeContextDefaults(fromAnn map[string]string, prog *ast.Program) map[string]string {
	out := make(map[string]string)
	for name, val := range fromAnn {
		out[name] = val
	}
	for name, val := range CollectContextDefaults(prog) {
		out[name] = val
	}
	return out
}
