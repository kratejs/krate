package bundler

import (
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/diag"
)

// rewriteAssetNamespaceRefs replaces `ns.name` with the asset URL, where
// spaces maps a namespace binding to the URLs of its members: `ns.default` of
// `import * as ns from "./logo.png"`, or `ns.logo` of a namespace import of a
// module that re-exports an asset.
func rewriteAssetNamespaceRefs(body []ast.Stmt, namespaces map[string]map[string]string) {
	if len(namespaces) == 0 {
		return
	}
	// replace swaps an Expr-typed slot holding `ns.default` for the URL.
	replace := func(slot reflect.Value) bool {
		if slot.Kind() != reflect.Interface || slot.IsNil() || !slot.CanSet() {
			return false
		}
		url, ok := namespaceMemberRead(slot.Interface(), namespaces)
		if !ok {
			return false
		}
		lit := reflect.ValueOf(&ast.Literal{Kind: ast.StringLit, Value: url})
		if !lit.Type().AssignableTo(slot.Type()) {
			return false
		}
		slot.Set(lit)
		return true
	}
	var walk func(v reflect.Value)
	walk = func(v reflect.Value) {
		switch v.Kind() {
		case reflect.Interface:
			if !v.IsNil() {
				walk(v.Elem())
			}
		case reflect.Pointer:
			if !v.IsNil() {
				walk(v.Elem())
			}
		case reflect.Slice, reflect.Array:
			for i := 0; i < v.Len(); i++ {
				if !replace(v.Index(i)) {
					walk(v.Index(i))
				}
			}
		case reflect.Struct:
			if v.Type() == astPosType {
				return
			}
			for i := 0; i < v.NumField(); i++ {
				if !v.Type().Field(i).IsExported() {
					continue
				}
				if !replace(v.Field(i)) {
					walk(v.Field(i))
				}
			}
		}
	}
	walk(reflect.ValueOf(body))
}

var astPosType = reflect.TypeOf(ast.Pos{})

func namespaceMemberRead(e any, namespaces map[string]map[string]string) (string, bool) {
	m, ok := e.(*ast.MemberExpr)
	if !ok {
		return "", false
	}
	obj, ok := m.Object.(*ast.Identifier)
	if !ok {
		return "", false
	}
	members, ok := namespaces[obj.Name]
	if !ok {
		return "", false
	}
	switch p := m.Property.(type) {
	case *ast.Identifier:
		if !m.Computed {
			url, ok := members[p.Name]
			return url, ok
		}
	case *ast.Literal:
		if m.Computed && p.Kind == ast.StringLit {
			url, ok := members[strings.Trim(p.Value, "\"'")]
			return url, ok
		}
	}
	return "", false
}

// reportUnresolvedAssetRefs warns about a read of an asset import binding that
// the rewrite did not replace with the asset URL. The binding is not a value
// in the emitted script, so the read would be a ReferenceError (or render as
// the bare name) in the browser.
func (b *Bundler) reportUnresolvedAssetRefs(mod *Module, locals, namespaces map[string]string) {
	if !ownProjectCode(mod) {
		return
	}
	names := map[string]bool{}
	for n := range locals {
		names[n] = true
	}
	for n := range namespaces {
		names[n] = true
	}
	left := map[string]ast.Pos{}
	for _, stmt := range mod.Program.Body {
		if _, isImport := stmt.(*ast.ImportStmt); isImport {
			continue
		}
		walkASTNodes(reflect.ValueOf(stmt), func(n any) bool {
			switch x := n.(type) {
			case *ast.MemberExpr:
				// `obj.logo` names a property, not the binding.
				if !x.Computed {
					if id, ok := x.Property.(*ast.Identifier); ok && names[id.Name] {
						walkASTNodes(reflect.ValueOf(x.Object), func(m any) bool { return noteAssetRead(m, names, left) })
						return false
					}
				}
			case *ast.Identifier:
				return noteAssetRead(x, names, left)
			}
			return true
		})
	}
	keys := make([]string, 0, len(left))
	for name := range left {
		keys = append(keys, name)
	}
	sort.Strings(keys)
	for _, name := range keys {
		pos := left[name]
		msg := "asset import `" + name + "` is used where the compiler cannot replace it with the file's URL"
		hint := "the binding does not exist in the browser script, so this read fails there; use it as a plain value in JSX or a component (`<img src={" + name + "}/>`), or import the file with a `?url` suffix"
		if _, isNS := namespaces[name]; isNS {
			hint = "only `" + name + ".default` is replaced by the file's URL; import the default instead (`import " + name + " from ...`)"
		}
		d := diag.Diagnostic{File: mod.Path, Line: pos.Line, Col: pos.Col, Message: msg, Hint: hint, Severity: diag.SeverityWarning}.WithCode(diag.CodeAssetBindingUnresolved)
		d.Source = sourceLine(mod, pos.Line)
		mod.Diagnostics = append(append([]diag.Diagnostic(nil), mod.Diagnostics...), d)
	}
}

func noteAssetRead(n any, names map[string]bool, left map[string]ast.Pos) bool {
	if id, ok := n.(*ast.Identifier); ok && names[id.Name] {
		if _, seen := left[id.Name]; !seen {
			left[id.Name] = id.Position
		}
	}
	return true
}

// assetExports are the names a module exports that stand for an asset file's
// URL, found through any chain of re-exports.
type assetExports struct {
	// urls maps an export name to the URL: `export { logo }` of an asset
	// import, `export default logo`, `export { default as logo } from
	// "./logo.png"`, or a name another module exports this way.
	urls map[string]string
	// spaces maps a namespace export (`export * as icons from "./icons"`) to
	// the URLs of its members.
	spaces map[string]map[string]string
}

// assetResolver follows asset imports through re-exports. Results are memoized
// per module path; a cycle resolves to nothing for the module in progress.
type assetResolver struct {
	b      *Bundler
	byPath map[string]*Module
	memo   map[string]*assetExports
	busy   map[string]bool
}

func newAssetResolver(b *Bundler, byPath map[string]*Module) *assetResolver {
	return &assetResolver{b: b, byPath: byPath, memo: map[string]*assetExports{}, busy: map[string]bool{}}
}

// target returns the module an import or re-export source resolves to.
func (r *assetResolver) target(importer, src string) *Module {
	resolved := r.b.resolveImportForModule(importer, strings.Trim(src, "\"'"))
	if resolved == "" {
		return nil
	}
	return r.byPath[cleanPath(resolved)]
}

// bindings returns what the imports of mod bind to asset URLs: local names
// (default and named imports) and namespace bindings with their members.
// directSpaces are the namespace bindings imported straight from an asset
// file, whose only member is `default`.
func (r *assetResolver) bindings(mod *Module) (locals map[string]string, spaces map[string]map[string]string, directSpaces map[string]string) {
	locals = map[string]string{}
	spaces = map[string]map[string]string{}
	directSpaces = map[string]string{}
	for _, stmt := range mod.Program.Body {
		imp, ok := stmt.(*ast.ImportStmt)
		if !ok || imp.Source == "" {
			continue
		}
		if url, ok := r.b.importedAssetURL(mod.Path, imp); ok {
			if imp.Default != "" {
				locals[imp.Default] = url
			}
			for _, n := range imp.Named {
				if n.Remote == "default" && n.Local != "" {
					locals[n.Local] = url
				}
			}
			if imp.Namespace != "" {
				spaces[imp.Namespace] = map[string]string{"default": url}
				directSpaces[imp.Namespace] = url
			}
			continue
		}
		t := r.target(mod.Path, imp.Source)
		if t == nil || t.Program == nil {
			continue
		}
		ex := r.exports(t)
		if ex == nil {
			continue
		}
		if imp.Default != "" {
			if url, ok := ex.urls["default"]; ok {
				locals[imp.Default] = url
			}
		}
		for _, n := range imp.Named {
			if url, ok := ex.urls[n.Remote]; ok {
				locals[n.Local] = url
			} else if members, ok := ex.spaces[n.Remote]; ok {
				spaces[n.Local] = members
			}
		}
		if imp.Namespace != "" && (len(ex.urls) > 0 || len(ex.spaces) > 0) {
			spaces[imp.Namespace] = ex.urls
		}
	}
	return locals, spaces, directSpaces
}

// exports computes the asset-valued exports of mod.
func (r *assetResolver) exports(mod *Module) *assetExports {
	key := cleanPath(mod.Path)
	if ex, ok := r.memo[key]; ok {
		return ex
	}
	if r.busy[key] || mod.Program == nil {
		return nil
	}
	r.busy[key] = true
	defer delete(r.busy, key)

	locals, spaces, _ := r.bindings(mod)
	out := &assetExports{urls: map[string]string{}, spaces: map[string]map[string]string{}}
	own := map[string]bool{}
	var stars []*ast.ExportStmt
	for _, stmt := range mod.Program.Body {
		exp, ok := stmt.(*ast.ExportStmt)
		if !ok {
			continue
		}
		switch {
		case exp.ReexportSource != "" && exp.Namespace == "" && exp.StarReexport:
			stars = append(stars, exp)
		case exp.ReexportSource != "" && exp.Namespace != "":
			own[exp.Namespace] = true
			if t := r.target(mod.Path, exp.ReexportSource); t != nil {
				if ex := r.exports(t); ex != nil && len(ex.urls) > 0 {
					out.spaces[exp.Namespace] = ex.urls
				}
			}
		case exp.ReexportSource != "":
			var ex *assetExports
			url, isAsset := r.b.assetURLForSource(mod.Path, exp.ReexportSource)
			if !isAsset {
				if t := r.target(mod.Path, exp.ReexportSource); t != nil {
					ex = r.exports(t)
				}
			}
			for _, n := range exp.ReexportNamed {
				own[n.Remote] = true
				switch {
				case isAsset && n.Local == "default":
					out.urls[n.Remote] = url
				case ex != nil && ex.urls[n.Local] != "":
					out.urls[n.Remote] = ex.urls[n.Local]
				case ex != nil && ex.spaces[n.Local] != nil:
					out.spaces[n.Remote] = ex.spaces[n.Local]
				}
			}
		case exp.Default && exp.Declaration == nil && exp.Local != "":
			own["default"] = true
			if url, ok := locals[exp.Local]; ok {
				out.urls["default"] = url
			} else if members, ok := spaces[exp.Local]; ok {
				out.spaces["default"] = members
			}
		case exp.Declaration != nil:
			for _, name := range declaredNames(exp.Declaration) {
				own[name] = true
			}
			if exp.Default {
				own["default"] = true
			}
		default:
			for _, n := range exp.Named {
				own[n.Remote] = true
				if url, ok := locals[n.Local]; ok {
					out.urls[n.Remote] = url
				} else if members, ok := spaces[n.Local]; ok {
					out.spaces[n.Remote] = members
				}
			}
		}
	}
	// `export * from` re-exports every name but default that the module does
	// not export itself.
	for _, exp := range stars {
		t := r.target(mod.Path, exp.ReexportSource)
		if t == nil {
			continue
		}
		ex := r.exports(t)
		if ex == nil {
			continue
		}
		for name, url := range ex.urls {
			if name != "default" && !own[name] {
				if _, dup := out.urls[name]; !dup {
					out.urls[name] = url
				}
			}
		}
		for name, members := range ex.spaces {
			if name != "default" && !own[name] && out.spaces[name] == nil {
				out.spaces[name] = members
			}
		}
	}
	if len(out.urls) == 0 && len(out.spaces) == 0 {
		out = nil
	}
	r.memo[key] = out
	return out
}

func cleanPath(p string) string { return filepath.Clean(p) }

func sourceLine(mod *Module, line int) string {
	if mod.SourceCode == "" || line <= 0 {
		return ""
	}
	lines := strings.Split(mod.SourceCode, "\n")
	if line > len(lines) {
		return ""
	}
	return strings.TrimRight(lines[line-1], "\r")
}

// walkASTNodes visits every pointer node under v depth-first; visit returning
// false skips that node's children.
func walkASTNodes(v reflect.Value, visit func(any) bool) {
	switch v.Kind() {
	case reflect.Interface:
		if !v.IsNil() {
			walkASTNodes(v.Elem(), visit)
		}
	case reflect.Pointer:
		if v.IsNil() {
			return
		}
		if !visit(v.Interface()) {
			return
		}
		walkASTNodes(v.Elem(), visit)
	case reflect.Slice, reflect.Array:
		for i := 0; i < v.Len(); i++ {
			walkASTNodes(v.Index(i), visit)
		}
	case reflect.Struct:
		if v.Type() == astPosType {
			return
		}
		for i := 0; i < v.NumField(); i++ {
			if v.Type().Field(i).IsExported() {
				walkASTNodes(v.Field(i), visit)
			}
		}
	}
}
