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
// it TierRuntime). An async PAGE renders entirely per request in the SSR
// sidecar (ssr, or isr with a revalidate window); an async child of a
// synchronous page becomes a request-time region rendered from the page server
// bundle, around a static shell.

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
// and reports whether the page component itself is async (a server page).
func (b *Builder) applyServerComponentMode(page string, entry *bundler.Module, ann *irtree.Annotations, staticMode bool, mode *RenderMode, revalidate *int) (bool, error) {
	entryFn := ann.Functions[ann.EntryPoint]
	serverPage := entryFn != nil && entryFn.Async
	children := serverComponentNames(ann)
	if !serverPage && len(children) == 0 {
		return false, nil
	}
	if staticMode {
		name, fn := ann.EntryPoint, entryFn
		if !serverPage {
			name, fn = children[0], ann.Functions[children[0]]
		}
		return false, serverDiag(ann, name, fn, entry.SourceCode,
			"async component "+name+" needs request-time rendering, but output is \"static\"",
			"remove `output: \"static\"` from the config, or make "+name+" synchronous").WithCode(diag.CodeServerComponentStaticOutput)
	}
	if serverPage {
		switch {
		case *mode == RenderISR:
		case pageRevalidateExport(entry.Program) > 0:
			*mode = RenderISR
			*revalidate = pageRevalidateExport(entry.Program)
		default:
			*mode = RenderSSR
			*revalidate = 0
		}
		printf("  %s⚡%s %s is an async server component -> %s\n", cCyan, cReset, filepath.Base(page), mode.String())
		return true, nil
	}
	if *mode == RenderSSG {
		*mode = RenderStreaming
		fmt.Fprintf(os.Stderr, "  %s⚡%s %s: [%s] renders server component %s per request -> streaming\n", cCyan, cReset, filepath.Base(page), diag.CodeServerComponentStreaming, children[0])
	}
	return false, nil
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

// asyncLayoutError rejects an async layout: layouts are baked into the static
// shell at build time, so they cannot await.
func asyncLayoutError(layoutPath string, ann *irtree.Annotations, raw string) error {
	fn := ann.Functions[ann.EntryPoint]
	if fn == nil || !fn.Async {
		return nil
	}
	d := diag.New(layoutPath, fn.Position.Line, fn.Position.Col,
		"layout "+ann.EntryPoint+" is an async function; layouts cannot await data",
		"keep the layout synchronous and move the data fetching into an async child component that a page renders",
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
// page bundle can re-export it even when the module keeps it private.
func serverExportPlugin(exports []ServerExport, pagePath string) api.Plugin {
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
					return api.OnLoadResult{}, nil
				}
				data, err := os.ReadFile(args.Path)
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
