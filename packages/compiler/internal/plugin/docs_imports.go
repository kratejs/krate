package plugin

import (
	"path/filepath"
	"regexp"
	"strings"
)

var (
	esmFromRe   = regexp.MustCompile(`(\bfrom\s*)(["'])([^"'\n]+)(["'])`)
	esmBareRe   = regexp.MustCompile(`^(import\s*)(["'])([^"'\n]+)(["'])`)
	esmDynamRe  = regexp.MustCompile(`(\bimport\s*\(\s*)(["'])([^"'\n]+)(["'])(\s*\))`)
	esmFromHead = regexp.MustCompile(`^(import|export)\b`)
)

// isRelativeSpecifier reports whether an import specifier is a ./ or ../ path.
func isRelativeSpecifier(s string) bool {
	return s == "." || s == ".." || strings.HasPrefix(s, "./") || strings.HasPrefix(s, "../")
}

// relocateImports rewrites the relative specifiers of MDX ESM statements
// (imports, re-exports and dynamic import() calls) so they resolve from the
// generated file in genDir to the same target they name from srcDir, the
// directory of the content file. Aliases, packages and URLs are left alone.
func relocateImports(stmt, srcDir, genDir string) string {
	fix := func(spec string) string {
		if !isRelativeSpecifier(spec) || srcDir == genDir {
			return spec
		}
		target := filepath.Join(srcDir, filepath.FromSlash(spec))
		rel, err := filepath.Rel(genDir, target)
		if err != nil {
			return spec
		}
		rel = filepath.ToSlash(rel)
		if !strings.HasPrefix(rel, ".") {
			rel = "./" + rel
		}
		if strings.HasSuffix(spec, "/") && !strings.HasSuffix(rel, "/") {
			rel += "/"
		}
		return rel
	}
	sub := func(re *regexp.Regexp) func(string) string {
		return func(m string) string {
			g := re.FindStringSubmatch(m)
			return g[1] + g[2] + fix(g[3]) + g[4] + strings.Join(g[5:], "")
		}
	}
	// Static forms only apply to import/export declarations; a "from" inside
	// an exported value's string must not change.
	if esmFromHead.MatchString(stmt) && !isValueExport(stmt) {
		stmt = esmBareRe.ReplaceAllStringFunc(stmt, sub(esmBareRe))
		stmt = esmFromRe.ReplaceAllStringFunc(stmt, sub(esmFromRe))
	}
	return esmDynamRe.ReplaceAllStringFunc(stmt, sub(esmDynamRe))
}

// isValueExport reports whether stmt is an export of a declaration (const,
// function, class ...) rather than a specifier list or re-export.
func isValueExport(stmt string) bool {
	if !strings.HasPrefix(stmt, "export") {
		return false
	}
	rest := strings.TrimSpace(strings.TrimPrefix(stmt, "export"))
	return !strings.HasPrefix(rest, "{") && !strings.HasPrefix(rest, "*") && !strings.HasPrefix(rest, "type {") && !strings.HasPrefix(rest, "type*")
}
