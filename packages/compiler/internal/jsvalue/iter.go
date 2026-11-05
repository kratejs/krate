package jsvalue

import (
	"fmt"
	"math"
)

// Iterator is a stateful JS iterator (the value of arr.entries(),
// map.keys(), ...). It is consumed once: next advances it.
type Iterator struct {
	// Tag is the Symbol.toStringTag: "Array Iterator", "Map Iterator", ...
	Tag  string
	next func() (Value, bool)
	done bool
}

// step advances the iterator. ok=false once it is exhausted.
func (it *Iterator) step() (Value, bool) {
	if it.done {
		return Value{}, false
	}
	v, ok := it.next()
	if !ok {
		it.done = true
	}
	return v, ok
}

// NewIterator returns an iterator value that calls next until it reports false.
func NewIterator(tag string, next func() (Value, bool)) Value {
	return Value{kind: KindIterator, iter: &Iterator{Tag: tag, next: next}}
}

// Collection is the storage of a Map or a Set: entries in insertion order
// with deleted ones kept as tombstones, so an iterator started earlier keeps
// its position when entries are added or removed (JS iteration is live).
type Collection struct {
	isMap   bool
	entries []collEntry
	index   map[string]int
	size    int
}

type collEntry struct {
	key, val Value
	deleted  bool
}

// NewMap returns an empty Map.
func NewMap() Value {
	return Value{kind: KindMap, coll: &Collection{isMap: true, index: map[string]int{}}}
}

// NewSet returns an empty Set.
func NewSet() Value {
	return Value{kind: KindSet, coll: &Collection{index: map[string]int{}}}
}

// keyID is the SameValueZero identity of a key. ok=false for a value that
// cannot be a key here (JSX, which has no stable identity).
func keyID(v Value) (string, bool) {
	switch v.kind {
	case KindUndefined:
		return "u", true
	case KindNull:
		return "l", true
	case KindBool:
		if v.b {
			return "b1", true
		}
		return "b0", true
	case KindNumber:
		if v.n == 0 {
			return "n0", true
		}
		return "n" + NumberToString(v.n), true
	case KindString:
		return "s" + v.s, true
	case KindBigInt:
		return "i" + v.s, true
	case KindArray:
		return fmt.Sprintf("a%p", v.arr), true
	case KindObject:
		return fmt.Sprintf("o%p", v.obj), true
	case KindFunction:
		return fmt.Sprintf("f%p", v.fn), true
	case KindIterator:
		return fmt.Sprintf("t%p", v.iter), true
	case KindMap, KindSet:
		return fmt.Sprintf("c%p", v.coll), true
	case KindDate:
		return fmt.Sprintf("d%p", v.date), true
	}
	return "", false
}

func (c *Collection) put(key, val Value) bool {
	id, ok := keyID(key)
	if !ok {
		return false
	}
	if key.kind == KindNumber && key.n == 0 {
		key = Number(0) // -0 is stored as +0
	}
	if i, ok := c.index[id]; ok {
		c.entries[i].val = val
		return true
	}
	c.index[id] = len(c.entries)
	c.entries = append(c.entries, collEntry{key: key, val: val})
	c.size++
	return true
}

func (c *Collection) lookup(key Value) (int, bool) {
	id, ok := keyID(key)
	if !ok {
		return 0, false
	}
	i, ok := c.index[id]
	return i, ok
}

func (c *Collection) remove(key Value) bool {
	i, ok := c.lookup(key)
	if !ok {
		return false
	}
	id, _ := keyID(key)
	c.entries[i].deleted = true
	c.entries[i].key, c.entries[i].val = Value{}, Value{}
	delete(c.index, id)
	c.size--
	return true
}

func (c *Collection) clear() {
	for i := range c.entries {
		c.entries[i] = collEntry{deleted: true}
	}
	c.index = map[string]int{}
	c.size = 0
}

// cursor walks the live entries from position *pos.
func (c *Collection) cursor() func() (collEntry, bool) {
	pos := 0
	return func() (collEntry, bool) {
		for pos < len(c.entries) {
			e := c.entries[pos]
			pos++
			if !e.deleted {
				return e, true
			}
		}
		return collEntry{}, false
	}
}

// Size returns the number of entries of a Map or Set (0 for other values).
func (v Value) Size() int {
	if (v.kind == KindMap || v.kind == KindSet) && v.coll != nil {
		return v.coll.size
	}
	return 0
}

// arrayIterator iterates arr live: elements pushed while iterating are seen.
func arrayIterator(arr *Array, kind string) Value {
	i := 0
	return NewIterator("Array Iterator", func() (Value, bool) {
		if i >= len(arr.Elems) {
			return Value{}, false
		}
		idx := i
		i++
		switch kind {
		case "keys":
			return Number(float64(idx)), true
		case "values":
			return arr.Elems[idx], true
		}
		return NewArray(Number(float64(idx)), arr.Elems[idx]), true
	})
}

func collectionIterator(c *Collection, kind string) Value {
	cur := c.cursor()
	tag := "Set Iterator"
	if c.isMap {
		tag = "Map Iterator"
	}
	return NewIterator(tag, func() (Value, bool) {
		e, ok := cur()
		if !ok {
			return Value{}, false
		}
		switch kind {
		case "keys":
			return e.key, true
		case "values":
			if c.isMap {
				return e.val, true
			}
			return e.key, true
		}
		if c.isMap {
			return NewArray(e.key, e.val), true
		}
		return NewArray(e.key, e.key), true
	})
}

func stringIterator(s string) Value {
	rs := []rune(s)
	i := 0
	return NewIterator("String Iterator", func() (Value, bool) {
		if i >= len(rs) {
			return Value{}, false
		}
		i++
		return String(string(rs[i-1])), true
	})
}

// iterSource returns a pull function for an iterable value: arrays, strings,
// iterators, Maps (as [key, value] pairs) and Sets. ok=false when v is not
// iterable.
func iterSource(v Value) (func() (Value, bool), bool) {
	switch v.kind {
	case KindArray:
		i := 0
		arr := v.arr
		return func() (Value, bool) {
			if i >= len(arr.Elems) {
				return Value{}, false
			}
			i++
			return arr.Elems[i-1], true
		}, true
	case KindString:
		return stringIterator(v.s).iter.step, true
	case KindIterator:
		return v.iter.step, true
	case KindMap:
		return collectionIterator(v.coll, "entries").iter.step, true
	case KindSet:
		return collectionIterator(v.coll, "values").iter.step, true
	}
	return nil, false
}

// maxIterated bounds how many items one iteration may produce.
const maxIterated = 1_000_000

// drain pulls up to limit items (all when limit < 0).
func drain(next func() (Value, bool), limit int) ([]Value, bool) {
	var out []Value
	for limit < 0 || len(out) < limit {
		v, ok := next()
		if !ok {
			break
		}
		out = append(out, v)
		if len(out) > maxIterated {
			return out, false
		}
	}
	return out, true
}

// iterate returns the elements produced by iterating v. An iterator is
// consumed.
func iterate(v Value) ([]Value, bool) {
	if v.kind == KindArray {
		return v.Elems(), true
	}
	next, ok := iterSource(v)
	if !ok {
		return nil, false
	}
	return drain(next, -1)
}

// iterateN returns at most n elements of v. For an iterator it stops pulling
// after n, as array destructuring does, so the rest stays available.
func iterateN(v Value, n int) ([]Value, bool) {
	next, ok := iterSource(v)
	if !ok {
		return nil, false
	}
	return drain(next, n)
}

func iterResult(v Value, done bool) Value {
	return ObjectFrom([]string{"value", "done"}, map[string]Value{"value": v, "done": Bool(done)})
}

var iteratorMembers = memberSet("next toArray forEach map filter take drop reduce some every find flatMap toString")

// iteratorBuiltinMembers are the members of Iterator.prototype (ES2025). Those
// without an implementation here stay unresolved instead of reading undefined.
var iteratorBuiltinMembers = memberSet("next return throw toArray forEach map filter take drop reduce some every find flatMap constructor toString valueOf hasOwnProperty")

func iteratorMethod(recv Value, name string, args []Value) Value {
	it := recv.iter
	switch name {
	case "next":
		if v, ok := it.step(); ok {
			return iterResult(v, false)
		}
		return iterResult(Undefined(), true)
	case "toString":
		return String("[object " + it.Tag + "]")
	case "toArray":
		items, ok := drain(it.step, -1)
		if !ok {
			return Unsupported("iterator too long")
		}
		return NewArray(items...)
	case "forEach", "some", "every", "find", "reduce":
		fn := arg(args, 0)
		if !fn.IsFunction() {
			return Unsupported("Iterator.prototype." + name + " without a callback")
		}
		return iteratorFold(it, name, fn, args)
	case "map", "filter", "flatMap":
		fn := arg(args, 0)
		if !fn.IsFunction() {
			return Unsupported("Iterator.prototype." + name + " without a callback")
		}
		i := 0
		var inner func() (Value, bool)
		return NewIterator("Iterator Helper", func() (Value, bool) {
			for {
				if inner != nil {
					if v, ok := inner(); ok {
						return v, true
					}
					inner = nil
				}
				v, ok := it.step()
				if !ok {
					return Value{}, false
				}
				r := fn.Call(v, Number(float64(i)))
				i++
				if r.IsUnknown() {
					return r, true
				}
				switch name {
				case "map":
					return r, true
				case "filter":
					if r.ToBoolean() {
						return v, true
					}
				default:
					src, ok := iterSource(r)
					if !ok || r.kind == KindString {
						return Unsupported("flatMap callback result is not an iterator"), true
					}
					inner = src
				}
			}
		})
	case "take", "drop":
		lim := arg(args, 0).ToNumber()
		if math.IsNaN(lim) || lim < 0 {
			return Unsupported("RangeError: " + name + " limit must be a non-negative number")
		}
		n := 0
		return NewIterator("Iterator Helper", func() (Value, bool) {
			if name == "take" {
				if float64(n) >= lim {
					return Value{}, false
				}
				n++
				return it.step()
			}
			for float64(n) < lim {
				n++
				if _, ok := it.step(); !ok {
					return Value{}, false
				}
			}
			return it.step()
		})
	}
	return Unsupported("Iterator.prototype." + name)
}

func iteratorFold(it *Iterator, name string, fn Value, args []Value) Value {
	i := 0
	var acc Value
	haveAcc := false
	if name == "reduce" {
		if len(args) > 1 {
			acc, haveAcc = args[1], true
		}
	}
	for {
		v, ok := it.step()
		if !ok {
			break
		}
		idx := Number(float64(i))
		if name == "reduce" {
			if !haveAcc {
				acc, haveAcc = v, true
				i++
				continue
			}
			acc = fn.Call(acc, v, idx)
			if acc.IsUnknown() {
				return acc
			}
			i++
			continue
		}
		r := fn.Call(v, idx)
		if r.IsUnknown() {
			return r
		}
		i++
		switch name {
		case "some":
			if r.ToBoolean() {
				return Bool(true)
			}
		case "every":
			if !r.ToBoolean() {
				return Bool(false)
			}
		case "find":
			if r.ToBoolean() {
				return v
			}
		}
	}
	switch name {
	case "some":
		return Bool(false)
	case "every":
		return Bool(true)
	case "reduce":
		if !haveAcc {
			return Unsupported("TypeError: Reduce of empty iterator with no initial value")
		}
		return acc
	}
	return Undefined()
}

// Map and Set

var (
	mapMembers = memberSet("get set has delete clear forEach keys values entries size toString")
	setMembers = memberSet("add has delete clear forEach keys values entries size toString")
	// Members of Map.prototype / Set.prototype without an implementation here.
	mapBuiltinMembers = memberSet("get set has delete clear forEach keys values entries size constructor toString valueOf hasOwnProperty")
	setBuiltinMembers = memberSet("add has delete clear forEach keys values entries size union intersection difference symmetricDifference isSubsetOf isSupersetOf isDisjointFrom constructor toString valueOf hasOwnProperty")
)

func collectionProperty(v Value, key string) (Value, bool) {
	c := v.coll
	if key == "size" {
		return Number(float64(c.size)), true
	}
	members := setMembers
	if c.isMap {
		members = mapMembers
	}
	if members[key] {
		return Func(key, func(args []Value) Value {
			for _, a := range args {
				if a.IsUnknown() {
					return a
				}
			}
			return collectionMethod(v, key, args)
		}), true
	}
	return Value{}, false
}

func collectionMethod(recv Value, name string, args []Value) Value {
	c := recv.coll
	switch name {
	case "toString":
		if c.isMap {
			return String("[object Map]")
		}
		return String("[object Set]")
	case "get":
		if i, ok := c.lookup(arg(args, 0)); ok {
			return c.entries[i].val
		}
		if _, ok := keyID(arg(args, 0)); !ok {
			return Unsupported("Map key of this kind")
		}
		return Undefined()
	case "has":
		if _, ok := keyID(arg(args, 0)); !ok {
			return Unsupported("collection key of this kind")
		}
		_, ok := c.lookup(arg(args, 0))
		return Bool(ok)
	case "set":
		if !c.put(arg(args, 0), arg(args, 1)) {
			return Unsupported("Map key of this kind")
		}
		return recv
	case "add":
		if !c.put(arg(args, 0), arg(args, 0)) {
			return Unsupported("Set value of this kind")
		}
		return recv
	case "delete":
		if _, ok := keyID(arg(args, 0)); !ok {
			return Unsupported("collection key of this kind")
		}
		return Bool(c.remove(arg(args, 0)))
	case "clear":
		c.clear()
		return Undefined()
	case "keys", "values", "entries":
		return collectionIterator(c, name)
	case "forEach":
		fn := arg(args, 0)
		if !fn.IsFunction() {
			return Unsupported("forEach without a callback")
		}
		cur := c.cursor()
		for {
			e, ok := cur()
			if !ok {
				return Undefined()
			}
			val := e.val
			if !c.isMap {
				val = e.key
			}
			if r := fn.Call(val, e.key, recv); r.IsUnknown() {
				return r
			}
		}
	}
	return Unsupported("method " + name)
}

// newCollection builds a Map or Set from an optional iterable.
func newCollection(isMap bool, args []Value) Value {
	var out Value
	if isMap {
		out = NewMap()
	} else {
		out = NewSet()
	}
	src := arg(args, 0)
	if src.kind == KindUnknown {
		return src
	}
	if src.IsNullish() {
		return out
	}
	items, ok := iterate(src)
	if !ok {
		return Unsupported("TypeError: the Map/Set argument is not iterable")
	}
	for _, it := range items {
		if it.IsUnknown() {
			return it
		}
		if !isMap {
			if !out.coll.put(it, it) {
				return Unsupported("Set value of this kind")
			}
			continue
		}
		if it.kind != KindArray {
			return Unsupported("TypeError: Map entries must be arrays")
		}
		e := it.Elems()
		var k, v Value
		if len(e) > 0 {
			k = e[0]
		}
		if len(e) > 1 {
			v = e[1]
		}
		if !out.coll.put(k, v) {
			return Unsupported("Map key of this kind")
		}
	}
	return out
}

func mapCtor() Value {
	f := Func("Map", func([]Value) Value { return Unsupported("TypeError: Constructor Map requires 'new'") })
	f.fn.Construct = func(args []Value) Value { return newCollection(true, args) }
	return f
}

func setCtor() Value {
	f := Func("Set", func([]Value) Value { return Unsupported("TypeError: Constructor Set requires 'new'") })
	f.fn.Construct = func(args []Value) Value { return newCollection(false, args) }
	return f
}
