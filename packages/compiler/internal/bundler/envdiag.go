package bundler

import (
	"strings"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/diag"
)

// PublicEnvPrefix names the environment variables that are inlined into client
// code and static output.
const PublicEnvPrefix = "KRATE_PUBLIC_"

// envRoot reports whether e is `process.env` or `import.meta.env`, returning
// its spelling.
func envRoot(e ast.Expr) (string, bool) {
	m, ok := e.(*ast.MemberExpr)
	if !ok || m.Computed {
		return "", false
	}
	prop, ok := m.Property.(*ast.Identifier)
	if !ok || prop.Name != "env" {
		return "", false
	}
	switch o := m.Object.(type) {
	case *ast.Identifier:
		if o.Name == "process" {
			return "process.env", true
		}
	case *ast.ImportMetaExpr:
		return "import.meta.env", true
	}
	return "", false
}

// envReadDiags reports environment reads that survive the compile-time
// replacement (Options.Define) in client or statically rendered code. They
// would otherwise render empty with no explanation. Reads in server and
// runtime components, and in modules with server actions, are legitimate.
func (b *Bundler) envReadDiags(path, src string, prog *ast.Program, class ComponentClass) []diag.Diagnostic {
	if prog == nil || (class != ComponentClassClient && class != ComponentClassStatic) {
		return nil
	}
	if !strings.Contains(src, "import.meta.env") && !strings.Contains(src, "process.env") {
		return nil
	}
	if strings.Contains(src, "use server") || exportsAsyncFunction(prog) {
		return nil
	}
	lines := strings.Split(src, "\n")
	var out []diag.Diagnostic
	handled := map[*ast.MemberExpr]bool{}
	report := func(pos ast.Pos, what string) {
		d := diag.Diagnostic{
			File: path, Line: pos.Line, Col: pos.Col,
			Severity: diag.SeverityWarning,
			Message:  what + " is not exposed to the browser or to statically rendered output; it reads as undefined",
			Hint:     "name the variable with the " + PublicEnvPrefix + " prefix (for example " + PublicEnvPrefix + "API_URL) to inline it at build time, or read it in a server component, API route or server action",
		}.WithCode(diag.CodeEnvNotExposed)
		if pos.Line >= 1 && pos.Line <= len(lines) {
			d.Source = strings.TrimRight(lines[pos.Line-1], "\r")
		}
		out = append(out, d)
	}
	visitStmts(prog.Body, false, func(n any, _ bool) bool {
		m, ok := n.(*ast.MemberExpr)
		if !ok || handled[m] {
			return true
		}
		if root, isEnv := envRoot(m); isEnv {
			report(m.Position, root)
			return true
		}
		if root, isEnv := envRoot(m.Object); isEnv {
			handled[m.Object.(*ast.MemberExpr)] = true
			name := ""
			if id, ok := m.Property.(*ast.Identifier); ok && !m.Computed {
				name = id.Name
			} else if lit, ok := m.Property.(*ast.Literal); ok && lit.Kind == ast.StringLit {
				name = lit.Value
			}
			if name == "" {
				report(m.Position, root)
			} else {
				report(m.Position, root+"."+name)
			}
		}
		return true
	})
	return out
}

// exportsAsyncFunction reports whether the module exports an async function.
// An async component is a server component: it renders per request on the
// server, where process.env is the real environment.
func exportsAsyncFunction(prog *ast.Program) bool {
	for _, stmt := range prog.Body {
		ex, ok := stmt.(*ast.ExportStmt)
		if !ok {
			continue
		}
		switch d := ex.Declaration.(type) {
		case *ast.FnDecl:
			if d.Async {
				return true
			}
		case *ast.VarStmt:
			for _, decl := range d.Decls {
				if fn, ok := decl.Init.(*ast.ArrowFn); ok && fn.Async {
					return true
				}
			}
		}
	}
	return false
}
