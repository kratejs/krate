package annotator

import (
	"path/filepath"
	"strings"

	"krate-compiler/internal/ast"
	"krate-compiler/internal/config"
	irtree "krate-compiler/internal/irtree"
	"krate-compiler/internal/sigutil"
)

// Annotate performs static analysis on a parsed program to determine
// component tiers, signal declarations, streaming mode, and used components.
func Annotate(prog *ast.Program, cfg *config.Config, sourceFile string, rawSource string) *irtree.Annotations {
	ann := &irtree.Annotations{
		ComponentTiers:   make(map[string]irtree.ComponentTier),
		SourceFile:       sourceFile,
		Signals:          make(map[string]ast.Expr),
		UsedComponents:   make(map[string]bool),
		Functions:        make(map[string]*ast.FnDecl),
		ComponentSources: make(map[string]string),
		ComponentRaw:     make(map[string]string),
	}

	// 1. Collect all function declarations
	collectFunctionsWithSource(prog.Body, ann.Functions, ann.ComponentSources, ann.ComponentRaw, sourceFile, rawSource)

	// 2. Find default export → entry point
	ann.EntryPoint = findDefaultExport(prog.Body)

	// 3. Walk used-component graph
	if ann.EntryPoint != "" {
		ann.UsedComponents[ann.EntryPoint] = true
		collectUsedFuncs(ann.Functions, ann.EntryPoint, ann.UsedComponents)
	}

	// 4. Classify each used component's tier
	classifyTiers(ann, cfg)

	// 5. Detect signal declarations
	for name := range ann.UsedComponents {
		if fn, ok := ann.Functions[name]; ok {
			collectSignalDecls(fn.Body, ann.Signals)
		}
	}

	// 6. Detect streaming config
	ann.HasStreaming = detectStreamingConfig(prog.Body)

	// 7. Detect <Suspense> usage
	if ann.EntryPoint != "" {
		if fn, ok := ann.Functions[ann.EntryPoint]; ok {
			ann.HasSuspense = detectSuspenseUsage(fn.Body, ann.Functions)
		}
	}

	return ann
}

// classifyTiers determines component tiers based on the priority rules:
// 1. Directive in source (@server, @static, @runtime)
// 2. File convention (*.server.tsx, *.static.tsx, *.runtime.tsx)
// 3. Config name lists (serverComponents, runtimeComponents)
// 4. Config directory lists (serverDirs, runtimeDirs)
// 5. Default: TierClient
//
// Each function is classified using its OWN module's source file and raw text
// (recorded by collectFunctionsWithSource), so a page-level `// @server`
// directive doesn't leak onto imported *.runtime.tsx components.
func classifyTiers(ann *irtree.Annotations, cfg *config.Config) {
	for name := range ann.UsedComponents {
		src := ann.ComponentSources[name]
		raw := ann.ComponentRaw[name]
		ann.ComponentTiers[name] = classifyComponent(name, ann.Functions[name], cfg, src, raw)
	}
}

// classifyComponent determines a single component's tier.
func classifyComponent(name string, fn *ast.FnDecl, cfg *config.Config, sourceFile string, rawSource string) irtree.ComponentTier {
	// 1. Directive in source (leading string literal with @annotation)
	if fn != nil {
		if tier := detectDirectiveTier(fn); tier != irtree.TierUnknown {
			return tier
		}
	}

	// 1b. Directive in raw source comments (// @server, // @static, // @runtime)
	if tier := detectDirectiveTierFromSource(rawSource); tier != irtree.TierUnknown {
		return tier
	}

	// 2. File convention
	if tier := detectFileConventionTier(sourceFile); tier != irtree.TierUnknown {
		return tier
	}

	// 3. Config name lists
	if cfg != nil {
		for _, n := range cfg.ServerComponents {
			if n == name {
				return irtree.TierServer
			}
		}
		for _, n := range cfg.RuntimeComponents {
			if n == name {
				return irtree.TierRuntime
			}
		}
	}

	// 4. Config directory lists
	if cfg != nil && sourceFile != "" {
		for _, dir := range cfg.ServerDirs {
			if matchesDir(sourceFile, dir) {
				return irtree.TierServer
			}
		}
		for _, dir := range cfg.RuntimeDirs {
			if matchesDir(sourceFile, dir) {
				return irtree.TierRuntime
			}
		}
	}

	// 5. Default: client
	return irtree.TierClient
}

// detectDirectiveTier checks the first statement of a function for a tier directive.
func detectDirectiveTier(fn *ast.FnDecl) irtree.ComponentTier {
	if len(fn.Body) == 0 {
		return irtree.TierUnknown
	}
	exprStmt, ok := fn.Body[0].(*ast.ExprStmt)
	if !ok {
		return irtree.TierUnknown
	}
	lit, ok := exprStmt.Expression.(*ast.Literal)
	if !ok || lit.Kind != ast.StringLit {
		return irtree.TierUnknown
	}
	val := strings.TrimSpace(lit.Value)
	switch {
	case strings.Contains(val, "@static"):
		return irtree.TierStatic
	case strings.Contains(val, "@server"):
		return irtree.TierServer
	case strings.Contains(val, "@runtime"):
		return irtree.TierRuntime
	case strings.Contains(val, "@client"):
		return irtree.TierClient
	}
	return irtree.TierUnknown
}

// detectDirectiveTierFromSource checks raw source text for comment-based directives
// like "// @server", "// @static", "// @runtime".
func detectDirectiveTierFromSource(source string) irtree.ComponentTier {
	if source == "" {
		return irtree.TierUnknown
	}
	check := source
	if len(check) > 200 {
		check = check[:200]
	}
	switch {
	case strings.Contains(check, "@static"):
		return irtree.TierStatic
	case strings.Contains(check, "@server"):
		return irtree.TierServer
	case strings.Contains(check, "@runtime"):
		return irtree.TierRuntime
	case strings.Contains(check, "@client"):
		return irtree.TierClient
	}
	return irtree.TierUnknown
}

// detectFileConventionTier checks the source file name for tier conventions.
func detectFileConventionTier(sourceFile string) irtree.ComponentTier {
	base := filepath.Base(sourceFile)
	switch {
	case strings.HasSuffix(base, ".server.tsx") || strings.HasSuffix(base, ".server.ts"):
		return irtree.TierServer
	case strings.HasSuffix(base, ".static.tsx") || strings.HasSuffix(base, ".static.ts"):
		return irtree.TierStatic
	case strings.HasSuffix(base, ".runtime.tsx") || strings.HasSuffix(base, ".runtime.ts"):
		return irtree.TierRuntime
	}
	return irtree.TierUnknown
}

// matchesDir checks if a file path is within a directory.
func matchesDir(filePath, dir string) bool {
	rel, err := filepath.Rel(dir, filePath)
	if err != nil {
		return false
	}
	return !strings.HasPrefix(rel, "..")
}

// detectStreamingConfig checks for `export const config = { streaming: true }`.
func detectStreamingConfig(body []ast.Stmt) bool {
	for _, stmt := range body {
		switch s := stmt.(type) {
		case *ast.ExportStmt:
			if s.Declaration != nil {
				if vStmt, ok := s.Declaration.(*ast.VarStmt); ok {
					for _, decl := range vStmt.Decls {
						if decl.Name == "config" && decl.Init != nil {
							return isStreamingTrue(decl.Init)
						}
					}
				}
			}
		case *ast.VarStmt:
			for _, decl := range s.Decls {
				if decl.Name == "config" && decl.Init != nil {
					return isStreamingTrue(decl.Init)
				}
			}
		}
	}
	return false
}

func isStreamingTrue(expr ast.Expr) bool {
	obj, ok := expr.(*ast.ObjectExpr)
	if !ok {
		return false
	}
	for _, prop := range obj.Properties {
		if prop.Key == "streaming" && !prop.Spread {
			if lit, ok := prop.Value.(*ast.Literal); ok && lit.Kind == ast.BoolLit && lit.Value == "true" {
				return true
			}
		}
	}
	return false
}

// detectSuspenseUsage walks the AST looking for <Suspense> JSX elements.
func detectSuspenseUsage(body []ast.Stmt, functions map[string]*ast.FnDecl) bool {
	for _, stmt := range body {
		switch s := stmt.(type) {
		case *ast.ReturnStmt:
			if s.Value != nil && hasSuspenseInExpr(s.Value) {
				return true
			}
		case *ast.ForStmt:
			if detectSuspenseUsage(s.Body, functions) {
				return true
			}
		case *ast.WhileStmt:
			if detectSuspenseUsage(s.Body, functions) {
				return true
			}
		case *ast.IfStmt:
			if detectSuspenseUsage(s.Consequent, functions) || detectSuspenseUsage(s.Alternate, functions) {
				return true
			}
		case *ast.BlockStmt:
			if detectSuspenseUsage(s.Body, functions) {
				return true
			}
		}
	}
	return false
}

func hasSuspenseInExpr(expr ast.Expr) bool {
	switch e := expr.(type) {
	case *ast.JSXElement:
		if e.Opening.Name == "Suspense" {
			return true
		}
		for _, child := range e.Children {
			switch c := child.(type) {
			case *ast.JSXElementChild:
				if hasSuspenseInExpr(c.Element) {
					return true
				}
			case *ast.JSXExprContainer:
				if hasSuspenseInExpr(c.Expression) {
					return true
				}
			}
		}
	case *ast.JSXFragment:
		for _, child := range e.Children {
			switch c := child.(type) {
			case *ast.JSXElementChild:
				if hasSuspenseInExpr(c.Element) {
					return true
				}
			case *ast.JSXExprContainer:
				if hasSuspenseInExpr(c.Expression) {
					return true
				}
			}
		}
	case *ast.TypeAssertion:
		return hasSuspenseInExpr(e.Expr)
	}
	return false
}

// ─── AST walking helpers (ported from renderer) ────────────────────────────

// collectFunctions walks function bodies recording declarations.
func collectFunctions(body []ast.Stmt, dest map[string]*ast.FnDecl) {
	collectFunctionsWithSource(body, dest, nil, nil, "", "")
}

// collectFunctionsWithSource walks function bodies recording declarations and,
// when sourcePath/rawSource are non-empty, records the module source each
// function came from so tier classification uses the component's own file.
func collectFunctionsWithSource(body []ast.Stmt, dest map[string]*ast.FnDecl, sources, raws map[string]string, sourcePath, rawSource string) {
	record := func(fn *ast.FnDecl) {
		dest[fn.Name] = fn
		if sources != nil {
			sources[fn.Name] = sourcePath
			raws[fn.Name] = rawSource
		}
	}
	for _, stmt := range body {
		switch s := stmt.(type) {
		case *ast.FnDecl:
			record(s)
		case *ast.ExportStmt:
			if s.Declaration != nil {
				if fn, ok := s.Declaration.(*ast.FnDecl); ok {
					record(fn)
				}
			}
		case *ast.VarStmt:
			for _, decl := range s.Decls {
				if len(decl.Name) > 0 && decl.Name[0] >= 'A' && decl.Name[0] <= 'Z' {
					if fn := extractComponentFromVar(decl); fn != nil {
						record(fn)
					}
				}
			}
		case *ast.ForStmt:
			collectFunctionsWithSource(s.Body, dest, sources, raws, sourcePath, rawSource)
		case *ast.WhileStmt:
			collectFunctionsWithSource(s.Body, dest, sources, raws, sourcePath, rawSource)
		case *ast.DoWhileStmt:
			collectFunctionsWithSource(s.Body, dest, sources, raws, sourcePath, rawSource)
		case *ast.SwitchStmt:
			for _, c := range s.Cases {
				collectFunctionsWithSource(c.Body, dest, sources, raws, sourcePath, rawSource)
			}
		case *ast.TryStmt:
			collectFunctionsWithSource(s.Body, dest, sources, raws, sourcePath, rawSource)
			if s.Catch != nil {
				collectFunctionsWithSource(s.Catch.Body, dest, sources, raws, sourcePath, rawSource)
			}
			collectFunctionsWithSource(s.Finally, dest, sources, raws, sourcePath, rawSource)
		case *ast.BlockStmt:
			collectFunctionsWithSource(s.Body, dest, sources, raws, sourcePath, rawSource)
		}
	}
}

func findDefaultExport(body []ast.Stmt) string {
	for _, stmt := range body {
		if exp, ok := stmt.(*ast.ExportStmt); ok && exp.Default {
			if fn, ok := exp.Declaration.(*ast.FnDecl); ok {
				return fn.Name
			}
			if exp.Local != "" {
				return exp.Local
			}
		}
	}
	return ""
}

func collectUsedFuncs(functions map[string]*ast.FnDecl, entry string, used map[string]bool) {
	if entry == "" {
		return
	}
	used[entry] = true
	fn := functions[entry]
	if fn == nil {
		return
	}
	collectComponentRefs(fn.Body, functions, used)
}

func collectComponentRefs(body []ast.Stmt, functions map[string]*ast.FnDecl, used map[string]bool) {
	for _, stmt := range body {
		switch s := stmt.(type) {
		case *ast.ReturnStmt:
			if s.Value != nil {
				collectExprRefs(s.Value, functions, used)
			}
		case *ast.ForStmt:
			collectComponentRefs(s.Body, functions, used)
		case *ast.WhileStmt:
			collectComponentRefs(s.Body, functions, used)
		case *ast.DoWhileStmt:
			collectComponentRefs(s.Body, functions, used)
		case *ast.SwitchStmt:
			for _, c := range s.Cases {
				collectComponentRefs(c.Body, functions, used)
			}
		case *ast.TryStmt:
			collectComponentRefs(s.Body, functions, used)
			if s.Catch != nil {
				collectComponentRefs(s.Catch.Body, functions, used)
			}
			collectComponentRefs(s.Finally, functions, used)
		case *ast.IfStmt:
			collectComponentRefs(s.Consequent, functions, used)
			collectComponentRefs(s.Alternate, functions, used)
		case *ast.BlockStmt:
			collectComponentRefs(s.Body, functions, used)
		}
	}
}

func collectExprRefs(expr ast.Expr, functions map[string]*ast.FnDecl, used map[string]bool) {
	if expr == nil {
		return
	}
	switch e := expr.(type) {
	case *ast.JSXElement:
		name := e.Opening.Name
		if len(name) > 0 && name[0] >= 'A' && name[0] <= 'Z' {
			if _, ok := functions[name]; ok && !used[name] {
				used[name] = true
				if fn := functions[name]; fn != nil {
					collectComponentRefs(fn.Body, functions, used)
				}
			}
		}
		for _, child := range e.Children {
			switch c := child.(type) {
			case *ast.JSXElementChild:
				collectExprRefs(c.Element, functions, used)
			case *ast.JSXFragmentChild:
				collectFragmentRefs(c.Fragment, functions, used)
			}
		}
	case *ast.JSXFragment:
		for _, child := range e.Children {
			switch c := child.(type) {
			case *ast.JSXElementChild:
				collectExprRefs(c.Element, functions, used)
			case *ast.JSXFragmentChild:
				collectFragmentRefs(c.Fragment, functions, used)
			}
		}
	case *ast.TypeAssertion:
		collectExprRefs(e.Expr, functions, used)
	}
}

func collectFragmentRefs(frag *ast.JSXFragment, functions map[string]*ast.FnDecl, used map[string]bool) {
	for _, child := range frag.Children {
		switch c := child.(type) {
		case *ast.JSXElementChild:
			collectExprRefs(c.Element, functions, used)
		case *ast.JSXFragmentChild:
			collectFragmentRefs(c.Fragment, functions, used)
		}
	}
}

// collectSignalDecls walks function bodies to find createSignal / createResource calls.
func collectSignalDecls(body []ast.Stmt, signals map[string]ast.Expr) {
	for _, d := range sigutil.Find(body, true) {
		if d.Initial != nil {
			signals[d.Name] = d.Initial
		}
	}
}

// extractComponentFromVar attempts to extract a component function from a variable
// declaration like "const Button = (props) => { ... }"
func extractComponentFromVar(decl *ast.VarDecl) *ast.FnDecl {
	if decl.Init == nil {
		return nil
	}
	innerFn := unwrapComponentInit(decl.Init)
	if innerFn == nil {
		return nil
	}
	return &ast.FnDecl{
		Name:   decl.Name,
		Params: innerFn.Params,
		Body:   innerFn.Body,
	}
}

func unwrapComponentInit(expr ast.Expr) *ast.ArrowFn {
	switch e := expr.(type) {
	case *ast.ArrowFn:
		return e
	case *ast.CallExpr:
		if isForwardRefLike(e.Callee) {
			if len(e.Args) >= 1 {
				return unwrapComponentInit(e.Args[0])
			}
		}
	}
	return nil
}

func isForwardRefLike(callee ast.Expr) bool {
	id, ok := callee.(*ast.Identifier)
	if !ok {
		return false
	}
	return id.Name == "forwardRef" || id.Name == "memo"
}

// ModuleSource carries a bundled module's program plus its source file path and
// raw text so imported components can be tier-classified against their own file.
type ModuleSource struct {
	Program   *ast.Program
	Path      string
	RawSource string
}

// MergeModuleFunctions collects function declarations from additional bundled modules
// and merges them into the annotations. Imported components (e.g. DocsLayout, SidebarNav)
// live in separate bundled modules that the entry module's AST doesn't contain.
// Each module's own source path/raw text is recorded so tier classification for
// imported components uses their own file conventions (e.g. *.runtime.tsx).
func MergeModuleFunctions(ann *irtree.Annotations, modules []ModuleSource) {
	for _, mod := range modules {
		if mod.Program != nil {
			collectFunctionsWithSource(mod.Program.Body, ann.Functions, ann.ComponentSources, ann.ComponentRaw, mod.Path, mod.RawSource)
		}
	}
	// Re-walk used components to pick up newly discovered functions
	if ann.EntryPoint != "" {
		collectUsedFuncs(ann.Functions, ann.EntryPoint, ann.UsedComponents)
	}
	// Collect signal declarations for any newly discovered components so the
	// annotation signal map is complete for imported modules too.
	for name := range ann.UsedComponents {
		if fn, ok := ann.Functions[name]; ok {
			collectSignalDecls(fn.Body, ann.Signals)
		}
	}
}

// ReclassifyTiers re-runs tier classification for all used components.
// Call after MergeModuleFunctions so newly discovered components get classified
// against their own module sources.
func ReclassifyTiers(ann *irtree.Annotations, cfg *config.Config) {
	classifyTiers(ann, cfg)
}
