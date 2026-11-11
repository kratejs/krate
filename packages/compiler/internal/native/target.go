package native

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/bundler"
	"github.com/kratejs/krate/packages/compiler/internal/config"
	"github.com/kratejs/krate/packages/compiler/internal/diag"
	"github.com/kratejs/krate/packages/compiler/internal/gohost"
	"github.com/kratejs/krate/packages/compiler/internal/tsstrip"
)

// configFile returns the display name of the project's config file.
func configFile(root string) string {
	for _, n := range []string{"krate.config.ts", "krate.config.js", "krate.config.mjs", "krate.config.json"} {
		if _, err := os.Stat(filepath.Join(root, n)); err == nil {
			return n
		}
	}
	return "krate.config.ts"
}

func configDiag(root string, code diag.Code, msg, hint string) diag.Diagnostic {
	return diag.New(configFile(root), 1, 1, msg, hint, "").WithCode(code)
}

// CheckTarget reports what a native build cannot do: request-time rendering,
// API routes, middleware, runtime components and server actions. Each kind has
// its own code.
func CheckTarget(root string, cfg *config.Config) ([]diag.Diagnostic, error) {
	var out []diag.Diagnostic

	if cfg.SSR.Streaming {
		out = append(out, configDiag(root, diag.CodeNativeServerRendering, "ssr.streaming is set, but a native build has no server",
			"remove ssr.streaming: every route is prerendered in a native build; load per-request data from host code"))
	}
	if cfg.PPR {
		out = append(out, configDiag(root, diag.CodeNativeServerRendering, "ppr is set, but a native build has no server",
			"remove ppr: every route is prerendered in a native build"))
	}
	if len(cfg.RuntimeComponents) > 0 {
		out = append(out, configDiag(root, diag.CodeNativeRuntimeComponent, "runtimeComponents is set, but a native build renders no component per request",
			"remove runtimeComponents, or use serverComponents for build-time rendering"))
	}
	if len(cfg.RuntimeDirs) > 0 {
		out = append(out, configDiag(root, diag.CodeNativeRuntimeComponent, "runtimeDirs is set, but a native build renders no component per request",
			"remove runtimeDirs, or use serverDirs for build-time rendering"))
	}

	for _, name := range []string{"middleware.ts", "middleware.js", filepath.Join("src", "middleware.ts"), filepath.Join("src", "middleware.js")} {
		if _, err := os.Stat(filepath.Join(root, name)); err == nil {
			out = append(out, diag.New(filepath.ToSlash(name), 1, 1, "middleware runs in front of a server, and a native build has no server",
				"remove "+filepath.ToSlash(name)+"; do the check in host code or in the UI", "").WithCode(diag.CodeNativeMiddleware))
		}
	}

	files, err := projectFiles(root, cfg)
	if err != nil {
		return nil, err
	}
	apiDir := filepath.Join(root, filepath.FromSlash(gohost.DefaultAPIDir)) + string(filepath.Separator)
	pagesDir := cfg.PagesDir
	if pagesDir != "" && !filepath.IsAbs(pagesDir) {
		pagesDir = filepath.Join(root, pagesDir)
	}
	for _, f := range files {
		rel := relPath(root, f)
		if strings.HasPrefix(f, apiDir) {
			base := filepath.Base(f)
			if strings.HasPrefix(base, "_") || strings.HasSuffix(base, "_test.go") {
				continue
			}
			out = append(out, diag.New(rel, 1, 1, "API route "+rel+" has nothing to serve it: a native build has no HTTP server",
				"move the logic into host code (a .go file or an @host function) and call it from the UI", "").WithCode(diag.CodeNativeAPIRoute))
			continue
		}
		if !isScript(f) {
			continue
		}
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		inPages := pagesDir != "" && strings.HasPrefix(f, pagesDir+string(filepath.Separator))
		out = append(out, checkFileTarget(root, f, string(data), cfg, inPages)...)
	}
	return out, nil
}

func checkFileTarget(root, path, src string, cfg *config.Config, inPages bool) []diag.Diagnostic {
	rel := relPath(root, path)
	var out []diag.Diagnostic
	runtimeHint := "use @server (build-time rendering) or a client component that loads its data from host code"

	for _, t := range bundler.FileTierSources(src, path, cfg.ServerComponents, cfg.RuntimeComponents, cfg.ServerDirs, cfg.RuntimeDirs) {
		if t.Class == bundler.ComponentClassRuntime {
			out = append(out, mkDiag(rel, src, t.Line, 1, diag.CodeNativeRuntimeComponent,
				"runtime component: "+t.What+" makes this file render per request, and a native build has no server", runtimeHint))
		}
	}

	needParse := inPages || strings.Contains(src, "@runtime") || strings.Contains(src, "use server") || strings.Contains(src, "async")
	if !needParse {
		return out
	}
	prog, errs := tsstrip.Parse(path, src, tsstrip.Options{})
	if prog == nil || len(errs) > 0 {
		return out
	}
	if prog.Tier == "runtime" {
		out = append(out, mkDiag(rel, src, 1, 1, diag.CodeNativeRuntimeComponent,
			"runtime component: the @runtime decorator makes this file render per request, and a native build has no server", runtimeHint))
	}
	if hasUseServer(prog.Body) {
		out = append(out, mkDiag(rel, src, 1, 1, diag.CodeNativeServerAction,
			"server actions: this \"use server\" module is called through a server, and a native build has no server",
			"call an @host function (or a Go function) from the UI instead of a server action"))
	}
	for _, s := range collectSites(prog) {
		if s.decorator == "runtime" || bodyMarker(s.body) == "runtime" {
			out = append(out, mkDiag(rel, src, s.pos.Line, s.pos.Col, diag.CodeNativeRuntimeComponent,
				s.name+" is a runtime component, and a native build has no server", runtimeHint))
		} else if s.top && s.async && isComponent(s) && s.decorator == "" && bodyMarker(s.body) == "" {
			out = append(out, mkDiag(rel, src, s.pos.Line, s.pos.Col, diag.CodeNativeRuntimeComponent,
				s.name+" is an async component, which renders per request, and a native build has no server",
				"make it synchronous and load data from host code, or use @server for build-time rendering"))
		}
		if hasUseServer(s.body) {
			out = append(out, mkDiag(rel, src, s.pos.Line, s.pos.Col, diag.CodeNativeServerAction,
				"server actions: "+s.name+" has a \"use server\" directive, and a native build has no server",
				"call an @host function (or a Go function) from the UI instead of a server action"))
		}
	}
	if inPages {
		out = append(out, pageRenderOptions(rel, src, prog)...)
	}
	return out
}

// pageRenderOptions finds the page exports that ask for request-time rendering.
func pageRenderOptions(rel, src string, prog *ast.Program) []diag.Diagnostic {
	var out []diag.Diagnostic
	hint := "remove it: every route is prerendered in a native build; load per-request data from host code"
	report := func(pos ast.Pos, what string) {
		out = append(out, mkDiag(rel, src, pos.Line, pos.Col, diag.CodeNativeServerRendering,
			what+" asks for request-time rendering, and a native build has no server", hint))
	}
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
			if d == nil || d.Init == nil {
				continue
			}
			switch d.Name {
			case "dynamic":
				if lit, ok := d.Init.(*ast.Literal); ok && lit.Kind == ast.StringLit && lit.Value == "force-dynamic" {
					report(exp.Position, "`export const dynamic = \"force-dynamic\"`")
				}
			case "revalidate":
				if !staticRevalidate(d.Init) {
					report(exp.Position, "`export const revalidate`")
				}
			case "config":
				obj, ok := d.Init.(*ast.ObjectExpr)
				if !ok {
					continue
				}
				for _, p := range obj.Properties {
					if p == nil {
						continue
					}
					switch p.Key {
					case "ssr", "isr", "streaming":
						if lit, ok := p.Value.(*ast.Literal); ok && lit.Value == "true" {
							report(exp.Position, "`export const config = { "+p.Key+": true }`")
						}
					case "revalidate":
						if lit, ok := p.Value.(*ast.Literal); ok {
							if n, err := strconv.Atoi(lit.Value); err == nil && n > 0 {
								report(exp.Position, "`export const config = { revalidate }`")
							}
						}
					}
				}
			}
		}
	}
	return out
}

// staticRevalidate reports whether a revalidate export keeps the page static
// (`false` or `Infinity`).
func staticRevalidate(e ast.Expr) bool {
	switch x := e.(type) {
	case *ast.Literal:
		return x.Value == "false"
	case *ast.Identifier:
		return x.Name == "Infinity"
	}
	return false
}

// CheckWebBuild reports host markers in a web build: host code only runs in a
// native build. It reads only files that can carry a marker.
func CheckWebBuild(root string, cfg *config.Config) ([]diag.Diagnostic, error) {
	var out []diag.Diagnostic
	hint := "host code only runs in a native build: remove the marker, or build with `krate native`"
	if len(cfg.HostFunctions) > 0 {
		out = append(out, configDiag(root, diag.CodeHostInWebBuild, "hostFunctions is set, but this is a web build", hint))
	}
	if len(cfg.HostDirs) > 0 {
		out = append(out, configDiag(root, diag.CodeHostInWebBuild, "hostDirs is set, but this is a web build", hint))
	}
	files, err := projectFiles(root, cfg)
	if err != nil {
		return nil, err
	}
	for _, f := range files {
		if !isScript(f) {
			continue
		}
		rel := relPath(root, f)
		if IsHostFileName(f) {
			out = append(out, diag.New(rel, 1, 1, "the `.host` file name marks host code, but this is a web build", hint, "").WithCode(diag.CodeHostInWebBuild))
			continue
		}
		data, err := os.ReadFile(f)
		if err != nil || !strings.Contains(string(data), "@host") {
			continue
		}
		src := string(data)
		if bundler.HasDirective(src, hostDirectiveName) {
			out = append(out, mkDiag(rel, src, directiveLine(src), 1, diag.CodeHostInWebBuild, "the `// @host` comment marks host code, but this is a web build", hint))
			continue
		}
		prog, errs := tsstrip.Parse(f, src, tsstrip.Options{})
		if prog == nil || len(errs) > 0 {
			continue
		}
		for _, s := range collectSites(prog) {
			if s.decorator == "host" || bodyMarker(s.body) == "host" {
				out = append(out, mkDiag(rel, src, s.pos.Line, s.pos.Col, diag.CodeHostInWebBuild,
					s.name+" is marked @host, but this is a web build", hint))
			}
		}
	}
	return out, nil
}
