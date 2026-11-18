package build

import (
	"os"
	"strings"

	"github.com/kratejs/krate/packages/compiler/ast"
)

// apiRouteRuntime is a route's `export const runtime` choice.
type apiRouteRuntime int

const (
	apiRuntimeAuto   apiRouteRuntime = iota // no (usable) export
	apiRuntimeNodeJS                        // 'nodejs': always served by Node
	apiRuntimeEdge                          // 'edge': stays on the embedded runtime
)

// readAPIRouteRuntime reads `export const runtime = 'nodejs' | 'edge'` from an
// API route source. Any other value (or none) leaves the choice to the build.
func readAPIRouteRuntime(file string) apiRouteRuntime {
	data, err := os.ReadFile(file)
	if err != nil || !strings.Contains(string(data), "runtime") {
		return apiRuntimeAuto
	}
	prog, err := parseSourceFile(file, data)
	if err != nil || prog == nil {
		return apiRuntimeAuto
	}
	return apiRuntimeFromProgram(prog)
}

func apiRuntimeFromProgram(prog *ast.Program) apiRouteRuntime {
	for _, stmt := range prog.Body {
		exp, ok := stmt.(*ast.ExportStmt)
		if !ok {
			continue
		}
		vs, ok := exp.Declaration.(*ast.VarStmt)
		if !ok {
			continue
		}
		for _, d := range vs.Decls {
			if d.Name != "runtime" || d.Init == nil {
				continue
			}
			lit := d.Init
			for {
				p, ok := lit.(*ast.ParenExpr)
				if !ok {
					break
				}
				lit = p.Expr
			}
			l, ok := lit.(*ast.Literal)
			if !ok || l.Kind != ast.StringLit {
				return apiRuntimeAuto
			}
			switch strings.Trim(l.Value, "\"'`") {
			case "nodejs":
				return apiRuntimeNodeJS
			case "edge", "experimental-edge":
				return apiRuntimeEdge
			}
			return apiRuntimeAuto
		}
	}
	return apiRuntimeAuto
}
