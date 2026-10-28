package build

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/evanw/esbuild/pkg/api"
	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/gotsgen"
	"github.com/kratejs/krate/packages/compiler/internal/irtree"
	"github.com/kratejs/krate/packages/compiler/internal/tsstrip"
)

// This file lets an SSR/ISR page's server bundle (compiled by esbuild from raw
// TSX) handle `.go` component imports, which esbuild has no loader for:
//
//  1. annotateGoCalls injects a `__krateGoId` prop (irtree.GoCallID) at every
//     Go-component call site, so a shim can identify its exact call site.
//  2. goServerBundlePlugin resolves `.go` imports to a generated JS shim whose
//     exports delegate to @krate/runtime/server's goComponent(). Server-tier
//     components inline their build-time baked HTML (page.GoBaked); runtime-tier
//     components emit a marker the Go server resolves over the host.

// goComponentLocals returns the local JSX names in a page/module that resolve to
// Go components (`import { X } from "./x.go"`). Returns nil when the source has
// no `.go` imports.
func goComponentLocals(srcPath, src, root string) map[string]bool {
	prog := parseProgram(srcPath, src)
	if prog == nil {
		return nil
	}
	out := map[string]bool{}
	for _, stmt := range prog.Body {
		imp, ok := stmt.(*ast.ImportStmt)
		if !ok {
			continue
		}
		module := strings.Trim(imp.Source, "\"'")
		if !strings.HasSuffix(module, ".go") {
			continue
		}
		abs := module
		if !filepath.IsAbs(abs) {
			abs = filepath.Join(filepath.Dir(srcPath), module)
		}
		comps := goComponentsOfFile(abs)
		if len(comps) == 0 {
			continue
		}
		byName := make(map[string]bool, len(comps))
		for _, c := range comps {
			byName[c.Name] = true
		}
		for _, ni := range imp.Named {
			if byName[ni.Remote] {
				out[ni.Local] = true
			}
		}
		if imp.Default != "" && len(comps) == 1 {
			out[imp.Default] = true
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// goComponentsOfFile parses a `.go` file and returns its exported components.
func goComponentsOfFile(abs string) []gotsgen.Component {
	data, err := os.ReadFile(abs)
	if err != nil {
		return nil
	}
	comps, err := gotsgen.ParseFile(abs, data)
	if err != nil {
		return nil
	}
	return comps
}

// annotateGoCalls inserts `__krateGoId="<callid>"` after the tag name of every
// Go-component JSX element in src. Ids are derived from the element's source
// position (irtree.GoCallID), matching the emitter's build-time render ids.
func annotateGoCalls(src, srcPath string, locals map[string]bool) string {
	if len(locals) == 0 {
		return src
	}
	prog := parseProgram(srcPath, src)
	if prog == nil {
		return src
	}

	type insertion struct {
		at   int
		text string
	}
	var inserts []insertion

	var walkExpr func(ast.Expr)
	var walkChild func(ast.JSXChild)
	var walkStmt func([]ast.Stmt)

	addJSX := func(v *ast.JSXElement) {
		if v.Opening != nil && locals[v.Opening.Name] {
			if at, ok := tagNameEnd(src, v.Position, v.Opening.Name); ok {
				id := irtree.GoCallID(v.Position.Line, v.Position.Col, v.Opening.Name)
				inserts = append(inserts, insertion{at, fmt.Sprintf(" __krateGoId=%q", id)})
			}
		}
	}

	walkChild = func(c ast.JSXChild) {
		switch ch := c.(type) {
		case *ast.JSXExprContainer:
			walkExpr(ch.Expression)
		case *ast.JSXElementChild:
			walkExpr(ch.Element)
		case *ast.JSXFragmentChild:
			walkExpr(ch.Fragment)
		}
	}

	walkExpr = func(e ast.Expr) {
		if e == nil {
			return
		}
		switch v := e.(type) {
		case *ast.JSXElement:
			addJSX(v)
			for _, c := range v.Children {
				walkChild(c)
			}
		case *ast.JSXFragment:
			for _, c := range v.Children {
				walkChild(c)
			}
		case *ast.CallExpr:
			walkExpr(v.Callee)
			for _, a := range v.Args {
				walkExpr(a)
			}
		case *ast.MemberExpr:
			walkExpr(v.Object)
			walkExpr(v.Property)
		case *ast.BinaryExpr:
			walkExpr(v.Left)
			walkExpr(v.Right)
		case *ast.UnaryExpr:
			walkExpr(v.Arg)
		case *ast.ConditionalExpr:
			walkExpr(v.Test)
			walkExpr(v.Consequent)
			walkExpr(v.Alternate)
		case *ast.TemplateExpr:
			for _, p := range v.Parts {
				walkExpr(p)
			}
		case *ast.ArrowFn:
			walkStmt(v.Body)
		case *ast.AwaitExpr:
			walkExpr(v.Arg)
		case *ast.NewExpr:
			walkExpr(v.Callee)
			for _, a := range v.Args {
				walkExpr(a)
			}
		case *ast.ObjectExpr:
			for _, p := range v.Properties {
				if p.Value != nil {
					walkExpr(p.Value)
				}
			}
		case *ast.ArrayExpr:
			for _, el := range v.Elements {
				walkExpr(el)
			}
		}
	}

	walkStmt = func(stmts []ast.Stmt) {
		for _, stmt := range stmts {
			switch s := stmt.(type) {
			case *ast.ExprStmt:
				walkExpr(s.Expression)
			case *ast.VarStmt:
				for _, d := range s.Decls {
					if d.Init != nil {
						walkExpr(d.Init)
					}
				}
			case *ast.FnDecl:
				walkStmt(s.Body)
			case *ast.ReturnStmt:
				if s.Value != nil {
					walkExpr(s.Value)
				}
			case *ast.BlockStmt:
				walkStmt(s.Body)
			case *ast.IfStmt:
				walkExpr(s.Test)
				walkStmt(s.Consequent)
				walkStmt(s.Alternate)
			case *ast.ForStmt:
				walkExpr(s.Test)
				walkStmt(s.Body)
			case *ast.ForInStmt:
				walkExpr(s.Right)
				walkStmt(s.Body)
			case *ast.WhileStmt:
				walkExpr(s.Test)
				walkStmt(s.Body)
			case *ast.DoWhileStmt:
				walkStmt(s.Body)
				walkExpr(s.Test)
			case *ast.SwitchStmt:
				walkExpr(s.Discriminant)
				for _, c := range s.Cases {
					if c.Test != nil {
						walkExpr(c.Test)
					}
					walkStmt(c.Body)
				}
			case *ast.TryStmt:
				walkStmt(s.Body)
				if s.Catch != nil {
					walkStmt(s.Catch.Body)
				}
				walkStmt(s.Finally)
			case *ast.ThrowStmt:
				walkExpr(s.Value)
			case *ast.ExportStmt:
				if s.Declaration != nil {
					walkStmt([]ast.Stmt{s.Declaration})
				}
			}
		}
	}

	walkStmt(prog.Body)
	if len(inserts) == 0 {
		return src
	}
	// Apply right-to-left so earlier offsets stay valid.
	sort.Slice(inserts, func(i, j int) bool { return inserts[i].at > inserts[j].at })
	out := src
	for _, in := range inserts {
		if in.at < 0 || in.at > len(out) {
			continue
		}
		out = out[:in.at] + in.text + out[in.at:]
	}
	return out
}

// parseProgram parses page/module source into an AST (nil on a TypeScript
// syntax error). TypeScript is stripped first and positions refer to src, so
// JSX positions match the page AST the emitter derives Go call ids from.
func parseProgram(path, src string) *ast.Program {
	prog, _ := tsstrip.Parse(path, src, tsstrip.Options{})
	return prog
}

// loaderForExt maps a JS/TS file extension to an esbuild loader.
func loaderForExt(path string) api.Loader {
	switch filepath.Ext(path) {
	case ".ts":
		return api.LoaderTS
	case ".tsx":
		return api.LoaderTSX
	case ".jsx":
		return api.LoaderJSX
	default:
		return api.LoaderJS
	}
}

// tagNameEnd returns the byte offset immediately after the tag name of the
// JSX element starting at p (p.Offset is the byte offset of its `<`).
func tagNameEnd(src string, p ast.Pos, name string) (int, bool) {
	i := p.Offset
	if i < 0 || i >= len(src) || src[i] != '<' {
		return 0, false
	}
	i++
	for i < len(src) && (src[i] == ' ' || src[i] == '\t' || src[i] == '\n' || src[i] == '\r') {
		i++
	}
	if !strings.HasPrefix(src[i:], name) {
		return 0, false
	}
	return i + len(name), true
}

// goServerBundlePlugin resolves/loads `.go` imports and the `krate:go-baked`
// virtual module for one SSR page bundle. baked keys are Go call-site ids
// (irtree.GoCallID) mapped to build-time rendered HTML. root is the project
// root, used to read a component file's `// krate:revalidate` directive (baked
// into the shim so the marker needs no name-based lookup at serve time).
func goServerBundlePlugin(baked map[string]string, root string) api.Plugin {
	return api.Plugin{
		Name: "krate-go",
		Setup: func(pb api.PluginBuild) {
			pb.OnResolve(api.OnResolveOptions{Filter: `^krate:go-baked$`}, func(api.OnResolveArgs) (api.OnResolveResult, error) {
				return api.OnResolveResult{Path: "krate:go-baked", Namespace: "krate-go-baked"}, nil
			})
			pb.OnLoad(api.OnLoadOptions{Filter: `.*`, Namespace: "krate-go-baked"}, func(api.OnLoadArgs) (api.OnLoadResult, error) {
				j, _ := json.Marshal(baked)
				contents := "export default " + string(j) + ";\n"
				return api.OnLoadResult{Contents: &contents, Loader: api.LoaderJS}, nil
			})

			pb.OnResolve(api.OnResolveOptions{Filter: `\.go$`}, func(args api.OnResolveArgs) (api.OnResolveResult, error) {
				abs := args.Path
				if !filepath.IsAbs(abs) {
					abs = filepath.Join(args.ResolveDir, abs)
				}
				return api.OnResolveResult{Path: abs, Namespace: "krate-go"}, nil
			})
			pb.OnLoad(api.OnLoadOptions{Filter: `.*`, Namespace: "krate-go"}, func(args api.OnLoadArgs) (api.OnLoadResult, error) {
				contents := goShimSource(goComponentsOfFile(args.Path), regionRevalidate(root, args.Path))
				return api.OnLoadResult{Contents: &contents, Loader: api.LoaderJS}, nil
			})

			// Nested project modules (imported components) that themselves call a
			// Go component are annotated too, so their call sites can inline baked
			// HTML. Project files are the only ones loaded (packages are external).
			pb.OnLoad(api.OnLoadOptions{Filter: `\.[jt]sx?$`, Namespace: "file"}, func(args api.OnLoadArgs) (api.OnLoadResult, error) {
				data, err := os.ReadFile(args.Path)
				if err != nil {
					return api.OnLoadResult{}, err
				}
				src := annotateGoModuleSource(args.Path, string(data))
				return api.OnLoadResult{Contents: &src, Loader: loaderForExt(args.Path)}, nil
			})
		},
	}
}

// annotateGoModuleSource annotates the Go-component call sites of a nested
// project module so they can inline baked HTML in the server bundle.
func annotateGoModuleSource(path, src string) string {
	if strings.Contains(src, ".go\"") || strings.Contains(src, ".go'") {
		if locals := goComponentLocals(path, src, ""); len(locals) > 0 {
			src = annotateGoCalls(src, path, locals)
		}
	}
	return src
}

// goShimSource builds a JS module exporting each Go component as a function that
// delegates to @krate/runtime/server's goComponent bridge. revalidate is the
// component file's per-region cache window (0 = none), baked into each shim so
// no name-based lookup is needed at serve time.
func goShimSource(comps []gotsgen.Component, revalidate int) string {
	var b strings.Builder
	b.WriteString("import { goComponent } from \"@krate/runtime/server\";\n")
	b.WriteString("import baked from \"krate:go-baked\";\n")
	for _, c := range comps {
		tier := string(c.Tier)
		if tier == "" {
			tier = "server"
		}
		fmt.Fprintf(&b, "export const %s = (props) => goComponent(%q, %q, baked, props, %d);\n", c.Name, c.Name, tier, revalidate)
	}
	if len(comps) == 1 {
		fmt.Fprintf(&b, "export default %s;\n", comps[0].Name)
	}
	return b.String()
}
