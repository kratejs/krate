package build

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/evanw/esbuild/pkg/api"
	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/bundler"
	"github.com/kratejs/krate/packages/compiler/internal/diag"
	"github.com/kratejs/krate/packages/compiler/internal/fsutil"
	"github.com/kratejs/krate/packages/compiler/internal/irtree"
	"github.com/kratejs/krate/packages/compiler/internal/tsstrip"
	"github.com/kratejs/krate/packages/native/jsruntime"
)

// assets/preact-render-to-string.js is vendored from preact-render-to-string
// 6.8.2 (MIT; 6.8.1 renders nothing under Preact 11); see assets/preact-render-to-string.LICENSE.
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
func (b *Builder) buildIslands(owner string, exts []irtree.ExternalComponent, imports, localImports []irtree.ExternalImport, resolveDir string, prov *islandProviders) (string, string, map[string]string, *islandStyles, error) {
	if len(exts) == 0 {
		b.setIslandEntries(owner, nil)
		return "", "", nil, nil, nil
	}
	if resolveDir == "" {
		resolveDir = b.Root
	}

	if anyPreactIsland(exts) {
		if _, err := b.ensurePreactShared(); err != nil {
			return "", "", nil, nil, diag.Diagnostic{
				Message: "island runtime could not be built: " + err.Error(),
				Hint:    "install Preact 10.20 or newer with `pnpm add preact`, then rebuild",
			}.WithCode(diag.CodeIslandAsset)
		}
		if v := installedPreactVersion(b.Root); v != "" && versionLess(v, "10.20.0") {
			fmt.Fprintf(os.Stderr, "  %sWarning:%s preact %s is older than 10.20.0; islands use hooks and compat APIs that older releases can break\n", cYellow, cReset, v)
		}
	}
	var boot strings.Builder
	var importsJS strings.Builder
	ssrHTML := map[string]string{}
	sink := &islandCSSSink{root: b.Root}
	allImports := append(append([]irtree.ExternalImport(nil), imports...), localImports...)

	var entries []islandEntry
	// register records the entry module of an island chunk and returns the URL
	// the page script imports. The chunk itself is written once for the whole
	// site (writeIslandBundles), so modules shared by pages are shared files.
	register := func(src string) string {
		name := b.islandEntryName(src, resolveDir)
		entries = append(entries, islandEntry{Name: name, Source: src, ResolveDir: resolveDir, Virtual: prov.virtualModules()})
		return b.islandEntryURL(name)
	}
	buildFailure := func(e irtree.ExternalComponent, err error) error {
		d := diag.Diagnostic{
			File:    e.File,
			Line:    e.Line,
			Col:     e.Col,
			Message: "island " + e.ID + " (<" + e.Local + ">) could not be built: " + err.Error(),
			Hint:    "check the island package and its local imports, then rebuild",
		}
		// The problem is in the package (or a module it imports), so
		// point there and say which page island pulled it in.
		var be *bundleError
		if errors.As(err, &be) && be.Loc != nil {
			d.File, d.Line, d.Col = be.Loc.File, be.Loc.Line, be.Loc.Col
			d.Message = "island " + e.ID + " (<" + e.Local + "> used at " + b.relPath(e.File) + ":" + strconv.Itoa(e.Line) + ":" + strconv.Itoa(e.Col) + ") could not be built: " + err.Error()
		}
		return d.WithCode(diag.CodeIslandAsset)
	}

	chunkURL := map[string]string{}
	var reactGroup, realGroup []irtree.ExternalComponent
	for _, e := range exts {
		if e.Subtree == "" {
			continue
		}
		if e.RealReact {
			// Client-rendered on real React; the server emits the fallback.
			realGroup = append(realGroup, e)
			if fb := realFallbackHTML(e); fb != "" {
				ssrHTML[e.ID] = fb
			}
			continue
		}
		used := islandImportsFor(e.Subtree, allImports)
		if e.ReactIsland {
			// Bundled together below so module-level state is shared.
			reactGroup = append(reactGroup, e)
		} else {
			clientSrc := b.islandClientSource(e, used, prov)
			if _, err := b.buildIslandBundle(clientSrc, resolveDir, false, nil, sink, prov); err != nil {
				return "", "", nil, nil, buildFailure(e, err)
			}
			chunkURL[e.ID] = register(clientSrc)
		}

		if e.ClientOnly || (!e.ReactIsland && isCSSInJSPackage(e.Source)) {
			printIslandWarning(e, diag.CodeIslandClientOnly, "island "+e.ID+" (<"+e.Local+">) is client-only: its CSS-in-JS library creates styles in the browser", "use CSS modules or Tailwind for content that must be styled before JavaScript loads")
			continue
		}
		if !e.SSRPropsKnown {
			printIslandWarning(e, diag.CodeIslandClientOnly, "island "+e.ID+" (<"+e.Local+">) stays client-only: a bridged prop has no build-time initial value", "pass build-time initial values, or keep the island client-only intentionally")
			continue
		}
		ssrSrc := b.islandSSRSource(e, used, prov)
		html, err := b.buildIslandSSR(ssrSrc, resolveDir, sink, prov)
		if err != nil {
			hint := "fix the browser-only dependency or keep the island client-only intentionally"
			if strings.Contains(strings.ToLower(err.Error()), "provider") {
				// A provider in a layout is a Krate component; islands are
				// separate Preact trees and do not see it.
				hint = "a missing provider must be rendered inside the island (in the module that uses the hook), not in a layout; " + hint
			}
			printIslandWarning(e, diag.CodeIslandClientOnly, "island "+e.ID+" stays client-only: server render failed: "+err.Error(), hint)
			continue
		}
		ssrHTML[e.ID] = html
	}
	if len(reactGroup) > 0 {
		groupSrc := b.islandGroupClientSource(reactGroup, allImports, prov)
		if _, err := b.buildIslandBundle(groupSrc, resolveDir, false, nil, sink, prov); err != nil {
			return "", "", nil, nil, buildFailure(reactGroup[0], err)
		}
		url := register(groupSrc)
		for _, e := range reactGroup {
			chunkURL[e.ID] = url
		}
	}
	if len(realGroup) > 0 {
		groupSrc := realIslandGroupSource(realGroup, allImports)
		if err := b.buildRealIslandBundle(groupSrc, resolveDir, sink); err != nil {
			return "", "", nil, nil, buildFailure(realGroup[0], err)
		}
		name := b.islandEntryName(groupSrc, resolveDir)
		entries = append(entries, islandEntry{Name: name, Source: groupSrc, ResolveDir: resolveDir, Real: true})
		url := b.islandEntryURL(name)
		for _, e := range realGroup {
			chunkURL[e.ID] = url
		}
	}
	for _, e := range exts {
		if url, ok := chunkURL[e.ID]; ok {
			importsJS.WriteString(islandBootLine(e, url))
		}
	}

	styles, err := b.buildIslandStyles(sink)
	if err != nil {
		return "", "", nil, nil, err
	}
	b.setIslandEntries(owner, entries)
	return boot.String(), importsJS.String(), ssrHTML, styles, nil
}

// islandBootLine is the page script statement that loads an island chunk and
// mounts the island: at load by default, when the placeholder scrolls into
// view for client:visible, or when the browser is idle for client:idle.
func islandBootLine(e irtree.ExternalComponent, url string) string {
	// A deferred island's chunk must not be preloaded (the page-level preload
	// scan matches whole "/chunks/x.js" literals), so its URL is built in two
	// parts.
	lit := strconv.Quote(url)
	dir := ""
	if e.Hydrate != "" {
		cut := strings.LastIndex(url, "/")
		dir = "var d=" + strconv.Quote(url[:cut]) + ";"
		lit = "d+" + strconv.Quote(url[cut:])
	}
	// The placeholder is looked up when the page script runs, not when the
	// chunk arrives: after a client-side navigation the ids repeat on the next
	// page, and a late chunk must not mount into that page's placeholder.
	sel := "[data-krate-island=" + e.ID + "]"
	load := "import(" + lit + ").then(function(m){m.mount(" + strconv.Quote(e.ID) + ",el);}).catch(function(e){ console.error(\"krate external island:\", e); });"
	switch e.Hydrate {
	case "idle":
		return "(function(){" + dir + "var el=document.querySelector('" + sel + "');var go=function(){" + load + "};if(typeof requestIdleCallback===\"function\")requestIdleCallback(go,{timeout:2000});else setTimeout(go,200);})();\n"
	case "visible":
		return "(function(){" + dir + "var el=document.querySelector('" + sel + "');var go=function(){" + load + "};" +
			"if(!el||typeof IntersectionObserver!==\"function\"){go();return;}" +
			"var t=el.firstElementChild,block=false;" +
			"if(!t||getComputedStyle(t).display===\"contents\"){el.style.display=\"block\";t=el;block=true;}" +
			"var io=new IntersectionObserver(function(es){for(var i=0;i<es.length;i++){if(es[i].isIntersecting){io.disconnect();if(block)el.style.display=\"contents\";go();return;}}},{rootMargin:\"200px\"});" +
			"io.observe(t);})();\n"
	}
	return "(function(){var el=document.querySelector('" + sel + "');" + load + "})();\n"
}

// printIslandWarning emits a source-located island diagnostic. The build
// package owns the asset failure, but the source position comes from the IR.
func printIslandWarning(e irtree.ExternalComponent, code diag.Code, msg, hint string) {
	msg = strings.Replace(msg, "(<"+reactIslandPageLocal+">)", "(the page component)", 1)
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
			// An island is bundled from the original sources, which still
			// export the name the bundler may have moved (bundler.OriginalName).
			remote := imp.Remote
			if remote == "" {
				remote = imp.Local
			}
			remote = bundler.OriginalName(remote)
			if remote != imp.Local {
				fmt.Fprintf(sb, "import { %s as %s } from %q;\n", remote, imp.Local, imp.Source)
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

func (b *Builder) islandClientSource(e irtree.ExternalComponent, imports []irtree.ExternalImport, prov *islandProviders) string {
	var src strings.Builder
	src.WriteString("import { Component, Fragment, h, hydrate, render } from \"preact\";\n")
	src.WriteString(prov.importLines())
	writeIslandImportLines(&src, imports)
	src.WriteString(islandBoundaryJS)
	src.WriteString("export function mount(only, elArg) {\n")
	src.WriteString(islandMountBlock(e, prov))
	src.WriteString("}\n")
	return src.String()
}

// islandGroupClientSource builds one module for several islands. Islands that
// run the project's own modules (React libraries with module-level state, such
// as a zustand store) must share one copy of those modules, which separate
// chunks would not.
func (b *Builder) islandGroupClientSource(group []irtree.ExternalComponent, all []irtree.ExternalImport, prov *islandProviders) string {
	var src strings.Builder
	src.WriteString("import { Component, Fragment, h, hydrate, render } from \"preact\";\n")
	src.WriteString(prov.importLines())
	seen := map[string]bool{}
	for _, e := range group {
		for _, imp := range islandImportsFor(e.Subtree, all) {
			key := imp.Source + "\x00" + imp.Local
			if seen[key] {
				continue
			}
			seen[key] = true
			writeIslandImportLines(&src, []irtree.ExternalImport{imp})
		}
	}
	src.WriteString(islandBoundaryJS)
	src.WriteString("export function mount(only, elArg) {\n")
	for _, e := range group {
		src.WriteString(islandMountBlock(e, prov))
	}
	src.WriteString("}\n")
	return src.String()
}

func islandMountBlock(e irtree.ExternalComponent, prov *islandProviders) string {
	var src strings.Builder
	fmt.Fprintf(&src, "\nif (only === void 0 || only === %q) {\n  const el = elArg || document.querySelector('[data-krate-island=%q]');\n", e.ID, e.ID)
	src.WriteString("  if (el && el.isConnected !== false && !el.hasAttribute(\"data-krate-island-hydrated\")) {\n    try {\n")
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
	src.WriteString(prov.wrapVNodeJS("__vnode"))
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

func (b *Builder) islandSSRSource(e irtree.ExternalComponent, imports []irtree.ExternalImport, prov *islandProviders) string {
	var src strings.Builder
	src.WriteString("import { h, Fragment } from \"preact\";\n")
	src.WriteString("import renderToString from \"preact-render-to-string\";\n")
	src.WriteString(prov.importLines())
	writeIslandImportLines(&src, imports)
	props := e.SSRPropsJS
	if props == "" {
		props = "{}"
	}
	fmt.Fprintf(&src, "\nconst props = %s;\n", props)
	src.WriteString("globalThis.__krate_island_html = \"\";\nglobalThis.__krate_island_ssr_error = \"\";\ntry { globalThis.__krate_island_html = renderToString(")
	src.WriteString(prov.wrapExprJS(e.Subtree))
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
	preact := findPreactDir(root)
	if preact == "" {
		preact = filepath.Join(root, "node_modules", "preact")
	}
	compat := preactEntry(preact, "./compat", "compat/dist/compat.module.js")
	jsx := preactEntry(preact, "./jsx-runtime", "jsx-runtime/dist/jsxRuntime.module.js")
	hooks := preactEntry(preact, "./hooks", "hooks/dist/hooks.module.js")
	core := preactEntry(preact, ".", "dist/preact.module.js")
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

// findPreactDir returns the installed preact package directory: the project's
// node_modules first, then those of its ancestors (a hoisted workspace
// install). Empty when preact is not installed.
func findPreactDir(root string) string {
	dir := root
	for {
		cand := filepath.Join(dir, "node_modules", "preact")
		if _, err := os.Stat(filepath.Join(cand, "package.json")); err == nil {
			return cand
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

// preactEntry returns the absolute file a preact subpath ("." , "./compat",
// "./hooks", "./jsx-runtime") resolves to. Preact 10 ships dist/*.module.js
// and Preact 11 ships *.mjs, so the file comes from the package's own
// "exports" map; legacy is the Preact 10 path, used when the map is absent.
func preactEntry(preactDir, subpath, legacy string) string {
	file := legacy
	if data, err := os.ReadFile(filepath.Join(preactDir, "package.json")); err == nil {
		var pkg struct {
			Exports map[string]json.RawMessage `json:"exports"`
		}
		if json.Unmarshal(data, &pkg) == nil {
			if raw, ok := pkg.Exports[subpath]; ok {
				if target := exportTarget(raw); target != "" {
					file = strings.TrimPrefix(target, "./")
				}
			}
		}
	}
	return filepath.ToSlash(filepath.Join(preactDir, filepath.FromSlash(file)))
}

// exportTarget picks the plain-file target of one "exports" entry: a string,
// or the "import"/"default" condition of an object.
func exportTarget(raw json.RawMessage) string {
	var str string
	if json.Unmarshal(raw, &str) == nil {
		return str
	}
	var cond map[string]json.RawMessage
	if json.Unmarshal(raw, &cond) != nil {
		return ""
	}
	for _, key := range []string{"browser", "import", "default"} {
		if v, ok := cond[key]; ok {
			if t := exportTarget(v); t != "" {
				return t
			}
		}
	}
	return ""
}

// bundleLocation is a source position reported by esbuild inside a package or
// module the island pulls in.
type bundleLocation struct {
	File      string
	Line, Col int
}

// bundleError is an island bundling failure; Loc is where esbuild found the
// problem (the package file), when it reported one.
type bundleError struct {
	Loc *bundleLocation
	Msg string
}

func (e *bundleError) Error() string { return e.Msg }

// esbuildFile turns a path esbuild reported (relative to the process working
// directory) into a project-relative slash path.
func (b *Builder) esbuildFile(file string) string {
	cwd, _ := os.Getwd()
	return esbuildRelFile(b.Root, cwd, file)
}

// esbuildRelFile makes a path esbuild reported (relative to base, its working
// directory) relative to the project root.
func esbuildRelFile(root, base, file string) string {
	if !filepath.IsAbs(file) && base != "" {
		file = filepath.Join(base, file)
	}
	if rel, err := filepath.Rel(root, file); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return filepath.ToSlash(rel)
	}
	return filepath.ToSlash(file)
}

func (b *Builder) buildIslandBundle(src, resolveDir string, ssr bool, plugin *api.Plugin, sink *islandCSSSink, prov *islandProviders) ([]byte, error) {
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
		// The automatic runtime imports from preact/jsx-runtime in every module,
		// so island modules need no `h` in scope. A module's own
		// `@jsxImportSource` pragma (emotion) still takes precedence.
		JSX:             api.JSXAutomatic,
		JSXImportSource: "preact",
		LogLevel:        api.LogLevelSilent,
		Alias:           islandAlias(b.Root, ssr),
		Plugins:         []api.Plugin{islandCSSPlugin(sink), islandJSXPlugin(), islandTagsPlugin(b.Root, ssr), reconcilerGuardPlugin()},
	}
	if prov != nil {
		opts.Plugins = append(opts.Plugins, islandEntryPlugin(nil, prov.virtualModules()))
	}
	if ssr {
		opts.Format = api.FormatIIFE
		opts.Platform = api.PlatformBrowser
		opts.Stdin.Sourcefile = "krate-island-ssr.tsx"
		if plugin != nil {
			opts.Plugins = append(opts.Plugins, *plugin)
		}
	} else {
		opts.External = []string{"preact", "preact/compat", "preact/hooks", "preact/jsx-runtime"}
		opts.Plugins = append(opts.Plugins, islandRequirePlugin())
	}
	result := esbuildBuild(opts)
	if len(result.Errors) > 0 {
		var msgs []string
		var loc *bundleLocation
		for _, e := range result.Errors {
			if e.Location != nil {
				file := b.esbuildFile(e.Location.File)
				msgs = append(msgs, fmt.Sprintf("%s:%d:%d: %s", file, e.Location.Line, e.Location.Column+1, e.Text))
				if loc == nil && !strings.HasPrefix(e.Location.File, "krate-island") {
					loc = &bundleLocation{File: file, Line: e.Location.Line, Col: e.Location.Column + 1}
				}
			} else {
				msgs = append(msgs, e.Text)
			}
		}
		joined := strings.Join(msgs, "\n")
		if findPreactDir(b.Root) == "" && strings.Contains(joined, "preact") {
			return nil, &bundleError{Loc: loc, Msg: fmt.Sprintf("external React components require Preact, which is not installed in this project\n  hint: install it with `pnpm add preact` (or `npm install preact`)\n  detail: %s", joined)}
		}
		return nil, &bundleError{Loc: loc, Msg: "bundling external React island:\n" + joined}
	}
	if len(result.OutputFiles) == 0 {
		return nil, fmt.Errorf("external React island produced no output")
	}
	return result.OutputFiles[0].Contents, nil
}

// islandJSXPlugin compiles the project's own JSX modules for Preact. The
// project tsconfig usually says `jsxImportSource: "@krate/runtime"`, which
// esbuild applies to every module under it, so the JSX options are given per
// module instead. A module's own `@jsxImportSource` pragma still wins.
func islandJSXPlugin() api.Plugin { return islandJSXPluginFor("preact") }

// islandJSXPluginFor is islandJSXPlugin for another JSX runtime package
// ("react" for real-React islands).
func islandJSXPluginFor(importSource string) api.Plugin {
	return api.Plugin{
		Name: "krate-island-jsx",
		Setup: func(pb api.PluginBuild) {
			pb.OnLoad(api.OnLoadOptions{Filter: `\.[jt]sx$`, Namespace: "file"}, func(args api.OnLoadArgs) (api.OnLoadResult, error) {
				if strings.Contains(filepath.ToSlash(args.Path), "/node_modules/") {
					return api.OnLoadResult{}, nil
				}
				data, err := os.ReadFile(args.Path)
				if err != nil {
					return api.OnLoadResult{}, err
				}
				loader := api.LoaderTSX
				if strings.HasSuffix(args.Path, ".jsx") {
					loader = api.LoaderJSX
				}
				source := string(data)
				if importSource == "preact" {
					source = injectIslandTags(source)
				}
				res := api.Transform(tsstrip.BlankTierDecorators(args.Path, source), api.TransformOptions{
					Loader:          loader,
					Sourcefile:      args.Path,
					JSX:             api.JSXAutomatic,
					JSXImportSource: importSource,
					TsconfigRaw:     `{"compilerOptions":{"jsx":"react-jsx","jsxImportSource":"` + importSource + `"}}`,
					Target:          api.ES2020,
					LogLevel:        api.LogLevelSilent,
				})
				if len(res.Errors) > 0 {
					return api.OnLoadResult{Errors: res.Errors, Warnings: res.Warnings}, nil
				}
				code := string(res.Code)
				return api.OnLoadResult{Contents: &code, Loader: api.LoaderJS, ResolveDir: filepath.Dir(args.Path), Warnings: res.Warnings}, nil
			})
		},
	}
}

// islandRequirePlugin lets CommonJS code (swr's use-sync-external-store shim,
// older hook libraries) `require("react")`. The shared Preact modules are
// external ESM, which esbuild cannot require from an ES module bundle, so a
// require call resolves to a tiny module that re-exports the import.
func islandRequirePlugin() api.Plugin {
	return api.Plugin{
		Name: "krate-island-require",
		Setup: func(pb api.PluginBuild) {
			target := map[string]string{
				"react":                 "preact/compat",
				"react-dom":             "preact/compat",
				"react-dom/client":      "preact/compat",
				"preact/compat":         "preact/compat",
				"preact/hooks":          "preact/hooks",
				"preact":                "preact",
				"react/jsx-runtime":     "preact/jsx-runtime",
				"react/jsx-dev-runtime": "preact/jsx-runtime",
				"preact/jsx-runtime":    "preact/jsx-runtime",
			}
			pb.OnResolve(api.OnResolveOptions{Filter: `^(react|react-dom|react-dom/client|react/jsx-runtime|react/jsx-dev-runtime|preact|preact/compat|preact/hooks|preact/jsx-runtime)$`}, func(args api.OnResolveArgs) (api.OnResolveResult, error) {
				if args.Kind != api.ResolveJSRequireCall {
					return api.OnResolveResult{}, nil
				}
				return api.OnResolveResult{Path: target[args.Path], Namespace: "krate-island-require"}, nil
			})
			pb.OnLoad(api.OnLoadOptions{Filter: `.*`, Namespace: "krate-island-require"}, func(args api.OnLoadArgs) (api.OnLoadResult, error) {
				contents := fmt.Sprintf("export * from %q;\n", args.Path)
				if args.Path == "preact/compat" {
					contents += "import d from \"preact/compat\";\nexport default d;\n"
				}
				return api.OnLoadResult{Contents: &contents, Loader: api.LoaderJS}, nil
			})
		},
	}
}

func (b *Builder) buildIslandSSR(src, resolveDir string, sink *islandCSSSink, prov *islandProviders) (string, error) {
	out, err := b.buildIslandBundle(src, resolveDir, true, islandRenderToStringPlugin(resolveDir), sink, prov)
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
// It returns the page HTML and the <Head> content the islands rendered, which
// belongs in the document head.
func injectIslandSSR(html string, rendered map[string]string) (string, string) {
	ids := make([]string, 0, len(rendered))
	for id := range rendered {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	var head strings.Builder
	for _, id := range ids {
		inner := rendered[id]
		if inner == "" {
			continue
		}
		inner, h := liftIslandHead(inner)
		head.WriteString(h)
		placeholder := `<div data-krate-island="` + id + `" style="display:contents"></div>`
		replacement := `<div data-krate-island="` + id + `" data-krate-island-ssr style="display:contents">` + inner + `</div>`
		html = strings.ReplaceAll(html, placeholder, replacement)
	}
	return html, head.String()
}

const (
	islandHeadOpen  = "<krate-head>"
	islandHeadClose = "</krate-head>"
)

// liftIslandHead removes the <krate-head> wrappers an island's <Head> renders
// from its server markup and returns them separately. Wrappers nest; the
// content of the outermost one is kept, inner wrappers are dropped.
func liftIslandHead(html string) (body, head string) {
	if !strings.Contains(html, islandHeadOpen) {
		return html, ""
	}
	var out, hb strings.Builder
	i := 0
	for i < len(html) {
		open := strings.Index(html[i:], islandHeadOpen)
		if open < 0 {
			break
		}
		out.WriteString(html[i : i+open])
		j := i + open + len(islandHeadOpen)
		depth := 1
		for depth > 0 && j < len(html) {
			nextOpen := strings.Index(html[j:], islandHeadOpen)
			nextClose := strings.Index(html[j:], islandHeadClose)
			if nextClose < 0 {
				hb.WriteString(html[j:])
				j = len(html)
				break
			}
			if nextOpen >= 0 && nextOpen < nextClose {
				hb.WriteString(html[j : j+nextOpen])
				j += nextOpen + len(islandHeadOpen)
				depth++
				continue
			}
			hb.WriteString(html[j : j+nextClose])
			j += nextClose + len(islandHeadClose)
			depth--
		}
		i = j
	}
	out.WriteString(html[min(i, len(html)):])
	return out.String(), hb.String()
}

// islandTagNames are the compiler-provided tags an island module may use as
// globals, the way a Krate component does.
var islandTagNames = []string{"Head", "Suspense", "Script", "Style", "Link", "Image", "Icon", "SyntaxHighlight"}

// injectIslandTags imports the tags a module uses and does not bind itself.
func injectIslandTags(src string) string {
	var names []string
	for _, tag := range islandTagNames {
		if !strings.Contains(src, "<"+tag) && !strings.Contains(src, tag+">") {
			continue
		}
		if declaresBinding(src, tag) || importsBinding(src, tag) {
			continue
		}
		names = append(names, tag)
	}
	if len(names) == 0 {
		return src
	}
	return "import { " + strings.Join(names, ", ") + " } from \"krate-island-tags\"; " + src
}

// islandTagsPlugin serves the module injectIslandTags imports. In the client
// bundle <Head> renders nothing: its content is lifted into the document head
// at build time. In the server render it wraps its children in <krate-head>,
// which liftIslandHead removes from the markup.
func islandTagsPlugin(root string, ssr bool) api.Plugin {
	const ns = "krate-island-tags"
	head := "() => null"
	if ssr {
		head = "(p) => h(\"krate-head\", null, p.children)"
	}
	contents := `import { h, Fragment } from "preact";
const none = () => null;
export const Head = ` + head + `;
export const Script = none, Style = none, Icon = none;
export const Suspense = (p) => h(Fragment, null, p.children);
export const SyntaxHighlight = (p) => h("pre", null, h("code", null, p.children));
export const Link = (p) => { const { children, prefetch, replace, scroll, ...rest } = p; return h("a", rest, children); };
export const Image = (p) => h("img", p);
`
	return api.Plugin{
		Name: "krate-island-tags",
		Setup: func(pb api.PluginBuild) {
			pb.OnResolve(api.OnResolveOptions{Filter: `^krate-island-tags$`}, func(api.OnResolveArgs) (api.OnResolveResult, error) {
				return api.OnResolveResult{Path: "krate-island-tags", Namespace: ns}, nil
			})
			pb.OnLoad(api.OnLoadOptions{Filter: `.*`, Namespace: ns}, func(api.OnLoadArgs) (api.OnLoadResult, error) {
				return api.OnLoadResult{Contents: &contents, Loader: api.LoaderJS, ResolveDir: root}, nil
			})
		},
	}
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
export { useState, useEffect, useLayoutEffect, useRef, useMemo, useCallback, useReducer, useContext, useId, useDebugValue, useImperativeHandle } from "preact/hooks";
// A star export never carries the default export, but classic code does
// "import React from 'react'" (recharts, headlessui, many design systems).
import __compat from "preact/compat";
export default __compat;`
		}
		result := esbuildBuild(api.BuildOptions{
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
	dir := findPreactDir(root)
	if dir == "" {
		return ""
	}
	data, err := os.ReadFile(filepath.Join(dir, "package.json"))
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

// anyPreactIsland reports whether any island of the page runs on Preact.
func anyPreactIsland(exts []irtree.ExternalComponent) bool {
	for _, e := range exts {
		if !e.RealReact {
			return true
		}
	}
	return false
}
