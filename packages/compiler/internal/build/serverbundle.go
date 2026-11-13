package build

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/evanw/esbuild/pkg/api"
	"github.com/kratejs/krate/packages/compiler/internal/bundler"
	"github.com/kratejs/krate/packages/compiler/internal/safe"
)

// CompileServerBundles compiles SSR/ISR/streaming pages into self-contained ESM bundles
// that the Node.js renderer can import. Each bundle is compiled with:
//   - JSX automatic transform using @krate/runtime/jsx-runtime
//   - Head, Suspense, Script, Style imported from @krate/runtime/server
//   - All dependencies bundled (except node_modules externals)
func CompileServerBundles(res *bundler.ImportResolver, results []*PageResult, root, outDir string, sourcemap bool, actionModules map[string]map[string]string, actionBasePath string) map[string]string {
	bundles, _ := compileServerBundles(res, results, root, outDir, sourcemap, actionModules, actionBasePath)
	return bundles
}

// compileServerBundles is CompileServerBundles that also returns the bundle
// failures of pages that render server components. Those pages have no baked
// body to fall back to, so a missing bundle must fail the build.
func compileServerBundles(res *bundler.ImportResolver, results []*PageResult, root, outDir string, sourcemap bool, actionModules map[string]map[string]string, actionBasePath string) (map[string]string, []error) {
	// Collect non-SSG pages that actually need the Node renderer. A streaming
	// page whose regions are all Go (or that has no dynamic regions at all) is
	// rendered by the Go server + Go host alone, so its page bundle is never
	// loaded - skip compiling it.
	var ssrPages []*PageResult
	for _, r := range results {
		if r.Mode != RenderSSG && r.SourcePath != "" && pageNeedsNodeServerBundle(r) {
			ssrPages = append(ssrPages, r)
		}
	}
	if len(ssrPages) == 0 {
		return nil, nil
	}

	bundleDir := filepath.Join(outDir, ".krate", "server-bundles")
	_ = os.MkdirAll(bundleDir, 0755)

	bundles := make(map[string]string, len(ssrPages))
	impls := &clientSSRImpls{res: res, root: root, bundleDir: bundleDir, actions: actionModules, basePath: actionBasePath}
	var fatal []error
	var mu sync.Mutex
	fail := func(page *PageResult, err error) {
		if !page.ServerPage && len(page.ServerExports) == 0 && len(page.ComposedLayouts) == 0 {
			return
		}
		mu.Lock()
		fatal = append(fatal, fmt.Errorf("%s: server bundle: %w", page.SourcePath, err))
		mu.Unlock()
	}
	var wg sync.WaitGroup
	pool := newWorkerPool(buildWorkerLimit())

	for _, page := range ssrPages {
		page := page
		wg.Add(1)
		go func() {
			defer wg.Done()
			pool.acquire()
			defer pool.release()
			defer func() {
				if r := recover(); r != nil {
					_ = safe.Recover(r) // prints the stack; this page is skipped
				}
			}()

			absSource := filepath.Join(root, page.SourcePath)
			sourceData, err := os.ReadFile(absSource)
			if err != nil {
				fmt.Fprintf(os.Stderr, "  %sWarning: cannot read %s for server bundle:%s %v\n", cYellow, page.SourcePath, cReset, err)
				return
			}

			// Inject Head/Suspense/Script/Style imports at the top of the file
			// so pages can use them as globals (matching krate compiler behavior)
			pageSrc := string(sourceData)
			if page.IslandStubs != nil {
				pageSrc = page.IslandStubs.applyAttrs(pageSrc)
			}
			// Annotate Go-component call sites with __krateGoId so the shim can
			// inline a server-tier component's build-time HTML (page.GoBaked).
			if locals := goComponentLocals(absSource, pageSrc, root); len(locals) > 0 {
				pageSrc = annotateGoCalls(pageSrc, absSource, locals)
			}
			injectedSource := injectServerGlobals(pageSrc) + serverExportEntrySuffix(page.ServerExports, absSource) + metadataEntrySuffix(page.MetadataSegments, page.MetadataPageFiles, root)

			// Compile with esbuild - use stdin so esbuild resolves relative imports
			// from the project root (AbsWorkingDir) rather than a temp file location
			route := page.OutName
			if route == "." || route == "" {
				route = "index"
			}
			outName := strings.TrimPrefix(route, "/") + ".server.mjs"
			outPath := filepath.Join(bundleDir, outName)

			cbPlugin, cbErr := clientBoundaryPlugin(page.ClientBoundaries, impls, outPath)
			if cbErr != nil {
				fmt.Fprintf(os.Stderr, "  %sServer bundle error (%s):%s %v\n", cRed, page.SourcePath, cReset, cbErr)
				fail(page, cbErr)
				return
			}

			stdinContents, stdinDir, stdinFile := injectedSource, filepath.Dir(absSource), filepath.Base(absSource)
			var composed []api.Plugin
			if len(page.ComposedLayouts) > 0 {
				const pageModule = "krate-composed-page"
				stdinContents = composedEntrySource(pageModule, page.ComposedLayouts)
				composed = append(composed, composedLayoutPlugin(pageModule, injectedSource, filepath.Dir(absSource), page.ComposedLayouts))
			}

			result := esbuildBuild(api.BuildOptions{
				AbsWorkingDir:   root,
				Bundle:          true,
				Format:          api.FormatESModule,
				Platform:        api.PlatformNode,
				Outfile:         outPath,
				Write:           true,
				Sourcemap:       sourceMapMode(sourcemap),
				JSX:             api.JSXAutomatic,
				JSXSideEffects:  false,
				JSXImportSource: "@krate/runtime/server",
				TsconfigRaw:     `{ "compilerOptions": { "jsx": "react-jsx", "jsxImportSource": "@krate/runtime/server" } }`,
				Banner:          basePathBanner(actionBasePath),
				Packages:        api.PackagesExternal,
				External: []string{
					"@krate/runtime",
					"@krate/runtime/*",
				},
				Plugins: serverBundlePlugins(page, absSource, append(composed, reactServerShimPlugin(), serverCSSPlugin(root), cbPlugin, unusedClientModulePlugin(), serverExportPlugin(page.ServerExports, absSource), goServerBundlePlugin(page.GoBaked, root), actionServerBundlePlugin(res, actionModules, actionBasePath))...),
				Stdin: &api.StdinOptions{
					Loader:     api.LoaderTSX,
					Contents:   stdinContents,
					ResolveDir: stdinDir,
					Sourcefile: stdinFile,
				},
			})

			if len(result.Errors) > 0 {
				for _, e := range result.Errors {
					fmt.Fprintf(os.Stderr, "  %sServer bundle error (%s):%s %s\n", cYellow, page.SourcePath, cReset, e.Text)
				}
				fail(page, fmt.Errorf("%s", result.Errors[0].Text))
				return
			}

			if err := reportMissingServerExports(page, outPath); err != nil {
				fail(page, err)
			}

			relBundle, _ := filepath.Rel(outDir, outPath)
			mu.Lock()
			bundles[page.SourcePath] = relBundle
			mu.Unlock()
		}()
	}

	wg.Wait()

	return bundles, fatal
}

// pageNeedsNodeServerBundle reports whether a non-SSG page requires a compiled
// Node server bundle: SSR/ISR pages (coarse page region) or any page with a
// non-Go dynamic region. Streaming pages whose regions are all Go (or that have
// no dynamic regions) are served without the Node renderer.
func pageNeedsNodeServerBundle(r *PageResult) bool {
	if r.Mode == RenderSSR || r.Mode == RenderISR {
		return true
	}
	for _, reg := range r.Regions {
		if reg.Runtime != "go" {
			return true
		}
	}
	return false
}

// injectServerGlobals prepends imports for Head, Suspense, Script, Style, Link, Image, Icon
// so TSX pages can use them as globals (matching krate's compiler behavior).
func injectServerGlobals(source string) string {
	needsHead := strings.Contains(source, "<Head") || strings.Contains(source, "Head>")
	needsSuspense := strings.Contains(source, "<Suspense") || strings.Contains(source, "Suspense>")
	needsScript := strings.Contains(source, "<Script") || strings.Contains(source, "Script>")
	needsStyle := strings.Contains(source, "<Style") || strings.Contains(source, "Style>")
	needsLink := strings.Contains(source, "<Link") || strings.Contains(source, "Link>")
	needsImage := strings.Contains(source, "<Image") || strings.Contains(source, "Image>")
	needsIcon := strings.Contains(source, "<Icon") || strings.Contains(source, "Icon>")
	needsSyntaxHighlight := strings.Contains(source, "<SyntaxHighlight") || strings.Contains(source, "SyntaxHighlight>")

	var names []string
	if needsHead {
		names = append(names, "Head")
	}
	if needsSuspense {
		names = append(names, "Suspense")
	}
	if needsScript {
		names = append(names, "Script")
	}
	if needsStyle {
		names = append(names, "Style")
	}
	if needsLink {
		names = append(names, "Link")
	}
	if needsImage {
		names = append(names, "Image")
	}
	if needsIcon {
		names = append(names, "Icon")
	}
	if needsSyntaxHighlight {
		names = append(names, "SyntaxHighlight")
	}

	if len(names) == 0 {
		return source
	}

	importLine := fmt.Sprintf("import { %s } from '@krate/runtime/server';", strings.Join(names, ", "))
	return importLine + "\n" + source
}

// reactServerShimPlugin maps `import { Suspense } from "react"` in a server
// bundle onto Krate's server Suspense. Without it the bare import stays
// external and fails at load time (ERR_MODULE_NOT_FOUND) unless react happens
// to be installed. Only the names Krate's server renderer implements are
// exported, so any other React import fails the build with esbuild's "no
// matching export" error instead of resolving to something that silently
// behaves differently.
func reactServerShimPlugin() api.Plugin {
	return api.Plugin{
		Name: "krate-react-server-shim",
		Setup: func(b api.PluginBuild) {
			b.OnResolve(api.OnResolveOptions{Filter: `^react$`}, func(args api.OnResolveArgs) (api.OnResolveResult, error) {
				return api.OnResolveResult{Path: "react", Namespace: "krate-react-shim"}, nil
			})
			b.OnLoad(api.OnLoadOptions{Filter: `.*`, Namespace: "krate-react-shim"}, func(args api.OnLoadArgs) (api.OnLoadResult, error) {
				contents := "import { Suspense, Fragment, createSignal, createEffect, createMemo, onCleanup, onMount } from '@krate/runtime/server';\n" +
					"export { Suspense, Fragment, createSignal, createEffect, createMemo, onCleanup, onMount };\n" +
					"export default { Suspense, Fragment };\n"
				return api.OnLoadResult{Contents: &contents, Loader: api.LoaderJS, ResolveDir: ""}, nil
			})
		},
	}
}

// serverBundlePlugins adds the island stubs of the page, first, so they see
// the page's imports before any other plugin.
func serverBundlePlugins(page *PageResult, absSource string, plugins ...api.Plugin) []api.Plugin {
	if page.IslandStubs == nil {
		return plugins
	}
	return append([]api.Plugin{islandServerStubPlugin(page.IslandStubs, absSource)}, plugins...)
}
