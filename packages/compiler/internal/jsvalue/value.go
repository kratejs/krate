// Package jsvalue models JavaScript values for build-time evaluation.
//
// The compiler folds signal-less component bodies and constant expressions at
// build time. Doing that with plain Go strings loses the JS type of a value
// (the number 0 and the string "0" both become "0", `false` renders as the text
// "false", and a prop string starting with "<" is mistaken for markup). Value
// keeps the JS type so truthiness, operators, equality and React child
// rendering follow JS/React semantics, and only JSX values are ever emitted as
// raw markup.
package jsvalue

import (
	"sort"
	"strings"
)

// Kind is the JS type of a Value.
type Kind uint8

const (
	// KindUndefined is the zero Kind, so a zero Value is undefined.
	KindUndefined Kind = iota
	// KindUnknown is a value that is not statically known at build time. It is
	// either dynamic (resolved at runtime, e.g. a signal read) or the result of
	// a construct the evaluator does not support; Reason says which.
	KindUnknown
	KindNull
	KindBool
	KindNumber
	KindString
	KindArray
	KindObject
	KindFunction
	// KindJSX is a rendered JSX element/fragment: a safe, already-escaped HTML
	// fragment. It is the only kind ever written to HTML unescaped.
	KindJSX
)

func (k Kind) String() string {
	switch k {
	case KindUndefined:
		return "undefined"
	case KindNull:
		return "null"
	case KindBool:
		return "boolean"
	case KindNumber:
		return "number"
	case KindString:
		return "string"
	case KindArray:
		return "array"
	case KindObject:
		return "object"
	case KindFunction:
		return "function"
	case KindJSX:
		return "jsx"
	}
	return "unknown"
}

// Value is an immutable handle to a JS value. Arrays and objects are reference
// types (like JS) so `arr.push(x)` in a statically evaluated loop is visible
// through every handle.
type Value struct {
	kind Kind
	b    bool
	n    float64
	s    string // string value, JSX html, or Unknown reason
	arr  *Array
	obj  *Object
	fn   *Function
	// unsupported marks an Unknown produced by an unsupported construct (as
	// opposed to a value that is merely dynamic).
	unsupported bool
}

// Array is a JS array.
type Array struct {
	Elems []Value
}

// Object is a JS plain object with insertion-ordered keys.
type Object struct {
	keys  []string
	props map[string]Value
}

// Function is a callable value (an arrow function closure or a built-in).
type Function struct {
	Name string
	// Call invokes the function. It must not be nil.
	Call func(args []Value) Value
}

var (
	undefinedValue = Value{kind: KindUndefined}
	nullValue      = Value{kind: KindNull}
	trueValue      = Value{kind: KindBool, b: true}
	falseValue     = Value{kind: KindBool}
)

// Undefined returns the undefined value.
func Undefined() Value { return undefinedValue }

// Null returns the null value.
func Null() Value { return nullValue }

// Bool returns a boolean value.
func Bool(b bool) Value {
	if b {
		return trueValue
	}
	return falseValue
}

// Number returns a number value.
func Number(f float64) Value { return Value{kind: KindNumber, n: f} }

// String returns a string value.
func String(s string) Value { return Value{kind: KindString, s: s} }

// JSX returns a rendered JSX value wrapping safe HTML.
func JSX(html string) Value { return Value{kind: KindJSX, s: html} }

// NewArray returns an array value holding elems (not copied).
func NewArray(elems ...Value) Value {
	return Value{kind: KindArray, arr: &Array{Elems: elems}}
}

// ArrayOf returns an array value holding a copy of elems.
func ArrayOf(elems []Value) Value {
	cp := make([]Value, len(elems))
	copy(cp, elems)
	return NewArray(cp...)
}

// NewObject returns an empty object value.
func NewObject() Value {
	return Value{kind: KindObject, obj: &Object{props: map[string]Value{}}}
}

// Func returns a function value.
func Func(name string, call func(args []Value) Value) Value {
	return Value{kind: KindFunction, fn: &Function{Name: name, Call: call}}
}

// Dynamic returns an Unknown value for something only known at runtime.
func Dynamic(reason string) Value { return Value{kind: KindUnknown, s: reason} }

// Unsupported returns an Unknown value for a construct the evaluator does not
// implement. Callers without a runtime fallback must report it as a diagnostic.
func Unsupported(reason string) Value {
	return Value{kind: KindUnknown, s: reason, unsupported: true}
}

// Kind returns the value's kind.
func (v Value) Kind() Kind { return v.kind }

// IsUnknown reports whether the value is not statically known.
func (v Value) IsUnknown() bool { return v.kind == KindUnknown }

// IsUnsupported reports whether the value is Unknown because of an
// unsupported construct.
func (v Value) IsUnsupported() bool { return v.kind == KindUnknown && v.unsupported }

// Reason returns why an Unknown value is unknown ("" otherwise).
func (v Value) Reason() string {
	if v.kind != KindUnknown {
		return ""
	}
	return v.s
}

// IsNullish reports whether the value is null or undefined.
func (v Value) IsNullish() bool { return v.kind == KindNull || v.kind == KindUndefined }

// IsJSX reports whether the value is rendered markup.
func (v Value) IsJSX() bool { return v.kind == KindJSX }

// IsArray reports whether the value is an array.
func (v Value) IsArray() bool { return v.kind == KindArray }

// IsObject reports whether the value is a plain object.
func (v Value) IsObject() bool { return v.kind == KindObject }

// IsFunction reports whether the value is callable.
func (v Value) IsFunction() bool { return v.kind == KindFunction }

// BoolValue returns the payload of a boolean value.
func (v Value) BoolValue() bool { return v.b }

// NumberValue returns the payload of a number value.
func (v Value) NumberValue() float64 { return v.n }

// StringValue returns the payload of a string value.
func (v Value) StringValue() string {
	if v.kind == KindString {
		return v.s
	}
	return ""
}

// HTML returns the markup of a JSX value ("" otherwise).
func (v Value) HTML() string {
	if v.kind == KindJSX {
		return v.s
	}
	return ""
}

// Elems returns the elements of an array value (nil otherwise). The slice is
// shared; callers must not mutate it.
func (v Value) Elems() []Value {
	if v.kind == KindArray && v.arr != nil {
		return v.arr.Elems
	}
	return nil
}

// Push appends to an array value in place (no-op for non-arrays).
func (v Value) Push(elems ...Value) {
	if v.kind == KindArray && v.arr != nil {
		v.arr.Elems = append(v.arr.Elems, elems...)
	}
}

// Call invokes a function value. Calling a non-function yields Unsupported.
func (v Value) Call(args ...Value) Value {
	if v.kind != KindFunction || v.fn == nil || v.fn.Call == nil {
		if v.kind == KindUnknown {
			return v
		}
		return Unsupported("value is not a function")
	}
	return v.fn.Call(args)
}

// Same reports reference identity for arrays/objects/functions.
func (v Value) same(o Value) bool {
	switch v.kind {
	case KindArray:
		return v.arr == o.arr
	case KindObject:
		return v.obj == o.obj
	case KindFunction:
		return v.fn == o.fn
	}
	return false
}

// Object methods

// Keys returns an object's own keys in JS order: integer-like keys ascending,
// then string keys in insertion order.
func (v Value) Keys() []string {
	if v.kind != KindObject || v.obj == nil {
		return nil
	}
	var ints, strs []string
	for _, k := range v.obj.keys {
		if isArrayIndex(k) {
			ints = append(ints, k)
		} else {
			strs = append(strs, k)
		}
	}
	if len(ints) == 0 {
		return append([]string(nil), strs...)
	}
	sort.Slice(ints, func(i, j int) bool {
		if len(ints[i]) != len(ints[j]) {
			return len(ints[i]) < len(ints[j])
		}
		return ints[i] < ints[j]
	})
	return append(ints, strs...)
}

// Get returns an object's own property (ok=false when absent).
func (v Value) Get(key string) (Value, bool) {
	if v.kind != KindObject || v.obj == nil {
		return undefinedValue, false
	}
	p, ok := v.obj.props[key]
	return p, ok
}

// Set assigns an object property in place.
func (v Value) Set(key string, val Value) {
	if v.kind != KindObject || v.obj == nil {
		return
	}
	if _, ok := v.obj.props[key]; !ok {
		v.obj.keys = append(v.obj.keys, key)
	}
	v.obj.props[key] = val
}

// ObjectFrom builds an object from ordered key/value pairs.
func ObjectFrom(keys []string, vals map[string]Value) Value {
	o := NewObject()
	for _, k := range keys {
		o.Set(k, vals[k])
	}
	return o
}

func isArrayIndex(k string) bool {
	if k == "" || len(k) > 10 {
		return false
	}
	if k != "0" && k[0] == '0' {
		return false
	}
	for i := 0; i < len(k); i++ {
		if k[i] < '0' || k[i] > '9' {
			return false
		}
	}
	return true
}

// Containment checks

// ContainsUnknown reports whether v is Unknown or (for arrays/objects) holds
// an Unknown anywhere inside.
func (v Value) ContainsUnknown() bool {
	switch v.kind {
	case KindUnknown:
		return true
	case KindArray:
		for _, e := range v.Elems() {
			if e.ContainsUnknown() {
				return true
			}
		}
	case KindObject:
		for _, k := range v.obj.keys {
			if v.obj.props[k].ContainsUnknown() {
				return true
			}
		}
	}
	return false
}

// firstUnknown returns the first Unknown found inside v.
func (v Value) firstUnknown() Value {
	switch v.kind {
	case KindUnknown:
		return v
	case KindArray:
		for _, e := range v.Elems() {
			if u := e.firstUnknown(); u.kind == KindUnknown {
				return u
			}
		}
	case KindObject:
		for _, k := range v.obj.keys {
			if u := v.obj.props[k].firstUnknown(); u.kind == KindUnknown {
				return u
			}
		}
	}
	return Value{kind: KindUndefined}
}

// GoString aids debugging and test failure messages.
func (v Value) GoString() string {
	switch v.kind {
	case KindUnknown:
		if v.unsupported {
			return "Unsupported(" + v.s + ")"
		}
		return "Dynamic(" + v.s + ")"
	case KindJSX:
		return "JSX(" + v.s + ")"
	case KindFunction:
		return "Function(" + v.fn.Name + ")"
	}
	if src, ok := JSSource(v); ok {
		return src
	}
	return strings.ToUpper(v.kind.String())
}
