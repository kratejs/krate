package lexer

import "testing"

// TestModernOperatorTokens covers the modern operator tokens the parser accepts
// for compiled-library source.
func TestModernOperatorTokens(t *testing.T) {
	cases := []struct {
		src  string
		want Kind
	}{
		{"**", POW},
		{"**=", POW_ASSIGN},
		{"??=", NULLISH_ASSIGN},
		{"&&=", AND_ASSIGN},
		{"||=", OR_ASSIGN},
		{"<<", SHL},
		{">>", SHR},
		{">>>", USHIFT_RIGHT},
		{"<<=", SHL_ASSIGN},
		{">>=", SHR_ASSIGN},
	}
	for _, c := range cases {
		toks := tokens(c.src)
		if len(toks) == 0 {
			t.Fatalf("%q: no tokens", c.src)
		}
		if toks[0].Kind != c.want {
			t.Errorf("%q: kind = %v, want %v", c.src, toks[0].Kind, c.want)
		}
	}
}

// TestPrivateMemberToken verifies `#name` is lexed as a single identifier.
func TestPrivateMemberToken(t *testing.T) {
	toks := tokens("#count")
	if len(toks) == 0 {
		t.Fatal("no tokens")
	}
	kind(t, toks[0], Identifier)
	value(t, toks[0], "#count")
}
