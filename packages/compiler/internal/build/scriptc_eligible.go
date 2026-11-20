package build

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/bundler"
)

// What a route must not use to run on the scriptc server. Each entry is
// something the server does not provide, so a route that compiled would run
// without it and misbehave. A route that uses one falls back to the embedded
// runtime. Every entry must be removed when the server gains the feature:
// keep this list the single place that says what the server lacks.
var (
	// scriptcUnsupportedImports are module specifiers, or prefixes ending in
	// "/", of Krate runtime and server modules and of Next.js, whose helpers
	// need the request store the server does not have.
	scriptcUnsupportedImports = []string{"@krate/", "next/"}
	// scriptcUnsupportedMembers are properties of a request.
	scriptcUnsupportedMembers = map[string]string{
		"cookies": "request.cookies",
		"nextUrl": "request.nextUrl",
	}
	// scriptcUnsupportedCalls are called by bare name: the request-store
	// helpers, after() and the revalidation functions.
	scriptcUnsupportedCalls = map[string]string{
		"after":          "after()",
		"revalidatePath": "revalidatePath()",
		"revalidateTag":  "revalidateTag()",
		"cookies":        "cookies()",
		"headers":        "headers()",
		"draftMode":      "draftMode()",
	}
)

// scriptcFinding is the first use of something the scriptc server lacks.
type scriptcFinding struct {
	file      string // absolute path of the module that uses it
	line, col int
	what      string
}

func scriptcUnsupportedImport(spec string) bool {
	for _, p := range scriptcUnsupportedImports {
		if spec == strings.TrimSuffix(p, "/") || strings.HasPrefix(spec, p) {
			return true
		}
	}
	return false
}

// scriptcRouteFinding scans a route module and the local modules it imports,
// transitively under root, for what the scriptc server lacks. aliases (may be
// nil) follows the project's tsconfig path aliases. It returns nil for a route
// that uses none of it.
func scriptcRouteFinding(root, file string, aliases *bundler.ImportResolver) *scriptcFinding {
	seen := map[string]bool{}
	var scan func(file string) *scriptcFinding
	scan = func(file string) *scriptcFinding {
		if seen[file] {
			return nil
		}
		seen[file] = true
		data, err := os.ReadFile(file)
		if err != nil {
			return nil
		}
		prog, err := parseSourceFile(file, data)
		if err != nil || prog == nil {
			return nil
		}
		var found *scriptcFinding
		set := func(pos ast.Pos, what string) {
			if found == nil {
				found = &scriptcFinding{file: file, line: pos.Line, col: pos.Col, what: what}
			}
		}
		var local []string
		visit := func(spec string, pos ast.Pos) {
			spec = strings.Trim(spec, "\"'`")
			switch {
			case scriptcUnsupportedImport(spec):
				set(pos, "it imports "+spec+", which needs the request store the scriptc server lacks")
			case strings.HasPrefix(spec, "."):
				if p := resolveLocalModule(filepath.Dir(file), spec); p != "" && scriptcUnderRoot(root, p) {
					local = append(local, p)
				}
			default:
				if p := aliases.ResolveAlias(spec); p != "" && scriptcUnderRoot(root, p) {
					local = append(local, p)
				}
			}
		}
		for _, stmt := range prog.Body {
			switch s := stmt.(type) {
			case *ast.ImportStmt:
				visit(s.Source, s.Position)
			case *ast.ExportStmt:
				if s.ReexportSource != "" {
					visit(s.ReexportSource, s.Position)
				}
			}
		}
		bundler.WalkStmts(prog.Body, func(n any, _ bool) bool {
			switch e := n.(type) {
			case *ast.MemberExpr:
				if id, ok := e.Property.(*ast.Identifier); ok && !e.Computed {
					if what, bad := scriptcUnsupportedMembers[id.Name]; bad {
						set(id.Position, "it uses "+what+", which the scriptc server does not provide")
					}
				}
			case *ast.CallExpr:
				if id, ok := e.Callee.(*ast.Identifier); ok {
					if what, bad := scriptcUnsupportedCalls[id.Name]; bad {
						set(id.Position, "it calls "+what+", which the scriptc server does not provide")
					}
				}
			}
			return true
		})
		if found != nil {
			return found
		}
		for _, p := range local {
			if f := scan(p); f != nil {
				return f
			}
		}
		return nil
	}
	return scan(file)
}

// resolveLocalModule resolves a relative import specifier from dir to a source
// file, trying the extensions and index files a bundler would.
func resolveLocalModule(dir, spec string) string {
	base := filepath.Join(dir, filepath.FromSlash(spec))
	exts := []string{".ts", ".tsx", ".js", ".jsx", ".mts", ".mjs"}
	var cands []string
	if filepath.Ext(base) != "" {
		cands = append(cands, base)
		// An import written with .js can name a .ts file.
		trimmed := strings.TrimSuffix(base, filepath.Ext(base))
		for _, e := range exts {
			cands = append(cands, trimmed+e)
		}
	}
	for _, e := range exts {
		cands = append(cands, base+e, filepath.Join(base, "index"+e))
	}
	for _, c := range cands {
		if fi, err := os.Stat(c); err == nil && !fi.IsDir() {
			return c
		}
	}
	return ""
}

func scriptcUnderRoot(root, p string) bool {
	rel, err := filepath.Rel(root, p)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) &&
		!strings.Contains(filepath.ToSlash(rel), "node_modules/")
}
