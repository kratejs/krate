package dbusiface

import (
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/kratejs/krate/packages/native/internal/dbus"
)

var (
	tVariant = reflect.TypeOf(Variant{})
	tPath    = reflect.TypeOf(ObjectPath(""))
	tSig     = reflect.TypeOf(Signature(""))
	tStruct  = reflect.TypeOf(Struct(nil))
)

// sigOfType returns the D-Bus signature of a Go type.
func sigOfType(t reflect.Type) (string, error) {
	switch t {
	case tVariant:
		return "v", nil
	case tPath:
		return "o", nil
	case tSig:
		return "g", nil
	}
	switch t.Kind() {
	case reflect.Uint8:
		return "y", nil
	case reflect.Bool:
		return "b", nil
	case reflect.Int16:
		return "n", nil
	case reflect.Uint16:
		return "q", nil
	case reflect.Int32:
		return "i", nil
	case reflect.Uint32:
		return "u", nil
	case reflect.Int64:
		return "x", nil
	case reflect.Uint64:
		return "t", nil
	case reflect.Float64:
		return "d", nil
	case reflect.String:
		return "s", nil
	case reflect.Slice:
		e, err := sigOfType(t.Elem())
		return "a" + e, err
	case reflect.Map:
		k, err := sigOfType(t.Key())
		if err != nil {
			return "", err
		}
		v, err := sigOfType(t.Elem())
		return "a{" + k + v + "}", err
	case reflect.Struct:
		var b strings.Builder
		b.WriteByte('(')
		for i := 0; i < t.NumField(); i++ {
			if !t.Field(i).IsExported() {
				continue
			}
			s, err := sigOfType(t.Field(i).Type)
			if err != nil {
				return "", err
			}
			b.WriteString(s)
		}
		b.WriteByte(')')
		return b.String(), nil
	}
	return "", fmt.Errorf("dbus: unsupported Go type %s", t)
}

// sigOfValue is sigOfType for a value; decoded Struct values and []any have
// no static element type, so they are derived from the contents.
func sigOfValue(v reflect.Value) (string, error) {
	for v.Kind() == reflect.Interface {
		v = v.Elem()
	}
	if !v.IsValid() {
		return "", errors.New("dbus: nil value")
	}
	if v.Type() == tStruct {
		var b strings.Builder
		b.WriteByte('(')
		for i := 0; i < v.Len(); i++ {
			s, err := sigOfValue(v.Index(i))
			if err != nil {
				return "", err
			}
			b.WriteString(s)
		}
		b.WriteByte(')')
		return b.String(), nil
	}
	if v.Kind() == reflect.Slice && v.Type().Elem().Kind() == reflect.Interface {
		if v.Len() == 0 {
			return "av", nil // best guess; typed slices are exact
		}
		e, err := sigOfValue(v.Index(0))
		return "a" + e, err
	}
	if v.Kind() == reflect.Map && v.Type().Elem().Kind() == reflect.Interface {
		k, err := sigOfType(v.Type().Key())
		if err != nil {
			return "", err
		}
		val := "v"
		for _, key := range v.MapKeys() {
			if _, isVar := v.MapIndex(key).Elem().Interface().(Variant); !isVar {
				if val, err = sigOfValue(v.MapIndex(key)); err != nil {
					return "", err
				}
			}
			break
		}
		return "a{" + k + val + "}", nil
	}
	return sigOfType(v.Type())
}

// splitSig returns the complete types of sig.
func splitSig(sig string) []string {
	var out []string
	for len(sig) > 0 {
		n := singleLen(sig)
		out = append(out, sig[:n])
		sig = sig[n:]
	}
	return out
}

// singleLen is the length of the first complete type in s.
func singleLen(s string) int {
	switch s[0] {
	case 'a':
		return 1 + singleLen(s[1:])
	case '(', '{':
		open, closeCh := s[0], byte(')')
		if open == '{' {
			closeCh = '}'
		}
		depth := 0
		for i := 0; i < len(s); i++ {
			if s[i] == open {
				depth++
			} else if s[i] == closeCh {
				depth--
				if depth == 0 {
					return i + 1
				}
			}
		}
		return len(s)
	}
	return 1
}

// encodeArgs returns the body signature of args and the values in the form
// internal/dbus encodes.
func encodeArgs(args []any) (string, []any, error) {
	var sig strings.Builder
	out := make([]any, 0, len(args))
	for _, a := range args {
		v := reflect.ValueOf(a)
		s, err := sigOfValue(v)
		if err != nil {
			return "", nil, err
		}
		w, err := toWire(s, v)
		if err != nil {
			return "", nil, err
		}
		sig.WriteString(s)
		out = append(out, w)
	}
	return sig.String(), out, nil
}

// toWire converts v, which has signature sig, to the Go value internal/dbus
// expects for that type.
func toWire(sig string, v reflect.Value) (any, error) {
	for v.Kind() == reflect.Interface {
		v = v.Elem()
	}
	switch sig[0] {
	case 'y':
		return byte(v.Uint()), nil
	case 'b':
		return v.Bool(), nil
	case 'n':
		return int16(v.Int()), nil
	case 'q':
		return uint16(v.Uint()), nil
	case 'i':
		return int32(v.Int()), nil
	case 'u':
		return uint32(v.Uint()), nil
	case 'x':
		return v.Int(), nil
	case 't':
		return v.Uint(), nil
	case 'd':
		return v.Float(), nil
	case 's':
		return v.String(), nil
	case 'o':
		return dbus.ObjectPath(v.String()), nil
	case 'g':
		return dbus.Signature(v.String()), nil
	case 'v':
		inner := reflect.ValueOf(v.Interface().(Variant).Value)
		s, err := sigOfValue(inner)
		if err != nil {
			return nil, err
		}
		w, err := toWire(s, inner)
		if err != nil {
			return nil, err
		}
		return dbus.Variant{Sig: s, Value: w}, nil
	case 'a':
		elem := sig[1:]
		if elem == "y" && v.Kind() == reflect.Slice {
			return append([]byte(nil), v.Bytes()...), nil
		}
		if elem[0] == '{' {
			parts := splitSig(elem[1 : len(elem)-1])
			keys := v.MapKeys()
			sortKeys(keys)
			out := make(map[any]any, len(keys))
			for _, k := range keys {
				kw, err := toWire(parts[0], k)
				if err != nil {
					return nil, err
				}
				vw, err := toWire(parts[1], v.MapIndex(k))
				if err != nil {
					return nil, err
				}
				out[kw] = vw
			}
			return out, nil
		}
		out := make([]any, v.Len())
		for i := range out {
			w, err := toWire(elem, v.Index(i))
			if err != nil {
				return nil, err
			}
			out[i] = w
		}
		return out, nil
	case '(':
		fields := splitSig(sig[1 : len(sig)-1])
		out := make([]any, 0, len(fields))
		if v.Type() == tStruct || v.Kind() == reflect.Slice {
			for i, f := range fields {
				w, err := toWire(f, v.Index(i))
				if err != nil {
					return nil, err
				}
				out = append(out, w)
			}
			return out, nil
		}
		fi := 0
		for i := 0; i < v.NumField(); i++ {
			if !v.Type().Field(i).IsExported() {
				continue
			}
			w, err := toWire(fields[fi], v.Field(i))
			if err != nil {
				return nil, err
			}
			out = append(out, w)
			fi++
		}
		return out, nil
	}
	return nil, fmt.Errorf("dbus: cannot encode signature %q", sig)
}

// fromWire converts a value decoded by internal/dbus to the types this
// package documents: arrays of non-bytes become []any, structs Struct, and
// variants, object paths and signatures this package's types.
func fromWire(v any) any {
	switch x := v.(type) {
	case dbus.Variant:
		return Variant{Value: fromWire(x.Value)}
	case dbus.ObjectPath:
		return ObjectPath(x)
	case dbus.Signature:
		return Signature(x)
	case dbus.Struct:
		out := make(Struct, len(x))
		for i, f := range x {
			out[i] = fromWire(f)
		}
		return out
	case []byte:
		return x
	case []string:
		out := make([]any, len(x))
		for i, s := range x {
			out[i] = s
		}
		return out
	case []dbus.ObjectPath:
		out := make([]any, len(x))
		for i, s := range x {
			out[i] = ObjectPath(s)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, f := range x {
			out[i] = fromWire(f)
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, f := range x {
			out[k] = fromWire(f)
		}
		return out
	case map[any]any:
		out := make(map[any]any, len(x))
		for k, f := range x {
			out[fromWire(k)] = fromWire(f)
		}
		return out
	}
	return v
}

func fromWireAll(vs []any) []any {
	out := make([]any, len(vs))
	for i, v := range vs {
		out[i] = fromWire(v)
	}
	return out
}

func sortKeys(keys []reflect.Value) {
	sort.Slice(keys, func(i, j int) bool {
		a, b := keys[i], keys[j]
		switch a.Kind() {
		case reflect.String:
			return a.String() < b.String()
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			return a.Int() < b.Int()
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			return a.Uint() < b.Uint()
		}
		return false
	})
}
