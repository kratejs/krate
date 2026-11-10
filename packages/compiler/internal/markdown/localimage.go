package markdown

import (
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/kratejs/krate/packages/compiler/internal/sitepath"
)

// splitRelativeImage reports whether dest is a relative file reference, and
// splits off any ?query or #fragment.
func splitRelativeImage(dest string) (path, suffix string, ok bool) {
	d := strings.TrimSpace(dest)
	if d == "" || strings.HasPrefix(d, "/") || strings.HasPrefix(d, "#") || strings.HasPrefix(d, "?") {
		return "", "", false
	}
	if i := strings.IndexByte(d, ':'); i >= 0 && schemeRe.MatchString(d[:i]) {
		return "", "", false
	}
	if i := strings.IndexAny(d, "?#"); i >= 0 {
		d, suffix = d[:i], d[i:]
	}
	if d == "" {
		return "", "", false
	}
	if u, err := url.PathUnescape(d); err == nil {
		d = u
	}
	return d, suffix, true
}

// LocalImageResolver returns a Config.ImageURL function for one markdown
// file. A relative destination that names an existing file inside root is
// registered through register (source path and its hashed /assets/ URL) and
// replaced by that URL under base. A root-relative destination is a file in
// public/ and gets base. Anything else is left alone.
func LocalImageResolver(root, mdFile, base string, register func(abs, siteURL string)) func(string) (string, bool) {
	dir := filepath.Dir(mdFile)
	return func(dest string) (string, bool) {
		if base != "" && sitepath.IsRootRelative(strings.TrimSpace(dest)) {
			return RootLinkResolver(base)(strings.TrimSpace(dest))
		}
		p, suffix, ok := splitRelativeImage(dest)
		if !ok {
			return "", false
		}
		abs := filepath.Clean(filepath.Join(dir, filepath.FromSlash(p)))
		if rel, err := filepath.Rel(root, abs); err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return "", false
		}
		if fi, err := os.Stat(abs); err != nil || fi.IsDir() {
			return "", false
		}
		data, err := os.ReadFile(abs)
		if err != nil {
			return "", false
		}
		sum := sha256.Sum256(data)
		ext := filepath.Ext(abs)
		name := safeAssetName(strings.TrimSuffix(filepath.Base(abs), ext))
		siteURL := "/assets/" + name + "-" + hex.EncodeToString(sum[:4]) + ext
		register(abs, siteURL)
		return strings.TrimRight(base, "/") + siteURL + suffix, true
	}
}

// RootLinkResolver returns a Config.LinkURL function that adds base to a
// root-relative destination. It returns nil for a site at the root.
func RootLinkResolver(base string) func(string) (string, bool) {
	if base == "" {
		return nil
	}
	return func(dest string) (string, bool) {
		if !sitepath.IsRootRelative(dest) {
			return "", false
		}
		return sitepath.Join(base, dest), true
	}
}

// safeAssetName keeps a file name URL-safe by replacing anything outside
// [A-Za-z0-9._-] with a hyphen.
func safeAssetName(name string) string {
	var b strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '_', r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	if b.Len() == 0 {
		return "image"
	}
	return b.String()
}
