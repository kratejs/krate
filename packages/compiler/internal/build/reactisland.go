package build

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/annotator"
	"github.com/kratejs/krate/packages/compiler/internal/bundler"
	"github.com/kratejs/krate/packages/compiler/internal/diag"
	"github.com/kratejs/krate/packages/compiler/internal/irtree"
	"github.com/kratejs/krate/packages/compiler/internal/resolver"
)

// reactIslandPageLocal is the tag a page that is itself a React island renders:
// the page component is replaced by one island whose root is the page module.
const reactIslandPageLocal = "KrateReactIsland"

var hookNameRe = regexp.MustCompile(`^use[A-Z0-9]`)

// reactIslandModule is a user module whose components call hooks of an npm
// React library (zustand, react-hook-form, swr, react-query, ...) or use a
// CSS-in-JS library. Such code needs React's render model, which Krate's
// lowered components do not have, so the module runs on Preact in an island.
type reactIslandModule struct {
	Path string
	Pkg  string // package whose hook (or styling API) the module uses
	Name string // the hook or API, for diagnostics
	Pos  ast.Pos
	// ClientOnly is set when the module (or one it imports) uses a CSS-in-JS
	// library: styles are created in the browser, so there is no server markup.
	ClientOnly bool
	// Real is set when the module needs real react and react-dom (a React
	// reconciler library): the island is bundled without the Preact alias.
	Real bool
	// Direct is set when the module itself imports the real-React package
	// (Real alone also covers modules that import such a module).
	Direct bool
}

type reactPkgInfo struct {
	Name       string
	ReactBound bool
	// Reconciler is set when the package is built on a React reconciler.
	Reconciler bool
}

var reactPkgCache sync.Map // package dir -> reactPkgInfo

// reactPackageOf reports the npm package that owns file and whether it is
// built against React (declares react or react-dom as a dependency).
func reactPackageOf(file string) reactPkgInfo {
	dir := filepath.Dir(file)
	for {
		if v, ok := reactPkgCache.Load(dir); ok {
			return v.(reactPkgInfo)
		}
		data, err := os.ReadFile(filepath.Join(dir, "package.json"))
		if err == nil {
			var pkg struct {
				Name         string            `json:"name"`
				Dependencies map[string]string `json:"dependencies"`
				Peer         map[string]string `json:"peerDependencies"`
			}
			if json.Unmarshal(data, &pkg) == nil && pkg.Name != "" {
				info := reactPkgInfo{Name: pkg.Name}
				for _, deps := range []map[string]string{pkg.Dependencies, pkg.Peer} {
					if _, ok := deps["react"]; ok {
						info.ReactBound = true
					}
					if _, ok := deps["react-dom"]; ok {
						info.ReactBound = true
					}
					for _, marker := range reconcilerMarkers {
						if _, ok := deps[marker]; ok {
							info.Reconciler = true
						}
					}
				}
				if info.Reconciler {
					reconcilerPkgs.Store(info.Name, true)
				}
				reactPkgCache.Store(dir, info)
				return info
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir || filepath.Base(dir) == "node_modules" {
			return reactPkgInfo{}
		}
		dir = parent
	}
}

// reactLibBindings lists the bindings a module imports from React-bound npm
// packages: local name -> package, plus the namespace imports.
func reactLibBindings(m *bundler.Module, resolve func(importer, spec string) string) (libs, namespaces map[string]string) {
	libs, namespaces = map[string]string{}, map[string]string{}
	for _, stmt := range m.Program.Body {
		imp, ok := stmt.(*ast.ImportStmt)
		if !ok {
			continue
		}
		src := strings.Trim(imp.Source, "\"'")
		if !isBarePackageImport(src) || isCompatAliased(src) || isDataImportSpec(src) ||
			src == "server-only" || src == "client-only" || strings.HasPrefix(src, "node:") {
			continue
		}
		resolved := resolve(m.Path, src)
		if resolved == "" || !resolver.IsCompiledDependency(resolved) || isStylePath(resolved) {
			continue
		}
		info := reactPackageOf(resolved)
		if !info.ReactBound {
			continue
		}
		if imp.Default != "" {
			libs[imp.Default] = info.Name
		}
		if imp.Namespace != "" {
			namespaces[imp.Namespace] = info.Name
		}
		for _, n := range imp.Named {
			local := n.Local
			if local == "" {
				local = n.Remote
			}
			libs[local] = info.Name
		}
	}
	return libs, namespaces
}

// factoryRoot returns the leftmost identifier of a call chain such as
// `create()(fn)`, `lib.factory(fn)` or styled.div`...`: the binding whose
// package built the value.
func factoryRoot(e ast.Expr) string {
	for {
		switch x := e.(type) {
		case *ast.CallExpr:
			e = x.Callee
		case *ast.NewExpr:
			e = x.Callee
		case *ast.Identifier:
			return x.Name
		case *ast.MemberExpr:
			e = x.Object
		case *ast.TaggedTemplateExpr:
			e = x.Tag
		default:
			return ""
		}
	}
}

// moduleLevelDecls yields the variable declarations at the top level of a
// module, including exported ones.
func moduleLevelDecls(prog *ast.Program) []*ast.VarDecl {
	var out []*ast.VarDecl
	for _, stmt := range prog.Body {
		switch s := stmt.(type) {
		case *ast.VarStmt:
			out = append(out, s.Decls...)
		case *ast.ExportStmt:
			if vs, ok := s.Declaration.(*ast.VarStmt); ok {
				out = append(out, vs.Decls...)
			}
		}
	}
	return out
}

// reactHookInfo is what one user module imports and defines in the way of
// hooks from React-bound npm packages.
type reactHookInfo struct {
	m         *bundler.Module
	nsLibs    map[string]string
	hooks     map[string]string // local hook name -> package
	factories map[string]string // component name -> package that built it
	instances map[string]string // value name -> package that built it
	libPkgs   map[string]bool   // every React-bound package the module imports
	pragma    string            // package named by @jsxImportSource
	// tagLibs maps each binding imported from a React-bound package to that
	// package; islandTags maps each binding imported from a React island
	// module to its package. A module that renders either as a JSX tag needs
	// React's render model just as a module calling a hook does.
	tagLibs    map[string]string
	islandTags map[string]string
}

var jsxImportSourceRe = regexp.MustCompile(`@jsxImportSource\s+([^\s*]+)`)

// reactPragmaPackage reports whether a @jsxImportSource pragma names a React
// library (emotion), as opposed to React, Preact or Krate itself.
func reactPragmaPackage(m *bundler.Module, pkg string, resolve func(importer, spec string) string) (string, bool) {
	switch pkg {
	case "react", "preact", "@krate/runtime":
		return "", false
	}
	resolved := resolve(m.Path, pkg+"/jsx-runtime")
	if resolved == "" || !resolver.IsCompiledDependency(resolved) {
		return "", false
	}
	info := reactPackageOf(resolved)
	return info.Name, info.ReactBound
}

// cssInJSPackages creates its styles in the browser while rendering.
func isCSSInJSPackage(pkg string) bool {
	switch {
	case pkg == "styled-components", pkg == "styled-jsx", pkg == "goober",
		strings.HasPrefix(pkg, "@emotion/"), strings.HasPrefix(pkg, "@mui/"),
		strings.HasPrefix(pkg, "@chakra-ui/"), strings.HasPrefix(pkg, "@stitches/"),
		strings.HasPrefix(pkg, "@pigment-css/"):
		return true
	}
	return false
}

func (i *reactHookInfo) hooksOf(name string) (string, bool) {
	if i == nil {
		return "", false
	}
	pkg, ok := i.hooks[name]
	return pkg, ok
}

// addFactories records the module-level bindings a library factory builds, such
// as a store hook: `const useStore = create(...)`.
func (i *reactHookInfo) addFactories(prog *ast.Program, libs, nsLibs map[string]string) {
	for _, d := range moduleLevelDecls(prog) {
		if d.Init == nil {
			continue
		}
		root := factoryRoot(d.Init)
		pkg, ok := libs[root]
		if !ok {
			pkg, ok = nsLibs[root]
		}
		if !ok {
			continue
		}
		if d.IsDestructuring {
			for _, n := range d.Names {
				if hookNameRe.MatchString(n) {
					i.hooks[n] = pkg
				}
			}
		} else if hookNameRe.MatchString(d.Name) {
			i.hooks[d.Name] = pkg
		} else if d.Name != "" && d.Name[0] >= 'A' && d.Name[0] <= 'Z' {
			// A component built by a library factory: styled.div`...`,
			// styled(Base)(...), motion(Button), connect(...)(View).
			i.factories[d.Name] = pkg
		} else if d.Name != "" {
			// A value built by a library (new QueryClient(), createTheme()),
			// which only means something to that library's components.
			i.instances[d.Name] = pkg
		}
	}
}

// detectReactIslandModules finds the user modules that call hooks of a
// React-bound npm package. A module that calls a custom hook of such a module
// is one too, so the set is closed over user-module imports.
func (b *Builder) detectReactIslandModules(mods []*bundler.Module, resolve func(importer, spec string) string) map[string]*reactIslandModule {
	infos := map[string]*reactHookInfo{}
	out := map[string]*reactIslandModule{}
	for _, m := range mods {
		if m == nil || m.Program == nil || m.IsExternal || m.IsCSS || resolver.IsCompiledDependency(m.Path) {
			continue
		}
		libs, nsLibs := reactLibBindings(m, resolve)
		in := &reactHookInfo{m: m, nsLibs: nsLibs, hooks: map[string]string{}, factories: map[string]string{}, instances: map[string]string{}, libPkgs: map[string]bool{},
			tagLibs: map[string]string{}, islandTags: map[string]string{}}
		for local, pkg := range libs {
			in.libPkgs[pkg] = true
			in.tagLibs[local] = pkg
		}
		for local, pkg := range nsLibs {
			in.libPkgs[pkg] = true
			in.tagLibs[local] = pkg
		}
		for local, pkg := range libs {
			if hookNameRe.MatchString(local) {
				in.hooks[local] = pkg
			}
		}
		// A module importing no React library has no factory to look for.
		if len(libs) > 0 || len(nsLibs) > 0 {
			release := b.readProgram(m.Program)
			in.addFactories(m.Program, libs, nsLibs)
			release()
		}
		// A file-level pragma switches the JSX runtime to a React library's
		// (emotion's css prop).
		if mm := jsxImportSourceRe.FindStringSubmatch(m.SourceCode); mm != nil {
			if pkg, ok := reactPragmaPackage(m, mm[1], resolve); ok {
				in.pragma = pkg
			}
		}
		infos[m.Path] = in
		infos[m.Path] = in
	}

	scan := func(in *reactHookInfo) bool {
		if _, done := out[in.m.Path]; done {
			return false
		}
		if len(in.hooks) == 0 && len(in.nsLibs) == 0 && len(in.factories) == 0 && len(in.instances) == 0 && in.pragma == "" &&
			len(in.tagLibs) == 0 && len(in.islandTags) == 0 {
			return false
		}
		defer b.readProgram(in.m.Program)()
		var hit *reactIslandModule
		if in.pragma != "" {
			hit = &reactIslandModule{Path: in.m.Path, Pkg: in.pragma, Name: "@jsxImportSource " + in.pragma, Pos: ast.Pos{Line: 1, Col: 1}}
		}
		if hit == nil && !in.m.IsEntry {
			if pkg := b.realLibPackage(in.libPkgs); pkg != "" {
				hit = &reactIslandModule{Path: in.m.Path, Pkg: pkg, Name: "import of " + pkg, Pos: ast.Pos{Line: 1, Col: 1}}
			}
		}
		if hit == nil && len(in.factories) > 0 {
			tags := map[string]bool{}
			for _, t := range irtree.JSXElementNames(in.m.Program.Body) {
				if i := strings.IndexByte(t, '.'); i >= 0 {
					t = t[:i]
				}
				tags[t] = true
			}
			for _, d := range moduleLevelDecls(in.m.Program) {
				if pkg, ok := in.factories[d.Name]; ok && tags[d.Name] {
					hit = &reactIslandModule{Path: in.m.Path, Pkg: pkg, Name: d.Name, Pos: d.Init.Pos()}
					break
				}
			}
		}
		// A module with an async component is server code: the components of
		// React libraries it renders are package boundaries (see
		// packageboundary.go), not a reason to run the module as an island.
		asyncMod := moduleHasAsyncComponent(in.m.Program)
		irtree.WalkAST(in.m.Program.Body, func(n any) bool {
			if hit != nil {
				return false
			}
			// A component of a React library (a Radix primitive, an icon) or of
			// another island module, rendered as a tag: it only works under
			// React's render model, so the module runs as an island. The entry
			// module is left out: a page or layout renders such tags as islands
			// inside its own static markup (see reactIslandImports).
			if el, ok := n.(*ast.JSXElement); ok && el.Opening != nil && !in.m.IsEntry && !asyncMod {
				root, _, _ := strings.Cut(el.Opening.Name, ".")
				if pkg, ok := in.tagLibs[root]; ok && root != "" {
					hit = &reactIslandModule{Path: in.m.Path, Pkg: pkg, Name: "<" + el.Opening.Name + ">", Pos: el.Position}
					return false
				}
				if pkg, ok := in.islandTags[root]; ok && root != "" {
					hit = &reactIslandModule{Path: in.m.Path, Pkg: pkg, Name: "<" + el.Opening.Name + ">", Pos: el.Position}
					return false
				}
			}
			// A library-built value handed to a JSX attribute (client={qc},
			// theme={theme}) cannot reach a component rendered in an island
			// from outside it.
			if attr, ok := n.(*ast.JSXAttr); ok && len(in.instances) > 0 && attr.Value != nil {
				irtree.WalkAST(attr.Value, func(v any) bool {
					if id, ok := v.(*ast.Identifier); ok && hit == nil {
						if pkg, ok := in.instances[id.Name]; ok {
							hit = &reactIslandModule{Path: in.m.Path, Pkg: pkg, Name: id.Name, Pos: attr.Position}
						}
					}
					return hit == nil
				})
				return hit == nil
			}
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			switch c := call.Callee.(type) {
			case *ast.Identifier:
				if pkg, ok := in.hooks[c.Name]; ok {
					hit = &reactIslandModule{Path: in.m.Path, Pkg: pkg, Name: c.Name, Pos: call.Position}
				}
			case *ast.MemberExpr:
				obj, ok1 := c.Object.(*ast.Identifier)
				prop, ok2 := c.Property.(*ast.Identifier)
				if ok1 && ok2 && !c.Computed && hookNameRe.MatchString(prop.Name) {
					if pkg, ok := in.nsLibs[obj.Name]; ok {
						hit = &reactIslandModule{Path: in.m.Path, Pkg: pkg, Name: obj.Name + "." + prop.Name, Pos: call.Position}
					}
				}
			}
			return true
		})
		if hit != nil {
			out[in.m.Path] = hit
			return true
		}
		return false
	}
	for _, in := range infos {
		scan(in)
	}
	// A hook another user module exports (a store hook made there, or a custom
	// hook that calls a library hook) carries the requirement to every module
	// that calls it.
	for changed := true; changed; {
		changed = false
		for _, in := range infos {
			if _, done := out[in.m.Path]; done {
				continue
			}
			for _, stmt := range in.m.Program.Body {
				imp, ok := stmt.(*ast.ImportStmt)
				if !ok {
					continue
				}
				src := strings.Trim(imp.Source, "\"'")
				target := resolve(in.m.Path, src)
				srcInfo, isInfo := infos[target]
				island, isIsland := out[target]
				if !isInfo && !isIsland {
					continue
				}
				if isIsland {
					for _, local := range []string{imp.Default, imp.Namespace} {
						if local != "" {
							in.islandTags[local] = island.Pkg
						}
					}
					for _, n := range imp.Named {
						local := n.Local
						if local == "" {
							local = n.Remote
						}
						in.islandTags[local] = island.Pkg
					}
				}
				if imp.Default != "" && isIsland && hookNameRe.MatchString(imp.Default) {
					in.hooks[imp.Default] = island.Pkg
				}
				for _, n := range imp.Named {
					local := n.Local
					if local == "" {
						local = n.Remote
					}
					if !hookNameRe.MatchString(local) {
						continue
					}
					if pkg, ok := srcInfo.hooksOf(n.Remote); ok {
						in.hooks[local] = pkg
					} else if isIsland {
						in.hooks[local] = island.Pkg
					}
				}
			}
			if scan(in) {
				changed = true
			}
		}
	}
	for path, im := range out {
		im.ClientOnly = usesCSSInJS(path, infos, resolve, map[string]bool{})
	}
	b.markRealReactModules(out, infos, resolve)
	return out
}

// usesCSSInJS reports whether the module, or a user module it imports, uses a
// CSS-in-JS library.
func usesCSSInJS(path string, infos map[string]*reactHookInfo, resolve func(importer, spec string) string, seen map[string]bool) bool {
	in := infos[path]
	if in == nil || seen[path] {
		return false
	}
	seen[path] = true
	for pkg := range in.libPkgs {
		if isCSSInJSPackage(pkg) {
			return true
		}
	}
	if isCSSInJSPackage(in.pragma) {
		return true
	}
	for _, stmt := range in.m.Program.Body {
		if imp, ok := stmt.(*ast.ImportStmt); ok {
			if usesCSSInJS(resolve(path, strings.Trim(imp.Source, "\"'")), infos, resolve, seen) {
				return true
			}
		}
	}
	return false
}

// reactIslandImports lists the bindings that non-island modules import from
// island modules. Used as tags they render as islands that bundle the module.
func reactIslandImports(mods []*bundler.Module, islands map[string]*reactIslandModule, resolve func(importer, spec string) string) []irtree.ExternalImport {
	var out []irtree.ExternalImport
	seen := map[string]bool{}
	add := func(imp irtree.ExternalImport) {
		if imp.Local == "" || seen[imp.Local] {
			return
		}
		seen[imp.Local] = true
		imp.ReactIsland = true
		imp.ClientOnly = islands[imp.Source].ClientOnly
		imp.RealReact = islands[imp.Source].Real
		out = append(out, imp)
	}
	for _, m := range mods {
		if m == nil || m.Program == nil || m.IsExternal || m.IsCSS {
			continue
		}
		if _, isIsland := islands[m.Path]; isIsland {
			continue
		}
		for _, stmt := range m.Program.Body {
			imp, ok := stmt.(*ast.ImportStmt)
			if !ok {
				continue
			}
			target := resolve(m.Path, strings.Trim(imp.Source, "\"'"))
			if _, isIsland := islands[target]; !isIsland {
				continue
			}
			if imp.Default != "" {
				add(irtree.ExternalImport{Source: target, Local: imp.Default, Default: true})
			}
			if imp.Namespace != "" {
				add(irtree.ExternalImport{Source: target, Local: imp.Namespace, Namespace: true})
			}
			for _, n := range imp.Named {
				local := n.Local
				if local == "" {
					local = n.Remote
				}
				add(irtree.ExternalImport{Source: target, Local: local, Remote: n.Remote})
			}
		}
	}
	return out
}

// reactIslandPage rewrites a page whose module is a React island: its default
// component renders one island rooted at the page module itself. The original
// module is bundled for Preact from disk, so the program is only used here for
// the rest of the route (metadata, params, layouts). The input is not modified.
func reactIslandPage(mod *bundler.Module, pos ast.Pos) (*bundler.Module, string) {
	prog := mod.Program
	name := annotator.FindDefaultExport(prog.Body)
	if name == "" {
		return nil, "declare the page as `export default function Page() { ... }`"
	}
	props := false
	body := []ast.Stmt{&ast.ReturnStmt{Value: &ast.JSXElement{
		Position: pos,
		Opening:  &ast.JSXOpening{Name: reactIslandPageLocal, SelfClosing: true},
	}}}
	stub := func(fn *ast.FnDecl) *ast.FnDecl {
		props = props || len(fn.Params) > 0
		c := *fn
		c.Params = nil
		c.Body = body
		c.Async = false
		return &c
	}
	rewritten := false
	newBody := make([]ast.Stmt, len(prog.Body))
	copy(newBody, prog.Body)
	for i, stmt := range newBody {
		switch s := stmt.(type) {
		case *ast.FnDecl:
			if s.Name == name {
				newBody[i] = stub(s)
				rewritten = true
			}
		case *ast.ExportStmt:
			if fn, ok := s.Declaration.(*ast.FnDecl); ok && fn.Name == name {
				c := *s
				c.Declaration = stub(fn)
				newBody[i] = &c
				rewritten = true
			}
		case *ast.VarStmt:
			decls := make([]*ast.VarDecl, len(s.Decls))
			copy(decls, s.Decls)
			for j, d := range decls {
				if arrow, ok := d.Init.(*ast.ArrowFn); ok && d.Name == name {
					props = props || len(arrow.Params) > 0
					a := *arrow
					a.Params, a.Body, a.Expression, a.Async = nil, body, false, false
					dc := *d
					dc.Init = &a
					decls[j] = &dc
					rewritten = true
				}
			}
			if rewritten {
				c := *s
				c.Decls = decls
				newBody[i] = &c
			}
		}
	}
	if !rewritten {
		return nil, "declare the page as `export default function Page() { ... }`"
	}
	if props {
		return nil, "the page component takes props (params, searchParams), which an island cannot receive; move the component that uses the hook into its own module and render it from a page without props"
	}
	np := *prog
	np.Body = pruneIslandPageBody(newBody, name)
	nm := *mod
	nm.Program = &np
	nm.Imports = nil
	for _, st := range np.Body {
		switch v := st.(type) {
		case *ast.ImportStmt:
			nm.Imports = append(nm.Imports, strings.Trim(v.Source, "\"'"))
		case *ast.ExportStmt:
			if v.ReexportSource != "" {
				nm.Imports = append(nm.Imports, strings.Trim(v.ReexportSource, "\"'"))
			}
		}
	}
	return &nm, ""
}

// reactIslandDiag is the build error for a module that needs React's render
// model but sits in a static layout, which is baked at build time and has no
// island to host it.
func reactIslandDiag(m *reactIslandModule, where string) error {
	return diag.Diagnostic{
		File: m.Path, Line: m.Pos.Line, Col: m.Pos.Col,
		Message: "`" + m.Name + "` from \"" + m.Pkg + "\" needs React's render model, which Krate runs only in an island or a client component, and " + where + " cannot host it",
		Hint:    "make the layout async (`export default async function Layout`) so it renders per request and can host compiled React components, or render the component from a page instead",
	}.WithCode(diag.CodeReactLibNotInIsland)
}

// firstReactIsland returns the island module with the smallest path, so a
// diagnostic is stable across builds.
func firstReactIsland(islands map[string]*reactIslandModule) *reactIslandModule {
	var best *reactIslandModule
	for _, m := range islands {
		if best == nil || m.Path < best.Path {
			best = m
		}
	}
	return best
}

// cssPropWarnings reports `css={...}` on DOM elements in a module that does
// not run as a React island: emotion's css prop is only understood by emotion's
// JSX runtime, so Krate would render it as a plain attribute and drop the styles.
func (b *Builder) cssPropWarnings(m *bundler.Module) []diag.Diagnostic {
	if !strings.Contains(m.SourceCode, "css=") {
		return nil
	}
	defer b.readProgram(m.Program)()
	var out []diag.Diagnostic
	irtree.WalkAST(m.Program.Body, func(n any) bool {
		el, ok := n.(*ast.JSXElement)
		if !ok || el.Opening == nil || el.Opening.Name == "" || el.Opening.Name[0] < 'a' || el.Opening.Name[0] > 'z' {
			return true
		}
		for _, attr := range el.Opening.Attributes {
			if attr == nil || attr.Spread || attr.Name != "css" {
				continue
			}
			if lit, ok := attr.Value.(*ast.Literal); ok && lit.Kind == ast.StringLit {
				continue
			}
			out = append(out, diag.Diagnostic{
				File: m.Path, Line: el.Position.Line, Col: el.Position.Col,
				Message:  "`css` prop on <" + el.Opening.Name + "> is emotion's CSS-in-JS prop; Krate renders it as a plain attribute and the styles are lost",
				Hint:     "add `/** @jsxImportSource @emotion/react */` to the top of the file (the module then runs as a Preact island), or use a class with CSS modules or Tailwind",
				Severity: diag.SeverityWarning,
			}.WithCode(diag.CodeCSSInJS))
		}
		return true
	})
	return out
}

// pruneIslandPageBody drops what the Krate side of an island page does not need
// (helper components, module-level values, side-effect statements): the module
// runs in full inside the island, and the Krate pipeline would otherwise try to
// compile its hooks and initializers for the page script. Imports, exports
// (metadata, generateStaticParams) and whatever the exports reference stay.
func pruneIslandPageBody(body []ast.Stmt, defaultName string) []ast.Stmt {
	declares := func(st ast.Stmt) []string {
		switch d := st.(type) {
		case *ast.FnDecl:
			return []string{d.Name}
		case *ast.ClassDecl:
			return []string{d.Name}
		case *ast.VarStmt:
			var names []string
			for _, decl := range d.Decls {
				names = append(names, decl.Name)
				names = append(names, decl.Names...)
			}
			return names
		}
		return nil
	}
	needed := map[string]bool{defaultName: true}
	refs := func(st ast.Stmt) {
		irtree.WalkAST(st, func(n any) bool {
			if id, ok := n.(*ast.Identifier); ok {
				needed[id.Name] = true
			}
			return true
		})
	}
	for _, st := range body {
		if exp, ok := st.(*ast.ExportStmt); ok && !exp.Default {
			refs(st)
			if exp.Local != "" {
				needed[exp.Local] = true
			}
			for _, n := range exp.Named {
				needed[n.Local] = true
			}
		}
	}
	keep := make([]bool, len(body))
	for changed := true; changed; {
		changed = false
		for i, st := range body {
			if keep[i] {
				continue
			}
			for _, n := range declares(st) {
				if n != "" && needed[n] {
					keep[i] = true
					refs(st)
					changed = true
					break
				}
			}
		}
	}
	out := make([]ast.Stmt, 0, len(body))
	for i, st := range body {
		switch v := st.(type) {
		case *ast.ExportStmt:
			out = append(out, st)
		case *ast.ImportStmt:
			// Side-effect imports (stylesheets) stay; a binding nothing kept
			// reads has no use on this side.
			bound := v.Default != "" || v.Namespace != "" || len(v.Named) > 0
			used := !bound || needed[v.Default] || needed[v.Namespace]
			for _, n := range v.Named {
				local := n.Local
				if local == "" {
					local = n.Remote
				}
				used = used || needed[local]
			}
			if used {
				out = append(out, st)
			}
		default:
			if keep[i] {
				out = append(out, st)
			}
		}
	}
	return out
}

// krateSideModules keeps the modules the Krate pipeline still compiles: those
// reachable from the entry without going through an island module. A module
// that only an island imports (its store, its hooks, the library) is bundled by
// the island build alone.
func krateSideModules(mods []*bundler.Module, entry *bundler.Module, islands map[string]*reactIslandModule, resolve func(importer, spec string) string) []*bundler.Module {
	byPath := make(map[string]*bundler.Module, len(mods))
	for _, m := range mods {
		if m != nil {
			byPath[m.Path] = m
		}
	}
	keep := map[string]bool{entry.Path: true}
	queue := []*bundler.Module{entry}
	for len(queue) > 0 {
		m := queue[0]
		queue = queue[1:]
		// The entry of an island page is the Krate shell of the page; its own
		// imports were pruned with the rest of its body.
		for _, spec := range m.Imports {
			path := resolve(m.Path, spec)
			next := byPath[path]
			if next == nil || keep[path] {
				continue
			}
			if _, isIsland := islands[path]; isIsland {
				continue
			}
			keep[path] = true
			queue = append(queue, next)
		}
	}
	out := make([]*bundler.Module, 0, len(mods))
	for _, m := range mods {
		if m != nil && keep[m.Path] {
			out = append(out, m)
		}
	}
	return out
}
