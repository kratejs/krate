package syntaxhighlight

import (
	"strings"
	"testing"
)

func TestHighlightOmitsLineWrappersWithoutMarkedLines(t *testing.T) {
	t.Parallel()
	code := "const a = 1;\nconst b = 2;\n"
	plain := HighlightTheme(code, "ts", DefaultTheme)
	if strings.Contains(plain, `class="line"`) || strings.Contains(plain, `class="cl"`) {
		t.Errorf("unmarked code kept line wrappers: %s", plain)
	}
	if strings.HasPrefix(plain, "<pre") || strings.Contains(plain, "<code") {
		t.Errorf("unexpected block wrapper: %s", plain)
	}
	if got := textOf(plain); got != code {
		t.Errorf("text changed: %q != %q", got, code)
	}

	marked := HighlightThemeLines(code, "ts", DefaultTheme, "2")
	if !strings.Contains(marked, `<span class="line hl">`) || !strings.Contains(marked, `<span class="line">`) {
		t.Errorf("marked code lost its line wrappers: %s", marked)
	}
	if got := textOf(marked); got != code {
		t.Errorf("marked text changed: %q != %q", got, code)
	}
}

// textOf strips tags and unescapes the few entities chroma writes.
func textOf(s string) string {
	var sb strings.Builder
	in := false
	for _, r := range s {
		switch {
		case r == '<':
			in = true
		case r == '>':
			in = false
		case !in:
			sb.WriteRune(r)
		}
	}
	return strings.NewReplacer("&amp;", "&", "&lt;", "<", "&gt;", ">", "&#34;", `"`, "&#39;", "'").Replace(sb.String())
}
