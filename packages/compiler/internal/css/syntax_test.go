package css

import (
	"strings"
	"testing"
)

func TestCheckSyntaxAcceptsValidCSS(t *testing.T) {
	valid := []string{
		"",
		".a{color:red}\n.b{margin:0}",
		"@media (min-width:1px){.a{color:red}}",
		".a{color:red;&:hover{color:blue}}",
		"/* } { */ .a{color:red}",
		`.a::after{content:"}{"}`,
		`.a::after{content:'\'}'}`,
		".a{background:url(data:image/svg+xml;utf8,<svg>{</svg>)}",
		`.a{background:url("x}.png")}`,
		`[data-x="{"]{color:red}`,
		".a\\{b{color:red}",
		":root{--x:{a:b}}",
	}
	for _, src := range valid {
		if ps := CheckSyntax(src); len(ps) != 0 {
			t.Errorf("CheckSyntax(%q) = %v, want none", src, ps)
		}
		if got := Balance(src); got != src {
			t.Errorf("Balance(%q) changed a valid sheet: %q", src, got)
		}
	}
}

func TestCheckSyntaxReportsPositions(t *testing.T) {
	tests := []struct {
		name, src string
		line, col int
		contains  string
	}{
		{"unclosed block", ".a{color:red}\n.b {\n  color:blue;\n", 2, 4, "never closed"},
		{"stray close", ".a{color:red}\n}\n.b{margin:0}", 2, 1, "no matching"},
		{"unclosed comment", ".a{color:red}\n  /* oops\n.b{margin:0}", 2, 3, "comment"},
		{"unclosed string", ".a{content:\"abc\n}", 1, 12, "string"},
		{"unclosed nested", "@media (x){\n.a{color:red}\n", 1, 11, "never closed"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ps := CheckSyntax(tc.src)
			if len(ps) == 0 {
				t.Fatal("no problem reported")
			}
			p := ps[0]
			if p.Line != tc.line || p.Col != tc.col || !strings.Contains(p.Message, tc.contains) {
				t.Fatalf("got %d:%d %q, want %d:%d containing %q", p.Line, p.Col, p.Message, tc.line, tc.col, tc.contains)
			}
		})
	}
}

func TestBalanceKeepsOneSheetFromLeakingIntoTheNext(t *testing.T) {
	next := ".next{color:blue}"
	for name, a := range map[string]string{
		"unclosed block":   ".a{color:red;\n.a2{color:green}\n",
		"stray close":      ".a{color:red}}\n",
		"unclosed comment": ".a{color:red}\n/* trailing",
	} {
		t.Run(name, func(t *testing.T) {
			fixed := Balance(a)
			if ps := CheckSyntax(fixed); len(ps) != 0 {
				t.Fatalf("Balance left problems %v in %q", ps, fixed)
			}
			if ps := CheckSyntax(fixed + "\n" + next); len(ps) != 0 {
				t.Fatalf("joined sheets are not balanced: %v", ps)
			}
		})
	}
	if got := Balance(".a{color:red;"); got != ".a{color:red;}\n" {
		t.Errorf("Balance = %q", got)
	}
}
