package gobind

import (
	"go/token"
	"go/types"
	"reflect"
	"sort"
	"strings"

	"github.com/kratejs/krate/packages/compiler/internal/diag"
)

const (
	krateImportPath = "github.com/kratejs/krate/packages/native/krate"
	timePath        = "time"
)

type kind int

const (
	kBool kind = iota
	kString
	kInt
	kUint
	kFloat
	kBytes
	kTime
	kSlice
	kArray
	kMap
	kPtr
	kStruct
)

// wtype is the wire view of a Go type.
type wtype struct {
	kind  kind
	gt    types.Type   // the Go type, aliases resolved
	named *types.Named // set for named types other than time.Time
	basic *types.Basic // underlying basic type of scalar kinds
	elem  *wtype       // slice, array, map and pointer element
	key   types.Type   // map key Go type
	size  int64        // array length
	// fields of a struct, flattened and in declaration order.
	fields []*wfield
	// partial is true while the type is being mapped.
	partial bool
}

// wfield is one JSON-visible struct field.
type wfield struct {
	json      string
	omitEmpty bool
	// sel is the Go selector below the struct value, such as "Base.ID".
	sel string
	t   *wtype
}

// site is where a type is used; diagnostics are reported against it.
type site struct {
	label string // such as `Add parameter "task"`
	pos   token.Pos
}

// mapper turns Go types into wtypes, reporting what has no mapping.
type mapper struct {
	a     *analyzer
	cache map[string]*wtype
}

func newMapper(a *analyzer) *mapper {
	return &mapper{a: a, cache: map[string]*wtype{}}
}

// typeKey identifies a type across packages.
func typeKey(t types.Type) string {
	return types.TypeString(t, func(p *types.Package) string { return p.Path() })
}

// typeName is a type's name for messages.
func typeName(t types.Type) string {
	return types.TypeString(t, func(p *types.Package) string { return p.Name() })
}

func isNamedFrom(t types.Type, path, name string) bool {
	n, ok := types.Unalias(t).(*types.Named)
	if !ok || n.Obj().Pkg() == nil {
		return false
	}
	return n.Obj().Pkg().Path() == path && n.Obj().Name() == name
}

type trail struct {
	site *site
	path []string
	pos  token.Pos
}

func (tr trail) with(name string, pos token.Pos) trail {
	p := make([]string, len(tr.path), len(tr.path)+1)
	copy(p, tr.path)
	tr.path = append(p, name)
	if pos.IsValid() {
		tr.pos = pos
	}
	return tr
}

// pathString joins a trail: field names with dots, "[]" and "{}" markers
// without.
func pathString(path []string) string {
	var b strings.Builder
	for _, p := range path {
		if b.Len() > 0 && p != "[]" && p != "{}" {
			b.WriteByte('.')
		}
		b.WriteString(p)
	}
	return b.String()
}

func (m *mapper) fail(tr trail, code diag.Code, what, hint string) {
	msg := tr.site.label
	if len(tr.path) > 0 {
		msg += ": " + pathString(tr.path)
	}
	msg += " " + what
	m.a.report(tr.pos, tr.site.pos, code, msg, hint)
}

// Map maps t used at s. It returns nil after reporting a diagnostic.
func (m *mapper) Map(t types.Type, s *site) *wtype {
	return m.wt(t, trail{site: s, pos: s.pos})
}

func (m *mapper) wt(t types.Type, tr trail) *wtype {
	t = types.Unalias(t)
	switch t := t.(type) {
	case *types.Basic:
		return m.basic(t, tr)
	case *types.Named:
		return m.named(t, tr)
	case *types.Pointer:
		elem := m.wt(t.Elem(), tr)
		if elem == nil {
			return nil
		}
		return &wtype{kind: kPtr, gt: t, elem: elem}
	case *types.Slice, *types.Array, *types.Map, *types.Struct:
		return m.composite(t, tr)
	case *types.Interface:
		what := "has type " + typeName(t)
		if t.Empty() {
			what = "has type any"
		}
		m.fail(tr, diag.CodeGoBindType, what+", which has no wire mapping",
			"use a concrete type: a struct, slice, map or scalar. Interfaces carry no field list for the generated encoder")
	case *types.Chan:
		m.fail(tr, diag.CodeGoBindType, "is a channel; channels only work as the result of a stream function",
			"return the channel from an exported function as (<-chan T, error), or send the values in a slice")
	case *types.Signature:
		m.fail(tr, diag.CodeGoBindType, "is a func and cannot cross the host boundary",
			"pass data, not behavior; resolve the callback in Go code")
	case *types.TypeParam:
		m.fail(tr, diag.CodeGoBindType, "has the type parameter "+t.Obj().Name()+", which does not resolve to a concrete type",
			"use an instantiated type; exported host functions cannot be generic")
	default:
		m.fail(tr, diag.CodeGoBindType, "has type "+typeName(t)+", which has no wire mapping", "use a struct, slice, map or scalar type")
	}
	return nil
}

func (m *mapper) basic(t *types.Basic, tr trail) *wtype {
	w := &wtype{gt: t, basic: t}
	switch t.Kind() {
	case types.Bool:
		w.kind = kBool
	case types.String:
		w.kind = kString
	case types.Int, types.Int8, types.Int16, types.Int32, types.Int64:
		w.kind = kInt
	case types.Uint, types.Uint8, types.Uint16, types.Uint32, types.Uint64:
		w.kind = kUint
	case types.Float32, types.Float64:
		w.kind = kFloat
	case types.Complex64, types.Complex128:
		m.fail(tr, diag.CodeGoBindType, "has type "+t.Name()+", which has no wire mapping",
			"send the real and imaginary parts as two float64 fields")
		return nil
	case types.Uintptr, types.UnsafePointer:
		m.fail(tr, diag.CodeGoBindType, "has type "+t.Name()+", which has no wire mapping",
			"pointers and handles cannot leave the process; send an id or a value instead")
		return nil
	default:
		m.fail(tr, diag.CodeGoBindType, "has type "+t.Name()+", which has no wire mapping", "use a bool, string, number, struct, slice or map")
		return nil
	}
	return w
}

var marshalerMethods = []string{"MarshalJSON", "UnmarshalJSON", "MarshalText", "UnmarshalText"}

func marshalerOf(t *types.Named) string {
	for _, recv := range []types.Type{t, types.NewPointer(t)} {
		ms := types.NewMethodSet(recv)
		for _, name := range marshalerMethods {
			if ms.Lookup(nil, name) != nil {
				return name
			}
		}
	}
	return ""
}

func (m *mapper) named(t *types.Named, tr trail) *wtype {
	obj := t.Obj()
	if obj.Pkg() != nil && obj.Pkg().Path() == timePath && obj.Name() == "Time" {
		return &wtype{kind: kTime, gt: t}
	}
	key := typeKey(t)
	if w, ok := m.cache[key]; ok {
		return w
	}
	if obj.Pkg() == nil {
		// error and comparable are the only named universe types.
		m.fail(tr, diag.CodeGoBindType, "has type "+obj.Name()+", which has no wire mapping",
			"return errors as the error result; convert them to a string or a struct with a code field for data")
		return nil
	}
	if !obj.Exported() {
		m.fail(tr, diag.CodeGoBindType, "has the unexported type "+obj.Name(),
			"export the type: the generated glue is a different package and must be able to name it")
		return nil
	}
	if t.TypeParams().Len() > 0 && t.TypeArgs().Len() == 0 {
		m.fail(tr, diag.CodeGoBindType, "has the generic type "+obj.Name()+" without type arguments",
			"instantiate the type, for example "+obj.Name()+"[string]")
		return nil
	}
	if meth := marshalerOf(t); meth != "" {
		m.fail(tr, diag.CodeGoBindMarshaler, "has type "+typeName(t)+", which defines "+meth+"; generated encoders do not call it",
			"convert the value to a plain type (string, number or struct) in the exported function")
		return nil
	}
	w := &wtype{gt: t, named: t, partial: true}
	m.cache[key] = w
	if len(tr.path) == 0 {
		tr = tr.with(obj.Name(), token.NoPos)
	}
	if !m.fill(w, t.Underlying(), tr) {
		delete(m.cache, key)
		return nil
	}
	w.partial = false
	return w
}

// composite maps an unnamed slice, array, map or struct.
func (m *mapper) composite(t types.Type, tr trail) *wtype {
	// Unnamed composites are not cached by identity: they are cheap and their
	// codecs are shared by the generator through the type key.
	w := &wtype{gt: t}
	if !m.fill(w, t, tr) {
		return nil
	}
	return w
}

func isUint8(t types.Type) bool {
	b, ok := types.Unalias(t).(*types.Basic)
	return ok && b.Kind() == types.Uint8
}

// fill sets w's shape from the underlying type u.
func (m *mapper) fill(w *wtype, u types.Type, tr trail) bool {
	switch u := u.(type) {
	case *types.Basic:
		b := m.basic(u, tr)
		if b == nil {
			return false
		}
		w.kind, w.basic = b.kind, u
		return true
	case *types.Slice:
		if isUint8(u.Elem()) {
			w.kind = kBytes
			return true
		}
		elem := m.wt(u.Elem(), tr.with("[]", token.NoPos))
		if elem == nil {
			return false
		}
		w.kind, w.elem = kSlice, elem
		return true
	case *types.Array:
		elem := m.wt(u.Elem(), tr.with("[]", token.NoPos))
		if elem == nil {
			return false
		}
		w.kind, w.elem, w.size = kArray, elem, u.Len()
		return true
	case *types.Pointer:
		elem := m.wt(u.Elem(), tr)
		if elem == nil {
			return false
		}
		w.kind, w.elem = kPtr, elem
		return true
	case *types.Map:
		kb, ok := u.Key().Underlying().(*types.Basic)
		if !ok || kb.Kind() != types.String {
			m.fail(tr, diag.CodeGoBindType, "is a map with key type "+typeName(u.Key())+"; only string keys are supported",
				"use map[string]T, or a slice of key and value structs")
			return false
		}
		if n, ok := types.Unalias(u.Key()).(*types.Named); ok {
			if !n.Obj().Exported() && n.Obj().Pkg() != nil {
				m.fail(tr, diag.CodeGoBindType, "has the unexported key type "+n.Obj().Name(), "export the type or use string keys")
				return false
			}
			if meth := marshalerOf(n); meth != "" {
				m.fail(tr, diag.CodeGoBindMarshaler, "has key type "+typeName(n)+", which defines "+meth,
					"use plain string keys and convert in the exported function")
				return false
			}
		}
		elem := m.wt(u.Elem(), tr.with("{}", token.NoPos))
		if elem == nil {
			return false
		}
		w.kind, w.elem, w.key = kMap, elem, u.Key()
		return true
	case *types.Struct:
		return m.fillStruct(w, u, tr)
	}
	// Anything else reaches here through a named type whose underlying type
	// is an interface, channel or func.
	return m.wt(u, tr) != nil
}

// cand is a struct field before name conflicts are resolved.
type cand struct {
	name   string
	tagged bool
	depth  int
	order  int
	omit   bool
	f      *types.Var
	sel    []string
	exp    bool // every embedded name on the path is exported
}

func parseTag(tag string) (name string, omit bool, skip bool) {
	v, ok := reflect.StructTag(tag).Lookup("json")
	if !ok {
		return "", false, false
	}
	if v == "-" {
		return "", false, true
	}
	parts := strings.Split(v, ",")
	for _, o := range parts[1:] {
		if o == "omitempty" {
			omit = true
		}
	}
	return parts[0], omit, false
}

func (m *mapper) fillStruct(w *wtype, st *types.Struct, tr trail) bool {
	var cands []cand
	order := 0
	ok := true
	var walk func(st *types.Struct, depth int, sel []string, exp bool, seen map[*types.Struct]bool)
	walk = func(st *types.Struct, depth int, sel []string, exp bool, seen map[*types.Struct]bool) {
		for i := 0; i < st.NumFields(); i++ {
			f := st.Field(i)
			tagName, omit, skip := parseTag(st.Tag(i))
			if skip {
				continue
			}
			if f.Embedded() && tagName == "" {
				ft := types.Unalias(f.Type())
				isPtr := false
				if p, isP := ft.(*types.Pointer); isP {
					ft, isPtr = types.Unalias(p.Elem()), true
				}
				if es, isS := ft.Underlying().(*types.Struct); isS && !isTime(ft) {
					if isPtr {
						m.fail(tr.with(f.Name(), f.Pos()), diag.CodeGoBindType, "is an embedded pointer, which has no wire mapping",
							"embed the struct by value, or give the field a name")
						ok = false
						continue
					}
					if seen[es] {
						continue
					}
					seen[es] = true
					walk(es, depth+1, append(append([]string(nil), sel...), f.Name()), exp && f.Exported(), seen)
					delete(seen, es)
					continue
				}
			}
			if !f.Exported() {
				continue
			}
			name := tagName
			if name == "" {
				name = f.Name()
			}
			cands = append(cands, cand{
				name: name, tagged: tagName != "", depth: depth, order: order, omit: omit, f: f,
				sel: append(append([]string(nil), sel...), f.Name()), exp: exp,
			})
			order++
		}
	}
	walk(st, 0, nil, true, map[*types.Struct]bool{st: true})

	byName := map[string][]cand{}
	for _, c := range cands {
		byName[c.name] = append(byName[c.name], c)
	}
	var winners []cand
	for _, group := range byName {
		min := group[0].depth
		for _, c := range group {
			if c.depth < min {
				min = c.depth
			}
		}
		var top, tagged []cand
		for _, c := range group {
			if c.depth == min {
				top = append(top, c)
				if c.tagged {
					tagged = append(tagged, c)
				}
			}
		}
		switch {
		case len(top) == 1:
			winners = append(winners, top[0])
		case len(tagged) == 1:
			winners = append(winners, tagged[0])
		}
	}
	sort.Slice(winners, func(i, j int) bool { return winners[i].order < winners[j].order })

	w.kind = kStruct
	for _, c := range winners {
		ft := m.wt(c.f.Type(), tr.with(c.f.Name(), c.f.Pos()))
		if ft == nil {
			ok = false
			continue
		}
		sel := c.sel[len(c.sel)-1]
		if c.exp {
			sel = strings.Join(c.sel, ".")
		}
		w.fields = append(w.fields, &wfield{json: c.name, omitEmpty: c.omit, sel: sel, t: ft})
	}
	return ok
}

func isTime(t types.Type) bool { return isNamedFrom(t, timePath, "Time") }
