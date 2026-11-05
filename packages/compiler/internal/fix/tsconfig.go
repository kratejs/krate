package fix

import (
	"encoding/json"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/kratejs/krate/packages/compiler/internal/config"
	"github.com/kratejs/krate/packages/compiler/internal/diag"
)

// fixTSConfig repairs the one tsconfig mistake whose intent is unambiguous: a
// baseUrl that names a subdirectory (./src) while every paths target repeats
// that directory (@/* -> ./src/*). tsc and Krate resolve targets against
// baseUrl, so the targets can only have meant the project root. The fix is
// gated on the file system: each target must exist under the project root and
// not under baseUrl.
func fixTSConfig(root string) (string, []Change, bool) {
	file := filepath.Join(root, "tsconfig.json")
	raw, err := os.ReadFile(file)
	if err != nil {
		return "", nil, false
	}
	src := string(raw)
	var parsed struct {
		CompilerOptions struct {
			BaseURL *string             `json:"baseUrl"`
			Paths   map[string][]string `json:"paths"`
		} `json:"compilerOptions"`
	}
	if json.Unmarshal(config.StripJSONC(raw), &parsed) != nil {
		return "", nil, false
	}
	co := parsed.CompilerOptions
	if co.BaseURL == nil || len(co.Paths) == 0 {
		return "", nil, false
	}
	base := cleanRel(*co.BaseURL)
	if base == "" || base == "." || strings.HasPrefix(base, "..") || path.IsAbs(base) {
		return "", nil, false
	}
	for _, targets := range co.Paths {
		if len(targets) == 0 {
			return "", nil, false
		}
		for _, t := range targets {
			if !targetMeantRoot(root, base, t) {
				return "", nil, false
			}
		}
	}
	start, end, line, col, ok := findBaseURLValue(src)
	if !ok {
		return "", nil, false
	}
	out := src[:start] + `"."` + src[end:]
	return out, []Change{{
		File: "tsconfig.json", Line: line, Col: col, Code: diag.CodeTSConfigBaseURL,
		From: src[start:end], To: `"."`,
	}}, true
}

// cleanRel normalizes a tsconfig path to a clean slash-separated relative
// path ("./src/" becomes "src").
func cleanRel(p string) string {
	p = strings.ReplaceAll(p, `\`, "/")
	if p == "" {
		return ""
	}
	return path.Clean(p)
}

// targetMeantRoot reports whether a paths target starts with the baseUrl
// directory, exists relative to the project root, and does not exist relative
// to baseUrl.
func targetMeantRoot(root, base, target string) bool {
	t := cleanRel(target)
	if t == "" || t == "." || path.IsAbs(t) || strings.HasPrefix(t, "..") {
		return false
	}
	if t != base && !strings.HasPrefix(t, base+"/") {
		return false
	}
	// Drop the wildcard and anything after it: "src/*" and "src/lib/*.ts".
	literal := t
	if i := strings.IndexByte(literal, '*'); i >= 0 {
		literal = literal[:i]
	}
	literal = strings.TrimRight(literal, "/")
	if literal == "" {
		return false
	}
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(literal))); err != nil {
		return false
	}
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(base), filepath.FromSlash(literal))); err == nil {
		return false
	}
	return true
}

// findBaseURLValue locates the string value of compilerOptions.baseUrl in
// JSONC source, skipping comments, and returns its byte span (quotes
// included) plus the 1-based line and column of the opening quote.
func findBaseURLValue(src string) (start, end, line, col int, ok bool) {
	var keys []string // key whose value is being read, per object depth
	depth := 0
	lineNo, lineStart := 1, 0
	i := 0
	for i < len(src) {
		c := src[i]
		switch {
		case c == '\n':
			lineNo++
			lineStart = i + 1
			i++
		case c == '/' && i+1 < len(src) && src[i+1] == '/':
			for i < len(src) && src[i] != '\n' {
				i++
			}
		case c == '/' && i+1 < len(src) && src[i+1] == '*':
			i += 2
			for i+1 < len(src) && !(src[i] == '*' && src[i+1] == '/') {
				if src[i] == '\n' {
					lineNo++
					lineStart = i + 1
				}
				i++
			}
			i += 2
		case c == '{':
			depth++
			for len(keys) < depth {
				keys = append(keys, "")
			}
			keys[depth-1] = ""
			i++
		case c == '}':
			if depth > 0 {
				keys[depth-1] = ""
				depth--
			}
			i++
		case c == '"':
			s := i
			i++
			for i < len(src) && src[i] != '"' {
				if src[i] == '\\' {
					i++
				}
				i++
			}
			if i >= len(src) {
				return 0, 0, 0, 0, false
			}
			i++
			text := src[s:i]
			j := i
			for j < len(src) && (src[j] == ' ' || src[j] == '\t' || src[j] == '\r' || src[j] == '\n') {
				j++
			}
			if j < len(src) && src[j] == ':' && depth > 0 {
				var key string
				if json.Unmarshal([]byte(text), &key) == nil {
					keys[depth-1] = key
				}
				continue
			}
			if depth == 2 && keys[0] == "compilerOptions" && keys[1] == "baseUrl" {
				return s, i, lineNo, s - lineStart + 1, true
			}
		default:
			i++
		}
	}
	return 0, 0, 0, 0, false
}
