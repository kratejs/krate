package build

import (
	"strings"
	"sync"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/bundler"
	"github.com/kratejs/krate/packages/compiler/internal/diag"
	"github.com/kratejs/krate/packages/compiler/internal/irtree"
	"github.com/kratejs/krate/packages/compiler/internal/resolver"
)

// A statically built layout can render React islands: a client component of
// the project (a "use client" toaster wrapper) or a component of a package
// that keeps module-level state its callers share (sonner's Toaster). The
// layout emits a placeholder per island, and every page under the layout
// builds the island chunks together with its own, so the layout's islands and
// the islands of the page share one copy of each library module (sonner's
// toast() reaches the Toaster of the layout). Layouts that host islands are
// swapped on client-side navigation instead of being kept mounted.

// layoutIslands is what a layout hands to the pages it wraps.
type layoutIslands struct {
	Exts    []irtree.ExternalComponent
	Imports []irtree.ExternalImport
}

// layoutIslandCache holds the islands of each layout built so far
// (path -> *layoutIslands, nil when the layout hosts none).
var layoutIslandCache sync.Map

// sharedStatePackages are React packages whose components read module-level
// state that plain functions of the same package write (toast() and <Toaster />).
// A component of one of them rendered by a layout runs as an island, so the
// calls made by the islands of a page reach it.
var sharedStatePackages = map[string]bool{
	"sonner":          true,
	"react-hot-toast": true,
}

// layoutIslandImports lists the bindings of the layout's modules that render
// as islands: bindings of island modules and components of
// sharedStatePackages used as tags.
func layoutIslandImports(mods []*bundler.Module, islands map[string]*reactIslandModule, resolve func(importer, spec string) string) []irtree.ExternalImport {
	out := reactIslandImports(mods, islands, resolve)
	seen := map[string]bool{}
	for _, imp := range out {
		seen[imp.Local] = true
	}
	for _, m := range mods {
		if m == nil || m.Program == nil || m.IsExternal || m.IsCSS || resolver.IsCompiledDependency(m.Path) {
			continue
		}
		if _, isIsland := islands[m.Path]; isIsland {
			continue
		}
		var tags map[string]bool
		for _, stmt := range m.Program.Body {
			imp, ok := stmt.(*ast.ImportStmt)
			if !ok {
				continue
			}
			src := strings.Trim(imp.Source, "\"'")
			if !sharedStatePackages[src] {
				continue
			}
			if tags == nil {
				tags = map[string]bool{}
				for _, t := range irtree.JSXElementNames(m.Program.Body) {
					root, _, _ := strings.Cut(t, ".")
					tags[root] = true
				}
			}
			for _, n := range imp.Named {
				local := n.Local
				if local == "" {
					local = n.Remote
				}
				if !tags[local] || seen[local] {
					continue
				}
				seen[local] = true
				out = append(out, irtree.ExternalImport{Source: src, Local: local, Remote: n.Remote, ReactIsland: true})
			}
		}
	}
	return out
}

// layoutIslandsFor returns the islands the layouts around a page host,
// outermost layout first.
func (b *Builder) layoutIslandsFor(page string) ([]irtree.ExternalComponent, []irtree.ExternalImport) {
	stack := b.routeStack(page)
	var exts []irtree.ExternalComponent
	var imps []irtree.ExternalImport
	for i := len(stack) - 1; i >= 0; i-- {
		v, ok := layoutIslandCache.Load(stack[i].Path)
		if !ok {
			continue
		}
		if li, _ := v.(*layoutIslands); li != nil {
			exts = append(exts, li.Exts...)
			imps = append(imps, li.Imports...)
		}
	}
	return exts, imps
}

// layoutHostsIslands reports whether the layout at path renders islands.
func layoutHostsIslands(path string) bool {
	v, ok := layoutIslandCache.Load(path)
	if !ok {
		return false
	}
	li, _ := v.(*layoutIslands)
	return li != nil && len(li.Exts) > 0
}

// checkLayoutIslands rejects an island that reads the layout's own state,
// which an island cannot see.
func checkLayoutIslands(exts []irtree.ExternalComponent) error {
	for _, e := range exts {
		if e.PropsJS != "" {
			return diag.Diagnostic{
				File: e.File, Line: e.Line, Col: e.Col,
				Message: "island <" + e.Local + "> in a layout reads the layout's own state, which an island cannot see",
				Hint:    "pass constants or imported values as props, or move the state into the island's module",
			}.WithCode(diag.CodeReactLibNotInIsland)
		}
	}
	return nil
}
