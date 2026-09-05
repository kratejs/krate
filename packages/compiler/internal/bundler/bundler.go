package bundler

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"krate-compiler/internal/ast"
	"krate-compiler/internal/css"
	"krate-compiler/internal/lexer"
	"krate-compiler/internal/markdown"
	"krate-compiler/internal/parser"
)

type PkgJSON struct {
	Main   string `json:"main"`
	Module string `json:"module"`
}

type Module struct {
	Path           string
	Program        *ast.Program
	Imports        []string
	IsEntry        bool
	IsCSS          bool
	IsExternal     bool
	ComponentClass ComponentClass // server, runtime, or client component
	SourceCode     string         // raw source for directive scanning
}

type CSSModuleInfo struct {
	ResolvedPath string
	ScopedCSS    string
	Mappings     map[string]string
}

type Bundle struct {
	EntryPath   string
	Modules     []*Module
	CSS         string
	CSSModules  map[string]*CSSModuleInfo
	AssetFiles  map[string]string // resolved source path → hashed site URL (/assets/…)
	WorkerFiles map[string]string // worker source path → hashed site URL (/workers/…)
	WorkerEsm   map[string]bool   // worker source path → true when built with `type: 'module'`
	Frontmatter map[string]string // .mdx frontmatter, if any
}

type Bundler struct {
	root              string
	seen              map[string]bool
	order             []*Module
	css               []string
	cssModules        map[string]*CSSModuleInfo
	assets            map[string]string // resolved source path → hashed site URL
	workers           map[string]string // worker source path → hashed site URL (/workers/…)
	workerEsm         map[string]bool   // worker source path → built as ES module
	frontmatter       map[string]string // from .mdx frontmatter
	emitReact         bool
	pathAliases       []pathAlias
	tsBaseDir         string
	serverComponents  []string
	runtimeComponents []string
	serverDirs        []string
	runtimeDirs       []string
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

var reactNames = map[string]string{
	"useState":      "createSignal",
	"useEffect":     "createEffect",
	"useMemo":       "createMemo",
	"useRef":        "useRef",
	"useCallback":   "useCallback",
	"forwardRef":    "forwardRef",
	"createElement": "createElement",
}

func New(root string) *Bundler {
	return &Bundler{
		root:    root,
		seen:    make(map[string]bool),
		assets:  make(map[string]string),
		workers: make(map[string]string),
		workerEsm: make(map[string]bool),
	}
}

// SetEmitReact enables React-to-krate rewriting for all modules.
func (b *Bundler) SetEmitReact(v bool) {
	b.emitReact = v
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

// resolveImportForModule resolves an import, checking path aliases first.
func (b *Bundler) resolveImportForModule(importer, imp string) string {
	resolved := resolveImport(importer, imp)
	if resolved != "" {
		return resolved
	}
	if len(b.pathAliases) > 0 && b.tsBaseDir != "" {
		resolved = resolvePathAlias(imp, b.pathAliases, b.tsBaseDir)
		if resolved != "" {
			return resolved
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
	// to the hashed /workers/… URL the worker is emitted at.
	b.rewriteWorkerRefs()

	if err := b.CheckCompositionRules(); err != nil {
		return nil, err
	}

	return &Bundle{
		EntryPath:   entry,
		Modules:     b.order,
		CSS:         strings.Join(b.css, "\n"),
		CSSModules:  b.cssModules,
		AssetFiles:  b.assets,
		WorkerFiles: b.workers,
		WorkerEsm:   b.workerEsm,
		Frontmatter: b.frontmatter,
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

	if strings.HasSuffix(path, ".css") {
		data, err := os.ReadFile(abs)
		if err != nil {
			return fmt.Errorf("reading %s: %w", path, err)
		}

		if strings.Contains(path, ".module.") {
			scopedCSS, mapping, err := css.ProcessModule(abs, string(data))
			if err != nil {
				return fmt.Errorf("processing css module %s: %w", path, err)
			}
			b.css = append(b.css, scopedCSS)
			b.cssModules[abs] = &CSSModuleInfo{
				ResolvedPath: abs,
				ScopedCSS:    scopedCSS,
				Mappings:     mapping,
			}
		} else {
			b.css = append(b.css, string(data))
		}

		b.order = append(b.order, &Module{
			Path:  path,
			IsCSS: true,
		})
		return nil
	}

	if strings.HasSuffix(path, ".json") {
		data, err := os.ReadFile(abs)
		if err != nil {
			return fmt.Errorf("reading %s: %w", path, err)
		}
		mod := &Module{
			Path:    path,
			Program: jsonToAST(data),
			IsEntry: isEntry,
		}
		b.collectImports(mod.Program, mod)
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

	if strings.HasSuffix(path, ".md") || strings.HasSuffix(path, ".mdx") {
		data, err := os.ReadFile(abs)
		if err != nil {
			return fmt.Errorf("reading %s: %w", path, err)
		}

		src := string(data)
		mcfg := markdown.DefaultConfig()
		var tsxSource string

		if strings.HasSuffix(path, ".mdx") {
			result := markdown.ParseMDX(src, mcfg)
			if len(result.Frontmatter) > 0 {
				b.frontmatter = result.Frontmatter
			}
			tsxSource = generateMDXBundleTSX(path, src, result, mcfg)
		} else {
			// Plain .md pages are routed through the same MDX-style TSX bundle so
			// fenced code blocks render as the <Code> component.
			result := markdown.ParseMDX(src, mcfg)
			if len(result.Frontmatter) > 0 {
				b.frontmatter = result.Frontmatter
			}
			tsxSource = generateMDXBundleTSX(path, src, result, mcfg)
		}

		tokens := lexer.New(tsxSource).Tokenize()
		p := parser.New(tokens)
		p.Filename = path
		prog := p.ParseProgram()

		if b.emitReact {
			RewriteReact(prog)
		}

		mod := &Module{
			Path:    path,
			Program: prog,
			IsEntry: isEntry,
		}
		b.collectImports(prog, mod)
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

	if !strings.HasSuffix(path, ".tsx") && !strings.HasSuffix(path, ".ts") &&
		!strings.HasSuffix(path, ".jsx") && !strings.HasSuffix(path, ".js") {
		if assetExtensions[strings.ToLower(filepath.Ext(path))] {
			data, err := os.ReadFile(abs)
			if err != nil {
				return fmt.Errorf("reading %s: %w", path, err)
			}
			base := filepath.Base(path)
			ext := filepath.Ext(base)
			name := strings.TrimSuffix(base, ext)
			hash := hashBytes(data)
			url := "/assets/" + name + "-" + hash + ext
			b.assets[abs] = url
		}
		b.order = append(b.order, &Module{
			Path:       path,
			IsExternal: true,
		})
		return nil
	}

	// The krate client runtime is provided at runtime by the shared chunk
	// (its exports — createSignal, h, initRouter, etc. — are window globals).
	// Its AST is never referenced by the compiler's emitted code, so skip
	// reading/lexing/parsing it entirely. This avoids re-parsing ~160 KB of
	// runtime JS for every page that imports it.
	if isKrateRuntime(abs) {
		b.order = append(b.order, &Module{
			Path:       path,
			IsExternal: true,
		})
		return nil
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

	if b.emitReact {
		RewriteReact(prog)
	}

	mod := &Module{
		Path:       path,
		Program:    prog,
		IsEntry:    isEntry,
		SourceCode: src,
	}
	// Classify server/runtime components
	mod.ComponentClass = ClassifyComponent(src, path, b.serverComponents, b.runtimeComponents, b.serverDirs, b.runtimeDirs)

	b.collectImports(prog, mod)
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
			if exp.StarReexport && exp.ReexportSource != "" {
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

// resolveImport resolves an import path to an absolute file path.
func resolveImport(importer, imp string) string {
	dir := filepath.Dir(importer)

	if strings.HasPrefix(imp, ".") || strings.HasPrefix(imp, "..") {
		resolved := filepath.Clean(filepath.Join(dir, imp))

		if fileExists(resolved) {
			info, err := os.Stat(resolved)
			if err == nil && info.IsDir() {
				for _, index := range []string{"index.tsx", "index.ts", "index.jsx", "index.js", "index.md", "index.mdx"} {
					candidate := filepath.Join(resolved, index)
					if fileExists(candidate) {
						return candidate
					}
				}
			}
			return resolved
		}

		extensions := []string{".tsx", ".ts", ".jsx", ".js", ".md", ".mdx", ".css", ".json"}
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

	return resolveNodeModule(dir, imp)
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
					extensions := []string{".tsx", ".ts", ".jsx", ".js", ".css", ".json"}
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

func resolveNodeModule(startDir, pkg string) string {
	pkgDir := pkg
	if strings.HasPrefix(pkg, "@") {
		dir := filepath.Dir(startDir)
		for {
			base := filepath.Join(dir, "node_modules", pkgDir)
			if info, err := os.Stat(base); err == nil && info.IsDir() {
				return resolvePackageDir(base)
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
		return ""
	}

	dir := startDir
	for {
		base := filepath.Join(dir, "node_modules", pkgDir)
		if info, err := os.Stat(base); err == nil && info.IsDir() {
			return resolvePackageDir(base)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ""
}

func resolvePackageDir(pkgDir string) string {
	pkgJSONPath := filepath.Join(pkgDir, "package.json")
	if data, err := os.ReadFile(pkgJSONPath); err == nil {
		var pkg PkgJSON
		if json.Unmarshal(data, &pkg) == nil {
			if pkg.Module != "" {
				candidate := filepath.Join(pkgDir, pkg.Module)
				if fileExists(candidate) {
					return candidate
				}
			}
			if pkg.Main != "" {
				candidate := filepath.Join(pkgDir, pkg.Main)
				if fileExists(candidate) {
					return candidate
				}
			}
		}
	}

	indices := []string{"index.tsx", "index.ts", "index.jsx", "index.js"}
	for _, idx := range indices {
		candidate := filepath.Join(pkgDir, idx)
		if fileExists(candidate) {
			return candidate
		}
	}

	return ""
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
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

func RewriteReact(prog *ast.Program) {
	imports := make(map[string]string) // local name → react api name
	var reactAlias string
	var toRemove []int

	for i, stmt := range prog.Body {
		imp, ok := stmt.(*ast.ImportStmt)
		if !ok {
			continue
		}
		src := strings.Trim(imp.Source, "\"'")
		if src != "react" {
			continue
		}
		toRemove = append(toRemove, i)

		if len(imp.Named) == 0 {
			reactAlias = "React"
		}

		for _, named := range imp.Named {
			local := named.Local
			if local == "" {
				local = named.Remote
			}

			if named.Remote == "default" || named.Remote == "*" || named.Remote == "React" {
				reactAlias = local
				continue
			}

			if krateName, ok := reactNames[named.Remote]; ok {
				imports[local] = krateName
			}
		}
	}

	for i := len(toRemove) - 1; i >= 0; i-- {
		idx := toRemove[i]
		prog.Body = append(prog.Body[:idx], prog.Body[idx+1:]...)
	}

	if reactAlias != "" {
		shimSource := fmt.Sprintf("const %s = krate;", reactAlias)
		shimTokens := lexer.New(shimSource).Tokenize()
		shimParser := parser.New(shimTokens)
		shimProg := shimParser.ParseProgram()
		if len(shimProg.Body) > 0 {
			prog.Body = append(shimProg.Body, prog.Body...)
		}
	}

	if len(imports) == 0 && reactAlias == "" {
		return
	}

	rewriteStmts(prog.Body, imports, reactAlias)
}

func rewriteStmts(stmts []ast.Stmt, imports map[string]string, reactAlias string) {
	for _, stmt := range stmts {
		rewriteStmt(stmt, imports, reactAlias)
	}
}

func rewriteStmt(stmt ast.Stmt, imports map[string]string, reactAlias string) {
	if stmt == nil {
		return
	}

	switch s := stmt.(type) {
	case *ast.ReturnStmt:
		s.Value = rewriteExpr(s.Value, imports, reactAlias)
	case *ast.VarStmt:
		for _, decl := range s.Decls {
			decl.Init = rewriteExpr(decl.Init, imports, reactAlias)
		}
	case *ast.ExprStmt:
		s.Expression = rewriteExpr(s.Expression, imports, reactAlias)
	case *ast.FnDecl:
		for _, p := range s.Params {
			if p.Default != nil {
				p.Default = rewriteExpr(p.Default, imports, reactAlias)
			}
		}
		rewriteStmts(s.Body, imports, reactAlias)
	case *ast.ExportStmt:
		if s.Declaration != nil {
			rewriteStmt(s.Declaration, imports, reactAlias)
		}
	case *ast.IfStmt:
		s.Test = rewriteExpr(s.Test, imports, reactAlias)
		rewriteStmts(s.Consequent, imports, reactAlias)
		rewriteStmts(s.Alternate, imports, reactAlias)
	case *ast.BlockStmt:
		rewriteStmts(s.Body, imports, reactAlias)
	case *ast.ForStmt:
		if s.Init != nil {
			rewriteStmt(s.Init, imports, reactAlias)
		}
		if s.Test != nil {
			s.Test = rewriteExpr(s.Test, imports, reactAlias)
		}
		if s.Update != nil {
			s.Update = rewriteExpr(s.Update, imports, reactAlias)
		}
		rewriteStmts(s.Body, imports, reactAlias)
	case *ast.ForInStmt:
		if s.Left != nil {
			s.Left = rewriteExpr(s.Left, imports, reactAlias)
		}
		if s.Right != nil {
			s.Right = rewriteExpr(s.Right, imports, reactAlias)
		}
		rewriteStmts(s.Body, imports, reactAlias)
	case *ast.WhileStmt:
		if s.Test != nil {
			s.Test = rewriteExpr(s.Test, imports, reactAlias)
		}
		rewriteStmts(s.Body, imports, reactAlias)
	case *ast.DoWhileStmt:
		rewriteStmts(s.Body, imports, reactAlias)
		if s.Test != nil {
			s.Test = rewriteExpr(s.Test, imports, reactAlias)
		}
	case *ast.SwitchStmt:
		if s.Discriminant != nil {
			s.Discriminant = rewriteExpr(s.Discriminant, imports, reactAlias)
		}
		for _, c := range s.Cases {
			if c.Test != nil {
				c.Test = rewriteExpr(c.Test, imports, reactAlias)
			}
			rewriteStmts(c.Body, imports, reactAlias)
		}
	case *ast.TryStmt:
		rewriteStmts(s.Body, imports, reactAlias)
		if s.Catch != nil {
			rewriteStmts(s.Catch.Body, imports, reactAlias)
		}
		rewriteStmts(s.Finally, imports, reactAlias)
	case *ast.ThrowStmt:
		s.Value = rewriteExpr(s.Value, imports, reactAlias)
	}
}

func rewriteExpr(expr ast.Expr, imports map[string]string, reactAlias string) ast.Expr {
	if expr == nil {
		return nil
	}

	switch e := expr.(type) {
	case *ast.CallExpr:
		for i, arg := range e.Args {
			e.Args[i] = rewriteExpr(arg, imports, reactAlias)
		}

		if mem, ok := e.Callee.(*ast.MemberExpr); ok && !mem.Computed {
			if objId, ok := mem.Object.(*ast.Identifier); ok && objId.Name == reactAlias {
				if propId, ok := mem.Property.(*ast.Identifier); ok {
					if target, ok := reactNames[propId.Name]; ok {
						switch target {
						case "createSignal", "createMemo", "createEffect":
							objId.Name = "krate"
							propId.Name = target
							return e
						case "useRef":
							var initVal ast.Expr = &ast.Literal{Kind: ast.NullLit, Value: "null"}
							if len(e.Args) >= 1 {
								initVal = e.Args[0]
							}
							return &ast.ObjectExpr{
								Properties: []*ast.ObjectProp{
									{Key: "current", Value: initVal},
								},
							}
						case "forwardRef":
							objId.Name = "krate"
							propId.Name = "forwardRef"
							return e
						case "useCallback":
							if len(e.Args) >= 1 {
								return rewriteExpr(e.Args[0], imports, reactAlias)
							}
							return e
						default:
							objId.Name = "krate"
							propId.Name = target
							return e
						}
					}
				}
			}
		}

		if id, ok := e.Callee.(*ast.Identifier); ok {
			if target, ok := imports[id.Name]; ok {
				switch target {
				case "createSignal", "createMemo", "createEffect":
					id.Name = target
					return e
				case "useRef":
					var initVal ast.Expr = &ast.Literal{Kind: ast.NullLit, Value: "null"}
					if len(e.Args) >= 1 {
						initVal = e.Args[0]
					}
					return &ast.ObjectExpr{
						Properties: []*ast.ObjectProp{
							{Key: "current", Value: initVal},
						},
					}
				case "forwardRef":
					return &ast.CallExpr{
						Callee: &ast.MemberExpr{
							Object:   &ast.Identifier{Name: "krate"},
							Property: &ast.Identifier{Name: "forwardRef"},
						},
						Args: e.Args,
					}
				case "useCallback":
					if len(e.Args) >= 1 {
						return rewriteExpr(e.Args[0], imports, reactAlias)
					}
					return e
				default:
					id.Name = target
					return e
				}
			}
		}

	case *ast.ObjectExpr:
		for _, prop := range e.Properties {
			prop.Value = rewriteExpr(prop.Value, imports, reactAlias)
		}
	case *ast.ArrayExpr:
		for i, elem := range e.Elements {
			e.Elements[i] = rewriteExpr(elem, imports, reactAlias)
		}
	case *ast.MemberExpr:
		e.Object = rewriteExpr(e.Object, imports, reactAlias)
		if e.Computed {
			e.Property = rewriteExpr(e.Property, imports, reactAlias)
		}
	case *ast.BinaryExpr:
		e.Left = rewriteExpr(e.Left, imports, reactAlias)
		e.Right = rewriteExpr(e.Right, imports, reactAlias)
	case *ast.UnaryExpr:
		e.Arg = rewriteExpr(e.Arg, imports, reactAlias)
	case *ast.ConditionalExpr:
		e.Test = rewriteExpr(e.Test, imports, reactAlias)
		e.Consequent = rewriteExpr(e.Consequent, imports, reactAlias)
		e.Alternate = rewriteExpr(e.Alternate, imports, reactAlias)
	case *ast.TypeAssertion:
		e.Expr = rewriteExpr(e.Expr, imports, reactAlias)
	case *ast.ArrowFn:
		for _, p := range e.Params {
			if p.Default != nil {
				p.Default = rewriteExpr(p.Default, imports, reactAlias)
			}
		}
		rewriteStmts(e.Body, imports, reactAlias)
	case *ast.TemplateExpr:
		for i, part := range e.Parts {
			e.Parts[i] = rewriteExpr(part, imports, reactAlias)
		}
	case *ast.NewExpr:
		e.Callee = rewriteExpr(e.Callee, imports, reactAlias)
		for i, arg := range e.Args {
			e.Args[i] = rewriteExpr(arg, imports, reactAlias)
		}
	case *ast.AwaitExpr:
		e.Arg = rewriteExpr(e.Arg, imports, reactAlias)
	case *ast.DynamicImport:
		e.Arg = rewriteExpr(e.Arg, imports, reactAlias)
	case *ast.ImportMetaExpr:
	case *ast.JSXElement:
		if e.Opening != nil {
			for _, attr := range e.Opening.Attributes {
				if attr.Value != nil {
					attr.Value = rewriteExpr(attr.Value, imports, reactAlias)
				}
			}
		}
		for i, child := range e.Children {
			e.Children[i] = rewriteJSXChild(child, imports, reactAlias)
		}
	case *ast.JSXFragment:
		for i, child := range e.Children {
			e.Children[i] = rewriteJSXChild(child, imports, reactAlias)
		}
	}

	return expr
}

func rewriteJSXChild(child ast.JSXChild, imports map[string]string, reactAlias string) ast.JSXChild {
	switch c := child.(type) {
	case *ast.JSXExprContainer:
		c.Expression = rewriteExpr(c.Expression, imports, reactAlias)
		return c
	case *ast.JSXElementChild:
		c.Element = rewriteExpr(c.Element, imports, reactAlias).(*ast.JSXElement)
		return c
	case *ast.JSXFragmentChild:
		c.Fragment = rewriteExpr(c.Fragment, imports, reactAlias).(*ast.JSXFragment)
		return c
	}
	return child
}

// rewriteCSSModuleRefs replaces CSS module member expressions (styles.card)
// with their hashed literal values across every bundled module. Each module's
// import statement binds a local name (styles) to a *.module.css file whose
// class→hash mapping was collected during resolution; member reads on that
// local name are swapped for the resolved class name so the irtree builder and
// hydration codegen emit the real value instead of a reference to the undefined
// runtime import.
func (b *Bundler) rewriteCSSModuleRefs() {
	for _, mod := range b.order {
		if mod.Program == nil {
			continue
		}
		localVars := map[string]map[string]string{} // local import name → class→hash
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

// rewriteAssetImportRefs replaces imported-asset binding reads with their
// hashed site URL literal across every bundled module. Each module's
// `import logo from './logo.png'` binds a local name (logo) to an asset file;
// bare reads of that local name are swapped for the resolved /assets/… URL so
// the irtree builder and hydration codegen emit the real URL instead of a
// reference to a runtime import that never exists.
func (b *Bundler) rewriteAssetImportRefs() {
	for _, mod := range b.order {
		if mod.Program == nil {
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
		if mod.Program == nil {
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
