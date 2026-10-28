package migrate

import (
	"regexp"
	"strings"

	"github.com/kratejs/krate/packages/compiler/internal/lexer"
	"github.com/kratejs/krate/packages/compiler/internal/parser"
)

var (
	htmlOpen  = regexp.MustCompile(`(?s)<html\b[^>]*>`)
	htmlClose = regexp.MustCompile(`(?s)</html\s*>`)
	bodyOpen  = regexp.MustCompile(`(?s)<body\b([^>]*)>`)
	bodyClose = regexp.MustCompile(`(?s)</body\s*>`)
)

// stripDocumentShell removes the Next.js root <html>/<body> wrappers. Body
// attributes are preserved on a <div>; html attributes are reported by the
// caller because they belong in site config.
func stripDocumentShell(src string) (string, bool) {
	if !strings.Contains(src, "<html") && !strings.Contains(src, "<body") {
		return src, false
	}
	out := bodyOpen.ReplaceAllString(src, "<div$1>")
	out = bodyClose.ReplaceAllString(out, "</div>")
	out = htmlOpen.ReplaceAllString(out, "<>")
	out = htmlClose.ReplaceAllString(out, "</>")
	p := parser.New(lexer.New(out).Tokenize())
	if prog := p.ParseProgram(); prog == nil || len(p.Errors()) > 0 {
		return src, false
	}
	return out, true
}
