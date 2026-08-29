package build

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"krate-compiler/internal/annotator"
	"krate-compiler/internal/ast"
	"krate-compiler/internal/bundler"
	"krate-compiler/internal/irtree"
	"krate-compiler/internal/reactive"
	"krate-compiler/internal/renderer"
	"krate-compiler/internal/tsexec"
)

// injectStaticParams seeds the root page component with concrete dynamic-route
// params so a statically generated page ([id].tsx via generateStaticParams)
// can render them. Params are exposed to the component's SSR evaluation via:
//   - `params` — a JSON object (e.g. { params } destructuring → params.id)
//   - each param key — a direct binding (e.g. { id } destructuring → id)
//
// Only pure, signal-less root components are SSREval'd this way; interactive
// (client) roots keep their normal hydration-driven emission.
func injectStaticParams(tree *irtree.ComponentTree, params map[string]string) {
	root := tree.Root
	if root == nil || params == nil || len(params) == 0 {
		return
	}
	if root.Tier != irtree.TierStatic && root.Tier != irtree.TierServer {
		// Interactive roots render their params reactively via the served props
		// script; injecting static bindings here would conflict with hydration.
		return
	}
	bindings := make(map[string]string, len(params)+1)
	objStr := make([]string, 0, len(params))
	for k, v := range params {
		bindings[k] = v
		bv, _ := json.Marshal(v)
		objStr = append(objStr, fmt.Sprintf("%q:%s", k, bv))
	}
	bindings["params"] = "{" + strings.Join(objStr, ",") + "}"
	if len(bindings) > 0 {
		if len(root.SSREvalBindings) > 0 {
			for k, v := range root.SSREvalBindings {
				if _, ok := bindings[k]; !ok {
					bindings[k] = v
				}
			}
		}
		root.SSREvalBindings = bindings
		root.IsSSREval = true
	}
}

// extractParamNames extracts parameter names from a dynamic route filename.
// e.g. "video/[id].tsx" → ["id"], "user/[username]/posts/[postId].tsx" → ["username", "postId"]
func extractParamNames(pagePath, pagesDir string) []string {
	rel, err := filepath.Rel(pagesDir, pagePath)
	if err != nil {
		return nil
	}
	rel = filepath.ToSlash(rel)
	parts := strings.Split(rel, "/")
	var params []string
	for _, part := range parts {
		cleaned := strings.TrimSuffix(part, filepath.Ext(part))
		if strings.HasPrefix(cleaned, "[") && strings.HasSuffix(cleaned, "]") {
			paramName := cleaned[1 : len(cleaned)-1]
			params = append(params, paramName)
		}
	}
	return params
}

// hasGenerateStaticParams checks if the program exports a generateStaticParams function.
func hasGenerateStaticParams(prog *ast.Program) bool {
	for _, stmt := range prog.Body {
		exp, ok := stmt.(*ast.ExportStmt)
		if !ok {
			continue
		}
		fn, ok := exp.Declaration.(*ast.FnDecl)
		if ok && fn.Name == "generateStaticParams" {
			return true
		}
	}
	return false
}

// executeGenerateStaticParams runs the page's generateStaticParams via npx tsx
// and returns the parsed param combinations.
func executeGenerateStaticParams(pagePath string) ([]map[string]string, error) {
	abs, err := filepath.Abs(pagePath)
	if err != nil {
		return nil, err
	}

	content := fmt.Sprintf(
		`import * as mod from '%s';
const fn = mod.generateStaticParams || (mod.default && mod.default.generateStaticParams);
if (typeof fn !== 'function') {
  process.stderr.write('generateStaticParams is not a function\n');
  process.exit(1);
}
const result = await fn();
console.log(JSON.stringify(result));
`,
		tsexec.ImportPath(abs),
	)

	output, _, err := tsexec.RunBootstrap("krate-gsp-bootstrap", content, filepath.Dir(abs), 30*time.Second)
	if err != nil {
		return nil, fmt.Errorf("generateStaticParams execution: %w", err)
	}

	var paramSets []map[string]string
	if err := json.Unmarshal(output, &paramSets); err != nil {
		// Try as array of interfaces
		var rawSets []map[string]interface{}
		if err2 := json.Unmarshal(output, &rawSets); err2 != nil {
			return nil, fmt.Errorf("parsing generateStaticParams output: %w\nraw: %s", err, string(output))
		}
		for _, raw := range rawSets {
			ps := make(map[string]string)
			for k, v := range raw {
				switch val := v.(type) {
				case string:
					ps[k] = val
				default:
					b, _ := json.Marshal(val)
					ps[k] = string(b)
				}
			}
			paramSets = append(paramSets, ps)
		}
	}

	return paramSets, nil
}

// staticParamsPage represents a page with params for static generation.
type staticParamsPage struct {
	PagePath string
	Params   map[string]string
	OutPath  string // e.g. "video/abc123"
}

// isDynamicRoute checks if a page path contains [param] segments.
func isDynamicRoute(pagePath, pagesDir string) bool {
	rel, err := filepath.Rel(pagesDir, pagePath)
	if err != nil {
		return false
	}
	rel = filepath.ToSlash(rel)
	return strings.Contains(rel, "[") && strings.Contains(rel, "]")
}

// resolveStaticParamsPages expands dynamic routes using generateStaticParams.
// It returns additional pages to build, each with concrete param values.
func (b *Builder) resolveStaticParamsPages(pages []string) []staticParamsPage {
	var expanded []staticParamsPage

	for _, page := range pages {
		if !isDynamicRoute(page, b.Cfg.PagesDir) {
			continue
		}

		// Bundle the page to check for generateStaticParams
		bnd := bundler.New(b.Root)
		bnd.SetEmitReact(b.Cfg.EmitReact)
		bnd.SetPathAliases(b.cfgPathAliasPrefixes(), b.cfgPathAliasTargets(), b.Cfg.TSBaseDir)
		bnd.SetServerComponents(b.Cfg.ServerComponents, b.Cfg.RuntimeComponents, b.Cfg.ServerDirs, b.Cfg.RuntimeDirs)

		bundle, err := bnd.Bundle(page)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  %sWarning: failed to bundle %s for generateStaticParams: %v%s\n", cYellow, page, err, cReset)
			continue
		}

		entryModule := findEntryModule(bundle.Modules)
		if entryModule == nil || entryModule.Program == nil {
			continue
		}

		if !hasGenerateStaticParams(entryModule.Program) {
			continue
		}

		fmt.Printf("  %s⚡%s generateStaticParams: %s\n", cCyan, cReset, filepath.Base(page))

		paramSets, err := executeGenerateStaticParams(page)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  %s✗ generateStaticParams error (%s):%s %v\n", cRed, filepath.Base(page), cReset, err)
			continue
		}

		for _, params := range paramSets {
			rel, _ := filepath.Rel(b.Cfg.PagesDir, page)
			rel = filepath.ToSlash(rel)
			rel = strings.TrimSuffix(rel, filepath.Ext(rel))

			parts := strings.Split(rel, "/")
			for i, part := range parts {
				if strings.HasPrefix(part, "[") && strings.HasSuffix(part, "]") {
					paramName := part[1 : len(part)-1]
					if val, ok := params[paramName]; ok {
						parts[i] = val
					}
				}
			}
			outPath := strings.Join(parts, "/")

			expanded = append(expanded, staticParamsPage{
				PagePath: page,
				Params:   params,
				OutPath:  outPath,
			})
		}
	}

	return expanded
}

// buildStaticParamsPage builds a single page with its params injected into the
// page component's props (via injectStaticParams).
func (b *Builder) buildStaticParamsPage(spp staticParamsPage) (*PageResult, string, error) {
	bnd := bundler.New(b.Root)
	bnd.SetEmitReact(b.Cfg.EmitReact)
	bnd.SetPathAliases(b.cfgPathAliasPrefixes(), b.cfgPathAliasTargets(), b.Cfg.TSBaseDir)
	bnd.SetServerComponents(b.Cfg.ServerComponents, b.Cfg.RuntimeComponents, b.Cfg.ServerDirs, b.Cfg.RuntimeDirs)

	bundle, err := bnd.Bundle(spp.PagePath)
	if err != nil {
		return nil, "", err
	}

	entryModule := findEntryModule(bundle.Modules)
	if entryModule == nil || entryModule.Program == nil {
		return nil, "", fmt.Errorf("no entry module found")
	}

	renderMode, revalidate := detectRenderMode(entryModule.Program)

	// Global streaming override: if configured, force all pages to stream.
	if b.Cfg.SSR.Streaming && renderMode != RenderStreaming {
		renderMode = RenderStreaming
		fmt.Fprintf(os.Stderr, "  %s⚡%s %s → streaming (global override)\n", cCyan, cReset, filepath.Base(spp.PagePath))
	}

	b.TransformUniversalIcons(entryModule.Program)
	b.TransformUniversalImages(entryModule.Program)

	// ─── New pipeline: Annotate → Build IR → Emit ──────────────────────────
	ann := annotator.Annotate(entryModule.Program, b.Cfg, spp.PagePath, entryModule.SourceCode)
	extraPrograms := moduleSources(bundle.Modules, entryModule)
	annotator.MergeModuleFunctions(ann, extraPrograms)
	tree := irtree.Build(entryModule.Program, ann)
	injectStaticParams(tree, spp.Params)
	emitter := renderer.NewEmitter()
	emitter.IconResolver = b.iconResolver
	emitter.EvalJS = b.jsExprEvaluator()
	emitResult := emitter.Emit(tree)
	renderer.EmitMeta(tree, emitResult)

	if len(emitResult.Errors) > 0 {
		return nil, "", renderErrors(spp.PagePath, emitResult.Errors)
	}

	// Compile-time reactive dependency validation. Surfaced as warnings so
	// dead signals / circular effects are caught before hydration ships.
	b.printReactiveDiags(reactive.Build(emitResult.Signatures).Validate())

	layoutPath := findLayout(spp.PagePath, b.Cfg.PagesDir)
	if layoutPath != "" {
		layoutRes, layoutCSS, err := b.executeLayoutPipeline(layoutPath, emitResult.HTML, nil)
		if err == nil {
			emitResult.HTML = layoutRes.HTML
			emitResult.HeadHTML = emitResult.HeadHTML + layoutRes.HeadHTML
			emitResult.ScriptHTML = emitResult.ScriptHTML + layoutRes.ScriptHTML
			emitResult.StyleHTML = emitResult.StyleHTML + layoutRes.StyleHTML
			bundle.CSS += layoutCSS
		}
	}

	pageDir := filepath.Join(b.Cfg.OutDir, spp.OutPath)
	if err := os.MkdirAll(pageDir, 0755); err != nil {
		return nil, "", fmt.Errorf("creating page dir: %w", err)
	}

	jsFile := ""
	hydrationJS := ""
	hasJS := false
	needsHydrate := len(emitResult.Signatures) > 0
	if needsHydrate {
		hydrationJS = renderer.GenerateNewHydrationJS(emitResult)
		if strings.TrimSpace(hydrationJS) != "" {
			hasJS = true
			if b.Cfg.ShouldMinifyJS() {
				hydrationJS = minifyJS(hydrationJS)
			}
			finalJS := strings.TrimSpace(hydrationJS)
			jsHash := hashContent([]byte(finalJS))
			jsFile = "index." + jsHash + ".js"
			jsPath := filepath.Join(pageDir, jsFile)
			os.WriteFile(jsPath, []byte(finalJS), 0644)
		} else {
			hydrationJS = ""
		}
	}

	relSrc, _ := filepath.Rel(b.Root, spp.PagePath)
	return &PageResult{
		Page:        spp.PagePath,
		OutName:     spp.OutPath,
		HTML:        emitResult.HTML,
		HeadHTML:    emitResult.HeadHTML,
		ScriptHTML:  emitResult.ScriptHTML,
		StyleHTML:   emitResult.StyleHTML,
		HydrationJS: hydrationJS,
		HasJS:       hasJS,
		JSFile:      jsFile,
		HasCSS:      bundle.CSS != "",
		UsedCSS:     emitResult.UsedCSS,
		UsedFuncs:   emitResult.UsedFuncs,
		Mode:        renderMode,
		Revalidate:  revalidate,
		SourcePath:  relSrc,
	}, bundle.CSS, nil
}
