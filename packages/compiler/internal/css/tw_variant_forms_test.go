package css

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

type twForm struct {
	Ats []string `json:"ats"`
	Sel string   `json:"sel"`
}

// flatRule is one style rule of the generator output with its enclosing
// at-rule preludes.
type flatRule struct {
	ats []string
	sel string
}

func squash(s string) string {
	s = strings.ReplaceAll(s, "'", `"`)
	// `*::marker` and `::marker` are the same descendant pseudo-element.
	return strings.ReplaceAll(strings.Join(strings.Fields(s), ""), "*::", "::")
}

// parseFlatRules parses compact generator output (nested at-rules around
// style rules) into flat rules.
func parseFlatRules(css string) []flatRule {
	var out []flatRule
	var walk func(s string, ats []string)
	walk = func(s string, ats []string) {
		for {
			s = strings.TrimSpace(s)
			if s == "" {
				return
			}
			open := strings.IndexByte(s, '{')
			if open < 0 {
				return
			}
			head := strings.TrimSpace(s[:open])
			depth, end := 0, -1
			for i := open; i < len(s); i++ {
				if s[i] == '{' {
					depth++
				} else if s[i] == '}' {
					depth--
					if depth == 0 {
						end = i
						break
					}
				}
			}
			if end < 0 {
				return
			}
			body := s[open+1 : end]
			if strings.HasPrefix(head, "@") {
				walk(body, append(append([]string(nil), ats...), head))
			} else {
				out = append(out, flatRule{ats: ats, sel: head})
			}
			s = s[end+1:]
		}
	}
	walk(css, nil)
	return out
}

// TestVariantFormsMatchTailwind compares selector and at-rule structure with
// Tailwind v4.3.3 (testdata/tw_variant_forms.json, from the reference
// compiler) for every non-breakpoint variant form. Whitespace is ignored.
func TestVariantFormsMatchTailwind(t *testing.T) {
	raw, err := os.ReadFile("testdata/tw_variant_forms.json")
	if err != nil {
		t.Fatal(err)
	}
	var golden map[string][]twForm
	if err := json.Unmarshal(raw, &golden); err != nil {
		t.Fatal(err)
	}
	for cls, want := range golden {
		g := NewTailwindGenerator()
		g.Theme.DarkMode = "media"
		out := g.Generate(map[string]bool{cls: true})
		esc := "." + EscapeClass(cls)
		var got []flatRule
		for _, r := range parseFlatRules(out) {
			// Tailwind prints a selector list as separate rules.
			for _, part := range topLevelSplit(r.sel, ',') {
				got = append(got, flatRule{ats: r.ats, sel: strings.ReplaceAll(strings.TrimSpace(part), esc, "&")})
			}
		}
		if len(got) != len(want) {
			t.Errorf("%s: got %d rules, want %d:\n%s", cls, len(got), len(want), out)
			continue
		}
		for i := range want {
			gs, ws := squash(got[i].sel), squash(want[i].Sel)
			ga, wa := squash(strings.Join(got[i].ats, "|")), squash(strings.Join(want[i].Ats, "|"))
			if gs != ws || ga != wa {
				t.Errorf("%s: rule %d\n got  %s  %v\n want %s  %v", cls, i, gs, got[i].ats, ws, want[i].Ats)
			}
		}
	}
}
