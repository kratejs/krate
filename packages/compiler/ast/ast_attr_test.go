package ast

import "testing"

func TestHTMLAttrNameReactAliases(t *testing.T) {
	cases := map[string]string{
		"className":       "class",
		"htmlFor":         "for",
		"tabIndex":        "tabindex",
		"readOnly":        "readonly",
		"autoComplete":    "autocomplete",
		"colSpan":         "colspan",
		"srcSet":          "srcset",
		"maxLength":       "maxlength",
		"contentEditable": "contenteditable",
		"acceptCharset":   "accept-charset",
	}
	for in, want := range cases {
		if got := HTMLAttrName(in); got != want {
			t.Errorf("HTMLAttrName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestHTMLAttrNameSVG(t *testing.T) {
	cases := map[string]string{
		"strokeWidth":     "stroke-width",
		"strokeLinecap":   "stroke-linecap",
		"fillRule":        "fill-rule",
		"clipPath":        "clip-path",
		"stopColor":       "stop-color",
		"textAnchor":      "text-anchor",
		"strokeDasharray": "stroke-dasharray",
	}
	for in, want := range cases {
		if got := HTMLAttrName(in); got != want {
			t.Errorf("HTMLAttrName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestHTMLAttrNamePassthrough(t *testing.T) {
	for _, in := range []string{"id", "href", "data-foo", "aria-label", "value"} {
		if got := HTMLAttrName(in); got != in {
			t.Errorf("HTMLAttrName(%q) should pass through, got %q", in, got)
		}
	}
}

func TestHTMLAttrNameUnknownPassesThrough(t *testing.T) {
	if got := HTMLAttrName("customThing"); got != "customThing" {
		t.Errorf("unknown attribute should pass through, got %q", got)
	}
}
