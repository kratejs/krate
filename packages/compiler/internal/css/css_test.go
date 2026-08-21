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
		{"margin: 0px;", "margin: 0;"},
		{"width: 0em;", "width: 0;"},
		{"gap: 0rem;", "gap: 0;"},
		{"width: 10px;", "width: 10px;"}, // non-zero unchanged
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
		{"calc(10px * 0)", "0"},
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

func TestRemoveTrailingSemicolons(t *testing.T) {
	input := "color: red; margin: 0; }"
	expected := "color: red; margin: 0}"
	got := removeTrailingSemicolons(input)
	if got != expected {
		t.Errorf("removeTrailingSemicolons(%q) = %q, want %q", input, got, expected)
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
			"a { margin: 10px; padding: 5px; margin: 20px; }",
			"a {margin:20px;padding:5px}",
		},
		{
			"a { COLOR: red; color: blue; }", // case-insensitive
			"a {color:blue}",
		},
	}
	for _, tt := range tests {
		got := removeDuplicateDeclarations(tt.input)
		if got != tt.expected {
			t.Errorf("removeDuplicateDeclarations(%q) = %q, want %q", tt.input, got, tt.expected)
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
