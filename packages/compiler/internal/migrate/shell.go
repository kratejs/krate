package migrate

import (
	"regexp"
	"strings"

	"github.com/kratejs/krate/packages/compiler/internal/lexer"
	"github.com/kratejs/krate/packages/compiler/internal/parser"
)

var (
	htmlClose = regexp.MustCompile(`(?s)</html\s*>`)
	bodyClose = regexp.MustCompile(`(?s)</body\s*>`)
	// suppressHydrationWarning has no meaning in Krate and would render as a
	// stray attribute.
	suppressHydration = regexp.MustCompile(`\s+suppressHydrationWarning(?:=\{\s*(?:true|false)\s*\})?`)
)

// openTag finds the start tag `<name ...>` in src and returns its bounds and the
// attribute text. The scan skips braces, strings and template literals, so an
// expression such as className={a > b ? "x" : "y"} does not end the tag.
func openTag(src, name string) (start, end int, attrs string, ok bool) {
	re := regexp.MustCompile(`<` + name + `\b`)
	loc := re.FindStringIndex(src)
	if loc == nil {
		return 0, 0, "", false
	}
	start = loc[0]
	depth := 0
	var quote byte
	for i := loc[1]; i < len(src); i++ {
		c := src[i]
		switch {
		case quote != 0:
			if c == '\\' {
				i++
			} else if c == quote {
				quote = 0
			}
		case c == '"' || c == '\'' || c == '`':
			quote = c
		case c == '{':
			depth++
		case c == '}':
			depth--
		case c == '>' && depth == 0:
			return start, i + 1, strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(src[loc[1]:i]), "/")), true
		}
	}
	return 0, 0, "", false
}

// stripDocumentShell removes the Next.js root <html>/<body> wrappers. Their
// attributes are kept: they move into the layout's <Head> as <html> and <body>
// elements, which set the attributes of the document elements. A static
// className or lang therefore carries over; a dynamic one is evaluated like
// any other <Head> expression. The second result reports whether the shell
// was rewritten.
func stripDocumentShell(src string) (string, bool) {
	if !strings.Contains(src, "<html") && !strings.Contains(src, "<body") {
		return src, false
	}
	out := src
	var attrTags []string
	// Body first: its position is after the html tag, so the html offsets stay valid.
	if bs, be, attrs, ok := openTag(out, "body"); ok {
		if attrs = strings.TrimSpace(suppressHydration.ReplaceAllString(" "+attrs, "")); attrs != "" {
			attrTags = append(attrTags, "<body "+attrs+" />")
		}
		out = out[:bs] + out[be:]
		out = bodyClose.ReplaceAllString(out, "")
	}
	hs, he, hattrs, ok := openTag(out, "html")
	if !ok {
		return src, false
	}
	hattrs = strings.TrimSpace(suppressHydration.ReplaceAllString(" "+hattrs, ""))
	if hattrs != "" {
		attrTags = append([]string{"<html " + hattrs + " />"}, attrTags...)
	}
	open := "<>"
	if len(attrTags) > 0 {
		open = "<><Head>" + strings.Join(attrTags, "") + "</Head>"
	}
	out = out[:hs] + open + out[he:]
	out = htmlClose.ReplaceAllString(out, "</>")
	p := parser.New(lexer.New(out).Tokenize())
	if prog := p.ParseProgram(); prog == nil || len(p.Errors()) > 0 {
		return src, false
	}
	return out, true
}
