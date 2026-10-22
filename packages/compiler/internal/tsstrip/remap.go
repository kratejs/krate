package tsstrip

import (
	"reflect"
	"sync"

	"github.com/kratejs/krate/packages/compiler/ast"
)

var posType = reflect.TypeOf(ast.Pos{})

// hasPos caches, per type, whether a value of that type can reach an ast.Pos,
// so the walk skips strings, numbers and other position-free subtrees.
var hasPos sync.Map // reflect.Type -> bool

func typeHasPos(t reflect.Type) bool {
	if v, ok := hasPos.Load(t); ok {
		return v.(bool)
	}
	// Mark in-progress types as true so recursive types terminate; the final
	// answer is stored once computed.
	hasPos.Store(t, true)
	res := false
	switch t.Kind() {
	case reflect.Struct:
		if t == posType {
			res = true
			break
		}
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			if f.IsExported() && typeHasPos(f.Type) {
				res = true
				break
			}
		}
	case reflect.Pointer, reflect.Slice, reflect.Array:
		res = typeHasPos(t.Elem())
	case reflect.Map:
		res = typeHasPos(t.Elem())
	case reflect.Interface:
		res = true
	}
	hasPos.Store(t, res)
	return res
}

// remapPositions rewrites every settable ast.Pos reachable from root. It walks
// the AST generically so node types added to package ast are covered without
// changes here. Shared nodes are visited once.
func remapPositions(root any, f func(ast.Pos) ast.Pos) {
	memo := map[ast.Pos]ast.Pos{}
	seen := map[uintptr]bool{}
	var walk func(v reflect.Value)
	walk = func(v reflect.Value) {
		switch v.Kind() {
		case reflect.Pointer:
			if v.IsNil() || !typeHasPos(v.Type().Elem()) {
				return
			}
			ptr := v.Pointer()
			if seen[ptr] {
				return
			}
			seen[ptr] = true
			walk(v.Elem())
		case reflect.Interface:
			if !v.IsNil() {
				walk(v.Elem())
			}
		case reflect.Struct:
			if v.Type() == posType {
				if !v.CanSet() {
					return
				}
				p := v.Interface().(ast.Pos)
				if p.Line <= 0 {
					return
				}
				np, ok := memo[p]
				if !ok {
					np = f(p)
					memo[p] = np
				}
				v.Set(reflect.ValueOf(np))
				return
			}
			t := v.Type()
			for i := 0; i < v.NumField(); i++ {
				if t.Field(i).IsExported() && typeHasPos(t.Field(i).Type) {
					walk(v.Field(i))
				}
			}
		case reflect.Slice, reflect.Array:
			if !typeHasPos(v.Type().Elem()) {
				return
			}
			for i := 0; i < v.Len(); i++ {
				walk(v.Index(i))
			}
		case reflect.Map:
			if !typeHasPos(v.Type().Elem()) {
				return
			}
			// Map values are not addressable; pointer values are still
			// rewritten in place through the pointer.
			it := v.MapRange()
			for it.Next() {
				walk(it.Value())
			}
		}
	}
	walk(reflect.ValueOf(root))
}
