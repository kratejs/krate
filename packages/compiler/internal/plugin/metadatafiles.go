package plugin

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/kratejs/krate/packages/compiler/internal/config"
	"github.com/kratejs/krate/packages/compiler/internal/diag"
)

// conventionFile returns the sitemap.ts, robots.ts or manifest.ts file of the
// pages directory (including root-level route groups such as (marketing)/),
// when there is one.
func conventionFile(cfg *config.Config, name string) (string, bool) {
	exts := []string{".ts", ".js", ".mjs"}
	for _, ext := range exts {
		p := filepath.Join(cfg.PagesDir, name+ext)
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			return p, true
		}
	}
	entries, err := os.ReadDir(cfg.PagesDir)
	if err != nil {
		return "", false
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		n := e.Name()
		if len(n) < 3 || n[0] != '(' || n[len(n)-1] != ')' {
			continue
		}
		for _, ext := range exts {
			p := filepath.Join(cfg.PagesDir, n, name+ext)
			if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
				return p, true
			}
		}
	}
	return "", false
}

// announceOverride says that a metadata file replaced what a plugin or the
// config would have written, so the user knows which one won.
func announceOverride(file, out, source string) {
	fmt.Fprintf(os.Stderr, "  \033[33mWarning:\033[0m %s: [%s] %s defines %s, so %s did not write it\n", filepath.Base(file), diag.CodeMetadataRouteOverride, filepath.Base(file), out, source)
	fmt.Fprintf(os.Stderr, "      hint: remove %s or the plugin/config to silence this\n", filepath.Base(file))
}
