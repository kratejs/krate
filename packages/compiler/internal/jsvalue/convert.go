package jsvalue

import (
	"math"
	"math/big"
	"strconv"
	"strings"
)

// Truthy implements ToBoolean. known=false for Unknown values.
func (v Value) Truthy() (truthy, known bool) {
	switch v.kind {
	case KindUnknown:
		return false, false
	case KindUndefined, KindNull:
		return false, true
	case KindBool:
		return v.b, true
	case KindNumber:
		return !(v.n == 0 || math.IsNaN(v.n)), true
	case KindString:
		return v.s != "", true
	case KindBigInt:
		return v.s != "0", true
	}
	// Arrays, objects, functions and JSX elements are objects: always truthy.
	return true, true
}

// ToBoolean returns the JS truthiness (Unknown counts as false; check
// IsUnknown first when that matters).
func (v Value) ToBoolean() bool {
	t, _ := v.Truthy()
	return t
}

// ToPrimitive converts objects to primitives (hint "default"/"number" behave
// alike for plain arrays and objects: valueOf returns the object itself, so
// toString is used).
func (v Value) ToPrimitive() Value {
	switch v.kind {
	case KindArray, KindObject, KindJSX, KindFunction:
		return String(v.ToString())
	}
	return v
}

// ToNumber implements the abstract ToNumber operation.
func (v Value) ToNumber() float64 {
	switch v.kind {
	case KindUndefined, KindUnknown:
		return math.NaN()
	case KindNull:
		return 0
	case KindBool:
		if v.b {
			return 1
		}
		return 0
	case KindNumber:
		return v.n
	case KindString:
		return StringToNumber(v.s)
	case KindBigInt:
		return StringToNumber(v.s)
	}
	return v.ToPrimitive().ToNumber()
}

// ToString implements the abstract ToString operation.
func (v Value) ToString() string {
	switch v.kind {
	case KindUndefined:
		return "undefined"
	case KindNull:
		return "null"
	case KindBool:
		if v.b {
			return "true"
		}
		return "false"
	case KindNumber:
		return NumberToString(v.n)
	case KindString, KindBigInt:
		return v.s
	case KindArray:
		return joinArray(v.Elems(), ",")
	case KindFunction:
		return "function " + v.fn.Name + "() { [native code] }"
	case KindObject, KindJSX:
		return "[object Object]"
	}
	return ""
}

func joinArray(elems []Value, sep string) string {
	var b strings.Builder
	for i, e := range elems {
		if i > 0 {
			b.WriteString(sep)
		}
		if e.IsNullish() {
			continue
		}
		b.WriteString(e.ToString())
	}
	return b.String()
}

// TypeOf implements the typeof operator.
func (v Value) TypeOf() string {
	switch v.kind {
	case KindUndefined:
		return "undefined"
	case KindNull, KindArray, KindObject, KindJSX:
		return "object"
	case KindBool:
		return "boolean"
	case KindNumber:
		return "number"
	case KindString:
		return "string"
	case KindFunction:
		return "function"
	case KindBigInt:
		return "bigint"
	}
	return ""
}

// NumberToString formats a float64 exactly like JS Number.prototype.toString()
// (ECMA-262 Number::toString with radix 10).
func NumberToString(f float64) string {
	switch {
	case math.IsNaN(f):
		return "NaN"
	case f == 0:
		return "0" // covers -0
	case math.IsInf(f, 1):
		return "Infinity"
	case math.IsInf(f, -1):
		return "-Infinity"
	}
	if f < 0 {
		return "-" + NumberToString(-f)
	}
	// Shortest round-trip digits in scientific form: d.ddddde[+-]XX.
	sci := strconv.FormatFloat(f, 'e', -1, 64)
	mant, expStr, _ := strings.Cut(sci, "e")
	digits := strings.Replace(mant, ".", "", 1)
	exp, _ := strconv.Atoi(expStr)
	k := len(digits)
	n := exp + 1 // position of the decimal point relative to the digits
	switch {
	case k <= n && n <= 21:
		return digits + strings.Repeat("0", n-k)
	case 0 < n && n <= 21:
		return digits[:n] + "." + digits[n:]
	case -6 < n && n <= 0:
		return "0." + strings.Repeat("0", -n) + digits
	}
	e := n - 1
	sign := "+"
	if e < 0 {
		sign = "-"
		e = -e
	}
	if k == 1 {
		return digits + "e" + sign + strconv.Itoa(e)
	}
	return digits[:1] + "." + digits[1:] + "e" + sign + strconv.Itoa(e)
}

// isJSWhitespace reports the WhiteSpace + LineTerminator code points that
// StringToNumber trims.
func isJSWhitespace(r rune) bool {
	switch r {
	case '\t', '\n', '\v', '\f', '\r', ' ', 0xA0, 0x1680, 0x2028, 0x2029, 0x202F, 0x205F, 0x3000, 0xFEFF:
		return true
	}
	return r >= 0x2000 && r <= 0x200A
}

// StringToNumber implements ToNumber applied to a string (StringNumericLiteral).
func StringToNumber(s string) float64 {
	s = strings.TrimFunc(s, isJSWhitespace)
	if s == "" {
		return 0
	}
	if len(s) > 2 && s[0] == '0' {
		switch s[1] {
		case 'x', 'X':
			return parseRadix(s[2:], 16)
		case 'o', 'O':
			return parseRadix(s[2:], 8)
		case 'b', 'B':
			return parseRadix(s[2:], 2)
		}
	}
	body := s
	sign := 1.0
	if body[0] == '+' || body[0] == '-' {
		if body[0] == '-' {
			sign = -1
		}
		body = body[1:]
	}
	if body == "Infinity" {
		return sign * math.Inf(1)
	}
	if !isStrDecimal(body) {
		return math.NaN()
	}
	f, err := strconv.ParseFloat(body, 64)
	if err != nil {
		// Range errors still carry the correctly rounded value (+-Inf or 0).
		if ne, ok := err.(*strconv.NumError); ok && ne.Err == strconv.ErrRange {
			return sign * f
		}
		return math.NaN()
	}
	return sign * f
}

// isStrDecimal validates StrUnsignedDecimalLiteral without the Infinity form:
// digits [. digits] [e[+-]digits] or . digits [exponent].
func isStrDecimal(s string) bool {
	i, n := 0, len(s)
	intDigits := 0
	for i < n && s[i] >= '0' && s[i] <= '9' {
		i++
		intDigits++
	}
	fracDigits := 0
	if i < n && s[i] == '.' {
		i++
		for i < n && s[i] >= '0' && s[i] <= '9' {
			i++
			fracDigits++
		}
	}
	if intDigits == 0 && fracDigits == 0 {
		return false
	}
	if i < n && (s[i] == 'e' || s[i] == 'E') {
		i++
		if i < n && (s[i] == '+' || s[i] == '-') {
			i++
		}
		expDigits := 0
		for i < n && s[i] >= '0' && s[i] <= '9' {
			i++
			expDigits++
		}
		if expDigits == 0 {
			return false
		}
	}
	return i == n
}

func parseRadix(digits string, base int) float64 {
	if digits == "" {
		return math.NaN()
	}
	z, ok := new(big.Int).SetString(digits, base)
	if !ok || z.Sign() < 0 {
		return math.NaN()
	}
	f, _ := new(big.Float).SetInt(z).Float64()
	return f
}

// ParseNumberLiteral parses JS numeric literal source text (as written in
// code): decimal, hex/octal/binary prefixes, legacy octal and numeric
// separators. ok=false for BigInt literals or malformed input.
func ParseNumberLiteral(src string) (float64, bool) {
	s := strings.ReplaceAll(src, "_", "")
	if s == "" || strings.HasSuffix(s, "n") {
		return 0, false
	}
	if len(s) > 2 && s[0] == '0' {
		switch s[1] {
		case 'x', 'X', 'o', 'O', 'b', 'B':
			f := StringToNumber(s)
			return f, !math.IsNaN(f)
		}
	}
	// Legacy octal (`017`) when every digit is octal.
	if len(s) > 1 && s[0] == '0' && s[1] >= '0' && s[1] <= '9' {
		octal := true
		for i := 1; i < len(s); i++ {
			if s[i] < '0' || s[i] > '7' {
				octal = false
				break
			}
		}
		if octal {
			return parseRadix(s[1:], 8), true
		}
	}
	if !isStrDecimal(s) {
		return 0, false
	}
	f := StringToNumber(s)
	return f, !math.IsNaN(f)
}

// ToInt32 implements the abstract ToInt32 operation.
func ToInt32(f float64) int32 {
	return int32(ToUint32(f))
}

// ToUint32 implements the abstract ToUint32 operation.
func ToUint32(f float64) uint32 {
	if math.IsNaN(f) || math.IsInf(f, 0) || f == 0 {
		return 0
	}
	t := math.Trunc(f)
	m := math.Mod(t, 4294967296)
	if m < 0 {
		m += 4294967296
	}
	return uint32(m)
}

// ToIntegerOrInfinity implements the abstract operation of the same name.
func ToIntegerOrInfinity(f float64) float64 {
	if math.IsNaN(f) || f == 0 {
		return 0
	}
	if math.IsInf(f, 0) {
		return f
	}
	return math.Trunc(f)
}

// parseBigIntLiteral parses BigInt literal source (`123n`, `0xFFn`) to its
// decimal digits.
func parseBigIntLiteral(src string) (string, bool) {
	s := strings.ReplaceAll(src, "_", "")
	if !strings.HasSuffix(s, "n") || len(s) < 2 {
		return "", false
	}
	s = s[:len(s)-1]
	base := 10
	if len(s) > 2 && s[0] == '0' {
		switch s[1] {
		case 'x', 'X':
			base, s = 16, s[2:]
		case 'o', 'O':
			base, s = 8, s[2:]
		case 'b', 'B':
			base, s = 2, s[2:]
		}
	}
	z, ok := new(big.Int).SetString(s, base)
	if !ok || z.Sign() < 0 {
		return "", false
	}
	return z.String(), true
}
