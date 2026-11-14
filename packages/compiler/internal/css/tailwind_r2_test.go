package css

import (
	"testing"
)

// AtThemeApply applies @theme CSS to the generator's theme.
func AtThemeApply(t *testing.T, g *TailwindGenerator, src string) {
	t.Helper()
	ParseAtTheme(src).Apply(&g.Theme)
}
