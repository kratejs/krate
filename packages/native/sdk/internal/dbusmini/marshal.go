package dbusmini

import (
	"encoding/binary"
	"errors"
	"fmt"
	"reflect"
	"strings"

	"github.com/kratejs/krate/packages/native/sdk/internal/dbusiface"
)

var (
	tVariant = reflect.TypeOf(dbusiface.Variant{})
	tPath    = reflect.TypeOf(dbusiface.ObjectPath(""))
	tSig     = reflect.TypeOf(dbusiface.Signature(""))
	tStruct  = reflect.TypeOf(dbusiface.Struct(nil))
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

// sigOfValue is sigOfType for a value; decoded dbusiface.Struct values and
// []any have no static element type, so they are derived from the contents.
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
			if _, isVar := v.MapIndex(key).Elem().Interface().(dbusiface.Variant); !isVar {
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

type encoder struct {
	buf []byte
	err error
}

func (e *encoder) pad(n int) {
	for len(e.buf)%n != 0 {
		e.buf = append(e.buf, 0)
	}
}

func (e *encoder) u32(v uint32) {
	e.pad(4)
	e.buf = binary.LittleEndian.AppendUint32(e.buf, v)
}

func (e *encoder) str(s string) {
	e.u32(uint32(len(s)))
	e.buf = append(e.buf, s...)
	e.buf = append(e.buf, 0)
}

func (e *encoder) sig(s string) {
	e.buf = append(e.buf, byte(len(s)))
	e.buf = append(e.buf, s...)
	e.buf = append(e.buf, 0)
}

func alignOf(sig byte) int {
	switch sig {
	case 'y', 'g', 'v':
		return 1
	case 'n', 'q':
		return 2
	case 'b', 'i', 'u', 's', 'o', 'a':
		return 4
	}
	return 8 // x t d ( {
}

// value encodes v as the type with signature sig.
func (e *encoder) value(sig string, v reflect.Value) {
	if e.err != nil {
		return
	}
	for v.Kind() == reflect.Interface {
		v = v.Elem()
	}
	switch sig[0] {
	case 'y':
		e.buf = append(e.buf, byte(v.Uint()))
	case 'b':
		x := uint32(0)
		if v.Bool() {
			x = 1
		}
		e.u32(x)
	case 'n', 'q':
		e.pad(2)
		var x uint64
		if sig[0] == 'n' {
			x = uint64(v.Int())
		} else {
			x = v.Uint()
		}
		e.buf = binary.LittleEndian.AppendUint16(e.buf, uint16(x))
	case 'i':
		e.u32(uint32(v.Int()))
	case 'u':
		e.u32(uint32(v.Uint()))
	case 'x':
		e.pad(8)
		e.buf = binary.LittleEndian.AppendUint64(e.buf, uint64(v.Int()))
	case 't':
		e.pad(8)
		e.buf = binary.LittleEndian.AppendUint64(e.buf, v.Uint())
	case 'd':
		e.pad(8)
		e.buf = binary.LittleEndian.AppendUint64(e.buf, mathFloat64bits(v.Float()))
	case 's', 'o':
		e.str(v.String())
	case 'g':
		e.sig(v.String())
	case 'v':
		inner := v.Interface().(dbusiface.Variant).Value
		iv := reflect.ValueOf(inner)
		s, err := sigOfValue(iv)
		if err != nil {
			e.err = err
			return
		}
		e.sig(s)
		e.value(s, iv)
	case 'a':
		elem := sig[1:]
		if elem[0] == '{' {
			e.u32(0)
			lenAt := len(e.buf) - 4
			e.pad(8)
			start := len(e.buf)
			kSig, vSig := splitDict(elem)
			keys := v.MapKeys()
			sortKeys(keys)
			for _, k := range keys {
				e.pad(8)
				e.value(kSig, k)
				e.value(vSig, v.MapIndex(k))
			}
			binary.LittleEndian.PutUint32(e.buf[lenAt:], uint32(len(e.buf)-start))
			return
		}
		e.u32(0)
		lenAt := len(e.buf) - 4
		e.pad(alignOf(elem[0]))
		start := len(e.buf)
		if elem == "y" && v.Kind() == reflect.Slice {
			e.buf = append(e.buf, v.Bytes()...)
		} else {
			for i := 0; i < v.Len(); i++ {
				e.value(elem, v.Index(i))
			}
		}
		binary.LittleEndian.PutUint32(e.buf[lenAt:], uint32(len(e.buf)-start))
	case '(':
		e.pad(8)
		fields := splitStruct(sig)
		if v.Type() == tStruct || v.Kind() == reflect.Slice {
			for i, f := range fields {
				e.value(f, v.Index(i))
			}
			return
		}
		fi := 0
		for i := 0; i < v.NumField(); i++ {
			if !v.Type().Field(i).IsExported() {
				continue
			}
			e.value(fields[fi], v.Field(i))
			fi++
		}
	default:
		e.err = fmt.Errorf("dbus: cannot encode signature %q", sig)
	}
}

// splitDict returns the key and value signatures of "{kv}".
func splitDict(s string) (string, string) {
	inner := s[1 : len(s)-1]
	n := singleLen(inner)
	return inner[:n], inner[n:]
}

// splitStruct returns the member signatures of "(...)".
func splitStruct(s string) []string {
	inner := s[1 : len(s)-1]
	var out []string
	for len(inner) > 0 {
		n := singleLen(inner)
		out = append(out, inner[:n])
		inner = inner[n:]
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

// encodeArgs marshals args and returns the body signature and bytes.
func encodeArgs(args []any) (string, []byte, error) {
	e := &encoder{}
	var sig strings.Builder
	for _, a := range args {
		v := reflect.ValueOf(a)
		s, err := sigOfValue(v)
		if err != nil {
			return "", nil, err
		}
		sig.WriteString(s)
		e.value(s, v)
	}
	return sig.String(), e.buf, e.err
}

type decoder struct {
	buf []byte
	off int
	err error
}

func (d *decoder) pad(n int) {
	for d.off%n != 0 {
		d.off++
	}
}

func (d *decoder) need(n int) bool {
	if d.err != nil {
		return false
	}
	if d.off+n > len(d.buf) {
		d.err = errors.New("dbus: message truncated")
		return false
	}
	return true
}

func (d *decoder) u32() uint32 {
	d.pad(4)
	if !d.need(4) {
		return 0
	}
	v := binary.LittleEndian.Uint32(d.buf[d.off:])
	d.off += 4
	return v
}

func (d *decoder) str() string {
	n := int(d.u32())
	if !d.need(n + 1) {
		return ""
	}
	s := string(d.buf[d.off : d.off+n])
	d.off += n + 1
	return s
}

func (d *decoder) sig() string {
	if !d.need(1) {
		return ""
	}
	n := int(d.buf[d.off])
	d.off++
	if !d.need(n + 1) {
		return ""
	}
	s := string(d.buf[d.off : d.off+n])
	d.off += n + 1
	return s
}

func (d *decoder) value(sig string) any {
	if d.err != nil {
		return nil
	}
	switch sig[0] {
	case 'y':
		if !d.need(1) {
			return nil
		}
		v := d.buf[d.off]
		d.off++
		return v
	case 'b':
		return d.u32() != 0
	case 'n', 'q':
		d.pad(2)
		if !d.need(2) {
			return nil
		}
		v := binary.LittleEndian.Uint16(d.buf[d.off:])
		d.off += 2
		if sig[0] == 'n' {
			return int16(v)
		}
		return v
	case 'i':
		return int32(d.u32())
	case 'u':
		return d.u32()
	case 'x', 't', 'd':
		d.pad(8)
		if !d.need(8) {
			return nil
		}
		v := binary.LittleEndian.Uint64(d.buf[d.off:])
		d.off += 8
		switch sig[0] {
		case 'x':
			return int64(v)
		case 'd':
			return mathFloat64frombits(v)
		}
		return v
	case 's':
		return d.str()
	case 'o':
		return dbusiface.ObjectPath(d.str())
	case 'g':
		return dbusiface.Signature(d.sig())
	case 'v':
		s := d.sig()
		if s == "" || d.err != nil {
			return nil
		}
		return dbusiface.Variant{Value: d.value(s)}
	case 'a':
		n := int(d.u32())
		elem := sig[1:]
		d.pad(alignOf(elem[0]))
		end := d.off + n
		if end > len(d.buf) {
			d.err = errors.New("dbus: array overruns the message")
			return nil
		}
		if elem == "y" {
			b := append([]byte(nil), d.buf[d.off:end]...)
			d.off = end
			return b
		}
		if elem[0] == '{' {
			kSig, vSig := splitDict(elem)
			strKeys := kSig == "s" || kSig == "o"
			ms := map[string]any{}
			ma := map[any]any{}
			for d.off < end && d.err == nil {
				d.pad(8)
				k := d.value(kSig)
				v := d.value(vSig)
				if strKeys {
					ms[fmt.Sprint(k)] = v
				} else {
					ma[k] = v
				}
			}
			if strKeys {
				return ms
			}
			return ma
		}
		out := []any{}
		for d.off < end && d.err == nil {
			out = append(out, d.value(elem))
		}
		return out
	case '(':
		d.pad(8)
		var out dbusiface.Struct
		for _, f := range splitStruct(sig) {
			out = append(out, d.value(f))
		}
		return out
	}
	d.err = fmt.Errorf("dbus: cannot decode signature %q", sig)
	return nil
}

func decodeBody(sig string, body []byte) ([]any, error) {
	d := &decoder{buf: body}
	var out []any
	for sig != "" {
		n := singleLen(sig)
		out = append(out, d.value(sig[:n]))
		sig = sig[n:]
	}
	return out, d.err
}
