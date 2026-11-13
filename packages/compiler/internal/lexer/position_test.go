package lexer

import (
	"testing"
)

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
