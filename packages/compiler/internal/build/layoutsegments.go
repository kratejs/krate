package build

import (
	"strconv"
	"strings"

	"github.com/kratejs/krate/packages/compiler/ast"
)

// useSelectedLayoutSegment(s) in a layout file read the path below that layout.
// The runtime hook counts from a depth argument; the build passes the depth of
// the layout's own directory (route groups do not count, a dynamic segment
// counts as one), so a nested layout sees "settings" where the root layout
// sees "dashboard". Calls in other files keep counting from the root.

var layoutSegmentHooks = map[string]bool{
	"useSelectedLayoutSegment":  true,
	"useSelectedLayoutSegments": true,
}

// layoutURLDepth is the number of URL segments above the layout file.
func layoutURLDepth(layoutPath, pagesDir string) int {
	dir := segmentURLDir(segmentDirKey(layoutPath, pagesDir))
	if dir == "" {
		return 0
	}
	return len(strings.Split(dir, "/"))
}

// passLayoutDepth adds the depth argument to the layout segment hooks that
// prog imports from the runtime. It is a no-op for a call that already has one.
func passLayoutDepth(prog *ast.Program, depth int) {
	if prog == nil {
		return
	}
	local := map[string]bool{}
	for _, stmt := range prog.Body {
		imp, ok := stmt.(*ast.ImportStmt)
		if !ok {
			continue
		}
		spec := strings.Trim(imp.Source, "\"'`")
		if spec != "@krate/runtime" && spec != "@krate/runtime/server" && spec != "next/navigation" {
			continue
		}
		for _, n := range imp.Named {
			if layoutSegmentHooks[n.Remote] {
				name := n.Local
				if name == "" {
					name = n.Remote
				}
				local[name] = true
			}
		}
	}
	if len(local) == 0 {
		return
	}
	walkProgramExprs(prog, func(e ast.Expr) bool {
		call, ok := e.(*ast.CallExpr)
		if !ok || len(call.Args) >= 2 {
			return true
		}
		if id, ok := call.Callee.(*ast.Identifier); ok && local[id.Name] {
			if len(call.Args) == 0 {
				call.Args = append(call.Args, &ast.Identifier{Position: call.Position, Name: "undefined"})
			}
			call.Args = append(call.Args, &ast.Literal{Position: call.Position, Kind: ast.NumberLit, Value: strconv.Itoa(depth)})
		}
		return true
	})
}
