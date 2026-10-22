package jsvalue

import (
	"math"
	"unicode/utf16"
)

// StrictEquals implements ===.
func StrictEquals(a, b Value) bool {
	if a.kind != b.kind {
		return false
	}
	switch a.kind {
	case KindUndefined, KindNull:
		return true
	case KindBool:
		return a.b == b.b
	case KindNumber:
		return a.n == b.n // NaN != NaN, +0 == -0
	case KindString, KindBigInt:
		return a.s == b.s
	case KindArray, KindObject, KindFunction:
		return a.same(b)
	}
	// JSX elements are fresh objects per evaluation; Unknown is never equal.
	return false
}

// SameValueZero is the comparison used by Array.prototype.includes.
func SameValueZero(a, b Value) bool {
	if a.kind == KindNumber && b.kind == KindNumber && math.IsNaN(a.n) && math.IsNaN(b.n) {
		return true
	}
	return StrictEquals(a, b)
}

// LooseEquals implements == (IsLooselyEqual).
func LooseEquals(a, b Value) bool {
	if a.kind == b.kind {
		return StrictEquals(a, b)
	}
	if a.IsNullish() && b.IsNullish() {
		return true
	}
	if a.IsNullish() || b.IsNullish() {
		return false
	}
	switch {
	case a.kind == KindNumber && b.kind == KindString:
		return a.n == StringToNumber(b.s)
	case a.kind == KindString && b.kind == KindNumber:
		return StringToNumber(a.s) == b.n
	case a.kind == KindBool:
		return LooseEquals(Number(a.ToNumber()), b)
	case b.kind == KindBool:
		return LooseEquals(a, Number(b.ToNumber()))
	case isObjectKind(a.kind) && !isObjectKind(b.kind):
		return LooseEquals(a.ToPrimitive(), b)
	case !isObjectKind(a.kind) && isObjectKind(b.kind):
		return LooseEquals(a, b.ToPrimitive())
	}
	return false
}

func isObjectKind(k Kind) bool {
	return k == KindArray || k == KindObject || k == KindFunction || k == KindJSX
}

// Add implements the + operator.
func Add(a, b Value) Value {
	pa, pb := a.ToPrimitive(), b.ToPrimitive()
	if pa.kind == KindString || pb.kind == KindString {
		return String(pa.ToString() + pb.ToString())
	}
	return Number(pa.ToNumber() + pb.ToNumber())
}

// Pow implements ** (which differs from math.Pow for NaN and +-1 bases).
func Pow(x, y float64) float64 {
	if math.IsNaN(y) {
		return math.NaN()
	}
	if y == 0 {
		return 1
	}
	if (x == 1 || x == -1) && math.IsInf(y, 0) {
		return math.NaN()
	}
	return math.Pow(x, y)
}

// compareStrings orders strings by UTF-16 code units, as JS does.
func compareStrings(a, b string) int {
	ua, ub := utf16.Encode([]rune(a)), utf16.Encode([]rune(b))
	for i := 0; i < len(ua) && i < len(ub); i++ {
		if ua[i] != ub[i] {
			if ua[i] < ub[i] {
				return -1
			}
			return 1
		}
	}
	switch {
	case len(ua) < len(ub):
		return -1
	case len(ua) > len(ub):
		return 1
	}
	return 0
}

// lessThan implements IsLessThan; known=false means the result is undefined
// (a NaN operand), which every relational operator treats as false.
func lessThan(a, b Value) (less, known bool) {
	pa, pb := a.ToPrimitive(), b.ToPrimitive()
	if pa.kind == KindString && pb.kind == KindString {
		return compareStrings(pa.s, pb.s) < 0, true
	}
	x, y := pa.ToNumber(), pb.ToNumber()
	if math.IsNaN(x) || math.IsNaN(y) {
		return false, false
	}
	return x < y, true
}

// BinaryOp applies a non-short-circuit binary operator. Unknown operands yield
// Unknown; unsupported operators yield Unsupported.
func BinaryOp(op string, a, b Value) Value {
	if a.kind == KindUnknown {
		return a
	}
	if b.kind == KindUnknown {
		return b
	}
	if (a.kind == KindBigInt || b.kind == KindBigInt) && op != "===" && op != "!==" {
		return Unsupported("BigInt operator " + op)
	}
	switch op {
	case "+":
		return Add(a, b)
	case "-":
		return Number(a.ToNumber() - b.ToNumber())
	case "*":
		return Number(a.ToNumber() * b.ToNumber())
	case "/":
		return Number(a.ToNumber() / b.ToNumber())
	case "%":
		return Number(math.Mod(a.ToNumber(), b.ToNumber()))
	case "**":
		return Number(Pow(a.ToNumber(), b.ToNumber()))
	case "==":
		return Bool(LooseEquals(a, b))
	case "!=":
		return Bool(!LooseEquals(a, b))
	case "===":
		return Bool(StrictEquals(a, b))
	case "!==":
		return Bool(!StrictEquals(a, b))
	case "<":
		r, _ := lessThan(a, b)
		return Bool(r)
	case ">":
		r, _ := lessThan(b, a)
		return Bool(r)
	case "<=":
		r, known := lessThan(b, a)
		return Bool(known && !r)
	case ">=":
		r, known := lessThan(a, b)
		return Bool(known && !r)
	case "&":
		return Number(float64(ToInt32(a.ToNumber()) & ToInt32(b.ToNumber())))
	case "|":
		return Number(float64(ToInt32(a.ToNumber()) | ToInt32(b.ToNumber())))
	case "^":
		return Number(float64(ToInt32(a.ToNumber()) ^ ToInt32(b.ToNumber())))
	case "<<":
		return Number(float64(ToInt32(a.ToNumber()) << (ToUint32(b.ToNumber()) & 31)))
	case ">>":
		return Number(float64(ToInt32(a.ToNumber()) >> (ToUint32(b.ToNumber()) & 31)))
	case ">>>":
		return Number(float64(ToUint32(a.ToNumber()) >> (ToUint32(b.ToNumber()) & 31)))
	case "in":
		if b.kind == KindObject {
			_, ok := b.Get(a.ToString())
			return Bool(ok)
		}
		if b.kind == KindArray {
			idx := a.ToString()
			if idx == "length" {
				return Bool(true)
			}
			n := StringToNumber(idx)
			return Bool(isArrayIndex(idx) && int(n) < len(b.Elems()))
		}
	}
	return Unsupported("binary operator " + op)
}

// UnaryOp applies a unary operator (excluding ++/--/delete).
func UnaryOp(op string, a Value) Value {
	if op == "typeof" {
		if a.kind == KindUnknown {
			return a
		}
		return String(a.TypeOf())
	}
	if a.kind == KindUnknown {
		return a
	}
	if a.kind == KindBigInt && op != "!" {
		return Unsupported("BigInt operator " + op)
	}
	switch op {
	case "!":
		return Bool(!a.ToBoolean())
	case "-":
		return Number(-a.ToNumber())
	case "+":
		return Number(a.ToNumber())
	case "~":
		return Number(float64(^ToInt32(a.ToNumber())))
	case "void":
		return Undefined()
	}
	return Unsupported("unary operator " + op)
}
