package jsvalue

import (
	"math"
	"math/big"
	"sort"
	"strconv"
	"strings"
	"unicode/utf16"

	"github.com/kratejs/krate/packages/compiler/ast"
)

// member evaluates a property read, honoring optional chaining.
func (in *Interp) member(e *ast.MemberExpr) (Value, bool) {
	if in.Hooks.Member != nil {
		if v, ok := in.Hooks.Member(e); ok {
			return v, false
		}
	}
	obj, short := in.eval(e.Object)
	if short {
		return Undefined(), true
	}
	if obj.IsUnknown() {
		return obj, false
	}
	if e.Optional && obj.IsNullish() {
		return Undefined(), true
	}
	key, ok := in.propertyKey(e)
	if !ok {
		return in.Eval(e.Property), false
	}
	if obj.IsNullish() {
		return Unsupported("TypeError: cannot read properties of " + obj.ToString() + " (reading '" + key + "') in " + calleeName(e)), false
	}
	return GetProperty(obj, key), false
}

// propertyKey returns the property name of a member expression. ok=false when
// a computed key is not statically known (the caller re-evaluates the key to
// surface its Unknown value).
func (in *Interp) propertyKey(e *ast.MemberExpr) (string, bool) {
	if !e.Computed {
		if id, ok := e.Property.(*ast.Identifier); ok {
			return id.Name, true
		}
	}
	k := in.Eval(e.Property)
	if k.IsUnknown() {
		return "", false
	}
	return k.ToPrimitive().ToString(), true
}

// call evaluates a call expression.
func (in *Interp) call(e *ast.CallExpr) (Value, bool) {
	if in.Hooks.Call != nil {
		if v, ok := in.Hooks.Call(e); ok {
			return v, false
		}
	}
	var fn Value
	if mem, ok := e.Callee.(*ast.MemberExpr); ok {
		recv, short := in.eval(mem.Object)
		if short {
			return Undefined(), true
		}
		if recv.IsUnknown() {
			return recv, false
		}
		if mem.Optional && recv.IsNullish() {
			return Undefined(), true
		}
		key, ok := in.propertyKey(mem)
		if !ok {
			return in.Eval(mem.Property), false
		}
		if recv.IsNullish() {
			return Unsupported("TypeError: cannot read properties of " + recv.ToString() + " (reading '" + key + "') in " + calleeName(mem)), false
		}
		fn = GetProperty(recv, key)
	} else {
		var short bool
		fn, short = in.eval(e.Callee)
		if short {
			return Undefined(), true
		}
	}
	if fn.IsUnknown() {
		return fn, false
	}
	if e.Optional && fn.IsNullish() {
		return Undefined(), true
	}
	if !fn.IsFunction() {
		return Unsupported(calleeName(e.Callee) + " is not a function"), false
	}
	args, bad := in.Args(e.Args)
	if bad.IsUnknown() {
		return bad, false
	}
	return fn.Call(args...), false
}

// Args evaluates call arguments, expanding spreads. A non-iterable spread
// returns the offending value as the second result.
func (in *Interp) Args(exprs []ast.Expr) ([]Value, Value) {
	args := make([]Value, 0, len(exprs))
	for _, a := range exprs {
		if u, ok := a.(*ast.UnaryExpr); ok && u.Op == "..." {
			src := in.Eval(u.Arg)
			items, ok := iterate(src)
			if !ok {
				return nil, unknownOr(src, "spread of a non-iterable value")
			}
			args = append(args, items...)
			continue
		}
		args = append(args, in.Eval(a))
	}
	return args, Value{kind: KindUndefined}
}

// GetProperty reads a property of a non-nullish value (built-in methods are
// returned as bound functions).
func GetProperty(v Value, key string) Value {
	switch v.kind {
	case KindUnknown:
		return v
	case KindArray:
		elems := v.Elems()
		if key == "length" {
			return Number(float64(len(elems)))
		}
		if isArrayIndex(key) {
			i, _ := strconv.Atoi(key)
			if i < len(elems) {
				return elems[i]
			}
			return Undefined()
		}
		if arrayMethods[key] {
			return boundMethod(v, key)
		}
	case KindString:
		units := utf16.Encode([]rune(v.s))
		if key == "length" {
			return Number(float64(len(units)))
		}
		if isArrayIndex(key) {
			i, _ := strconv.Atoi(key)
			if i < len(units) {
				return String(string(utf16.Decode(units[i : i+1])))
			}
			return Undefined()
		}
		if stringMethods[key] {
			return boundMethod(v, key)
		}
	case KindNumber:
		if numberMethods[key] {
			return boundMethod(v, key)
		}
	case KindBool:
		if key == "toString" || key == "valueOf" {
			return boundMethod(v, key)
		}
	case KindObject:
		if p, ok := v.Get(key); ok {
			return p
		}
		if objectMethods[key] {
			return boundMethod(v, key)
		}
	case KindFunction:
		if key == "name" {
			return String(v.fn.Name)
		}
	case KindJSX:
		return Dynamic("property " + key + " of a JSX element")
	}
	return Undefined()
}

var arrayMethods = map[string]bool{
	"map": true, "filter": true, "find": true, "findIndex": true, "findLast": true, "findLastIndex": true,
	"some": true, "every": true, "includes": true, "indexOf": true, "lastIndexOf": true, "join": true,
	"slice": true, "concat": true, "reverse": true, "toReversed": true, "forEach": true, "reduce": true,
	"flat": true, "flatMap": true, "at": true, "push": true, "sort": true, "toSorted": true,
	"toString": true, "keys": true, "entries": true, "values": true, "fill": true,
}

var stringMethods = map[string]bool{
	"toUpperCase": true, "toLowerCase": true, "toLocaleUpperCase": true, "toLocaleLowerCase": true,
	"trim": true, "trimStart": true, "trimEnd": true, "split": true, "slice": true, "substring": true,
	"startsWith": true, "endsWith": true, "includes": true, "indexOf": true, "lastIndexOf": true,
	"replace": true, "replaceAll": true, "padStart": true, "padEnd": true, "repeat": true,
	"charAt": true, "at": true, "concat": true, "toString": true, "valueOf": true, "charCodeAt": true,
}

var numberMethods = map[string]bool{"toFixed": true, "toString": true, "valueOf": true}

var objectMethods = map[string]bool{"hasOwnProperty": true, "toString": true}

func boundMethod(recv Value, name string) Value {
	return Func(name, func(args []Value) Value {
		for _, a := range args {
			if a.IsUnknown() {
				return a
			}
		}
		switch recv.kind {
		case KindArray:
			return arrayMethod(recv, name, args)
		case KindString:
			return stringMethod(recv.s, name, args)
		case KindNumber:
			return numberMethod(recv.n, name, args)
		case KindBool:
			return String(recv.ToString())
		case KindObject:
			switch name {
			case "hasOwnProperty":
				_, ok := recv.Get(arg(args, 0).ToString())
				return Bool(ok)
			case "toString":
				return String("[object Object]")
			}
		}
		return Unsupported("method " + name)
	})
}

func arg(args []Value, i int) Value {
	if i < len(args) {
		return args[i]
	}
	return Undefined()
}

// relIndex resolves a relative start/end argument against length n.
func relIndex(v Value, n int, def int) int {
	if v.kind == KindUndefined {
		return def
	}
	f := ToIntegerOrInfinity(v.ToNumber())
	if f < 0 {
		f += float64(n)
		if f < 0 {
			f = 0
		}
	}
	if f > float64(n) {
		f = float64(n)
	}
	return int(f)
}

// callback invokes fn(elem, index, array).
func callback(fn Value, el Value, i int, arr Value) Value {
	return fn.Call(el, Number(float64(i)), arr)
}

func arrayMethod(recv Value, name string, args []Value) Value {
	elems := recv.Elems()
	n := len(elems)
	needFn := func() (Value, bool) {
		fn := arg(args, 0)
		return fn, fn.IsFunction()
	}
	switch name {
	case "map", "flatMap":
		fn, ok := needFn()
		if !ok {
			return Unsupported("Array.prototype." + name + " callback is not a function")
		}
		out := make([]Value, 0, n)
		for i, el := range elems {
			r := callback(fn, el, i, recv)
			if r.IsUnsupported() {
				return r
			}
			if name == "flatMap" && r.IsArray() {
				out = append(out, r.Elems()...)
				continue
			}
			out = append(out, r)
		}
		return NewArray(out...)
	case "filter":
		fn, ok := needFn()
		if !ok {
			return Unsupported("Array.prototype.filter callback is not a function")
		}
		var out []Value
		for i, el := range elems {
			r := callback(fn, el, i, recv)
			t, known := r.Truthy()
			if !known {
				return r
			}
			if t {
				out = append(out, el)
			}
		}
		return NewArray(out...)
	case "find", "findIndex", "findLast", "findLastIndex", "some", "every":
		fn, ok := needFn()
		if !ok {
			return Unsupported("Array.prototype." + name + " callback is not a function")
		}
		order := make([]int, n)
		for i := range order {
			order[i] = i
			if name == "findLast" || name == "findLastIndex" {
				order[i] = n - 1 - i
			}
		}
		for _, i := range order {
			r := callback(fn, elems[i], i, recv)
			t, known := r.Truthy()
			if !known {
				return r
			}
			switch name {
			case "some":
				if t {
					return Bool(true)
				}
			case "every":
				if !t {
					return Bool(false)
				}
			case "find", "findLast":
				if t {
					return elems[i]
				}
			default:
				if t {
					return Number(float64(i))
				}
			}
		}
		switch name {
		case "some":
			return Bool(false)
		case "every":
			return Bool(true)
		case "find", "findLast":
			return Undefined()
		}
		return Number(-1)
	case "forEach":
		fn, ok := needFn()
		if !ok {
			return Unsupported("Array.prototype.forEach callback is not a function")
		}
		for i, el := range elems {
			if r := callback(fn, el, i, recv); r.IsUnknown() {
				return r
			}
		}
		return Undefined()
	case "reduce":
		fn, ok := needFn()
		if !ok {
			return Unsupported("Array.prototype.reduce callback is not a function")
		}
		start := 0
		var acc Value
		if len(args) >= 2 {
			acc = args[1]
		} else {
			if n == 0 {
				return Unsupported("TypeError: reduce of empty array with no initial value")
			}
			acc = elems[0]
			start = 1
		}
		for i := start; i < n; i++ {
			acc = fn.Call(acc, elems[i], Number(float64(i)), recv)
			if acc.IsUnknown() {
				return acc
			}
		}
		return acc
	case "includes":
		target := arg(args, 0)
		for _, el := range elems[relIndex(arg(args, 1), n, 0):] {
			if el.IsUnknown() {
				return el
			}
			if SameValueZero(el, target) {
				return Bool(true)
			}
		}
		return Bool(false)
	case "indexOf":
		target := arg(args, 0)
		for i := relIndex(arg(args, 1), n, 0); i < n; i++ {
			if elems[i].IsUnknown() {
				return elems[i]
			}
			if StrictEquals(elems[i], target) {
				return Number(float64(i))
			}
		}
		return Number(-1)
	case "lastIndexOf":
		target := arg(args, 0)
		for i := n - 1; i >= 0; i-- {
			if elems[i].IsUnknown() {
				return elems[i]
			}
			if StrictEquals(elems[i], target) {
				return Number(float64(i))
			}
		}
		return Number(-1)
	case "join", "toString":
		sep := ","
		if name == "join" && len(args) > 0 && args[0].kind != KindUndefined {
			sep = args[0].ToString()
		}
		for _, el := range elems {
			if el.ContainsUnknown() {
				return el.firstUnknown()
			}
		}
		return String(joinArray(elems, sep))
	case "slice":
		s := relIndex(arg(args, 0), n, 0)
		e := relIndex(arg(args, 1), n, n)
		if e < s {
			e = s
		}
		return ArrayOf(elems[s:e])
	case "concat":
		out := append([]Value(nil), elems...)
		for _, a := range args {
			if a.IsArray() {
				out = append(out, a.Elems()...)
			} else {
				out = append(out, a)
			}
		}
		return NewArray(out...)
	case "reverse", "toReversed":
		// Evaluated on a copy: build-time values may be shared between
		// component instances, so in-place mutation would leak across them.
		out := make([]Value, n)
		for i, el := range elems {
			out[n-1-i] = el
		}
		return NewArray(out...)
	case "flat":
		depth := 1.0
		if len(args) > 0 && args[0].kind != KindUndefined {
			depth = ToIntegerOrInfinity(args[0].ToNumber())
		}
		return NewArray(flatten(elems, depth)...)
	case "at":
		i := int(ToIntegerOrInfinity(arg(args, 0).ToNumber()))
		if i < 0 {
			i += n
		}
		if i < 0 || i >= n {
			return Undefined()
		}
		return elems[i]
	case "push":
		recv.Push(args...)
		return Number(float64(len(recv.Elems())))
	case "sort", "toSorted":
		out := append([]Value(nil), elems...)
		cmp := arg(args, 0)
		var failed Value
		sort.SliceStable(out, func(i, j int) bool {
			if failed.IsUnknown() {
				return false
			}
			a, b := out[i], out[j]
			if a.kind == KindUndefined || b.kind == KindUndefined {
				return b.kind == KindUndefined && a.kind != KindUndefined
			}
			if cmp.IsFunction() {
				r := cmp.Call(a, b)
				if r.IsUnknown() {
					failed = r
					return false
				}
				return r.ToNumber() < 0
			}
			return compareStrings(a.ToString(), b.ToString()) < 0
		})
		if failed.IsUnknown() {
			return failed
		}
		return NewArray(out...)
	case "keys", "entries", "values":
		out := make([]Value, n)
		for i, el := range elems {
			switch name {
			case "keys":
				out[i] = Number(float64(i))
			case "values":
				out[i] = el
			default:
				out[i] = NewArray(Number(float64(i)), el)
			}
		}
		return NewArray(out...)
	case "fill":
		out := append([]Value(nil), elems...)
		s := relIndex(arg(args, 1), n, 0)
		e := relIndex(arg(args, 2), n, n)
		for i := s; i < e; i++ {
			out[i] = arg(args, 0)
		}
		recv.arr.Elems = out
		return recv
	}
	return Unsupported("Array.prototype." + name)
}

func flatten(elems []Value, depth float64) []Value {
	var out []Value
	for _, el := range elems {
		if el.IsArray() && depth >= 1 {
			out = append(out, flatten(el.Elems(), depth-1)...)
			continue
		}
		out = append(out, el)
	}
	return out
}

func units(s string) []uint16 { return utf16.Encode([]rune(s)) }

func fromUnits(u []uint16) string { return string(utf16.Decode(u)) }

func indexUnits(hay, needle []uint16, from int) int {
	if from < 0 {
		from = 0
	}
	for i := from; i+len(needle) <= len(hay); i++ {
		match := true
		for j := range needle {
			if hay[i+j] != needle[j] {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}

func lastIndexUnits(hay, needle []uint16) int {
	for i := len(hay) - len(needle); i >= 0; i-- {
		if indexUnits(hay[i:i+len(needle)], needle, 0) == 0 {
			return i
		}
	}
	return -1
}

func stringMethod(s, name string, args []Value) Value {
	u := units(s)
	n := len(u)
	switch name {
	case "toUpperCase", "toLocaleUpperCase":
		return String(strings.ToUpper(s))
	case "toLowerCase", "toLocaleLowerCase":
		return String(strings.ToLower(s))
	case "trim":
		return String(strings.TrimFunc(s, isJSWhitespace))
	case "trimStart":
		return String(strings.TrimLeftFunc(s, isJSWhitespace))
	case "trimEnd":
		return String(strings.TrimRightFunc(s, isJSWhitespace))
	case "toString", "valueOf":
		return String(s)
	case "split":
		sepV := arg(args, 0)
		limit := math.MaxInt32
		if l := arg(args, 1); l.kind != KindUndefined {
			limit = int(ToUint32(l.ToNumber()))
		}
		var parts []Value
		switch {
		case sepV.kind == KindUndefined:
			parts = []Value{String(s)}
		case sepV.kind != KindString:
			return Unsupported("String.prototype.split with a non-string separator")
		case sepV.s == "":
			for _, c := range u {
				parts = append(parts, String(fromUnits([]uint16{c})))
			}
		default:
			for _, p := range strings.Split(s, sepV.s) {
				parts = append(parts, String(p))
			}
		}
		if len(parts) > limit {
			parts = parts[:limit]
		}
		return NewArray(parts...)
	case "slice":
		st := relIndex(arg(args, 0), n, 0)
		en := relIndex(arg(args, 1), n, n)
		if en < st {
			en = st
		}
		return String(fromUnits(u[st:en]))
	case "substring":
		clamp := func(v Value, def int) int {
			if v.kind == KindUndefined {
				return def
			}
			f := ToIntegerOrInfinity(v.ToNumber())
			if f < 0 {
				return 0
			}
			if f > float64(n) {
				return n
			}
			return int(f)
		}
		st, en := clamp(arg(args, 0), 0), clamp(arg(args, 1), n)
		if st > en {
			st, en = en, st
		}
		return String(fromUnits(u[st:en]))
	case "startsWith", "endsWith", "includes", "indexOf", "lastIndexOf":
		needleV := arg(args, 0)
		if needleV.kind != KindString && needleV.kind != KindNumber && needleV.kind != KindBool && !needleV.IsNullish() {
			return Unsupported("String.prototype." + name + " with a non-string argument")
		}
		needle := units(needleV.ToString())
		switch name {
		case "startsWith":
			pos := relIndexClamp(arg(args, 1), n, 0)
			return Bool(pos+len(needle) <= n && indexUnits(u[pos:pos+len(needle)], needle, 0) == 0)
		case "endsWith":
			end := relIndexClamp(arg(args, 1), n, n)
			st := end - len(needle)
			return Bool(st >= 0 && indexUnits(u[st:end], needle, 0) == 0)
		case "includes":
			return Bool(indexUnits(u, needle, relIndexClamp(arg(args, 1), n, 0)) >= 0)
		case "indexOf":
			return Number(float64(indexUnits(u, needle, relIndexClamp(arg(args, 1), n, 0))))
		default:
			return Number(float64(lastIndexUnits(u, needle)))
		}
	case "replace", "replaceAll":
		pat := arg(args, 0)
		if pat.kind != KindString {
			return Unsupported("String.prototype." + name + " with a non-string pattern (regular expressions are not supported at build time)")
		}
		repl := arg(args, 1)
		var out strings.Builder
		rest := s
		pos := 0
		for {
			i := strings.Index(rest, pat.s)
			if i < 0 || (pat.s == "" && pos > len(s)) {
				break
			}
			out.WriteString(rest[:i])
			var r string
			if repl.IsFunction() {
				rv := repl.Call(String(pat.s), Number(float64(len(units(s[:pos+i])))), String(s))
				if rv.IsUnknown() {
					return rv
				}
				r = rv.ToString()
			} else {
				r = expandReplacement(repl.ToString(), pat.s, s[:pos+i], s[pos+i+len(pat.s):])
			}
			out.WriteString(r)
			if name == "replace" {
				out.WriteString(rest[i+len(pat.s):])
				return String(out.String())
			}
			adv := i + len(pat.s)
			if pat.s == "" {
				if adv < len(rest) {
					_, size := firstRune(rest[adv:])
					out.WriteString(rest[adv : adv+size])
					adv += size
				} else {
					rest = ""
					break
				}
			}
			rest = rest[adv:]
			pos += adv
		}
		out.WriteString(rest)
		return String(out.String())
	case "padStart", "padEnd":
		target := int(ToIntegerOrInfinity(arg(args, 0).ToNumber()))
		fill := " "
		if f := arg(args, 1); f.kind != KindUndefined {
			fill = f.ToString()
		}
		if target <= n || fill == "" {
			return String(s)
		}
		fu := units(fill)
		need := target - n
		var pad []uint16
		for len(pad) < need {
			pad = append(pad, fu...)
		}
		pad = pad[:need]
		if name == "padStart" {
			return String(fromUnits(pad) + s)
		}
		return String(s + fromUnits(pad))
	case "repeat":
		c := ToIntegerOrInfinity(arg(args, 0).ToNumber())
		if c < 0 || math.IsInf(c, 0) || c*float64(len(s)) > 1<<24 {
			return Unsupported("String.prototype.repeat count out of range")
		}
		return String(strings.Repeat(s, int(c)))
	case "charAt", "at":
		i := int(ToIntegerOrInfinity(arg(args, 0).ToNumber()))
		if name == "at" && i < 0 {
			i += n
		}
		if i < 0 || i >= n {
			if name == "at" {
				return Undefined()
			}
			return String("")
		}
		return String(fromUnits(u[i : i+1]))
	case "charCodeAt":
		i := int(ToIntegerOrInfinity(arg(args, 0).ToNumber()))
		if i < 0 || i >= n {
			return Number(math.NaN())
		}
		return Number(float64(u[i]))
	case "concat":
		var b strings.Builder
		b.WriteString(s)
		for _, a := range args {
			b.WriteString(a.ToPrimitive().ToString())
		}
		return String(b.String())
	}
	return Unsupported("String.prototype." + name)
}

func firstRune(s string) (rune, int) {
	for i, r := range s {
		_ = i
		return r, len(string(r))
	}
	return 0, 0
}

func relIndexClamp(v Value, n, def int) int {
	if v.kind == KindUndefined {
		return def
	}
	f := ToIntegerOrInfinity(v.ToNumber())
	if f < 0 {
		return 0
	}
	if f > float64(n) {
		return n
	}
	return int(f)
}

// expandReplacement applies the $-patterns of String.prototype.replace.
func expandReplacement(repl, matched, before, after string) string {
	if !strings.Contains(repl, "$") {
		return repl
	}
	var b strings.Builder
	for i := 0; i < len(repl); i++ {
		c := repl[i]
		if c != '$' || i+1 >= len(repl) {
			b.WriteByte(c)
			continue
		}
		switch repl[i+1] {
		case '$':
			b.WriteByte('$')
		case '&':
			b.WriteString(matched)
		case '`':
			b.WriteString(before)
		case '\'':
			b.WriteString(after)
		default:
			b.WriteByte(c)
			continue
		}
		i++
	}
	return b.String()
}

func numberMethod(f float64, name string, args []Value) Value {
	switch name {
	case "toString", "valueOf":
		if r := arg(args, 0); r.kind != KindUndefined && r.ToNumber() != 10 {
			if name == "toString" {
				return Unsupported("Number.prototype.toString with a radix other than 10")
			}
		}
		if name == "valueOf" {
			return Number(f)
		}
		return String(NumberToString(f))
	case "toFixed":
		d := int(ToIntegerOrInfinity(arg(args, 0).ToNumber()))
		if d < 0 || d > 100 {
			return Unsupported("toFixed digits out of range")
		}
		if math.IsNaN(f) || math.IsInf(f, 0) || math.Abs(f) >= 1e21 {
			return String(NumberToString(f))
		}
		return String(toFixed(f, d))
	}
	return Unsupported("Number.prototype." + name)
}

// toFixed implements Number.prototype.toFixed for |f| < 1e21: the exact
// binary value is scaled and rounded half up (ties pick the larger integer).
func toFixed(f float64, d int) string {
	neg := f < 0
	if neg {
		f = -f
	}
	r := new(big.Rat).SetFloat64(f)
	scale := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(d)), nil)
	r.Mul(r, new(big.Rat).SetInt(scale))
	r.Add(r, big.NewRat(1, 2))
	n := new(big.Int).Quo(r.Num(), r.Denom())
	digits := n.String()
	if d > 0 {
		if len(digits) <= d {
			digits = strings.Repeat("0", d-len(digits)+1) + digits
		}
		digits = digits[:len(digits)-d] + "." + digits[len(digits)-d:]
	}
	if neg && f != 0 {
		return "-" + digits
	}
	return digits
}
