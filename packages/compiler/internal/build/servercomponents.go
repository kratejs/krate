package build

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/evanw/esbuild/pkg/api"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/annotator"
	"github.com/kratejs/krate/packages/compiler/internal/bundler"
	"github.com/kratejs/krate/packages/compiler/internal/diag"
	irtree "github.com/kratejs/krate/packages/compiler/internal/irtree"
	"github.com/kratejs/krate/packages/compiler/internal/jsvalue"
)

// Async server components.
//
// An async function component is a server component (the annotator classifies
// it TierRuntime). An async PAGE with generateStaticParams (or under
// output: "static" without dynamic segments) is prerendered at build time
// through the staged SSR driver unless it opts into request-time rendering
// (dynamic = "force-dynamic", revalidate, or request-only APIs such as
// headers(), cookies(), or searchParams). Other async pages render per request
// in the SSR sidecar (ssr, or isr with a revalidate window); an async child of
// a synchronous page becomes a request-time region rendered from the page
// server bundle, around a static shell.

// serverComponentOpts captures the page context needed to decide whether an
// async page is prerendered at build time or rendered per request.
type serverComponentOpts struct {
	staticMode bool
	isDynRoute bool
	hasGSP     bool
	staticOnly bool
	isExpanded bool
	cfg        pageConfig
	dataUsage  dataCacheUsage
	cut        *layoutCut
}

// serverPagePlan describes how an async page will be built.
type serverPagePlan struct {
	serverPage        bool
	prerender         bool
	prerenderRequired bool
	prerenderSeedISR  bool
}

// pageRequestsDynamic reports whether a page explicitly opts into per-request
// rendering via segment exports or uncached (no-store) data reads.
func pageRequestsDynamic(cfg pageConfig, usage dataCacheUsage) (string, bool) {
	if cfg.ssr {
		return "export const config = { ssr: true }", true
	}
	if cfg.streaming {
		return "export const config = { streaming: true }", true
	}
	if cfg.dynamic == "force-dynamic" {
		return `export const dynamic = "force-dynamic"`, true
	}
	if cfg.hasRevalidateExport && !cfg.revalidateForever && cfg.revalidateExport == 0 && cfg.dynamic != "force-static" && cfg.dynamic != "error" {
		return "export const revalidate = 0", true
	}
	if usage.noStore && cfg.dynamic != "force-static" && cfg.dynamic != "error" && !cfg.revalidateForever {
		return "uncached (no-store) data read", true
	}
	return "", false
}

// serverComponentNames returns the async components (other than the entry)
// the page renders, sorted.
func serverComponentNames(ann *irtree.Annotations) []string {
	var names []string
	for name := range ann.UsedComponents {
		if name == ann.EntryPoint {
			continue
		}
		if fn := ann.Functions[name]; fn != nil && fn.Async {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

// applyServerComponentMode adjusts the page render mode for server components
// and returns the build plan for an async page (or a page under an async layout).
func (b *Builder) applyServerComponentMode(page string, entry *bundler.Module, ann *irtree.Annotations, opts serverComponentOpts, mode *RenderMode, revalidate *int) (serverPagePlan, error) {
	entryFn := ann.Functions[ann.EntryPoint]
	cut := opts.cut
	serverPage := (entryFn != nil && entryFn.Async) || (cut != nil && !cut.Dropped)
	children := serverComponentNames(ann)
	if !serverPage && len(children) == 0 {
		return serverPagePlan{}, nil
	}
	dynReason, wantsDynamic := pageRequestsDynamic(opts.cfg, opts.dataUsage)
	staticPinned := opts.cfg.dynamic == "force-static" || opts.cfg.dynamic == "error" || opts.cfg.revalidateForever
	wantsISR := *mode == RenderISR || opts.cfg.isr ||
		(opts.cfg.hasRevalidateExport && !opts.cfg.revalidateForever && opts.cfg.revalidateExport > 0) ||
		(!staticPinned && (opts.dataUsage.minRevalidate > 0 || opts.dataUsage.tagged))

	if opts.staticMode {
		name, fn := ann.EntryPoint, entryFn
		if !serverPage {
			name, fn = children[0], ann.Functions[children[0]]
			return serverPagePlan{}, serverDiag(ann, name, fn, entry.SourceCode,
				"async component "+name+" needs request-time rendering, but output is \"static\"",
				"remove `output: \"static\"` from the config, make "+name+" synchronous, or make the page `async` so it can be prerendered at build time").WithCode(diag.CodeServerComponentStaticOutput)
		}
		if wantsDynamic {
			return serverPagePlan{}, serverDiag(ann, name, fn, entry.SourceCode,
				"async page "+name+" requests per-request rendering ("+dynReason+"), but output is \"static\"",
				"remove `output: \"static\"` from the config, or remove "+dynReason+" so the page can be prerendered at build time").WithCode(diag.CodeServerComponentStaticOutput)
		}
		if opts.isDynRoute && !opts.isExpanded {
			if !opts.hasGSP {
				return serverPagePlan{}, serverDiag(ann, name, fn, entry.SourceCode,
					"async dynamic page "+name+" needs generateStaticParams to prerender with output: \"static\"",
					"export `generateStaticParams` to list the routes to prerender, remove `output: \"static\"`, or make "+name+" synchronous").WithCode(diag.CodeServerComponentStaticOutput)
			}
			if !opts.staticOnly {
				return serverPagePlan{}, serverDiag(ann, name, fn, entry.SourceCode,
					"async dynamic page "+name+" allows on-demand dynamicParams, but output is \"static\"",
					"remove `dynamicParams = true` so only generateStaticParams routes are prerendered, or remove `output: \"static\"` from the config").WithCode(diag.CodeServerComponentStaticOutput)
			}
			// Template is dropped after generateStaticParams expansion.
			*mode = RenderSSR
			*revalidate = 0
			return serverPagePlan{serverPage: true}, nil
		}
		*mode = RenderSSR
		*revalidate = 0
		return serverPagePlan{serverPage: true, prerender: true, prerenderRequired: true}, nil
	}

	if serverPage {
		if !wantsDynamic && wantsISR {
			*mode = RenderISR
			if *revalidate <= 0 {
				switch {
				case opts.cfg.isr && opts.cfg.revalidate > 0:
					*revalidate = opts.cfg.revalidate
				case opts.cfg.hasRevalidateExport && opts.cfg.revalidateExport > 0:
					*revalidate = opts.cfg.revalidateExport
				case pageRevalidateExport(entry.Program) > 0:
					*revalidate = pageRevalidateExport(entry.Program)
				case opts.dataUsage.minRevalidate > 0:
					*revalidate = opts.dataUsage.minRevalidate
				case opts.dataUsage.tagged:
					*revalidate = taggedOnlyISRRevalidate
				default:
					*revalidate = defaultISRRevalidate
				}
			}
			plan := serverPagePlan{serverPage: true}
			if opts.isExpanded {
				plan.prerender = true
				plan.prerenderSeedISR = true
			}
			if cut != nil && !cut.Dropped && !(entryFn != nil && entryFn.Async) {
				printf("  %s*%s %s renders inside the async layout %s -> %s\n", cCyan, cReset, filepath.Base(page), filepath.Base(cut.Layouts[len(cut.Layouts)-1]), mode.String())
			} else {
				printf("  %s*%s %s is an async server component -> %s\n", cCyan, cReset, filepath.Base(page), mode.String())
			}
			return plan, nil
		}

		*mode = RenderSSR
		*revalidate = 0
		plan := serverPagePlan{serverPage: true}
		if !wantsDynamic && (opts.isExpanded || (!opts.isDynRoute && staticPinned)) {
			plan.prerender = true
		}
		if !plan.prerender {
			if cut != nil && !cut.Dropped && !(entryFn != nil && entryFn.Async) {
				printf("  %s*%s %s renders inside the async layout %s -> %s\n", cCyan, cReset, filepath.Base(page), filepath.Base(cut.Layouts[len(cut.Layouts)-1]), mode.String())
			} else {
				printf("  %s*%s %s is an async server component -> %s\n", cCyan, cReset, filepath.Base(page), mode.String())
			}
		}
		return plan, nil
	}
	if *mode == RenderSSG {
		*mode = RenderStreaming
		fmt.Fprintf(b.stderr(), "  %s*%s %s: [%s] renders server component %s per request -> streaming\n", cCyan, cReset, filepath.Base(page), diag.CodeServerComponentStreaming, children[0])
	}
	return serverPagePlan{}, nil
}

// pageRevalidateExport reads `export const revalidate = N` (Next.js route
// segment config) from a page. 0 when absent or not a positive literal.
func pageRevalidateExport(prog *ast.Program) int {
	if prog == nil {
		return 0
	}
	for _, stmt := range prog.Body {
		exp, ok := stmt.(*ast.ExportStmt)
		if !ok || exp.Default {
			continue
		}
		vs, ok := exp.Declaration.(*ast.VarStmt)
		if !ok {
			continue
		}
		for _, d := range vs.Decls {
			if d.Name != "revalidate" {
				continue
			}
			if lit, ok := d.Init.(*ast.Literal); ok && lit.Kind == ast.NumberLit {
				if n, err := strconv.Atoi(lit.Value); err == nil && n > 0 {
					return n
				}
			}
		}
	}
	return 0
}

// asyncLayoutError rejects an async layout that reaches the build-time layout
// pipeline. Pages render async layouts per request (see asyncLayoutCut); this
// is a route (a segment or intercepting page) whose layouts are baked.
func asyncLayoutError(layoutPath string, ann *irtree.Annotations, raw string) error {
	fn := ann.Functions[ann.EntryPoint]
	if fn == nil || !fn.Async {
		return nil
	}
	d := diag.New(layoutPath, fn.Position.Line, fn.Position.Col,
		"layout "+ann.EntryPoint+" is an async function, but the route it wraps is rendered at build time, where it cannot await data",
		"keep the layout synchronous and move the data fetching into an async child component, or use the layout only above pages that render per request",
		lineOf(raw, fn.Position.Line))
	return d.WithCode(diag.CodeAsyncLayout)
}

func serverDiag(ann *irtree.Annotations, name string, fn *ast.FnDecl, fallbackRaw, msg, hint string) diag.Diagnostic {
	file := ann.ComponentSources[name]
	if file == "" {
		file = ann.SourceFile
	}
	raw := ann.ComponentRaw[name]
	if raw == "" {
		raw = fallbackRaw
	}
	line, col := 0, 0
	if fn != nil {
		line, col = fn.Position.Line, fn.Position.Col
	}
	return diag.New(file, line, col, msg, hint, lineOf(raw, line))
}

// lineOf returns the 1-based line of src, or "".
func lineOf(src string, line int) string {
	if line <= 0 {
		return ""
	}
	start := 0
	for i := 1; i < line; i++ {
		j := indexByteFrom(src, '\n', start)
		if j < 0 {
			return ""
		}
		start = j + 1
	}
	end := indexByteFrom(src, '\n', start)
	if end < 0 {
		end = len(src)
	}
	out := src[start:end]
	if n := len(out); n > 0 && out[n-1] == '\r' {
		out = out[:n-1]
	}
	return out
}

func indexByteFrom(s string, c byte, from int) int {
	for i := from; i < len(s); i++ {
		if s[i] == c {
			return i
		}
	}
	return -1
}

// declaresBinding reports whether src declares name at the top level as a
// function, class or variable (exported or not).
func declaresBinding(src, name string) bool {
	re, err := regexp.Compile(`(?m)^[ \t]*(?:export[ \t]+)?(?:default[ \t]+)?(?:async[ \t]+)?(?:function[ \t]*\*?[ \t]*|const[ \t]+|let[ \t]+|var[ \t]+|class[ \t]+)` + regexp.QuoteMeta(name) + `\b`)
	if err != nil {
		return false
	}
	return re.MatchString(src)
}

// serverExportEntrySuffix returns the statements appended to a page bundle's
// entry so it exports every server component its regions render. Components
// declared in the page file are exported directly; components from other
// modules are re-exported from the alias serverExportPlugin adds there (or
// from the module's default export when it has no named binding).
func serverExportEntrySuffix(exports []ServerExport, pagePath string) string {
	if len(exports) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n")
	for _, e := range exports {
		if samePath(e.SourcePath, pagePath) {
			fmt.Fprintf(&b, "export { %s as %s };\n", e.Name, e.Key)
			continue
		}
		src, err := os.ReadFile(e.SourcePath)
		if err == nil && declaresBinding(string(src), e.Name) {
			fmt.Fprintf(&b, "export { %s } from %s;\n", e.Key, strconv.Quote(e.SourcePath))
		} else {
			fmt.Fprintf(&b, "export { default as %s } from %s;\n", e.Key, strconv.Quote(e.SourcePath))
		}
	}
	return b.String()
}

// serverExportPlugin appends `export { Name as Key }` to each module (other
// than the page) that declares a server component a region renders, so the
// page bundle can re-export it even when the module keeps it private. files
// keeps the file identities and sources for the run's other bundles.
func serverExportPlugin(exports []ServerExport, pagePath string, files *serverBundleFiles) api.Plugin {
	byFile := map[string][]ServerExport{}
	for _, e := range exports {
		if samePath(e.SourcePath, pagePath) {
			continue
		}
		key := filepath.Clean(e.SourcePath)
		byFile[key] = append(byFile[key], e)
		if resolved := canonicalPath(e.SourcePath); resolved != key {
			byFile[resolved] = append(byFile[resolved], e)
		}
	}
	return api.Plugin{
		Name: "krate-server-exports",
		Setup: func(pb api.PluginBuild) {
			if len(byFile) == 0 {
				return
			}
			pb.OnLoad(api.OnLoadOptions{Filter: `\.[cm]?[jt]sx?$`, Namespace: "file"}, func(args api.OnLoadArgs) (api.OnLoadResult, error) {
				list, ok := byFile[filepath.Clean(args.Path)]
				if !ok {
					for _, e := range exports {
						if files.sameFile(args.Path, e.SourcePath) {
							list = append(list, e)
						}
					}
					ok = len(list) > 0
				}
				if !ok {
					return api.OnLoadResult{}, nil
				}
				data, err := files.serverSource(pb.InitialOptions.AbsWorkingDir, args.Path)
				if err != nil {
					return api.OnLoadResult{}, err
				}
				// This plugin runs before the Go-component plugin and claims the
				// module, so apply its call-site annotation here too.
				src := annotateGoModuleSource(args.Path, injectServerGlobals(string(data)))
				for _, e := range list {
					if declaresBinding(src, e.Name) {
						src += fmt.Sprintf("\nexport { %s as %s };\n", e.Name, e.Key)
					}
				}
				return api.OnLoadResult{Contents: &src, Loader: loaderForPath(args.Path), ResolveDir: filepath.Dir(args.Path)}, nil
			})
		},
	}
}

func loaderForPath(p string) api.Loader {
	switch strings.ToLower(filepath.Ext(p)) {
	case ".tsx":
		return api.LoaderTSX
	case ".ts", ".mts", ".cts":
		return api.LoaderTS
	case ".jsx":
		return api.LoaderJSX
	default:
		return api.LoaderJS
	}
}

// reportMissingServerExports prints an error for each server component the
// compiled page bundle failed to export (esbuild drops unresolved TypeScript
// re-exports silently), so the gap is visible at build time and not only as a
// 500 from the sidecar at request time.
func reportMissingServerExports(page *PageResult, bundlePath string) error {
	if len(page.ServerExports) == 0 {
		return nil
	}
	data, err := os.ReadFile(bundlePath)
	if err != nil {
		return err
	}
	for _, e := range page.ServerExports {
		if !strings.Contains(string(data), e.Key) {
			fmt.Fprintf(os.Stderr, "  %sServer bundle error (%s):%s server component %s from %s could not be exported; declare it as a top-level function or const\n", cRed, page.SourcePath, cReset, e.Name, e.SourcePath)
			return fmt.Errorf("server component %s from %s could not be exported from the page bundle", e.Name, e.SourcePath)
		}
	}
	return nil
}

// topLevelAwaitError reports a module-level `await` in a page whose body is
// evaluated at build time: the evaluator cannot wait for the Promise, so the
// value would otherwise render empty.
func topLevelAwaitError(entry *bundler.Module, extra []annotator.ModuleSource) error {
	mods := append([]annotator.ModuleSource{{Program: entry.Program, Path: entry.Path, RawSource: entry.SourceCode}}, extra...)
	for _, m := range mods {
		aw := irtree.TopLevelAwait(m.Program)
		if aw == nil {
			continue
		}
		return diag.New(m.Path, aw.Position.Line, aw.Position.Col,
			jsvalue.AwaitReason+" (module-level await in a page rendered at build time)",
			"move the await into an async component (a server component) so it runs per request, or make the page component async",
			lineOf(m.RawSource, aw.Position.Line)).WithCode(diag.CodeBuildTimeAwait)
	}
	return nil
}
