package build

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/evanw/esbuild/pkg/api"
	"github.com/kratejs/krate/packages/compiler/internal/annotator"
	"github.com/kratejs/krate/packages/compiler/internal/diag"
	"github.com/kratejs/krate/packages/compiler/internal/tsstrip"
)

// Async layouts.
//
// A layout that awaits data cannot be baked into the static shell. It runs as
// a server component instead: the page and every layout between it and the
// outermost async layout render per request in the SSR sidecar as one tree,
//
//	AsyncLayout(children = InnerLayout(children = Page))
//
// and that tree replaces the page region of the shell, which keeps the layouts
// above the async one. redirect(), notFound() and the other request APIs
// behave as they do in an async page, so an async layout can gate its
// subtree. Pages under an async layout are rendered per request, whether or
// not they are async themselves, and follow the server component rules.

// layoutCut is the request-time part of a page's layout stack.
type layoutCut struct {
	// Layouts are the absolute paths of the layouts the sidecar renders around
	// the page, innermost first, ending with the outermost async layout.
	Layouts []string
	// Covered is the number of route boundaries (from the page outward) that
	// leave the baked stack.
	Covered int
	// CSS and Deps are the layouts' stylesheets and module graphs, which stay
	// in the page build.
	CSS        string
	Deps       []string
	Boundaries []ClientBoundary
	// Actions is true when a layout uses server actions.
	Actions bool
}

var asyncLayoutMemo sync.Map // layout path -> bool, cleared per build

func (b *Builder) layoutIsAsync(path string) bool {
	if v, ok := asyncLayoutMemo.Load(path); ok {
		return v.(bool)
	}
	async := false
	if data, err := os.ReadFile(path); err == nil && strings.Contains(string(data), "async") {
		if prog, errs := tsstrip.Parse(path, string(data), tsstrip.Options{}); prog != nil && len(errs) == 0 {
			async = entryIsAsync(prog)
		}
	}
	asyncLayoutMemo.Store(path, async)
	return async
}

// asyncLayoutCut finds the outermost async layout of a page's route stack. It
// returns nil when no layout above the page is async.
func (b *Builder) asyncLayoutCut(page string) (*layoutCut, error) {
	stack := b.routeStack(page)
	cut := -1
	for i, rb := range stack {
		if rb.Kind == "layout" && b.layoutIsAsync(rb.Path) {
			cut = i
		}
	}
	if cut < 0 {
		return nil, nil
	}
	out := &layoutCut{Covered: cut + 1}
	for _, rb := range stack[:cut+1] {
		if rb.Kind != "layout" {
			return nil, diag.Diagnostic{
				File:    rb.src(),
				Line:    1,
				Col:     1,
				Message: filepath.Base(rb.src()) + " sits below the async layout " + filepath.Base(stack[cut].Path) + ", which renders per request; " + withArticle(rb.Kind) + " cannot run inside it",
				Hint:    "move the " + rb.Kind + " above the async layout, or make the layout synchronous and fetch data in an async child component",
			}.WithCode(diag.CodeAsyncLayout)
		}
		out.Layouts = append(out.Layouts, rb.Path)
	}
	return out, nil
}

// prepareLayoutCut bundles the request-time layouts of a page: their CSS, their
// module graph, and the client boundaries and package boundaries of the
// components they render.
func (b *Builder) prepareLayoutCut(page string) (*layoutCut, error) {
	cut, err := b.asyncLayoutCut(page)
	if err != nil || cut == nil {
		return nil, err
	}
	seen := map[string]bool{}
	for _, layoutPath := range cut.Layouts {
		bnd := b.newBundler()
		bundle, err := bnd.Bundle(layoutPath)
		if err != nil {
			return nil, err
		}
		cut.Deps = append(cut.Deps, layoutDepPaths(layoutPath, bundle)...)
		cut.Actions = cut.Actions || bundleHasActions(bundle)
		cut.CSS = bundle.CSS + cut.CSS
		b.modCSS.store(layoutPath, bundle.CSSModules)
		mod := findEntryModule(bundle.Modules)
		if mod == nil || mod.Program == nil || !hasDefaultExportStmt(mod.Program) {
			return nil, diag.Diagnostic{
				File: layoutPath, Line: 1, Col: 1,
				Message: "no default export: a layout must default-export a component",
				Hint:    "add `export default async function Layout({ children }) { return <>{children}</> }`",
			}
		}
		if err := b.ensureTransformedModules(bundle.Modules); err != nil {
			return nil, err
		}
		ann := annotator.Annotate(mod.Program, b.Cfg, layoutPath, mod.SourceCode)
		extra := moduleSources(bundle, mod)
		annotator.MergeModuleFunctions(ann, extra)
		annotator.MergeImportAliases(ann, extra, annotator.ModuleSource{Program: mod.Program, Path: mod.Path, RawSource: mod.SourceCode, Resolver: bundle.Resolver})
		annotator.ReclassifyTiers(ann, b.Cfg)
		boundaries, err := b.prepareClientBoundaries(bundle, ann, true)
		if err != nil {
			return nil, err
		}
		islands := b.detectReactIslandModules(bundle.Modules, bnd.Resolve)
		pkgs, err := b.serverTreeBoundaries(bundle, islands, bnd.Resolve, true)
		if err != nil {
			return nil, err
		}
		for _, cb := range append(boundaries, pkgs...) {
			if !seen[cb.ModuleID] {
				seen[cb.ModuleID] = true
				cut.Boundaries = append(cut.Boundaries, cb)
			}
		}
	}
	return cut, nil
}

// mergeBoundaries adds the boundaries of the request-time layouts to a page's
// own, keeping one entry per module.
func mergeBoundaries(own, extra []ClientBoundary) []ClientBoundary {
	if len(extra) == 0 {
		return own
	}
	seen := map[string]bool{}
	for _, cb := range own {
		seen[cb.ModuleID] = true
	}
	for _, cb := range extra {
		if !seen[cb.ModuleID] {
			seen[cb.ModuleID] = true
			own = append(own, cb)
		}
	}
	return own
}

// bakedRouteStack is the part of a page's route stack that is baked into the
// shell: everything above the outermost async layout.
func (b *Builder) bakedRouteStack(page string, cut *layoutCut) []routeBoundary {
	stack := b.routeStack(page)
	if cut == nil || cut.Covered > len(stack) {
		return stack
	}
	return stack[cut.Covered:]
}

// composedEntrySource is the entry module of a page server bundle whose page
// renders inside async layouts. pageModule is the virtual module id of the
// page source, layouts are listed innermost first.
func composedEntrySource(pageModule string, layouts []string) string {
	var sb strings.Builder
	sb.WriteString("import { jsx as __kjsx } from \"@krate/runtime/server/jsx-runtime\";\n")
	sb.WriteString("import __KratePage from " + strconv.Quote(pageModule) + ";\n")
	for i, l := range layouts {
		sb.WriteString("import __KrateLayout" + strconv.Itoa(i) + " from " + strconv.Quote(filepath.ToSlash(l)) + ";\n")
	}
	sb.WriteString("export * from " + strconv.Quote(pageModule) + ";\n")
	sb.WriteString("export default async function __KrateComposed(props) {\n")
	sb.WriteString("  const params = props && props.params;\n")
	sb.WriteString("  let node = __kjsx(__KratePage, props || {});\n")
	for i := range layouts {
		sb.WriteString("  node = __kjsx(__KrateLayout" + strconv.Itoa(i) + ", { params, children: node });\n")
	}
	sb.WriteString("  return node;\n}\n")
	return sb.String()
}

// composedLayoutPlugin resolves the virtual page module of a composed entry to
// the page's injected source, and gives the layouts the same global imports
// (Head, Link, ...) the page gets.
func composedLayoutPlugin(root string, pageModule, pageSource, pageDir string, layouts []string) api.Plugin {
	isLayout := map[string]bool{}
	for _, l := range layouts {
		isLayout[filepath.Clean(l)] = true
	}
	return api.Plugin{
		Name: "krate-composed-layouts",
		Setup: func(pb api.PluginBuild) {
			const ns = "krate-composed-page"
			pb.OnResolve(api.OnResolveOptions{Filter: "^" + specFilterEscape.ReplaceAllStringFunc(pageModule, func(s string) string { return `\` + s }) + "$"}, func(args api.OnResolveArgs) (api.OnResolveResult, error) {
				return api.OnResolveResult{Path: pageModule, Namespace: ns}, nil
			})
			pb.OnLoad(api.OnLoadOptions{Filter: ".*", Namespace: ns}, func(api.OnLoadArgs) (api.OnLoadResult, error) {
				return api.OnLoadResult{Contents: &pageSource, Loader: api.LoaderTSX, ResolveDir: pageDir}, nil
			})
			pb.OnLoad(api.OnLoadOptions{Filter: `\.[cm]?[jt]sx?$`, Namespace: "file"}, func(args api.OnLoadArgs) (api.OnLoadResult, error) {
				if !isLayout[filepath.Clean(args.Path)] {
					return api.OnLoadResult{}, nil
				}
				data, err := readServerSource(root, args.Path)
				if err != nil {
					return api.OnLoadResult{}, err
				}
				src := injectServerGlobals(string(data))
				return api.OnLoadResult{Contents: &src, Loader: api.LoaderTSX, ResolveDir: filepath.Dir(args.Path)}, nil
			})
		},
	}
}

// layoutCutLayouts lists the request-time layouts of a cut (nil for no cut).
func layoutCutLayouts(cut *layoutCut) []string {
	if cut == nil {
		return nil
	}
	return cut.Layouts
}

// withArticle prefixes a route block kind with "a" or "an".
func withArticle(kind string) string {
	if kind != "" && strings.ContainsRune("aeiou", rune(kind[0])) {
		return "an " + kind
	}
	return "a " + kind
}
