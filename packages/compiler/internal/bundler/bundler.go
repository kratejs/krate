package bundler

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/css"
	"github.com/kratejs/krate/packages/compiler/internal/lexer"
	"github.com/kratejs/krate/packages/compiler/internal/markdown"
	"github.com/kratejs/krate/packages/compiler/internal/parser"
	"github.com/kratejs/krate/packages/compiler/internal/resolver"
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
	}
	return false
}

type CSSModuleInfo struct {
	ResolvedPath string
	ScopedCSS    string
	Mappings     map[string]string
}

type Bundle struct {
	EntryPath      string
	Modules        []*Module
	CSS            string
	CSSModules     map[string]*CSSModuleInfo
	AssetFiles     map[string]string // resolved source path -> hashed site URL (/assets/...)
	WorkerFiles    map[string]string // worker source path -> hashed site URL (/workers/...)
	WorkerEsm      map[string]bool   // worker source path -> true when built with `type: 'module'`
	DynImportFiles map[string]string // dynamic-import source path -> hashed site URL (/chunks/...)
	Frontmatter    map[string]any    // .mdx frontmatter, if any
}

type Bundler struct {
	root              string
	seen              map[string]bool
	order             []*Module
	css               []string
	cssModules        map[string]*CSSModuleInfo
	assets            map[string]string // resolved source path -> hashed site URL
	workers           map[string]string // worker source path -> hashed site URL (/workers/...)
	workerEsm         map[string]bool   // worker source path -> built as ES module
	dynImports        map[string]string // dynamic-import source path -> hashed site URL (/chunks/...)
	frontmatter       map[string]any    // from .mdx frontmatter
	pathAliases       []pathAlias
	tsBaseDir         string
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
}

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
}

// SetServerComponents configures the server component name lists and directory
// lists for classification. Components in serverDirs are treated as @server,
// components in runtimeDirs are treated as @runtime.
func (b *Bundler) SetServerComponents(server []string, runtime []string, serverDirs []string, runtimeDirs []string) {
	b.serverComponents = server
	b.runtimeComponents = runtime
	b.serverDirs = serverDirs
	b.runtimeDirs = runtimeDirs
}

// resolveImportForModule resolves an import, checking virtual modules and path
// aliases first.
func (b *Bundler) resolveImportForModule(importer, imp string) string {
	if b.virtualModules != nil {
		if path, ok := b.virtualModules[imp]; ok {
			return path
		}
	}
	resolved := resolveImport(importer, imp)
	if resolved != "" {
		return resolved
	}
	if len(b.pathAliases) > 0 && b.tsBaseDir != "" {
		resolved = resolvePathAlias(imp, b.pathAliases, b.tsBaseDir)
		if resolved != "" {
			return resolved
		}
		// Fallback: tsconfig paths targets are normally relative to baseUrl,
		// but many projects declare them relative to the project root (e.g.
		// `@/*: ["./src/*"]`). When baseUrl resolution misses, try the root the
		// tsconfig lives in.
		if b.root != "" && b.root != b.tsBaseDir {
			resolved = resolvePathAlias(imp, b.pathAliases, b.root)
			if resolved != "" {
				return resolved
			}
		}
	}
	return ""
}

func (b *Bundler) Bundle(entry string) (*Bundle, error) {
	b.seen = make(map[string]bool)
	b.order = nil
	b.css = nil
	b.cssModules = make(map[string]*CSSModuleInfo)
	b.frontmatter = nil

	err := b.resolveModule(entry, true)
	if err != nil {
		return nil, err
	}

	// Rewrite CSS module member expressions (styles.card) to their hashed
	// literal values now that all modules are resolved. Without this, both the
	// SSR HTML and the hydration JS reference the undefined `styles` import at
	// runtime (class bindings degrade to data-kattr-class markers that throw
	// "styles is not defined").
	b.rewriteCSSModuleRefs()

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

	if err := b.CheckCompositionRules(); err != nil {
		return nil, err
	}

	return &Bundle{
		EntryPath:      entry,
		Modules:        b.order,
		CSS:            strings.Join(b.css, "\n"),
		CSSModules:     b.cssModules,
		AssetFiles:     b.assets,
		WorkerFiles:    b.workers,
		WorkerEsm:      b.workerEsm,
		DynImportFiles: b.dynImports,
		Frontmatter:    b.frontmatter,
	}, nil
}

func (b *Bundler) resolveModule(path string, isEntry bool) error {
	abs := path
	if !filepath.IsAbs(path) {
		abs = filepath.Join(b.root, path)
	}
	abs = filepath.Clean(abs)

	isExternalPkg := !filepath.IsAbs(path) && !fileExists(filepath.Join(b.root, path))

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
		if fi, err := os.Stat(abs); err == nil && !fi.IsDir() {
			mtime, size, statOK = fi.ModTime(), fi.Size(), true
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

	return b.resolveTSXModule(path, abs, mtime, size, statOK, isEntry)
}

// isSourceExt reports whether a path is a JS/TS source file the bundler parses.
func isSourceExt(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".tsx", ".ts", ".mts", ".jsx", ".js", ".mjs", ".cjs":
		return true
	}
	return false
}

func (b *Bundler) resolveCSSModule(path, abs string, mtime time.Time, size int64, statOK bool) error {
	if statOK {
		if e, ok := b.cache.getCSS(abs, mtime, size); ok {
			if e.isModule {
				b.css = append(b.css, e.css)
				b.cssModules[abs] = &CSSModuleInfo{ResolvedPath: abs, ScopedCSS: e.css, Mappings: e.mapping}
			} else {
				b.css = append(b.css, e.css)
			}
			for k, v := range e.assets {
				b.assets[k] = v
			}
			b.order = append(b.order, &Module{Path: path, IsCSS: true})
			return nil
		}
	}

	data, err := os.ReadFile(abs)
	if err != nil {
		return fmt.Errorf("reading %s: %w", path, err)
	}

	// Inline @imports here, relative to THIS file's directory. Inlining later on
	// the concatenated page CSS loses per-file origin, so a first-level
	// `@import "./tokens.css"` would resolve against the wrong directory.
	inlined := css.InlineImports(string(data), filepath.Dir(abs))
	// Resolve and hash `url(...)` assets relative to this sheet so CSS images
	// and fonts are content-addressed and copied like JS-imported assets.
	inlined, assets := b.rewriteCSSUrls(inlined, filepath.Dir(abs))

	isModule := strings.Contains(path, ".module.")
	entry := &cssEntry{mtime: mtime, size: size, isModule: isModule, assets: assets}
	if isModule {
		scopedCSS, mapping, err := css.ProcessModule(abs, inlined)
		if err != nil {
			return fmt.Errorf("processing css module %s: %w", path, err)
		}
		b.css = append(b.css, scopedCSS)
		b.cssModules[abs] = &CSSModuleInfo{ResolvedPath: abs, ScopedCSS: scopedCSS, Mappings: mapping}
		entry.css = scopedCSS
		entry.mapping = mapping
	} else {
		b.css = append(b.css, inlined)
		entry.css = inlined
	}
	if statOK {
		b.cache.putCSS(abs, entry)
	}
	b.order = append(b.order, &Module{Path: path, IsCSS: true})
	return nil
}

func (b *Bundler) resolveJSONModule(path, abs string, mtime time.Time, size int64, statOK bool, isEntry bool) error {
	cacheable := statOK && !isEntry
	if cacheable {
		if e, ok := b.cache.getProgram(abs, mtime, size); ok {
			mod := &Module{Path: path, Program: e.program, IsEntry: isEntry}
			mod.Imports = append([]string(nil), e.imports...)
			b.order = append(b.order, mod)
			for _, imp := range mod.Imports {
				resolved := b.resolveImportForModule(path, imp)
				if resolved != "" {
					if err := b.resolveModule(resolved, false); err != nil {
						return err
					}
				}
			}
			return nil
		}
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return fmt.Errorf("reading %s: %w", path, err)
	}
	prog := jsonToAST(data)
	mod := &Module{Path: path, Program: prog, IsEntry: isEntry}
	b.collectImports(prog, mod)
	b.order = append(b.order, mod)
	if cacheable {
		b.cache.putProgram(abs, &programEntry{
			mtime: mtime, size: size, program: prog, imports: append([]string(nil), mod.Imports...),
		})
	}
	for _, imp := range mod.Imports {
		resolved := b.resolveImportForModule(path, imp)
		if resolved != "" {
			if err := b.resolveModule(resolved, false); err != nil {
				return err
			}
		}
	}
	return nil
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
			for _, imp := range mod.Imports {
				resolved := b.resolveImportForModule(path, imp)
				if resolved != "" {
					if err := b.resolveModule(resolved, false); err != nil {
						return err
					}
				}
			}
			return nil
		}
	}

	data, err := os.ReadFile(abs)
	if err != nil {
		return fmt.Errorf("reading %s: %w", path, err)
	}
	src := string(data)
	mcfg := markdown.DefaultConfig()
	result := markdown.ParseMDX(src, mcfg)
	if len(result.Frontmatter) > 0 {
		b.frontmatter = result.Frontmatter
	}
	tsxSource := generateMDXBundleTSX(path, src, result, mcfg)

	tokens := lexer.New(tsxSource).Tokenize()
	p := parser.New(tokens)
	p.Filename = path
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		return fmt.Errorf("%s: %s", path, parser.FormatDiagnostics(errs))
	}
	RewriteReact(prog)

	mod := &Module{Path: path, Program: prog, IsEntry: isEntry}
	b.collectImports(prog, mod)
	mod.Rewritable = moduleNeedsRewrite(tsxSource, mod.Imports)
	b.order = append(b.order, mod)
	if statOK && !mod.Rewritable {
		b.cache.putProgram(abs, &programEntry{
			mtime: mtime, size: size, program: prog,
			imports: append([]string(nil), mod.Imports...), frontmatter: result.Frontmatter,
		})
	}
	for _, imp := range mod.Imports {
		resolved := b.resolveImportForModule(path, imp)
		if resolved != "" {
			if err := b.resolveModule(resolved, false); err != nil {
				return err
			}
		}
	}
	return nil
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
			mod := &Module{Path: path, Program: e.program, IsEntry: isEntry, SourceCode: e.source, ComponentClass: e.compClass}
			mod.Imports = append([]string(nil), e.imports...)
			b.order = append(b.order, mod)
			for _, imp := range mod.Imports {
				resolved := b.resolveImportForModule(path, imp)
				if resolved != "" {
					if err := b.resolveModule(resolved, false); err != nil {
						return err
					}
				}
			}
			return nil
		}
	}

	data, err := os.ReadFile(abs)
	if err != nil {
		return fmt.Errorf("reading %s: %w", path, err)
	}
	src := string(data)
	tokens := lexer.New(src).Tokenize()
	p := parser.New(tokens)
	p.Filename = path
	p.SetSource(src)
	prog := p.ParseProgram()
	if len(p.Errors()) > 0 {
		return fmt.Errorf("%s", parser.FormatDiagnostics(p.Errors()))
	}
	RewriteReact(prog)

	mod := &Module{Path: path, Program: prog, IsEntry: isEntry, SourceCode: src}
	mod.ComponentClass = ClassifyComponent(src, path, b.serverComponents, b.runtimeComponents, b.serverDirs, b.runtimeDirs)
	b.collectImports(prog, mod)
	// Modules a rewrite pass may mutate are parsed fresh per bundle (never
	// shared), since those rewrites are not idempotent.
	mod.Rewritable = moduleNeedsRewrite(src, mod.Imports)
	b.order = append(b.order, mod)
	if cacheable && !mod.Rewritable {
		b.cache.putProgram(abs, &programEntry{
			mtime: mtime, size: size, program: prog, source: src,
			imports: append([]string(nil), mod.Imports...), compClass: mod.ComponentClass,
		})
	}
	for _, imp := range mod.Imports {
		resolved := b.resolveImportForModule(path, imp)
		if resolved != "" {
			if err := b.resolveModule(resolved, false); err != nil {
				return err
			}
		}
	}
	return nil
}

// isKrateRuntime reports whether an absolute path points into the krate client
// runtime package (node_modules/@krate/runtime).
func isKrateRuntime(abs string) bool {
	norm := filepath.ToSlash(abs)
	return strings.Contains(norm, "/node_modules/@krate/runtime/")
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
			// A star or namespace re-export depends on its source module.
			if (exp.StarReexport || exp.Namespace != "") && exp.ReexportSource != "" {
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
func ResolveImport(importer, imp string) string {
	return resolveImport(importer, imp)
}

// resolveImport resolves an import path to an absolute file path.
func resolveImport(importer, imp string) string {
	dir := filepath.Dir(importer)

	if strings.HasPrefix(imp, ".") || strings.HasPrefix(imp, "..") {
		resolved := filepath.Clean(filepath.Join(dir, imp))

		if fileExists(resolved) {
			info, err := os.Stat(resolved)
			if err == nil && info.IsDir() {
				for _, index := range []string{"index.tsx", "index.ts", "index.mts", "index.jsx", "index.js", "index.mjs", "index.cjs", "index.md", "index.mdx"} {
					candidate := filepath.Join(resolved, index)
					if fileExists(candidate) {
						return candidate
					}
				}
			}
			return resolved
		}

		extensions := []string{".tsx", ".ts", ".mts", ".jsx", ".js", ".mjs", ".cjs", ".md", ".mdx", ".css", ".json"}
		for _, ext := range extensions {
			candidate := resolved + ext
			if fileExists(candidate) {
				return candidate
			}
		}
		return ""
	}

	// Resolve krate/* virtual packages (e.g., krate/components)
	if resolved := resolveKratePackage(dir, imp); resolved != "" {
		return resolved
	}

	return resolver.NodeModule(dir, imp)
}

// resolvePathAlias tries to resolve an import using TypeScript path aliases.
// Returns the resolved absolute path, or "" if no alias matched.
func resolvePathAlias(imp string, aliases []pathAlias, tsBaseDir string) string {
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

					if fileExists(targetPath) {
						info, err := os.Stat(targetPath)
						if err == nil && info.IsDir() {
							for _, index := range []string{"index.tsx", "index.ts", "index.jsx", "index.js"} {
								candidate := filepath.Join(targetPath, index)
								if fileExists(candidate) {
									return candidate
								}
							}
						}
						return targetPath
					}

					// Try with extensions
					extensions := []string{".tsx", ".ts", ".mts", ".jsx", ".js", ".mjs", ".cjs", ".css", ".json"}
					for _, ext := range extensions {
						candidate := targetPath + ext
						if fileExists(candidate) {
							return candidate
						}
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
					if fileExists(targetPath) {
						return targetPath
					}
				}
			}
		}
	}
	return ""
}

// resolveKratePackage resolves krate/* and @krate/* imports via node_modules.
// Walks up from importerDir looking for node_modules/@krate/{name}/.
func resolveKratePackage(importerDir, imp string) string {
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
		if _, err := os.Stat(pkgDir); err == nil {
			candidates := []string{
				filepath.Join(pkgDir, "index.tsx"),
				filepath.Join(pkgDir, "index.ts"),
				filepath.Join(pkgDir, "index.jsx"),
				filepath.Join(pkgDir, "index.js"),
			}
			for _, c := range candidates {
				if fileExists(c) {
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
func generateMDXBundleTSX(path, src string, result *markdown.MDXResult, mcfg markdown.Config) string {
	imports := markdown.ExtractImports(src)
	_, segments := markdown.ParseMDXSegments(src, mcfg)

	var sb strings.Builder
	for _, imp := range imports {
		sb.WriteString(imp)
		sb.WriteString("\n")
	}
	if markdown.HasCodeSegments(segments) {
		sb.WriteString("import { Code } from \"@krate/components\";\n")
	}
	if markdown.HasAsideSegments(segments) {
		sb.WriteString("import { Aside } from \"@krate/components\";\n")
	}
	sb.WriteString("\nexport default function MDXContent() {\n")
	sb.WriteString("  return (\n")
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
			sb.WriteString(markdown.BuildCodeJSX(seg.Code.Lang, seg.Code.Code))
			sb.WriteString("\n")
		}
		if seg.Aside != nil {
			sb.WriteString("      ")
			sb.WriteString(markdown.BuildAsideJSX(seg.Aside))
			sb.WriteString("\n")
		}
	}

	sb.WriteString("    </div>\n")
	sb.WriteString("  );\n")
	sb.WriteString("}\n")
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
				if prop, ok := e.Property.(*ast.Identifier); ok && !e.Computed {
					if hash, ok := mappings[prop.Name]; ok {
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

// cssURLRe matches `url(...)` references in CSS (quoted or bare).
var cssURLRe = regexp.MustCompile(`url\(\s*(['"]?)([^'")]+)(['"]?)\s*\)`)

// rewriteCSSUrls resolves relative `url(...)` references in a stylesheet
// against the sheet's directory, registers each asset in the bundle's asset map
// (content-hashed, served from /assets/), and rewrites the URL. External URLs,
// data URIs, fragments, `var(...)` and root-absolute paths are left untouched.
// rewriteCSSUrls resolves and content-hashes `url(...)` assets relative to the
// sheet's directory, returning the rewritten CSS and the assets it referenced
// (source path -> hashed site URL) so the mapping can be cached and replayed.
func (b *Bundler) rewriteCSSUrls(cssText, cssDir string) (string, map[string]string) {
	used := make(map[string]string)
	out := cssURLRe.ReplaceAllStringFunc(cssText, func(m string) string {
		sub := cssURLRe.FindStringSubmatch(m)
		if len(sub) < 4 {
			return m
		}
		quote := sub[1]
		ref := strings.TrimSpace(sub[2])
		if ref == "" || isExternalCSSURL(ref) {
			return m
		}
		frag := ""
		if i := strings.IndexAny(ref, "?#"); i >= 0 {
			frag = ref[i:]
			ref = ref[:i]
		}
		if ref == "" {
			return m
		}
		abs := filepath.Clean(filepath.Join(cssDir, filepath.FromSlash(ref)))
		if rel, err := filepath.Rel(b.root, abs); err != nil || rel == ".." ||
			strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return m
		}
		if _, err := os.Stat(abs); err != nil {
			return m
		}
		url, ok := b.assets[abs]
		if !ok {
			data, err := os.ReadFile(abs)
			if err != nil {
				return m
			}
			base := filepath.Base(abs)
			ext := filepath.Ext(base)
			name := strings.TrimSuffix(base, ext)
			url = "/assets/" + name + "-" + hashBytes(data) + ext
			b.assets[abs] = url
		}
		used[abs] = url
		return "url(" + quote + url + frag + quote + ")"
	})
	return out, used
}

// isExternalCSSURL reports whether a CSS url() target is not a project-relative
// file path.
func isExternalCSSURL(ref string) bool {
	l := strings.ToLower(ref)
	for _, p := range []string{"http://", "https://", "//", "data:", "blob:", "#", "var("} {
		if strings.HasPrefix(l, p) {
			return true
		}
	}
	return false
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
			resolved := src
			if !filepath.IsAbs(resolved) {
				resolved = filepath.Join(b.root, resolved)
			}
			if strings.HasPrefix(src, ".") {
				modDir := filepath.Dir(mod.Path)
				resolved = filepath.Join(modDir, src)
				if !filepath.IsAbs(resolved) {
					resolved = filepath.Join(b.root, resolved)
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

func jsonToAST(data []byte) *ast.Program {
	var val interface{}
	if err := json.Unmarshal(data, &val); err != nil {
		return &ast.Program{}
	}
	expr := jsonToExpr(val)
	return &ast.Program{
		Body: []ast.Stmt{
			&ast.ExportStmt{
				Default:     true,
				Declaration: &ast.ExprStmt{Expression: expr},
			},
		},
	}
}

func jsonToExpr(val interface{}) ast.Expr {
	switch v := val.(type) {
	case nil:
		return &ast.Literal{Kind: ast.NullLit, Value: "null"}
	case bool:
		s := "false"
		if v {
			s = "true"
		}
		return &ast.Literal{Kind: ast.BoolLit, Value: s}
	case float64:
		return &ast.Literal{Kind: ast.NumberLit, Value: fmt.Sprintf("%v", v)}
	case string:
		return &ast.Literal{Kind: ast.StringLit, Value: v}
	case []interface{}:
		elems := make([]ast.Expr, len(v))
		for i, e := range v {
			elems[i] = jsonToExpr(e)
		}
		return &ast.ArrayExpr{Elements: elems}
	case map[string]interface{}:
		props := make([]*ast.ObjectProp, 0, len(v))
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			props = append(props, &ast.ObjectProp{
				Key:   k,
				Value: jsonToExpr(v[k]),
			})
		}
		return &ast.ObjectExpr{Properties: props}
	}
	return &ast.Literal{Kind: ast.NullLit, Value: "null"}
}
