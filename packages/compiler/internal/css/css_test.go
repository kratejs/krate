package css

import (
	"strings"
	"testing"
)

func TestMinifyBasic(t *testing.T) {
	input := `
		/* comment */
		body {
			margin: 0;
			padding: 0;
		}
	`
	result := Minify(input)
	if strings.Contains(result, "/*") {
		t.Errorf("expected comments removed, got: %s", result)
	}
	if strings.Contains(result, "\n\n") {
		t.Errorf("expected whitespace collapsed, got: %s", result)
	}
}

func TestShortenHexColors(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"#aabbcc", "#abc"},
		{"#aabbccdd", "#abcd"},
		{"#112233", "#123"},
		{"#abcdee", "#abcdee"}, // not shortenable
	}
	for _, tt := range tests {
		got := shortenHexColors(tt.input)
		if got != tt.expected {
			t.Errorf("shortenHexColors(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestRemoveZeroUnits(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"0px", "0"},
		{"0em", "0"},
		{"0rem 0rem", "0 0"},
		{"10px", "10px"}, // non-zero unchanged
		{"0.0px", "0"},
		{"10.0px", "10.0px"},
		{"1.0rem", "1.0rem"},
		{"translateX(1.0px)", "translateX(1.0px)"},
		{"translateX(0px)", "translateX(0)"},
		{"0fr", "0fr"},
		{"1fr 0fr", "1fr 0fr"},
		{"0s", "0s"},
		{"0ms", "0ms"},
		{"0deg", "0deg"},
		{"0%", "0%"},
		{"calc(100% - 0px)", "calc(100% - 0px)"},
		{"max(0px,10px)", "max(0px,10px)"},
		{"clamp(0px,50%,100px)", "clamp(0px,50%,100px)"},
		{"calc(1px + var(--x,0px))", "calc(1px + var(--x,0px))"},
		{"var(--x,0px)", "var(--x,0px)"},
		{"x-0px", "x-0px"},
		{"#0px", "#0px"},
		{"\\30px", "\\30px"},
	}
	for _, tt := range tests {
		got := removeZeroUnits(tt.input)
		if got != tt.expected {
			t.Errorf("removeZeroUnits(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestRgbaToHex(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"rgba(255, 255, 255, 1)", "#ffffff"},
		{"rgba(0, 0, 0, 1.0)", "#000000"},
		{"rgba(0, 0, 0, 1.00)", "#000000"},
		{"rgb(255, 128, 0)", "#ff8000"},
		{"rgba(255, 255, 255, 0.5)", "rgba(255, 255, 255, 0.5)"}, // alpha != 1, unchanged
		{"rgba(255, 255, 255, 0)", "rgba(255, 255, 255, 0)"},
	}
	for _, tt := range tests {
		got := rgbaToHex(tt.input)
		if got != tt.expected {
			t.Errorf("rgbaToHex(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestSimplifyCalc(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"calc(0 + 10px)", "10px"},
		{"calc(10px + 0)", "10px"},
		{"calc(10px * 1)", "10px"},
		{"calc(10px * 0)", "0px"},
		{"calc(1 * 10px)", "10px"},
		{"calc(0 - 10px)", "-10px"},
		{"calc(100% - 20px)", "calc(100% - 20px)"}, // complex, unchanged
	}
	for _, tt := range tests {
		got := simplifyCalc(tt.input)
		if got != tt.expected {
			t.Errorf("simplifyCalc(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestRemoveDuplicateDeclarations(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{
			"a { color: red; color: blue; }",
			"a {color:blue}",
		},
		{
			// The last occurrence stays at its own position.
			"a { margin: 10px; padding: 5px; margin: 20px; }",
			"a {padding:5px;margin:20px}",
		},
		{
			"a { COLOR: red; color: blue; }", // case-insensitive
			"a {color:blue}",
		},
		{
			// Media-query rules must never bleed declarations across sibling rules.
			"@media (max-width: 1279px) { .sidebar { transform: translateX(-100%); top: 0; } .sidebar.open { transform: translateX(0); } .toc { display: block; top: 48px; } }",
			"@media (max-width: 1279px) { .sidebar {transform:translateX(-100%);top:0} .sidebar.open {transform:translateX(0)} .toc {display:block;top:48px} }",
		},
		{
			// Strings containing semicolons (data URIs, content) must not break
			// the declaration splitter.
			`.x { content: ";"; background: url("data:image/svg+xml;utf8,<svg></svg>"); color: red; color: blue; }`,
			`.x {content:";";background:url("data:image/svg+xml;utf8,<svg></svg>");color:blue}`,
		},
	}
	for _, tt := range tests {
		got := Minify(tt.input)
		if got != tt.expected {
			t.Errorf("Minify(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestRemoveDuplicateDeclarationsPreservesFormatting(t *testing.T) {
	tests := []struct {
		name, input, expected string
	}{
		{
			name:     "folds duplicate, keeps newlines",
			input:    "a {\n  color: red;\n  color: blue;\n}\n",
			expected: "a {\n  color: blue;\n}\n",
		},
		{
			name:     "folds duplicate custom property",
			input:    "a {\n  --x: 1rem;\n  --x: 2rem;\n}\n",
			expected: "a {\n  --x: 2rem;\n}\n",
		},
		{
			name:     "case-distinct custom properties are preserved",
			input:    "a {\n  --Foo: 1;\n  --foo: 2;\n}\n",
			expected: "a {\n  --Foo: 1;\n  --foo: 2;\n}\n",
		},
		{
			name:     "vendor-prefixed fallback preserved",
			input:    "a {\n  display: -webkit-box;\n  display: flex;\n}\n",
			expected: "a {\n  display: -webkit-box;\n  display: flex;\n}\n",
		},
		{
			name:     "earlier important wins",
			input:    "a {\n  color: red !important;\n  color: blue;\n}\n",
			expected: "a {\n  color: red !important;\n}\n",
		},
		{
			name:     "comment survives a folded declaration",
			input:    "a {\n  /* keep */\n  color: red;\n  color: blue;\n}\n",
			expected: "a {\n  /* keep */\n  color: blue;\n}\n",
		},
		{
			name:     "no duplicates is a no-op",
			input:    "a {\n  color: red;\n  margin: 0;\n}\n",
			expected: "a {\n  color: red;\n  margin: 0;\n}\n",
		},
		{
			name:     "recurses into at-rules",
			input:    "@media (x) {\n  a {\n    color: red;\n    color: blue;\n  }\n}\n",
			expected: "@media (x) {\n  a {\n    color: blue;\n  }\n}\n",
		},
	}
	for _, tt := range tests {
		got := RemoveDuplicateDeclarations(tt.input)
		if got != tt.expected {
			t.Errorf("%s: RemoveDuplicateDeclarations(%q) = %q, want %q", tt.name, tt.input, got, tt.expected)
		}
	}
}

func TestMinifyIntegration(t *testing.T) {
	input := `
		/* Full test */
		.container {
			margin: 0px;
			color: rgba(255, 255, 255, 1);
			padding: 10px;
			padding: 20px;
			width: calc(0 + 100%);
		}
		.empty {}
	`
	result := Minify(input)
	if strings.Contains(result, "/*") {
		t.Errorf("comments should be removed: %s", result)
	}
	if strings.Contains(result, "margin: 0px") || strings.Contains(result, "margin:0px") {
		t.Errorf("zero units should be removed: %s", result)
	}
	if strings.Contains(result, "rgba") {
		t.Errorf("rgba(1) should be converted to hex: %s", result)
	}
	if strings.Contains(result, "empty") {
		t.Errorf("empty rules should be removed: %s", result)
	}
	// padding:10px should be deduplicated away, keeping only padding:20px
	if !strings.Contains(result, "padding:20px") {
		t.Errorf("should keep last padding declaration: %s", result)
	}
}
