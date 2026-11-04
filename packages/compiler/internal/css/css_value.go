package css

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var (
	hexColorRe = regexp.MustCompile(`#[0-9a-fA-F]{6,8}\b`)
	rgbaRe     = regexp.MustCompile(`rgba\(\s*(\d{1,3})\s*,\s*(\d{1,3})\s*,\s*(\d{1,3})\s*,\s*(0?\.?\d+|1\.0*)\s*\)`)
	rgbRe      = regexp.MustCompile(`rgb\(\s*(\d{1,3})\s*,\s*(\d{1,3})\s*,\s*(\d{1,3})\s*\)`)
	rgbSpaceRe = regexp.MustCompile(`rgb\(\s*(\d{1,3})\s+(\d{1,3})\s+(\d{1,3})\s*(?:/\s*([0-9.]+)\s*)?\)`)
)

// minifyValue applies the value-level transforms to one declaration value.
// Custom property values only get color shortening: they are substituted
// verbatim into var()/calc() later, so their numbers and units must survive.
func minifyValue(prop, val string, ctx minCtx) string {
	val = rgbaToHex(val)
	val = shortenHexColors(val)
	if strings.HasPrefix(prop, "--") || ctx.property {
		return val
	}
	val = simplifyCalc(val)
	switch stripVendorPrefix(strings.ToLower(prop)) {
	case "flex", "flex-basis":
		// 0px and 0 differ in the flex shorthand (0 is a flex-grow number).
		return val
	}
	return removeZeroUnits(val)
}

// shortenHexColors shortens #rrggbb to #rgb when possible, and #rrggbbaa to #rgba.
func shortenHexColors(val string) string {
	return hexColorRe.ReplaceAllStringFunc(val, func(match string) string {
		if len(match) == 7 { // #rrggbb
			if match[1] == match[2] && match[3] == match[4] && match[5] == match[6] {
				return "#" + string(match[1]) + string(match[3]) + string(match[5])
			}
		}
		if len(match) == 9 { // #rrggbbaa
			if match[1] == match[2] && match[3] == match[4] && match[5] == match[6] && match[7] == match[8] {
				return "#" + string(match[1]) + string(match[3]) + string(match[5]) + string(match[7])
			}
		}
		return match
	})
}

// rgbaToHex converts opaque color functions to #hex. Both the legacy
// comma-separated syntax (rgba(r,g,b,1), rgb(r,g,b)) and the modern
// space-separated syntax (rgb(r g b), rgb(r g b / 1)) are handled.
func rgbaToHex(val string) string {
	val = rgbaRe.ReplaceAllStringFunc(val, func(match string) string {
		parts := rgbaRe.FindStringSubmatch(match)
		if len(parts) < 5 {
			return match
		}
		a := parts[4]
		if a != "1" && a != "1.0" && a != "1.00" {
			return match
		}
		return fmt.Sprintf("#%02x%02x%02x", parseByte(parts[1]), parseByte(parts[2]), parseByte(parts[3]))
	})
	val = rgbRe.ReplaceAllStringFunc(val, func(match string) string {
		parts := rgbRe.FindStringSubmatch(match)
		if len(parts) < 4 {
			return match
		}
		return fmt.Sprintf("#%02x%02x%02x", parseByte(parts[1]), parseByte(parts[2]), parseByte(parts[3]))
	})
	val = rgbSpaceRe.ReplaceAllStringFunc(val, func(match string) string {
		parts := rgbSpaceRe.FindStringSubmatch(match)
		if len(parts) < 5 {
			return match
		}
		if a := strings.TrimSpace(parts[4]); a != "" && a != "1" && a != "1.0" {
			return match
		}
		return fmt.Sprintf("#%02x%02x%02x", parseByte(parts[1]), parseByte(parts[2]), parseByte(parts[3]))
	})
	return val
}

func parseByte(s string) uint8 {
	n := 0
	for _, c := range s {
		if c >= '0' && c <= '9' {
			n = n*10 + int(c-'0')
		}
	}
	if n > 255 {
		n = 255
	}
	return uint8(n)
}

var zeroStripUnits = map[string]bool{
	"px": true, "em": true, "rem": true, "ex": true, "ch": true, "pt": true,
	"pc": true, "in": true, "cm": true, "mm": true, "q": true, "vw": true,
	"vh": true, "vmin": true, "vmax": true, "vi": true, "vb": true, "lh": true,
	"rlh": true, "cap": true, "ic": true,
	"svw": true, "svh": true, "lvw": true, "lvh": true, "dvw": true, "dvh": true,
	"svmin": true, "svmax": true, "lvmin": true, "lvmax": true, "dvmin": true, "dvmax": true,
	"cqw": true, "cqh": true, "cqi": true, "cqb": true, "cqmin": true, "cqmax": true,
}

// Functions whose arguments are math or substituted token streams: a unit on
// a zero there is significant (calc(100% - 0px)) or lands elsewhere later.
var zeroKeepFuncs = map[string]bool{
	"calc": true, "min": true, "max": true, "clamp": true, "abs": true,
	"sign": true, "mod": true, "rem": true, "round": true, "hypot": true,
	"sqrt": true, "pow": true, "log": true, "exp": true, "sin": true,
	"cos": true, "tan": true, "asin": true, "acos": true, "atan": true,
	"atan2": true, "calc-size": true, "var": true, "env": true, "attr": true,
	"if": true,
}

// removeZeroUnits turns a zero length such as 0px into 0 in a declaration
// value. Only whole number tokens that are exactly zero are changed, and never
// inside math functions or var()/env() fallbacks. Units that are not lengths
// (fr, %, s, deg) are left alone.
func removeZeroUnits(val string) string {
	if !strings.ContainsRune(val, '0') {
		return val
	}
	var b strings.Builder
	b.Grow(len(val))
	var stack []bool
	keep := 0
	pending := ""
	n := len(val)
	for i := 0; i < n; {
		c := val[i]
		switch {
		case c == 0:
			end := n
			if j := strings.IndexByte(val[i+1:], 0); j >= 0 {
				end = i + 1 + j + 1
			}
			b.WriteString(val[i:end])
			i = end
			pending = ""
		case c == '"' || c == '\'':
			end := cssSkipString(val, i)
			b.WriteString(val[i:end])
			i = end
			pending = ""
		case c == '(':
			blocked := zeroKeepFuncs[stripVendorPrefix(strings.ToLower(pending))]
			stack = append(stack, blocked)
			if blocked {
				keep++
			}
			pending = ""
			b.WriteByte(c)
			i++
		case c == ')':
			if len(stack) > 0 {
				if stack[len(stack)-1] {
					keep--
				}
				stack = stack[:len(stack)-1]
			}
			pending = ""
			b.WriteByte(c)
			i++
		case c == '#':
			j := i + 1
			for j < n && isIdentByte(val[j]) || j < n && val[j] == '-' {
				j++
			}
			b.WriteString(val[i:j])
			i = j
			pending = ""
		case startsNumber(val, i):
			j := scanNumber(val, i)
			k := j
			if k < n && val[k] == '%' {
				k++
			} else {
				for k < n && (isIdentByte(val[k]) || val[k] == '-') {
					k++
				}
			}
			num, unit := val[i:j], strings.ToLower(val[j:k])
			if keep == 0 && zeroStripUnits[unit] && isZeroNumber(num) {
				b.WriteByte('0')
			} else {
				b.WriteString(val[i:k])
			}
			i = k
			pending = ""
		case isIdentByte(c) || c == '-' || c == '\\':
			j := i
			for j < n && (isIdentByte(val[j]) || val[j] == '-' || val[j] == '\\' && j+1 < n) {
				if val[j] == '\\' {
					j++
				}
				j++
			}
			if j == i {
				j = i + 1
			}
			b.WriteString(val[i:j])
			pending = val[i:j]
			i = j
		default:
			b.WriteByte(c)
			i++
			pending = ""
		}
	}
	return b.String()
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

// startsNumber reports whether a numeric token begins at s[i].
func startsNumber(s string, i int) bool {
	c := s[i]
	if c == '+' || c == '-' {
		i++
		if i >= len(s) {
			return false
		}
		c = s[i]
	}
	if isDigit(c) {
		return true
	}
	return c == '.' && i+1 < len(s) && isDigit(s[i+1])
}

// scanNumber returns the end of the numeric part (sign, digits, fraction,
// exponent) of the token starting at s[i].
func scanNumber(s string, i int) int {
	n := len(s)
	if i < n && (s[i] == '+' || s[i] == '-') {
		i++
	}
	for i < n && isDigit(s[i]) {
		i++
	}
	if i+1 < n && s[i] == '.' && isDigit(s[i+1]) {
		i++
		for i < n && isDigit(s[i]) {
			i++
		}
	}
	if i < n && (s[i] == 'e' || s[i] == 'E') {
		j := i + 1
		if j < n && (s[j] == '+' || s[j] == '-') {
			j++
		}
		if j < n && isDigit(s[j]) {
			for j < n && isDigit(s[j]) {
				j++
			}
			i = j
		}
	}
	return i
}

// isZeroNumber reports whether a numeric token has only zero digits and no
// exponent (0, -0, 0.0, .0).
func isZeroNumber(num string) bool {
	seen := false
	for i := 0; i < len(num); i++ {
		switch c := num[i]; {
		case c == '0':
			seen = true
		case c == '.' || ((c == '+' || c == '-') && i == 0):
		default:
			return false
		}
	}
	return seen
}

var calcOperand = `[+-]?(?:\d+\.?\d*|\.\d+)(?:[eE][+-]?\d+)?(?:%|[a-zA-Z]+)?`
var (
	calcMulRe = regexp.MustCompile(`^(` + calcOperand + `)\s*([*/])\s*(` + calcOperand + `)$`)
	calcAddRe = regexp.MustCompile(`^(` + calcOperand + `)\s+([+-])\s+(` + calcOperand + `)$`)
	calcNumRe = regexp.MustCompile(`^([+-]?(?:\d+\.?\d*|\.\d+)(?:[eE][+-]?\d+)?)(.*)$`)
)

// simplifyCalc folds trivial calc() expressions in a declaration value. Only
// a body that is a single binary operation on two plain numbers or dimensions
// is rewritten; anything with var(), functions or more terms is left alone.
func simplifyCalc(val string) string {
	if !strings.Contains(strings.ToLower(val), "calc(") {
		return val
	}
	var b strings.Builder
	n := len(val)
	for i := 0; i < n; {
		c := val[i]
		switch {
		case c == '\\':
			end := min(i+2, n)
			b.WriteString(val[i:end])
			i = end
		case c == 0:
			end := n
			if j := strings.IndexByte(val[i+1:], 0); j >= 0 {
				end = i + 1 + j + 1
			}
			b.WriteString(val[i:end])
			i = end
		case c == '"' || c == '\'':
			end := cssSkipString(val, i)
			b.WriteString(val[i:end])
			i = end
		case (c == 'c' || c == 'C') && hasPrefixFold(val[i:], "calc(") && (i == 0 || !isIdentByte(val[i-1]) && val[i-1] != '-'):
			closeIdx := matchParen(val, i+4)
			if closeIdx < 0 {
				b.WriteString(val[i:])
				return b.String()
			}
			inner := simplifyCalc(val[i+5 : closeIdx])
			if out, ok := simplifyCalcBody(strings.TrimSpace(inner)); ok {
				b.WriteString(out)
			} else {
				b.WriteString(val[i : i+5])
				b.WriteString(inner)
				b.WriteByte(')')
			}
			i = closeIdx + 1
		default:
			b.WriteByte(c)
			i++
		}
	}
	return b.String()
}

func splitCalcOperand(op string) (num float64, unit string) {
	m := calcNumRe.FindStringSubmatch(op)
	if m == nil {
		return 0, op
	}
	f, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		return 0, op
	}
	return f, m[2]
}

// simplifyCalcBody applies identity rules to `A op B` where both operands are
// single numeric tokens. A product with zero keeps the unit of the other
// operand (0px, 0s), never a bare 0, so the result stays type-correct.
func simplifyCalcBody(expr string) (string, bool) {
	var a, op, c string
	if m := calcMulRe.FindStringSubmatch(expr); m != nil {
		a, op, c = m[1], m[2], m[3]
	} else if m := calcAddRe.FindStringSubmatch(expr); m != nil {
		a, op, c = m[1], m[2], m[3]
	} else {
		return "", false
	}
	av, au := splitCalcOperand(a)
	cv, cu := splitCalcOperand(c)
	switch op {
	case "+":
		if av == 0 {
			return c, true
		}
		if cv == 0 {
			return a, true
		}
	case "-":
		if cv == 0 {
			return a, true
		}
		if av == 0 {
			return negateOperand(c), true
		}
	case "*":
		if au == "" && av == 1 {
			return c, true
		}
		if cu == "" && cv == 1 {
			return a, true
		}
		if au == "" && av == 0 {
			return "0" + cu, true
		}
		if cu == "" && cv == 0 {
			return "0" + au, true
		}
	case "/":
		if cu == "" && cv == 1 {
			return a, true
		}
	}
	return "", false
}

func negateOperand(op string) string {
	switch op[0] {
	case '-':
		return op[1:]
	case '+':
		return "-" + op[1:]
	}
	return "-" + op
}
