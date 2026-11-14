package build

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/evanw/esbuild/pkg/api"
	"github.com/kratejs/krate/packages/compiler/internal/bundler"
	"github.com/kratejs/krate/packages/compiler/internal/diag"
	"github.com/kratejs/krate/packages/compiler/internal/environ"
	"github.com/kratejs/krate/packages/compiler/internal/execx"
	"github.com/kratejs/krate/packages/compiler/internal/fsutil"
	"github.com/kratejs/krate/packages/compiler/internal/jseval"
	"github.com/kratejs/krate/packages/compiler/internal/pluginapi"
)

// Build-time metadata.
//
// A static page's `metadata` and `generateMetadata` (and those of the layouts
// above it) run once at build time, once per generateStaticParams entry, and
// the tags land in the page's <head>. The chain is evaluated with the same
// runtime code that serves a request (runtime/src/metadata.ts), first in the
// embedded JavaScript engine and, when that cannot run the module (fetch, Node
// built-ins), in Node. Pages that render per request resolve it per request
// instead (the server bundle carries the chain).

// metadataResult is the outcome for one set of route params.
type metadataResult struct {
	// Tags is the head markup, one tag per line.
	Tags string `json:"tags"`
	// Errors are the messages of generateMetadata functions that threw.
	Errors []string `json:"errors"`
	// ErrorFiles names, per error, the project-relative file of the segment
	// whose generateMetadata threw ("" when it is unknown).
	ErrorFiles []string `json:"errorFiles"`
}

type metadataKey struct {
	page   string
	params string
}

var metadataCache sync.Map // metadataKey -> metadataResult

func paramsKey(params map[string]string) string {
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var sb strings.Builder
	for _, k := range keys {
		sb.WriteString(k + "=" + params[k] + "\x00")
	}
	return sb.String()
}

// metadataEntrySource builds the module that resolves the chain for each
// param set and assigns the results to the eval global (or prints them).
//
// imports maps a module to the path to import in its place (a slice of it, see
// sliceModule); labels stay those of the original files.
func metadataEntrySource(page string, segs []metaSegment, pageFiles, root string, sets []map[string]string, imports map[string]string, print bool) string {
	var b strings.Builder
	imp := func(p string) string {
		if v, ok := imports[p]; ok {
			return filepath.ToSlash(v)
		}
		return filepath.ToSlash(p)
	}
	b.WriteString("import { metadataHeadTags } from \"@krate/runtime/server\";\n")
	fmt.Fprintf(&b, "import * as __page from %s;\n", strconv.Quote(imp(page)))
	var chain []string
	for i, sg := range segs {
		var fields []string
		if sg.Path != "" {
			fmt.Fprintf(&b, "import * as __l%d from %s;\n", i, strconv.Quote(imp(sg.Path)))
			fields = append(fields, fmt.Sprintf("file: %s", strconv.Quote(relTo(root, sg.Path))), fmt.Sprintf("mod: __l%d", i))
		}
		if sg.Files != "" {
			fields = append(fields, "files: "+sg.Files)
		}
		chain = append(chain, "{ "+strings.Join(fields, ", ")+" }")
	}
	pageSeg := fmt.Sprintf("{ file: %s, mod: __page", strconv.Quote(relTo(root, page)))
	if pageFiles != "" {
		pageSeg += ", files: " + pageFiles
	}
	chain = append(chain, pageSeg+" }")
	setsJSON, _ := json.Marshal(sets)
	fmt.Fprintf(&b, "const __sets = %s;\nconst __chain = [%s];\n", setsJSON, strings.Join(chain, ", "))
	// The async wrapper keeps top-level await out of the bundle: a dependency
	// that reads __dirname next to one is rejected by Node and Bun alike.
	b.WriteString(`const __results = [];
const __run = async () => {
  for (const params of __sets) {
    const errors = [];
    const errorFiles = [];
    let tags = "";
    try {
      tags = await metadataHeadTags(__chain, { params, searchParams: {} }, (msg, err) => {
        errors.push(String((err && err.message) || err));
        const m = /^generateMetadata in (.*) failed;/.exec(msg);
        errorFiles.push(m ? m[1] : "");
      });
    } catch (err) {
      errors.push(String((err && err.message) || err));
      errorFiles.push("");
    }
    __results.push({ tags, errors, errorFiles });
  }
`)
	if print {
		b.WriteString("  console.log(JSON.stringify(__results));\n")
		b.WriteString("};\n__run().catch((err) => { console.error(err); process.exit(1); });\n")
	} else {
		fmt.Fprintf(&b, "  globalThis.%s = __results;\n", jseval.ResultGlobal)
		fmt.Fprintf(&b, "};\n__run().catch((err) => { globalThis.%s = String((err && err.message) || err); });\n", jseval.ErrorGlobal)
	}
	return b.String()
}

func relTo(root, path string) string {
	if r, err := filepath.Rel(root, path); err == nil {
		return filepath.ToSlash(r)
	}
	return filepath.ToSlash(path)
}

// metadataSliceExports are the exports metadata evaluation reads from a
// segment module.
var metadataSliceExports = []string{"metadata", "viewport", "generateMetadata"}

// metadataSlices cuts the page module and every layout of its chain down to
// the metadata exports and their dependencies. It returns the import to use
// per module and the in-memory files behind them; a module that cannot be cut
// safely is left out and imported whole.
func metadataSlices(page string, segs []metaSegment) (imports, virtual map[string]string) {
	imports, virtual = map[string]string{}, map[string]string{}
	paths := []string{page}
	for _, sg := range segs {
		if sg.Path != "" {
			paths = append(paths, sg.Path)
		}
	}
	for _, p := range paths {
		if sl, ok := sliceModule(p, metadataSliceExports); ok {
			imports[p] = sl.Path
			virtual[sl.Path] = sl.Source
		}
	}
	return imports, virtual
}

// serverGlobalsTransform gives project components the compiler-provided tags
// (Link, Head, ...) as globals, as they have in a page.
func (b *Builder) serverGlobalsTransform(path, src string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".tsx", ".jsx":
	default:
		return src
	}
	if strings.Contains(filepath.ToSlash(path), "/node_modules/") || !pluginapi.WithinRoot(b.Root, path) {
		return src
	}
	return injectServerGlobals(src)
}

// evalMetadata resolves the chain of page for every param set. It evaluates
// the metadata exports and what they depend on; when that fails (or the
// closure cannot be cut), the whole modules run instead.
func (b *Builder) evalMetadata(page string, segs []metaSegment, pageFiles string, sets []map[string]string, bnd *bundler.Bundler) ([]metadataResult, error) {
	abs, err := filepath.Abs(page)
	if err != nil {
		return nil, err
	}
	imports, virtual := metadataSlices(abs, segs)
	out, err := b.evalMetadataModules(abs, segs, pageFiles, sets, bnd, imports, virtual)
	if len(virtual) > 0 && (err != nil || anyErrors(out)) {
		if whole, werr := b.evalMetadataModules(abs, segs, pageFiles, sets, bnd, nil, nil); werr == nil && !anyErrors(whole) {
			return whole, nil
		}
	}
	return out, err
}

func (b *Builder) evalMetadataModules(abs string, segs []metaSegment, pageFiles string, sets []map[string]string, bnd *bundler.Bundler, imports, virtual map[string]string) ([]metadataResult, error) {
	resolve := func(importer, spec string) (string, bool) {
		if bnd != nil {
			if p := bnd.Resolve(importer, spec); p != "" {
				return p, true
			}
		}
		return "", false
	}
	parse := func(raw []byte) ([]metadataResult, error) {
		var out []metadataResult
		if err := json.Unmarshal(raw, &out); err != nil {
			return nil, fmt.Errorf("parsing metadata output: %w", err)
		}
		if len(out) != len(sets) {
			return nil, fmt.Errorf("metadata output has %d results for %d param sets", len(out), len(sets))
		}
		return out, nil
	}

	raw, eerr := jseval.Eval(jseval.Options{
		Source:        metadataEntrySource(abs, segs, pageFiles, b.Root, sets, imports, false),
		ResolveDir:    filepath.Dir(abs),
		Sourcefile:    "krate-metadata-entry.ts",
		Loader:        api.LoaderTSX,
		Env:           b.scriptEnv(),
		Await:         true,
		ResolveImport: resolve,

		// Module-level JSX in the page chain compiles without React in scope.
		JSXImportSource: "@krate/runtime/server",
		VirtualFiles:    virtual,
		SourceTransform: b.serverGlobalsTransform,
	})
	if eerr == nil {
		if out, perr := parse(raw); perr == nil {
			// A module the embedded engine cannot fully run (no fetch, no Node
			// built-ins) reports it as a generateMetadata error: run it in Node.
			if !anyErrors(out) {
				return out, nil
			}
		}
	}

	output, err := b.runBootstrapBundle(abs, metadataEntrySource(abs, segs, pageFiles, b.Root, sets, imports, true), "metadata", bnd, virtual)
	if err != nil {
		if eerr != nil {
			return nil, fmt.Errorf("%v (embedded engine: %v)", err, eerr)
		}
		return nil, err
	}
	return parse(output)
}

func anyErrors(rs []metadataResult) bool {
	for _, r := range rs {
		if len(r.Errors) > 0 {
			return true
		}
	}
	return false
}

// runBootstrapBundle bundles source (resolving imports like the page does) to
// ESM and runs it with the configured runtime, returning its stdout.
//
// virtual holds in-memory modules the source imports (see jseval.Options).
func (b *Builder) runBootstrapBundle(abs, source, prefix string, bnd *bundler.Bundler, virtual map[string]string) ([]byte, error) {
	jsxSource := ""
	if prefix == "imageroute" || prefix == "metaroute" || prefix == "metadata" {
		// opengraph-image.tsx and pages with module-level JSX build their
		// element trees with the server JSX runtime, not React.
		jsxSource = "@krate/runtime/server"
	}
	resolve := func(importer, spec string) (string, bool) {
		if bnd != nil {
			if p := bnd.Resolve(importer, spec); p != "" {
				return p, true
			}
		}
		return "", false
	}
	js, err := b.bundleExternalizingNative(jseval.Options{
		Source:        source,
		ResolveDir:    filepath.Dir(abs),
		Sourcefile:    "krate-" + prefix + "-bootstrap.tsx",
		Loader:        api.LoaderTSX,
		ResolveImport: resolve,

		JSXImportSource: jsxSource,
		VirtualFiles:    virtual,
		SourceTransform: b.serverGlobalsTransform,
	})
	if err != nil {
		return nil, fmt.Errorf("bundling %s bootstrap: %w", prefix, err)
	}
	genDir := filepath.Join(b.Root, ".krate", "gen")
	if err := os.MkdirAll(genDir, 0755); err != nil {
		return nil, fmt.Errorf("creating bootstrap directory: %w", err)
	}
	path := filepath.Join(genDir, prefix+"-"+hashContent([]byte(js))+".mjs")
	if err := fsutil.WriteFileIfChanged(path, []byte(js), 0644); err != nil {
		return nil, fmt.Errorf("writing bootstrap: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmdName, args := "node", []string{path}
	switch b.gspRuntime() {
	case "bun":
		cmdName, args = "bun", []string{"run", path}
	case "deno":
		cmdName, args = "deno", []string{"run", "--allow-net", "--allow-read", "--allow-env", "--allow-sys", path}
	}
	cmd := execx.Command(ctx, cmdName, args...)
	cmd.Dir = filepath.Dir(abs)
	if len(b.Env) > 0 {
		cmd.Env = append(os.Environ(), environ.KVList(b.Env)...)
	}
	out, err := cmd.Output()
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return nil, errors.New("execution timed out after 30s")
		}
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return nil, fmt.Errorf("execution failed:\n%s", strings.TrimSpace(string(exitErr.Stderr)))
		}
		return nil, err
	}
	// The module may print before the result; the result is the last line.
	lines := strings.Split(strings.TrimRight(string(out), "\n"), "\n")
	return []byte(lines[len(lines)-1]), nil
}

// bundleExternalizingNative is jseval.Bundle for a bundle that Node, Bun or
// Deno runs: the packages in serverExternalPackages, and every package found to
// contain a native addon, stay external and load from node_modules at run time
// (the way API route bundles do; see buildExternalizingNative).
func (b *Builder) bundleExternalizingNative(opts jseval.Options) (string, error) {
	var externals []string
	if b.Cfg != nil {
		externals = append(externals, b.Cfg.ServerExternalPackages...)
	}
	for attempt := 0; ; attempt++ {
		found := &nativeAddons{}
		o := opts
		o.External = externalPatterns(externals)
		o.Plugins = []api.Plugin{nativeAddonPlugin(found, func(pkg, file string) string {
			if pkg == "" {
				return fmt.Sprintf("%s is a native addon in your own sources and cannot be bundled", file)
			}
			return fmt.Sprintf("package %q contains a native addon (%s) and cannot be bundled", pkg, filepath.Base(file))
		})}
		js, err := jseval.Bundle(o)
		pkgs := found.list()
		if err == nil || len(pkgs) == 0 || attempt >= 4 {
			return js, err
		}
		externals = append(externals, pkgs...)
	}
}

// prefetchMetadata evaluates the chain of page for every param set in one
// run and caches the results, so a page expanded from generateStaticParams
// pays for one evaluation, not one per entry.
func (b *Builder) prefetchMetadata(page string, sets []map[string]string, bnd *bundler.Bundler) {
	if len(sets) == 0 || !b.pageNeedsMetadataEval(page) {
		return
	}
	segs, pageFiles := b.metadataSegments(page)
	results, err := b.evalMetadata(page, segs, pageFiles, sets, bnd)
	for i, set := range sets {
		r := metadataResult{}
		if err != nil {
			r.Errors = []string{err.Error()}
		} else {
			r = results[i]
		}
		metadataCache.Store(metadataKey{page, paramsKey(set)}, r)
	}
}

// pageNeedsMetadataEval reports whether the page's chain has anything the Go
// literal reader cannot resolve alone: a generateMetadata function anywhere,
// or layout metadata to merge.
func (b *Builder) pageNeedsMetadataEval(page string) bool {
	src, err := os.ReadFile(page)
	if err != nil {
		return false
	}
	if _, fn := moduleMetadataExports(string(src)); fn {
		return true
	}
	segs, pageFiles := b.metadataSegments(page)
	return len(segs) > 0 || pageFiles != ""
}

// pageMetadataHeadFor returns the head tags for a page's metadata chain.
//
// requestTime is true for a page that renders per request with a server
// bundle: when its chain has a generateMetadata function, the renderer
// resolves the whole chain per request and the build bakes nothing.
func (b *Builder) pageMetadataHeadFor(page, source string, requestTime bool, params map[string]string, bnd *bundler.Bundler, segs []metaSegment, pageFiles string) string {
	switch strings.ToLower(filepath.Ext(page)) {
	case ".md", ".mdx":
		return ""
	}
	pageMeta, pageFn := moduleMetadataExports(source)
	if !pageMeta && !pageFn && len(segs) == 0 && pageFiles == "" {
		return ""
	}
	chainFn := pageFn
	for _, sg := range segs {
		if sg.Path == "" {
			continue
		}
		if data, err := os.ReadFile(sg.Path); err == nil {
			if _, fn := moduleMetadataExports(string(data)); fn {
				chainFn = true
			}
		}
	}
	if requestTime && chainFn {
		return ""
	}
	// A page-only literal, with nothing to merge, reads without running code.
	if !chainFn && len(segs) == 0 && pageFiles == "" && !viewportExportRe.MatchString(source) {
		if tags, declared := pageMetadataLiteral(page, source); tags != "" || !declared {
			return tags
		}
	}
	res, ok := metadataCache.Load(metadataKey{page, paramsKey(params)})
	var r metadataResult
	if ok {
		r = res.(metadataResult)
	} else {
		results, err := b.evalMetadata(page, segs, pageFiles, []map[string]string{params}, bnd)
		if err != nil {
			r = metadataResult{Errors: []string{err.Error()}}
		} else {
			r = results[0]
		}
		metadataCache.Store(metadataKey{page, paramsKey(params)}, r)
	}
	for i, msg := range r.Errors {
		file := ""
		if i < len(r.ErrorFiles) {
			file = r.ErrorFiles[i]
		}
		site, siteSrc := b.metadataErrorSite(page, source, segs, file)
		name, line, col, text := metadataExportPosition(siteSrc)
		if line == 0 {
			line, col = 1, 1
		}
		b.warnMetadata(site, line, col, text, name+" could not be evaluated at build time: "+msg,
			"the tags of this segment were left out; render the page per request (`export const dynamic = \"force-dynamic\"`) so "+name+" runs with the request, or fix the error")
	}
	if r.Tags == "" {
		return ""
	}
	return metadataTagsToHTML(r.Tags)
}

// metadataErrorSite finds the file and source an evaluation error belongs to:
// the segment that file names, else the page when it exports metadata, else the
// innermost layout that does (an error from the chain is the layout's, even on a
// page such as 404.tsx that exports nothing itself).
func (b *Builder) metadataErrorSite(page, source string, segs []metaSegment, file string) (string, string) {
	if file != "" {
		abs := file
		if !filepath.IsAbs(abs) {
			abs = filepath.Join(b.Root, filepath.FromSlash(file))
		}
		if filepath.Clean(abs) == filepath.Clean(page) {
			return page, source
		}
		if data, err := os.ReadFile(abs); err == nil {
			return abs, string(data)
		}
	}
	if m, f := moduleMetadataExports(source); m || f {
		return page, source
	}
	for i := len(segs) - 1; i >= 0; i-- {
		if segs[i].Path == "" {
			continue
		}
		if data, err := os.ReadFile(segs[i].Path); err == nil {
			if m, f := moduleMetadataExports(string(data)); m || f {
				return segs[i].Path, string(data)
			}
		}
	}
	return page, source
}

// metadataExportPosition names the export a failed evaluation belongs to and
// where it is declared: generateMetadata only when that is all the page has,
// otherwise metadata. A zero line means the export was not found in source.
func metadataExportPosition(source string) (name string, line, col int, text string) {
	hasMeta, hasFn := moduleMetadataExports(source)
	re := metadataExportRe
	name = "metadata"
	if hasFn && !hasMeta {
		name, re = "generateMetadata", generateMetadataRe
	}
	loc := re.FindStringIndex(source)
	if loc == nil {
		return name, 0, 0, ""
	}
	start := loc[0] + len(source[loc[0]:loc[1]]) - len(strings.TrimLeft(source[loc[0]:loc[1]], " \t\r\n"))
	line = strings.Count(source[:start], "\n") + 1
	lineStart := strings.LastIndex(source[:start], "\n") + 1
	end := strings.IndexByte(source[start:], '\n')
	if end < 0 {
		end = len(source) - start
	}
	return name, line, start - lineStart + 1, strings.TrimRight(source[lineStart:start+end], "\r")
}

func (b *Builder) warnMetadata(page string, line, col int, srcLine, msg, hint string) {
	rel := page
	if r, err := filepath.Rel(b.Root, page); err == nil {
		rel = filepath.ToSlash(r)
	}
	printWarningsOnce(os.Stderr, []diag.Diagnostic{{
		File:     rel,
		Line:     line,
		Col:      col,
		Source:   srcLine,
		Severity: diag.SeverityWarning,
		Code:     diag.CodeMetadataNotStatic,
		Message:  msg,
		Hint:     hint,
	}})
}

// metadataTagsToHTML turns the tags (one per line) into the page head text.
func metadataTagsToHTML(tags string) string {
	if !strings.HasSuffix(tags, "\n") {
		tags += "\n"
	}
	return tags
}
