package hostts

import (
	"os"
	"regexp"
	"strings"

	"github.com/kratejs/krate/packages/compiler/internal/diag"
)

// param is one parameter of a function, read from source text.
type param struct {
	name    string
	nameOff int
	typ     string
	typOff  int
}

// paramList reads the parameters of the function whose declaration (or, for a
// const, whose name) starts at off in the masked source m. It returns nil when
// it cannot read them with confidence.
func paramList(m string, off int) []param {
	open := findParamsOpen(m, off)
	if open < 0 {
		return nil
	}
	end := matchParen(m, open)
	if end < 0 {
		return nil
	}
	var out []param
	depth := 0
	start := open + 1
	flush := func(to int) {
		seg := m[start:to]
		if strings.TrimSpace(seg) == "" {
			return
		}
		if p, ok := readParam(m, start, to); ok {
			out = append(out, p)
		}
	}
	for i := open + 1; i < end-1; i++ {
		switch m[i] {
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			depth--
		case '<':
			depth++
		case '>':
			if i > 0 && m[i-1] == '=' {
				continue
			}
			depth--
		case ',':
			if depth == 0 {
				flush(i)
				start = i + 1
			}
		}
		if depth < 0 {
			return nil
		}
	}
	if depth != 0 {
		return nil
	}
	flush(end - 1)
	return out
}

func matchParen(m string, open int) int {
	depth := 0
	for i := open; i < len(m); i++ {
		switch m[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return i + 1
			}
		}
	}
	return -1
}

// findParamsOpen finds the `(` that opens the parameter list of the function
// declaration or const that starts at off.
func findParamsOpen(m string, off int) int {
	i := off
	// Skip a leading decorator and the export keywords.
	for {
		for i < len(m) && strings.IndexByte(" \t\r\n", m[i]) >= 0 {
			i++
		}
		skipped := false
		for _, kw := range []string{"@host", "export", "default"} {
			if strings.HasPrefix(m[i:], kw) && (i+len(kw) >= len(m) || !isIdent(m[i+len(kw)])) {
				i += len(kw)
				skipped = true
			}
		}
		if !skipped {
			break
		}
	}
	// A const: skip `const name`, then an optional type annotation up to `=`.
	for _, kw := range []string{"const", "let", "var"} {
		if strings.HasPrefix(m[i:], kw) && i+len(kw) < len(m) && !isIdent(m[i+len(kw)]) {
			j := i + len(kw)
			for j < len(m) && m[j] == ' ' {
				j++
			}
			for j < len(m) && isIdent(m[j]) {
				j++
			}
			for j < len(m) && strings.IndexByte(" \t\r\n", m[j]) >= 0 {
				j++
			}
			if j < len(m) && m[j] == ':' {
				depth := 0
				for ; j < len(m); j++ {
					c := m[j]
					if c == '=' && j+1 < len(m) && m[j+1] == '>' {
						j++
						continue
					}
					if c == '(' || c == '[' || c == '{' || c == '<' {
						depth++
					} else if c == ')' || c == ']' || c == '}' || c == '>' {
						depth--
					} else if c == '=' && depth == 0 {
						break
					}
				}
			}
			i = j
			break
		}
	}
	for ; i < len(m); i++ {
		switch m[i] {
		case '(':
			return i
		case '{', ';':
			return -1
		case '=':
			if i+1 < len(m) && m[i+1] == '>' {
				return -1
			}
		}
	}
	return -1
}

func readParam(m string, from, to int) (param, bool) {
	i := from
	for i < to && strings.IndexByte(" \t\r\n", m[i]) >= 0 {
		i++
	}
	if strings.HasPrefix(m[i:to], "...") {
		i += 3
	}
	p := param{nameOff: i}
	if i < to && (m[i] == '{' || m[i] == '[') {
		depth := 0
		for ; i < to; i++ {
			if m[i] == '{' || m[i] == '[' {
				depth++
			} else if m[i] == '}' || m[i] == ']' {
				depth--
				if depth == 0 {
					i++
					break
				}
			}
		}
		p.name = "(destructured)"
	} else {
		j := i
		for j < to && isIdent(m[j]) {
			j++
		}
		p.name = m[i:j]
		i = j
	}
	for i < to && (m[i] == '?' || m[i] == '!' || m[i] == ' ' || m[i] == '\t' || m[i] == '\n' || m[i] == '\r') {
		i++
	}
	if i >= to || m[i] != ':' {
		return p, p.name != ""
	}
	i++
	for i < to && strings.IndexByte(" \t\r\n", m[i]) >= 0 {
		i++
	}
	p.typOff = i
	depth := 0
	j := i
	for ; j < to; j++ {
		c := m[j]
		switch c {
		case '(', '[', '{', '<':
			depth++
		case ')', ']', '}':
			depth--
		case '>':
			if j > 0 && m[j-1] == '=' {
				continue
			}
			depth--
		case '=':
			if depth == 0 && !(j+1 < to && m[j+1] == '>') {
				p.typ = strings.TrimSpace(m[i:j])
				return p, true
			}
		}
	}
	p.typ = strings.TrimSpace(m[i:to])
	return p, true
}

var domTypes = map[string]bool{
	"Node": true, "Element": true, "Document": true, "Window": true, "EventTarget": true,
	"Event": true, "UIEvent": true, "MouseEvent": true, "KeyboardEvent": true, "PointerEvent": true,
	"FocusEvent": true, "InputEvent": true, "TouchEvent": true, "WheelEvent": true, "DragEvent": true,
	"ClipboardEvent": true, "CustomEvent": true, "SubmitEvent": true, "HTMLElement": true,
	"SVGElement": true, "HTMLInputElement": true, "HTMLButtonElement": true, "HTMLFormElement": true,
	"HTMLDivElement": true, "HTMLSelectElement": true, "HTMLTextAreaElement": true,
	"HTMLCanvasElement": true, "HTMLAnchorElement": true, "HTMLImageElement": true,
	"HTMLVideoElement": true, "HTMLAudioElement": true, "HTMLSpanElement": true,
	"HTMLTableElement": true, "HTMLLabelElement": true, "HTMLDialogElement": true,
}

var identRe = regexp.MustCompile(`^[A-Za-z_$][\w$]*`)

// typeChecker decides whether an annotation names a type that cannot cross
// the boundary. It only answers yes when it is sure.
type typeChecker struct {
	masked string
	// classOf reports whether an imported name is a class, given its module
	// specifier (the empty string when unknown).
	imported func(name string) (spec string, ok bool)
	isClass  func(spec, name string) bool
}

func (c *typeChecker) declaredHere(name string) bool {
	re := regexp.MustCompile(`\b(?:interface|type|enum)\s+` + regexp.QuoteMeta(name) + `\b`)
	return re.MatchString(c.masked)
}

func (c *typeChecker) classHere(name string) bool {
	re := regexp.MustCompile(`\bclass\s+` + regexp.QuoteMeta(name) + `\b`)
	return re.MatchString(c.masked)
}

// problem returns why typ cannot cross, or "".
func (c *typeChecker) problem(typ string) string {
	typ = strings.TrimSpace(typ)
	typ = strings.TrimPrefix(typ, "readonly ")
	for _, part := range splitTop(typ, '|', '&') {
		part = strings.TrimSpace(part)
		for strings.HasPrefix(part, "(") && strings.HasSuffix(part, ")") && matchParen(part, 0) == len(part) && !strings.Contains(part, "=>") {
			part = strings.TrimSpace(part[1 : len(part)-1])
		}
		if part == "" {
			continue
		}
		if strings.Contains(part, "=>") && !strings.Contains(part, " extends ") {
			return "a function type"
		}
		if strings.HasPrefix(part, "new ") || strings.HasPrefix(part, "new(") || strings.HasPrefix(part, "abstract new") {
			return "a constructor type"
		}
		base := part
		for strings.HasSuffix(base, "[]") {
			base = strings.TrimSpace(strings.TrimSuffix(base, "[]"))
		}
		if strings.HasPrefix(base, "Array<") && strings.HasSuffix(base, ">") {
			if r := c.problem(base[len("Array<") : len(base)-1]); r != "" {
				return r
			}
			continue
		}
		name := identRe.FindString(base)
		if name == "" {
			continue
		}
		if rest := base[len(name):]; rest != "" && rest[0] != '<' {
			continue
		}
		if name == "Function" {
			return "a function type"
		}
		if domTypes[name] && !c.declaredHere(name) && !c.classHere(name) {
			if _, imp := c.imported(name); !imp {
				return "the DOM type " + name
			}
			continue
		}
		if c.classHere(name) {
			return "an instance of the class " + name
		}
		if spec, ok := c.imported(name); ok && c.isClass != nil && c.isClass(spec, name) {
			return "an instance of the class " + name
		}
	}
	return ""
}

// splitTop splits s at the separators that sit outside brackets.
func splitTop(s string, seps ...byte) []string {
	var out []string
	depth, start := 0, 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '(', '[', '{', '<':
			depth++
		case ')', ']', '}':
			depth--
		case '>':
			if i > 0 && s[i-1] == '=' {
				continue
			}
			depth--
		default:
			if depth == 0 && strings.IndexByte(string(seps), s[i]) >= 0 {
				out = append(out, s[start:i])
				start = i + 1
			}
		}
	}
	return append(out, s[start:])
}

// classExportRe finds `export class Name` and `class Name` in a module.
func exportsClass(path, name string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	re := regexp.MustCompile(`\bclass\s+` + regexp.QuoteMeta(name) + `\b`)
	return re.MatchString(mask(string(data)))
}

// boundaryDiag builds the KR7190 diagnostic for a parameter.
func boundaryDiag(rel, src, fn string, p param, why string) diag.Diagnostic {
	line, col := lineCol(src, p.nameOff)
	return diag.New(rel, line, col,
		"parameter "+p.name+" of host function "+fn+" is "+why+", which cannot cross the host boundary",
		"arguments are sent as data (strings, numbers, booleans, arrays, plain objects, Date, Map, Set, bytes): pass the plain values you need instead, for example an id or the element's value",
		lineText(src, line)).WithCode(diag.CodeHostBoundaryType)
}
