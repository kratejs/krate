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
	namedDefault    = regexp.MustCompile(`(?m)^[ \t]*export\s+default\s+([A-Za-z_$][\w$]*)\s*;?[ \t]*\r?\n?`)
)

// convertPageMetadata wraps a page whose default export is a function with a
// small <Head> parent. Arrow/class/async defaults stay manual because wrapping
// them changes their execution semantics.
func convertPageMetadata(rep *Report) {
	for i := range rep.Files {
		f := &rep.Files[i]
		if f.Kind != KindPage {
			continue
		}
		src := string(f.Content)
		obj, ok := staticMetadata(f.Content)
		if !ok {
			obj, ok = staticGenerateMetadata(f.Content)
		}
		head := ""
		if ok {
			head = pageMetadataHead(obj)
		}
		dynamicHelper := ""
		asyncMeta := false
		if head == "" && syncGenerateMetadata(f.Content) {
			head = "<__KrateMetadata {...props} />"
			dynamicHelper = generatedMetadataComponent
		} else if head == "" && asyncGenerateMetadata(f.Content) {
			head = "<Suspense fallback={null}><__KrateMetadata {...props} /></Suspense>"
			dynamicHelper = generatedAsyncMetadataComponent
			asyncMeta = true
		}
		if head == "" {
			continue
		}
		if strings.Contains(src, "PageWithHead") || strings.Contains(src, "Page__krateBody") || strings.Contains(src, "__KrateMetadata") {
			removeMetadataManual(rep, f.Source)
			continue
		}
		body, wrapped := wrapPageWithHead(src, head)
		if !wrapped {
			body, wrapped = wrapNamedDefaultPageWithHead(src, head)
		}
		if !wrapped {
			continue
		}
		p := parser.New(lexer.New(body).Tokenize())
		prog := p.ParseProgram()
		if prog == nil || len(p.Errors()) > 0 {
			continue
		}
		if dynamicHelper != "" {
			if asyncMeta {
				body = ensureReactImports(body, "Suspense", "use")
				f.Rewrites = append(f.Rewrites, "wrapped async generateMetadata in Suspense/<Head>")
			} else {
				f.Rewrites = append(f.Rewrites, "wrapped dynamic generateMetadata in <Head>")
			}
			body += "\n" + dynamicHelper
		} else {
			f.Rewrites = append(f.Rewrites, "wrapped static page metadata in <Head>")
		}
		f.Content = []byte(body)
		removeMetadataManual(rep, f.Source)
		rep.Warnings = append(rep.Warnings, f.Source+": static metadata was moved into <Head>; the metadata export is left in place for review")
	}
}

func ensureReactImports(src string, names ...string) string {
	var missing []string
	for _, name := range names {
		re := regexp.MustCompile(`import\s*\{[^}]*\b` + regexp.QuoteMeta(name) + `\b[^}]*\}\s*from\s*["']react["']`)
		if !re.MatchString(src) {
			missing = append(missing, name)
		}
	}
	if len(missing) == 0 {
		return src
	}
	return "import { " + strings.Join(missing, ", ") + " } from \"react\";\n" + src
}

func wrapPageWithHead(src, head string) (string, bool) {
	loc := defaultFunction.FindStringSubmatchIndex(src)
	if loc == nil {
		return "", false
	}
	match := src[loc[0]:loc[1]]
	if strings.Contains(match, "async") {
		return "", false
	}
	name := ""
	if loc[5] >= 0 {
		name = src[loc[4]:loc[5]]
	}
	if name == "" {
		name = "Page__krateBody"
		src = src[:loc[0]] + "function " + name + src[loc[1]:]
	} else {
		bodyFn := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(match), "export default"))
		src = src[:loc[0]] + bodyFn + src[loc[1]:]
	}
	if strings.Contains(src, "PageWithHead") {
		return "", false
	}
	src += "\n\nexport default function PageWithHead(props: any) {\n  return (\n    <>\n      " + head + "\n      <" + name + " {...props} />\n    </>\n  );\n}\n"
	return src, true
}

func wrapNamedDefaultPageWithHead(src, head string) (string, bool) {
	if strings.Contains(src, "PageWithHead") {
		return "", false
	}
	loc := namedDefault.FindStringSubmatchIndex(src)
	if loc == nil {
		return "", false
	}
	name := src[loc[2]:loc[3]]
	src = src[:loc[0]] + src[loc[1]:]
	src += "\n\nexport default function PageWithHead(props: any) {\n  return (\n    <>\n      " + head + "\n      <" + name + " {...props} />\n    </>\n  );\n}\n"
	return src, true
}

func removeMetadataManual(rep *Report, source string) {
	var kept []ManualItem
	for _, m := range rep.Manual {
		if m.Source == source && (strings.Contains(m.Reason, "metadata export is not translated") || strings.Contains(m.Reason, "generateMetadata is not translated")) {
			continue
		}
		kept = append(kept, m)
	}
	rep.Manual = kept
}

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

func asyncGenerateMetadata(src []byte) bool {
	prog := parser.New(lexer.New(string(src)).Tokenize()).ParseProgram()
	if prog == nil {
		return false
	}
	for _, stmt := range prog.Body {
		exp, ok := stmt.(*ast.ExportStmt)
		if !ok || exp.Default {
			continue
		}
		switch d := exp.Declaration.(type) {
		case *ast.FnDecl:
			if d.Name == "generateMetadata" && d.Async {
				return true
			}
		case *ast.VarStmt:
			for _, decl := range d.Decls {
				if decl.Name != "generateMetadata" {
					continue
				}
				if arrow, ok := decl.Init.(*ast.ArrowFn); ok && arrow.Async {
					return true
				}
			}
		}
	}
	return false
}

func syncGenerateMetadata(src []byte) bool {
	prog := parser.New(lexer.New(string(src)).Tokenize()).ParseProgram()
	if prog == nil {
		return false
	}
	for _, stmt := range prog.Body {
		exp, ok := stmt.(*ast.ExportStmt)
		if !ok || exp.Default {
			continue
		}
		switch d := exp.Declaration.(type) {
		case *ast.FnDecl:
			if d.Name == "generateMetadata" && !d.Async {
				return true
			}
		case *ast.VarStmt:
			for _, decl := range d.Decls {
				if decl.Name != "generateMetadata" {
					continue
				}
				if arrow, ok := decl.Init.(*ast.ArrowFn); ok && !arrow.Async {
					return true
				}
			}
		}
	}
	return false
}

const generatedMetadataComponent = `function __KrateMetadata(props: any) {
  const m = generateMetadata(props) || {};
  const og = m.openGraph || {};
  const tw = m.twitter || {};
  const title = typeof m.title === "string" ? m.title : (m.title?.default || m.title?.absolute);
  const ogImage = Array.isArray(og.images) ? (typeof og.images[0] === "string" ? og.images[0] : og.images[0]?.url) : (typeof og.images === "string" ? og.images : og.images?.url);
  const twImage = Array.isArray(tw.images) ? (typeof tw.images[0] === "string" ? tw.images[0] : tw.images[0]?.url) : (typeof tw.images === "string" ? tw.images : tw.images?.url);
  const canonical = m.alternates?.canonical;
  return (
    <Head>
      {title ? <title>{title}</title> : null}
      {m.description ? <meta name="description" content={m.description} /> : null}
      {m.themeColor ? <meta name="theme-color" content={m.themeColor} /> : null}
      {typeof m.robots === "string" ? <meta name="robots" content={m.robots} /> : null}
      {canonical ? <link rel="canonical" href={canonical} /> : null}
      {og.title ? <meta property="og:title" content={og.title} /> : null}
      {og.description ? <meta property="og:description" content={og.description} /> : null}
      {og.type ? <meta property="og:type" content={og.type} /> : null}
      {og.url ? <meta property="og:url" content={og.url} /> : null}
      {ogImage ? <meta property="og:image" content={ogImage} /> : null}
      {tw.card ? <meta name="twitter:card" content={tw.card} /> : null}
      {tw.site ? <meta name="twitter:site" content={tw.site} /> : null}
      {tw.creator ? <meta name="twitter:creator" content={tw.creator} /> : null}
      {tw.title ? <meta name="twitter:title" content={tw.title} /> : null}
      {tw.description ? <meta name="twitter:description" content={tw.description} /> : null}
      {twImage ? <meta name="twitter:image" content={twImage} /> : null}
    </Head>
  );
}
`

const generatedAsyncMetadataComponent = `const __krateMetadataCache = new Map<string, any>();
function __KrateMetadata(props: any) {
  const key = JSON.stringify(props);
  if (!__krateMetadataCache.has(key)) __krateMetadataCache.set(key, generateMetadata(props));
  const m = use(__krateMetadataCache.get(key)) || {};
  const og = m.openGraph || {};
  const tw = m.twitter || {};
  const title = typeof m.title === "string" ? m.title : (m.title?.default || m.title?.absolute);
  const ogImage = Array.isArray(og.images) ? (typeof og.images[0] === "string" ? og.images[0] : og.images[0]?.url) : (typeof og.images === "string" ? og.images : og.images?.url);
  const twImage = Array.isArray(tw.images) ? (typeof tw.images[0] === "string" ? tw.images[0] : tw.images[0]?.url) : (typeof tw.images === "string" ? tw.images : tw.images?.url);
  const canonical = m.alternates?.canonical;
  return (
    <Head>
      {title ? <title>{title}</title> : null}
      {m.description ? <meta name="description" content={m.description} /> : null}
      {m.themeColor ? <meta name="theme-color" content={m.themeColor} /> : null}
      {typeof m.robots === "string" ? <meta name="robots" content={m.robots} /> : null}
      {canonical ? <link rel="canonical" href={canonical} /> : null}
      {og.title ? <meta property="og:title" content={og.title} /> : null}
      {og.description ? <meta property="og:description" content={og.description} /> : null}
      {og.type ? <meta property="og:type" content={og.type} /> : null}
      {og.url ? <meta property="og:url" content={og.url} /> : null}
      {ogImage ? <meta property="og:image" content={ogImage} /> : null}
      {tw.card ? <meta name="twitter:card" content={tw.card} /> : null}
      {tw.site ? <meta name="twitter:site" content={tw.site} /> : null}
      {tw.creator ? <meta name="twitter:creator" content={tw.creator} /> : null}
      {tw.title ? <meta name="twitter:title" content={tw.title} /> : null}
      {tw.description ? <meta name="twitter:description" content={tw.description} /> : null}
      {twImage ? <meta name="twitter:image" content={twImage} /> : null}
    </Head>
  );
}
`
