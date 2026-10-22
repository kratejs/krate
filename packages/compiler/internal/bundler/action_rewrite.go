package bundler

import (
	"fmt"
	"path/filepath"
	"sort"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/diag"
)

// SetActionModules configures the server-action modules discovered by the
// build. Keys are clean absolute source paths; values map an exported action
// name to its public action id.
func (b *Bundler) SetActionModules(modules map[string]map[string]string) {
	b.actionModules = make(map[string]map[string]string, len(modules))
	for path, exports := range modules {
		cp := make(map[string]string, len(exports))
		for name, id := range exports {
			cp[name] = id
		}
		b.actionModules[filepath.Clean(path)] = cp
	}
}

// SetActionBasePath sets the deployment base path prepended to action URLs.
func (b *Bundler) SetActionBasePath(base string) {
	b.actionBasePath = base
}

// rewriteActionImports replaces imports of `"use server"` modules with client
// proxies. The action source itself is never added to the page module graph, so
// server code cannot leak into the hydration bundle.
func (b *Bundler) rewriteActionImports(prog *ast.Program, abs string) (map[string]bool, bool) {
	if prog == nil || len(b.actionModules) == 0 {
		return nil, false
	}
	handled := map[string]bool{}
	changed := false

	var body []ast.Stmt
	for _, stmt := range prog.Body {
		imp, ok := stmt.(*ast.ImportStmt)
		if !ok || imp.Source == "" {
			body = append(body, stmt)
			continue
		}
		src := trimImportSource(imp.Source)
		target := b.resolveImportForModule(abs, src)
		exports, ok := b.actionModules[filepath.Clean(target)]
		if !ok {
			body = append(body, stmt)
			continue
		}
		changed = true
		handled[src] = true
		decls := make([]*ast.VarDecl, 0, 1+len(imp.Named))
		if imp.Namespace != "" {
			obj := &ast.ObjectExpr{}
			names := make([]string, 0, len(exports))
			for name := range exports {
				names = append(names, name)
			}
			sort.Strings(names)
			for _, name := range names {
				proxy := b.actionProxy(exports[name])
				if err := b.noteActionImport(imp.Namespace+"."+name, actionURL(b.actionBasePath, exports[name])); err != nil {
					return handled, changed
				}
				obj.Properties = append(obj.Properties, &ast.ObjectProp{Key: name, Value: proxy})
			}
			decls = append(decls, &ast.VarDecl{Name: imp.Namespace, Init: obj})
		}
		if imp.Default != "" {
			id, ok := exports["default"]
			if !ok {
				_ = b.setActionError(diag.New(abs, imp.Position.Line, imp.Position.Col,
					fmt.Sprintf("imports the default export from the server action module %s, but it has none", src),
					"export a default async function or import a named action", "").WithCode(diag.CodeActionImportMissing))
				return handled, changed
			}
			if err := b.noteActionImport(imp.Default, actionURL(b.actionBasePath, id)); err != nil {
				return handled, changed
			}
			decls = append(decls, &ast.VarDecl{Name: imp.Default, Init: b.actionProxy(id)})
		}
		for _, n := range imp.Named {
			remote := n.Remote
			if remote == "" {
				remote = n.Local
			}
			local := n.Local
			if local == "" {
				local = remote
			}
			id, ok := exports[remote]
			if !ok {
				_ = b.setActionError(diag.New(abs, imp.Position.Line, imp.Position.Col,
					fmt.Sprintf("imports %q from the server action module %s, but it is not exported", remote, src),
					fmt.Sprintf("export an async function named %q from that file", remote), "").WithCode(diag.CodeActionImportMissing))
				return handled, changed
			}
			if err := b.noteActionImport(local, actionURL(b.actionBasePath, id)); err != nil {
				return handled, changed
			}
			decls = append(decls, &ast.VarDecl{Name: local, Init: b.actionProxy(id)})
		}
		if len(decls) > 0 {
			body = append(body, &ast.VarStmt{Position: imp.Position, Kind: ast.VarConst, Decls: decls})
		}
	}
	prog.Body = body
	return handled, changed
}

func (b *Bundler) actionProxy(id string) ast.Expr {
	return &ast.CallExpr{
		Callee: &ast.Identifier{Name: "__krateAction"},
		Args:   []ast.Expr{&ast.Literal{Kind: ast.StringLit, Value: id}},
	}
}

func (b *Bundler) noteActionImport(local, url string) error {
	if prev, ok := b.actionImports[local]; ok && prev != url {
		return b.setActionError(fmt.Errorf("server action import %q resolves to two different actions (%s and %s)\n  hint: alias one of the imports", local, prev, url))
	}
	b.actionImports[local] = url
	return nil
}

func (b *Bundler) setActionError(err error) error {
	if b.actionErr == nil {
		b.actionErr = err
	}
	return b.actionErr
}

func actionURL(basePath, id string) string {
	return basePath + "/__krate/action/" + id
}

func trimImportSource(source string) string {
	if len(source) >= 2 {
		if (source[0] == '"' && source[len(source)-1] == '"') || (source[0] == '\'' && source[len(source)-1] == '\'') {
			return source[1 : len(source)-1]
		}
	}
	return source
}
