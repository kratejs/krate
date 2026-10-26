package build

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/evanw/esbuild/pkg/api"
	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/diag"
	"github.com/kratejs/krate/packages/compiler/internal/fsutil"
	"github.com/kratejs/krate/packages/compiler/internal/irtree"
	"github.com/kratejs/krate/packages/compiler/internal/jsruntime"
)

// assets/preact-render-to-string.js is vendored from preact-render-to-string
// 6.8.1 (MIT); see assets/preact-render-to-string.LICENSE.
//
//go:embed assets/preact-render-to-string.js
var islandRenderToStringJS string

// entryExternalImports collects the page's bare-package import bindings. These
// are re-emitted into an external island module so its JSX subtree resolves.
func entryExternalImports(prog *ast.Program) (map[string]irtree.ExternalImport, []irtree.ExternalImport, map[string]bool) {
	byLocal := map[string]irtree.ExternalImport{}
	var list []irtree.ExternalImport
	locals := map[string]bool{}
	seen := map[string]bool{}
	add := func(imp irtree.ExternalImport) {
		if imp.Local == "" || imp.Source == "" {
			return
		}
		key := imp.Source + "\x00" + imp.Local
		if seen[key] {
			return
		}
		seen[key] = true
		byLocal[imp.Local] = imp
		list = append(list, imp)
	}
	if prog == nil {
		return byLocal, nil, nil
	}
	for _, stmt := range prog.Body {
		imp, ok := stmt.(*ast.ImportStmt)
		if !ok {
			continue
		}
		src := strings.Trim(imp.Source, "\"'")
		if !isBarePackageImport(src) {
			// Relative/local imports cannot be re-emitted into an external
			// island; remember their bindings so the builder can warn.
			if strings.HasPrefix(src, ".") || strings.HasPrefix(src, "/") {
				if imp.Default != "" {
					locals[imp.Default] = true
				}
				if imp.Namespace != "" {
					locals[imp.Namespace] = true
				}
				for _, n := range imp.Named {
					local := n.Local
					if local == "" {
						local = n.Remote
					}
					locals[local] = true
				}
			}
			continue
		}
		if isCompatAliased(src) {
			// `react*` and Radix helpers compile against @krate/runtime directly;
			// they are not external islands.
			continue
		}
		if imp.Default != "" {
			add(irtree.ExternalImport{Source: src, Local: imp.Default, Default: true})
		}
		if imp.Namespace != "" {
			add(irtree.ExternalImport{Source: src, Local: imp.Namespace, Namespace: true})
		}
		for _, n := range imp.Named {
			local := n.Local
			if local == "" {
				local = n.Remote
			}
			add(irtree.ExternalImport{Source: src, Local: local, Remote: n.Remote})
		}
	}
	return byLocal, list, locals
}

// entryLocalImports collects relative JS/TS imports from the page module. They
// are re-emitted into an island so local React-compatible components used as
// tags inside an external subtree can be bundled with it.
func entryLocalImports(prog *ast.Program) []irtree.ExternalImport {
	if prog == nil {
		return nil
	}
	var out []irtree.ExternalImport
	seen := map[string]bool{}
	add := func(imp irtree.ExternalImport) {
		if imp.Local == "" || imp.Source == "" {
			return
		}
		key := imp.Source + "\x00" + imp.Local
		if seen[key] {
			return
		}
		seen[key] = true
		out = append(out, imp)
	}
	for _, stmt := range prog.Body {
		imp, ok := stmt.(*ast.ImportStmt)
		if !ok {
			continue
		}
		src := strings.Trim(imp.Source, "\"'")
		if !strings.HasPrefix(src, ".") && !strings.HasPrefix(src, "/") {
			continue
		}
		if isStyleAsset(src) {
			continue
		}
		if imp.Default != "" {
			add(irtree.ExternalImport{Source: src, Local: imp.Default, Default: true})
		}
		if imp.Namespace != "" {
			add(irtree.ExternalImport{Source: src, Local: imp.Namespace, Namespace: true})
		}
		for _, n := range imp.Named {
			local := n.Local
			if local == "" {
				local = n.Remote
			}
			add(irtree.ExternalImport{Source: src, Local: local, Remote: n.Remote})
		}
	}
	return out
}

func isStyleAsset(src string) bool {
	switch strings.ToLower(filepath.Ext(src)) {
	case ".css", ".scss", ".sass", ".less", ".styl", ".svg", ".png", ".jpg", ".jpeg", ".gif", ".webp", ".avif":
		return true
	}
	return false
}

// isCompatAliased reports whether a bare specifier resolves to Krate's own
// compatibility runtime (react/react-dom/jsx-runtime and the Radix slot helper)
// rather than to an external island.
func isCompatAliased(src string) bool {
	switch src {
	case "react", "react-dom", "react-dom/client",
		"react/jsx-runtime", "react/jsx-dev-runtime",
		"@radix-ui/react-slot":
		return true
	}
	return false
}

// isBarePackageImport reports whether src is a bare npm specifier (not a
// relative path and not a Krate virtual/workspace package).
func isBarePackageImport(src string) bool {
	if src == "" {
		return false
	}
	if strings.HasPrefix(src, ".") || strings.HasPrefix(src, "/") {
		return false
	}
	if strings.HasPrefix(src, "krate/") || strings.HasPrefix(src, "@krate/") {
		return false
	}
	return true
}

// buildIslands bundles each external subtree as its own Preact island chunk.
// It also attempts a server render for islands whose bridged props have known
// initial values, returning the HTML to inject into each placeholder.
//
// resolveDir is the page module directory: relative local imports used inside
// an island resolve from there, not from the project root.
func (b *Builder) buildIslands(exts []irtree.ExternalComponent, imports, localImports []irtree.ExternalImport, resolveDir string) (string, string, map[string]string, error) {
	if len(exts) == 0 {
		return "", "", nil, nil
	}
	if resolveDir == "" {
		resolveDir = b.Root
	}

	if _, err := b.ensurePreactShared(); err != nil {
		return "", "", nil, diag.Diagnostic{
			Message: "island runtime could not be built: " + err.Error(),
			Hint:    "install Preact 10.20 or newer with `pnpm add preact`, then rebuild",
		}.WithCode(diag.CodeIslandAsset)
	}
	if v := installedPreactVersion(b.Root); v != "" && versionLess(v, "10.20.0") {
		fmt.Fprintf(os.Stderr, "  %sWarning:%s preact %s is older than 10.20.0; islands use hooks and compat APIs that older releases can break\n", cYellow, cReset, v)
	}
	var boot strings.Builder
	var importsJS strings.Builder
	ssrHTML := map[string]string{}
	allImports := append(append([]irtree.ExternalImport(nil), imports...), localImports...)

	for _, e := range exts {
		if e.Subtree == "" {
			continue
		}
		used := islandImportsFor(e.Subtree, allImports)
		clientSrc := b.islandClientSource(e, used)
		out, err := b.buildIslandBundle(clientSrc, resolveDir, false, nil)
		if err != nil {
			return "", "", nil, diag.Diagnostic{
				File:    e.File,
				Line:    e.Line,
				Col:     e.Col,
				Message: "island " + e.ID + " (<" + e.Local + ">) could not be built: " + err.Error(),
				Hint:    "check the island package and its local imports, then rebuild",
			}.WithCode(diag.CodeIslandAsset)
		}
		name := "island." + hashContent(out) + ".js"
		chunksDir := filepath.Join(b.Cfg.OutDir, "chunks")
		if err := os.MkdirAll(chunksDir, 0755); err != nil {
			return "", "", nil, fmt.Errorf("creating chunks dir: %w", err)
		}
		if err := fsutil.WriteFileIfChanged(filepath.Join(chunksDir, name), out, 0644); err != nil {
			return "", "", nil, fmt.Errorf("writing island chunk: %w", err)
		}
		url := "/chunks/" + name
		if bp := b.Cfg.BaseURLPath(); bp != "" {
			url = bp + url
		}
		fmt.Fprintf(&importsJS, "import(%q).catch(function(e){ console.error(\"krate external island:\", e); });\n", url)

		if !e.SSRPropsKnown {
			printIslandWarning(e, diag.CodeIslandClientOnly, "island "+e.ID+" (<"+e.Local+">) stays client-only: a bridged prop has no build-time initial value", "pass build-time initial values, or keep the island client-only intentionally")
			continue
		}
		ssrSrc := b.islandSSRSource(e, used)
		html, err := b.buildIslandSSR(ssrSrc, resolveDir)
		if err != nil {
			printIslandWarning(e, diag.CodeIslandClientOnly, "island "+e.ID+" stays client-only: server render failed: "+err.Error(), "fix the browser-only dependency or keep the island client-only intentionally")
			continue
		}
		ssrHTML[e.ID] = html
	}

	return boot.String(), importsJS.String(), ssrHTML, nil
}

// printIslandWarning emits a source-located island diagnostic. The build
// package owns the asset failure, but the source position comes from the IR.
func printIslandWarning(e irtree.ExternalComponent, code diag.Code, msg, hint string) {
	d := diag.Diagnostic{
		File: e.File, Line: e.Line, Col: e.Col,
		Message: msg, Hint: hint, Severity: diag.SeverityWarning,
	}.WithCode(code)
	fmt.Fprintf(os.Stderr, "  %sWarning:%s %s\n", cYellow, cReset, d.Error())
	if d.Hint != "" {
		fmt.Fprintf(os.Stderr, "      hint: %s\n", d.Hint)
	}
}

// islandImportsFor keeps only bindings referenced as JSX tags in the subtree.
// The external root is always present, and local components used as tags are
// included so esbuild can compile them into the island.
func islandImportsFor(subtree string, imports []irtree.ExternalImport) []irtree.ExternalImport {
	var out []irtree.ExternalImport
	seen := map[string]bool{}
	for _, imp := range imports {
		if imp.Local == "" || !strings.Contains(subtree, "<"+imp.Local) {
			continue
		}
		key := imp.Source + "\x00" + imp.Local
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, imp)
	}
	return out
}

func writeIslandImportLines(sb *strings.Builder, imports []irtree.ExternalImport) {
	for _, imp := range imports {
		switch {
		case imp.Namespace:
			fmt.Fprintf(sb, "import * as %s from %q;\n", imp.Local, imp.Source)
		case imp.Default:
			fmt.Fprintf(sb, "import %s from %q;\n", imp.Local, imp.Source)
		default:
			if imp.Remote != "" && imp.Remote != imp.Local {
				fmt.Fprintf(sb, "import { %s as %s } from %q;\n", imp.Remote, imp.Local, imp.Source)
			} else {
				fmt.Fprintf(sb, "import { %s } from %q;\n", imp.Local, imp.Source)
			}
		}
	}
}

const islandBoundaryJS = `
class KrateIslandBoundary extends Component {
  constructor(p) { super(p); this.state = { failed: false }; }
  static getDerivedStateFromError() { return { failed: true }; }
  componentDidCatch(e) { if (this.props && typeof this.props.onError === "function") this.props.onError(e); }
  render() {
    if (this.state.failed) return h("span", { "data-krate-island-error": "", role: "alert" }, "This component could not load.");
    return this.props.children;
  }
}
`

func (b *Builder) islandClientSource(e irtree.ExternalComponent, imports []irtree.ExternalImport) string {
	var src strings.Builder
	src.WriteString("import { Component, Fragment, h, hydrate, render } from \"preact\";\n")
	writeIslandImportLines(&src, imports)
	src.WriteString(islandBoundaryJS)
	fmt.Fprintf(&src, "\n{\n  const el = document.querySelector('[data-krate-island=%q]');\n", e.ID)
	src.WriteString("  if (el) {\n    try {\n")
	if e.PropsJS != "" {
		fmt.Fprintf(&src, "      const getProps=(globalThis.__krate_island_values||{})[%q];\n", e.ID)
		fmt.Fprintf(&src, "      const subscribe=(globalThis.__krate_island_subscribe||{})[%q];\n", e.ID)
		src.WriteString("      class KrateIslandData extends Component {\n")
		src.WriteString("        constructor(p){super(p);this.state={v:0};}\n")
		src.WriteString("        componentDidMount(){var self=this;if(subscribe)this._off=subscribe(function(){self.setState({v:self.state.v+1});});}\n")
		src.WriteString("        componentWillUnmount(){if(this._off)this._off();}\n")
		fmt.Fprintf(&src, "        render(){var props=typeof getProps==='function'?getProps():{};return (%s);}\n", e.Subtree)
		src.WriteString("      }\n")
		src.WriteString("      var __vnode=h(KrateIslandData,{});\n")
	} else {
		fmt.Fprintf(&src, "      var __vnode=(%s);\n", e.Subtree)
	}
	src.WriteString("      var __boundary=h(KrateIslandBoundary,{onError:function(err){console.error(\"krate external island:\",String((err&&(err.name?err.name+\": \":\"\"))+(err&&err.message||err)||err));el.setAttribute(\"data-krate-island-error\",\"\");}},__vnode);\n")
	src.WriteString("      var __start=typeof performance!==\"undefined\"?performance.now():0;\n")
	src.WriteString("      if (el.childNodes && el.childNodes.length) hydrate(__boundary,el); else render(__boundary,el);\n")
	src.WriteString("      var __end=typeof performance!==\"undefined\"?performance.now():0;\n")
	fmt.Fprintf(&src, "      (globalThis.__krate_island_hydration ||= {})[%q]={start:__start,end:__end,ms:__end-__start};\n", e.ID)
	src.WriteString("      el.setAttribute(\"data-krate-island-hydrated\",\"\");\n")
	src.WriteString("      (globalThis.__krate_island_cleanups ||= []).push(function(){ render(null, el); });\n")
	src.WriteString("    } catch (e) { console.error(\"krate external island:\", String((e&&(e.name?e.name+\": \":\"\"))+(e&&e.message||e)||e)); el.setAttribute(\"data-krate-island-error\", \"\"); el.textContent = \"This component could not load.\"; }\n  }\n}\n")
	return src.String()
}

func (b *Builder) islandSSRSource(e irtree.ExternalComponent, imports []irtree.ExternalImport) string {
	var src strings.Builder
	src.WriteString("import { h, Fragment } from \"preact\";\n")
	src.WriteString("import renderToString from \"preact-render-to-string\";\n")
	writeIslandImportLines(&src, imports)
	props := e.SSRPropsJS
	if props == "" {
		props = "{}"
	}
	fmt.Fprintf(&src, "\nconst props = %s;\n", props)
	src.WriteString("globalThis.__krate_island_html = \"\";\nglobalThis.__krate_island_ssr_error = \"\";\ntry { globalThis.__krate_island_html = renderToString(")
	src.WriteString(e.Subtree)
	src.WriteString("); } catch (e) { globalThis.__krate_island_ssr_error = String((e && (e.name ? e.name + \": \" : \"\") + (e.message || e)) || e) + \"\\n\" + String((e && e.stack) || \"\"); }\n")
	return src.String()
}

// islandAlias maps React to the project's single Preact installation. Server
// rendering uses absolute module paths so packages inside node_modules and the
// embedded preact-render-to-string module all share one Preact instance.
func islandAlias(root string, ssr bool) map[string]string {
	if !ssr {
		return map[string]string{
			"react":                 "preact/compat",
			"react-dom":             "preact/compat",
			"react-dom/client":      "preact/compat",
			"react/jsx-runtime":     "preact/jsx-runtime",
			"react/jsx-dev-runtime": "preact/jsx-runtime",
		}
	}
	preact := filepath.Join(root, "node_modules", "preact")
	abs := func(rel string) string { return filepath.ToSlash(filepath.Join(preact, rel)) }
	compat := abs("compat/dist/compat.module.js")
	jsx := abs("jsx-runtime/dist/jsxRuntime.module.js")
	hooks := abs("hooks/dist/hooks.module.js")
	core := abs("dist/preact.module.js")
	return map[string]string{
		"react":                 compat,
		"react-dom":             compat,
		"react-dom/client":      compat,
		"react/jsx-runtime":     jsx,
		"react/jsx-dev-runtime": jsx,
		"preact":                core,
		"preact/compat":         compat,
		"preact/jsx-runtime":    jsx,
		"preact/hooks":          hooks,
	}
}

func (b *Builder) buildIslandBundle(src, resolveDir string, ssr bool, plugin *api.Plugin) ([]byte, error) {
	opts := api.BuildOptions{
		Stdin: &api.StdinOptions{
			Contents:   src,
			ResolveDir: resolveDir,
			Sourcefile: "krate-island.tsx",
			Loader:     api.LoaderTSX,
		},
		Bundle:            true,
		Write:             false,
		MinifyWhitespace:  b.shouldMinifyJS(),
		MinifyIdentifiers: b.shouldMinifyJS(),
		MinifySyntax:      b.shouldMinifyJS(),
		Format:            api.FormatESModule,
		Platform:          api.PlatformBrowser,
		Target:            api.ES2020,
		JSX:               api.JSXTransform,
		JSXFactory:        "h",
		JSXFragment:       "Fragment",
		LogLevel:          api.LogLevelSilent,
		Alias:             islandAlias(b.Root, ssr),
	}
	if ssr {
		opts.Format = api.FormatIIFE
		opts.Platform = api.PlatformBrowser
		opts.Stdin.Sourcefile = "krate-island-ssr.tsx"
		if plugin != nil {
			opts.Plugins = []api.Plugin{*plugin}
		}
	} else {
		opts.External = []string{"preact", "preact/compat", "preact/hooks", "preact/jsx-runtime"}
	}
	result := api.Build(opts)
	if len(result.Errors) > 0 {
		var msgs []string
		for _, e := range result.Errors {
			if e.Location != nil {
				msgs = append(msgs, fmt.Sprintf("%s:%d:%d: %s", e.Location.File, e.Location.Line, e.Location.Column, e.Text))
			} else {
				msgs = append(msgs, e.Text)
			}
		}
		joined := strings.Join(msgs, "\n")
		if strings.Contains(joined, "preact") {
			return nil, fmt.Errorf("external React components require Preact, which is not installed in this project\n  hint: install it with `pnpm add preact` (or `npm install preact`)\n  detail: %s", joined)
		}
		return nil, fmt.Errorf("bundling external React island:\n%s", joined)
	}
	if len(result.OutputFiles) == 0 {
		return nil, fmt.Errorf("external React island produced no output")
	}
	return result.OutputFiles[0].Contents, nil
}

func (b *Builder) buildIslandSSR(src, resolveDir string) (string, error) {
	out, err := b.buildIslandBundle(src, resolveDir, true, islandRenderToStringPlugin(resolveDir))
	if err != nil {
		return "", err
	}
	rt, err := jsruntime.New()
	if err != nil {
		return "", err
	}
	defer rt.Close()
	if _, err := rt.Execute(string(out)); err != nil {
		return "", err
	}
	if v, err := rt.GetGlobal("__krate_island_ssr_error"); err == nil && v != nil && fmt.Sprint(v) != "" {
		return "", fmt.Errorf("%v", v)
	}
	v, err := rt.GetGlobal("__krate_island_html")
	if err != nil {
		return "", err
	}
	html, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("island server render did not produce HTML")
	}
	return html, nil
}

func islandRenderToStringPlugin(resolveDir string) *api.Plugin {
	return &api.Plugin{
		Name: "krate-island-render-to-string",
		Setup: func(pb api.PluginBuild) {
			pb.OnResolve(api.OnResolveOptions{Filter: `^preact-render-to-string$`}, func(api.OnResolveArgs) (api.OnResolveResult, error) {
				return api.OnResolveResult{Path: "preact-render-to-string", Namespace: "krate-island-render"}, nil
			})
			pb.OnLoad(api.OnLoadOptions{Filter: `.*`, Namespace: "krate-island-render"}, func(api.OnLoadArgs) (api.OnLoadResult, error) {
				contents := islandRenderToStringJS
				return api.OnLoadResult{Contents: &contents, Loader: api.LoaderJS, ResolveDir: resolveDir}, nil
			})
		},
	}
}

// injectIslandSSR adds the server-rendered subtree to each island placeholder.
func injectIslandSSR(html string, rendered map[string]string) string {
	for id, inner := range rendered {
		if inner == "" {
			continue
		}
		placeholder := `<div data-krate-island="` + id + `" style="display:contents"></div>`
		replacement := `<div data-krate-island="` + id + `" data-krate-island-ssr style="display:contents">` + inner + `</div>`
		html = strings.ReplaceAll(html, placeholder, replacement)
	}
	return html
}

// ensurePreactShared builds the shared Preact chunks once per build and records
// the import map. Each specifier is a standalone ESM module; cross-specifier
// imports stay bare and are resolved by the import map.
func (b *Builder) ensurePreactShared() (map[string]string, error) {
	b.islandMu.Lock()
	defer b.islandMu.Unlock()
	if b.preactChunks != nil {
		return b.preactChunks, nil
	}
	specs := []struct {
		name      string
		externals []string
	}{
		{"preact", nil},
		{"preact/hooks", []string{"preact"}},
		{"preact/jsx-runtime", []string{"preact"}},
		{"preact/compat", []string{"preact", "preact/hooks", "preact/jsx-runtime"}},
	}
	urls := make(map[string]string, len(specs))
	for _, s := range specs {
		// esbuild's `External` matches a package and all of its subpaths, so
		// "preact" would also externalize "preact/compat" (leaving a
		// self-referential re-export). Resolve exact specifiers only.
		var plugins []api.Plugin
		if len(s.externals) > 0 {
			exts := s.externals
			plugins = append(plugins, api.Plugin{
				Name: "krate-preact-externals",
				Setup: func(pb api.PluginBuild) {
					pb.OnResolve(api.OnResolveOptions{Filter: `.*`}, func(args api.OnResolveArgs) (api.OnResolveResult, error) {
						for _, ext := range exts {
							if args.Path == ext {
								return api.OnResolveResult{Path: args.Path, External: true}, nil
							}
						}
						return api.OnResolveResult{}, nil
					})
				},
			})
		}
		contents := fmt.Sprintf("export * from %q;", s.name)
		if s.name == "preact/compat" {
			// preact/compat re-exports preact/hooks, which is externalized here.
			// esbuild cannot enumerate an external module's exports, so the
			// generated chunk lacks static hook exports (e.g. useLayoutEffect)
			// and Radix's `import { useLayoutEffect } from "react"` fails.
			// Re-export the hooks explicitly.
			contents += `
export { useState, useEffect, useLayoutEffect, useRef, useMemo, useCallback, useReducer, useContext, useId, useDebugValue, useImperativeHandle } from "preact/hooks";`
		}
		result := api.Build(api.BuildOptions{
			Stdin: &api.StdinOptions{
				Contents:   contents,
				ResolveDir: b.Root,
				Sourcefile: "krate-preact-shared.ts",
				Loader:     api.LoaderTS,
			},
			Bundle:            true,
			Write:             false,
			MinifyWhitespace:  b.shouldMinifyJS(),
			MinifyIdentifiers: b.shouldMinifyJS(),
			MinifySyntax:      b.shouldMinifyJS(),
			Format:            api.FormatESModule,
			Platform:          api.PlatformBrowser,
			Target:            api.ES2020,
			Plugins:           plugins,
			LogLevel:          api.LogLevelSilent,
		})
		if len(result.Errors) > 0 || len(result.OutputFiles) == 0 {
			var detail []string
			for _, e := range result.Errors {
				detail = append(detail, e.Text)
			}
			return nil, fmt.Errorf("external React components require Preact, which is not installed in this project\n  hint: install it with `pnpm add preact` (or `npm install preact`)\n  detail: %s", strings.Join(detail, "; "))
		}
		out := result.OutputFiles[0].Contents
		name := "chunks/preact-" + strings.ReplaceAll(s.name, "/", "-") + "." + hashContent(out) + ".js"
		chunksDir := filepath.Join(b.Cfg.OutDir, "chunks")
		if err := os.MkdirAll(chunksDir, 0755); err != nil {
			return nil, err
		}
		if err := fsutil.WriteFileIfChanged(filepath.Join(b.Cfg.OutDir, name), out, 0644); err != nil {
			return nil, err
		}
		urls[s.name] = assetHref(b.Cfg.BaseURLPath(), name)
	}
	data, err := json.Marshal(map[string]any{"imports": urls})
	if err != nil {
		return nil, err
	}
	b.preactChunks = urls
	b.islandImportMap = string(data)
	return urls, nil
}

// islandImportMapHTML returns the import map script for pages containing
// islands (empty when none).
func (b *Builder) islandImportMapHTML() string {
	b.islandMu.Lock()
	defer b.islandMu.Unlock()
	if b.islandImportMap == "" {
		return ""
	}
	return `<script type="importmap">` + b.islandImportMap + `</script>` + "\n"
}

// installedPreactVersion reads the project's resolved preact version. It is
// diagnostic only: an older release still builds, but is called out because
// islands rely on compat behavior added in later 10.x releases.
func installedPreactVersion(root string) string {
	data, err := os.ReadFile(filepath.Join(root, "node_modules", "preact", "package.json"))
	if err != nil {
		return ""
	}
	var pkg struct {
		Version string `json:"version"`
	}
	if json.Unmarshal(data, &pkg) != nil {
		return ""
	}
	return pkg.Version
}

func versionLess(a, b string) bool {
	parse := func(s string) [3]int {
		var out [3]int
		s = strings.SplitN(s, "-", 2)[0]
		for i, part := range strings.SplitN(s, ".", 3) {
			if i >= 3 {
				break
			}
			n := 0
			for _, r := range part {
				if r < '0' || r > '9' {
					break
				}
				n = n*10 + int(r-'0')
			}
			out[i] = n
		}
		return out
	}
	av, bv := parse(a), parse(b)
	for i := 0; i < 3; i++ {
		if av[i] != bv[i] {
			return av[i] < bv[i]
		}
	}
	return false
}
