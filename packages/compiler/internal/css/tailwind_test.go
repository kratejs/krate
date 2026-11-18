package css

import (
	"strings"
	"testing"
)

func TestEscapeSelector(t *testing.T) {
	tests := map[string]string{
		"w-1/2":         "w-1\\/2",
		"w-[100px]":     "w-\\[100px\\]",
		"bg-red-500/50": "bg-red-500\\/50",
		"text-[#fff]":   "text-\\[\\#fff\\]",
		"p-1.5":         "p-1\\.5",
		"hover:bg":      "hover\\:bg",
		"2xl:flex":      "\\32 xl\\:flex",
		"-mt-4":         "-mt-4",
	}
	for in, want := range tests {
		if got := EscapeClass(in); got != want {
			t.Errorf("EscapeClass(%q) = %q, want %q", in, got, want)
		}
	}
}

// rounded-[...] normalizes like every other arbitrary value: calc() without
// spaces around `-` is invalid, so the browser dropped the declaration.
func TestRoundedArbitraryIsNormalized(t *testing.T) {
	for cls, want := range map[string]string{
		"rounded-[calc(var(--x)-2px)]":   "{border-radius:calc(var(--x) - 2px)}",
		"rounded-[calc(var(--x)_-_2px)]": "{border-radius:calc(var(--x) - 2px)}",
		"rounded-[a_b]":                  "{border-radius:a b}",
		`rounded-[a\_b]`:                 "{border-radius:a_b}",
		"rounded-(--r)":                  "{border-radius:var(--r)}",
	} {
		got := Minify(NewTailwindGenerator().Generate(map[string]bool{cls: true}))
		if !strings.HasSuffix(got, want) {
			t.Errorf("%s: got %s, want ...%s", cls, got, want)
		}
	}
}
