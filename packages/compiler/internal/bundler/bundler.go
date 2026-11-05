package bundler

import (
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/css"
	"github.com/kratejs/krate/packages/compiler/internal/diag"
	"github.com/kratejs/krate/packages/compiler/internal/escape"
	"github.com/kratejs/krate/packages/compiler/internal/lexer"
	"github.com/kratejs/krate/packages/compiler/internal/markdown"
	"github.com/kratejs/krate/packages/compiler/internal/parser"
	"github.com/kratejs/krate/packages/compiler/internal/resolver"
	"github.com/kratejs/krate/packages/compiler/internal/tsstrip"
)

type Module struct {
	Path           string
	Program        *ast.Program
	Imports        []string
	IsEntry        bool
	IsCSS          bool
	IsExternal     bool
	ComponentClass ComponentClass // server, runtime, or client component
	SourceCode     string         // raw source for directive scanning
	// Rewritable is true when a per-bundle rewrite pass (CSS-module refs, asset
	// imports, workers, dynamic imports) may mutate this module's AST. Such
	// modules are never shared via the parse cache (the rewrites are not
	// idempotent), so caching stays safe for the rewrite-free majority.
	Rewritable bool
	// Diagnostics are coded warnings from the React rewrite (for example a
	// dependency array the compiler cannot resolve).
	Diagnostics []diag.Diagnostic
}

// moduleNeedsRewrite reports whether any per-bundle rewrite pass could touch a
// module with this source/import set. Conservative: false negatives would leave
// a rewrite unapplied, so the checks bias toward "true".
func moduleNeedsRewrite(src string, imports []string) bool {
	if strings.Contains(src, "Worker") || strings.Contains(src, "import(") {
		return true
	}
	for _, imp := range imports {
		if assetExtensions[strings.ToLower(filepath.Ext(imp))] {
			return true
		}
		if strings.HasSuffix(imp, ".module.css") {
			return true
		}
		if isDataImport(imp) {
			return true
		}
	}
	return false
}

type CSSModuleInfo struct {
	ResolvedPath string
	ScopedCSS    string
	Mappings     map[string]string
}

type Bundle struct {
	EntryPath  string
	Modules    []*Module
	CSS        string
	CSSModules map[string]*CSSModuleInfo
	// CSSDeps are the files the page's CSS was built from beyond its modules:
	// @import targets, url() assets and composes sources.
	CSSDeps        []string
	AssetFiles     map[string]string // resolved source path -> hashed site URL (/assets/...)
	WorkerFiles    map[string]string // worker source path -> hashed site URL (/workers/...)
	WorkerEsm      map[string]bool   // worker source path -> true when built with `type: 'module'`
	DynImportFiles map[string]string // dynamic-import source path -> hashed site URL (/chunks/...)
	Frontmatter    map[string]any    // .mdx frontmatter, if any
	ActionImports  map[string]string // local binding -> server-action URL
}

type Bundler struct {
	root              string
	seen              map[string]bool
	order             []*Module
	css               []string
	cssModules        map[string]*CSSModuleInfo
	cssDeps           map[string]bool
	assets            map[string]string // resolved source path -> hashed site URL
	workers           map[string]string // worker source path -> hashed site URL (/workers/...)
	workerEsm         map[string]bool   // worker source path -> built as ES module
	dynImports        map[string]string // dynamic-import source path -> hashed site URL (/chunks/...)
	frontmatter       map[string]any    // from .mdx frontmatter
	actionModules     map[string]map[string]string
	actionBasePath    string
	actionImports     map[string]string
	actionErr         error
	pathAliases       []pathAlias
	tsBaseDir         string
	baseURL           string
	serverComponents  []string
	runtimeComponents []string
	serverDirs        []string
	runtimeDirs       []string

	// virtualModules maps a bare import specifier (e.g. "krate/content") to a
	// generated file path on disk. Krate codegens these (content collections,
	// route manifests) so pages can import build-time data without a real
	// package on disk.
	virtualModules map[string]string

	// cache memoizes per-file parse results. Shared across bundles in a build
	// so shared modules are parsed once instead of once per page. May be nil.
	cache *ModuleCache

	// tsOpts are the tsconfig options used when stripping TypeScript.
	tsOpts tsstrip.Options

	// dev selects development export conditions.
	dev bool

	// twImports controls how `@import "tailwindcss"` is treated in CSS.
	twImports css.TailwindImportMode

	// mdCfg is the markdown configuration for .md/.mdx modules. The zero
	// value (mdSet false) means markdown.DefaultConfig().
	mdCfg markdown.Config
	mdSet bool
	// mdBase is the site base path prefixed to image URLs found in markdown.
	mdBase string
}

// SetMarkdownBasePath sets the site base path prepended to relative images
// that markdown files reference.
func (b *Bundler) SetMarkdownBasePath(base string) { b.mdBase = base }

// SetMarkdownConfig sets the markdown options used when compiling .md and
// .mdx modules (page-level and imported).
func (b *Bundler) SetMarkdownConfig(cfg markdown.Config) {
	b.mdCfg = cfg
	b.mdSet = true
}

func (b *Bundler) markdownConfig() markdown.Config {
	if b.mdSet {
		return b.mdCfg
	}
	return markdown.DefaultConfig()
}

// SetDevMode picks the development or production package.json export
// conditions used to resolve npm packages.
func (b *Bundler) SetDevMode(dev bool) { b.dev = dev }

func (b *Bundler) conditions() resolver.Conditions { return resolver.ClientConditions(b.dev) }

// SetTailwindImports configures the handling of Tailwind package imports.
func (b *Bundler) SetTailwindImports(m css.TailwindImportMode) { b.twImports = m }

// SetTSOptions configures how TypeScript sources are lowered (decorators,
// class-field semantics) before parsing.
func (b *Bundler) SetTSOptions(opts tsstrip.Options) { b.tsOpts = opts }

// parseError keeps each diagnostic addressable while printing them together.
type parseError struct {
	summary string
	errs    []error
}

func (e *parseError) Error() string   { return e.summary }
func (e *parseError) Unwrap() []error { return e.errs }

// SetModuleCache attaches a shared parse cache. A nil cache disables caching.
func (b *Bundler) SetModuleCache(c *ModuleCache) {
	b.cache = c
}

// assetExtensions are file extensions that get copied to /assets/ with a
// content-hashed name and replaced by their URL string wherever imported.
var assetExtensions = map[string]bool{
	".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".webp": true,
	".avif": true, ".svg": true, ".ico": true, ".bmp": true,
	".wasm": true, ".glb": true, ".gltf": true, ".obj": true, ".mtl": true,
	".bin": true, ".hdr": true, ".exr": true,
	".mp4": true, ".webm": true, ".ogv": true, ".mp3": true, ".wav": true,
	".ogg": true, ".flac": true,
	".woff": true, ".woff2": true, ".ttf": true, ".otf": true, ".eot": true,
}

// pathAlias is an internal representation of a TypeScript path alias.
type pathAlias struct {
	prefix  string
	targets []string
}

func New(root string) *Bundler {
	return &Bundler{
		root:           root,
		seen:           make(map[string]bool),
		assets:         make(map[string]string),
		workers:        make(map[string]string),
		workerEsm:      make(map[string]bool),
		dynImports:     make(map[string]string),
		virtualModules: make(map[string]string),
		cache:          NewModuleCache(),
	}
}

// SetVirtualModules maps bare import specifiers to generated files on disk.
// The bundler resolves these before node_modules so codegen'd modules such as
// `krate/content` can be imported by pages.
func (b *Bundler) SetVirtualModules(mods map[string]string) {
	if b.virtualModules == nil {
		b.virtualModules = make(map[string]string)
	}
	for spec, path := range mods {
		b.virtualModules[spec] = path
	}
}

// SetPathAliases configures TypeScript path alias resolution.
// prefixes and targets are parallel slices: prefixes[i] maps to targets[i].
func (b *Bundler) SetPathAliases(prefixes []string, targets [][]string, tsBaseDir string) {
	b.tsBaseDir = tsBaseDir
	b.pathAliases = nil
	for i, prefix := range prefixes {
		if i < len(targets) {
			b.pathAliases = append(b.pathAliases, pathAlias{prefix: prefix, targets: targets[i]})
		}
	}
	registerPathAliases(b.pathAliases, tsBaseDir)
}

// SetBaseURL sets tsconfig compilerOptions.baseUrl (absolute). A bare import
// that node_modules cannot satisfy is then tried as a path under it.
func (b *Bundler) SetBaseURL(dir string) { b.baseURL = dir }

// SetServerComponents configures the server component name lists and directory
// lists for classification. Components in serverDirs are treated as @server,
// components in runtimeDirs are treated as @runtime.
func (b *Bundler) SetServerComponents(server []string, runtime []string, serverDirs []string, runtimeDirs []string) {
	b.serverComponents = server
	b.runtimeComponents = runtime
	b.serverDirs = serverDirs
	b.runtimeDirs = runtimeDirs
}

// resolverCacheEnabled gates the filesystem/import resolution memo. Set
// KRATE_NO_RESOLVER_CACHE to disable it (used to A/B resolver caching).
var resolverCacheEnabled = os.Getenv("KRATE_NO_RESOLVER_CACHE") == ""

// fileExists reports whether path exists, memoizing os.Stat results on the
// shared build cache so repeated probes (extension guessing, node_modules
// walks) don't re-hit the filesystem across pages.
func (b *Bundler) fileExists(path string) bool {
	if b.cache == nil || !resolverCacheEnabled {
		return resolver.Exists(path)
	}
	if v, ok := b.cache.res.existsResult(path); ok {
		return v
	}
	v := resolver.Exists(path)
	b.cache.res.putExists(path, v)
	return v
}

// resolveImportForModule resolves an import, checking virtual modules and path
// aliases first. Results are memoized per (importer dir, specifier) on the
// shared build cache, so the same import from the same directory (very common
// across pages) does no filesystem work after the first resolution.
func (b *Bundler) resolveImportForModule(importer, imp string) string {
	if b.cache == nil || !resolverCacheEnabled {
		return b.resolveImportForModuleUncached(importer, imp)
	}
	key := filepath.Dir(importer) + "\x00" + imp
	if v, ok := b.cache.res.importResult(key); ok {
		return v
	}
	v := b.resolveImportForModuleUncached(importer, imp)
	b.cache.res.putImport(key, v)
	return v
}

// Resolve resolves an import specifier the way the bundler does (virtual
// modules, relative paths, tsconfig path aliases, and krate/* packages),
// returning "" when it cannot be resolved. Exposed so in-process evaluation
// (e.g. generateStaticParams) shares the bundler's resolution rules.
func (b *Bundler) Resolve(importer, spec string) string {
	return b.resolveImportForModule(importer, spec)
}

func (b *Bundler) resolveImportForModuleUncached(importer, imp string) string {
	if b.virtualModules != nil {
		if path, ok := b.virtualModules[imp]; ok {
			return path
		}
	}
	// React ecosystem imports resolve to Krate's compatibility runtime. React
	// source hooks/JSX are lowered by RewriteReact; this catches imports that
	// survive that pass (react-dom, react/jsx-runtime in compiled packages).
	if compat := reactCompatSpecifier(imp); compat != "" {
		if p := resolver.NodeModule(filepath.Dir(importer), compat); p != "" {
			return p
		}
	}
	// tsconfig paths win over node_modules, as in tsc.
	if len(b.pathAliases) > 0 && b.tsBaseDir != "" {
		if resolved := resolvePathAlias(imp, b.pathAliases, b.tsBaseDir, b.fileExists); resolved != "" {
			return resolved
		}
	}
	resolved := resolveImport(importer, imp, b.fileExists, b.conditions())
	if resolved == "" && b.baseURL != "" && isBareSpecifier(imp) {
		// baseUrl without a matching paths entry: tsc resolves non-relative
		// specifiers against it. node_modules wins here, so an installed
		// package is never shadowed by a same-named folder.
		resolved = resolveFileOrDir(filepath.Join(b.baseURL, filepath.FromSlash(imp)), b.fileExists)
	}
	return resolved
}

// isBareSpecifier reports whether imp names a package or baseUrl-relative path
// (not relative, absolute, a URL, or a #subpath import).
func isBareSpecifier(imp string) bool {
	return imp != "" && !strings.HasPrefix(imp, ".") && !strings.HasPrefix(imp, "/") &&
		!strings.HasPrefix(imp, "#") && !strings.Contains(imp, ":")
}

// matchesPathAlias reports whether imp matches a configured tsconfig paths
// pattern, so a miss is reported as a broken alias rather than a missing
// package.
func (b *Bundler) matchesPathAlias(imp string) (string, bool) {
	for _, a := range b.pathAliases {
		if strings.HasSuffix(a.prefix, "/*") {
			base := strings.TrimSuffix(a.prefix, "/*")
			if imp == base || strings.HasPrefix(imp, base+"/") {
				return a.prefix, true
			}
		} else if imp == a.prefix {
			return a.prefix, true
		}
	}
	return "", false
}

// unresolvedImportError builds the diagnostic for an import in mod that no
// resolution rule could satisfy. It returns nil for imports that are handled
// elsewhere (compat runtime specifiers) or that live in third-party code,
// which cannot act on the hint.
func (b *Bundler) unresolvedImportError(mod *Module, imp string) error {
	if mod.IsExternal || mod.Program == nil {
		return nil
	}
	if resolver.IsCompiledDependency(mod.Path) {
		return nil
	}
	if reactCompatSpecifier(imp) != "" || strings.HasPrefix(imp, "node:") {
		return nil
	}
	// The client runtime is shipped by the compiler (window globals), so its
	// package need not be installed for a page to build.
	// server-only / client-only are marker packages enforced by the build's
	// client-boundary check, not modules to load.
	if imp == "server-only" || imp == "client-only" {
		return nil
	}
	if imp == "@krate/runtime" || strings.HasPrefix(imp, "@krate/runtime/") {
		return nil
	}
	var pos ast.Pos
	for _, stmt := range mod.Program.Body {
		switch st := stmt.(type) {
		case *ast.ImportStmt:
			if strings.Trim(st.Source, "\"'") == imp {
				pos = st.Position
			}
		case *ast.ExportStmt:
			if strings.Trim(st.ReexportSource, "\"'") == imp {
				pos = st.Position
			}
		}
		if pos.Line > 0 {
			break
		}
	}
	d := diag.Diagnostic{File: mod.Path, Line: pos.Line, Col: pos.Col}
	if mod.SourceCode != "" && pos.Line > 0 {
		lines := strings.Split(mod.SourceCode, "\n")
		if pos.Line <= len(lines) {
			d.Source = strings.TrimRight(lines[pos.Line-1], "\r")
		}
	}
	switch {
	case strings.HasPrefix(imp, ".") || strings.HasPrefix(imp, "/"):
		d.Message = "cannot resolve import " + strconv.Quote(imp)
		d.Hint = "no file matches that path relative to " + filepath.Base(filepath.Dir(mod.Path)) + "/ (tried .tsx, .ts, .jsx, .js, .css, .json and index files); check the spelling and the file extension"
	default:
		if prefix, ok := b.matchesPathAlias(imp); ok {
			d.Message = "cannot resolve import " + strconv.Quote(imp) + " (matched tsconfig path alias " + strconv.Quote(prefix) + ")"
			d.Hint = "no file exists at the alias target; targets resolve relative to compilerOptions.baseUrl (" + b.tsBaseDir + "). If baseUrl is \"./src\" and the targets also start with ./src/, run `krate fix --apply` to set baseUrl to \".\""
		} else {
			d.Message = "cannot resolve import " + strconv.Quote(imp) + ": package is not installed"
			d.Hint = "install it with `pnpm add " + packageNameOf(imp) + "`, or fix the import if it is a typo or a missing tsconfig paths alias"
		}
	}
	return d
}

func packageNameOf(spec string) string {
	parts := strings.Split(spec, "/")
	if strings.HasPrefix(spec, "@") && len(parts) >= 2 {
		return parts[0] + "/" + parts[1]
	}
	return parts[0]
}

func (b *Bundler) Bundle(entry string) (*Bundle, error) {
	b.seen = make(map[string]bool)
	b.order = nil
	b.css = nil
	b.cssModules = make(map[string]*CSSModuleInfo)
	b.cssDeps = make(map[string]bool)
	b.frontmatter = nil
	b.actionImports = make(map[string]string)
	b.actionErr = nil

	err := b.resolveModule(entry, true)
	if err != nil {
		return nil, err
	}
	b.isolatePrivateNames()

	// Rewrite CSS module member expressions (styles.card) to their hashed
	// literal values now that all modules are resolved. Without this, both the
	// SSR HTML and the hydration JS reference the undefined `styles` import at
	// runtime (class bindings degrade to data-kattr-class markers that throw
	// "styles is not defined").
	b.rewriteCSSModuleRefs()

	// Inline JSON, ?raw and ?url imports as module constants so SSR text and
	// hydration code both see the values.
	if err := b.rewriteDataImports(); err != nil {
		return nil, err
	}

	// Rewrite asset imports (`import logo from './logo.png'`) to their hashed
	// site URL literal so both SSR HTML and hydration JS use the copied file.
	b.rewriteAssetImportRefs()

	// Rewrite `new Worker('./x.ts')` (and `new Worker(new URL(..., import.meta.url))`)
	// to the hashed /workers/... URL the worker is emitted at.
	b.rewriteWorkerRefs()

	// Rewrite `import('./x.ts')` to the hashed /chunks/... URL the module is
	// emitted at, so dynamic imports are reachable in the built site instead of
	// pointing at a stray source file.
	b.rewriteDynamicImportRefs()

	if b.actionErr != nil {
		return nil, b.actionErr
	}
	if err := b.CheckCompositionRules(); err != nil {
		return nil, err
	}

	return &Bundle{
		EntryPath:      entry,
		Modules:        b.order,
		CSS:            strings.Join(b.css, "\n"),
		CSSModules:     b.cssModules,
		CSSDeps:        sortedKeys(b.cssDeps),
		AssetFiles:     b.assets,
		WorkerFiles:    b.workers,
		WorkerEsm:      b.workerEsm,
		DynImportFiles: b.dynImports,
		Frontmatter:    b.frontmatter,
		ActionImports:  b.actionImports,
	}, nil
}

// resolveModuleImports resolves and loads every import of mod. An import no
// rule can resolve is a build error located at the import statement.
func (b *Bundler) resolveModuleImports(mod *Module) error {
	if err := b.checkRuntimeImports(mod); err != nil {
		return err
	}
	for _, imp := range mod.Imports {
		if err := checkUnsupportedStylesheet(mod, imp); err != nil {
			return err
		}
		if base, query, ok := splitImportQuery(imp); ok {
			if err := b.resolveQueryImport(mod, imp, base, query); err != nil {
				return err
			}
			continue
		}
		resolved := b.resolveImportForModule(mod.Path, imp)
		if resolved == "" {
			if err := b.unresolvedImportError(mod, imp); err != nil {
				return err
			}
			continue
		}
		if err := b.resolveModule(resolved, false); err != nil {
			return err
		}
	}
	return nil
}

func (b *Bundler) resolveModule(path string, isEntry bool) error {
	abs := path
	if !filepath.IsAbs(path) {
		abs = filepath.Join(b.root, path)
	}
	abs = filepath.Clean(abs)

	isExternalPkg := !filepath.IsAbs(path) && !b.fileExists(filepath.Join(b.root, path))

	seenKey := abs
	if isExternalPkg {
		seenKey = path
	}

	if b.seen[seenKey] {
		return nil
	}
	b.seen[seenKey] = true

	if !filepath.IsAbs(path) {
		abs = filepath.Join(b.root, path)
	}

	// Stat once so cached entries can be validated cheaply.
	var mtime time.Time
	var size int64
	statOK := false
	if !isExternalPkg {
		if s := resolver.StatPath(abs); s.Exists && !s.IsDir {
			mtime, size, statOK = s.Mtime, s.Size, true
		}
	}

	switch {
	case strings.HasSuffix(path, ".css"):
		return b.resolveCSSModule(path, abs, mtime, size, statOK)
	case strings.HasSuffix(path, ".json"):
		return b.resolveJSONModule(path, abs, mtime, size, statOK, isEntry)
	case strings.HasSuffix(path, ".md"), strings.HasSuffix(path, ".mdx"):
		return b.resolveMarkdownModule(path, abs, mtime, size, statOK, isEntry)
	}

	if !isSourceExt(path) {
		if assetExtensions[strings.ToLower(filepath.Ext(path))] {
			return b.resolveAssetModule(path, abs, mtime, size, statOK)
		}
		b.order = append(b.order, &Module{Path: path, IsExternal: true})
		return nil
	}

	// The krate client runtime is provided at runtime by the shared chunk
	// (its exports - createSignal, h, initRouter, etc. - are window globals).
	// Its AST is never referenced by the compiler's emitted code, so skip
	// reading/lexing/parsing it entirely.
	if isKrateRuntime(abs) {
		b.order = append(b.order, &Module{Path: path, IsExternal: true})
		return nil
	}

	if err := b.resolveTSXModule(path, abs, mtime, size, statOK, isEntry); err != nil {
		// A compiled dependency that Krate's TSX parser cannot read is kept as an
		// opaque external module. Its components are mounted at runtime (see the
		// build's external-component bundling), so a parser gap in a third-party
		// package must not fail the whole page.
		if !isEntry && resolver.IsCompiledDependency(abs) {
			b.order = append(b.order, &Module{Path: path, IsExternal: true})
			return nil
		}
		return err
	}
	return nil
}

// isSourceExt reports whether a path is a JS/TS source file the bundler parses.
func isSourceExt(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".tsx", ".ts", ".mts", ".cts", ".jsx", ".js", ".mjs", ".cjs":
		return true
	}
	return false
}

func (b *Bundler) resolveJSONModule(path, abs string, mtime time.Time, size int64, statOK bool, isEntry bool) error {
	cacheable := statOK && !isEntry
	if cacheable {
		if e, ok := b.cache.getProgram(abs, mtime, size); ok {
			mod := &Module{Path: path, Program: e.program, IsEntry: isEntry}
			mod.Imports = append([]string(nil), e.imports...)
			b.order = append(b.order, mod)
			if err := b.resolveModuleImports(mod); err != nil {
				return err
			}
			return nil
		}
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return fmt.Errorf("reading %s: %w", path, err)
	}
	prog, jerr := jsonToAST(data)
	if jerr != nil {
		return jsonDiagnostic(path, data, jerr)
	}
	mod := &Module{Path: path, Program: prog, IsEntry: isEntry}
	b.collectImports(prog, mod)
	b.order = append(b.order, mod)
	if cacheable {
		b.cache.putProgram(abs, &programEntry{
			mtime: mtime, size: size, program: prog, imports: append([]string(nil), mod.Imports...),
		})
	}
	return b.resolveModuleImports(mod)
}

func (b *Bundler) resolveMarkdownModule(path, abs string, mtime time.Time, size int64, statOK bool, isEntry bool) error {
	if statOK {
		if e, ok := b.cache.getProgram(abs, mtime, size); ok {
			if len(e.frontmatter) > 0 {
				b.frontmatter = e.frontmatter
			}
			mod := &Module{Path: path, Program: e.program, IsEntry: isEntry}
			mod.Imports = append([]string(nil), e.imports...)
			b.order = append(b.order, mod)
			if err := b.resolveModuleImports(mod); err != nil {
				return err
			}
			return nil
		}
	}

	data, err := os.ReadFile(abs)
	if err != nil {
		return fmt.Errorf("reading %s: %w", path, err)
	}
	src := string(data)
	mcfg := b.markdownConfig()
	mdImages := 0
	mcfg.ImageURL = markdown.LocalImageResolver(b.root, abs, b.mdBase, func(imgAbs, siteURL string) {
		b.assets[imgAbs] = siteURL
		mdImages++
	})
	compiled := markdown.Compile(src, mcfg, markdown.Options{MDX: strings.HasSuffix(abs, ".mdx")})
	for _, d := range compiled.Diags {
		if !d.Warning {
			return fmt.Errorf("%s:%d:%d: %s (hint: %s)", path, d.Line, d.Col, d.Msg, d.Hint)
		}
	}
	if len(compiled.Frontmatter) > 0 {
		b.frontmatter = compiled.Frontmatter
	}
	result := &markdown.MDXResult{Frontmatter: compiled.Frontmatter}
	tsxSource := generateMDXBundleTSX(path, compiled, mcfg)

	tokens := lexer.New(tsxSource).Tokenize()
	p := parser.New(tokens)
	p.Filename = path
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		// The parser saw generated code, so its line:col do not match the
		// markdown file. Say so, and point at the usual cause.
		return fmt.Errorf("%s: the compiled markdown is not valid JSX (line:col below refer to the generated code, not this file; check that every JSX tag is closed and nested properly, and put a blank line between a JSX tag and a block it wraps): %s", path, parser.FormatDiagnostics(errs))
	}
	RewriteReact(prog)

	mod := &Module{Path: path, Program: prog, IsEntry: isEntry}
	b.collectImports(prog, mod)
	mod.Rewritable = moduleNeedsRewrite(tsxSource, mod.Imports)
	b.order = append(b.order, mod)
	// A page that references local images is not cached: the cache keys on the
	// markdown file alone, so a changed image would keep its old hashed URL.
	if statOK && !mod.Rewritable && mdImages == 0 {
		b.cache.putProgram(abs, &programEntry{
			mtime: mtime, size: size, program: prog,
			imports: append([]string(nil), mod.Imports...), frontmatter: result.Frontmatter,
		})
	}
	return b.resolveModuleImports(mod)
}

func (b *Bundler) resolveAssetModule(path, abs string, mtime time.Time, size int64, statOK bool) error {
	if statOK {
		if e, ok := b.cache.getAsset(abs, mtime, size); ok {
			b.assets[abs] = e.url
			b.order = append(b.order, &Module{Path: path, IsExternal: true})
			return nil
		}
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return fmt.Errorf("reading %s: %w", path, err)
	}
	base := filepath.Base(path)
	ext := filepath.Ext(base)
	name := strings.TrimSuffix(base, ext)
	url := "/assets/" + name + "-" + hashBytes(data) + ext
	b.assets[abs] = url
	if statOK {
		b.cache.putAsset(abs, &assetEntry{mtime: mtime, size: size, url: url})
	}
	b.order = append(b.order, &Module{Path: path, IsExternal: true})
	return nil
}

func (b *Bundler) resolveTSXModule(path, abs string, mtime time.Time, size int64, statOK bool, isEntry bool) error {
	// Entry modules are never cached: pages/layouts mutate their own entry AST
	// (plugins may replace it; universal transforms run in place), so sharing an
	// entry program would leak mutations across builds. Shared imports are the
	// hot cost and are cached.
	cacheable := statOK && !isEntry
	if cacheable {
		if e, ok := b.cache.getProgram(abs, mtime, size); ok {
			mod := &Module{Path: path, Program: e.program, IsEntry: isEntry, SourceCode: e.source, ComponentClass: e.compClass, Diagnostics: e.diags}
			mod.Imports = append([]string(nil), e.imports...)
			b.order = append(b.order, mod)
			if err := b.resolveModuleImports(mod); err != nil {
				return err
			}
			return nil
		}
	}

	data, err := os.ReadFile(abs)
	if err != nil {
		return fmt.Errorf("reading %s: %w", path, err)
	}
	src := string(data)
	// TypeScript is stripped by esbuild first; positions in the AST and in
	// diagnostics refer to the original src. SourceCode stays the original so
	// comment directives (`// @server`) are still seen.
	prog, errs := tsstrip.Parse(path, src, b.tsOpts)
	if len(errs) > 0 {
		return &parseError{summary: parser.FormatDiagnostics(errs), errs: errs}
	}
	isolateDecoratorTemps(prog, src)
	reactDiags, err := splitReactDiags(RewriteReactDiag(prog, path, src))
	if err != nil {
		return err
	}
	if strings.Contains(filepath.ToSlash(abs), "/node_modules/") {
		// Third-party packages cannot act on these hints; keep builds quiet.
		reactDiags = nil
	}
	b.checkInlineUseServer(prog, path, src)
	_, actionChanged := b.rewriteActionImports(prog, abs)

	mod := &Module{Path: path, Program: prog, IsEntry: isEntry, SourceCode: src, Diagnostics: reactDiags}
	mod.ComponentClass = ClassifyComponent(src, path, b.serverComponents, b.runtimeComponents, b.serverDirs, b.runtimeDirs)
	if class, ok := classFromTier(prog.Tier); ok {
		mod.ComponentClass = class
	}
	if !strings.Contains(filepath.ToSlash(abs), "/node_modules/") {
		mod.Diagnostics = append(mod.Diagnostics, b.envReadDiags(path, src, prog, mod.ComponentClass)...)
		reactDiags = mod.Diagnostics
	}
	b.collectImports(prog, mod)
	// Modules a rewrite pass may mutate are parsed fresh per bundle (never
	// shared), since those rewrites are not idempotent.
	mod.Rewritable = actionChanged || moduleNeedsRewrite(src, mod.Imports)
	b.order = append(b.order, mod)
	if cacheable && !mod.Rewritable {
		b.cache.putProgram(abs, &programEntry{
			mtime: mtime, size: size, program: prog, source: src,
			imports: append([]string(nil), mod.Imports...), compClass: mod.ComponentClass,
			diags: reactDiags,
		})
	}
	return b.resolveModuleImports(mod)
}

// isKrateRuntime reports whether an absolute path points into the krate client
// runtime package (node_modules/@krate/runtime).
func isKrateRuntime(abs string) bool {
	norm := filepath.ToSlash(abs)
	return strings.Contains(norm, "/node_modules/@krate/runtime/")
}

// reactCompatSpecifier maps a React ecosystem module specifier to Krate's React
// compatibility runtime (a subpath of @krate/runtime). Empty when unrelated.
// React source imports are removed by RewriteReact; this covers imports that
// survive it, notably compiled packages importing react-dom / jsx-runtime.
func reactCompatSpecifier(imp string) string {
	switch imp {
	case "react", "react-dom", "react-dom/client":
		return "@krate/runtime/react"
	case "react/jsx-runtime", "react/jsx-dev-runtime":
		return "@krate/runtime/react/jsx-runtime"
	case "@radix-ui/react-slot":
		// shadcn's `asChild` primitive: compile against Krate's native Slot.
		return "@krate/runtime/slot"
	}
	return ""
}

func (b *Bundler) collectImports(prog *ast.Program, mod *Module) {
	for _, stmt := range prog.Body {
		if imp, ok := stmt.(*ast.ImportStmt); ok {
			src := imp.Source
			if src != "" {
				src = strings.Trim(src, "\"'")
				mod.Imports = append(mod.Imports, src)
			}
		}
		if exp, ok := stmt.(*ast.ExportStmt); ok {
			// Any re-export (`export *`, `export * as`, `export { X } from`)
			// depends on its source module.
			if exp.ReexportSource != "" {
				src := strings.Trim(exp.ReexportSource, "\"'")
				mod.Imports = append(mod.Imports, src)
			}
		}
	}
}

// CompositionError represents a violation of server/client component boundary rules.
type CompositionError struct {
	Importer      string
	Imported      string
	ImportedClass string
}

func (e *CompositionError) Error() string {
	return fmt.Sprintf("composition rule violation: client component %s cannot import %s component %s\n  Hint: Server/runtime components can only be imported by other server/runtime components, or rendered as children via props.",
		e.Importer, e.ImportedClass, e.Imported)
}

// CheckCompositionRules verifies that client components do not import server/runtime components.
// Static components can be freely imported by any tier (they produce no client JS).
func (b *Bundler) CheckCompositionRules() error {
	classMap := make(map[string]ComponentClass)
	for _, mod := range b.order {
		if mod.ComponentClass != ComponentClassClient && mod.ComponentClass != ComponentClassStatic {
			classMap[mod.Path] = mod.ComponentClass
		}
	}

	for _, mod := range b.order {
		// Client and static components cannot import server/runtime components
		if mod.ComponentClass != ComponentClassClient && mod.ComponentClass != ComponentClassStatic {
			continue
		}
		for _, imp := range mod.Imports {
			resolved := b.resolveImportForModule(mod.Path, imp)
			if resolved == "" {
				continue
			}
			if cls, ok := classMap[resolved]; ok {
				return &CompositionError{
					Importer:      mod.Path,
					Imported:      resolved,
					ImportedClass: cls.String(),
				}
			}
		}
	}
	return nil
}

// KrateRoot is the absolute path to the krate compiler's root directory.
// Set by the build system at startup to enable virtual package resolution.
var KrateRoot string

// ResolveImport resolves an import path to an absolute file path. Exported for
// consumers (e.g. the annotator's import-binding resolution) that need the same
// relative/node_modules/index resolution rules the bundler applies.
// resolveMemo is the process-wide resolution memo for callers that don't hold a
// Bundler (the annotator's alias builder). It is reset once per build
// (ResetResolveCache) so cross-build file changes aren't served from a stale
// negative entry; within a build it is shared safely by the build's parallel
// page workers.
var resolveMemo = newResolverMemo()

// ResetResolveCache clears the process-wide resolution memo. Call once per
// build, before bundling.
func ResetResolveCache() { resolveMemo.reset() }

func memoizedExists(path string) bool {
	if v, ok := resolveMemo.existsResult(path); ok {
		return v
	}
	v := resolver.Exists(path)
	resolveMemo.putExists(path, v)
	return v
}

func ResolveImport(importer, imp string) string {
	if !resolverCacheEnabled {
		return resolveImportAliased(importer, imp, resolver.Exists)
	}
	key := filepath.Dir(importer) + "\x00" + imp
	if v, ok := resolveMemo.importResult(key); ok {
		return v
	}
	v := resolveImportAliased(importer, imp, memoizedExists)
	resolveMemo.putImport(key, v)
	return v
}

// aliasRegistry holds the tsconfig path aliases of the build in progress, for
// callers of ResolveImport that hold no Bundler (the annotator follows imports
// to the module that declares a component, and `@/components` is one).
var aliasRegistry struct {
	sync.RWMutex
	aliases []pathAlias
	baseDir string
}

func registerPathAliases(aliases []pathAlias, baseDir string) {
	aliasRegistry.Lock()
	aliasRegistry.aliases, aliasRegistry.baseDir = aliases, baseDir
	aliasRegistry.Unlock()
}

func resolveImportAliased(importer, imp string, exists func(string) bool) string {
	aliasRegistry.RLock()
	aliases, base := aliasRegistry.aliases, aliasRegistry.baseDir
	aliasRegistry.RUnlock()
	if len(aliases) > 0 && base != "" && !strings.HasPrefix(imp, ".") {
		if resolved := resolvePathAlias(imp, aliases, base, exists); resolved != "" {
			return resolved
		}
	}
	return resolveImport(importer, imp, exists, nil)
}

// resolveImport resolves an import path to an absolute file path. exists is the
// filesystem probe (memoized by the caller's build cache).
func resolveImport(importer, imp string, exists func(string) bool, conds resolver.Conditions) string {
	dir := filepath.Dir(importer)

	if strings.HasPrefix(imp, ".") || strings.HasPrefix(imp, "..") {
		resolved := filepath.Clean(filepath.Join(dir, imp))

		if ts := tsSourceForJSSpecifier(resolved, exists); ts != "" {
			return ts
		}
		return resolveFileOrDir(resolved, exists)
	}

	// Resolve krate/* virtual packages (e.g., krate/components)
	if resolved := resolveKratePackage(dir, imp, exists); resolved != "" {
		return resolved
	}

	if conds == nil {
		conds = resolver.ClientConditions(false)
	}
	if strings.HasPrefix(imp, "#") {
		return resolver.PackageImport(dir, imp, conds)
	}
	return resolver.NodeModuleWith(dir, imp, conds)
}

// tsSourceForJSSpecifier maps a TypeScript ESM import written with a JS
// extension (./util.js, ./x.mjs) to the TS source beside it, as tsc does under
// moduleResolution node16/nodenext/bundler. Returns "" when path has no such
// sibling.
func tsSourceForJSSpecifier(path string, exists func(string) bool) string {
	var alts []string
	switch strings.ToLower(filepath.Ext(path)) {
	case ".js":
		alts = []string{".ts", ".tsx"}
	case ".jsx":
		alts = []string{".tsx"}
	case ".mjs":
		alts = []string{".mts"}
	case ".cjs":
		alts = []string{".cts"}
	default:
		return ""
	}
	stem := strings.TrimSuffix(path, filepath.Ext(path))
	for _, ext := range alts {
		if exists(stem + ext) {
			return stem + ext
		}
	}
	return ""
}

// resolvePathAlias tries to resolve an import using TypeScript path aliases.
// Returns the resolved absolute path, or "" if no alias matched.
func resolvePathAlias(imp string, aliases []pathAlias, tsBaseDir string, exists func(string) bool) string {
	for _, alias := range aliases {
		prefix := alias.prefix
		// Handle wildcard patterns like "@/*"
		if strings.HasSuffix(prefix, "/*") {
			prefixBase := strings.TrimSuffix(prefix, "/*")
			if imp == prefixBase || strings.HasPrefix(imp, prefixBase+"/") {
				suffix := strings.TrimPrefix(imp, prefixBase)
				if suffix == "" {
					suffix = "/"
				}
				for _, target := range alias.targets {
					// Replace * in target with the suffix
					targetPath := strings.Replace(target, "*", suffix, 1)
					if !filepath.IsAbs(targetPath) {
						targetPath = filepath.Join(tsBaseDir, targetPath)
					}
					targetPath = filepath.Clean(targetPath)

					if ts := tsSourceForJSSpecifier(targetPath, exists); ts != "" {
						return ts
					}
					if found := resolveFileOrDir(targetPath, exists); found != "" {
						return found
					}
				}
			}
		} else {
			// Exact match (no wildcard)
			if imp == prefix {
				for _, target := range alias.targets {
					targetPath := target
					if !filepath.IsAbs(targetPath) {
						targetPath = filepath.Join(tsBaseDir, targetPath)
					}
					targetPath = filepath.Clean(targetPath)
					if found := resolveFileOrDir(targetPath, exists); found != "" {
						return found
					}
				}
			}
		}
	}
	return ""
}

// sourceExtensions are the extensions tried, in order, after an extensionless
// path; sourceIndexes are the directory index files.
var (
	sourceExtensions = []string{".tsx", ".ts", ".mts", ".jsx", ".js", ".mjs", ".cjs", ".md", ".mdx", ".css", ".json"}
	sourceIndexes    = []string{"index.tsx", "index.ts", "index.mts", "index.jsx", "index.js", "index.mjs", "index.cjs", "index.md", "index.mdx"}
)

// resolveFileOrDir resolves an extensionless or exact path the way Node and tsc
// do: an exact file first, then the path plus each extension, and only then a
// directory's index file. A directory that shares its stem with a file (Button.tsx
// beside Button/style.css) therefore never shadows the file. Returns "" when
// nothing matches.
func resolveFileOrDir(path string, exists func(string) bool) string {
	isDir := false
	if exists(path) {
		if !resolver.StatPath(path).IsDir {
			return path
		}
		isDir = true
	}
	for _, ext := range sourceExtensions {
		if candidate := path + ext; exists(candidate) {
			return candidate
		}
	}
	if isDir {
		for _, index := range sourceIndexes {
			if candidate := filepath.Join(path, index); exists(candidate) {
				return candidate
			}
		}
	}
	return ""
}

// resolveKratePackage resolves krate/* and @krate/* imports via node_modules.
// Walks up from importerDir looking for node_modules/@krate/{name}/.
func resolveKratePackage(importerDir, imp string, exists func(string) bool) string {
	name := ""
	if strings.HasPrefix(imp, "krate/") {
		name = strings.TrimPrefix(imp, "krate/")
	} else if strings.HasPrefix(imp, "@krate/") {
		name = strings.TrimPrefix(imp, "@krate/")
	}
	if name == "" {
		return ""
	}

	dir := importerDir
	for {
		// Try scoped package: node_modules/@krate/{name}/
		pkgDir := filepath.Join(dir, "node_modules", "@krate", name)
		if resolver.StatPath(pkgDir).Exists {
			candidates := []string{
				filepath.Join(pkgDir, "index.tsx"),
				filepath.Join(pkgDir, "index.ts"),
				filepath.Join(pkgDir, "index.jsx"),
				filepath.Join(pkgDir, "index.js"),
			}
			for _, c := range candidates {
				if exists(c) {
					return c
				}
			}
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ""
}

func fileExists(path string) bool {
	return resolver.Exists(path)
}

// generateMDXBundleTSX creates a TSX source string from MDX content.
// It preserves import statements and renders JSX blocks as actual JSX elements,
// while markdown content is embedded as template literal strings.
func generateMDXBundleTSX(path string, compiled *markdown.Result, mcfg markdown.Config) string {
	imports := compiled.ESM
	segments := compiled.Segments

	var sb strings.Builder
	for _, imp := range imports {
		sb.WriteString(imp)
		sb.WriteString("\n")
	}
	for _, s := range segments {
		if s.JSX != "" && strings.Contains(s.JSX, "frontmatter") {
			sb.WriteString(markdown.FrontmatterDecl(compiled.Frontmatter, compiled.ESM))
			break
		}
	}
	if markdown.HasCodeSegments(segments) {
		sb.WriteString("import { Code } from \"@krate/components\";\n")
	}
	if markdown.HasAsideSegments(segments) {
		sb.WriteString("import { Aside } from \"@krate/components\";\n")
	}
	sb.WriteString("\nexport default function MDXContent() {\n")
	sb.WriteString("  return (\n")
	sb.WriteString("    <>\n")
	sb.WriteString(mdxHeadJSX(compiled.Frontmatter))
	sb.WriteString("    <div class=\"md-content\">\n")

	for _, seg := range segments {
		if seg.HTML != "" {
			jsxHTML := markdown.HTMLToJSX(seg.HTML)
			escaped := escapeBundleTemplateLit(jsxHTML)
			sb.WriteString("      <div dangerouslySetInnerHTML={{__html: `")
			sb.WriteString(escaped)
			sb.WriteString("`}} />\n")
		}
		if seg.JSX != "" {
			sb.WriteString("      ")
			sb.WriteString(seg.JSX)
			sb.WriteString("\n")
		}
		if seg.Code != nil {
			sb.WriteString("      ")
			sb.WriteString(markdown.BuildCodeJSXMeta(seg.Code.Lang, seg.Code.Meta, seg.Code.Code))
			sb.WriteString("\n")
		}
		if seg.Aside != nil {
			sb.WriteString("      ")
			sb.WriteString(markdown.BuildAsideJSX(seg.Aside))
			sb.WriteString("\n")
		}
	}

	sb.WriteString("    </div>\n")
	sb.WriteString("    </>\n")
	sb.WriteString("  );\n")
	sb.WriteString("}\n")
	return sb.String()
}

// mdxHeadJSX emits a <Head> block for the frontmatter title and description so
// markdown pages get a document <title> and meta description.
func mdxHeadJSX(fm map[string]any) string {
	title, _ := fm["title"].(string)
	desc, _ := fm["description"].(string)
	if title == "" && desc == "" {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("      <Head>\n")
	if title != "" {
		sb.WriteString("        <title>{" + escape.JSStringDQ(title) + "}</title>\n")
	}
	if desc != "" {
		sb.WriteString("        <meta name=\"description\" content={" + escape.JSStringDQ(desc) + "} />\n")
	}
	sb.WriteString("      </Head>\n")
	return sb.String()
}

func escapeBundleTemplateLit(s string) string {
	s = strings.ReplaceAll(s, "`", "\\`")
	s = strings.ReplaceAll(s, "${", "\\${")
	return s
}

// rewriteCSSModuleRefs replaces CSS module member expressions (styles.card)
// with their hashed literal values across every bundled module. Each module's
// import statement binds a local name (styles) to a *.module.css file whose
// class->hash mapping was collected during resolution; member reads on that
// local name are swapped for the resolved class name so the irtree builder and
// hydration codegen emit the real value instead of a reference to the undefined
// runtime import.
func (b *Bundler) rewriteCSSModuleRefs() {
	for _, mod := range b.order {
		if mod.Program == nil || !mod.Rewritable {
			continue
		}
		localVars := map[string]map[string]string{} // local import name -> class->hash
		for _, stmt := range mod.Program.Body {
			imp, ok := stmt.(*ast.ImportStmt)
			if !ok || imp.Default == "" {
				continue
			}
			src := strings.Trim(imp.Source, "\"'")
			if !strings.Contains(src, ".module.css") {
				continue
			}
			resolved := src
			if !filepath.IsAbs(resolved) {
				resolved = filepath.Join(b.root, resolved)
			}
			// Relative imports are resolved relative to the importing module,
			// matching resolveImportForModule, so the key matches cssModules.
			if strings.HasPrefix(src, ".") {
				modDir := filepath.Dir(mod.Path)
				resolved = filepath.Join(modDir, src)
				if !filepath.IsAbs(resolved) {
					resolved = filepath.Join(b.root, resolved)
				}
			}
			resolved = filepath.Clean(resolved)
			if info, ok := b.cssModules[resolved]; ok {
				localVars[imp.Default] = info.Mappings
			}
		}
		if len(localVars) == 0 {
			continue
		}
		rewriteCSSModuleStmts(mod.Program.Body, localVars)
	}
}

func rewriteCSSModuleStmts(stmts []ast.Stmt, locals map[string]map[string]string) {
	for _, stmt := range stmts {
		rewriteCSSModuleStmt(stmt, locals)
	}
}

func rewriteCSSModuleStmt(stmt ast.Stmt, locals map[string]map[string]string) {
	if stmt == nil {
		return
	}
	switch s := stmt.(type) {
	case *ast.ReturnStmt:
		s.Value = rewriteCSSModuleExpr(s.Value, locals)
	case *ast.VarStmt:
		for _, decl := range s.Decls {
			if decl.Init != nil {
				decl.Init = rewriteCSSModuleExpr(decl.Init, locals)
			}
		}
	case *ast.ExprStmt:
		s.Expression = rewriteCSSModuleExpr(s.Expression, locals)
	case *ast.FnDecl:
		for _, p := range s.Params {
			if p.Default != nil {
				p.Default = rewriteCSSModuleExpr(p.Default, locals)
			}
		}
		rewriteCSSModuleStmts(s.Body, locals)
	case *ast.ExportStmt:
		if s.Declaration != nil {
			rewriteCSSModuleStmt(s.Declaration, locals)
		}
	case *ast.IfStmt:
		s.Test = rewriteCSSModuleExpr(s.Test, locals)
		rewriteCSSModuleStmts(s.Consequent, locals)
		rewriteCSSModuleStmts(s.Alternate, locals)
	case *ast.BlockStmt:
		rewriteCSSModuleStmts(s.Body, locals)
	case *ast.ForStmt:
		if s.Init != nil {
			rewriteCSSModuleStmt(s.Init, locals)
		}
		if s.Test != nil {
			s.Test = rewriteCSSModuleExpr(s.Test, locals)
		}
		if s.Update != nil {
			s.Update = rewriteCSSModuleExpr(s.Update, locals)
		}
		rewriteCSSModuleStmts(s.Body, locals)
	case *ast.ForInStmt:
		if s.Left != nil {
			s.Left = rewriteCSSModuleExpr(s.Left, locals)
		}
		if s.Right != nil {
			s.Right = rewriteCSSModuleExpr(s.Right, locals)
		}
		rewriteCSSModuleStmts(s.Body, locals)
	case *ast.SwitchStmt:
		s.Discriminant = rewriteCSSModuleExpr(s.Discriminant, locals)
		for _, c := range s.Cases {
			if c.Test != nil {
				c.Test = rewriteCSSModuleExpr(c.Test, locals)
			}
			rewriteCSSModuleStmts(c.Body, locals)
		}
	case *ast.TryStmt:
		rewriteCSSModuleStmts(s.Body, locals)
		if s.Catch != nil {
			rewriteCSSModuleStmts(s.Catch.Body, locals)
		}
		rewriteCSSModuleStmts(s.Finally, locals)
	case *ast.ThrowStmt:
		s.Value = rewriteCSSModuleExpr(s.Value, locals)
	case *ast.WhileStmt:
		s.Test = rewriteCSSModuleExpr(s.Test, locals)
		rewriteCSSModuleStmts(s.Body, locals)
	case *ast.DoWhileStmt:
		s.Test = rewriteCSSModuleExpr(s.Test, locals)
		rewriteCSSModuleStmts(s.Body, locals)
	}
}

func rewriteCSSModuleExpr(expr ast.Expr, locals map[string]map[string]string) ast.Expr {
	if expr == nil {
		return nil
	}
	switch e := expr.(type) {
	case *ast.MemberExpr:
		if id, ok := e.Object.(*ast.Identifier); ok {
			if mappings, ok := locals[id.Name]; ok {
				if key, ok := cssModuleMemberKey(e); ok {
					if hash, ok := mappings[key]; ok {
						return &ast.Literal{Kind: ast.StringLit, Value: hash}
					}
				}
			}
		}
		e.Object = rewriteCSSModuleExpr(e.Object, locals)
		if e.Computed {
			e.Property = rewriteCSSModuleExpr(e.Property, locals)
		}
	case *ast.CallExpr:
		e.Callee = rewriteCSSModuleExpr(e.Callee, locals)
		for i, arg := range e.Args {
			e.Args[i] = rewriteCSSModuleExpr(arg, locals)
		}
	case *ast.ObjectExpr:
		for _, prop := range e.Properties {
			prop.Value = rewriteCSSModuleExpr(prop.Value, locals)
		}
	case *ast.ArrayExpr:
		for i, elem := range e.Elements {
			e.Elements[i] = rewriteCSSModuleExpr(elem, locals)
		}
	case *ast.BinaryExpr:
		e.Left = rewriteCSSModuleExpr(e.Left, locals)
		e.Right = rewriteCSSModuleExpr(e.Right, locals)
	case *ast.UnaryExpr:
		e.Arg = rewriteCSSModuleExpr(e.Arg, locals)
	case *ast.ConditionalExpr:
		e.Test = rewriteCSSModuleExpr(e.Test, locals)
		e.Consequent = rewriteCSSModuleExpr(e.Consequent, locals)
		e.Alternate = rewriteCSSModuleExpr(e.Alternate, locals)
	case *ast.TypeAssertion:
		e.Expr = rewriteCSSModuleExpr(e.Expr, locals)
	case *ast.ArrowFn:
		for _, p := range e.Params {
			if p.Default != nil {
				p.Default = rewriteCSSModuleExpr(p.Default, locals)
			}
		}
		rewriteCSSModuleStmts(e.Body, locals)
	case *ast.TemplateExpr:
		for i, part := range e.Parts {
			e.Parts[i] = rewriteCSSModuleExpr(part, locals)
		}
	case *ast.NewExpr:
		e.Callee = rewriteCSSModuleExpr(e.Callee, locals)
		for i, arg := range e.Args {
			e.Args[i] = rewriteCSSModuleExpr(arg, locals)
		}
	case *ast.AwaitExpr:
		e.Arg = rewriteCSSModuleExpr(e.Arg, locals)
	case *ast.DynamicImport:
		e.Arg = rewriteCSSModuleExpr(e.Arg, locals)
	case *ast.ImportMetaExpr:
	case *ast.JSXElement:
		if e.Opening != nil {
			for _, attr := range e.Opening.Attributes {
				if attr.Value != nil {
					attr.Value = rewriteCSSModuleExpr(attr.Value, locals)
				}
			}
		}
		for i, child := range e.Children {
			e.Children[i] = rewriteCSSModuleJSXChild(child, locals)
		}
	case *ast.JSXFragment:
		for i, child := range e.Children {
			e.Children[i] = rewriteCSSModuleJSXChild(child, locals)
		}
	}
	return expr
}

// cssModuleMemberKey returns the class key read by `styles.key` or
// `styles["key"]`.
func cssModuleMemberKey(e *ast.MemberExpr) (string, bool) {
	if !e.Computed {
		if prop, ok := e.Property.(*ast.Identifier); ok {
			return prop.Name, true
		}
		return "", false
	}
	if lit, ok := e.Property.(*ast.Literal); ok && lit.Kind == ast.StringLit {
		return lit.Value, true
	}
	return "", false
}

func rewriteCSSModuleJSXChild(child ast.JSXChild, locals map[string]map[string]string) ast.JSXChild {
	switch c := child.(type) {
	case *ast.JSXExprContainer:
		c.Expression = rewriteCSSModuleExpr(c.Expression, locals)
		return c
	case *ast.JSXElementChild:
		c.Element = rewriteCSSModuleExpr(c.Element, locals).(*ast.JSXElement)
		return c
	case *ast.JSXFragmentChild:
		c.Fragment = rewriteCSSModuleExpr(c.Fragment, locals).(*ast.JSXFragment)
		return c
	}
	return child
}

// rewriteAssetImportRefs replaces imported-asset binding reads with their
// hashed site URL literal across every bundled module. Each module's
// `import logo from './logo.png'` binds a local name (logo) to an asset file;
// bare reads of that local name are swapped for the resolved /assets/... URL so
// the irtree builder and hydration codegen emit the real URL instead of a
// reference to a runtime import that never exists.
func (b *Bundler) rewriteAssetImportRefs() {
	for _, mod := range b.order {
		if mod.Program == nil || !mod.Rewritable {
			continue
		}
		localVars := map[string]string{}
		for _, stmt := range mod.Program.Body {
			imp, ok := stmt.(*ast.ImportStmt)
			if !ok || imp.Default == "" {
				continue
			}
			src := strings.Trim(imp.Source, "\"'")
			// Resolve the way the import itself was resolved, so a tsconfig
			// alias (`@/assets/logo.png`) finds the same file as a relative path.
			resolved := b.resolveImportForModule(mod.Path, src)
			if resolved == "" {
				resolved = src
				if !filepath.IsAbs(resolved) {
					resolved = filepath.Join(b.root, resolved)
				}
				if strings.HasPrefix(src, ".") {
					resolved = filepath.Join(filepath.Dir(mod.Path), src)
					if !filepath.IsAbs(resolved) {
						resolved = filepath.Join(b.root, resolved)
					}
				}
			}
			resolved = filepath.Clean(resolved)
			if url, ok := b.assets[resolved]; ok {
				localVars[imp.Default] = url
			}
		}
		if len(localVars) == 0 {
			continue
		}
		rewriteAssetRefsStmts(mod.Program.Body, localVars)
	}
}

func rewriteAssetRefsStmts(stmts []ast.Stmt, locals map[string]string) {
	for _, stmt := range stmts {
		rewriteAssetRefsStmt(stmt, locals)
	}
}

func rewriteAssetRefsStmt(stmt ast.Stmt, locals map[string]string) {
	if stmt == nil {
		return
	}
	switch s := stmt.(type) {
	case *ast.ReturnStmt:
		s.Value = rewriteAssetRefsExpr(s.Value, locals)
	case *ast.VarStmt:
		for _, decl := range s.Decls {
			if decl.Init != nil {
				decl.Init = rewriteAssetRefsExpr(decl.Init, locals)
			}
		}
	case *ast.ExprStmt:
		s.Expression = rewriteAssetRefsExpr(s.Expression, locals)
	case *ast.FnDecl:
		for _, p := range s.Params {
			if p.Default != nil {
				p.Default = rewriteAssetRefsExpr(p.Default, locals)
			}
		}
		rewriteAssetRefsStmts(s.Body, locals)
	case *ast.ExportStmt:
		if s.Declaration != nil {
			rewriteAssetRefsStmt(s.Declaration, locals)
		}
	case *ast.IfStmt:
		s.Test = rewriteAssetRefsExpr(s.Test, locals)
		rewriteAssetRefsStmts(s.Consequent, locals)
		rewriteAssetRefsStmts(s.Alternate, locals)
	case *ast.ForStmt:
		if s.Init != nil {
			rewriteAssetRefsStmt(s.Init, locals)
		}
		s.Test = rewriteAssetRefsExpr(s.Test, locals)
		s.Update = rewriteAssetRefsExpr(s.Update, locals)
		rewriteAssetRefsStmts(s.Body, locals)
	case *ast.WhileStmt:
		s.Test = rewriteAssetRefsExpr(s.Test, locals)
		rewriteAssetRefsStmts(s.Body, locals)
	case *ast.DoWhileStmt:
		s.Test = rewriteAssetRefsExpr(s.Test, locals)
		rewriteAssetRefsStmts(s.Body, locals)
	case *ast.SwitchStmt:
		s.Discriminant = rewriteAssetRefsExpr(s.Discriminant, locals)
		for _, c := range s.Cases {
			c.Test = rewriteAssetRefsExpr(c.Test, locals)
			rewriteAssetRefsStmts(c.Body, locals)
		}
	case *ast.TryStmt:
		rewriteAssetRefsStmts(s.Body, locals)
		if s.Catch != nil {
			rewriteAssetRefsStmts(s.Catch.Body, locals)
		}
		rewriteAssetRefsStmts(s.Finally, locals)
	}
}

// rewriteAssetRefsExpr replaces bare Identifier reads that shadow-resolve to an
// asset import binding with the bound URL string literal.
func rewriteAssetRefsExpr(expr ast.Expr, locals map[string]string) ast.Expr {
	if expr == nil {
		return nil
	}
	switch e := expr.(type) {
	case *ast.Identifier:
		if url, ok := locals[e.Name]; ok && e.Name != "undefined" {
			return &ast.Literal{Kind: ast.StringLit, Value: url}
		}
		return e
	case *ast.MemberExpr:
		e.Object = rewriteAssetRefsExpr(e.Object, locals)
		if e.Computed {
			e.Property = rewriteAssetRefsExpr(e.Property, locals)
		}
		return e
	case *ast.CallExpr:
		e.Callee = rewriteAssetRefsExpr(e.Callee, locals)
		for i, arg := range e.Args {
			e.Args[i] = rewriteAssetRefsExpr(arg, locals)
		}
		return e
	case *ast.NewExpr:
		e.Callee = rewriteAssetRefsExpr(e.Callee, locals)
		for i, arg := range e.Args {
			e.Args[i] = rewriteAssetRefsExpr(arg, locals)
		}
		return e
	case *ast.AwaitExpr:
		e.Arg = rewriteAssetRefsExpr(e.Arg, locals)
		return e
	case *ast.DynamicImport:
		e.Arg = rewriteAssetRefsExpr(e.Arg, locals)
		return e
	case *ast.ImportMetaExpr:
		return e
	case *ast.UnaryExpr:
		e.Arg = rewriteAssetRefsExpr(e.Arg, locals)
		return e
	case *ast.BinaryExpr:
		e.Left = rewriteAssetRefsExpr(e.Left, locals)
		e.Right = rewriteAssetRefsExpr(e.Right, locals)
		return e
	case *ast.ConditionalExpr:
		e.Test = rewriteAssetRefsExpr(e.Test, locals)
		e.Consequent = rewriteAssetRefsExpr(e.Consequent, locals)
		e.Alternate = rewriteAssetRefsExpr(e.Alternate, locals)
		return e
	case *ast.TypeAssertion:
		e.Expr = rewriteAssetRefsExpr(e.Expr, locals)
		return e
	case *ast.ThisExpr:
		return e
	case *ast.ArrowFn:
		for i, p := range e.Params {
			if p.Default != nil {
				e.Params[i].Default = rewriteAssetRefsExpr(p.Default, locals)
			}
		}
		rewriteAssetRefsStmts(e.Body, locals)
		return e
	case *ast.ArrayExpr:
		for i, el := range e.Elements {
			e.Elements[i] = rewriteAssetRefsExpr(el, locals)
		}
		return e
	case *ast.ObjectExpr:
		for _, prop := range e.Properties {
			if prop.Value != nil {
				prop.Value = rewriteAssetRefsExpr(prop.Value, locals)
			}
		}
		return e
	case *ast.TemplateExpr:
		for i, p := range e.Parts {
			e.Parts[i] = rewriteAssetRefsExpr(p, locals)
		}
		return e
	case *ast.JSXElement:
		if e.Opening != nil {
			for _, attr := range e.Opening.Attributes {
				if attr.Value != nil {
					attr.Value = rewriteAssetRefsExpr(attr.Value, locals)
				}
			}
		}
		for i, child := range e.Children {
			e.Children[i] = rewriteAssetJSXChild(child, locals)
		}
		return e
	case *ast.JSXFragment:
		for i, child := range e.Children {
			e.Children[i] = rewriteAssetJSXChild(child, locals)
		}
		return e
	}
	return expr
}

func rewriteAssetJSXChild(child ast.JSXChild, locals map[string]string) ast.JSXChild {
	switch c := child.(type) {
	case *ast.JSXExprContainer:
		c.Expression = rewriteAssetRefsExpr(c.Expression, locals)
		return c
	case *ast.JSXElementChild:
		c.Element = rewriteAssetRefsExpr(c.Element, locals).(*ast.JSXElement)
		return c
	case *ast.JSXFragmentChild:
		c.Fragment = rewriteAssetRefsExpr(c.Fragment, locals).(*ast.JSXFragment)
		return c
	}
	return child
}

// workerSourceExts are the source extensions a `new Worker(...)` target may
// have. Anything else in a Worker call is left untouched.
var workerSourceExts = map[string]bool{".ts": true, ".tsx": true, ".js": true, ".jsx": true}

// rewriteWorkerRefs scans every bundled module for `new Worker(...)` /
// `Worker(...)` calls whose first argument is a string literal or
// `new URL(<literal>, import.meta.url)`. The referenced source file is
// registered as a worker (emitted at /workers/<name>-<hash>.js) and the
// argument is rewritten to its URL so both the hydration JS and the browser
// load the bundled worker instead of a stray source file.
func (b *Bundler) rewriteWorkerRefs() {
	for _, mod := range b.order {
		if mod.Program == nil || !mod.Rewritable {
			continue
		}
		for _, stmt := range mod.Program.Body {
			rewriteWorkerStmt(stmt, b, mod.Path)
		}
	}
}

func rewriteWorkerStmt(stmt ast.Stmt, b *Bundler, importer string) {
	if stmt == nil {
		return
	}
	switch s := stmt.(type) {
	case *ast.ReturnStmt:
		s.Value = rewriteWorkerExpr(s.Value, b, importer)
	case *ast.VarStmt:
		for _, decl := range s.Decls {
			if decl.Init != nil {
				decl.Init = rewriteWorkerExpr(decl.Init, b, importer)
			}
		}
	case *ast.ExprStmt:
		s.Expression = rewriteWorkerExpr(s.Expression, b, importer)
	case *ast.FnDecl:
		for _, p := range s.Params {
			if p.Default != nil {
				p.Default = rewriteWorkerExpr(p.Default, b, importer)
			}
		}
		for _, body := range s.Body {
			rewriteWorkerStmt(body, b, importer)
		}
	case *ast.ExportStmt:
		if s.Declaration != nil {
			rewriteWorkerStmt(s.Declaration, b, importer)
		}
	case *ast.IfStmt:
		s.Test = rewriteWorkerExpr(s.Test, b, importer)
		rewriteWorkerStmts(s.Consequent, b, importer)
		rewriteWorkerStmts(s.Alternate, b, importer)
	case *ast.ForStmt:
		if s.Init != nil {
			rewriteWorkerStmt(s.Init, b, importer)
		}
		s.Test = rewriteWorkerExpr(s.Test, b, importer)
		s.Update = rewriteWorkerExpr(s.Update, b, importer)
		rewriteWorkerStmts(s.Body, b, importer)
	case *ast.WhileStmt:
		s.Test = rewriteWorkerExpr(s.Test, b, importer)
		rewriteWorkerStmts(s.Body, b, importer)
	case *ast.DoWhileStmt:
		s.Test = rewriteWorkerExpr(s.Test, b, importer)
		rewriteWorkerStmts(s.Body, b, importer)
	case *ast.SwitchStmt:
		s.Discriminant = rewriteWorkerExpr(s.Discriminant, b, importer)
		for _, c := range s.Cases {
			c.Test = rewriteWorkerExpr(c.Test, b, importer)
			rewriteWorkerStmts(c.Body, b, importer)
		}
	case *ast.TryStmt:
		rewriteWorkerStmts(s.Body, b, importer)
		if s.Catch != nil {
			rewriteWorkerStmts(s.Catch.Body, b, importer)
		}
		rewriteWorkerStmts(s.Finally, b, importer)
	}
}

func rewriteWorkerStmts(stmts []ast.Stmt, b *Bundler, importer string) {
	for _, stmt := range stmts {
		rewriteWorkerStmt(stmt, b, importer)
	}
}

// rewriteWorkerExpr walks an expression, rewriting Worker constructor calls.
// It recurses through every expression position so `new Worker(...)` can appear
// anywhere (assignment, arrow body, prop value, JSX attr, etc.).
func rewriteWorkerExpr(expr ast.Expr, b *Bundler, importer string) ast.Expr {
	if expr == nil {
		return nil
	}
	switch e := expr.(type) {
	case *ast.NewExpr:
		if isWorkerCallee(e.Callee) {
			rewriteWorkerCallee(&e.Args, e.Args, b, importer)
			return e
		}
		e.Callee = rewriteWorkerExpr(e.Callee, b, importer)
		for i, arg := range e.Args {
			e.Args[i] = rewriteWorkerExpr(arg, b, importer)
		}
		return e
	case *ast.CallExpr:
		if isWorkerCallee(e.Callee) {
			rewriteWorkerCallee(&e.Args, e.Args, b, importer)
			return e
		}
		e.Callee = rewriteWorkerExpr(e.Callee, b, importer)
		for i, arg := range e.Args {
			e.Args[i] = rewriteWorkerExpr(arg, b, importer)
		}
		return e
	case *ast.Identifier:
		return e
	case *ast.MemberExpr:
		e.Object = rewriteWorkerExpr(e.Object, b, importer)
		if e.Computed {
			e.Property = rewriteWorkerExpr(e.Property, b, importer)
		}
		return e
	case *ast.AwaitExpr:
		e.Arg = rewriteWorkerExpr(e.Arg, b, importer)
		return e
	case *ast.DynamicImport:
		e.Arg = rewriteWorkerExpr(e.Arg, b, importer)
		return e
	case *ast.ImportMetaExpr:
		return e
	case *ast.UnaryExpr:
		e.Arg = rewriteWorkerExpr(e.Arg, b, importer)
		return e
	case *ast.BinaryExpr:
		e.Left = rewriteWorkerExpr(e.Left, b, importer)
		e.Right = rewriteWorkerExpr(e.Right, b, importer)
		return e
	case *ast.ConditionalExpr:
		e.Test = rewriteWorkerExpr(e.Test, b, importer)
		e.Consequent = rewriteWorkerExpr(e.Consequent, b, importer)
		e.Alternate = rewriteWorkerExpr(e.Alternate, b, importer)
		return e
	case *ast.TypeAssertion:
		e.Expr = rewriteWorkerExpr(e.Expr, b, importer)
		return e
	case *ast.ThisExpr:
		return e
	case *ast.ArrowFn:
		for i, p := range e.Params {
			if p.Default != nil {
				e.Params[i].Default = rewriteWorkerExpr(p.Default, b, importer)
			}
		}
		rewriteWorkerStmts(e.Body, b, importer)
		return e
	case *ast.ArrayExpr:
		for i, el := range e.Elements {
			e.Elements[i] = rewriteWorkerExpr(el, b, importer)
		}
		return e
	case *ast.ObjectExpr:
		for _, prop := range e.Properties {
			if prop.Value != nil {
				prop.Value = rewriteWorkerExpr(prop.Value, b, importer)
			}
		}
		return e
	case *ast.TemplateExpr:
		for i, p := range e.Parts {
			e.Parts[i] = rewriteWorkerExpr(p, b, importer)
		}
		return e
	case *ast.JSXElement:
		if e.Opening != nil {
			for _, attr := range e.Opening.Attributes {
				if attr.Value != nil {
					attr.Value = rewriteWorkerExpr(attr.Value, b, importer)
				}
			}
		}
		for i, child := range e.Children {
			e.Children[i] = rewriteWorkerJSXChild(child, b, importer)
		}
		return e
	case *ast.JSXFragment:
		for i, child := range e.Children {
			e.Children[i] = rewriteWorkerJSXChild(child, b, importer)
		}
		return e
	}
	return expr
}

func rewriteWorkerJSXChild(child ast.JSXChild, b *Bundler, importer string) ast.JSXChild {
	switch c := child.(type) {
	case *ast.JSXExprContainer:
		c.Expression = rewriteWorkerExpr(c.Expression, b, importer)
		return c
	case *ast.JSXElementChild:
		c.Element = rewriteWorkerExpr(c.Element, b, importer).(*ast.JSXElement)
		return c
	case *ast.JSXFragmentChild:
		c.Fragment = rewriteWorkerExpr(c.Fragment, b, importer).(*ast.JSXFragment)
		return c
	}
	return child
}

func isWorkerCallee(callee ast.Expr) bool {
	switch c := callee.(type) {
	case *ast.Identifier:
		return c.Name == "Worker"
	case *ast.MemberExpr:
		if c.Computed {
			return false
		}
		if prop, ok := c.Property.(*ast.Identifier); ok && prop.Name == "Worker" {
			if obj, ok := c.Object.(*ast.Identifier); ok {
				return obj.Name == "window" || obj.Name == "self" || obj.Name == "globalThis"
			}
		}
	}
	return false
}

// rewriteWorkerCallee resolves the first Worker argument to a registered worker
// URL and replaces the argument with it.
func rewriteWorkerCallee(args *[]ast.Expr, original []ast.Expr, b *Bundler, importer string) {
	if len(original) == 0 {
		return
	}
	candidate := ""
	switch arg := original[0].(type) {
	case *ast.Literal:
		if arg.Kind == ast.StringLit {
			candidate = arg.Value
		}
	case *ast.NewExpr:
		if urlCallee, ok := arg.Callee.(*ast.Identifier); ok && urlCallee.Name == "URL" && len(arg.Args) > 0 {
			if lit, ok := arg.Args[0].(*ast.Literal); ok && lit.Kind == ast.StringLit {
				candidate = lit.Value
			}
		}
	}
	if candidate == "" {
		return
	}
	abs := resolveWorkerPath(importer, candidate, b.root)
	if abs == "" {
		return
	}
	ext := strings.ToLower(filepath.Ext(abs))
	if !workerSourceExts[ext] {
		return
	}
	url, ok := b.workers[abs]
	if !ok {
		data, err := os.ReadFile(abs)
		if err != nil {
			return
		}
		base := filepath.Base(abs)
		name := strings.TrimSuffix(base, ext)
		url = "/workers/" + name + "-" + hashBytes(data) + ".js"
		b.workers[abs] = url
	}
	if workerOptionsModuleStyle(original) {
		b.workerEsm[abs] = true
	}
	(*args)[0] = &ast.Literal{Kind: ast.StringLit, Value: url}
}

// workerOptionsModuleStyle reports whether the Worker options object opts into
// `{ type: 'module' }` (an ES-module worker).
func workerOptionsModuleStyle(args []ast.Expr) bool {
	if len(args) < 2 {
		return false
	}
	obj, ok := args[1].(*ast.ObjectExpr)
	if !ok {
		return false
	}
	for _, prop := range obj.Properties {
		if prop.Spread || prop.Shorthand {
			continue
		}
		if prop.Key != "type" {
			continue
		}
		if lit, ok := prop.Value.(*ast.Literal); ok && lit.Kind == ast.StringLit && lit.Value == "module" {
			return true
		}
	}
	return false
}

// resolveWorkerPath resolves a Worker argument target to an absolute source
// path, appending common source extensions when the literal has none.
func resolveWorkerPath(importer, candidate, root string) string {
	abs := candidate
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(filepath.Dir(importer), abs)
		if !filepath.IsAbs(abs) {
			abs = filepath.Join(root, abs)
		}
	}
	abs = filepath.Clean(abs)
	if fileExists(abs) {
		return abs
	}
	for _, ext := range []string{".ts", ".tsx", ".js", ".jsx"} {
		if fileExists(abs + ext) {
			return abs + ext
		}
	}
	return ""
}

// dynamicImportSourceExts are the source extensions a dynamic `import()`
// target may have. Anything else (css, assets, json, bare packages) is left
// untouched so we never rewrite an import to a chunk we can't build.
var dynamicImportSourceExts = map[string]bool{".ts": true, ".tsx": true, ".js": true, ".jsx": true}

// rewriteDynamicImportRefs scans every bundled module for a dynamic
// `import('<spec>')` whose specifier resolves to a real source file. The target
// is registered as a chunk (emitted at /chunks/<name>-<hash>.js) and the import
// argument is rewritten to that URL, so the built site fetches a bundled module
// instead of a stray source file (which would 404 or be unreachable).
func (b *Bundler) rewriteDynamicImportRefs() {
	for _, mod := range b.order {
		if mod.Program == nil || !mod.Rewritable {
			continue
		}
		for _, stmt := range mod.Program.Body {
			rewriteDynamicImportStmt(stmt, b, mod.Path)
		}
	}
}

func rewriteDynamicImportStmt(stmt ast.Stmt, b *Bundler, importer string) {
	if stmt == nil {
		return
	}
	switch s := stmt.(type) {
	case *ast.ReturnStmt:
		s.Value = rewriteDynamicImportExpr(s.Value, b, importer)
	case *ast.VarStmt:
		for _, decl := range s.Decls {
			if decl.Init != nil {
				decl.Init = rewriteDynamicImportExpr(decl.Init, b, importer)
			}
		}
	case *ast.ExprStmt:
		s.Expression = rewriteDynamicImportExpr(s.Expression, b, importer)
	case *ast.FnDecl:
		for _, p := range s.Params {
			if p.Default != nil {
				p.Default = rewriteDynamicImportExpr(p.Default, b, importer)
			}
		}
		for _, body := range s.Body {
			rewriteDynamicImportStmt(body, b, importer)
		}
	case *ast.ExportStmt:
		if s.Declaration != nil {
			rewriteDynamicImportStmt(s.Declaration, b, importer)
		}
	case *ast.IfStmt:
		s.Test = rewriteDynamicImportExpr(s.Test, b, importer)
		rewriteDynamicImportStmts(s.Consequent, b, importer)
		rewriteDynamicImportStmts(s.Alternate, b, importer)
	case *ast.BlockStmt:
		rewriteDynamicImportStmts(s.Body, b, importer)
	case *ast.ForStmt:
		if s.Init != nil {
			rewriteDynamicImportStmt(s.Init, b, importer)
		}
		s.Test = rewriteDynamicImportExpr(s.Test, b, importer)
		s.Update = rewriteDynamicImportExpr(s.Update, b, importer)
		rewriteDynamicImportStmts(s.Body, b, importer)
	case *ast.WhileStmt:
		s.Test = rewriteDynamicImportExpr(s.Test, b, importer)
		rewriteDynamicImportStmts(s.Body, b, importer)
	case *ast.DoWhileStmt:
		s.Test = rewriteDynamicImportExpr(s.Test, b, importer)
		rewriteDynamicImportStmts(s.Body, b, importer)
	case *ast.SwitchStmt:
		s.Discriminant = rewriteDynamicImportExpr(s.Discriminant, b, importer)
		for _, c := range s.Cases {
			c.Test = rewriteDynamicImportExpr(c.Test, b, importer)
			rewriteDynamicImportStmts(c.Body, b, importer)
		}
	case *ast.TryStmt:
		rewriteDynamicImportStmts(s.Body, b, importer)
		if s.Catch != nil {
			rewriteDynamicImportStmts(s.Catch.Body, b, importer)
		}
		rewriteDynamicImportStmts(s.Finally, b, importer)
	case *ast.ThrowStmt:
		s.Value = rewriteDynamicImportExpr(s.Value, b, importer)
	}
}

func rewriteDynamicImportStmts(stmts []ast.Stmt, b *Bundler, importer string) {
	for _, stmt := range stmts {
		rewriteDynamicImportStmt(stmt, b, importer)
	}
}

// rewriteDynamicImportExpr walks an expression, rewriting `import(<literal>)`
// arguments. It recurses through every expression position so a dynamic import
// can appear anywhere (effect bodies, event handlers, JSX expr containers, ...).
func rewriteDynamicImportExpr(expr ast.Expr, b *Bundler, importer string) ast.Expr {
	if expr == nil {
		return nil
	}
	switch e := expr.(type) {
	case *ast.DynamicImport:
		rewriteDynamicImportCallee(&e.Arg, b, importer)
		return e
	case *ast.Identifier:
		return e
	case *ast.MemberExpr:
		e.Object = rewriteDynamicImportExpr(e.Object, b, importer)
		if e.Computed {
			e.Property = rewriteDynamicImportExpr(e.Property, b, importer)
		}
		return e
	case *ast.CallExpr:
		e.Callee = rewriteDynamicImportExpr(e.Callee, b, importer)
		for i, arg := range e.Args {
			e.Args[i] = rewriteDynamicImportExpr(arg, b, importer)
		}
		return e
	case *ast.NewExpr:
		e.Callee = rewriteDynamicImportExpr(e.Callee, b, importer)
		for i, arg := range e.Args {
			e.Args[i] = rewriteDynamicImportExpr(arg, b, importer)
		}
		return e
	case *ast.AwaitExpr:
		e.Arg = rewriteDynamicImportExpr(e.Arg, b, importer)
		return e
	case *ast.UnaryExpr:
		e.Arg = rewriteDynamicImportExpr(e.Arg, b, importer)
		return e
	case *ast.BinaryExpr:
		e.Left = rewriteDynamicImportExpr(e.Left, b, importer)
		e.Right = rewriteDynamicImportExpr(e.Right, b, importer)
		return e
	case *ast.ConditionalExpr:
		e.Test = rewriteDynamicImportExpr(e.Test, b, importer)
		e.Consequent = rewriteDynamicImportExpr(e.Consequent, b, importer)
		e.Alternate = rewriteDynamicImportExpr(e.Alternate, b, importer)
		return e
	case *ast.TypeAssertion:
		e.Expr = rewriteDynamicImportExpr(e.Expr, b, importer)
		return e
	case *ast.ThisExpr:
		return e
	case *ast.ArrowFn:
		for i, p := range e.Params {
			if p.Default != nil {
				e.Params[i].Default = rewriteDynamicImportExpr(p.Default, b, importer)
			}
		}
		rewriteDynamicImportStmts(e.Body, b, importer)
		return e
	case *ast.ArrayExpr:
		for i, el := range e.Elements {
			e.Elements[i] = rewriteDynamicImportExpr(el, b, importer)
		}
		return e
	case *ast.ObjectExpr:
		for _, prop := range e.Properties {
			if prop.Value != nil {
				prop.Value = rewriteDynamicImportExpr(prop.Value, b, importer)
			}
		}
		return e
	case *ast.TemplateExpr:
		for i, p := range e.Parts {
			e.Parts[i] = rewriteDynamicImportExpr(p, b, importer)
		}
		return e
	case *ast.JSXElement:
		if e.Opening != nil {
			for _, attr := range e.Opening.Attributes {
				if attr.Value != nil {
					attr.Value = rewriteDynamicImportExpr(attr.Value, b, importer)
				}
			}
		}
		for i, child := range e.Children {
			e.Children[i] = rewriteDynamicImportJSXChild(child, b, importer)
		}
		return e
	case *ast.JSXFragment:
		for i, child := range e.Children {
			e.Children[i] = rewriteDynamicImportJSXChild(child, b, importer)
		}
		return e
	}
	return expr
}

func rewriteDynamicImportJSXChild(child ast.JSXChild, b *Bundler, importer string) ast.JSXChild {
	switch c := child.(type) {
	case *ast.JSXExprContainer:
		c.Expression = rewriteDynamicImportExpr(c.Expression, b, importer)
		return c
	case *ast.JSXElementChild:
		c.Element = rewriteDynamicImportExpr(c.Element, b, importer).(*ast.JSXElement)
		return c
	case *ast.JSXFragmentChild:
		c.Fragment = rewriteDynamicImportExpr(c.Fragment, b, importer).(*ast.JSXFragment)
		return c
	}
	return child
}

// rewriteDynamicImportCallee resolves a dynamic import target to a registered
// chunk URL and replaces the argument with the URL literal.
func rewriteDynamicImportCallee(arg *ast.Expr, b *Bundler, importer string) {
	if arg == nil || *arg == nil {
		return
	}
	candidate := ""
	switch a := (*arg).(type) {
	case *ast.Literal:
		if a.Kind == ast.StringLit {
			candidate = a.Value
		}
	case *ast.TemplateExpr:
		if len(a.Parts) == 1 {
			if lit, ok := a.Parts[0].(*ast.Literal); ok && lit.Kind == ast.StringLit {
				candidate = lit.Value
			}
		}
	}
	if candidate == "" {
		return
	}
	abs := resolveDynamicImportPath(importer, candidate, b.root)
	if abs == "" {
		return
	}
	ext := strings.ToLower(filepath.Ext(abs))
	if !dynamicImportSourceExts[ext] {
		return
	}
	base := filepath.Base(abs)
	name := strings.TrimSuffix(base, filepath.Ext(base))
	data, err := os.ReadFile(abs)
	if err != nil {
		return
	}
	url := "/chunks/" + name + "-" + hashBytes(data) + ".js"
	if existing, ok := b.dynImports[abs]; ok {
		url = existing
	} else {
		b.dynImports[abs] = url
	}
	// Keep the argument literal for single-expression imports so
	// `import(...)` still reads as a module specifier; a plain string literal
	// is what we produce for worker/asset rewrites as well.
	*arg = &ast.Literal{Kind: ast.StringLit, Value: url}
}

// resolveDynamicImportPath resolves a dynamic import specifier to an absolute
// source path, appending common source extensions when the literal has none.
func resolveDynamicImportPath(importer, candidate, root string) string {
	abs := candidate
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(filepath.Dir(importer), abs)
		if !filepath.IsAbs(abs) {
			abs = filepath.Join(root, abs)
		}
	}
	abs = filepath.Clean(abs)
	if fileExists(abs) {
		return abs
	}
	for _, ext := range []string{".ts", ".tsx", ".js", ".jsx"} {
		if fileExists(abs + ext) {
			return abs + ext
		}
	}
	return ""
}

// hashBytes returns a short content hash used to fingerprint copied assets.
func hashBytes(data []byte) string {
	h := fnv.New32a()
	h.Write(data)
	v := h.Sum32()
	const chars = "abcdefghijklmnopqrstuvwxyz0123456789"
	var buf [6]byte
	for i := 5; i >= 0; i-- {
		buf[i] = chars[v%36]
		v /= 36
	}
	return string(buf[:])
}
