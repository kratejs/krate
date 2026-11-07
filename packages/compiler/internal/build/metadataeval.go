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
func metadataEntrySource(page string, segs []metaSegment, pageFiles, root string, sets []map[string]string, print bool) string {
	var b strings.Builder
	b.WriteString("import { metadataHeadTags } from \"@krate/runtime/server\";\n")
	fmt.Fprintf(&b, "import * as __page from %s;\n", strconv.Quote(filepath.ToSlash(page)))
	var chain []string
	for i, sg := range segs {
		var fields []string
		if sg.Path != "" {
			fmt.Fprintf(&b, "import * as __l%d from %s;\n", i, strconv.Quote(filepath.ToSlash(sg.Path)))
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
	b.WriteString(`const __results = [];
for (const params of __sets) {
  const errors = [];
  let tags = "";
  try {
    tags = await metadataHeadTags(__chain, { params, searchParams: {} }, (_msg, err) => errors.push(String((err && err.message) || err)));
  } catch (err) {
    errors.push(String((err && err.message) || err));
  }
  __results.push({ tags, errors });
}
`)
	if print {
		b.WriteString("console.log(JSON.stringify(__results));\n")
	} else {
		fmt.Fprintf(&b, "globalThis.%s = __results;\n", jseval.ResultGlobal)
	}
	return b.String()
}

func relTo(root, path string) string {
	if r, err := filepath.Rel(root, path); err == nil {
		return filepath.ToSlash(r)
	}
	return filepath.ToSlash(path)
}

// evalMetadata resolves the chain of page for every param set.
func (b *Builder) evalMetadata(page string, segs []metaSegment, pageFiles string, sets []map[string]string, bnd *bundler.Bundler) ([]metadataResult, error) {
	abs, err := filepath.Abs(page)
	if err != nil {
		return nil, err
	}
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
		Source:        metadataEntrySource(abs, segs, pageFiles, b.Root, sets, false),
		ResolveDir:    filepath.Dir(abs),
		Sourcefile:    "krate-metadata-entry.ts",
		Loader:        api.LoaderTSX,
		Env:           b.scriptEnv(),
		Await:         true,
		ResolveImport: resolve,
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

	output, err := b.runBootstrapBundle(abs, metadataEntrySource(abs, segs, pageFiles, b.Root, sets, true), "metadata", bnd)
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
func (b *Builder) runBootstrapBundle(abs, source, prefix string, bnd *bundler.Bundler) ([]byte, error) {
	jsxSource := ""
	if prefix == "imageroute" || prefix == "metaroute" {
		// opengraph-image.tsx builds its element tree with the server JSX runtime.
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
	js, err := jseval.Bundle(jseval.Options{
		Source:        source,
		ResolveDir:    filepath.Dir(abs),
		Sourcefile:    "krate-" + prefix + "-bootstrap.tsx",
		Loader:        api.LoaderTSX,
		ResolveImport: resolve,

		JSXImportSource: jsxSource,
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
	for _, msg := range r.Errors {
		b.warnMetadata(page, "generateMetadata could not be evaluated at build time: "+msg,
			"the tags of this segment were left out; render the page per request (`export const dynamic = \"force-dynamic\"`) so generateMetadata runs with the request, or fix the error")
	}
	if r.Tags == "" {
		return ""
	}
	return metadataTagsToHTML(r.Tags)
}

func (b *Builder) warnMetadata(page, msg, hint string) {
	rel := page
	if r, err := filepath.Rel(b.Root, page); err == nil {
		rel = filepath.ToSlash(r)
	}
	printWarningsOnce(os.Stderr, []diag.Diagnostic{{
		File:     rel,
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
