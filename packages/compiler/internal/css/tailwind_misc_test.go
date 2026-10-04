package css

import (
	"strings"
	"testing"
)

func TestMiscUtilityFamilies(t *testing.T) {
	cases := map[string]string{
		"fill-blue-500":            "fill: ",
		"stroke-2":                 "stroke-width: 2;",
		"stroke-current":           "stroke: currentColor;",
		"text-ellipsis":            "text-overflow: ellipsis;",
		"text-clip":                "text-overflow: clip;",
		"table-fixed":              "table-layout: fixed;",
		"border-collapse":          "border-collapse: collapse;",
		"caption-bottom":           "caption-side: bottom;",
		"bg-clip-text":             "-webkit-background-clip: text;",
		"bg-origin-content":        "background-origin: content-box;",
		"snap-x":                   "scroll-snap-type: x",
		"snap-mandatory":           "--tw-scroll-snap-strictness: mandatory;",
		"forced-color-adjust-none": "forced-color-adjust: none;",
		"ps-4":                     "padding-inline-start: 1rem;",
		"me-2":                     "margin-inline-end:",
		"bg-none":                  "background-image: none;",
	}
	for cls, want := range cases {
		if got := gen(t, cls); !strings.Contains(got, want) {
			t.Errorf("%s: want %q, got: %s", cls, want, got)
		}
	}
}

func TestPlaceholderPseudoElement(t *testing.T) {
	out := gen(t, "placeholder-red-500")
	if !strings.Contains(out, "::placeholder") {
		t.Errorf("placeholder-* must target ::placeholder:\n%s", out)
	}
}
