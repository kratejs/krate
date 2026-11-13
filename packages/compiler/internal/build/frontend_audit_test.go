package build

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

// buildPlain builds files without Tailwind and returns the index page HTML and
// the captured build log.
func buildPlain(t *testing.T, files map[string]string) (html, logged string) {
	t.Helper()
	_, out, logged := buildFiles(t, files, func(c *config.Config) {
		c.Tailwind.Enabled = false
		c.Tailwind.Strict = false
	})
	b, err := os.ReadFile(filepath.Join(out, "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	return string(b), logged
}
