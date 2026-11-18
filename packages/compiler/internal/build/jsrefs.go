package build

import "strings"

// dynamicImportRef is recorded in a reference set for a dynamic import():
// the imported module is code the build did not scan.
const dynamicImportRef = "import("

// dynamicGlobalRef is recorded for a computed member of the global object
// (window[name]): the code can read any global.
const dynamicGlobalRef = "window["

// globalObjects are the names whose members are global bindings:
// window.__krateOwn reads the same binding as __krateOwn.
var globalObjects = map[string]bool{"window": true, "globalThis": true, "self": true}

// regexAfterKeyword lists the keywords after which a slash starts a regular
// expression rather than a division.
var regexAfterKeyword = map[string]bool{
	"return": true, "typeof": true, "instanceof": true, "in": true, "of": true,
	"new": true, "delete": true, "void": true, "throw": true, "case": true,
	"do": true, "else": true, "yield": true, "await": true,
}

type jsTokKind byte

const (
	jsTokNone jsTokKind = iota
	jsTokIdent
	jsTokPunct
	jsTokLiteral // string, number, template or regular expression
)

type jsTok struct {
	kind jsTokKind
	text string
}

// addJSRefs adds to refs the names src may read from the global scope:
// every identifier outside string, template and regular expression literals
// and comments, except property names (a.b, {b: 1}). Members of window,
// globalThis and self count, as does window["name"]. A dynamic import()
// adds dynamicImportRef. Local bindings are not resolved, so a local that
// shares a runtime export's name still counts; that only ships more.
//
// It returns false when src does not tokenize to its end (an unterminated
// literal or comment); the caller must then treat every identifier-like word
// as a reference, since part of the code may have been skipped.
func addJSRefs(refs map[string]bool, src string) bool {
	var prev, prev2 jsTok
	push := func(t jsTok) {
		prev2, prev = prev, t
	}
	// braces tracks open { per nesting level; true marks a template
	// substitution (${), whose } resumes the template.
	var braces []bool
	i := 0
	n := len(src)

	// scanTemplate reads template characters from i up to the closing
	// backtick or a substitution. It reports whether the template ended or
	// opened a substitution, and false for ok when src ends first.
	scanTemplate := func() (ok bool) {
		for i < n {
			switch c := src[i]; {
			case c == '\\':
				i += 2
			case c == '`':
				i++
				push(jsTok{kind: jsTokLiteral})
				return true
			case c == '$' && i+1 < n && src[i+1] == '{':
				i += 2
				braces = append(braces, true)
				push(jsTok{kind: jsTokPunct, text: "{"})
				return true
			default:
				i++
			}
		}
		return false
	}

	for i < n {
		c := src[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f' || c == '\v':
			i++
		case c == '/' && i+1 < n && src[i+1] == '/':
			for i < n && src[i] != '\n' {
				i++
			}
		case c == '/' && i+1 < n && src[i+1] == '*':
			end := strings.Index(src[i+2:], "*/")
			if end < 0 {
				return false
			}
			i += end + 4
		case c == '/' && regexAllowed(prev):
			i++
			inClass := false
			for {
				if i >= n || src[i] == '\n' {
					return false
				}
				ch := src[i]
				i++
				if ch == '\\' {
					i++
				} else if ch == '[' {
					inClass = true
				} else if ch == ']' {
					inClass = false
				} else if ch == '/' && !inClass {
					break
				}
			}
			for i < n && isJSIdentByte(src[i]) {
				i++
			}
			push(jsTok{kind: jsTokLiteral})
		case c == '\'' || c == '"':
			start := i + 1
			i++
			for {
				if i >= n || src[i] == '\n' {
					return false
				}
				ch := src[i]
				if ch == '\\' {
					i += 2
					continue
				}
				i++
				if ch == c {
					break
				}
			}
			body := src[start : i-1]
			if prev.kind == jsTokPunct && prev.text == "[" && prev2.kind == jsTokIdent && globalObjects[prev2.text] && isJSIdent(body) {
				refs[body] = true
			}
			push(jsTok{kind: jsTokLiteral})
		case c == '`':
			i++
			if !scanTemplate() {
				return false
			}
		case c >= '0' && c <= '9' || c == '.' && i+1 < n && src[i+1] >= '0' && src[i+1] <= '9':
			for i < n && (isJSIdentByte(src[i]) || src[i] == '.') {
				i++
			}
			push(jsTok{kind: jsTokLiteral})
		case c == '#' && i+1 < n && isJSIdentStart(src[i+1]):
			// A private name (#x) is a member, never a global.
			i++
			for i < n && isJSIdentByte(src[i]) {
				i++
			}
			push(jsTok{kind: jsTokIdent, text: "#"})
		case isJSIdentStart(c):
			start := i
			for i < n && isJSIdentByte(src[i]) {
				i++
			}
			name := src[start:i]
			member := prev.kind == jsTokPunct && (prev.text == "." || prev.text == "?.")
			switch {
			case member:
				if prev2.kind == jsTokIdent && globalObjects[prev2.text] {
					refs[name] = true
				}
			case name == "import" && nextNonSpace(src, i) == '(':
				refs[dynamicImportRef] = true
			case prev.kind == jsTokPunct && (prev.text == "{" || prev.text == ",") && nextNonSpace(src, i) == ':':
				// An object literal key or a destructuring key.
			default:
				refs[name] = true
			}
			push(jsTok{kind: jsTokIdent, text: name})
		case c == '}':
			i++
			if k := len(braces); k > 0 {
				tmpl := braces[k-1]
				braces = braces[:k-1]
				if tmpl {
					if !scanTemplate() {
						return false
					}
					continue
				}
			}
			push(jsTok{kind: jsTokPunct, text: "}"})
		case c == '{':
			i++
			braces = append(braces, false)
			push(jsTok{kind: jsTokPunct, text: "{"})
		default:
			text := src[i : i+1]
			for _, op := range [...]string{"...", "?.", "++", "--"} {
				if strings.HasPrefix(src[i:], op) {
					// a?.5:1 is a conditional, not optional chaining.
					if op == "?." && i+2 < n && src[i+2] >= '0' && src[i+2] <= '9' {
						continue
					}
					text = op
					break
				}
			}
			if text == "[" && prev.kind == jsTokIdent && globalObjects[prev.text] && !staticMember(src, i+1) {
				refs[dynamicGlobalRef] = true
			}
			i += len(text)
			push(jsTok{kind: jsTokPunct, text: text})
		}
	}
	return true
}

// regexAllowed reports whether a slash after prev starts a regular
// expression.
func regexAllowed(prev jsTok) bool {
	switch prev.kind {
	case jsTokNone:
		return true
	case jsTokIdent:
		return regexAfterKeyword[prev.text]
	case jsTokLiteral:
		return false
	}
	switch prev.text {
	case ")", "]", "++", "--":
		return false
	}
	return true
}

// staticMember reports whether src[i:] starts with a string literal
// member name and the closing bracket: "name"].
func staticMember(src string, i int) bool {
	for i < len(src) && (src[i] == ' ' || src[i] == '\t' || src[i] == '\n' || src[i] == '\r') {
		i++
	}
	if i >= len(src) || src[i] != '"' && src[i] != '\'' {
		return false
	}
	q := src[i]
	end := strings.IndexByte(src[i+1:], q)
	if end < 0 {
		return false
	}
	return isJSIdent(src[i+1:i+1+end]) && nextNonSpace(src, i+2+end) == ']'
}

func nextNonSpace(src string, i int) byte {
	for ; i < len(src); i++ {
		switch src[i] {
		case ' ', '\t', '\n', '\r':
			continue
		}
		return src[i]
	}
	return 0
}

func isJSIdentStart(c byte) bool {
	return c == '_' || c == '$' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= 0x80
}

func isJSIdentByte(c byte) bool {
	return isJSIdentStart(c) || c >= '0' && c <= '9'
}

func isJSIdent(s string) bool {
	if s == "" || !isJSIdentStart(s[0]) {
		return false
	}
	for i := 1; i < len(s); i++ {
		if !isJSIdentByte(s[i]) {
			return false
		}
	}
	return true
}

// jsRefs returns the global references of the given scripts (see
// addJSRefs). A script that does not tokenize adds every identifier-like
// word it contains, and a dynamicImportRef when it mentions import(.
func jsRefs(scripts ...string) map[string]bool {
	refs := make(map[string]bool)
	for _, s := range scripts {
		if s == "" || addJSRefs(refs, s) {
			continue
		}
		for id := range identifierSet(s) {
			refs[id] = true
		}
		if strings.Contains(s, dynamicImportRef) {
			refs[dynamicImportRef] = true
		}
	}
	return refs
}
