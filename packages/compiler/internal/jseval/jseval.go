// Package jseval evaluates TypeScript/JavaScript: it bundles an entry module
// (resolving imports) with esbuild and runs it in the embedded QuickJS runtime,
// or on a Node or Bun worker pool when the caller passes one. It backs
// build-time evaluation of user TS such as krate.config.ts, tailwind.config.ts
// and page metadata.
package jseval

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime/debug"
	"strings"
	"time"

	"github.com/evanw/esbuild/pkg/api"

	"github.com/kratejs/krate/packages/compiler/internal/jsworker"
	"github.com/kratejs/krate/packages/compiler/internal/safe"
	"github.com/kratejs/krate/packages/compiler/internal/tsstrip"
	"github.com/kratejs/krate/packages/native/jsruntime"
)

// ResultGlobal is the global the entry assigns its result to. Eval returns its
// JSON encoding, so entries end with e.g.
// `globalThis.__krateEvalResult = config;`.
const ResultGlobal = "__krateEvalResult"

// Options configures Eval.
type Options struct {
	// Source is the entry module source (TS/TSX/JS). It may import real files,
	// which esbuild resolves and bundles relative to ResolveDir.
	Source string
	// ResolveDir is the esbuild resolve/working directory for the entry.
	ResolveDir string
	// Sourcefile names the entry in diagnostics (default "__krate_entry.ts").
	Sourcefile string
	// Loader is the entry loader (default TS).
	Loader api.Loader
	// Env, when non-empty, is exposed to the script as process.env.
	Env map[string]string
	// ResolveImport, when set, is consulted before esbuild for every import
	// specifier. Returning ok=true resolves it to an absolute file path (used to
	// map `krate/content` and tsconfig path aliases, which esbuild can't see).
	ResolveImport func(importer, spec string) (string, bool)
	// Await drains the QuickJS job queue after the module evaluates, so a result
	// assigned from a resolved Promise becomes visible.
	Await bool
	// NodeShims resolves the Node built-ins configs use (node:path, node:url,
	// node:fs, node:os) to small in-process implementations, and any other
	// node: import to a stub that fails with a precise message.
	NodeShims bool
	// JSXImportSource, when set, compiles JSX with the automatic runtime from
	// that package (for example "@krate/runtime/server"), ignoring tsconfig.
	JSXImportSource string
	// VirtualFiles maps absolute paths to module source that does not exist on
	// disk. An import of such a path loads the given source, and its own
	// imports resolve relative to that path as if the file were real.
	VirtualFiles map[string]string
	// External lists esbuild external patterns (package names, "pkg/*") that
	// stay unbundled.
	External []string
	// Plugins are extra esbuild plugins, run after the built-in ones.
	Plugins []api.Plugin
	// SourceTransform, when set, rewrites the source of every JS/TS file the
	// bundle loads from disk (path is absolute) before it is compiled.
	SourceTransform func(path, src string) string
	// Worker, when set, runs the bundle in a Node or Bun worker process
	// instead of QuickJS. When the worker is unavailable, or the bundle only
	// parses as a module there, the embedded engine runs it. Ignored with
	// NodeShims.
	Worker *jsworker.Pool
	// WorkerLabel names the evaluation in the pool's profile.
	WorkerLabel string
	// QuickJSDone, when set, is called with the time a QuickJS run took.
	QuickJSDone func(time.Duration)
}

// defaultMemoryLimit is the QuickJS heap limit of one evaluation. Bundled page
// modules and their dependencies need far more than the 32 MiB the runtime
// allows request handlers; KRATE_JS_MEMORY_MB overrides it.
const defaultMemoryLimit = 128 << 20

// Eval bundles the entry (resolving imports) and runs it in the embedded QuickJS
// runtime, returning the JSON value assigned to globalThis.__krateEvalResult.
// It returns an error when the source cannot be bundled or executed, so callers
// can fall back to a static path.
func Eval(opts Options) (out json.RawMessage, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = safe.Recover(r)
		}
	}()

	bundle, err := bundle(opts, false)
	if err != nil {
		return nil, err
	}
	err = guardFaults(func() (rerr error) {
		out, rerr = run(opts, bundle)
		return rerr
	})
	return out, err
}

// guardFaults runs fn and turns a crash of the embedded engine into an error.
//
// QuickJS can corrupt its heap when an allocation fails while it builds the
// out-of-memory error (a use-after-free in js_free_shape, reached from
// JS_ThrowOutOfMemory). That surfaces as a fault in the transpiled engine,
// which Go treats as fatal unless the goroutine asks for a panic instead. The
// runtime is discarded after the call, so the damage stays inside it.
func guardFaults(fn func() error) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("the embedded JavaScript engine crashed (it probably ran out of memory; KRATE_JS_MEMORY_MB raises the limit): %w", safe.Recover(r))
		}
	}()
	defer debug.SetPanicOnFault(debug.SetPanicOnFault(true))
	return fn()
}

// run executes the bundled entry in a fresh runtime and returns its result.
func run(opts Options, bundle string) (json.RawMessage, error) {
	if opts.Worker != nil && !opts.NodeShims {
		if out, ok, err := runWorker(opts, bundle); ok {
			return out, err
		}
	}
	if opts.QuickJSDone != nil {
		defer func(t0 time.Time) { opts.QuickJSDone(time.Since(t0)) }(time.Now())
	}
	rtOpts := jsruntime.Options{}
	if os.Getenv("KRATE_JS_MEMORY_MB") == "" {
		rtOpts.MemoryLimit = defaultMemoryLimit
	}
	rt, err := jsruntime.NewWithOptions(rtOpts)
	if err != nil {
		return nil, fmt.Errorf("creating JS runtime: %w", err)
	}
	defer rt.Close()
	if len(opts.Env) > 0 {
		_ = rt.SetEnv(opts.Env)
	}
	if opts.NodeShims {
		registerNodeHost(rt, opts.ResolveDir)
	}

	if _, err := rt.ExecuteModule(bundle); err != nil {
		return nil, explainDynamicRequire(err)
	}
	if opts.Await {
		rt.DrainJobs()
	}
	if msg, _ := rt.Execute(errorExpr); msg != nil {
		if text, _ := msg.(string); text != "" {
			return nil, &ThrownError{Text: text}
		}
	}
	res, err := rt.Execute(resultExpr)
	if err != nil {
		return nil, err
	}
	s, _ := res.(string)
	if s == "" || s == "null" {
		return nil, fmt.Errorf("entry produced no %s (the module may have run out of memory; KRATE_JS_MEMORY_MB raises the limit)", ResultGlobal)
	}
	return json.RawMessage(s), nil
}

// Expressions the result is read with, in either engine.
const (
	errorExpr  = `typeof globalThis.` + ErrorGlobal + ` === "string" ? globalThis.` + ErrorGlobal + ` : ""`
	resultExpr = `JSON.stringify(globalThis.` + ResultGlobal + ` ?? null)`
	untilExpr  = `globalThis.` + ResultGlobal + ` !== undefined || typeof globalThis.` + ErrorGlobal + ` === "string"`
)

// runWorker is run on the worker pool; ok is false when QuickJS must run the
// bundle instead.
func runWorker(opts Options, bundle string) (json.RawMessage, bool, error) {
	res, err := opts.Worker.Run(jsworker.Request{
		Code:   bundle,
		Module: true,
		Env:    opts.Env,
		Await:  opts.Await,
		Until:  untilExpr,
		Reads:  []string{errorExpr, resultExpr},
		Label:  opts.WorkerLabel,
	})
	if err != nil || res.Unsupported || len(res.Reads) != 2 {
		return nil, false, nil
	}
	if v := res.Reads[0]; v.IsString && v.S != "" {
		return nil, true, &ThrownError{Text: v.S}
	}
	v := res.Reads[1]
	if v.Err != "" {
		return nil, true, errors.New(v.Err)
	}
	if !v.IsString || v.S == "" || v.S == "null" {
		return nil, true, fmt.Errorf("entry produced no %s (the module may have run out of memory; KRATE_JS_MEMORY_MB raises the limit)", ResultGlobal)
	}
	return json.RawMessage(v.S), true, nil
}

// BundleMessage is one esbuild error. File is absolute and Col is 1-based; both
// are zero when the error has no source location.
type BundleMessage struct {
	File     string
	Line     int
	Col      int
	Text     string
	LineText string
}

// BundleError is returned when the entry or a module it imports cannot be
// bundled. Callers use Messages to tell a syntax error in a file from a missing
// package.
type BundleError struct {
	Messages []BundleMessage
	text     string
}

func (e *BundleError) Error() string { return "bundling entry:\n" + e.text }

// Bundle compiles the entry module and returns plain ESM JavaScript without
// executing it, for a Node-compatible runtime. Node built-ins stay external and
// CommonJS dependencies may require() them (createRequire banner).
func Bundle(opts Options) (string, error) {
	return bundle(opts, true)
}

func bundle(opts Options, node bool) (string, error) {
	sourcefile := opts.Sourcefile
	if sourcefile == "" {
		sourcefile = "__krate_entry.ts"
	}
	loader := opts.Loader
	if loader == api.LoaderNone {
		loader = api.LoaderTS
	}
	var plugins []api.Plugin
	virtual := map[string]string{}
	for p, src := range opts.VirtualFiles {
		virtual[filepath.Clean(p)] = src
	}
	if len(virtual) > 0 {
		plugins = append(plugins, virtualFilesPlugin(virtual))
	}
	plugins = append(plugins, MarkerPackagesPlugin())
	if opts.NodeShims {
		plugins = append(plugins, nodeShimPlugin())
	}
	if opts.ResolveImport != nil {
		plugins = append(plugins, resolveForwarder(opts.ResolveImport))
	}
	plugins = append(plugins, krateResolver(), importMetaPreserver(opts.NodeShims || node, virtual, opts.SourceTransform))
	plugins = append(plugins, opts.Plugins...)
	buildOpts := api.BuildOptions{
		Stdin: &api.StdinOptions{
			Contents:   opts.Source,
			ResolveDir: opts.ResolveDir,
			Sourcefile: sourcefile,
			Loader:     loader,
		},
		Bundle:   true,
		Format:   api.FormatESModule,
		Platform: api.PlatformNode,
		Write:    false,
		LogLevel: api.LogLevelSilent,
		Plugins:  plugins,
		External: opts.External,
	}
	if opts.JSXImportSource != "" {
		buildOpts.JSX = api.JSXAutomatic
		buildOpts.JSXImportSource = opts.JSXImportSource
		buildOpts.TsconfigRaw = `{ "compilerOptions": { "jsx": "react-jsx", "jsxImportSource": "` + opts.JSXImportSource + `" } }`
	}
	tsstrip.HandleTierDecorators(&buildOpts)
	if node {
		ApplyNodeESM(&buildOpts)
	}
	result := api.Build(buildOpts)
	if len(result.Errors) > 0 {
		var sb strings.Builder
		be := &BundleError{}
		for _, e := range result.Errors {
			if e.Location != nil {
				fmt.Fprintf(&sb, "%s:%d:%d: %s\n", e.Location.File, e.Location.Line, e.Location.Column, e.Text)
				file := e.Location.File
				if abs, err := filepath.Abs(file); err == nil {
					file = abs
				}
				be.Messages = append(be.Messages, BundleMessage{
					File: file, Line: e.Location.Line, Col: e.Location.Column + 1,
					Text: e.Text, LineText: strings.TrimRight(e.Location.LineText, "\r\n"),
				})
			} else {
				sb.WriteString(e.Text + "\n")
				be.Messages = append(be.Messages, BundleMessage{Text: e.Text})
			}
		}
		be.text = sb.String()
		return "", be
	}
	if len(result.OutputFiles) == 0 {
		return "", fmt.Errorf("bundle produced no output")
	}
	return string(result.OutputFiles[0].Contents), nil
}

// importMetaPreserver rewrites, per source file, `import.meta.url` to a string
// literal of that file's own file:// URL, so bundling every module together does
// not collapse their URLs onto the bundle's location. Plugin and theme factories
// rely on this to point the compiler back at their real source on disk.
func importMetaPreserver(nodeShims bool, virtual map[string]string, transform func(path, src string) string) api.Plugin {
	return api.Plugin{
		Name: "krate-preserve-import-meta",
		Setup: func(pb api.PluginBuild) {
			// Style and asset imports carry no data for build-time evaluation;
			// load them empty instead of parsing them as JavaScript.
			pb.OnLoad(api.OnLoadOptions{Filter: assetFilter, Namespace: "file"}, func(args api.OnLoadArgs) (api.OnLoadResult, error) {
				if args.Namespace != "file" {
					return api.OnLoadResult{}, nil
				}
				empty := ""
				return api.OnLoadResult{Contents: &empty, Loader: api.LoaderJS}, nil
			})
			pb.OnLoad(api.OnLoadOptions{Filter: `\.(?:[cm]?[jt]sx?|json)$`, Namespace: "file"}, func(args api.OnLoadArgs) (api.OnLoadResult, error) {
				if args.Namespace != "file" {
					return api.OnLoadResult{}, nil
				}
				src, isVirtual := virtual[filepath.Clean(args.Path)]
				if !isVirtual {
					data, err := os.ReadFile(args.Path)
					if err != nil {
						return api.OnLoadResult{}, nil
					}
					src = string(data)
					if transform != nil {
						src = transform(args.Path, src)
					}
				}
				if strings.Contains(src, "import.meta.") {
					src = rewriteImportMeta(src, metaLiterals{
						url:  fmt.Sprintf("%q", fileURL(args.Path)),
						dir:  fmt.Sprintf("%q", filepath.ToSlash(filepath.Dir(args.Path))),
						file: fmt.Sprintf("%q", filepath.ToSlash(args.Path)),
					})
				}
				contents := src
				contents = tsstrip.BlankTierDecorators(args.Path, contents)
				if nodeShims {
					contents = injectDirnameVars(args.Path, contents)
				}
				return api.OnLoadResult{Contents: &contents, Loader: loaderForPath(args.Path)}, nil
			})
		},
	}
}

// virtualFilesPlugin resolves the absolute paths of files that exist only in
// memory; importMetaPreserver loads them.
func virtualFilesPlugin(virtual map[string]string) api.Plugin {
	return api.Plugin{
		Name: "krate-virtual-files",
		Setup: func(pb api.PluginBuild) {
			pb.OnResolve(api.OnResolveOptions{Filter: absPathFilter}, func(args api.OnResolveArgs) (api.OnResolveResult, error) {
				if !filepath.IsAbs(args.Path) {
					return api.OnResolveResult{}, nil
				}
				p := filepath.Clean(args.Path)
				if _, ok := virtual[p]; !ok {
					return api.OnResolveResult{}, nil
				}
				return api.OnResolveResult{Path: p, Namespace: "file"}, nil
			})
		},
	}
}

var (
	dirnameDeclRe  = regexp.MustCompile(`\b(?:const|let|var|function|class)\s+__dirname\b`)
	filenameDeclRe = regexp.MustCompile(`\b(?:const|let|var|function|class)\s+__filename\b`)
)

// injectDirnameVars gives a module the CommonJS __dirname and __filename of its
// own file, which QuickJS does not define. The declaration goes on the first
// line (no newline) so positions in errors stay put, and only for a name the
// module uses without declaring it (`const __dirname = dirname(fileURLToPath(
// import.meta.url))` is the usual ESM idiom and must keep working).
func injectDirnameVars(path, src string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".json":
		return src
	}
	if strings.HasPrefix(src, "#!") {
		return src
	}
	var decl string
	if strings.Contains(src, "__dirname") && !dirnameDeclRe.MatchString(src) {
		decl += fmt.Sprintf("var __dirname=%q;", filepath.ToSlash(filepath.Dir(path)))
	}
	if strings.Contains(src, "__filename") && !filenameDeclRe.MatchString(src) {
		decl += fmt.Sprintf("var __filename=%q;", filepath.ToSlash(path))
	}
	return decl + src
}

// absPathFilter matches every specifier filepath.IsAbs accepts on any OS: a
// rooted path, a drive path with either separator, and a UNC path.
const absPathFilter = `^(?:/|[A-Za-z]:[\\/]|\\[\\/])`

// assetFilter matches imports of styles, images, fonts and other non-code
// assets.
const assetFilter = `\.(?:css|scss|sass|less|styl|pcss|svg|png|jpe?g|gif|webp|avif|ico|bmp|woff2?|ttf|otf|eot|mp4|webm|mp3|wav|ogg|pdf|wasm)$`

// metaLiterals holds the quoted replacements for the import.meta properties
// importMetaPreserver rewrites: url is the file:// URL, dir and file are the
// module's directory and path.
type metaLiterals struct {
	url, dir, file string
}

// match returns the literal for the import.meta property at src[i:end] and its
// length, or 0 when none starts there.
func (m metaLiterals) match(src string, i, end int) (string, int) {
	for _, t := range []struct{ name, lit string }{
		{"import.meta.url", m.url},
		{"import.meta.dirname", m.dir},
		{"import.meta.filename", m.file},
	} {
		if end-i >= len(t.name) && src[i:i+len(t.name)] == t.name && identifierBoundary(src, i, i+len(t.name)) {
			return t.lit, len(t.name)
		}
	}
	return "", 0
}

// rewriteImportMeta returns src with every code-context occurrence of
// import.meta.url, import.meta.dirname and import.meta.filename replaced by
// its quoted literal. Occurrences inside string literals, template-literal
// text, and comments are left untouched, so source that merely mentions the
// text (for example a docs code sample held in a string) is not corrupted.
func rewriteImportMeta(src string, lits metaLiterals) string {
	var b strings.Builder
	b.Grow(len(src))
	writeCode(&b, src, 0, len(src), lits, false)
	return b.String()
}

// writeCode copies src[begin:end] in code context, replacing import.meta properties
// with lits. When stopAtBrace is set it returns just past the '}' that closes the
// enclosing template expression; otherwise it consumes the whole range.
func writeCode(b *strings.Builder, src string, begin, end int, lits metaLiterals, stopAtBrace bool) int {
	depth := 0
	i := begin
	for i < end {
		c := src[i]
		switch {
		case c == '{':
			depth++
			b.WriteByte(c)
			i++
		case c == '}' && stopAtBrace && depth == 0:
			b.WriteByte(c)
			return i + 1
		case c == '}':
			if depth > 0 {
				depth--
			}
			b.WriteByte(c)
			i++
		case c == '/' && i+1 < end && src[i+1] == '/':
			i = writeLineComment(b, src, i, end)
		case c == '/' && i+1 < end && src[i+1] == '*':
			i = writeBlockComment(b, src, i, end)
		case c == '\'' || c == '"':
			i = writeQuoted(b, src, i, end)
		case c == '`':
			i = writeTemplate(b, src, i, end, lits)
		default:
			if c == 'i' {
				if lit, n := lits.match(src, i, end); n > 0 {
					b.WriteString(lit)
					i += n
					continue
				}
			}
			b.WriteByte(c)
			i++
		}
	}
	return i
}

func writeLineComment(b *strings.Builder, src string, i, end int) int {
	j := i + 2
	for j < end && src[j] != '\n' {
		j++
	}
	b.WriteString(src[i:j])
	return j
}

func writeBlockComment(b *strings.Builder, src string, i, end int) int {
	stop := end
	if j := strings.Index(src[i+2:end], "*/"); j >= 0 {
		stop = i + 2 + j + 2
	}
	b.WriteString(src[i:stop])
	return stop
}

func writeQuoted(b *strings.Builder, src string, i, end int) int {
	q := src[i]
	j := i + 1
	for j < end {
		switch src[j] {
		case '\\':
			j += 2
			continue
		case q:
			j++
			b.WriteString(src[i:j])
			return j
		case '\n':
			b.WriteString(src[i:j])
			return j
		}
		j++
	}
	b.WriteString(src[i:end])
	return end
}

// writeTemplate copies a template literal starting at the backtick src[i],
// rewriting import.meta properties inside each ${...} substitution and leaving the
// literal text untouched.
func writeTemplate(b *strings.Builder, src string, i, end int, lits metaLiterals) int {
	j := i + 1
	for j < end {
		switch src[j] {
		case '\\':
			j += 2
			continue
		case '`':
			j++
			b.WriteString(src[i:j])
			return j
		case '$':
			if j+1 < end && src[j+1] == '{' {
				b.WriteString(src[i : j+2])
				i = writeCode(b, src, j+2, end, lits, true)
				j = i
				continue
			}
			j++
		default:
			j++
		}
	}
	b.WriteString(src[i:end])
	return end
}

// identifierBoundary reports whether src[start:end] is a standalone token (not
// adjacent to an identifier character or a property access dot).
func identifierBoundary(src string, start, end int) bool {
	if start > 0 {
		if p := src[start-1]; isIdentByte(p) || p == '.' {
			return false
		}
	}
	if end < len(src) {
		if n := src[end]; isIdentByte(n) || n == '.' {
			return false
		}
	}
	return true
}

func isIdentByte(c byte) bool {
	return c == '_' || c == '$' ||
		(c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') ||
		(c >= '0' && c <= '9')
}

// fileURL converts an absolute filesystem path to a file:// URL (with a leading
// slash so Windows drive letters become file:///C:/...).
func fileURL(abs string) string {
	p := filepath.ToSlash(abs)
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return "file://" + p
}

func loaderForPath(p string) api.Loader {
	switch strings.ToLower(filepath.Ext(p)) {
	case ".ts":
		return api.LoaderTS
	case ".tsx":
		return api.LoaderTSX
	case ".jsx":
		return api.LoaderJSX
	case ".json":
		return api.LoaderJSON
	case ".mjs", ".cjs", ".js":
		return api.LoaderJS
	default:
		return api.LoaderJS
	}
}
