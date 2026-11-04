package css

import "strings"

// maxGroupLen bounds how far a bracket or paren group may extend, so
// unbalanced punctuation in minified files cannot cause quadratic scans.
const maxGroupLen = 400

// candidateTokens extracts Tailwind-looking tokens from any source text (TSX,
// TS, MDX, Markdown, HTML). Like Tailwind's own extractor it does not depend on
// string-literal tokenization: the file is split on boundary characters,
// bracket and paren groups are kept whole (including quotes and operators
// inside them), and each token is kept when it has the shape of a utility.
func candidateTokens(src string) []string {
	var out []string
	seen := map[string]bool{}
	extractCandidates(src, func(tok string) {
		tok = cleanCandidate(tok)
		if tok == "" || seen[tok] || !looksLikeUtility(tok) {
			return
		}
		seen[tok] = true
		out = append(out, tok)
	})
	return out
}

// isTokenByte reports bytes that may appear in a candidate outside brackets.
func isTokenByte(c byte) bool {
	switch {
	case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		return true
	}
	switch c {
	case '-', '_', '.', '/', '%', '#', '!', ':', '@', '*':
		return true
	}
	return false
}

// extractCandidates calls emit for every raw token in src.
func extractCandidates(src string, emit func(string)) {
	n := len(src)
	i := 0
	for i < n {
		c := src[i]
		// A literal backslash-n/t/r escape inside a JS string is whitespace.
		if c == '\\' && i+1 < n && (src[i+1] == 'n' || src[i+1] == 't' || src[i+1] == 'r') {
			i += 2
			continue
		}
		if !isTokenByte(c) && c != '[' && c != '(' {
			i++
			continue
		}
		start := i
		for i < n {
			c = src[i]
			if isTokenByte(c) {
				i++
				continue
			}
			if c == '[' || c == '(' {
				end, ok := matchGroup(src, i)
				if !ok {
					break
				}
				if isCodeGroup(src, start, i, end) {
					// Call arguments or array literal: the token ends here and
					// the group content is scanned as ordinary source.
					emit(src[start:i])
					extractCandidates(src[i+1:end-1], emit)
					i = end
					start = i
					continue
				}
				i = end
				continue
			}
			break
		}
		if i > start {
			emit(src[start:i])
		} else {
			i++
		}
	}
}

// matchGroup returns the index just past the bracket or paren group opening at
// i. Quotes inside a group protect closers (`content-[')']`). Whitespace, an
// unterminated quote, or an oversized group means "not a utility group".
func matchGroup(src string, i int) (int, bool) {
	var stack []byte
	limit := i + maxGroupLen
	if limit > len(src) {
		limit = len(src)
	}
	for j := i; j < limit; j++ {
		c := src[j]
		switch c {
		case '[':
			stack = append(stack, ']')
		case '(':
			stack = append(stack, ')')
		case ']', ')':
			if len(stack) == 0 || stack[len(stack)-1] != c {
				return 0, false
			}
			stack = stack[:len(stack)-1]
			if len(stack) == 0 {
				return j + 1, true
			}
		case '\'', '"', '`':
			k := j + 1
			for k < limit && src[k] != c {
				if src[k] == ' ' || src[k] == '\n' || src[k] == '\t' || src[k] == '\r' {
					return 0, false
				}
				if src[k] == '\\' {
					k++
				}
				k++
			}
			if k >= limit {
				return 0, false
			}
			j = k
		case ' ', '\n', '\t', '\r', '{', '}', ';':
			return 0, false
		}
	}
	return 0, false
}

// isCodeGroup distinguishes a utility's arbitrary-value group (`w-[10px]`,
// `bg-(--x)`, `[&_p]:mt-2`) from code punctuation (`clsx("a","b")`, `arr[0]`,
// `["a","b"]`). start is the token start, open the group's opening index.
func isCodeGroup(src string, start, open, end int) bool {
	inner := src[open+1 : end-1]
	var prev byte
	if open > start {
		prev = src[open-1]
	}
	if src[open] == '(' {
		// Only `name-(--var)` style groups are utilities.
		return prev != '-'
	}
	if open == start || prev == '-' || prev == ':' || prev == '/' || prev == '@' {
		if inner != "" && (inner[0] == '"' || inner[0] == '\'' || inner[0] == '`') {
			return open == start
		}
		return false
	}
	return true
}

// cleanCandidate strips punctuation that belongs to the surrounding text
// rather than the class: a leading selector dot and trailing sentence dots.
func cleanCandidate(tok string) string {
	for strings.HasPrefix(tok, ".") {
		tok = tok[1:]
	}
	for strings.HasSuffix(tok, ".") {
		tok = tok[:len(tok)-1]
	}
	return tok
}

// splitCandidate splits a candidate at depth-0 colons into variant tokens and
// the base utility, honouring [..] and (..) groups.
func splitCandidate(tok string) (variants []string, base string) {
	depth, start := 0, 0
	var quote byte
	for i := 0; i < len(tok); i++ {
		if quote != 0 {
			if tok[i] == quote {
				quote = 0
			}
			continue
		}
		switch tok[i] {
		case '\'', '"', '`':
			if depth > 0 {
				quote = tok[i]
			}
		case '[', '(':
			depth++
		case ']', ')':
			if depth > 0 {
				depth--
			}
		case ':':
			if depth == 0 {
				variants = append(variants, tok[start:i])
				start = i + 1
			}
		}
	}
	return variants, tok[start:]
}

// looksLikeUtility is the shape filter that keeps prose and code identifiers
// out of the generator. After the variants are removed the base must be a
// bare utility keyword, an arbitrary property (`[mask-type:alpha]`), or contain
// a hyphen; dynamic prefixes (`bg-`), CSS variables (`--x`) and unbalanced
// groups are rejected.
func looksLikeUtility(tok string) bool {
	if len(tok) == 0 || len(tok) > 300 {
		return false
	}
	if strings.HasSuffix(tok, "-") || strings.HasSuffix(tok, ":") || strings.HasPrefix(tok, "--") {
		return false
	}
	depth := 0
	var quote byte
	for i := 0; i < len(tok); i++ {
		if quote != 0 {
			if tok[i] == quote {
				quote = 0
			}
			continue
		}
		switch tok[i] {
		case '\'', '"', '`':
			if depth > 0 {
				quote = tok[i]
			}
		case '[', '(':
			depth++
		case ']', ')':
			depth--
			if depth < 0 {
				return false
			}
		}
	}
	if depth != 0 || quote != 0 {
		return false
	}
	variants, base := splitCandidate(tok)
	for _, v := range variants {
		if v == "" {
			return false
		}
	}
	base = strings.TrimPrefix(strings.TrimSuffix(base, "!"), "!")
	if base == "" {
		return false
	}
	if base[0] == '[' {
		// Arbitrary property: [color:red].
		return strings.HasSuffix(base, "]") && strings.Contains(base, ":")
	}
	if base[0] == '-' {
		base = base[1:]
		if base == "" || base[0] < 'a' || base[0] > 'z' {
			return false
		}
	}
	if strings.Contains(base, "-") {
		return true
	}
	// A bare keyword: compare the part before any "/" (none today) and groups.
	return bareUtilities[base]
}
