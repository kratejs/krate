package escape

import "testing"

func TestHTML(t *testing.T) {
	cases := map[string]string{
		`<script>alert("x")</script>`: `&lt;script&gt;alert(&quot;x&quot;)&lt;/script&gt;`,
		`a & b`:                       `a &amp; b`,
		`it's`:                        `it&#39;s`,
		"unit\x1fsep":                 "unitsep", // internal array separator stripped
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

func TestJSStringDQ(t *testing.T) {
	cases := map[string]string{
		`he said "hi"`: `"he said \"hi\""`,
		`back\slash`:   `"back\\slash"`,
		"a\nb":         `"a\nb"`,
		`{"a":1}`:      `"{\"a\":1}"`,
	}
	for in, want := range cases {
		if got := JSStringDQ(in); got != want {
			t.Errorf("JSStringDQ(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestUnescapeJSString(t *testing.T) {
	cases := map[string]string{
		`a\nb`:         "a\nb",
		`a\tb`:         "a\tb",
		`a\rb`:         "a\rb",
		`a\bb`:         "a\bb",
		`a\fb`:         "a\fb",
		`a\vb`:         "a\vb",
		`a\0b`:         "a\x00b",
		`a\\b`:         `a\b`,
		`it\'s`:        `it's`,
		`say \"hi\"`:   `say "hi"`,
		"a\\\nb":       "ab", // line continuation elides the newline
		`\x41`:         "A",
		`\u0041`:       "A",
		`\u{1F600}`:    "\U0001F600",
		`\uD83D\uDE00`: "\U0001F600", // surrogate pair
		`\q`:           `\q`,         // unknown escape keeps the backslash
		`no escapes`:   `no escapes`,
	}
	for in, want := range cases {
		if got := UnescapeJSString(in); got != want {
			t.Errorf("UnescapeJSString(%q) = %q, want %q", in, got, want)
		}
	}
}
