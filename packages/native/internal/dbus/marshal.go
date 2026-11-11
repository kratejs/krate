package dbus

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"reflect"
	"strings"
)

// ObjectPath is a D-Bus object path (signature o).
type ObjectPath string

// Signature is a D-Bus type signature (signature g).
type Signature string

// Struct is a decoded struct (signature "(...)"): one entry per field. It
// keeps structs apart from arrays, which decode to []any. Encoding accepts
// either type for a struct.
type Struct []any

// Variant is a value tagged with its signature (signature v).
type Variant struct {
	Sig   string
	Value any
}

// V builds a Variant, checking nothing; encoding validates it.
func V(sig string, v any) Variant { return Variant{Sig: sig, Value: v} }

const maxDepth = 64

// typeLen returns the length of the first complete type in sig.
func typeLen(sig string) (int, error) {
	if sig == "" {
		return 0, errors.New("dbus: empty signature")
	}
	switch c := sig[0]; c {
	case 'y', 'b', 'n', 'q', 'i', 'u', 'x', 't', 'd', 's', 'o', 'g', 'v', 'h':
		return 1, nil
	case 'a':
		n, err := typeLen(sig[1:])
		return n + 1, err
	case '(', '{':
		closer := byte(')')
		if c == '{' {
			closer = '}'
		}
		i := 1
		for i < len(sig) && sig[i] != closer {
			n, err := typeLen(sig[i:])
			if err != nil {
				return 0, err
			}
			i += n
		}
		if i >= len(sig) || i == 1 {
			return 0, fmt.Errorf("dbus: bad container type in %q", sig)
		}
		return i + 1, nil
	}
	return 0, fmt.Errorf("dbus: bad type code %q in signature %q", sig[0], sig)
}

// splitSig splits sig into its complete types.
func splitSig(sig string) ([]string, error) {
	var out []string
	for sig != "" {
		n, err := typeLen(sig)
		if err != nil {
			return nil, err
		}
		out = append(out, sig[:n])
		sig = sig[n:]
	}
	return out, nil
}

func alignOf(t byte) int {
	switch t {
	case 'y', 'g', 'v':
		return 1
	case 'n', 'q':
		return 2
	case 'b', 'i', 'u', 's', 'o', 'a':
		return 4
	case 'x', 't', 'd', '(', '{':
		return 8
	}
	return 1
}

// encoder writes values in little-endian wire form.
type encoder struct {
	buf []byte
}

func (e *encoder) align(n int) {
	for len(e.buf)%n != 0 {
		e.buf = append(e.buf, 0)
	}
}

func (e *encoder) u32(v uint32) {
	e.align(4)
	e.buf = binary.LittleEndian.AppendUint32(e.buf, v)
}

func (e *encoder) str(s string) {
	e.u32(uint32(len(s)))
	e.buf = append(e.buf, s...)
	e.buf = append(e.buf, 0)
}

func (e *encoder) sig(s string) error {
	if len(s) > 255 {
		return errors.New("dbus: signature too long")
	}
	e.buf = append(e.buf, byte(len(s)))
	e.buf = append(e.buf, s...)
	e.buf = append(e.buf, 0)
	return nil
}

// encodeAll encodes vals against sig.
func (e *encoder) encodeAll(sig string, vals []any) error {
	types, err := splitSig(sig)
	if err != nil {
		return err
	}
	if len(types) != len(vals) {
		return fmt.Errorf("dbus: signature %q wants %d values, got %d", sig, len(types), len(vals))
	}
	for i, t := range types {
		if err := e.encode(t, vals[i], 0); err != nil {
			return err
		}
	}
	return nil
}

func badValue(t string, v any) error {
	return fmt.Errorf("dbus: cannot encode %T as %q", v, t)
}

func (e *encoder) encode(t string, v any, depth int) error {
	if depth > maxDepth {
		return errors.New("dbus: value nested too deeply")
	}
	switch t[0] {
	case 'y':
		b, ok := v.(byte)
		if !ok {
			return badValue(t, v)
		}
		e.buf = append(e.buf, b)
	case 'b':
		b, ok := v.(bool)
		if !ok {
			return badValue(t, v)
		}
		if b {
			e.u32(1)
		} else {
			e.u32(0)
		}
	case 'n':
		n, ok := v.(int16)
		if !ok {
			return badValue(t, v)
		}
		e.align(2)
		e.buf = binary.LittleEndian.AppendUint16(e.buf, uint16(n))
	case 'q':
		n, ok := v.(uint16)
		if !ok {
			return badValue(t, v)
		}
		e.align(2)
		e.buf = binary.LittleEndian.AppendUint16(e.buf, n)
	case 'i':
		n, ok := v.(int32)
		if !ok {
			return badValue(t, v)
		}
		e.u32(uint32(n))
	case 'u':
		n, ok := v.(uint32)
		if !ok {
			return badValue(t, v)
		}
		e.u32(n)
	case 'x':
		n, ok := v.(int64)
		if !ok {
			return badValue(t, v)
		}
		e.align(8)
		e.buf = binary.LittleEndian.AppendUint64(e.buf, uint64(n))
	case 't':
		n, ok := v.(uint64)
		if !ok {
			return badValue(t, v)
		}
		e.align(8)
		e.buf = binary.LittleEndian.AppendUint64(e.buf, n)
	case 'd':
		f, ok := v.(float64)
		if !ok {
			return badValue(t, v)
		}
		e.align(8)
		e.buf = binary.LittleEndian.AppendUint64(e.buf, math.Float64bits(f))
	case 's', 'o':
		var s string
		switch x := v.(type) {
		case string:
			s = x
		case ObjectPath:
			s = string(x)
		default:
			return badValue(t, v)
		}
		if strings.IndexByte(s, 0) >= 0 {
			return errors.New("dbus: string contains a NUL byte")
		}
		e.str(s)
	case 'g':
		var s string
		switch x := v.(type) {
		case string:
			s = x
		case Signature:
			s = string(x)
		default:
			return badValue(t, v)
		}
		return e.sig(s)
	case 'v':
		var vr Variant
		switch x := v.(type) {
		case Variant:
			vr = x
		case *Variant:
			vr = *x
		default:
			return badValue(t, v)
		}
		if n, err := typeLen(vr.Sig); err != nil || n != len(vr.Sig) {
			return fmt.Errorf("dbus: variant signature %q is not one complete type", vr.Sig)
		}
		if err := e.sig(vr.Sig); err != nil {
			return err
		}
		return e.encode(vr.Sig, vr.Value, depth+1)
	case 'a':
		return e.encodeArray(t, v, depth)
	case '(':
		var fields []any
		switch f := v.(type) {
		case []any:
			fields = f
		case Struct:
			fields = f
		default:
			return badValue(t, v)
		}
		inner, err := splitSig(t[1 : len(t)-1])
		if err != nil {
			return err
		}
		if len(inner) != len(fields) {
			return fmt.Errorf("dbus: struct %q wants %d fields, got %d", t, len(inner), len(fields))
		}
		e.align(8)
		for i, ft := range inner {
			if err := e.encode(ft, fields[i], depth+1); err != nil {
				return err
			}
		}
	default:
		return fmt.Errorf("dbus: cannot encode type %q", t)
	}
	return nil
}

func (e *encoder) encodeArray(t string, v any, depth int) error {
	elem := t[1:]
	// Fast path for byte arrays.
	if elem == "y" {
		if b, ok := v.([]byte); ok {
			e.u32(uint32(len(b)))
			e.buf = append(e.buf, b...)
			return nil
		}
	}
	rv := reflect.ValueOf(v)
	e.u32(0) // length, patched below
	lenAt := len(e.buf) - 4
	e.align(alignOf(elem[0]))
	start := len(e.buf)
	switch {
	case elem[0] == '{':
		if rv.Kind() != reflect.Map {
			return badValue(t, v)
		}
		inner, err := splitSig(elem[1 : len(elem)-1])
		if err != nil || len(inner) != 2 {
			return fmt.Errorf("dbus: bad dict entry type %q", elem)
		}
		iter := rv.MapRange()
		for iter.Next() {
			e.align(8)
			if err := e.encode(inner[0], iter.Key().Interface(), depth+1); err != nil {
				return err
			}
			if err := e.encode(inner[1], iter.Value().Interface(), depth+1); err != nil {
				return err
			}
		}
	case rv.Kind() == reflect.Slice || rv.Kind() == reflect.Array:
		for i := 0; i < rv.Len(); i++ {
			if err := e.encode(elem, rv.Index(i).Interface(), depth+1); err != nil {
				return err
			}
		}
	default:
		return badValue(t, v)
	}
	n := len(e.buf) - start
	if n > 1<<26 {
		return errors.New("dbus: array too large")
	}
	binary.LittleEndian.PutUint32(e.buf[lenAt:], uint32(n))
	return nil
}

// decoder reads values from a buffer whose offset 0 is 8-aligned in the
// message.
type decoder struct {
	buf []byte
	pos int
	ord binary.ByteOrder
}

var errShort = errors.New("dbus: message truncated")

func (d *decoder) align(n int) error {
	for d.pos%n != 0 {
		if d.pos >= len(d.buf) {
			return errShort
		}
		d.pos++
	}
	return nil
}

func (d *decoder) take(n int) ([]byte, error) {
	if n < 0 || d.pos+n > len(d.buf) {
		return nil, errShort
	}
	b := d.buf[d.pos : d.pos+n]
	d.pos += n
	return b, nil
}

func (d *decoder) u32() (uint32, error) {
	if err := d.align(4); err != nil {
		return 0, err
	}
	b, err := d.take(4)
	if err != nil {
		return 0, err
	}
	return d.ord.Uint32(b), nil
}

func (d *decoder) str() (string, error) {
	n, err := d.u32()
	if err != nil {
		return "", err
	}
	b, err := d.take(int(n) + 1)
	if err != nil {
		return "", err
	}
	if b[len(b)-1] != 0 {
		return "", errors.New("dbus: string is not NUL terminated")
	}
	return string(b[:len(b)-1]), nil
}

func (d *decoder) sig() (string, error) {
	b, err := d.take(1)
	if err != nil {
		return "", err
	}
	s, err := d.take(int(b[0]) + 1)
	if err != nil {
		return "", err
	}
	return string(s[:len(s)-1]), nil
}

func (d *decoder) decodeAll(sig string) ([]any, error) {
	types, err := splitSig(sig)
	if err != nil {
		return nil, err
	}
	out := make([]any, 0, len(types))
	for _, t := range types {
		v, err := d.decode(t, 0)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

func (d *decoder) decode(t string, depth int) (any, error) {
	if depth > maxDepth {
		return nil, errors.New("dbus: value nested too deeply")
	}
	switch t[0] {
	case 'y':
		b, err := d.take(1)
		if err != nil {
			return nil, err
		}
		return b[0], nil
	case 'b':
		n, err := d.u32()
		return n != 0, err
	case 'n', 'q':
		if err := d.align(2); err != nil {
			return nil, err
		}
		b, err := d.take(2)
		if err != nil {
			return nil, err
		}
		if t[0] == 'n' {
			return int16(d.ord.Uint16(b)), nil
		}
		return d.ord.Uint16(b), nil
	case 'i':
		n, err := d.u32()
		return int32(n), err
	case 'u':
		return d.u32()
	case 'x', 't', 'd':
		if err := d.align(8); err != nil {
			return nil, err
		}
		b, err := d.take(8)
		if err != nil {
			return nil, err
		}
		n := d.ord.Uint64(b)
		switch t[0] {
		case 'x':
			return int64(n), nil
		case 't':
			return n, nil
		}
		return math.Float64frombits(n), nil
	case 's':
		return d.str()
	case 'o':
		s, err := d.str()
		return ObjectPath(s), err
	case 'g':
		s, err := d.sig()
		return Signature(s), err
	case 'v':
		s, err := d.sig()
		if err != nil {
			return nil, err
		}
		if n, err := typeLen(s); err != nil || n != len(s) {
			return nil, fmt.Errorf("dbus: variant signature %q is not one complete type", s)
		}
		v, err := d.decode(s, depth+1)
		return Variant{Sig: s, Value: v}, err
	case 'a':
		return d.decodeArray(t, depth)
	case '(':
		inner, err := splitSig(t[1 : len(t)-1])
		if err != nil {
			return nil, err
		}
		if err := d.align(8); err != nil {
			return nil, err
		}
		out := make(Struct, 0, len(inner))
		for _, ft := range inner {
			v, err := d.decode(ft, depth+1)
			if err != nil {
				return nil, err
			}
			out = append(out, v)
		}
		return out, nil
	}
	return nil, fmt.Errorf("dbus: cannot decode type %q", t)
}

func (d *decoder) decodeArray(t string, depth int) (any, error) {
	n, err := d.u32()
	if err != nil {
		return nil, err
	}
	if n > 1<<26 {
		return nil, errors.New("dbus: array too large")
	}
	elem := t[1:]
	if err := d.align(alignOf(elem[0])); err != nil {
		return nil, err
	}
	if elem == "y" {
		b, err := d.take(int(n))
		return append([]byte(nil), b...), err
	}
	end := d.pos + int(n)
	if end > len(d.buf) {
		return nil, errShort
	}
	if elem[0] == '{' {
		inner, err := splitSig(elem[1 : len(elem)-1])
		if err != nil || len(inner) != 2 {
			return nil, fmt.Errorf("dbus: bad dict entry type %q", elem)
		}
		strKey := inner[0] == "s" || inner[0] == "o" || inner[0] == "g"
		ms := map[string]any{}
		ma := map[any]any{}
		for d.pos < end {
			if err := d.align(8); err != nil {
				return nil, err
			}
			k, err := d.decode(inner[0], depth+1)
			if err != nil {
				return nil, err
			}
			v, err := d.decode(inner[1], depth+1)
			if err != nil {
				return nil, err
			}
			if strKey {
				ms[fmt.Sprint(k)] = v
			} else {
				ma[k] = v
			}
		}
		if d.pos != end {
			return nil, errors.New("dbus: array length mismatch")
		}
		if strKey {
			return ms, nil
		}
		return ma, nil
	}
	var out []any
	for d.pos < end {
		v, err := d.decode(elem, depth+1)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	if d.pos != end {
		return nil, errors.New("dbus: array length mismatch")
	}
	switch elem {
	case "s":
		ss := make([]string, len(out))
		for i, v := range out {
			ss[i] = v.(string)
		}
		return ss, nil
	case "o":
		os := make([]ObjectPath, len(out))
		for i, v := range out {
			os[i] = v.(ObjectPath)
		}
		return os, nil
	}
	if out == nil {
		out = []any{}
	}
	return out, nil
}

var leOrder binary.ByteOrder = binary.LittleEndian
