package build

import (
	"os"
	"strings"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/diag"
)

// accessDeniedSources are the import sources that provide forbidden() and
// unauthorized().
var accessDeniedSources = map[string]bool{
	"@krate/runtime/server": true,
	"next/navigation":       true,
}

// detectAccessDeniedCall finds the first call to forbidden() or unauthorized()
// imported from a server module in prog.
func detectAccessDeniedCall(prog *ast.Program) (name string, pos ast.Pos, ok bool) {
	if prog == nil {
		return "", ast.Pos{}, false
	}
	local := map[string]string{}
	for _, stmt := range prog.Body {
		imp, isImp := stmt.(*ast.ImportStmt)
		if !isImp || !accessDeniedSources[strings.Trim(imp.Source, "\"'`")] {
			continue
		}
		for _, n := range imp.Named {
			if n.Remote == "forbidden" || n.Remote == "unauthorized" {
				local[n.Local] = n.Remote
			}
		}
	}
	if len(local) == 0 {
		return "", ast.Pos{}, false
	}
	walkProgramExprs(prog, func(e ast.Expr) bool {
		if ok {
			return false
		}
		call, isCall := e.(*ast.CallExpr)
		if !isCall {
			return true
		}
		if id, isID := call.Callee.(*ast.Identifier); isID {
			if remote, hit := local[id.Name]; hit {
				name, pos, ok = remote, call.Position, true
				return false
			}
		}
		return true
	})
	return name, pos, ok
}

// warnStaticAccessDenied warns that forbidden() or unauthorized() sits in code
// that is baked at build time. where is "page" or "layout".
func warnStaticAccessDenied(file string, prog *ast.Program, source, where string) *diag.Diagnostic {
	name, pos, ok := detectAccessDeniedCall(prog)
	if !ok {
		return nil
	}
	msg := name + "() is called in a " + where + " that is built statically, so it never runs per request and the " + where + " is served to everyone"
	hint := "export `dynamic = \"force-dynamic\"` from the page, or call " + name + "() from an async server component the page renders"
	if where == "layout" {
		msg = name + "() is called in a layout; layouts are baked into the static shell at build time, so the call never runs"
		hint = "call " + name + "() from an async server component the page renders, or check the request in middleware"
	}
	d := warnAt(file, source, pos, diag.CodeStaticAccessDenied, msg, hint)
	return &d
}

func printAccessDeniedWarning(d *diag.Diagnostic) {
	if d != nil {
		printWarningsOnce(os.Stderr, []diag.Diagnostic{*d})
	}
}
