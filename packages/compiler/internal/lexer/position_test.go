package lexer

import (
	"strings"
	"testing"
)

type wantPos struct {
	value     string
	line, col int
	offset    int
}

func checkPositions(t *testing.T, src string, want []wantPos) {
	t.Helper()
	toks := tokens(src)
	for _, w := range want {
		found := false
		for _, tk := range toks {
			if tk.Value == w.value {
				found = true
				if tk.Line != w.line || tk.Col != w.col || tk.Offset != w.offset {
					t.Errorf("%q at %d:%d (offset %d), want %d:%d (offset %d)", w.value, tk.Line, tk.Col, tk.Offset, w.line, w.col, w.offset)
				}
				if src[tk.Offset:tk.End] != tk.Value {
					t.Errorf("%q: src[Offset:End] = %q", w.value, src[tk.Offset:tk.End])
				}
				break
			}
		}
		if !found {
			t.Errorf("token %q not found", w.value)
		}
	}
}

func TestPositionsASCII(t *testing.T) {
	checkPositions(t, "const x = 1;\n  foo(bar);", []wantPos{
		{"const", 1, 1, 0}, {"x", 1, 7, 6}, {"1", 1, 11, 10},
		{"foo", 2, 3, 15}, {"bar", 2, 7, 19},
	})
}

func TestPositionsLineTerminators(t *testing.T) {
	// \r\n, lone \r, U+2028 and U+2029 all end a line.
	src := "a\r\nb\rc" + string(rune(0x2028)) + "d" + string(rune(0x2029)) + "e"
	checkPositions(t, src, []wantPos{
		{"a", 1, 1, 0}, {"b", 2, 1, 3}, {"c", 3, 1, 5}, {"d", 4, 1, 9}, {"e", 5, 1, 13},
	})
}

func TestPositionsUTF16Columns(t *testing.T) {
	// The emoji is one rune but two UTF-16 code units; e-acute is one unit.
	emoji := string(rune(0x1F600))
	src := `s = "` + emoji + `"; t = "` + string(rune(0xE9)) + `"; u`
	toks := tokens(src)
	var u Token
	for _, tk := range toks {
		if tk.Value == "u" {
			u = tk
		}
	}
	// s=1 ' '=2 '='=3 ' '=4 '"'=5 emoji=6,7 '"'=8 ';'=9 ' '=10 t=11 ' '=12
	// '='=13 ' '=14 '"'=15 e-acute=16 '"'=17 ';'=18 ' '=19 u=20
	if u.Line != 1 || u.Col != 20 {
		t.Fatalf("u at %d:%d, want 1:20", u.Line, u.Col)
	}
}

func TestPositionsMultilineTemplate(t *testing.T) {
	src := "x = `a\nb ${y}\nc`;\nz"
	checkPositions(t, src, []wantPos{
		{"`a\nb ${", 1, 5, 4}, {"y", 2, 5, 11}, {"}\nc`", 2, 6, 12}, {"z", 4, 1, 18},
	})
}

func TestNewlineBeforeFlag(t *testing.T) {
	toks := tokens("a /* x\n */ b\nc // d\ne")
	want := map[string]bool{"a": false, "b": true, "c": true, "e": true}
	for _, tk := range toks {
		if w, ok := want[tk.Value]; ok && tk.NewlineBefore != w {
			t.Errorf("%q NewlineBefore = %v, want %v", tk.Value, tk.NewlineBefore, w)
		}
	}
}

func TestSplitLinesMatchesLexer(t *testing.T) {
	src := "a\r\nb\rc\nd"
	lines := SplitLines(src)
	if strings.Join(lines, "|") != "a|b|c|d" {
		t.Fatalf("lines = %q", lines)
	}
}

func TestUnicodeIdentifiers(t *testing.T) {
	// Latin letter, Greek letter with combining mark, ZWJ continuation,
	// and an escaped identifier.
	src := "caf" + string(rune(0xE9)) + " " + string(rune(0x3B1)) + string(rune(0x301)) + " a" + string(rune(0x200D)) + "b \\u0061bc \\u{62}cd"
	toks := tokens(src)
	var idents []string
	for _, tk := range toks {
		if tk.Kind == Identifier {
			idents = append(idents, tk.Value)
		}
	}
	if len(idents) != 5 {
		t.Fatalf("identifiers = %q", idents)
	}
	if idents[3] != "abc" || idents[4] != "bcd" {
		t.Errorf("escaped identifiers decoded as %q, %q", idents[3], idents[4])
	}
	for _, tk := range toks {
		if tk.Value == "abc" && !tk.Escaped {
			t.Error("escaped identifier not marked Escaped")
		}
	}
}

func TestStringRawNewlineIsError(t *testing.T) {
	toks := tokens("x = \"a\nb\";")
	if toks[2].Kind != Error {
		t.Fatalf("expected Error for a raw newline in a string, got %v", toks[2])
	}
	// A line continuation is fine.
	toks = tokens("x = \"a\\\nb\";")
	if toks[2].Kind != String {
		t.Fatalf("line continuation: got %v", toks[2])
	}
}

func TestLexerEdgeCases(t *testing.T) {
	cases := []struct {
		name  string
		src   string
		kinds []Kind
	}{
		{"regex-class", "/[/]/.test(s)", []Kind{Regexp, DOT, Identifier, LPAREN, Identifier, RPAREN}},
		{"regex-escape", `/\//g`, []Kind{Regexp}},
		{"regex-v-flag", "/[a--b]/v", []Kind{Regexp}},
		{"inc-div", "a++ / 2", []Kind{Identifier, INC, DIV, Number}},
		{"regex-after-if", "if (x) /re/.test(y)", []Kind{If_, LPAREN, Identifier, RPAREN, Regexp, DOT, Identifier, LPAREN, Identifier, RPAREN}},
		{"div-after-call", "f(x) / 2", []Kind{Identifier, LPAREN, Identifier, RPAREN, DIV, Number}},
		{"quest-dot-number", "a?.5:1", []Kind{Identifier, QUEST, Number, COLON, Number}},
		{"hex-plus", "0xE+1", []Kind{Number, PLUS, Number}},
		{"exponent", "1e+5", []Kind{Number}},
		{"bigint", "10n 0x1Fn", []Kind{Number, Number}},
		{"separators", "1_000_000", []Kind{Number}},
		{"hashbang", "#!/usr/bin/env node\nx", []Kind{Identifier}},
		{"half-comment", "/*/ x */ y", []Kind{Identifier}},
		{"private-name", "this.#x", []Kind{This_, DOT, Identifier}},
		{"pow", "a ** b **= c", []Kind{Identifier, POW, Identifier, POW_ASSIGN, Identifier}},
		{"logical-assign", "a ||= b &&= c ??= d", []Kind{Identifier, OR_ASSIGN, Identifier, AND_ASSIGN, Identifier, NULLISH_ASSIGN, Identifier}},
		{"bit-assign", "a &= b |= c ^= d >>>= e", []Kind{Identifier, BIT_AND_ASSIGN, Identifier, BIT_OR_ASSIGN, Identifier, BIT_XOR_ASSIGN, Identifier, USHR_ASSIGN, Identifier}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			toks := tokens(tc.src)
			toks = toks[:len(toks)-1] // EOF
			if len(toks) != len(tc.kinds) {
				t.Fatalf("got %v", toks)
			}
			for i, k := range tc.kinds {
				if toks[i].Kind != k {
					t.Errorf("token %d = %v, want %s", i, toks[i], kindName(k))
				}
			}
		})
	}
}

func TestUnterminatedConstructsAreErrors(t *testing.T) {
	for _, src := range []string{"/* never closed", "x = /abc\n", "`abc", "'abc", "0x", "1_", "/a/gg"} {
		found := false
		for _, tk := range tokens(src) {
			if tk.Kind == Error && tk.Err != "" {
				found = true
			}
		}
		if !found {
			t.Errorf("%q: expected an Error token", src)
		}
	}
}

func TestTypeContextDoesNotOpenJSX(t *testing.T) {
	for _, src := range []string{
		"type F = <T>(x: T) => T;\nconst re = /a/g;",
		"interface I { <T>(a: T): T }\nconst re = /a/g;",
		"const f = <T extends X>(x: T) => x;\nconst re = /a/g;",
		"const f = <T = string>(x: T) => x;\nconst re = /a/g;",
	} {
		sawRegex := false
		for _, tk := range tokens(src) {
			if tk.Kind == LT && tk.JSX {
				t.Errorf("%q: `<` opened JSX", src)
			}
			if tk.Kind == Regexp {
				sawRegex = true
			}
		}
		if !sawRegex {
			t.Errorf("%q: JSX state leaked into the rest of the file", src)
		}
	}
	// Lowercase tags remain JSX even when followed by parenthesized text.
	toks := tokens("x = <p>(Optional): name</p>;")
	if toks[2].Kind != LT || !toks[2].JSX {
		t.Fatalf("expected JSX `<`, got %v", toks[2])
	}
}
