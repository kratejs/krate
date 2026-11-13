package caps

import (
	"testing"
)

// splitArgs is CommandLineToArgvW for arguments after the program name.
func splitArgs(s string) []string {
	var out []string
	i := 0
	for i < len(s) {
		for i < len(s) && (s[i] == ' ' || s[i] == '\t') {
			i++
		}
		if i >= len(s) {
			break
		}
		var cur []byte
		inQ := false
		for i < len(s) {
			c := s[i]
			if c == '\\' {
				n := 0
				for i < len(s) && s[i] == '\\' {
					n++
					i++
				}
				if i < len(s) && s[i] == '"' {
					cur = append(cur, make([]byte, n/2)...)
					for j := range cur[len(cur)-n/2:] {
						cur[len(cur)-n/2+j] = '\\'
					}
					if n%2 == 1 {
						cur = append(cur, '"')
						i++
					}
				} else {
					for j := 0; j < n; j++ {
						cur = append(cur, '\\')
					}
				}
				continue
			}
			if c == '"' {
				inQ = !inQ
				i++
				continue
			}
			if !inQ && (c == ' ' || c == '\t') {
				break
			}
			cur = append(cur, c)
			i++
		}
		out = append(out, string(cur))
	}
	return out
}

func TestQuoteArgsRoundTrip(t *testing.T) {
	sets := [][]string{
		{"a", "b c", `d"e`, ``, `f\`, `g\\"h`, `C:\Program Files\App\`, "tab\tx", `\\server\share`, `"`, `\"`, `a  b`},
		{"--open", `C:\Users\me\My Documents\a "b".txt`},
	}
	for _, set := range sets {
		line := QuoteArgs(set)
		got := splitArgs(line)
		if len(got) != len(set) {
			t.Fatalf("%q -> %q (%d args, want %d)", set, line, len(got), len(set))
		}
		for i := range set {
			if got[i] != set[i] {
				t.Errorf("arg %d: %q -> %q", i, set[i], got[i])
			}
		}
	}
}
