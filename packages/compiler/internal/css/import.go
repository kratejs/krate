package css

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// maxImportDepth bounds @import nesting.
const maxImportDepth = 10

// ImportOptions configures InlineImportsWith.
type ImportOptions struct {
	// Root is the containment root: resolved local files must stay inside it
	// (files found through Resolve inside a node_modules directory are also
	// allowed). Empty means the base directory passed to InlineImportsWith.
	Root string
	// Resolve maps a specifier that is not a file relative to the importing
	// file (for example a package path) to an absolute file, or "".
	Resolve func(spec, fromDir string) string
	// RewriteURLs is applied to the text of every imported file (not the entry
	// sheet) before its own imports are expanded. dir is the imported file's
	// directory.
	RewriteURLs func(css, dir string) string
	// OnRead is called with every file that was read.
	OnRead func(path string)
	// Warn receives a message for every import that could not be honoured.
	Warn func(msg string)
}

// InlineImports resolves and inlines @import directives in CSS content. Paths
// resolve relative to basePath, which is also the containment root. External
// imports are hoisted to the top, unresolved ones are left in place.
func InlineImports(css, basePath string) string {
	return InlineImportsWith(css, basePath, ImportOptions{Root: basePath})
}

// InlineImportsWith inlines @import directives found at the top level of css.
// baseDir is the directory of the sheet that css came from.
func InlineImportsWith(css, baseDir string, opts ImportOptions) string {
	if !hasImport(css) {
		return HoistImports(css)
	}
	if opts.Root == "" {
		opts.Root = baseDir
	}
	st := &importer{opts: opts}
	body, hoisted := st.inline(css, baseDir, nil, 0)
	if len(hoisted) > 0 {
		body = strings.Join(hoisted, "\n") + "\n" + body
	}
	return HoistImports(body)
}

func hasImport(css string) bool {
	return strings.Contains(strings.ToLower(css), "@import")
}

type importer struct {
	opts ImportOptions
}

func (st *importer) warn(format string, args ...any) {
	if st.opts.Warn != nil {
		st.opts.Warn(fmt.Sprintf(format, args...))
	}
}

// importStmt is a parsed `@import` statement.
type importStmt struct {
	target  string
	prelude string
	text    string // the statement as written, without a trailing ';'
}

// parseImport parses the @import statement spanning css[start:end].
func parseImport(css string, start, end int) (importStmt, bool) {
	body := strings.TrimSuffix(css[start:end], ";")
	i := len("@import")
	skip := func() {
		for i < len(body) {
			if isCSSSpace(body[i]) {
				i++
			} else if body[i] == '/' && i+1 < len(body) && body[i+1] == '*' {
				i = skipComment(body, i)
			} else {
				return
			}
		}
	}
	skip()
	if i >= len(body) {
		return importStmt{}, false
	}
	var target string
	switch {
	case body[i] == '"' || body[i] == '\'':
		target, i = unquoteCSSString(body, i)
	case hasFoldAt(body, i, "url("):
		ref, _, e, ok := parseURLToken(body, i)
		if !ok {
			return importStmt{}, false
		}
		target, i = ref, e
	default:
		return importStmt{}, false
	}
	return importStmt{target: target, prelude: strings.TrimSpace(body[i:]), text: strings.TrimSpace(body)}, true
}

// inline expands the imports of css (a file in dir) and returns the result and
// the statements (external @import) that must be hoisted to the top of the
// final sheet.
func (st *importer) inline(css, dir string, chain []string, depth int) (string, []string) {
	var out strings.Builder
	var hoisted []string
	level, i, last := 0, 0, 0
	for i < len(css) {
		c := css[i]
		switch {
		case c == '/' && i+1 < len(css) && css[i+1] == '*':
			i = skipComment(css, i)
		case c == '"' || c == '\'':
			i = cssSkipString(css, i)
		case c == '{':
			level++
			i++
		case c == '}':
			if level > 0 {
				level--
			}
			i++
		case c == '@' && level == 0 && hasFoldAt(css, i, "@import") && isWordEnd(css, i+len("@import")):
			end, _ := stmtEnd(css, i)
			if end < 0 {
				i++
				continue
			}
			stmt, ok := parseImport(css, i, end)
			if !ok {
				st.warn("unsupported @import syntax %q left in place", strings.TrimSpace(css[i:end]))
				i = end
				continue
			}
			out.WriteString(css[last:i])
			last = end
			i = end
			body, hs, keep := st.expand(stmt, dir, chain, depth)
			hoisted = append(hoisted, hs...)
			if keep {
				out.WriteString(stmt.text + ";")
			} else {
				out.WriteString(body)
			}
		default:
			i++
		}
	}
	out.WriteString(css[last:])
	return out.String(), hoisted
}

// expand resolves one import. keep is true when the statement must stay as
// written (it could not be inlined).
func (st *importer) expand(stmt importStmt, dir string, chain []string, depth int) (body string, hoisted []string, keep bool) {
	if strings.ContainsRune(stmt.target, 0) {
		st.warn("@import %q contains a null byte and was not inlined", stmt.target)
		return "", nil, true
	}
	if isExternalImport(stmt.target) {
		return "", []string{stmt.text + ";"}, false
	}
	abs := st.locate(stmt.target, dir)
	if abs == "" {
		return "", nil, true
	}
	if !st.allowed(abs) {
		st.warn("@import %q resolves outside the project root and was not inlined", stmt.target)
		return "", nil, true
	}
	for _, a := range chain {
		if a == abs {
			st.warn("circular @import of %q skipped", stmt.target)
			return "", nil, false
		}
	}
	if depth >= maxImportDepth {
		st.warn("@import %q is nested more than %d levels deep and was not inlined", stmt.target, maxImportDepth)
		return "", nil, true
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		st.warn("@import %q could not be read: %v", stmt.target, err)
		return "", nil, true
	}
	if st.opts.OnRead != nil {
		st.opts.OnRead(abs)
	}
	text := string(data)
	idir := filepath.Dir(abs)
	if st.opts.RewriteURLs != nil {
		text = st.opts.RewriteURLs(text, idir)
	}
	text, hs := st.inline(text, idir, append(append([]string(nil), chain...), abs), depth+1)
	text = stripCharset(text)

	wrapped, cond := wrapImportPrelude(stmt.prelude, text)
	if cond {
		// An @import inside a conditional block is invalid, so a conditional
		// import of a sheet that itself pulls in external stylesheets passes
		// its condition on to those statements.
		for _, h := range hs {
			hoisted = append(hoisted, st.conditionHoisted(h, stmt))
		}
	} else {
		hoisted = hs
	}
	return wrapped + "\n", hoisted, false
}

// conditionHoisted attaches the outer import's prelude to an external @import
// that was found inside a conditionally imported sheet.
func (st *importer) conditionHoisted(h string, outer importStmt) string {
	inner, ok := parseImport(h, 0, len(h))
	if !ok {
		return h
	}
	if inner.prelude != "" {
		st.warn("external %q inside conditional import %q: the outer condition cannot be combined and was dropped", inner.target, outer.target)
		return h
	}
	return strings.TrimSuffix(h, ";") + " " + outer.prelude + ";"
}

// locate finds the file an @import target refers to, warning when none exists.
func (st *importer) locate(target, dir string) string {
	t := strings.TrimPrefix(target, "~")
	if filepath.IsAbs(t) {
		if fileExists(t) {
			return filepath.Clean(t)
		}
		st.warn("@import %q not found", target)
		return ""
	}
	if strings.HasPrefix(t, "/") {
		st.warn("@import %q is root-relative and cannot be resolved; use a relative path or package specifier", target)
		return ""
	}
	if p := filepath.Join(dir, filepath.FromSlash(t)); fileExists(p) {
		return p
	}
	if st.opts.Resolve != nil {
		if p := st.opts.Resolve(t, dir); p != "" {
			return p
		}
	}
	st.warn("@import %q not found (looked relative to %s and in node_modules)", target, dir)
	return ""
}

func (st *importer) allowed(abs string) bool {
	if withinPath(st.opts.Root, abs) {
		return true
	}
	return strings.Contains(filepath.ToSlash(abs), "/node_modules/")
}

func fileExists(p string) bool {
	s, err := os.Stat(p)
	return err == nil && !s.IsDir()
}

// stripCharset removes @charset statements from an inlined sheet; they are only
// valid at the very start of a file.
func stripCharset(css string) string {
	if !strings.Contains(strings.ToLower(css), "@charset") {
		return css
	}
	var out strings.Builder
	level, i, last := 0, 0, 0
	for i < len(css) {
		c := css[i]
		switch {
		case c == '/' && i+1 < len(css) && css[i+1] == '*':
			i = skipComment(css, i)
		case c == '"' || c == '\'':
			i = cssSkipString(css, i)
		case c == '{':
			level++
			i++
		case c == '}':
			if level > 0 {
				level--
			}
			i++
		case c == '@' && level == 0 && hasFoldAt(css, i, "@charset") && isWordEnd(css, i+len("@charset")):
			end, _ := stmtEnd(css, i)
			if end < 0 {
				i++
				continue
			}
			out.WriteString(css[last:i])
			last = end
			i = end
		default:
			i++
		}
	}
	out.WriteString(css[last:])
	return out.String()
}

// wrapImportPrelude applies an `@import` prelude (`layer`, `layer(name)`,
// `supports(...)`, then a media query) to the inlined content as nested
// at-rules: @supports { @media { @layer { ... } } }. cond reports whether a
// wrapper was added.
func wrapImportPrelude(prelude, css string) (string, bool) {
	p := strings.TrimSpace(prelude)
	if p == "" {
		return css, false
	}
	var layer, supports string
	hasLayer := false
	if hasFoldAt(p, 0, "layer") && (len(p) == 5 || p[5] == '(' || isCSSSpace(p[5])) {
		hasLayer = true
		rest := strings.TrimLeft(p[5:], " \t\r\n")
		if strings.HasPrefix(rest, "(") {
			if e := matchParen(rest, 0); e > 0 {
				layer = strings.TrimSpace(rest[1:e])
				p = strings.TrimSpace(rest[e+1:])
			}
		} else {
			p = strings.TrimSpace(p[5:])
		}
	}
	if hasFoldAt(p, 0, "supports") && len(p) > 8 && strings.HasPrefix(strings.TrimLeft(p[8:], " "), "(") {
		rest := strings.TrimLeft(p[8:], " ")
		if e := matchParen(rest, 0); e > 0 {
			supports = supportsCondition(strings.TrimSpace(rest[1:e]))
			p = strings.TrimSpace(rest[e+1:])
		}
	}
	media := p
	out := css
	if hasLayer {
		if layer == "" {
			out = "@layer{" + out + "}"
		} else {
			out = "@layer " + layer + "{" + out + "}"
		}
	}
	if media != "" {
		out = "@media " + media + "{" + out + "}"
	}
	if supports != "" {
		out = "@supports " + supports + "{" + out + "}"
	}
	return out, hasLayer || media != "" || supports != ""
}

// supportsCondition turns the inside of `supports(...)` into an @supports
// prelude: a bare declaration is parenthesised, a condition is kept.
func supportsCondition(inner string) string {
	l := strings.ToLower(inner)
	if strings.HasPrefix(inner, "(") || strings.HasPrefix(l, "not ") || strings.HasPrefix(l, "not(") {
		return inner
	}
	if k := strings.IndexByte(inner, '('); k > 0 && !strings.ContainsRune(inner[:k], ':') {
		return inner
	}
	return "(" + inner + ")"
}

// HoistImports moves @charset and @import statements that appear after other
// rules to the top of the sheet, where browsers honour them. Leading @layer
// statements keep their position ahead of the hoisted imports.
func HoistImports(css string) string {
	lower := strings.ToLower(css)
	if !strings.Contains(lower, "@import") && !strings.Contains(lower, "@charset") {
		return css
	}
	var charset, moved []string
	var out strings.Builder
	leading := true
	leadEnd := 0 // offset in out where hoisted imports are inserted
	level, i, last := 0, 0, 0
	flush := func(upto int) { out.WriteString(css[last:upto]); last = upto }
	for i < len(css) {
		c := css[i]
		switch {
		case isCSSSpace(c) && level == 0:
			i++
		case c == '/' && i+1 < len(css) && css[i+1] == '*':
			i = skipComment(css, i)
		case c == '"' || c == '\'':
			i = cssSkipString(css, i)
		case c == '{':
			level++
			leading = false
			i++
		case c == '}':
			if level > 0 {
				level--
			}
			i++
		case c == '@' && level == 0:
			isCharset := hasFoldAt(css, i, "@charset") && isWordEnd(css, i+len("@charset"))
			isImport := hasFoldAt(css, i, "@import") && isWordEnd(css, i+len("@import"))
			isLayer := hasFoldAt(css, i, "@layer") && isWordEnd(css, i+len("@layer"))
			end, stmt := stmtEnd(css, i)
			switch {
			case (isCharset || isImport) && stmt:
				text := strings.TrimSpace(css[i:end])
				if !strings.HasSuffix(text, ";") {
					text += ";"
				}
				switch {
				case isCharset:
					charset = append(charset, text)
					flush(i)
					last = end
				case leading:
					// already in a valid position
					flush(end)
					leadEnd = out.Len()
				default:
					moved = append(moved, text)
					flush(i)
					last = end
				}
				i = end
			case isLayer && stmt:
				if leading {
					flush(end)
					leadEnd = out.Len()
				}
				i = end
			default:
				leading = false
				i++
			}
		default:
			if level == 0 {
				leading = false
			}
			i++
		}
	}
	flush(len(css))
	body := out.String()
	if len(charset) == 0 && len(moved) == 0 {
		return css
	}
	var head strings.Builder
	if len(charset) > 0 {
		head.WriteString(charset[0])
		head.WriteByte('\n')
	}
	head.WriteString(body[:leadEnd])
	if len(moved) > 0 {
		head.WriteByte('\n')
		head.WriteString(strings.Join(moved, "\n"))
		head.WriteByte('\n')
	}
	head.WriteString(body[leadEnd:])
	return head.String()
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
