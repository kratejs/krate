package lexer

import (
	"testing"
)

func tokens(src string) []Token {
	l := New(src)
	all := l.Tokenize()
	var out []Token
	for _, t := range all {
		if t.Kind != Whitespace {
			out = append(out, t)
		}
	}
	return out
}

func kind(t *testing.T, tok Token, expected Kind) {
	t.Helper()
	if tok.Kind != expected {
		t.Errorf("expected kind %v, got %v (value=%q, line=%d, col=%d)", expected, tok.Kind, tok.Value, tok.Line, tok.Col)
	}
}

func value(t *testing.T, tok Token, expected string) {
	t.Helper()
	if tok.Value != expected {
		t.Errorf("expected value %q, got %q", expected, tok.Value)
	}
}

func TestTemplateLiteral(t *testing.T) {
	// Template chunks carry their delimiters; interpolated expressions are
	// ordinary tokens between them.
	toks := tokens("`hello ${name} world`")
	kind(t, toks[0], TEMPLATE_START)
	value(t, toks[0], "`hello ${")
	kind(t, toks[1], Identifier)
	value(t, toks[1], "name")
	kind(t, toks[2], TEMPLATE_END)
	value(t, toks[2], "} world`")
}

func TestRegexLiteral(t *testing.T) {
	// After operator like assignment, / starts a regex
	toks := tokens("re=/test/i")
	kind(t, toks[0], Identifier)
	value(t, toks[0], "re")
	kind(t, toks[1], ASSIGN)
	kind(t, toks[2], Regexp)
	value(t, toks[2], "/test/i")
}

func TestJSXWithChildren(t *testing.T) {
	toks := tokens("<div>hello</div>")
	kind(t, toks[0], LT)
	if !toks[0].JSX {
		t.Error("opening `<` must be marked as JSX")
	}
	kind(t, toks[1], Identifier)
	value(t, toks[1], "div")
	kind(t, toks[2], GT)
	kind(t, toks[3], JSXText)
	value(t, toks[3], "hello")
	kind(t, toks[4], LT_SLASH)
	kind(t, toks[5], Identifier)
	value(t, toks[5], "div")
	kind(t, toks[6], GT)
}
