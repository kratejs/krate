package migrate

import (
	"path"
	"strings"

	"github.com/kratejs/krate/packages/compiler/ast"
)

// serverFileKeys carries next.config serverExternalPackages and
// outputFileTracingIncludes, when they are static, to the Krate keys of the
// same name, and handles the output, tracing root, transpilePackages and
// turbopack keys (nextconfig_output.go). It returns the keys it translated; a
// computed value stays a manual item from translateOtherKeys.
func serverFileKeys(rc *ruleCtx, obj *ast.ObjectExpr, out *nextConfigData, notes *nextConfigNotes) map[string]bool {
	done := map[string]bool{}
	if v := prop(obj, "serverExternalPackages"); v != nil {
		if pkgs, ok := stringArray(v); ok {
			out.ServerExternalPackages = pkgs
			done["serverExternalPackages"] = true
		}
	}
	outputKeys(rc, obj, out, notes, done)
	if v := prop(obj, "outputFileTracingIncludes"); v != nil {
		if inc, ok := staticTraceIncludes(v, out.TracingDepth); ok {
			out.OutputFileTracingIncludes = inc
			done["outputFileTracingIncludes"] = true
			if out.TracingDepth > 0 && len(inc) > 1 {
				notes.Info = append(notes.Info, "outputFileTracingIncludes keeps its route keys, but the node deploy output is one server: it copies the files of every route")
			}
		}
	}
	transpileKeys(rc, obj, notes, done)
	turbopackKeys(rc, obj, out, notes, done)
	return done
}

// staticTraceIncludes reads { '/route': ['./glob'] }. Globs are made
// project-relative ("./data/**" -> "data/**"). A glob that leaves the project
// is translated only when the outputFileTracingRoot was (depth > 0, the number
// of levels from the project up to the root) and the glob stays inside it.
func staticTraceIncludes(e ast.Expr, depth int) (map[string][]string, bool) {
	o, ok := e.(*ast.ObjectExpr)
	if !ok {
		return nil, false
	}
	out := map[string][]string{}
	for _, p := range o.Properties {
		if p == nil || p.Spread || p.Computed {
			return nil, false
		}
		globs, ok := stringArray(p.Value)
		if !ok {
			return nil, false
		}
		for _, g := range globs {
			g = path.Clean(strings.ReplaceAll(g, "\\", "/"))
			if g == "." || g == ".." || strings.HasPrefix(g, "/") {
				return nil, false
			}
			if strings.HasPrefix(g, "../") {
				ups := 0
				for _, seg := range strings.Split(g, "/") {
					if seg != ".." {
						break
					}
					ups++
				}
				if ups > depth {
					return nil, false
				}
			}
			out[p.Key] = append(out[p.Key], g)
		}
	}
	return out, true
}
