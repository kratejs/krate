// Package deploy writes host-specific adapter files (redirects, config) into a
// built site so it can be dropped onto a static host without manual setup.
package deploy

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

// Targets lists the supported deploy adapters.
var Targets = []string{"netlify", "vercel", "cloudflare", "gh-pages"}

// Emit writes the adapter files for target into outDir and returns their paths.
func Emit(target string, cfg *config.Config, outDir string) ([]string, error) {
	if fi, err := os.Stat(outDir); err != nil || !fi.IsDir() {
		return nil, fmt.Errorf("output directory %q not found; run `krate build` first", outDir)
	}
	switch target {
	case "netlify", "cloudflare":
		return emitRedirects(cfg, outDir)
	case "vercel":
		return emitVercel(cfg, outDir)
	case "gh-pages":
		return emitGitHubPages(outDir)
	default:
		return nil, fmt.Errorf("unknown deploy target %q (want one of: %s)", target, strings.Join(Targets, ", "))
	}
}

// emitRedirects writes a `_redirects` file (Netlify + Cloudflare Pages format).
func emitRedirects(cfg *config.Config, outDir string) ([]string, error) {
	if len(cfg.Redirects) == 0 && len(cfg.Rewrites) == 0 {
		return nil, nil
	}
	var b strings.Builder
	for _, r := range cfg.Redirects {
		status := "302"
		if r.Permanent {
			status = "301"
		}
		fmt.Fprintf(&b, "%s\t%s\t%s\n", r.Source, r.Destination, status)
	}
	for _, r := range cfg.Rewrites {
		fmt.Fprintf(&b, "%s\t%s\t200\n", r.Source, r.Destination)
	}
	p := filepath.Join(outDir, "_redirects")
	if err := os.WriteFile(p, []byte(b.String()), 0644); err != nil {
		return nil, fmt.Errorf("writing _redirects: %w", err)
	}
	return []string{p}, nil
}

// emitVercel writes a `vercel.json` with redirects and rewrites.
func emitVercel(cfg *config.Config, outDir string) ([]string, error) {
	type vRedirect struct {
		Source      string `json:"source"`
		Destination string `json:"destination"`
		Permanent   bool   `json:"permanent,omitempty"`
	}
	type vRewrite struct {
		Source      string `json:"source"`
		Destination string `json:"destination"`
	}
	doc := map[string]interface{}{}
	if len(cfg.Redirects) > 0 {
		rs := make([]vRedirect, 0, len(cfg.Redirects))
		for _, r := range cfg.Redirects {
			rs = append(rs, vRedirect{Source: r.Source, Destination: r.Destination, Permanent: r.Permanent})
		}
		doc["redirects"] = rs
	}
	if len(cfg.Rewrites) > 0 {
		rw := make([]vRewrite, 0, len(cfg.Rewrites))
		for _, r := range cfg.Rewrites {
			rw = append(rw, vRewrite{Source: r.Source, Destination: r.Destination})
		}
		doc["rewrites"] = rw
	}
	if len(doc) == 0 {
		doc["cleanUrls"] = true
	}

	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, err
	}
	p := filepath.Join(outDir, "vercel.json")
	if err := os.WriteFile(p, append(data, '\n'), 0644); err != nil {
		return nil, fmt.Errorf("writing vercel.json: %w", err)
	}
	return []string{p}, nil
}

// emitGitHubPages writes `.nojekyll` and a root `404.html`, which GitHub Pages
// requires for SPA-style/fallback routing and to skip Jekyll processing.
func emitGitHubPages(outDir string) ([]string, error) {
	var written []string

	nojekyll := filepath.Join(outDir, ".nojekyll")
	if err := os.WriteFile(nojekyll, nil, 0644); err != nil {
		return nil, fmt.Errorf("writing .nojekyll: %w", err)
	}
	written = append(written, nojekyll)

	dest := filepath.Join(outDir, "404.html")
	if _, err := os.Stat(dest); os.IsNotExist(err) {
		src := filepath.Join(outDir, "404", "index.html")
		if data, err := os.ReadFile(src); err == nil {
			if err := os.WriteFile(dest, data, 0644); err != nil {
				return nil, fmt.Errorf("writing 404.html: %w", err)
			}
			written = append(written, dest)
		}
	}
	return written, nil
}
