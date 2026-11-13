package migrate

import (
	"path"
	"strings"

	"github.com/kratejs/krate/packages/compiler/ast"
)

// serverFileKeys carries next.config serverExternalPackages and
// outputFileTracingIncludes, when they are static, to the Krate keys of the
// same name. It returns the keys it translated; a computed value stays a
// manual item from translateOtherKeys.
func serverFileKeys(obj *ast.ObjectExpr, out *nextConfigData) map[string]bool {
	done := map[string]bool{}
	if v := prop(obj, "serverExternalPackages"); v != nil {
		if pkgs, ok := stringArray(v); ok {
			out.ServerExternalPackages = pkgs
			done["serverExternalPackages"] = true
		}
	}
	if v := prop(obj, "outputFileTracingIncludes"); v != nil {
		if inc, ok := staticTraceIncludes(v); ok {
			out.OutputFileTracingIncludes = inc
			done["outputFileTracingIncludes"] = true
		}
	}
	return done
}

// staticTraceIncludes reads { '/route': ['./glob'] }. Globs are made
// project-relative ("./data/**" -> "data/**"); one that leaves the project is
// not translated.
func staticTraceIncludes(e ast.Expr) (map[string][]string, bool) {
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
			if g == "." || g == ".." || strings.HasPrefix(g, "../") || strings.HasPrefix(g, "/") {
				return nil, false
			}
			out[p.Key] = append(out[p.Key], g)
		}
	}
	return out, true
}
