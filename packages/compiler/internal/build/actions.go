package build

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/evanw/esbuild/pkg/api"
	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/bundler"
	"github.com/kratejs/krate/packages/compiler/internal/diag"
	"github.com/kratejs/krate/packages/compiler/internal/inlineaction"
	"github.com/kratejs/krate/packages/compiler/internal/jseval"
	"github.com/kratejs/krate/packages/compiler/internal/lexer"
	"github.com/kratejs/krate/packages/compiler/internal/parser"
)

// actionManifest is written to .krate/actions/manifest.json and read by the
// Node action sidecar. It is deliberately outside the public output directory:
// action bundles contain server code and must never be downloadable.
type actionManifest struct {
	Actions []actionEntry `json:"actions"`
	// Key seals the closed-over values of inline actions (see
	// inline_actions.go). It is absent when KRATE_ACTION_SECRET supplies the
	// secret or no inline action exists.
	Key string `json:"key,omitempty"`
}

type actionEntry struct {
	ID     string `json:"id"`
	Export string `json:"export"`
	File   string `json:"file"`
	Source string `json:"source,omitempty"`
	// Inline marks a hoisted inline action. Captures names the values its
	// token carries, passed as the leading arguments.
	Inline   bool     `json:"inline,omitempty"`
	Captures []string `json:"captures,omitempty"`
}

// actionModule is one file marked with the `"use server"` directive.
type actionModule struct {
	AbsPath string
	RelPath string
	Exports map[string]string // exported name -> action id
	File    string            // bundle path relative to .krate/actions
	// Inline lists the captured names of each hoisted inline action by export;
	// nil for a regular "use server" module.
	Inline map[string][]string
}

type actionRegistry struct {
	Modules map[string]*actionModule // clean absolute source path -> module
	Order   []*actionModule
	Entries []actionEntry
}

func (b *Builder) prepareActions() error {
	dir := filepath.Join(b.Root, ".krate", "actions")
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("clearing action bundles: %w", err)
	}
	b.actions = nil

	files, inlineFiles, err := discoverActionSources(b.Root, b.Cfg.OutDir)
	if err != nil {
		return err
	}
	if len(files) == 0 && len(inlineFiles) == 0 {
		return nil
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("creating action bundle dir: %w", err)
	}

	var errs []error
	reg := &actionRegistry{Modules: make(map[string]*actionModule, len(files))}
	for _, abs := range files {
		rel, err := filepath.Rel(b.Root, abs)
		if err != nil {
			return fmt.Errorf("action path %s: %w", abs, err)
		}
		rel = filepath.ToSlash(rel)
		data, err := os.ReadFile(abs)
		if err != nil {
			return fmt.Errorf("reading action %s: %w", rel, err)
		}
		exports, err := parseActionExports(string(data), rel)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		mod := &actionModule{AbsPath: abs, RelPath: rel, Exports: exports}
		reg.Modules[filepath.Clean(abs)] = mod
		reg.Order = append(reg.Order, mod)
	}

	inlineMods, err := b.hoistInlineModules(dir, inlineFiles)
	if err != nil {
		return err
	}
	reg.Order = append(reg.Order, inlineMods...)

	sort.Slice(reg.Order, func(i, j int) bool { return reg.Order[i].RelPath < reg.Order[j].RelPath })
	for _, mod := range reg.Order {
		if err := b.compileActionModule(dir, mod); err != nil {
			errs = append(errs, err)
			continue
		}
		for export, id := range mod.Exports {
			e := actionEntry{ID: id, Export: export, File: mod.File, Source: mod.RelPath}
			if caps, ok := mod.Inline[export]; ok {
				e.Inline, e.Captures = true, caps
			}
			reg.Entries = append(reg.Entries, e)
		}
	}
	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	sort.Slice(reg.Entries, func(i, j int) bool { return reg.Entries[i].ID < reg.Entries[j].ID })

	manifest := actionManifest{Actions: reg.Entries}
	perm := os.FileMode(0644)
	if len(inlineMods) > 0 {
		secret, fromEnv, err := b.actionSecret()
		if err != nil {
			return err
		}
		if !fromEnv {
			manifest.Key = secret
			perm = 0600
		}
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding action manifest: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), data, perm); err != nil {
		return fmt.Errorf("writing action manifest: %w", err)
	}
	b.actions = reg
	// Action rewrites are applied while parsing importer ASTs. A cache created
	// before an action was added would otherwise miss the rewrite.
	b.moduleCache = bundler.NewModuleCache()
	return nil
}

func (b *Builder) compileActionModule(dir string, mod *actionModule) error {
	data, err := os.ReadFile(mod.AbsPath)
	if err != nil {
		return fmt.Errorf("reading action %s: %w", mod.RelPath, err)
	}
	name := strings.TrimSuffix(filepath.Base(mod.RelPath), filepath.Ext(mod.RelPath))
	name = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			return r
		}
		return '-'
	}, name)
	mod.File = name + "-" + hashContent(data) + ".mjs"
	outPath := filepath.Join(dir, mod.File)

	opts := api.BuildOptions{
		AbsWorkingDir: b.Root,
		EntryPoints:   []string{mod.AbsPath},
		Bundle:        true,
		Format:        api.FormatESModule,
		Platform:      api.PlatformNode,
		Plugins:       []api.Plugin{jseval.MarkerPackagesPlugin(), externalNodeModulesPlugin()},
		Outfile:       outPath,
		Write:         true,
		Sourcemap:     sourceMapMode(b.Cfg.Sourcemap),
		LogLevel:      api.LogLevelSilent,
		Metafile:      true,
	}
	if b.Cfg.ShouldMinifyJS() {
		opts.MinifyWhitespace = true
		opts.MinifyIdentifiers = true
		opts.MinifySyntax = true
	}
	withAuthConfig(&opts, b.Cfg)
	result := esbuildBuild(opts)
	if len(result.Errors) > 0 {
		return fmt.Errorf("compiling server action %s:\n%s", mod.RelPath, formatEsbuildErrors(result.Errors))
	}
	return nil
}

// discoverActionSources walks the project for source files whose first
// statement is the `"use server"` directive, and also returns the other source
// files that mention "use server": candidates for inline actions. Build output,
// dependencies, and the framework runtime are excluded.
func discoverActionSources(root, outDir string) ([]string, []string, error) {
	outAbs, _ := filepath.Abs(outDir)
	var files, inline []string
	var misplaced []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			name := d.Name()
			if path != root {
				switch name {
				case "node_modules", ".git", ".krate", "dist", "public", "vendor":
					return filepath.SkipDir
				}
			}
			if outAbs != "" && filepath.Clean(path) == filepath.Clean(outAbs) {
				return filepath.SkipDir
			}
			return nil
		}
		if !isSourceFile(path) {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil || !strings.Contains(string(data), "use server") {
			return nil
		}
		prog, perrs := parseSourceFile(path, data)
		if perrs != nil {
			inline = append(inline, path)
			return nil
		}
		found, first := useServerDirective(prog)
		if found && first {
			files = append(files, path)
		} else if found {
			misplaced = append(misplaced, path)
		} else {
			inline = append(inline, path)
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	if len(misplaced) > 0 {
		return nil, nil, fmt.Errorf("\"use server\" must be the first statement in %s", strings.Join(misplaced, ", "))
	}
	return files, inline, nil
}

func isSourceFile(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".tsx", ".ts", ".mts", ".jsx", ".js", ".mjs", ".cjs":
		return true
	}
	return false
}

func parseSourceFile(path string, data []byte) (*ast.Program, error) {
	tokens := lexer.ForFile(path, string(data)).Tokenize()
	p := parser.New(tokens)
	p.Filename = path
	p.SetSource(string(data))
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		return nil, fmt.Errorf("%s", parser.FormatDiagnostics(errs))
	}
	return prog, nil
}

func useServerDirective(prog *ast.Program) (found, first bool) {
	if prog == nil {
		return false, false
	}
	for i, stmt := range prog.Body {
		expr, ok := stmt.(*ast.ExprStmt)
		if !ok {
			continue
		}
		lit, ok := expr.Expression.(*ast.Literal)
		if ok && lit.Kind == ast.StringLit && strings.TrimSpace(lit.Value) == "use server" {
			return true, i == 0
		}
	}
	return false, false
}

// parseActionExports returns every export of a `"use server"` file with its
// action id. File-level actions are the Next.js model: every export is a server
// endpoint. An export may be any expression (`export const save =
// createAction(...)`): whether its value is a function is checked when the
// module loads, because a wrapper call cannot be judged from syntax. Exports
// that can never be functions (literals, objects, arrays, classes) are
// rejected here.
func parseActionExports(source, rel string) (map[string]string, error) {
	prog, err := parseSourceFile(rel, []byte(source))
	if err != nil {
		return nil, err
	}
	// bindings maps each top-level binding to its initializer when it is a
	// single `const`/`let`/`var` (nil otherwise), so `export { n }` can be
	// judged by what `n` was declared as.
	bindings := map[string]ast.Expr{}
	classes := map[string]bool{}
	for _, stmt := range prog.Body {
		collectActionBindings(stmt, bindings, classes)
	}

	exports := map[string]string{}
	addExport := func(name string) error {
		name = strings.TrimSpace(name)
		if name == "" {
			return fmt.Errorf("server action file %s exports a function without a name", rel)
		}
		if _, exists := exports[name]; exists {
			return nil
		}
		exports[name] = actionID(rel, name)
		return nil
	}
	notFunction := func(pos ast.Pos, name, what string) error {
		return diag.New(rel, pos.Line, pos.Col,
			fmt.Sprintf("server action module exports %s %q; every export must be a function", what, name),
			"export an async function (or a call that returns one, such as a wrapper), or move the value into a module without \"use server\"", "").WithCode(diag.CodeActionExportNotFunction)
	}

	for _, stmt := range prog.Body {
		s, ok := stmt.(*ast.ExportStmt)
		if !ok {
			continue
		}
		if s.StarReexport || s.Namespace != "" {
			return nil, diag.New(rel, s.Position.Line, s.Position.Col,
				"a \"use server\" module cannot use `export *` or `export * as`",
				"name the actions you re-export: export { save } from \"./other\"", "").WithCode(diag.CodeActionReexport)
		}
		if s.ReexportSource != "" {
			for _, n := range s.ReexportNamed {
				remote := n.Remote
				if remote == "" {
					remote = n.Local
				}
				if err := addExport(remote); err != nil {
					return nil, err
				}
			}
			continue
		}
		switch d := s.Declaration.(type) {
		case nil:
		case *ast.FnDecl:
			name := d.Name
			if s.Default || name == "" {
				name = "default"
			}
			if err := addExport(name); err != nil {
				return nil, err
			}
		case *ast.VarStmt:
			for _, vd := range d.Decls {
				if vd == nil || (vd.Name == "" && !vd.IsDestructuring) {
					continue
				}
				if vd.IsDestructuring {
					return nil, diag.New(rel, s.Position.Line, s.Position.Col,
						"server action module exports a destructured binding; every export must be a named function",
						"export each action as its own `export const name = ...`", "").WithCode(diag.CodeActionExportNotFunction)
				}
				if neverCallable(vd.Init) {
					return nil, notFunction(s.Position, vd.Name, "non-function")
				}
				name := vd.Name
				if s.Default {
					name = "default"
				}
				if err := addExport(name); err != nil {
					return nil, err
				}
			}
		case *ast.ClassDecl:
			return nil, notFunction(s.Position, d.Name, "the class")
		case *ast.ExprStmt:
			if neverCallable(d.Expression) {
				return nil, notFunction(s.Position, "default", "a non-function default")
			}
			if err := addExport("default"); err != nil {
				return nil, err
			}
		default:
			return nil, diag.New(rel, s.Position.Line, s.Position.Col,
				"server action module exports a non-function declaration",
				"export only functions (or calls that return one) from a \"use server\" module", "").WithCode(diag.CodeActionExportNotFunction)
		}
		for _, n := range s.Named {
			if err := checkLocalActionExport(rel, s.Position, n.Local, bindings, classes, notFunction); err != nil {
				return nil, err
			}
			remote := n.Remote
			if remote == "" {
				remote = n.Local
			}
			if err := addExport(remote); err != nil {
				return nil, err
			}
		}
		if s.Declaration == nil && s.Default && s.Local != "" {
			if err := checkLocalActionExport(rel, s.Position, s.Local, bindings, classes, notFunction); err != nil {
				return nil, err
			}
			if err := addExport("default"); err != nil {
				return nil, err
			}
		}
	}
	if len(exports) == 0 {
		return nil, fmt.Errorf("server action file %s has \"use server\" but exports no functions", rel)
	}
	return exports, nil
}

// collectActionBindings records the top-level bindings of a module.
func collectActionBindings(stmt ast.Stmt, bindings map[string]ast.Expr, classes map[string]bool) {
	switch s := stmt.(type) {
	case *ast.ExportStmt:
		if s.Declaration != nil {
			collectActionBindings(s.Declaration, bindings, classes)
		}
	case *ast.FnDecl:
		if s.Name != "" {
			bindings[s.Name] = nil
		}
	case *ast.ClassDecl:
		if s.Name != "" {
			bindings[s.Name] = nil
			classes[s.Name] = true
		}
	case *ast.VarStmt:
		for _, d := range s.Decls {
			if d == nil {
				continue
			}
			if d.IsDestructuring {
				for _, n := range d.Names {
					bindings[n] = nil
				}
				continue
			}
			if d.Name != "" {
				bindings[d.Name] = d.Init
			}
		}
	case *ast.ImportStmt:
		if s.Default != "" {
			bindings[s.Default] = nil
		}
		if s.Namespace != "" {
			bindings[s.Namespace] = nil
		}
		for _, n := range s.Named {
			local := n.Local
			if local == "" {
				local = n.Remote
			}
			bindings[local] = nil
		}
	}
}

func checkLocalActionExport(rel string, pos ast.Pos, local string, bindings map[string]ast.Expr, classes map[string]bool,
	notFunction func(ast.Pos, string, string) error) error {
	init, ok := bindings[local]
	if !ok {
		return diag.New(rel, pos.Line, pos.Col,
			fmt.Sprintf("server action module exports %q, but it is not declared in this file", local),
			"export a function declared or imported in this file", "").WithCode(diag.CodeActionExportNotFunction)
	}
	if classes[local] {
		return notFunction(pos, local, "the class")
	}
	if neverCallable(init) {
		return notFunction(pos, local, "non-function")
	}
	return nil
}

// neverCallable reports whether an expression can only produce a value that is
// not a function (a literal, object, array, template, class or JSX element).
// Anything else, a call, identifier or conditional, might be a function and is
// checked when the module loads.
func neverCallable(expr ast.Expr) bool {
	switch e := expr.(type) {
	case *ast.Literal, *ast.ObjectExpr, *ast.ArrayExpr, *ast.TemplateExpr, *ast.ClassExpr, *ast.JSXElement, *ast.JSXFragment:
		return true
	case *ast.TypeAssertion:
		return neverCallable(e.Expr)
	case *ast.ParenExpr:
		return neverCallable(e.Expr)
	}
	return false
}

func actionID(rel, export string) string {
	return inlineaction.ID(rel, export)
}

func (b *Builder) actionModules() map[string]map[string]string {
	if b.actions == nil {
		return nil
	}
	out := make(map[string]map[string]string, len(b.actions.Modules))
	for path, mod := range b.actions.Modules {
		cp := make(map[string]string, len(mod.Exports))
		for name, id := range mod.Exports {
			cp[name] = id
		}
		out[path] = cp
	}
	return out
}

// actionServerBundlePlugin keeps server-action source out of SSR page bundles.
// Imports of a `"use server"` module resolve to a small proxy that renders as
// the action URL during SSR and is never executed on the server.
func actionServerBundlePlugin(res *bundler.ImportResolver, modules map[string]map[string]string, basePath string) api.Plugin {
	if len(modules) == 0 {
		return api.Plugin{Name: "krate-actions-empty", Setup: func(api.PluginBuild) {}}
	}
	return api.Plugin{
		Name: "krate-actions",
		Setup: func(pb api.PluginBuild) {
			pb.OnResolve(api.OnResolveOptions{Filter: `.*`}, func(args api.OnResolveArgs) (api.OnResolveResult, error) {
				if args.Path == "" {
					return api.OnResolveResult{}, nil
				}
				importer := filepath.Join(args.ResolveDir, "__krate_server__.tsx")
				target := res.Resolve(importer, args.Path)
				if target == "" {
					return api.OnResolveResult{}, nil
				}
				if _, ok := modules[filepath.Clean(target)]; !ok {
					return api.OnResolveResult{}, nil
				}
				return api.OnResolveResult{Path: filepath.Clean(target), Namespace: "krate-action"}, nil
			})
			pb.OnLoad(api.OnLoadOptions{Filter: `.*`, Namespace: "krate-action"}, func(args api.OnLoadArgs) (api.OnLoadResult, error) {
				exports := modules[filepath.Clean(args.Path)]
				if exports == nil {
					return api.OnLoadResult{}, nil
				}
				source := actionProxyModule(exports, basePath)
				return api.OnLoadResult{Contents: &source, Loader: api.LoaderJS}, nil
			})
		},
	}
}

func actionProxyModule(exports map[string]string, basePath string) string {
	baseJSON, _ := json.Marshal(basePath)
	var b strings.Builder
	b.WriteString("const __krateActionURLBase = ")
	b.Write(baseJSON)
	b.WriteString(";\n")
	b.WriteString(`function __krateActionProxy(id, bound) {
  bound = bound || [];
  function action() {
    var args = bound.concat(Array.prototype.slice.call(arguments));
    if (typeof window !== "undefined" && typeof window.__krateAction === "function") {
      return window.__krateAction(id).apply(null, args);
    }
    return Promise.reject(new Error("Server action cannot run during SSR"));
  }
  action.actionUrl = __krateActionURLBase + "/__krate/action/" + id;
  action.$$krateAction = id;
  action.$$krateBound = bound;
  action.bind = function (_this) {
    return __krateActionProxy(id, bound.concat(Array.prototype.slice.call(arguments, 1)));
  };
  action.toString = function () { return this.actionUrl; };
  return action;
}
`)
	names := make([]string, 0, len(exports))
	for name := range exports {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		id, _ := json.Marshal(exports[name])
		if name == "default" {
			b.WriteString("export default __krateActionProxy(")
			b.Write(id)
			b.WriteString(");\n")
			continue
		}
		b.WriteString("export const ")
		b.WriteString(name)
		b.WriteString(" = __krateActionProxy(")
		b.Write(id)
		b.WriteString(");\n")
	}
	return b.String()
}

func actionManifestPath(root string) string {
	return filepath.Join(root, ".krate", "actions", "manifest.json")
}

func hasActionManifest(root string) bool {
	data, err := os.ReadFile(actionManifestPath(root))
	if err != nil {
		return false
	}
	var m actionManifest
	if json.Unmarshal(data, &m) != nil {
		return false
	}
	return len(m.Actions) > 0
}

// isUseServerFile is a cheap prefilter for page discovery. Action files are not
// routes even when they live under the pages directory.
func isUseServerFile(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	buf := make([]byte, 4096)
	n, _ := f.Read(buf)
	src := strings.TrimSpace(string(buf[:n]))
	return strings.HasPrefix(src, "\"use server\"") || strings.HasPrefix(src, "'use server'")
}
