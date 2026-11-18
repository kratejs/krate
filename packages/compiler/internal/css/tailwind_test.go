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

// Arbitrary values pick their property the way Tailwind v4 infers the data
// type: the utility's type list in order, an explicit hint winning, and the
// family's default when nothing matches.
func TestArbitraryValueTypeInference(t *testing.T) {
	for cls, want := range map[string]string{
		"bg-[14px]":                 "{background-position:14px}",
		"bg-[calc(var(--x)-2px)]":   "{background-position:calc(var(--x) - 2px)}",
		"bg-[length:10px_20px]":     "{background-size:10px 20px}",
		"bg-[cover]":                "{background-size:cover}",
		"bg-[url(/a.png)]":          "{background-image:url(/a.png)}",
		"bg-[#fff]":                 "{background-color:#fff}",
		"bg-[var(--x)]":             "{background-color:var(--x)}",
		"text-[14px]":               "{font-size:14px}",
		"text-[red]":                "{color:red}",
		"text-[a_b]":                "{color:a b}",
		"font-[450]":                "font-weight:450}",
		"font-['Inter']":            "{font-family:'Inter'}",
		"font-[calc(var(--x)-2px)]": "font-weight:calc(var(--x) - 2px)}",
		"decoration-[3px]":          "{text-decoration-thickness:3px}",
		"decoration-[a_b]":          "{text-decoration-color:a b}",
		"border-t-[thin]":           "border-top-width:thin}",
		"border-t-[a_b]":            "{border-top-color:a b}",
		"ring-[3px]":                "0 0 0 calc(3px + ",
		"ring-[a_b]":                "{--tw-ring-color:a b}",
		"from-[10%]":                "{--tw-gradient-from-position:10%}",
		"mask-[50%]":                "{mask-position:50%}",
		"blur-[calc(var(--x)-2px)]": "--tw-blur:blur(calc(var(--x) - 2px));",
		"filter-[a_b]":              "{filter:a b}",
		"-col-[a_b]":                "{grid-column:calc(a b * -1)}",
		"@container-[size]":         "{container-type:size}",
	} {
		got := Minify(NewTailwindGenerator().Generate(map[string]bool{cls: true}))
		if !strings.Contains(got, want) {
			t.Errorf("%s: got %s, want ...%s", cls, got, want)
		}
	}
	for v, want := range map[string]string{
		"calc(1px)":       "line-width",
		"1px 2px 3px":     "line-width",
		"#fff":            "color",
		"var(--x)":        "",
		"--spacing(4)":    "line-width",
		"45deg":           "",
		"thin":            "line-width",
		"url(a), url(b)":  "url",
		"cover, contain":  "bg-size",
		"center top 1rem": "position",
	} {
		if got := twInfer(v, "color", "line-width", "length", "bg-size", "position", "url"); got != want {
			t.Errorf("twInfer(%q) = %q, want %q", v, got, want)
		}
	}
}
