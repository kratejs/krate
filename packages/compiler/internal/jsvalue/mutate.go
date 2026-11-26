package jsvalue

import (
	"github.com/kratejs/krate/packages/compiler/ast"
)

// inPlaceMethods are the array methods Interp.InPlace runs on the receiver.
var inPlaceMethods = map[string]bool{
	"reverse": true, "sort": true, "pop": true, "shift": true, "unshift": true, "splice": true,
}

// inPlaceArrayMethod runs an array method that changes its receiver. ok=false
// when name is not one of them. Callers only use it when the receiver is a
// private copy (Interp.InPlace).
func inPlaceArrayMethod(recv Value, name string, args []Value) (Value, bool) {
	if recv.kind != KindArray || recv.arr == nil {
		return Value{}, false
	}
	a := recv.arr
	n := len(a.Elems)
	switch name {
	case "reverse":
		for i, j := 0, n-1; i < j; i, j = i+1, j-1 {
			a.Elems[i], a.Elems[j] = a.Elems[j], a.Elems[i]
		}
		return recv, true
	case "sort":
		sorted := arrayMethod(recv, "toSorted", args)
		if sorted.IsUnknown() {
			return sorted, true
		}
		a.Elems = append([]Value(nil), sorted.Elems()...)
		return recv, true
	case "pop":
		if n == 0 {
			return Undefined(), true
		}
		last := a.Elems[n-1]
		a.Elems = a.Elems[:n-1]
		return last, true
	case "shift":
		if n == 0 {
			return Undefined(), true
		}
		first := a.Elems[0]
		a.Elems = append([]Value(nil), a.Elems[1:]...)
		return first, true
	case "unshift":
		a.Elems = append(append([]Value(nil), args...), a.Elems...)
		return Number(float64(len(a.Elems))), true
	case "splice":
		start := relIndex(arg(args, 0), n, 0)
		del := 0
		switch {
		case len(args) == 0:
		case len(args) == 1:
			del = n - start
		default:
			d := ToIntegerOrInfinity(args[1].ToNumber())
			switch {
			case d < 0:
				del = 0
			case d > float64(n-start):
				del = n - start
			default:
				del = int(d)
			}
		}
		removed := append([]Value(nil), a.Elems[start:start+del]...)
		var items []Value
		if len(args) > 2 {
			items = args[2:]
		}
		next := make([]Value, 0, n-del+len(items))
		next = append(next, a.Elems[:start]...)
		next = append(next, items...)
		next = append(next, a.Elems[start+del:]...)
		a.Elems = next
		return NewArray(removed...), true
	}
	return Value{}, false
}

// deleteMember removes an own property of an object (or clears an array
// slot). It reports whether the target could be changed.
func deleteMember(obj Value, key string) Value {
	switch obj.kind {
	case KindObject:
		o := obj.obj
		if _, ok := o.props[key]; !ok {
			return Bool(true)
		}
		delete(o.props, key)
		delete(o.getters, key)
		for i, k := range o.keys {
			if k == key {
				o.keys = append(o.keys[:i:i], o.keys[i+1:]...)
				break
			}
		}
		return Bool(true)
	case KindArray:
		if isArrayIndex(key) {
			idx := int(StringToNumber(key))
			if idx < len(obj.arr.Elems) {
				obj.arr.Elems[idx] = Undefined()
			}
			return Bool(true)
		}
	case KindUnknown:
		return obj
	}
	return Unsupported("delete of a property of this value")
}

// Cloner deep-copies values. Arrays, objects, Maps, Sets and Dates are
// copied; a value reached twice is copied once, so aliasing and cycles
// survive. Functions, JSX, RegExps, iterators and built-in namespaces are
// shared: they hold no state the build changes.
type Cloner struct {
	arrs  map[*Array]*Array
	objs  map[*Object]*Object
	colls map[*Collection]*Collection
}

// NewCloner returns a Cloner with an empty identity memo.
func NewCloner() *Cloner {
	return &Cloner{
		arrs:  map[*Array]*Array{},
		objs:  map[*Object]*Object{},
		colls: map[*Collection]*Collection{},
	}
}

// Clone returns a deep copy of v.
func (c *Cloner) Clone(v Value) Value {
	switch v.kind {
	case KindArray:
		if v.arr == nil {
			return v
		}
		if dup, ok := c.arrs[v.arr]; ok {
			v.arr = dup
			return v
		}
		dup := &Array{}
		c.arrs[v.arr] = dup
		dup.Elems = make([]Value, len(v.arr.Elems))
		for i, e := range v.arr.Elems {
			dup.Elems[i] = c.Clone(e)
		}
		if v.arr.Props != nil {
			dup.Props = c.cloneObject(v.arr.Props)
		}
		v.arr = dup
	case KindObject:
		v.obj = c.cloneObject(v.obj)
	case KindMap, KindSet:
		if v.coll == nil {
			return v
		}
		if dup, ok := c.colls[v.coll]; ok {
			v.coll = dup
			return v
		}
		dup := &Collection{isMap: v.coll.isMap, index: map[string]int{}}
		c.colls[v.coll] = dup
		for _, e := range v.coll.entries {
			if !e.deleted {
				dup.put(c.Clone(e.key), c.Clone(e.val))
			}
		}
		v.coll = dup
	case KindDate:
		if v.date != nil {
			d := *v.date
			v.date = &d
		}
	}
	return v
}

// Copy returns the copy made of v, when v itself (not something inside it)
// was copied.
func (c *Cloner) Copy(v Value) (Value, bool) {
	switch v.kind {
	case KindArray:
		if dup, ok := c.arrs[v.arr]; ok && v.arr != nil {
			v.arr = dup
			return v, true
		}
	case KindObject:
		if dup, ok := c.objs[v.obj]; ok && v.obj != nil {
			v.obj = dup
			return v, true
		}
	case KindMap, KindSet:
		if dup, ok := c.colls[v.coll]; ok && v.coll != nil {
			v.coll = dup
			return v, true
		}
	}
	return Value{}, false
}

func (c *Cloner) cloneObject(o *Object) *Object {
	if o == nil || o.builtin != "" {
		return o
	}
	if dup, ok := c.objs[o]; ok {
		return dup
	}
	dup := &Object{keys: append([]string(nil), o.keys...), props: make(map[string]Value, len(o.props))}
	c.objs[o] = dup
	for k, val := range o.props {
		dup.props[k] = c.Clone(val)
	}
	if len(o.getters) > 0 {
		dup.getters = make(map[string]bool, len(o.getters))
		for k := range o.getters {
			dup.getters[k] = true
		}
	}
	return dup
}

// JSXNode is a JSX element or fragment kept as source: the markup it
// stands for is built later, in the scope that renders it.
type JSXNode struct {
	Expr ast.Expr
}

// JSXFromNode returns a JSX value that carries the (already substituted)
// source of an element instead of rendered HTML.
func JSXFromNode(expr ast.Expr) Value {
	return Value{kind: KindJSX, jsx: &JSXNode{Expr: expr}}
}

// JSXSource returns the source of a JSX value made by JSXFromNode.
func (v Value) JSXSource() (ast.Expr, bool) {
	if v.kind == KindJSX && v.jsx != nil {
		return v.jsx.Expr, true
	}
	return nil, false
}

// ContainsJSXNode reports whether v holds, at any depth, a JSX value that
// carries source instead of HTML.
func (v Value) ContainsJSXNode() bool {
	return v.containsJSXNode(map[any]bool{})
}

func (v Value) containsJSXNode(seen map[any]bool) bool {
	switch v.kind {
	case KindJSX:
		return v.jsx != nil
	case KindArray:
		if v.arr == nil || seen[v.arr] {
			return false
		}
		seen[v.arr] = true
		for _, e := range v.arr.Elems {
			if e.containsJSXNode(seen) {
				return true
			}
		}
	case KindObject:
		if v.obj == nil || seen[v.obj] {
			return false
		}
		seen[v.obj] = true
		for _, p := range v.obj.props {
			if p.containsJSXNode(seen) {
				return true
			}
		}
	}
	return false
}

// DeepEqual reports whether a and b hold the same data: primitives by
// StrictEquals, arrays, objects, Maps and Sets element by element. Functions
// and the other kinds compare by identity. It is for telling whether a copy
// was changed, not a JS operator.
func DeepEqual(a, b Value) bool {
	return deepEqual(a, b, 0)
}

func deepEqual(a, b Value, depth int) bool {
	if a.kind != b.kind {
		return false
	}
	if depth > 200 {
		return a.same(b)
	}
	switch a.kind {
	case KindArray:
		if len(a.arr.Elems) != len(b.arr.Elems) {
			return false
		}
		for i := range a.arr.Elems {
			if !deepEqual(a.arr.Elems[i], b.arr.Elems[i], depth+1) {
				return false
			}
		}
		return true
	case KindObject:
		if len(a.obj.keys) != len(b.obj.keys) {
			return false
		}
		for i, k := range a.obj.keys {
			if b.obj.keys[i] != k || !deepEqual(a.obj.props[k], b.obj.props[k], depth+1) {
				return false
			}
		}
		return true
	case KindMap, KindSet:
		if a.coll.size != b.coll.size {
			return false
		}
		var ea, eb []collEntry
		for _, e := range a.coll.entries {
			if !e.deleted {
				ea = append(ea, e)
			}
		}
		for _, e := range b.coll.entries {
			if !e.deleted {
				eb = append(eb, e)
			}
		}
		for i := range ea {
			if !deepEqual(ea[i].key, eb[i].key, depth+1) || !deepEqual(ea[i].val, eb[i].val, depth+1) {
				return false
			}
		}
		return true
	case KindJSX:
		return a.s == b.s && a.jsx == b.jsx
	case KindDate:
		return a.date.ms == b.date.ms
	case KindUnknown:
		return a.s == b.s
	case KindNumber:
		return a.n == b.n || (a.n != a.n && b.n != b.n)
	}
	if a.kind == KindFunction || a.kind == KindIterator || a.kind == KindRegExp {
		return a.same(b)
	}
	return StrictEquals(a, b)
}
