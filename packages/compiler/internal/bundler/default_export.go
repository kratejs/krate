package bundler

import (
	"path/filepath"
	"strings"
	"unicode"

	"github.com/kratejs/krate/packages/compiler/ast"
)

// IsComponentExpr reports an expression that defines a component inline: a
// function (arrow or `function` expression), or such a function wrapped in
// memo()/forwardRef().
func IsComponentExpr(e ast.Expr) bool {
	switch x := e.(type) {
	case *ast.ArrowFn:
		return !x.Generator
	case *ast.CallExpr:
		id, ok := x.Callee.(*ast.Identifier)
		if !ok || (id.Name != "memo" && id.Name != "forwardRef") || len(x.Args) == 0 {
			return false
		}
		return IsComponentExpr(x.Args[0])
	}
	return false
}

// lowerDefaultExpressionExport names an inline default-exported component:
// `export default () => <div/>` and `export default memo(function V() {})`
// become `const Name = <expr>; export default Name;`. The compiler finds a
// module's components by declaration, so without a name such a page used to
// build to an empty document. Name comes from the file name; a clash with
// another module's binding is resolved by the private-name pass later.
func lowerDefaultExpressionExport(prog *ast.Program, path string) {
	for i, stmt := range prog.Body {
		exp, ok := stmt.(*ast.ExportStmt)
		if !ok || !exp.Default {
			continue
		}
		es, ok := exp.Declaration.(*ast.ExprStmt)
		if !ok || !IsComponentExpr(es.Expression) {
			continue
		}
		name := defaultExportName(prog, path)
		decl := &ast.VarStmt{Position: exp.Position, Kind: ast.VarConst, Decls: []*ast.VarDecl{{
			Name: name, Init: es.Expression, Tier: prog.Tier,
		}}}
		body := make([]ast.Stmt, 0, len(prog.Body)+1)
		body = append(body, prog.Body[:i]...)
		body = append(body, decl, &ast.ExportStmt{Position: exp.Position, Default: true, Local: name})
		body = append(body, prog.Body[i+1:]...)
		prog.Body = body
		return
	}
}

// defaultExportName derives a component name from the file name, avoiding
// the module's existing top-level names.
func defaultExportName(prog *ast.Program, path string) string {
	base := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	var b strings.Builder
	upper := true
	for _, r := range base {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			if upper {
				r = unicode.ToUpper(r)
				upper = false
			}
			if r < 128 {
				b.WriteRune(r)
			}
			continue
		}
		upper = true
	}
	name := b.String()
	if name == "" || name[0] >= '0' && name[0] <= '9' {
		name = "Page" + name
	}
	taken := collectModuleNames(prog)
	imported := map[string]bool{}
	for _, stmt := range prog.Body {
		if imp, ok := stmt.(*ast.ImportStmt); ok {
			imported[imp.Default] = true
			imported[imp.Namespace] = true
			for _, n := range imp.Named {
				imported[n.Local] = true
				imported[n.Remote] = true
			}
		}
	}
	for taken.private[name] || taken.fixed[name] || imported[name] {
		name += "Default"
	}
	return name
}
