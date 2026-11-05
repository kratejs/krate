package syntaxhighlight

import (
	"strings"

	"github.com/alecthomas/chroma/v2/lexers"
)

// ShippedLanguages are the chroma lexers the runtime tokenizer can run with
// exact parity: every one is checked against chroma on real-world code (see
// TestParityCorpus). A language outside this list is still highlighted by
// chroma at build time; only highlighting that happens in the browser or at
// request time needs one of these.
var ShippedLanguages = []string{
	"TypeScript", "JavaScript", "react", "JSON", "CSS", "HTML", "Bash", "Go",
	"Python", "Rust", "YAML", "markdown", "TOML", "XML", "C", "plaintext",
}

// ShippedLexer returns the canonical lexer name a language string selects, and
// whether the runtime tokenizer ships it.
func ShippedLexer(lang string) (string, bool) {
	lex := lexers.Get(NormalizeLanguage(lang))
	if lex == nil {
		return "", false
	}
	name := lex.Config().Name
	for _, s := range ShippedLanguages {
		if strings.EqualFold(s, name) {
			return name, true
		}
	}
	return name, false
}
