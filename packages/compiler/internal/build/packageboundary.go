package build

import (
	"encoding/json"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/evanw/esbuild/pkg/api"
	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/bundler"
	"github.com/kratejs/krate/packages/compiler/internal/diag"
	"github.com/kratejs/krate/packages/compiler/internal/irtree"
	"github.com/kratejs/krate/packages/compiler/internal/resolver"
)

// Package boundaries.
//
// A server component (an async page, layout or component) cannot run the
// components of a compiled React package: lucide-react icons, Radix
// primitives and the like need React's render model. Next treats a package
// that ships a "use client" directive as a client boundary, and so does
// Krate here, for any React-bound package: the components a server module
// imports from it are replaced, in the page's server bundle, by boundary
// stubs. The server renders them with Preact (React aliased to preact/compat)
// into the response; a component that has state or effects also becomes a
// <krate-island> that hydrates in the browser, while a stateless one (an
// icon) stays plain HTML and ships no JavaScript. Radix compound components
// keep their context because nested boundary elements travel with their
// parent (see clientBoundary in @krate/runtime/server).

// pkgBoundarySpec is one bare import specifier of a React-bound package and
// the exports of it that server modules use as components.
type pkgBoundarySpec struct {
	spec       string
	resolveDir string
	exports    map[string]bool
	real       bool
	// whole is set when a module uses the package as a namespace value
	// (`Icons[name]`, `Object.keys(Icons)`): every export is then a boundary export.
	whole bool
}

// packageBoundaryEntry is the JS preamble that defines `__m`, an object of
// exactly the exports a page uses, so a barrel such as lucide-react is
// tree-shaken down to the icons in use.
func packageBoundaryEntry(spec string, exports []string, whole bool) string {
	var imports, props []string
	if whole {
		return "import * as __ns from " + strconv.Quote(spec) + ";\nconst __m = { ...__ns };\n"
	}
	for i, name := range exports {
		local := "__e" + strconv.Itoa(i)
		if name == "default" {
			imports = append(imports, "import "+local+" from "+strconv.Quote(spec)+";")
		} else {
			imports = append(imports, "import { "+name+" as "+local+" } from "+strconv.Quote(spec)+";")
		}
		props = append(props, strconv.Quote(name)+": "+local)
	}
	return strings.Join(imports, "\n") + "\nconst __m = { " + strings.Join(props, ", ") + " };\n"
}

// preparePackageBoundaries finds the components that the non-client user
// modules of a server tree import from React-bound npm packages and builds a
// boundary for each package specifier. It returns nil when no module of the
// bundle reaches a package that way.
func (b *Builder) preparePackageBoundaries(mods []*bundler.Module, resolve func(importer, spec string) string, skip map[string]bool) ([]ClientBoundary, error) {
	specs := map[string]*pkgBoundarySpec{}
	for _, m := range mods {
		if m == nil || m.Program == nil || m.IsExternal || m.IsCSS || resolver.IsCompiledDependency(m.Path) || skip[m.Path] {
			continue
		}
		if bundler.HasUseClientDirective(m.SourceCode) {
			continue
		}
		b.collectPackageComponents(m, resolve, specs)
	}
	if len(specs) == 0 {
		return nil, nil
	}
	names := make([]string, 0, len(specs))
	for s := range specs {
		names = append(names, s)
	}
	sort.Strings(names)
	var out []ClientBoundary
	for _, name := range names {
		sp := specs[name]
		exports := make([]string, 0, len(sp.exports))
		for e := range sp.exports {
			exports = append(exports, e)
		}
		if sp.whole {
			all, err := b.packageExportNames(sp.spec, sp.resolveDir)
			if err != nil {
				return nil, err
			}
			seen := map[string]bool{}
			for _, e := range exports {
				seen[e] = true
			}
			for _, e := range all {
				if !seen[e] {
					exports = append(exports, e)
				}
			}
		}
		sort.Strings(exports)
		id := "c" + hashContent([]byte(sp.spec+"\x00"+strings.Join(exports, "\x00")))
		entry := packageBoundaryEntry(sp.spec, exports, sp.whole)
		if sp.real {
			cb := ClientBoundary{ModuleID: id, SourcePath: id, Spec: sp.spec, Exports: exports, Entry: entry, ResolveDir: sp.resolveDir, Real: true}
			cb.ChunkURL = b.registerRealBoundary(cb)
			out = append(out, cb)
			continue
		}
		url, stateful, err := b.clientChunkFor(id, sp.resolveDir, entry)
		if err != nil {
			return nil, err
		}
		out = append(out, ClientBoundary{
			ModuleID: id, SourcePath: id, Spec: sp.spec, Exports: exports, ChunkURL: url,
			Entry: entry, ResolveDir: sp.resolveDir, Hydrate: stateful,
		})
	}
	return out, nil
}

// collectPackageComponents records, into specs, the bindings of m imported
// from React-bound packages that m renders as components: used as a JSX tag,
// or a capitalized name handed around as a value (`icon={Users}`).
func (b *Builder) collectPackageComponents(m *bundler.Module, resolve func(importer, spec string) string, specs map[string]*pkgBoundarySpec) {
	var imports []*ast.ImportStmt
	var reexports []*ast.ExportStmt
	for _, stmt := range m.Program.Body {
		switch s := stmt.(type) {
		case *ast.ImportStmt:
			imports = append(imports, s)
		case *ast.ExportStmt:
			if s.ReexportSource != "" && len(s.ReexportNamed) > 0 {
				reexports = append(reexports, s)
			}
		}
	}
	if len(imports) == 0 && len(reexports) == 0 {
		return
	}
	defer b.readProgram(m.Program)()
	// valueRefs are capitalized names used as values (handed to a prop, kept
	// in a table); members are the `NS.Name` accesses of namespace imports;
	// bareNS are namespaces used as a whole, which no boundary can cover.
	valueRefs := map[string]bool{}
	members := map[string]map[string]bool{}
	bareNS := map[string]bool{}
	tagRoots := map[string]bool{}
	for _, t := range irtree.JSXElementNames(m.Program.Body) {
		root, rest, dotted := strings.Cut(t, ".")
		tagRoots[root] = true
		if dotted {
			first, _, _ := strings.Cut(rest, ".")
			if members[root] == nil {
				members[root] = map[string]bool{}
			}
			members[root][first] = true
		}
	}
	skip := map[*ast.Identifier]bool{}
	for _, stmt := range m.Program.Body {
		if exp, ok := stmt.(*ast.ExportStmt); ok && exp.ReexportSource == "" {
			for _, n := range exp.Named {
				valueRefs[n.Local] = true
			}
		}
	}
	irtree.WalkAST(m.Program.Body, func(n any) bool {
		switch x := n.(type) {
		case *ast.CallExpr:
			if id, ok := x.Callee.(*ast.Identifier); ok {
				skip[id] = true
			}
		case *ast.NewExpr:
			if id, ok := x.Callee.(*ast.Identifier); ok {
				skip[id] = true
			}
		case *ast.MemberExpr:
			if obj, ok := x.Object.(*ast.Identifier); ok {
				skip[obj] = true
				if prop, ok := x.Property.(*ast.Identifier); ok && !x.Computed {
					if members[obj.Name] == nil {
						members[obj.Name] = map[string]bool{}
					}
					members[obj.Name][prop.Name] = true
				} else {
					bareNS[obj.Name] = true
				}
			}
		case *ast.Identifier:
			if skip[x] {
				return true
			}
			bareNS[x.Name] = true
			if x.Name != "" && x.Name[0] >= 'A' && x.Name[0] <= 'Z' {
				valueRefs[x.Name] = true
			}
		}
		return true
	})
	isComponent := func(local string) bool {
		return tagRoots[local] || valueRefs[local]
	}
	reactSpec := func(src string) bool {
		if !isBarePackageImport(src) || isCompatAliased(src) || isDataImportSpec(src) ||
			src == "server-only" || src == "client-only" || strings.HasPrefix(src, "node:") {
			return false
		}
		resolved := resolve(m.Path, src)
		if resolved == "" || !resolver.IsCompiledDependency(resolved) || isStylePath(resolved) {
			return false
		}
		return reactPackageOf(resolved).ReactBound
	}
	add := func(src, export string) {
		sp := specs[src]
		if sp == nil {
			sp = &pkgBoundarySpec{spec: src, resolveDir: filepath.Dir(m.Path), exports: map[string]bool{}, real: b.isRealReactPackage(packageNameOf(src))}
			specs[src] = sp
		}
		if export != "" {
			sp.exports[export] = true
		}
	}
	for _, imp := range imports {
		src := strings.Trim(imp.Source, "\"'")
		if !reactSpec(src) {
			continue
		}
		if imp.Default != "" && isComponent(imp.Default) {
			add(src, "default")
		}
		if imp.Namespace != "" && bareNS[imp.Namespace] {
			// The namespace is used as a value, so any export may be read.
			add(src, "")
			specs[src].whole = true
		}
		if imp.Namespace != "" && !bareNS[imp.Namespace] {
			// `import * as Dialog from "@radix-ui/react-dialog"`: the members the
			// module reads are the exports; each is a component or a namespace.
			for member := range members[imp.Namespace] {
				add(src, member)
			}
		}
		// Every named import is an export of the boundary: the page's bundle
		// imports the specifier's boundary module, so a name used other than
		// as a tag (`Icon.displayName`, `Icon()`, `typeof Icon`) must exist too.
		for _, n := range imp.Named {
			remote := n.Remote
			if remote == "" {
				remote = n.Local
			}
			if remote != "" {
				add(src, bundler.OriginalName(remote))
			}
		}
	}
	// `export { Check, X } from "lucide-react"` (an icon barrel): the
	// capitalized names are components.
	for _, exp := range reexports {
		src := strings.Trim(exp.ReexportSource, "\"'")
		if !reactSpec(src) {
			continue
		}
		for _, n := range exp.ReexportNamed {
			if n.Local != "" && n.Local[0] >= 'A' && n.Local[0] <= 'Z' {
				add(src, bundler.OriginalName(n.Local))
			}
		}
	}
}

// serverTreeBoundaries builds the package boundaries of a page that renders
// server components. A server-rendered page (serverPage) also has its
// React-bound modules validated: a library hook outside a "use client" module
// is KR8013.
func (b *Builder) serverTreeBoundaries(bundle *bundler.Bundle, islands map[string]*reactIslandModule, resolve func(importer, spec string) string, serverPage bool) ([]ClientBoundary, error) {
	if serverPage {
		if err := serverPageIslandError(islands, bundle.Modules, "a server-rendered page"); err != nil {
			return nil, err
		}
	}
	return b.preparePackageBoundaries(bundle.Modules, resolve, realModulePaths(bundle.Modules, islands))
}

// moduleHasAsyncComponent reports whether a module declares an async
// component (a capitalized async function or arrow) at its top level.
func moduleHasAsyncComponent(prog *ast.Program) bool {
	capital := func(name string) bool { return name != "" && name[0] >= 'A' && name[0] <= 'Z' }
	for _, stmt := range prog.Body {
		switch s := stmt.(type) {
		case *ast.FnDecl:
			if s.Async && capital(s.Name) {
				return true
			}
		case *ast.VarStmt:
			for _, d := range s.Decls {
				if arrow, ok := d.Init.(*ast.ArrowFn); ok && arrow.Async && capital(d.Name) {
					return true
				}
			}
		case *ast.ExportStmt:
			switch d := s.Declaration.(type) {
			case *ast.FnDecl:
				if d.Async && (capital(d.Name) || s.Default) {
					return true
				}
			case *ast.VarStmt:
				for _, v := range d.Decls {
					if arrow, ok := v.Init.(*ast.ArrowFn); ok && arrow.Async && capital(v.Name) {
						return true
					}
				}
			}
		}
	}
	return false
}

// entryIsAsync reports whether a page's default export is an async function:
// the page then renders per request, and its React-bound modules stay in the
// module graph as server modules instead of leaving it as islands.
func entryIsAsync(prog *ast.Program) bool {
	if prog == nil {
		return false
	}
	local := ""
	for _, stmt := range prog.Body {
		exp, ok := stmt.(*ast.ExportStmt)
		if !ok || !exp.Default {
			continue
		}
		if fn, ok := exp.Declaration.(*ast.FnDecl); ok {
			return fn.Async
		}
		local = exp.Local
	}
	if local == "" {
		return false
	}
	for _, stmt := range prog.Body {
		switch s := stmt.(type) {
		case *ast.FnDecl:
			if s.Name == local {
				return s.Async
			}
		case *ast.VarStmt:
			for _, d := range s.Decls {
				if arrow, ok := d.Init.(*ast.ArrowFn); ok && d.Name == local {
					return arrow.Async
				}
			}
		}
	}
	return false
}

// reactIslandTagOnly reports whether a React island module only renders
// components of React libraries (as tags), without calling their hooks. A
// server module like that is not an error: its components become package
// boundaries.
func reactIslandTagOnly(m *reactIslandModule) bool {
	return strings.HasPrefix(m.Name, "<")
}

// serverPageIslandError returns the KR8013 error of a server-rendered page
// whose modules need React's render model in a way boundaries cannot give:
// a hook of a React library called outside a "use client" module.
func serverPageIslandError(islands map[string]*reactIslandModule, mods []*bundler.Module, where string) error {
	clientPaths := map[string]bool{}
	for _, m := range mods {
		if m != nil && m.Program != nil && !m.IsExternal && bundler.HasUseClientDirective(m.SourceCode) {
			clientPaths[m.Path] = true
		}
	}
	paths := make([]string, 0, len(islands))
	for p := range islands {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, p := range paths {
		im := islands[p]
		if clientPaths[p] || reactIslandTagOnly(im) || im.Direct {
			continue
		}
		return diag.Diagnostic{
			File: im.Path, Line: im.Pos.Line, Col: im.Pos.Col,
			Message: "`" + im.Name + "` from \"" + im.Pkg + "\" needs React's render model, and " + where + " renders per request on the server",
			Hint:    "add \"use client\" at the top of " + filepath.Base(im.Path) + " so it hydrates as a client component, or replace the hook with createSignal / createResource",
		}.WithCode(diag.CodeReactLibNotInIsland)
	}
	return nil
}

// packageExportNames lists the runtime export names of a package entry as
// esbuild resolves them (`export *` chains and aliased re-exports included).
// A package whose exports cannot be listed, such as a CommonJS one, is an
// error: a namespace boundary would otherwise be empty.
func (b *Builder) packageExportNames(spec, resolveDir string) ([]string, error) {
	res := esbuildBuild(api.BuildOptions{
		Stdin:         &api.StdinOptions{Contents: "export * from " + strconv.Quote(spec) + ";\n", ResolveDir: resolveDir, Sourcefile: "krate-package-exports.js", Loader: api.LoaderJS},
		AbsWorkingDir: b.Root,
		Bundle:        true,
		Write:         false,
		Metafile:      true,
		Outdir:        filepath.Join(b.Root, ".krate", "pkg-exports"),
		Format:        api.FormatESModule,
		Platform:      api.PlatformBrowser,
		External:      []string{"react", "react/*", "react-dom", "react-dom/*", "preact", "preact/*"},
		Alias:         islandAlias(b.Root, false),
		LogLevel:      api.LogLevelSilent,
	})
	var names []string
	if len(res.Errors) == 0 && res.Metafile != "" {
		var meta struct {
			Outputs map[string]struct {
				Exports []string `json:"exports"`
			} `json:"outputs"`
		}
		if json.Unmarshal([]byte(res.Metafile), &meta) == nil {
			for _, o := range meta.Outputs {
				names = append(names, o.Exports...)
			}
		}
	}
	if len(names) == 0 {
		return nil, diag.Diagnostic{
			File:    spec,
			Message: "`" + spec + "` is used as a namespace value in a server module, but its exports cannot be listed to build a client boundary for it",
			Hint:    "import the components you use by name (import { A, B } from \"" + spec + "\") or move the dynamic lookup into a \"use client\" component",
		}
	}
	sort.Strings(names)
	return names, nil
}
