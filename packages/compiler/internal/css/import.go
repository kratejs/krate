package css

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var importRe = regexp.MustCompile(`@import\s+(?:url\()?["']([^"']+)["']\)?\s*;?`)

// InlineImports resolves and inlines @import directives in CSS content.
// basePath is the directory containing the CSS file, used to resolve relative
// paths. All resolved imports must remain inside basePath (the project root) —
// traversal above it is treated as invalid and left un-inlined.
// It handles circular imports via a visited set and has a max depth of 10.
func InlineImports(css, basePath string) string {
	return inlineImportsDepth(css, basePath, basePath, make(map[string]bool), 0)
}

func inlineImportsDepth(css, basePath, rootPath string, visited map[string]bool, depth int) string {
	if depth > 10 {
		return css
	}

	var result strings.Builder
	remaining := css

	for {
		loc := importRe.FindStringIndex(remaining)
		if loc == nil {
			result.WriteString(remaining)
			break
		}

		// Write everything before the @import
		result.WriteString(remaining[:loc[0]])

		// Extract the import path
		matches := importRe.FindStringSubmatch(remaining[loc[0]:loc[1]])
		if len(matches) < 2 {
			result.WriteString(remaining[loc[0]:loc[1]])
			remaining = remaining[loc[1]:]
			continue
		}
		importPath := matches[1]

		// Reject imports that could read files outside basePath or trigger
		// network requests: URL schemes, protocol-relative URLs, null bytes,
		// and any path whose resolved location escapes basePath (project root).
		if strings.ContainsAny(importPath, "\x00") || isExternalImport(importPath) {
			result.WriteString(remaining[loc[0]:loc[1]])
			remaining = remaining[loc[1]:]
			continue
		}

		// Resolve the imported file path
		resolved := importPath
		if !filepath.IsAbs(importPath) {
			resolved = filepath.Join(basePath, importPath)
		}

		// Containment check: the cleaned absolute path must stay inside basePath.
		absResolved, err := filepath.Abs(resolved)
		if err != nil {
			result.WriteString(remaining[loc[0]:loc[1]])
			remaining = remaining[loc[1]:]
			continue
		}
		if !withinPath(rootPath, absResolved) {
			result.WriteString(remaining[loc[0]:loc[1]])
			remaining = remaining[loc[1]:]
			continue
		}

		// Check for circular imports
		if visited[absResolved] {
			remaining = remaining[loc[1]:]
			continue
		}
		visited[absResolved] = true

		// Read the imported file
		data, err := os.ReadFile(absResolved)
		if err != nil {
			// If file not found, leave the @import as-is
			result.WriteString(remaining[loc[0]:loc[1]])
			remaining = remaining[loc[1]:]
			continue
		}

		importedCSS := string(data)
		importDir := filepath.Dir(absResolved)

		// Recursively inline any @imports in the imported file. Resolution
		// continues relative to the imported file's directory, but containment
		// is always enforced against the original project root.
		importedCSS = inlineImportsDepth(importedCSS, importDir, rootPath, visited, depth+1)

		result.WriteString(importedCSS)
		remaining = remaining[loc[1]:]
	}

	return result.String()
}

// isExternalImport reports whether the import target is a URL or otherwise not
// a plain filesystem path under the project: http(s)://, protocol-relative //,
// and scheme-like prefixes (data:, file:, etc.).
func isExternalImport(importPath string) bool {
	lower := strings.ToLower(strings.TrimSpace(importPath))
	for _, prefix := range []string{"http://", "https://", "//", "data:", "file:", "about:", "javascript:"} {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	return false
}

// withinPath reports whether target resolves to a location inside base.
func withinPath(base, target string) bool {
	baseAbs, err := filepath.Abs(base)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(baseAbs, target)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
