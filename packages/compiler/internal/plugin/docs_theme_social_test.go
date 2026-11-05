package plugin

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

// The social buttons of the docs header share one wrapper element. The wrapper
// must be a flex container with a gap, or adjacent 32px buttons touch.
func TestDocsThemeSocialLinksHaveGap(t *testing.T) {
	dir := filepath.Join("..", "..", "..", "base-docs-theme", "src")
	chrome, err := os.ReadFile(filepath.Join(dir, "chrome.tsx"))
	if err != nil {
		t.Skipf("base-docs-theme sources not available: %v", err)
	}
	css, err := os.ReadFile(filepath.Join(dir, "theme.css"))
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`<span class="social-links-list">`).Match(chrome) {
		t.Error("SocialLinks must render its links inside .social-links-list")
	}
	rule := regexp.MustCompile(`(?s)\.social-links-list\s*\{[^}]*display:\s*inline-flex;[^}]*gap:\s*[\d.]+rem;`)
	if !rule.Match(css) {
		t.Error("theme.css must make .social-links-list a flex container with a gap")
	}
}
