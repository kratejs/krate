package escape

import "testing"

func TestHTML(t *testing.T) {
	cases := map[string]string{
		`<script>alert("x")</script>`: `&lt;script&gt;alert(&quot;x&quot;)&lt;/script&gt;`,
		`a & b`:                       `a &amp; b`,
		`it's`:                        `it&#39;s`,
		`plain text`:                  `plain text`,
	}
	for in, want := range cases {
		if got := HTML(in); got != want {
			t.Errorf("HTML(%q) = %q, want %q", in, got, want)
		}
	}
	if HTMLAttr("a<b") != HTML("a<b") {
		t.Error("HTMLAttr must be an alias of HTML")
	}
}

func TestJSString(t *testing.T) {
	cases := map[string]string{
		`a\b`:      `a\\b`,
		`it's`:     `it\'s`,
		"a\nb":     `a\nb`,
		"a\rb":     `a\rb`,
		"a\tb":     `a\tb`,
		"a\x00b":   `a\x00b`,
		"a\u2028b": `a\u2028b`,
		"a\u2029b": `a\u2029b`,
		`plain`:    `plain`,
	}
	for in, want := range cases {
		if got := JSString(in); got != want {
			t.Errorf("JSString(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestTemplateRaw(t *testing.T) {
	cases := []struct{ in, want string }{
		{"a\\nb", "a\nb"},            // escape -> newline
		{"a\\\\nb", "a\\nb"},         // literal backslash-n preserved
		{"a\\tb", "a\tb"},            // tab
		{"say \\`hi\\`", "say `hi`"}, // escaped backtick
		{"\\x41\\u0042\\u{43}", "ABC"},
		{"plain", "plain"},
		{"a\\\nb", "ab"}, // line continuation
	}
	for _, c := range cases {
		if got := TemplateRaw(c.in); got != c.want {
			t.Errorf("TemplateRaw(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
