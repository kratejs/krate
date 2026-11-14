package build

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/evanw/esbuild/pkg/api"
	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/tsstrip"
)

// pureModuleMark tags the inner resolve of purePrunePlugin so the plugin does
// not resolve its own request again.
type pureModuleMark struct{}

// purePrunePlugin lets esbuild drop a project module that nothing uses and
// whose top level only declares things (imports, exports, functions,
// constants), as webpack does for a side-effect-free module. Without it a
// barrel that re-exports a client component next to a server helper keeps the
// helper in the client bundle for the imports it makes (Node built-ins, native
// addons, database clients), although no client code reads it. Modules with
// any top-level statement that may have an effect are never marked, and
// packages keep their own `sideEffects` flag.
func purePrunePlugin(projectRoot string) api.Plugin {
	root := canonicalPath(projectRoot)
	return api.Plugin{
		Name: "krate-pure-modules",
		Setup: func(pb api.PluginBuild) {
			pb.OnResolve(api.OnResolveOptions{Filter: `.*`}, func(args api.OnResolveArgs) (api.OnResolveResult, error) {
				return pureResolve(pb, root, args)
			})
		},
	}
}

func pureResolve(pb api.PluginBuild, root string, args api.OnResolveArgs) (api.OnResolveResult, error) {

	if _, inner := args.PluginData.(pureModuleMark); inner || args.Kind == api.ResolveEntryPoint || args.Importer == "" ||
		args.Namespace != "file" || strings.Contains(filepath.ToSlash(args.Importer), "/node_modules/") {
		return api.OnResolveResult{}, nil
	}
	r := pb.Resolve(args.Path, api.ResolveOptions{
		Importer: args.Importer, Namespace: args.Namespace, ResolveDir: args.ResolveDir,
		Kind: args.Kind, PluginData: pureModuleMark{}, With: args.With,
	})
	if len(r.Errors) > 0 || r.External || r.Namespace != "file" || r.Path == "" {
		return api.OnResolveResult{}, nil
	}
	if !withinDir(root, canonicalPath(r.Path)) || strings.Contains(filepath.ToSlash(r.Path), "/node_modules/") || !isPureModule(r.Path) {
		return api.OnResolveResult{}, nil
	}
	return api.OnResolveResult{Path: r.Path, Namespace: r.Namespace, Suffix: r.Suffix, SideEffects: api.SideEffectsFalse}, nil
}

func withinDir(dir, path string) bool {
	rel, err := filepath.Rel(dir, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

type pureEntry struct {
	mtimeNS, size int64
	pure          bool
}

var pureModuleCache sync.Map // path -> pureEntry

var decoratorLineRe = regexp.MustCompile(`(?m)^\s*@[A-Za-z_]`)

// isPureModule reports whether the module's top level has no statement that
// may have an effect when evaluated.
func isPureModule(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".ts", ".tsx", ".js", ".jsx", ".mjs", ".mts":
	default:
		return false
	}
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	if v, ok := pureModuleCache.Load(path); ok {
		if e := v.(pureEntry); e.mtimeNS == info.ModTime().UnixNano() && e.size == info.Size() {
			return e.pure
		}
	}
	pure := false
	if data, err := os.ReadFile(path); err == nil && !decoratorLineRe.Match(data) {
		if prog, errs := tsstrip.Parse(path, string(data), tsstrip.Options{}); len(errs) == 0 && prog != nil {
			pure = programIsPure(prog)
		}
	}
	pureModuleCache.Store(path, pureEntry{mtimeNS: info.ModTime().UnixNano(), size: info.Size(), pure: pure})
	return pure
}

func programIsPure(prog *ast.Program) bool {
	for _, stmt := range prog.Body {
		if !pureTopStmt(stmt) {
			return false
		}
	}
	return true
}

func pureTopStmt(stmt ast.Stmt) bool {
	switch s := stmt.(type) {
	case *ast.ImportStmt:
		// A bare `import "./x"` runs for its effects.
		return s.Default != "" || s.Namespace != "" || len(s.Named) > 0
	case *ast.ExportStmt:
		if s.Declaration == nil {
			return true
		}
		return pureTopStmt(s.Declaration)
	case *ast.FnDecl, *ast.EmptyStmt:
		return true
	case *ast.ClassDecl:
		return pureClass(s)
	case *ast.VarStmt:
		for _, d := range s.Decls {
			if d.Init != nil && !pureExpr(d.Init) {
				return false
			}
		}
		return true
	case *ast.ExprStmt:
		lit, ok := s.Expression.(*ast.Literal)
		return ok && lit.Kind == ast.StringLit
	}
	return false
}

func pureClass(c *ast.ClassDecl) bool {
	if c.SuperClass != nil && !pureExpr(c.SuperClass) {
		return false
	}
	for _, m := range c.Members {
		if m.Computed || m.Kind == ast.ClassStaticBlock || (m.Static && m.Init != nil) {
			return false
		}
	}
	return true
}

// pureLazyCallees are React helpers whose call only builds an object.
var pureLazyCallees = map[string]bool{"lazy": true, "memo": true, "forwardRef": true}

func pureExpr(e ast.Expr) bool {
	switch x := e.(type) {
	case nil:
		return true
	case *ast.Identifier, *ast.Literal, *ast.ArrowFn, *ast.ThisExpr:
		return true
	case *ast.ParenExpr:
		return pureExpr(x.Expr)
	case *ast.TypeAssertion:
		return pureExpr(x.Expr)
	case *ast.TemplateExpr:
		for _, p := range x.Parts {
			if !pureExpr(p) {
				return false
			}
		}
		return true
	case *ast.ArrayExpr:
		for _, el := range x.Elements {
			if !pureExpr(el) {
				return false
			}
		}
		return true
	case *ast.ObjectExpr:
		for _, p := range x.Properties {
			if p.Spread || p.Getter || p.Setter || (p.Computed && !pureExpr(p.KeyExpr)) || !pureExpr(p.Value) {
				return false
			}
		}
		return true
	case *ast.UnaryExpr:
		return x.Op != "delete" && !x.Postfix && x.Op != "++" && x.Op != "--" && pureExpr(x.Arg)
	case *ast.BinaryExpr:
		return pureExpr(x.Left) && pureExpr(x.Right)
	case *ast.ConditionalExpr:
		return pureExpr(x.Test) && pureExpr(x.Consequent) && pureExpr(x.Alternate)
	case *ast.MemberExpr:
		return pureExpr(x.Object) && (!x.Computed || pureExpr(x.Property))
	case *ast.CallExpr:
		name := ""
		switch c := x.Callee.(type) {
		case *ast.Identifier:
			name = c.Name
		case *ast.MemberExpr:
			if id, ok := c.Property.(*ast.Identifier); ok && !c.Computed {
				name = id.Name
			}
		}
		if !pureLazyCallees[name] || x.Optional {
			return false
		}
		for _, a := range x.Args {
			if !pureExpr(a) {
				return false
			}
		}
		return true
	}
	return false
}
