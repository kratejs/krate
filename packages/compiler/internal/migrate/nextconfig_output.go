package migrate

import (
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/resolver"
)

// exprSource is the source text of an expression: from its start to the next
// top-level comma or closing bracket (quotes and nested brackets respected).
func exprSource(src string, e ast.Expr) string {
	off := e.Pos().Offset
	if off < 0 || off >= len(src) {
		return ""
	}
	src = src[off:]
	depth := 0
	var quote byte
	for i := 0; i < len(src); i++ {
		c := src[i]
		if quote != 0 {
			if c == '\\' {
				i++
			} else if c == quote {
				quote = 0
			}
			continue
		}
		switch c {
		case '\'', '"', '`':
			quote = c
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			if depth == 0 {
				return strings.TrimSpace(src[:i])
			}
			depth--
		case ',':
			if depth == 0 {
				return strings.TrimSpace(src[:i])
			}
		}
	}
	return strings.TrimSpace(src)
}

// staticPathValue evaluates a path-valued next.config property to a clean slash
// path relative to the project ("." for the project itself).
func staticPathValue(rc *ruleCtx, e ast.Expr) (string, bool) {
	v, ok := evalPathExpr(exprSource(rc.src, e))
	if !ok || path.IsAbs(v) || strings.Contains(v, ":") {
		return "", false
	}
	return path.Clean(v), true
}

// onlyParents reports whether p is "..", "../..", and so on, and how many
// levels that is.
func onlyParents(p string) (int, bool) {
	if p == "" || p == "." {
		return 0, false
	}
	n := 0
	for _, seg := range strings.Split(p, "/") {
		if seg != ".." {
			return 0, false
		}
		n++
	}
	return n, true
}

// outputKeys translates output: 'standalone', output: 'export', and
// outputFileTracingRoot.
func outputKeys(rc *ruleCtx, obj *ast.ObjectExpr, out *nextConfigData, notes *nextConfigNotes, done map[string]bool) {
	if v := prop(obj, "outputFileTracingRoot"); v != nil {
		if rel, ok := staticPathValue(rc, v); ok {
			if rel == "." {
				done["outputFileTracingRoot"] = true
				notes.Info = append(notes.Info, "outputFileTracingRoot is the project itself, which is Krate's default; nothing to set")
			} else if depth, up := onlyParents(rel); up {
				out.OutputFileTracingRoot = rel
				out.TracingDepth = depth
				if r, err := filepath.Rel(filepath.Join(rc.dir, filepath.FromSlash(rel)), rc.dir); err == nil {
					out.TracingRel = filepath.ToSlash(r)
				}
				done["outputFileTracingRoot"] = true
			}
		}
	}
	v := prop(obj, "output")
	if v == nil {
		return
	}
	mode, _ := stringValue(v)
	switch mode {
	case "export":
		out.Output = "static"
		done["output"] = true
		notes.Info = append(notes.Info, "output: 'export' became output: \"static\" in krate.config.ts: static pages and GET route handlers are generated at build time into dist/")
		return
	case "standalone":
	default:
		return
	}
	out.DeployTarget = "node"
	done["output"] = true
	dir := "dist/.krate/server/server.mjs"
	if out.TracingRel != "" {
		dir = "dist/.krate/server/" + out.TracingRel + "/server.mjs"
	}
	msg := "output: 'standalone' became deploy.target \"node\": krate deploy builds and writes the server to " + dir + "; run it with node and keep the whole dist/ folder (static files are served from it), like .next/standalone"
	if out.TracingRel != "" {
		msg += ". outputFileTracingRoot makes the output mirror the repository, so files read by a path relative to the app directory (../sibling/dist/x.mjs) are found; the server starts in its own directory like Next's standalone server"
	}
	notes.Info = append(notes.Info, msg)
	if out.TracingRel != "" {
		notes.Info = append(notes.Info, "krate deploy --target docker builds from the project directory and does not copy files outside it; for a monorepo image use the node target inside your own Dockerfile (copy dist/ and run node dist/.krate/server/"+out.TracingRel+"/server.mjs)")
	}
}

// pkgState describes how a transpilePackages entry resolves.
type pkgState struct {
	name   string
	kind   string // "source", "compiled" or "" when unresolved
	reason string
}

func resolveTranspile(dir, name string) pkgState {
	st := pkgState{name: name}
	file := resolver.NodeModuleWith(dir, name, resolver.ClientConditions(false))
	if file == "" {
		file = resolver.NodeModuleWith(dir, name, resolver.ServerConditions(false))
	}
	if file != "" {
		st.kind = "source"
		if strings.Contains(filepath.ToSlash(file), "/node_modules/") && resolver.IsCompiledDependency(file) {
			st.kind = "compiled"
		}
		return st
	}
	for d := dir; ; {
		pj := filepath.Join(d, "node_modules", filepath.FromSlash(name), "package.json")
		if data, err := os.ReadFile(pj); err == nil {
			if subpathOnlyExports(filepath.Dir(pj), data) {
				st.kind = "source"
				return st
			}
			st.reason = "it is installed but its package.json main/exports point at a file that does not exist (build the package, for example its dist/ folder, then migrate again)"
			return st
		}
		parent := filepath.Dir(d)
		if parent == d {
			break
		}
		d = parent
	}
	st.reason = "it is not installed (run the package manager's install at the workspace root, then migrate again)"
	return st
}

// subpathOnlyExports reports whether a package.json exposes only subpaths
// (`"exports": {"./*": "./src/*.ts"}`) whose targets exist. Such a package has
// no bare entry point and is imported through its subpaths.
func subpathOnlyExports(dir string, pkgJSON []byte) bool {
	var pkg struct {
		Main    string          `json:"main"`
		Exports json.RawMessage `json:"exports"`
	}
	if json.Unmarshal(pkgJSON, &pkg) != nil || pkg.Main != "" || len(pkg.Exports) == 0 {
		return false
	}
	var exports map[string]json.RawMessage
	if json.Unmarshal(pkg.Exports, &exports) != nil || len(exports) == 0 {
		return false
	}
	for key, raw := range exports {
		if key == "." || !strings.HasPrefix(key, "./") {
			return false
		}
		var target string
		if json.Unmarshal(raw, &target) != nil {
			continue
		}
		base := target
		if i := strings.IndexByte(base, '*'); i >= 0 {
			base = base[:i]
		}
		if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(base))); err != nil {
			return false
		}
	}
	return true
}

// transpileKeys checks that every transpilePackages entry resolves. Krate
// compiles workspace TypeScript source and ships compiled packages as they
// are, so the key itself has no effect; only a package that cannot be found
// needs attention.
func transpileKeys(rc *ruleCtx, obj *ast.ObjectExpr, notes *nextConfigNotes, done map[string]bool) {
	v := prop(obj, "transpilePackages")
	if v == nil {
		return
	}
	names, ok := stringArray(v)
	if !ok {
		return
	}
	done["transpilePackages"] = true
	resolver.ResetCaches()
	var source, compiled int
	var bad []pkgState
	for _, n := range names {
		switch st := resolveTranspile(rc.dir, n); st.kind {
		case "source":
			source++
		case "compiled":
			compiled++
		default:
			bad = append(bad, st)
		}
	}
	if source+compiled > 0 {
		var parts []string
		if source > 0 {
			parts = append(parts, fmt.Sprintf("%d resolve to source that Krate compiles without config", source))
		}
		if compiled > 0 {
			parts = append(parts, fmt.Sprintf("%d resolve to compiled packages", compiled))
		}
		notes.Info = append(notes.Info, fmt.Sprintf("transpilePackages: %d of %d packages checked; %s", source+compiled, len(names), strings.Join(parts, ", ")))
	}
	for _, st := range bad {
		notes.Manual = append(notes.Manual, ManualItem{
			Source: rc.file,
			Reason: fmt.Sprintf("transpilePackages lists %q, which does not resolve from the project: %s", st.name, st.reason),
			Hint:   "Krate needs no transpilePackages key, but the package must be importable; install or build it so node resolution finds it",
		})
	}
}

// turbopackKeys handles the next.config turbopack object.
func turbopackKeys(rc *ruleCtx, obj *ast.ObjectExpr, out *nextConfigData, notes *nextConfigNotes, done map[string]bool) {
	tp, ok := prop(obj, "turbopack").(*ast.ObjectExpr)
	if !ok {
		return
	}
	done["turbopack"] = true
	var keys []string
	for _, p := range tp.Properties {
		if p == nil || p.Spread || p.Computed {
			notes.Manual = append(notes.Manual, ManualItem{Source: rc.file, Reason: "turbopack spreads or computes keys; they were not read", Hint: "Krate has no turbopack settings; custom build steps go in a plugin (see Plugins)"})
			return
		}
		keys = append(keys, p.Key)
	}
	sort.Strings(keys)
	for _, k := range keys {
		v := prop(tp, k)
		switch k {
		case "root":
			rel, ok := staticPathValue(rc, v)
			switch {
			case ok && (rel == "." || rel == out.OutputFileTracingRoot || (out.EnvDir != "" && rel == path.Clean(out.EnvDir))):
				notes.Info = append(notes.Info, "turbopack.root is the same directory as the project, outputFileTracingRoot or envDir; Krate resolves workspace packages by node resolution, so nothing is set")
			default:
				notes.Manual = append(notes.Manual, ManualItem{Source: rc.file, Reason: "turbopack.root is not the project, the tracing root or the envDir root, or could not be resolved statically", Hint: "Krate resolves workspace packages by node resolution from the project; check that everything the app imports is installed or linked there"})
			}
		case "resolveAlias":
			notes.Manual = append(notes.Manual, ManualItem{Source: rc.file, Reason: "turbopack.resolveAlias is not translated", Hint: aliasHint(v)})
		case "rules":
			notes.Manual = append(notes.Manual, ManualItem{Source: rc.file, Reason: "turbopack.rules (webpack loaders) are not translated", Hint: "loaders are arbitrary build steps; implement the transform as a Krate plugin (see Plugins)"})
		default:
			notes.Warnings = append(notes.Warnings, "next.config turbopack."+k+" is not used")
		}
	}
}

// aliasHint advises a tsconfig paths entry for a static resolveAlias object.
func aliasHint(v ast.Expr) string {
	const base = "Krate reads tsconfig.json paths: add "
	o, ok := v.(*ast.ObjectExpr)
	if !ok {
		return "Krate reads tsconfig.json paths; add one entry per alias there"
	}
	var parts []string
	for _, p := range o.Properties {
		if p == nil || p.Spread || p.Computed {
			continue
		}
		if s, ok := stringValue(p.Value); ok {
			parts = append(parts, fmt.Sprintf("%q: [%q]", p.Key, s))
		}
	}
	if len(parts) == 0 {
		return "Krate reads tsconfig.json paths; add one entry per alias there"
	}
	return base + "compilerOptions.paths { " + strings.Join(parts, ", ") + " } (relative to baseUrl)"
}
