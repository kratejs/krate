package build

import (
	"encoding/json"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/kratejs/krate/packages/compiler/internal/css"
	"github.com/kratejs/krate/packages/compiler/internal/sitepath"
)

// generatedURLPrefixes are the output directories the compiler writes hashed
// files into. Their URLs always belong to this site.
var generatedURLPrefixes = []string{"/assets/", "/chunks/", "/workers/", "/_krate/"}

// basePathBanner is the esbuild banner that gives server-side code the site
// base path, which reaches the browser through window.__KRATE_CFG__. It returns
// nil for a site at the root.
func basePathBanner(base string) map[string]string {
	if base == "" {
		return nil
	}
	cfg, _ := json.Marshal(map[string]string{"basePath": base})
	return map[string]string{"js": "globalThis.__KRATE_CFG__=" + string(cfg) + ";"}
}

// prefixCSSURLs adds the base path to root-relative url() references in a
// finished stylesheet. A reference only moves when it names a file this site
// serves: a generated asset or a file in public/. Anything else may belong to
// another app on the same host, so it stays as written.
func (b *Builder) prefixCSSURLs(cssText string) string {
	base := b.Cfg.BaseURLPath()
	if base == "" {
		return cssText
	}
	return css.RewriteURLs(cssText, func(raw string) (string, bool) {
		ref := strings.TrimSpace(raw)
		if !sitepath.IsRootRelative(ref) || !b.isSiteFile(ref) {
			return "", false
		}
		return base + ref, true
	})
}

// headURLRe matches a <link href> or <script src> holding a root-relative URL.
var headURLRe = regexp.MustCompile(`(<(?:link|script)\b[^>]*?\s(?:href|src)=")(/[^/"][^"]*)(")`)

// prefixHeadURLs adds the base path to stylesheets, icons, preloads and scripts
// a page lists in its <head>. Like prefixCSSURLs, it only touches URLs of files
// this site serves, so a theme can write `/site.css` and work under any base.
func (b *Builder) prefixHeadURLs(doc string) string {
	base := b.Cfg.BaseURLPath()
	if base == "" {
		return doc
	}
	end := strings.Index(doc, "</head>")
	if end < 0 {
		return doc
	}
	head := headURLRe.ReplaceAllStringFunc(doc[:end], func(tag string) string {
		m := headURLRe.FindStringSubmatch(tag)
		if !b.isSiteFile(m[2]) {
			return tag
		}
		return m[1] + base + m[2] + m[3]
	})
	return head + doc[end:]
}

// isSiteFile reports whether a root-relative URL names a generated asset or a
// file in public/.
func (b *Builder) isSiteFile(ref string) bool {
	if i := strings.IndexAny(ref, "?#"); i >= 0 {
		ref = ref[:i]
	}
	for _, p := range generatedURLPrefixes {
		if strings.HasPrefix(ref, p) {
			return true
		}
	}
	if b.Cfg.PublicDir == "" {
		return false
	}
	// path.Clean on a rooted path cannot climb out of public/.
	fi, err := os.Stat(filepath.Join(b.Cfg.PublicDir, filepath.FromSlash(path.Clean(ref))))
	return err == nil && !fi.IsDir()
}
