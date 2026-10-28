package migrate

import (
	"strings"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/lexer"
	"github.com/kratejs/krate/packages/compiler/internal/parser"
)

// staticMetadata returns the object literal of `export const metadata = {...}`.
func staticMetadata(src []byte) (*ast.ObjectExpr, bool) {
	prog := parser.New(lexer.New(string(src)).Tokenize()).ParseProgram()
	if prog == nil {
		return nil, false
	}
	for _, stmt := range prog.Body {
		exp, ok := stmt.(*ast.ExportStmt)
		if !ok || exp.Default {
			continue
		}
		vs, ok := exp.Declaration.(*ast.VarStmt)
		if !ok {
			continue
		}
		for _, d := range vs.Decls {
			if d.Name != "metadata" {
				continue
			}
			obj, ok := d.Init.(*ast.ObjectExpr)
			return obj, ok
		}
	}
	return nil, false
}

// metadataSite maps the common static Next.js metadata fields to Krate's site
// config. Unknown fields are left for manual review.
func metadataSite(obj *ast.ObjectExpr) map[string]any {
	site := map[string]any{}
	title := prop(obj, "title")
	switch t := title.(type) {
	case *ast.Literal:
		if s, ok := stringValue(t); ok && s != "" {
			site["name"] = s
		}
	case *ast.ObjectExpr:
		if s, ok := stringValue(prop(t, "default")); ok && s != "" {
			site["name"] = s
		}
		if s, ok := stringValue(prop(t, "template")); ok && s != "" {
			site["titleTemplate"] = s
		}
	}
	if s, ok := stringValue(prop(obj, "description")); ok && s != "" {
		site["description"] = s
	}
	if s, ok := stringValue(prop(obj, "metadataBase")); ok && s != "" {
		site["url"] = strings.TrimRight(s, "/")
	}
	if s, ok := stringValue(prop(obj, "themeColor")); ok && s != "" {
		site["themeColor"] = s
	}

	og, _ := prop(obj, "openGraph").(*ast.ObjectExpr)
	if og != nil {
		if _, ok := site["name"]; !ok {
			if s, ok := stringValue(prop(og, "title")); ok && s != "" {
				site["name"] = s
			}
		}
		if _, ok := site["description"]; !ok {
			if s, ok := stringValue(prop(og, "description")); ok && s != "" {
				site["description"] = s
			}
		}
		if s := firstImageURL(prop(og, "images")); s != "" {
			site["image"] = s
		}
		if s, ok := stringValue(prop(og, "locale")); ok && s != "" {
			site["locale"] = strings.ReplaceAll(s, "_", "-")
		}
	}
	tw, _ := prop(obj, "twitter").(*ast.ObjectExpr)
	if tw != nil {
		for _, key := range []string{"site", "creator"} {
			if s, ok := stringValue(prop(tw, key)); ok && s != "" {
				site["twitter"] = strings.TrimPrefix(s, "@")
				break
			}
		}
	}
	if len(site) == 0 {
		return nil
	}
	return site
}

func firstImageURL(e ast.Expr) string {
	switch v := e.(type) {
	case *ast.Literal:
		s, _ := stringValue(v)
		return s
	case *ast.ArrayExpr:
		if len(v.Elements) > 0 {
			return firstImageURL(v.Elements[0])
		}
	case *ast.ObjectExpr:
		s, _ := stringValue(prop(v, "url"))
		return s
	}
	return ""
}
