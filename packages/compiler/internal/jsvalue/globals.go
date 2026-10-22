package jsvalue

import (
	"math"
	"strconv"
	"strings"
)

// globalObject returns the built-in global with the given name. Only pure,
// deterministic built-ins are provided; everything else (Date, Intl, Math.random,
// ...) is left to the host (see Hooks) or reported as unresolved.
func globalObject(name string) (Value, bool) {
	switch name {
	case "Math":
		return mathObject(), true
	case "Object":
		return objectCtor(), true
	case "Array":
		return arrayCtor(), true
	case "JSON":
		o := NewObject()
		o.Set("stringify", Func("stringify", func(args []Value) Value {
			v := arg(args, 0)
			if v.ContainsUnknown() {
				return v.firstUnknown()
			}
			if len(args) > 1 && !args[1].IsNullish() {
				return Unsupported("JSON.stringify with a replacer or indentation")
			}
			s, ok := JSONStringify(v)
			if !ok {
				if v.kind == KindUndefined || v.kind == KindFunction {
					return Undefined()
				}
				return Unsupported("JSON.stringify of a value with no build-time JSON form")
			}
			return String(s)
		}))
		return o, true
	case "String":
		return Func("String", func(args []Value) Value {
			if len(args) == 0 {
				return String("")
			}
			if args[0].IsUnknown() {
				return args[0]
			}
			return String(args[0].ToPrimitive().ToString())
		}), true
	case "Number":
		return Func("Number", func(args []Value) Value {
			if len(args) == 0 {
				return Number(0)
			}
			if args[0].IsUnknown() {
				return args[0]
			}
			return Number(args[0].ToNumber())
		}), true
	case "Boolean":
		return Func("Boolean", func(args []Value) Value {
			if args := args; len(args) > 0 && args[0].IsUnknown() {
				return args[0]
			}
			return Bool(arg(args, 0).ToBoolean())
		}), true
	case "parseInt":
		return Func("parseInt", func(args []Value) Value {
			if a := arg(args, 0); a.IsUnknown() {
				return a
			}
			return Number(parseInt(arg(args, 0).ToString(), arg(args, 1)))
		}), true
	case "parseFloat":
		return Func("parseFloat", func(args []Value) Value {
			if a := arg(args, 0); a.IsUnknown() {
				return a
			}
			return Number(parseFloat(arg(args, 0).ToString()))
		}), true
	case "isNaN":
		return Func("isNaN", func(args []Value) Value {
			if a := arg(args, 0); a.IsUnknown() {
				return a
			}
			return Bool(math.IsNaN(arg(args, 0).ToNumber()))
		}), true
	case "isFinite":
		return Func("isFinite", func(args []Value) Value {
			if a := arg(args, 0); a.IsUnknown() {
				return a
			}
			f := arg(args, 0).ToNumber()
			return Bool(!math.IsNaN(f) && !math.IsInf(f, 0))
		}), true
	}
	return Value{}, false
}

func numericFn(name string, f func(xs []float64) float64) Value {
	return Func(name, func(args []Value) Value {
		xs := make([]float64, len(args))
		for i, a := range args {
			if a.IsUnknown() {
				return a
			}
			xs[i] = a.ToNumber()
		}
		return Number(f(xs))
	})
}

func first(xs []float64) float64 {
	if len(xs) == 0 {
		return math.NaN()
	}
	return xs[0]
}

func mathObject() Value {
	o := NewObject()
	o.Set("PI", Number(math.Pi))
	o.Set("E", Number(math.E))
	unary := map[string]func(float64) float64{
		"abs": math.Abs, "floor": math.Floor, "ceil": math.Ceil, "trunc": math.Trunc,
		"sqrt": math.Sqrt, "cbrt": math.Cbrt, "log": math.Log, "log10": math.Log10, "log2": math.Log2,
		"exp": math.Exp, "sin": math.Sin, "cos": math.Cos, "tan": math.Tan,
		"round": func(x float64) float64 {
			// JS rounds half toward +Infinity.
			if math.IsNaN(x) || math.IsInf(x, 0) || x == 0 {
				return x
			}
			r := math.Floor(x + 0.5)
			if r == 0 && x < 0 {
				return math.Copysign(0, -1)
			}
			return r
		},
		"sign": func(x float64) float64 {
			switch {
			case math.IsNaN(x) || x == 0:
				return x
			case x > 0:
				return 1
			}
			return -1
		},
	}
	for name, fn := range unary {
		fn := fn
		o.Set(name, numericFn(name, func(xs []float64) float64 { return fn(first(xs)) }))
	}
	o.Set("max", numericFn("max", func(xs []float64) float64 {
		r := math.Inf(-1)
		for _, x := range xs {
			if math.IsNaN(x) {
				return x
			}
			if x > r || (x == 0 && r == 0 && !math.Signbit(x)) {
				r = x
			}
		}
		return r
	}))
	o.Set("min", numericFn("min", func(xs []float64) float64 {
		r := math.Inf(1)
		for _, x := range xs {
			if math.IsNaN(x) {
				return x
			}
			if x < r || (x == 0 && r == 0 && math.Signbit(x)) {
				r = x
			}
		}
		return r
	}))
	o.Set("pow", numericFn("pow", func(xs []float64) float64 {
		if len(xs) < 2 {
			return math.NaN()
		}
		return Pow(xs[0], xs[1])
	}))
	return o
}

func objectCtor() Value {
	o := NewObject()
	entriesOf := func(v Value) ([]string, []Value, bool) {
		switch v.kind {
		case KindObject:
			keys := v.Keys()
			vals := make([]Value, len(keys))
			for i, k := range keys {
				vals[i], _ = v.Get(k)
			}
			return keys, vals, true
		case KindArray:
			keys := make([]string, len(v.Elems()))
			for i := range keys {
				keys[i] = strconv.Itoa(i)
			}
			return keys, v.Elems(), true
		case KindString:
			var keys []string
			var vals []Value
			for i, c := range units(v.s) {
				keys = append(keys, strconv.Itoa(i))
				vals = append(vals, String(fromUnits([]uint16{c})))
			}
			return keys, vals, true
		case KindNumber, KindBool:
			return nil, nil, true
		}
		return nil, nil, false
	}
	method := func(name string, f func(keys []string, vals []Value) Value) {
		o.Set(name, Func(name, func(args []Value) Value {
			v := arg(args, 0)
			if v.IsUnknown() {
				return v
			}
			keys, vals, ok := entriesOf(v)
			if !ok {
				return Unsupported("Object." + name + " of " + v.Kind().String())
			}
			return f(keys, vals)
		}))
	}
	method("keys", func(keys []string, _ []Value) Value {
		out := make([]Value, len(keys))
		for i, k := range keys {
			out[i] = String(k)
		}
		return NewArray(out...)
	})
	method("values", func(_ []string, vals []Value) Value {
		return ArrayOf(vals)
	})
	method("entries", func(keys []string, vals []Value) Value {
		out := make([]Value, len(keys))
		for i, k := range keys {
			out[i] = NewArray(String(k), vals[i])
		}
		return NewArray(out...)
	})
	o.Set("fromEntries", Func("fromEntries", func(args []Value) Value {
		src := arg(args, 0)
		if src.IsUnknown() {
			return src
		}
		if !src.IsArray() {
			return Unsupported("Object.fromEntries of a non-array")
		}
		out := NewObject()
		for _, e := range src.Elems() {
			if !e.IsArray() {
				return unknownOr(e, "Object.fromEntries entry is not an array")
			}
			out.Set(arg(e.Elems(), 0).ToString(), arg(e.Elems(), 1))
		}
		return out
	}))
	o.Set("assign", Func("assign", func(args []Value) Value {
		if len(args) == 0 || !args[0].IsObject() {
			return unknownOr(arg(args, 0), "Object.assign target is not an object")
		}
		for _, src := range args[1:] {
			if src.IsUnknown() {
				return src
			}
			for _, k := range src.Keys() {
				v, _ := src.Get(k)
				args[0].Set(k, v)
			}
		}
		return args[0]
	}))
	return o
}

func arrayCtor() Value {
	o := NewObject()
	o.Set("isArray", Func("isArray", func(args []Value) Value {
		if a := arg(args, 0); a.IsUnknown() {
			return a
		}
		return Bool(arg(args, 0).IsArray())
	}))
	o.Set("from", Func("from", func(args []Value) Value {
		src := arg(args, 0)
		if src.IsUnknown() {
			return src
		}
		var items []Value
		if it, ok := iterate(src); ok {
			items = append([]Value(nil), it...)
		} else if src.IsObject() {
			// Array-like `{ length: n }`.
			l, _ := src.Get("length")
			n := int(ToIntegerOrInfinity(l.ToNumber()))
			if n < 0 || n > maxLoopIterations {
				return Unsupported("Array.from length out of range")
			}
			for i := 0; i < n; i++ {
				v, _ := src.Get(strconv.Itoa(i))
				items = append(items, v)
			}
		} else {
			return Unsupported("Array.from of " + src.Kind().String())
		}
		if fn := arg(args, 1); fn.IsFunction() {
			for i, el := range items {
				items[i] = fn.Call(el, Number(float64(i)))
			}
		}
		return NewArray(items...)
	}))
	o.Set("of", Func("of", func(args []Value) Value { return ArrayOf(args) }))
	return o
}

func parseInt(s string, radixV Value) float64 {
	s = strings.TrimLeftFunc(s, isJSWhitespace)
	sign := 1.0
	if s != "" && (s[0] == '+' || s[0] == '-') {
		if s[0] == '-' {
			sign = -1
		}
		s = s[1:]
	}
	radix := int(ToInt32(radixV.ToNumber()))
	stripPrefix := true
	if radix != 0 {
		if radix < 2 || radix > 36 {
			return math.NaN()
		}
		if radix != 16 {
			stripPrefix = false
		}
	} else {
		radix = 10
	}
	if stripPrefix && len(s) >= 2 && s[0] == '0' && (s[1] == 'x' || s[1] == 'X') {
		s = s[2:]
		radix = 16
	}
	end := 0
	for end < len(s) {
		c := s[end]
		var d int
		switch {
		case c >= '0' && c <= '9':
			d = int(c - '0')
		case c >= 'a' && c <= 'z':
			d = int(c-'a') + 10
		case c >= 'A' && c <= 'Z':
			d = int(c-'A') + 10
		default:
			d = 99
		}
		if d >= radix {
			break
		}
		end++
	}
	if end == 0 {
		return math.NaN()
	}
	if radix == 10 {
		f, _ := strconv.ParseFloat(s[:end], 64)
		return sign * f
	}
	return sign * parseRadix(strings.ToLower(s[:end]), radix)
}

func parseFloat(s string) float64 {
	s = strings.TrimLeftFunc(s, isJSWhitespace)
	body := s
	sign := 1.0
	if body != "" && (body[0] == '+' || body[0] == '-') {
		if body[0] == '-' {
			sign = -1
		}
		body = body[1:]
	}
	if strings.HasPrefix(body, "Infinity") {
		return sign * math.Inf(1)
	}
	// Longest prefix that is a StrDecimalLiteral.
	for end := len(body); end > 0; end-- {
		if isStrDecimal(body[:end]) {
			f, _ := strconv.ParseFloat(body[:end], 64)
			return sign * f
		}
	}
	return math.NaN()
}
