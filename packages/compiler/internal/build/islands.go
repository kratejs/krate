package build

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/evanw/esbuild/pkg/api"
	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/fsutil"
	"github.com/kratejs/krate/packages/compiler/internal/irtree"
)

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

// buildIslands bundles the recorded external subtrees against Preact and writes
// one island chunk per page. It returns JS that dynamically imports the chunk.
// Preact is resolved from the project's node_modules; when it is missing, the
// returned error tells the user to install it.
func (b *Builder) buildIslands(exts []irtree.ExternalComponent, imports []irtree.ExternalImport) (string, string, error) {
	if len(exts) == 0 {
		return "", "", nil
	}

	var src strings.Builder
	var boot strings.Builder
	src.WriteString("import { Component, Fragment, h, render } from \"preact\";\n")
	for _, imp := range imports {
		switch {
		case imp.Namespace:
			fmt.Fprintf(&src, "import * as %s from %q;\n", imp.Local, imp.Source)
		case imp.Default:
			fmt.Fprintf(&src, "import %s from %q;\n", imp.Local, imp.Source)
		default:
			if imp.Remote != "" && imp.Remote != imp.Local {
				fmt.Fprintf(&src, "import { %s as %s } from %q;\n", imp.Remote, imp.Local, imp.Source)
			} else {
				fmt.Fprintf(&src, "import { %s } from %q;\n", imp.Local, imp.Source)
			}
		}
	}
	for _, e := range exts {
		if e.Subtree == "" {
			continue
		}
		fmt.Fprintf(&src, "\n{\n  const el = document.querySelector('[data-krate-island=%q]');\n", e.ID)
		src.WriteString("  if (el) {\n    try {\n")
		if e.PropsJS != "" {
			fmt.Fprintf(&src, "    const getProps=(globalThis.__krate_island_values||{})[%q];\n", e.ID)
			fmt.Fprintf(&src, "    const subscribe=(globalThis.__krate_island_subscribe||{})[%q];\n", e.ID)
			cls := "KrateIsland" + strings.ReplaceAll(e.ID, "-", "")
			fmt.Fprintf(&src, "    class %s extends Component {\n", cls)
			src.WriteString("      constructor(p){super(p);this.state={v:0};}\n")
			src.WriteString("      componentDidMount(){var self=this;if(subscribe)this._off=subscribe(function(){self.setState({v:self.state.v+1});});}\n")
			src.WriteString("      componentWillUnmount(){if(this._off)this._off();}\n")
			fmt.Fprintf(&src, "      render(){var props=typeof getProps==='function'?getProps():{};return (%s);}\n", e.Subtree)
			src.WriteString("    }\n")
			fmt.Fprintf(&src, "    render(h(%s,{}), el);\n", cls)
		} else {
			fmt.Fprintf(&src, "    render(%s, el);\n", e.Subtree)
		}
		src.WriteString("      (globalThis.__krate_island_cleanups ||= []).push(function(){ render(null, el); });\n    } catch (e) { console.error(\"krate external island:\", e); el.setAttribute(\"data-krate-island-error\", \"\"); el.textContent = \"This component could not load.\"; }\n  }\n}\n")
	}

	// Preact is deliberately NOT a Krate dependency: resolve it from the user's
	// project and build one shared chunk set (import-mapped) so islands do not
	// duplicate it.
	if _, err := b.ensurePreactShared(); err != nil {
		return "", "", err
	}
	if v := installedPreactVersion(b.Root); v != "" && versionLess(v, "10.19.0") {
		fmt.Fprintf(os.Stderr, "  %sWarning:%s preact %s is older than 10.19.0; islands use hooks and compat APIs that older releases can break\n", cYellow, cReset, v)
	}
	alias := map[string]string{
		"react":                 "preact/compat",
		"react-dom":             "preact/compat",
		"react-dom/client":      "preact/compat",
		"react/jsx-runtime":     "preact/jsx-runtime",
		"react/jsx-dev-runtime": "preact/jsx-runtime",
	}

	result := api.Build(api.BuildOptions{
		Stdin: &api.StdinOptions{
			Contents:   src.String(),
			ResolveDir: b.Root,
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
		Alias:             alias,
		External:          []string{"preact", "preact/compat", "preact/hooks", "preact/jsx-runtime"},
	})
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
			return "", "", fmt.Errorf("external React components require Preact, which is not installed in this project\n  hint: install it with `pnpm add preact` (or `npm install preact`)\n  detail: %s", joined)
		}
		return "", "", fmt.Errorf("bundling external React island:\n%s", joined)
	}
	if len(result.OutputFiles) == 0 {
		return "", "", fmt.Errorf("external React island produced no output")
	}

	out := result.OutputFiles[0].Contents
	hash := hashContent(out)
	name := "island." + hash + ".js"
	chunksDir := filepath.Join(b.Cfg.OutDir, "chunks")
	if err := os.MkdirAll(chunksDir, 0755); err != nil {
		return "", "", fmt.Errorf("creating chunks dir: %w", err)
	}
	if err := fsutil.WriteFileIfChanged(filepath.Join(chunksDir, name), out, 0644); err != nil {
		return "", "", fmt.Errorf("writing island chunk: %w", err)
	}

	url := "/chunks/" + name
	if bp := b.Cfg.BaseURLPath(); bp != "" {
		url = bp + url
	}
	return boot.String(), fmt.Sprintf(`import(%q).catch(function(e){ console.error("krate external island:", e); });`, url), nil
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
			return nil, fmt.Errorf("external React components require Preact, which is not installed in this project\n  hint: install it with `pnpm add preact` (or `npm install preact`)")
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
