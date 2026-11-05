package css

import (
	"strconv"
	"strings"
)

// mathFuncs are the CSS math functions whose arguments get whitespace around
// the + - * / operators, as Tailwind v4 does for arbitrary values.
var mathFuncs = map[string]bool{
	"calc": true, "min": true, "max": true, "clamp": true, "mod": true,
	"rem": true, "round": true, "abs": true, "sign": true, "sin": true,
	"cos": true, "tan": true, "asin": true, "acos": true, "atan": true,
	"atan2": true, "pow": true, "sqrt": true, "hypot": true, "log": true,
	"exp": true,
}

// arbFrame is one open parenthesis while normalizing an arbitrary value.
type arbFrame struct {
	math bool // operators inside get spaced
	raw  bool // url(...): copied verbatim
}

// normalizeArbitrary applies Tailwind v4's arbitrary value rules to the text
// between [ and ]: underscores become spaces (a backslash-escaped underscore
// stays one, and nothing changes inside url()), and the operators of math
// functions get surrounding spaces (calc(100%-2rem) -> calc(100% - 2rem)).
func normalizeArbitrary(s string) string {
	if !strings.ContainsAny(s, "_+-*/,\\") {
		return s
	}
	var out strings.Builder
	out.Grow(len(s) + 8)
	var stack []arbFrame
	skipSpace := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		var top arbFrame
		if len(stack) > 0 {
			top = stack[len(stack)-1]
		}
		if top.raw {
			switch c {
			case '(':
				stack = append(stack, arbFrame{raw: true})
			case ')':
				stack = stack[:len(stack)-1]
			}
			out.WriteByte(c)
			continue
		}
		if skipSpace && (c == ' ' || c == '_') {
			continue
		}
		skipSpace = false
		switch {
		case c == '\\' && i+1 < len(s) && s[i+1] == '_':
			out.WriteByte('_')
			i++
		case c == '\\' && i+1 < len(s):
			out.WriteByte(c)
			out.WriteByte(s[i+1])
			i++
		case c == '_':
			out.WriteByte(' ')
		case c == '"' || c == '\'':
			out.WriteByte(c)
			j := i + 1
			for ; j < len(s) && s[j] != c; j++ {
				switch {
				case s[j] == '\\' && j+1 < len(s) && s[j+1] == '_':
					out.WriteByte('_')
					j++
				case s[j] == '\\' && j+1 < len(s):
					out.WriteByte(s[j])
					out.WriteByte(s[j+1])
					j++
				case s[j] == '_':
					out.WriteByte(' ')
				default:
					out.WriteByte(s[j])
				}
			}
			if j < len(s) {
				out.WriteByte(c)
			}
			i = j
		case c == '(':
			name := strings.ToLower(identBefore(out.String()))
			switch {
			case name == "url":
				stack = append(stack, arbFrame{raw: true})
			case name == "" && top.math:
				stack = append(stack, arbFrame{math: true})
			default:
				stack = append(stack, arbFrame{math: mathFuncs[name]})
			}
			out.WriteByte(c)
		case c == ')':
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
			out.WriteByte(c)
		case top.math && (c == '*' || c == '/'):
			writeOperator(&out, c)
			skipSpace = true
		case top.math && (c == '+' || c == '-'):
			if operandEnds(out.String()) {
				writeOperator(&out, c)
				skipSpace = true
			} else {
				out.WriteByte(c)
			}
		case top.math && c == ',':
			trimTrailingSpace(&out)
			out.WriteString(", ")
			skipSpace = true
		default:
			out.WriteByte(c)
		}
	}
	return out.String()
}

func writeOperator(out *strings.Builder, op byte) {
	trimTrailingSpace(out)
	out.WriteByte(' ')
	out.WriteByte(op)
	out.WriteByte(' ')
}

func trimTrailingSpace(out *strings.Builder) {
	s := strings.TrimRight(out.String(), " ")
	if len(s) != out.Len() {
		out.Reset()
		out.WriteString(s)
	}
}

// identBefore returns the identifier immediately preceding the end of s.
func identBefore(s string) string {
	i := len(s)
	for i > 0 {
		c := s[i-1]
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_' {
			i--
			continue
		}
		break
	}
	return s[i:]
}

// operandEnds reports whether the text so far ends with a complete operand
// (number, percentage, dimension or closing paren), so that a following + or -
// is a binary operator rather than a sign.
func operandEnds(s string) bool {
	s = strings.TrimRight(s, " ")
	if s == "" {
		return false
	}
	c := s[len(s)-1]
	switch {
	case c >= '0' && c <= '9', c == '.', c == '%', c == ')':
		return true
	case c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z':
		i := len(s)
		for i > 0 && (s[i-1] >= 'a' && s[i-1] <= 'z' || s[i-1] >= 'A' && s[i-1] <= 'Z') {
			i--
		}
		if i == 0 {
			return false
		}
		p := s[i-1]
		return p >= '0' && p <= '9' || p == '.' || p == '%'
	}
	return false
}

// parenToBracket rewrites the v4 CSS-variable shorthand `name-(--x)` and
// `name-(type:--x)` to the bracket form `name-[var(--x)]` / `name-[type:var(--x)]`,
// and a `/(--alpha)` color modifier to `/[var(--alpha)]`, so one code path
// handles both spellings. Class names without the shorthand are returned as is.
func parenToBracket(cls string) string {
	if !strings.Contains(cls, "(") || !strings.HasSuffix(cls, ")") {
		return cls
	}
	depth := 0
	for i := 0; i < len(cls); i++ {
		switch cls[i] {
		case '[':
			depth++
		case ']':
			if depth > 0 {
				depth--
			}
		case '(':
			if depth > 0 || i == 0 || (cls[i-1] != '-' && cls[i-1] != '/') {
				return cls
			}
			inner := cls[i+1 : len(cls)-1]
			typ := ""
			if j := strings.IndexByte(inner, ':'); j > 0 {
				typ, inner = inner[:j], inner[j+1:]
			}
			if !strings.HasPrefix(inner, "--") || strings.ContainsAny(inner, "[]") {
				return cls
			}
			if typ == "family-name" {
				typ = "family"
			}
			if typ != "" {
				typ += ":"
			}
			return cls[:i] + "[" + typ + "var(" + inner + ")]"
		}
	}
	return cls
}

// plainNumber reports whether s is an unsigned decimal such as 4 or 0.5.
func plainNumber(s string) bool {
	if s == "" || s[0] == '.' || s[len(s)-1] == '.' {
		return false
	}
	dots := 0
	for i := 0; i < len(s); i++ {
		switch {
		case s[i] >= '0' && s[i] <= '9':
		case s[i] == '.':
			dots++
		default:
			return false
		}
	}
	return dots <= 1
}

// formatNum formats a float without trailing zeros or float noise.
func formatNum(f float64) string {
	f = float64(int64(f*1e6+sign(f)*0.5)) / 1e6
	return strconv.FormatFloat(f, 'f', -1, 64)
}

func sign(f float64) float64 {
	if f < 0 {
		return -1
	}
	return 1
}

// spacingMultiple resolves a numeric spacing key (13, 0.5) to a multiple of the
// theme spacing unit (default 0.25rem).
func spacingMultiple(key string, theme TailwindTheme) (string, bool) {
	if !plainNumber(key) {
		return "", false
	}
	f, err := strconv.ParseFloat(key, 64)
	if err != nil {
		return "", false
	}
	if f == 0 {
		return "0", true
	}
	if theme.LiveVars["spacing"] {
		return "calc(var(--spacing) * " + key + ")", true
	}
	unit := strings.TrimSpace(theme.SpacingUnit)
	if unit == "" {
		unit = "0.25rem"
	}
	i := 0
	for i < len(unit) && (unit[i] >= '0' && unit[i] <= '9' || unit[i] == '.') {
		i++
	}
	if i > 0 && plainNumber(unit[:i]) && isUnitSuffix(unit[i:]) {
		n, err := strconv.ParseFloat(unit[:i], 64)
		if err == nil {
			return formatNum(f*n) + unit[i:], true
		}
	}
	return "calc(" + unit + " * " + key + ")", true
}

func isUnitSuffix(s string) bool {
	if s == "" {
		return true
	}
	for i := 0; i < len(s); i++ {
		if !(s[i] >= 'a' && s[i] <= 'z' || s[i] == '%') {
			return false
		}
	}
	return true
}

// negateLength negates a CSS length: literal numbers get a sign, anything else
// is wrapped in calc(... * -1).
func negateLength(v string) string {
	v = strings.TrimSpace(v)
	switch {
	case v == "" || v == "0" || v == "auto":
		return v
	case strings.HasPrefix(v, "-") && !strings.HasPrefix(v, "--") && !strings.ContainsAny(v, "( "):
		return v[1:]
	case v[0] >= '0' && v[0] <= '9' || v[0] == '.':
		if !strings.ContainsAny(v, "( ,") {
			return "-" + v
		}
	case strings.HasPrefix(v, "calc(var(--spacing) * ") && !strings.Contains(v, "-1)"):
		return "calc(var(--spacing) * -" + v[len("calc(var(--spacing) * "):]
	}
	return "calc(" + v + " * -1)"
}
