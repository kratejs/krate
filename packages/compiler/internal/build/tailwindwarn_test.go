package build

import (
	"path/filepath"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

func TestTailwindOffWarningPointsAtTheDirective(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeFileRel(t, root, "src/styles/globals.css", "body { margin: 0 }\n\n  @import 'tailwindcss';\n")
	writeFileRel(t, root, "node_modules/x/other.css", "@import 'tailwindcss';\n")
	cfg := config.Default()
	cfg.OutDir = root + "/dist"
	file, line, col := New(root, cfg).findTailwindDirective("@import 'tailwindcss'")
	if filepath.ToSlash(file) != "src/styles/globals.css" || line != 3 || col != 3 {
		t.Errorf("got %s:%d:%d, want src/styles/globals.css:3:3", file, line, col)
	}
}
