package migrate

import (
	"html"
	"regexp"
	"strings"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/lexer"
	"github.com/kratejs/krate/packages/compiler/internal/parser"
)

// pageMetadataHead converts static page metadata into a <Head> element. It
// returns "" when there is nothing safe to render.
func pageMetadataHead(obj *ast.ObjectExpr) string {
	if obj == nil {
		return ""
	}
	var tags []string
	if title := metadataTitle(obj); title != "" {
		tags = append(tags, "<title>"+jsxText(title)+"</title>")
	}
	if s, ok := stringValue(prop(obj, "description")); ok && s != "" {
		tags = append(tags, metaTag("name", "description", s))
	}
	if s, ok := stringValue(prop(obj, "themeColor")); ok && s != "" {
		tags = append(tags, metaTag("name", "theme-color", s))
	}
	if s, ok := stringValue(prop(obj, "robots")); ok && s != "" {
		tags = append(tags, metaTag("name", "robots", s))
	}
	if s := keywordsValue(prop(obj, "keywords")); s != "" {
		tags = append(tags, metaTag("name", "keywords", s))
	}
	if s := canonicalValue(prop(obj, "alternates")); s != "" {
		tags = append(tags, `<link rel="canonical" href="`+jsxAttr(s)+`" />`)
	}
	if s := iconValue(prop(obj, "icons")); s != "" {
		tags = append(tags, `<link rel="icon" href="`+jsxAttr(s)+`" />`)
	}
	tags = append(tags, openGraphTags(prop(obj, "openGraph"))...)
	tags = append(tags, twitterTags(prop(obj, "twitter"))...)
	if len(tags) == 0 {
		return ""
	}
	return "<Head>" + strings.Join(tags, "") + "</Head>"
}

func metadataTitle(obj *ast.ObjectExpr) string {
	switch t := prop(obj, "title").(type) {
	case *ast.Literal:
		s, _ := stringValue(t)
		return s
	case *ast.ObjectExpr:
		s, _ := stringValue(prop(t, "default"))
		if s == "" {
			s, _ = stringValue(prop(t, "absolute"))
		}
		return s
	}
	return ""
}

func openGraphTags(e ast.Expr) []string {
	og, ok := e.(*ast.ObjectExpr)
	if !ok {
		return nil
	}
	var tags []string
	if s, ok := stringValue(prop(og, "title")); ok && s != "" {
		tags = append(tags, metaTag("property", "og:title", s))
	}
	if s, ok := stringValue(prop(og, "description")); ok && s != "" {
		tags = append(tags, metaTag("property", "og:description", s))
	}
	if s, ok := stringValue(prop(og, "type")); ok && s != "" {
		tags = append(tags, metaTag("property", "og:type", s))
	}
	if s, ok := stringValue(prop(og, "url")); ok && s != "" {
		tags = append(tags, metaTag("property", "og:url", s))
	}
	if s, ok := stringValue(prop(og, "siteName")); ok && s != "" {
		tags = append(tags, metaTag("property", "og:site_name", s))
	}
	if s, ok := stringValue(prop(og, "locale")); ok && s != "" {
		tags = append(tags, metaTag("property", "og:locale", strings.ReplaceAll(s, "_", "-")))
	}
	if s := firstImageURL(prop(og, "images")); s != "" {
		tags = append(tags, metaTag("property", "og:image", s))
	}
	return tags
}

func twitterTags(e ast.Expr) []string {
	tw, ok := e.(*ast.ObjectExpr)
	if !ok {
		return nil
	}
	var tags []string
	for _, pair := range [][2]string{
		{"card", "twitter:card"},
		{"site", "twitter:site"},
		{"creator", "twitter:creator"},
		{"title", "twitter:title"},
		{"description", "twitter:description"},
	} {
		if s, ok := stringValue(prop(tw, pair[0])); ok && s != "" {
			tags = append(tags, metaTag("name", pair[1], s))
		}
	}
	if s := firstImageURL(prop(tw, "images")); s != "" {
		tags = append(tags, metaTag("name", "twitter:image", s))
	}
	return tags
}

func keywordsValue(e ast.Expr) string {
	switch v := e.(type) {
	case *ast.Literal:
		s, _ := stringValue(v)
		return s
	case *ast.ArrayExpr:
		var parts []string
		for _, el := range v.Elements {
			if s, ok := stringValue(el); ok {
				parts = append(parts, s)
			}
		}
		return strings.Join(parts, ", ")
	}
	return ""
}

func canonicalValue(e ast.Expr) string {
	obj, ok := e.(*ast.ObjectExpr)
	if !ok {
		return ""
	}
	s, _ := stringValue(prop(obj, "canonical"))
	return s
}

func iconValue(e ast.Expr) string {
	switch v := e.(type) {
	case *ast.Literal:
		s, _ := stringValue(v)
		return s
	case *ast.ObjectExpr:
		for _, key := range []string{"icon", "shortcut", "apple"} {
			if s := firstImageURL(prop(v, key)); s != "" {
				return s
			}
		}
	}
	return ""
}

func metaTag(attr, name, content string) string {
	return "<meta " + attr + `="` + jsxAttr(name) + `" content="` + jsxAttr(content) + `" />`
}

func jsxText(s string) string {
	s = html.EscapeString(s)
	return strings.NewReplacer("{", "&#123;", "}", "&#125;").Replace(s)
}

func jsxAttr(s string) string {
	return jsxText(s)
}

var (
	defaultFunction = regexp.MustCompile(`export\s+default\s+(async\s+)?function\s*([A-Za-z_$][\w$]*)?`)
	// namedDefault matches `export default Name;` as a whole statement, so
	// `export default async function Name` or `export default Name(...)` never
	// match. The trailing line break is part of the match.
	namedDefault = regexp.MustCompile(`(?m)^[ \t]*export[ \t]+default[ \t]+([A-Za-z_$][\w$]*)[ \t]*;?[ \t]*(?:\r?\n|$)`)
)

// staticGenerateMetadata returns a generateMetadata return object only when
// every value is a literal, array of literals, or nested object. Dynamic
// metadata still needs a human.
func staticGenerateMetadata(src []byte) (*ast.ObjectExpr, bool) {
	prog := parser.New(lexer.New(string(src)).Tokenize()).ParseProgram()
	if prog == nil {
		return nil, false
	}
	for _, stmt := range prog.Body {
		exp, ok := stmt.(*ast.ExportStmt)
		if !ok || exp.Default {
			continue
		}
		switch d := exp.Declaration.(type) {
		case *ast.FnDecl:
			if d.Name != "generateMetadata" {
				continue
			}
			obj := functionReturnObject(d.Body)
			if obj != nil && staticMetadataObject(obj) {
				return obj, true
			}
		case *ast.VarStmt:
			for _, decl := range d.Decls {
				if decl.Name != "generateMetadata" {
					continue
				}
				if arrow, ok := decl.Init.(*ast.ArrowFn); ok {
					obj := functionReturnObject(arrow.Body)
					if obj != nil && staticMetadataObject(obj) {
						return obj, true
					}
				}
			}
		}
	}
	return nil, false
}

func staticMetadataObject(obj *ast.ObjectExpr) bool {
	for _, p := range obj.Properties {
		if p == nil || p.Spread || p.Computed {
			return false
		}
		if !staticMetadataValue(p.Value) {
			return false
		}
	}
	return true
}

func staticMetadataValue(e ast.Expr) bool {
	switch v := e.(type) {
	case *ast.Literal:
		return v.Kind == ast.StringLit || v.Kind == ast.NumberLit || v.Kind == ast.BoolLit || v.Kind == ast.NullLit
	case *ast.ArrayExpr:
		for _, el := range v.Elements {
			if !staticMetadataValue(el) {
				return false
			}
		}
		return true
	case *ast.ObjectExpr:
		return staticMetadataObject(v)
	}
	return false
}
