package build

import (
	"fmt"
	"github.com/kratejs/krate/packages/compiler/internal/bundler"
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
	// Dropped marks the cut of a segment error page: the covered layouts are
	// left out and the page is prerendered inside the baked layouts only.
	Dropped bool
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
		if rb.Kind == "template" {
			return nil, diag.Diagnostic{
				File:    rb.src(),
				Line:    1,
				Col:     1,
				Message: filepath.Base(rb.src()) + " sits below the async layout " + filepath.Base(stack[cut].Path) + ", which renders per request; a template hydrates in the browser and cannot run inside it",
				Hint:    "move the template above the async layout, or make the layout synchronous and fetch data in an async child component",
			}.WithCode(diag.CodeAsyncLayout)
		}
		path := rb.Path
		if rb.Kind == "error" {
			// The page renders per request here, so the segment's error.tsx
			// catches on the server instead of through the client boundary.
			gen, err := b.serverBoundaryLayout(rb.src())
			if err != nil {
				return nil, diag.Diagnostic{File: rb.src(), Line: 1, Col: 1, Message: err.Error(), Hint: "check that .krate is writable"}
			}
			path = gen
		}
		out.Layouts = append(out.Layouts, path)
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
	if kind, ok := segmentKindOfFile(page); ok && kind == SegmentError {
		// An error page answers while the app is failing, so it is prerendered
		// and never depends on the renderer or on a layout that awaits data.
		fmt.Fprintf(b.stderr(), "  %s*%s %s is prerendered without the async layout %s\n", cCyan, cReset, filepath.Base(page), filepath.Base(cut.Layouts[len(cut.Layouts)-1]))
		return &layoutCut{Covered: cut.Covered, Dropped: true}, nil
	}
	seen := map[string]bool{}
	for _, layoutPath := range cut.Layouts {
		part, err := b.layoutCutPartFor(layoutPath)
		if err != nil {
			return nil, err
		}
		cut.Deps = append(cut.Deps, part.deps...)
		cut.Actions = cut.Actions || part.actions
		cut.CSS = part.css + cut.CSS
		b.modCSS.store(layoutPath, part.cssModules)
		for _, cb := range part.boundaries {
			if !seen[cb.ModuleID] {
				seen[cb.ModuleID] = true
				cut.Boundaries = append(cut.Boundaries, cb)
			}
		}
	}
	return cut, nil
}

// layoutCutPart is what one request-time layout adds to the layout cut of
// every page under it.
type layoutCutPart struct {
	deps       []string
	actions    bool
	css        string
	cssModules map[string]*bundler.CSSModuleInfo
	boundaries []ClientBoundary
}

type layoutCutMemo struct {
	part *layoutCutPart
	err  error
}

// prefetchLayoutCuts starts preparing the part of every async layout above
// pages, each on a worker slot of its own: every page under a layout needs
// its part, so the pages would otherwise wait for it one after another.
func (b *Builder) prefetchLayoutCuts(pages []string, pool *workerPool) {
	seen := map[string]bool{}
	for _, page := range pages {
		if kind, ok := segmentKindOfFile(page); ok && kind == SegmentError {
			continue
		}
		cut, err := b.asyncLayoutCut(page)
		if err != nil || cut == nil {
			continue
		}
		for _, l := range cut.Layouts {
			if seen[l] {
				continue
			}
			seen[l] = true
			go func() {
				pool.acquire()
				defer pool.release()
				defer func() { _ = recover() }() // the page that needs it reports the failure
				_, _ = b.layoutCutPartFor(l)
			}()
		}
	}
}

// layoutCutPartFor returns the part of an async layout, computed once per
// build and shared by the pages under it. The pages waiting for it count as
// waiting for a round (see buildRounds), so the round the computing page
// waits on still starts.
func (b *Builder) layoutCutPartFor(layoutPath string) (*layoutCutPart, error) {
	j, owner := b.roundJobs.own("layout-cut", layoutPath)
	if owner {
		rounds := b.rounds
		func() {
			defer rounds.finish(j)
			part, err := b.layoutCutPart(layoutPath)
			j.output = &layoutCutMemo{part: part, err: err}
		}()
	} else {
		b.rounds.wait(j)
	}
	m, _ := j.output.(*layoutCutMemo)
	if m == nil {
		return nil, fmt.Errorf("layout %s could not be prepared", layoutPath)
	}
	return m.part, m.err
}

// layoutCutPart bundles one request-time layout: its CSS, its module graph,
// and the client boundaries and package boundaries of the components it
// renders.
func (b *Builder) layoutCutPart(layoutPath string) (*layoutCutPart, error) {
	bnd := b.newBundler()
	bundle, err := bnd.Bundle(layoutPath)
	if err != nil {
		return nil, err
	}
	part := &layoutCutPart{
		deps:       layoutDepPaths(layoutPath, bundle),
		actions:    bundleHasActions(bundle),
		css:        bundle.CSS,
		cssModules: bundle.CSSModules,
	}
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
	// The chunk checks stay queued: each page under the layout waits for
	// them with its own (see awaitBoundaries).
	boundaries, err := b.prepareClientBoundariesDeferred(bundle, ann, true)
	if err != nil {
		return nil, err
	}
	islands := b.detectReactIslandModules(bundle.Modules, bnd.Resolve)
	pkgs, err := b.serverTreeBoundariesDeferred(bundle, islands, bnd.Resolve, true)
	if err != nil {
		if aerr := b.awaitBoundaries(boundaries); aerr != nil {
			return nil, aerr
		}
		return nil, err
	}
	part.boundaries = append(boundaries, pkgs...)
	return part, nil
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
// (Head, Link, ...) the page gets. files keeps the layout sources for the
// run's other bundles.
func composedLayoutPlugin(root string, pageModule, pageSource, pageDir string, layouts []string, files *serverBundleFiles) api.Plugin {
	isLayout := map[string]bool{}
	for _, l := range layouts {
		isLayout[filepath.Clean(l)] = true
	}
	return api.Plugin{
		Name: "krate-composed-layouts",
		Setup: func(pb api.PluginBuild) {
			const ns = "krate-composed-page"
			// The filters name this page's module alone: a build of several
			// pages has one plugin, and one virtual module, per page.
			exact := "^" + specFilterEscape.ReplaceAllStringFunc(pageModule, func(s string) string { return `\` + s }) + "$"
			pb.OnResolve(api.OnResolveOptions{Filter: exact}, func(args api.OnResolveArgs) (api.OnResolveResult, error) {
				return api.OnResolveResult{Path: pageModule, Namespace: ns}, nil
			})
			pb.OnLoad(api.OnLoadOptions{Filter: exact, Namespace: ns}, func(api.OnLoadArgs) (api.OnLoadResult, error) {
				return api.OnLoadResult{Contents: &pageSource, Loader: api.LoaderTSX, ResolveDir: pageDir}, nil
			})
			pb.OnLoad(api.OnLoadOptions{Filter: `\.[cm]?[jt]sx?$`, Namespace: "file"}, func(args api.OnLoadArgs) (api.OnLoadResult, error) {
				if !isLayout[filepath.Clean(args.Path)] {
					return api.OnLoadResult{}, nil
				}
				data, err := files.serverSource(root, args.Path)
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
