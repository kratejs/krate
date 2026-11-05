package bundler

import (
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/diag"
)

// rewriteAssetNamespaceRefs replaces `ns.default` of `import * as ns from
// "./logo.png"` with the asset URL.
func rewriteAssetNamespaceRefs(body []ast.Stmt, namespaces map[string]string) {
	if len(namespaces) == 0 {
		return
	}
	// replace swaps an Expr-typed slot holding `ns.default` for the URL.
	replace := func(slot reflect.Value) bool {
		if slot.Kind() != reflect.Interface || slot.IsNil() || !slot.CanSet() {
			return false
		}
		url, ok := namespaceDefaultRead(slot.Interface(), namespaces)
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

func namespaceDefaultRead(e any, namespaces map[string]string) (string, bool) {
	m, ok := e.(*ast.MemberExpr)
	if !ok {
		return "", false
	}
	obj, ok := m.Object.(*ast.Identifier)
	if !ok {
		return "", false
	}
	url, ok := namespaces[obj.Name]
	if !ok {
		return "", false
	}
	switch p := m.Property.(type) {
	case *ast.Identifier:
		if !m.Computed && p.Name == "default" {
			return url, true
		}
	case *ast.Literal:
		if m.Computed && p.Kind == ast.StringLit && strings.Trim(p.Value, "\"'") == "default" {
			return url, true
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

// reportReexportedAssetImports warns where a module imports a binding that
// another module re-exports from an asset import: the page script carries the
// asset's URL only for bindings imported straight from the asset file.
func (b *Bundler) reportReexportedAssetImports(byPath map[string]*Module) {
	assetBindings := map[string]map[string]bool{}
	for path, mod := range byPath {
		var names map[string]bool
		for _, stmt := range mod.Program.Body {
			imp, ok := stmt.(*ast.ImportStmt)
			if !ok {
				continue
			}
			if _, isAsset := b.importedAssetURL(mod.Path, imp); !isAsset {
				continue
			}
			if names == nil {
				names = map[string]bool{}
			}
			if imp.Default != "" {
				names[imp.Default] = true
			}
			for _, n := range imp.Named {
				if n.Remote == "default" {
					names[n.Local] = true
				}
			}
		}
		if names != nil {
			assetBindings[path] = names
		}
	}
	if len(assetBindings) == 0 {
		return
	}
	// exported name -> true, per module that re-exports an asset binding.
	reexported := map[string]map[string]bool{}
	for path, names := range assetBindings {
		for _, stmt := range byPath[path].Program.Body {
			exp, ok := stmt.(*ast.ExportStmt)
			if !ok {
				continue
			}
			if exp.Default && exp.Local != "" && names[exp.Local] {
				addSet(reexported, path, "default")
			}
			for _, n := range exp.Named {
				if names[n.Local] {
					addSet(reexported, path, n.Remote)
				}
			}
		}
	}
	if len(reexported) == 0 {
		return
	}
	for _, mod := range byPath {
		if !ownProjectCode(mod) {
			continue
		}
		for _, stmt := range mod.Program.Body {
			imp, ok := stmt.(*ast.ImportStmt)
			if !ok || imp.Source == "" {
				continue
			}
			target := b.resolveImportForModule(mod.Path, strings.Trim(imp.Source, "\"'"))
			if target == "" {
				continue
			}
			out := reexported[cleanPath(target)]
			if out == nil {
				continue
			}
			var bad []string
			if imp.Default != "" && out["default"] {
				bad = append(bad, imp.Default)
			}
			for _, n := range imp.Named {
				if out[n.Remote] {
					bad = append(bad, n.Local)
				}
			}
			if len(bad) == 0 {
				continue
			}
			sort.Strings(bad)
			msg := "`" + strings.Join(bad, "`, `") + "` imported from " + strconv.Quote(strings.Trim(imp.Source, "\"'")) + " is an asset import re-exported by that module; it has no value in the page script"
			hint := "import the asset file directly in this file (`import logo from \"./logo.png\"`), or have that module export the URL as a string constant"
			d := diag.Diagnostic{File: mod.Path, Line: imp.Position.Line, Col: imp.Position.Col, Message: msg, Hint: hint, Severity: diag.SeverityWarning}.WithCode(diag.CodeAssetBindingUnresolved)
			d.Source = sourceLine(mod, imp.Position.Line)
			mod.Diagnostics = append(append([]diag.Diagnostic(nil), mod.Diagnostics...), d)
		}
	}
}

func addSet(m map[string]map[string]bool, k, v string) {
	if m[k] == nil {
		m[k] = map[string]bool{}
	}
	m[k][v] = true
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
