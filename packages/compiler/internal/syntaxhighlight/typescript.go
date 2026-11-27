package syntaxhighlight

import (
	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
)

// The stock TypeScript lexer reads every "<Name" as a JSX tag, so in
// createSignal<Task[]>(...) the "[" and "]" fall inside a tag and come out as
// Error tokens. These rules claim type argument lists first, as plain
// punctuation, and leave JSX alone.
const (
	// A "<" glued to an identifier that opens a type argument list: Task[],
	// A | B, A, B, Outer<Inner or a lone argument followed by a call, an
	// assignment or a terminator.
	genericOpenPattern = `(?<=[\w$])<(?=[\w$.]+(?:\[\]|\s*[,|&]|<)|[\w$.]+>\s*[(=;),\]])`
	// A generic arrow parameter list: <T,>( and <T extends U>(.
	genericParamsPattern = `<(?=[\w$]+(?:\s*,\s*[\w$]+)*\s*,?\s*>\s*\(|[\w$]+\s+extends\s)`
)

func init() {
	base, ok := lexers.GlobalLexerRegistry.Get("TypeScript").(*chroma.RegexLexer)
	if !ok {
		return
	}
	rules, err := base.Rules()
	if err != nil {
		return
	}
	cfg := *base.Config()
	patched, err := chroma.NewLexer(&cfg, func() chroma.Rules {
		out := rules.Clone()
		root := make([]chroma.Rule, 0, len(out["root"])+2)
		root = append(root,
			chroma.Rule{Pattern: genericOpenPattern, Type: chroma.Punctuation},
			chroma.Rule{Pattern: genericParamsPattern, Type: chroma.Punctuation},
		)
		out["root"] = append(root, out["root"]...)
		return out
	})
	if err != nil {
		return
	}
	lexers.GlobalLexerRegistry.Register(patched)
}
