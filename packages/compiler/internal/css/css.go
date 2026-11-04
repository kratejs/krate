package css

import (
	"hash/fnv"
	"os"
	"strings"

	"github.com/kratejs/krate/packages/compiler/internal/fsutil"
)

type Asset struct {
	Path     string
	Content  string
	IsModule bool
}

func Collect(dir string) ([]*Asset, error) {
	var assets []*Asset

	err := fsutil.WalkExt(dir, map[string]bool{".css": true}, nil, func(path string, info os.FileInfo) error {
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		isModule := strings.Contains(info.Name(), ".module.")

		assets = append(assets, &Asset{
			Path:     path,
			Content:  string(data),
			IsModule: isModule,
		})
		return nil
	})

	return assets, err
}

func hashPath(path string) string {
	h := fnv.New32a()
	h.Write([]byte(path))
	v := h.Sum32()
	const chars = "abcdefghijklmnopqrstuvwxyz0123456789"
	var buf [6]byte
	for i := 5; i >= 0; i-- {
		buf[i] = chars[v%36]
		v /= 36
	}
	return string(buf[:])
}

// Minify compacts a stylesheet. Value-level transforms (hex shortening, rgb to
// hex, zero units, calc folding) run only on declaration values; selectors,
// at-rule preludes, names and custom property values are never rewritten.
func Minify(cssText string) string {
	// Protect strings, url(...) and /*! license comments so the whitespace and
	// value-level transforms cannot corrupt their contents; restore at the end.
	p := &cssLiteralProtector{}
	cur := collapseCSS(p.protect(strings.ReplaceAll(cssText, "\x00", "\uFFFD")))

	// A rewrite can unblock a later one (for example a dropped declaration no
	// longer separates two related ones), so run to a fixed point.
	for i := 0; i < 4; i++ {
		next := minifyItems(cur, minCtx{}, true)
		if next == cur {
			break
		}
		cur = next
	}
	return strings.TrimSpace(p.restore(cur))
}

// RemoveDuplicateDeclarations folds duplicate declarations within each rule,
// keeping the last value, while preserving the original formatting (whitespace,
// newlines, comments). Unlike css.Minify it performs no whitespace or value
// optimization, so it is safe to apply when minification is disabled.
func RemoveDuplicateDeclarations(css string) string {
	return dedupeWalk(css, minCtx{}, true)
}
