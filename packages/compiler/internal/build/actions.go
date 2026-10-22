package build

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/evanw/esbuild/pkg/api"
	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/bundler"
	"github.com/kratejs/krate/packages/compiler/internal/lexer"
	"github.com/kratejs/krate/packages/compiler/internal/parser"
)

// actionManifest is written to .krate/actions/manifest.json and read by the
// Node action sidecar. It is deliberately outside the public output directory:
// action bundles contain server code and must never be downloadable.
type actionManifest struct {
	Actions []actionEntry `json:"actions"`
}

type actionEntry struct {
	ID     string `json:"id"`
	Export string `json:"export"`
	File   string `json:"file"`
	Source string `json:"source,omitempty"`
}

// actionModule is one file marked with the `"use server"` directive.
type actionModule struct {
	AbsPath string
	RelPath string
	Exports map[string]string // exported name -> action id
	File    string            // bundle path relative to .krate/actions
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

	files, err := discoverActionFiles(b.Root, b.Cfg.OutDir)
	if err != nil {
		return err
	}
	if len(files) == 0 {
		return nil
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("creating action bundle dir: %w", err)
	}

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
			return err
		}
		mod := &actionModule{AbsPath: abs, RelPath: rel, Exports: exports}
		reg.Modules[filepath.Clean(abs)] = mod
		reg.Order = append(reg.Order, mod)
	}

	sort.Slice(reg.Order, func(i, j int) bool { return reg.Order[i].RelPath < reg.Order[j].RelPath })
	for _, mod := range reg.Order {
		if err := b.compileActionModule(dir, mod); err != nil {
			return err
		}
		for export, id := range mod.Exports {
			reg.Entries = append(reg.Entries, actionEntry{ID: id, Export: export, File: mod.File, Source: mod.RelPath})
		}
	}
	sort.Slice(reg.Entries, func(i, j int) bool { return reg.Entries[i].ID < reg.Entries[j].ID })

	data, err := json.MarshalIndent(actionManifest{Actions: reg.Entries}, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding action manifest: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), data, 0644); err != nil {
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
		Packages:      api.PackagesExternal,
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
	result := api.Build(opts)
	if len(result.Errors) > 0 {
		return fmt.Errorf("compiling server action %s:\n%s", mod.RelPath, formatEsbuildErrors(result.Errors))
	}
	return nil
}

// discoverActionFiles walks the project for source files whose first statement
// is the `"use server"` directive. Build output, dependencies, and the framework
// runtime are excluded.
func discoverActionFiles(root, outDir string) ([]string, error) {
	outAbs, _ := filepath.Abs(outDir)
	var files []string
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
			return nil
		}
		if hasUseServerDirective(prog) {
			files = append(files, path)
		}
		return nil
	})
	return files, err
}

func isSourceFile(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".tsx", ".ts", ".mts", ".jsx", ".js", ".mjs", ".cjs":
		return true
	}
	return false
}

func parseSourceFile(path string, data []byte) (*ast.Program, error) {
	tokens := lexer.New(string(data)).Tokenize()
	p := parser.New(tokens)
	p.Filename = path
	p.SetSource(string(data))
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		return nil, fmt.Errorf("%s", parser.FormatDiagnostics(errs))
	}
	return prog, nil
}

func hasUseServerDirective(prog *ast.Program) bool {
	if prog == nil || len(prog.Body) == 0 {
		return false
	}
	stmt, ok := prog.Body[0].(*ast.ExprStmt)
	if !ok {
		return false
	}
	lit, ok := stmt.Expression.(*ast.Literal)
	if !ok || lit.Kind != ast.StringLit {
		return false
	}
	return strings.TrimSpace(lit.Value) == "use server"
}

// parseActionExports returns every exported function in a `"use server"` file.
// File-level actions are the Next.js model: every export is a server endpoint.
func parseActionExports(source, rel string) (map[string]string, error) {
	prog, err := parseSourceFile(rel, []byte(source))
	if err != nil {
		return nil, err
	}
	localFns := map[string]bool{}
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
	recordFn := func(name string) error {
		localFns[name] = true
		return addExport(name)
	}

	for _, stmt := range prog.Body {
		switch s := stmt.(type) {
		case *ast.FnDecl:
			if s.Name != "" {
				localFns[s.Name] = true
			}
			if s.Export {
				if err := recordFn(s.Name); err != nil {
					return nil, err
				}
			}
		case *ast.VarStmt:
			for _, d := range s.Decls {
				if d != nil && d.Name != "" && isFunctionExpr(d.Init) {
					localFns[d.Name] = true
				}
			}
		case *ast.ExportStmt:
			if s.ReexportSource != "" || s.StarReexport || s.Namespace != "" {
				return nil, fmt.Errorf("server action file %s cannot re-export from another module; import and re-declare the action instead", rel)
			}
			if s.Declaration != nil {
				switch d := s.Declaration.(type) {
				case *ast.FnDecl:
					if d.Name != "" {
						localFns[d.Name] = true
					}
					name := d.Name
					if s.Default || name == "" {
						name = "default"
					}
					if err := addExport(name); err != nil {
						return nil, err
					}
				case *ast.VarStmt:
					for _, vd := range d.Decls {
						if vd == nil || vd.Name == "" {
							continue
						}
						if !isFunctionExpr(vd.Init) {
							return nil, fmt.Errorf("server action file %s exports non-function %q; every export must be a function", rel, vd.Name)
						}
						localFns[vd.Name] = true
						name := vd.Name
						if s.Default {
							name = "default"
						}
						if err := addExport(name); err != nil {
							return nil, err
						}
					}
				case *ast.ExprStmt:
					if !isFunctionExpr(d.Expression) {
						return nil, fmt.Errorf("server action file %s has an unsupported default export", rel)
					}
					if err := addExport("default"); err != nil {
						return nil, err
					}
				default:
					return nil, fmt.Errorf("server action file %s exports a non-function declaration", rel)
				}
			}
			if len(s.Named) > 0 {
				for _, n := range s.Named {
					if !localFns[n.Local] {
						return nil, fmt.Errorf("server action file %s exports %q, but it is not a function declared in this file", rel, n.Local)
					}
					if err := addExport(n.Remote); err != nil {
						return nil, err
					}
				}
			}
			if s.Local != "" {
				if !localFns[s.Local] {
					return nil, fmt.Errorf("server action file %s default-exports %q, but it is not a function declared in this file", rel, s.Local)
				}
				if err := addExport("default"); err != nil {
					return nil, err
				}
			}
		}
	}
	if len(exports) == 0 {
		return nil, fmt.Errorf("server action file %s has \"use server\" but exports no functions", rel)
	}
	return exports, nil
}

func isFunctionExpr(expr ast.Expr) bool {
	_, ok := expr.(*ast.ArrowFn)
	return ok
}

func actionID(rel, export string) string {
	sum := sha256.Sum256([]byte("krate:action:v1:" + filepath.ToSlash(rel) + "\x00" + export))
	return "a" + hex.EncodeToString(sum[:9])
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
func actionServerBundlePlugin(modules map[string]map[string]string, basePath string) api.Plugin {
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
				target := bundler.ResolveImport(importer, args.Path)
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
	b.WriteString(`function __krateActionProxy(id) {
  function action() {
    if (typeof window !== "undefined" && typeof window.__krateAction === "function") {
      return window.__krateAction(id).apply(null, arguments);
    }
    return Promise.reject(new Error("Server action cannot run during SSR"));
  }
  action.actionUrl = __krateActionURLBase + "/__krate/action/" + id;
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
