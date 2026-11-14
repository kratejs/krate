package build

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/evanw/esbuild/pkg/api"
	"github.com/kratejs/krate/packages/compiler/internal/bundler"
	"github.com/kratejs/krate/packages/compiler/internal/pluginapi"
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
			if hoisted, herr := hoistInlineActions(root, absSource, pageSrc); herr != nil {
				fmt.Fprintf(os.Stderr, "  %sServer bundle error (%s):%s %v\n", cRed, page.SourcePath, cReset, herr)
				fail(page, herr)
				return
			} else if hoisted != nil {
				pageSrc = withInlineActionImport(hoisted.Source)
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
				composed = append(composed, composedLayoutPlugin(root, pageModule, injectedSource, filepath.Dir(absSource), page.ComposedLayouts))
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
				External: []string{
					"@krate/runtime",
					"@krate/runtime/*",
				},
				Plugins: serverBundlePlugins(page, absSource, append(composed, reactServerShimPlugin(), serverCSSPlugin(root), cbPlugin, unusedClientModulePlugin(), serverExportPlugin(page.ServerExports, absSource), goServerBundlePlugin(page.GoBaked, root), actionServerBundlePlugin(res, actionModules, actionBasePath), inlineActionPlugin(root, actionBasePath), serverRuntimeTagsPlugin(), serverGlobalsPlugin(root), externalNodeModulesPlugin())...),
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

// serverGlobalTags are the compiler-provided tags a server bundle gives every
// project module as globals, in the order they are imported.
var serverGlobalTags = []string{"Head", "Suspense", "Script", "Style", "Link", "Image", "Icon", "SyntaxHighlight"}

// injectServerGlobals prepends imports for the tags the source uses (Head,
// Suspense, Script, Style, Link, Image, Icon, SyntaxHighlight) so TSX modules
// can use them as globals (matching krate's compiler behavior). A name the
// module already declares or imports is left alone, which makes the injection
// safe to apply twice.
func injectServerGlobals(source string) string {
	var names []string
	for _, tag := range serverGlobalTags {
		if !strings.Contains(source, "<"+tag) && !strings.Contains(source, tag+">") {
			continue
		}
		if declaresBinding(source, tag) || importsBinding(source, tag) {
			continue
		}
		names = append(names, tag)
	}
	if len(names) == 0 {
		return source
	}

	importLine := fmt.Sprintf("import { %s } from '@krate/runtime/server';", strings.Join(names, ", "))
	return importLine + "\n" + source
}

var importClauseRe = regexp.MustCompile(`(?s)\bimport\s+(?:type\s+)?([^;'"]*?)\s*from\s*['"]`)

// importsBinding reports whether the module imports a binding named name
// (default, namespace or named; `X as name` binds name).
func importsBinding(src, name string) bool {
	for _, m := range importClauseRe.FindAllStringSubmatch(src, -1) {
		clause := m[1]
		if open := strings.Index(clause, "{"); open >= 0 {
			end := strings.Index(clause[open:], "}")
			if end < 0 {
				continue
			}
			for _, spec := range strings.Split(clause[open+1:open+end], ",") {
				fields := strings.Fields(spec)
				if len(fields) > 0 && fields[len(fields)-1] == name {
					return true
				}
			}
			clause = clause[:open]
		}
		for _, part := range strings.Split(clause, ",") {
			part = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(part), "* as "))
			if part == name {
				return true
			}
		}
	}
	return false
}

// serverGlobalsPlugin gives project modules that no other plugin claims the
// compiler-provided tags, so a component imported by a page can use <Head>,
// <Link> and the rest as globals the way the page itself does. It must come
// after the plugins that claim a module: those read it through
// readServerSource, which injects the same imports.
func serverGlobalsPlugin(root string) api.Plugin {
	return api.Plugin{
		Name: "krate-server-globals",
		Setup: func(pb api.PluginBuild) {
			pb.OnLoad(api.OnLoadOptions{Filter: `\.[jt]sx$`, Namespace: "file"}, func(args api.OnLoadArgs) (api.OnLoadResult, error) {
				if strings.Contains(filepath.ToSlash(args.Path), "/node_modules/") || !pluginapi.WithinRoot(root, args.Path) {
					return api.OnLoadResult{}, nil
				}
				data, err := os.ReadFile(args.Path)
				if err != nil {
					return api.OnLoadResult{}, nil
				}
				src := injectServerGlobals(string(data))
				if src == string(data) {
					return api.OnLoadResult{}, nil
				}
				return api.OnLoadResult{Contents: &src, Loader: loaderForPath(args.Path), ResolveDir: filepath.Dir(args.Path)}, nil
			})
		},
	}
}

// serverRuntimeTagsPlugin maps the tag names of `import { Head } from
// "@krate/runtime"` onto the server renderer. @krate/runtime stays external
// in a server bundle and its client entry only carries inert values for these
// names; the module this plugin serves re-exports the whole client entry and
// overrides the tags with @krate/runtime/server.
func serverRuntimeTagsPlugin() api.Plugin {
	const ns = "krate-runtime-tags"
	return api.Plugin{
		Name: "krate-runtime-tags",
		Setup: func(pb api.PluginBuild) {
			pb.OnResolve(api.OnResolveOptions{Filter: `^@krate/runtime$`}, func(args api.OnResolveArgs) (api.OnResolveResult, error) {
				if args.Namespace == ns {
					return api.OnResolveResult{}, nil
				}
				return api.OnResolveResult{Path: "@krate/runtime", Namespace: ns}, nil
			})
			pb.OnLoad(api.OnLoadOptions{Filter: `.*`, Namespace: ns}, func(api.OnLoadArgs) (api.OnLoadResult, error) {
				contents := "export * from '@krate/runtime';\n" +
					"export { " + strings.Join(serverGlobalTags, ", ") + " } from '@krate/runtime/server';\n"
				return api.OnLoadResult{Contents: &contents, Loader: api.LoaderJS}, nil
			})
		},
	}
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
				contents := "import { Suspense, Fragment, lazy, createSignal, createEffect, createMemo, onCleanup, onMount } from '@krate/runtime/server';\n" +
					"export { Suspense, Fragment, lazy, createSignal, createEffect, createMemo, onCleanup, onMount };\n" +
					"export default { Suspense, Fragment, lazy };\n"
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
