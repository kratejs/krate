package wire

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"slices"
	"strconv"
	"time"
	"unicode/utf8"
)

// MaxBlobs is the most blobs one frame can carry (the count is a u16).
const MaxBlobs = 1<<16 - 1

type encFrame struct {
	obj        bool
	n          int    // members written so far
	keyPending bool   // objects: a key was written and awaits its value
	start      int    // offset of the opening bracket in buf
	wrap       string // objects: wrapper key if the first key is a reserved tag or escape
}

// Encoder builds one frame. It is append-only; the first misuse (a value
// without a key, an unbalanced End, NaN, ...) is remembered and returned by
// Err, Finish and WriteTo, and later calls are no-ops.
type Encoder struct {
	buf   []byte
	blobs [][]byte
	stack []encFrame
	root  bool
	err   error
}

// NewEncoder returns an empty Encoder.
func NewEncoder() *Encoder { return &Encoder{} }

// Reset clears the encoder for reuse and keeps its buffers.
func (e *Encoder) Reset() {
	e.buf = e.buf[:0]
	clear(e.blobs)
	e.blobs = e.blobs[:0]
	e.stack = e.stack[:0]
	e.root = false
	e.err = nil
}

// Err returns the first error recorded.
func (e *Encoder) Err() error { return e.err }

func (e *Encoder) fail(format string, args ...any) {
	if e.err == nil {
		e.err = fmt.Errorf("wire: encode: "+format, args...)
	}
}

func (e *Encoder) beginValue() bool {
	if e.err != nil {
		return false
	}
	if len(e.stack) == 0 {
		if e.root {
			e.fail("a frame holds exactly one root value")
			return false
		}
		return true
	}
	t := &e.stack[len(e.stack)-1]
	if t.obj {
		if !t.keyPending {
			e.fail("object value without a key")
			return false
		}
		t.keyPending = false
		return true
	}
	if t.n > 0 {
		e.buf = append(e.buf, ',')
	}
	return true
}

func (e *Encoder) endValue() {
	if len(e.stack) == 0 {
		e.root = true
		return
	}
	t := &e.stack[len(e.stack)-1]
	if !t.obj {
		t.n++
	}
}

// BeginObject opens an object.
func (e *Encoder) BeginObject() {
	if !e.beginValue() {
		return
	}
	e.stack = append(e.stack, encFrame{obj: true, start: len(e.buf)})
	e.buf = append(e.buf, '{')
}

// EndObject closes the current object. An object whose only key is a reserved
// tag ("$b", "$d", "$m", "$s") is wrapped as {"<tag>$": object} so it cannot be
// mistaken for a tagged value; one whose only key is already "<tag>$" is
// wrapped under that same key.
func (e *Encoder) EndObject() {
	if e.err != nil {
		return
	}
	if len(e.stack) == 0 || !e.stack[len(e.stack)-1].obj {
		e.fail("EndObject without BeginObject")
		return
	}
	t := e.stack[len(e.stack)-1]
	if t.keyPending {
		e.fail("object key without a value")
		return
	}
	e.stack = e.stack[:len(e.stack)-1]
	e.buf = append(e.buf, '}')
	if t.wrap != "" && t.n == 1 {
		e.buf = slices.Insert(e.buf, t.start, []byte(`{"`+t.wrap+`":`)...)
		e.buf = append(e.buf, '}')
	}
	e.endValue()
}

// BeginArray opens an array.
func (e *Encoder) BeginArray() {
	if !e.beginValue() {
		return
	}
	e.stack = append(e.stack, encFrame{start: len(e.buf)})
	e.buf = append(e.buf, '[')
}

// EndArray closes the current array.
func (e *Encoder) EndArray() {
	if e.err != nil {
		return
	}
	if len(e.stack) == 0 || e.stack[len(e.stack)-1].obj {
		e.fail("EndArray without BeginArray")
		return
	}
	e.stack = e.stack[:len(e.stack)-1]
	e.buf = append(e.buf, ']')
	e.endValue()
}

// Key writes an object key. The next call must write its value.
func (e *Encoder) Key(k string) {
	if e.err != nil {
		return
	}
	if len(e.stack) == 0 || !e.stack[len(e.stack)-1].obj {
		e.fail("Key outside an object")
		return
	}
	t := &e.stack[len(e.stack)-1]
	if t.keyPending {
		e.fail("two keys in a row")
		return
	}
	if t.n > 0 {
		e.buf = append(e.buf, ',')
	}
	if t.n == 0 {
		t.wrap = wrapKey(k)
	}
	t.n++
	t.keyPending = true
	e.buf = appendString(e.buf, k)
	e.buf = append(e.buf, ':')
}

// wrapKey returns the wrapper key for an object whose first key is k, or "".
func wrapKey(k string) string {
	switch k {
	case "$b", "$d", "$m", "$s":
		return k + "$"
	case "$b$", "$d$", "$m$", "$s$":
		return k
	}
	return ""
}

// String writes a string value.
func (e *Encoder) String(s string) {
	if !e.beginValue() {
		return
	}
	e.buf = appendString(e.buf, s)
	e.endValue()
}

// Int writes an integer.
func (e *Encoder) Int(v int64) {
	if !e.beginValue() {
		return
	}
	e.buf = strconv.AppendInt(e.buf, v, 10)
	e.endValue()
}

// Uint writes an unsigned integer.
func (e *Encoder) Uint(v uint64) {
	if !e.beginValue() {
		return
	}
	e.buf = strconv.AppendUint(e.buf, v, 10)
	e.endValue()
}

// Float writes a float. NaN and infinities are an error.
func (e *Encoder) Float(v float64) {
	if e.err != nil {
		return
	}
	if math.IsNaN(v) || math.IsInf(v, 0) {
		e.fail("cannot encode %v", v)
		return
	}
	if !e.beginValue() {
		return
	}
	e.buf = appendFloat(e.buf, v)
	e.endValue()
}

// Bool writes a boolean.
func (e *Encoder) Bool(v bool) {
	if !e.beginValue() {
		return
	}
	e.buf = strconv.AppendBool(e.buf, v)
	e.endValue()
}

// Null writes null.
func (e *Encoder) Null() {
	if !e.beginValue() {
		return
	}
	e.buf = append(e.buf, "null"...)
	e.endValue()
}

// Bytes adds b to the frame's blob list and writes {"$b": index}. The slice
// is retained, not copied, until the frame is built; do not modify it before.
func (e *Encoder) Bytes(b []byte) {
	if e.err != nil {
		return
	}
	if len(e.blobs) >= MaxBlobs {
		e.fail("more than %d blobs in one frame", MaxBlobs)
		return
	}
	if !e.beginValue() {
		return
	}
	e.buf = append(e.buf, `{"$b":`...)
	e.buf = strconv.AppendInt(e.buf, int64(len(e.blobs)), 10)
	e.buf = append(e.buf, '}')
	e.blobs = append(e.blobs, b)
	e.endValue()
}

// Time writes {"$d": RFC 3339 UTC with millisecond precision}.
func (e *Encoder) Time(t time.Time) {
	if e.err != nil {
		return
	}
	t = t.UTC()
	if y := t.Year(); y < 0 || y > 9999 {
		e.fail("time year %d out of range", y)
		return
	}
	if !e.beginValue() {
		return
	}
	e.buf = append(e.buf, `{"$d":"`...)
	e.buf = t.AppendFormat(e.buf, "2006-01-02T15:04:05.000Z")
	e.buf = append(e.buf, `"}`...)
	e.endValue()
}

// Raw writes already-encoded JSON as one value. The caller guarantees it is
// valid; blob references inside it are not remapped.
func (e *Encoder) Raw(json []byte) {
	if len(json) == 0 {
		e.fail("empty raw value")
		return
	}
	if !e.beginValue() {
		return
	}
	e.buf = append(e.buf, json...)
	e.endValue()
}

// Finish returns the complete frame, or the first error.
func (e *Encoder) Finish() ([]byte, error) {
	if e.err != nil {
		return nil, e.err
	}
	if len(e.stack) != 0 || !e.root {
		return nil, errors.New("wire: encode: incomplete frame")
	}
	size := 4 + len(e.buf) + 2
	for _, b := range e.blobs {
		size += 4 + len(b)
	}
	out := make([]byte, 0, size)
	out = binary.BigEndian.AppendUint32(out, uint32(len(e.buf)))
	out = append(out, e.buf...)
	out = binary.BigEndian.AppendUint16(out, uint16(len(e.blobs)))
	for _, b := range e.blobs {
		out = binary.BigEndian.AppendUint32(out, uint32(len(b)))
		out = append(out, b...)
	}
	return out, nil
}

// Frame returns the complete frame, or nil if the encoder is in error or
// incomplete; use Finish to see why.
func (e *Encoder) Frame() []byte {
	f, _ := e.Finish()
	return f
}

// WriteTo writes the frame with a single Write call.
func (e *Encoder) WriteTo(w io.Writer) (int64, error) {
	f, err := e.Finish()
	if err != nil {
		return 0, err
	}
	n, err := w.Write(f)
	return int64(n), err
}

const hexDigits = "0123456789abcdef"

// appendString quotes s like encoding/json without HTML escaping.
func appendString(dst []byte, s string) []byte {
	dst = append(dst, '"')
	start := 0
	for i := 0; i < len(s); {
		if b := s[i]; b < utf8.RuneSelf {
			if b >= 0x20 && b != '"' && b != '\\' {
				i++
				continue
			}
			dst = append(dst, s[start:i]...)
			switch b {
			case '\\', '"':
				dst = append(dst, '\\', b)
			case '\b':
				dst = append(dst, '\\', 'b')
			case '\f':
				dst = append(dst, '\\', 'f')
			case '\n':
				dst = append(dst, '\\', 'n')
			case '\r':
				dst = append(dst, '\\', 'r')
			case '\t':
				dst = append(dst, '\\', 't')
			default:
				dst = append(dst, '\\', 'u', '0', '0', hexDigits[b>>4], hexDigits[b&0xF])
			}
			i++
			start = i
			continue
		}
		c, size := utf8.DecodeRuneInString(s[i:])
		if c == utf8.RuneError && size == 1 {
			dst = append(dst, s[start:i]...)
			dst = append(dst, `\ufffd`...)
			i += size
			start = i
			continue
		}
		if c == 0x2028 || c == 0x2029 {
			dst = append(dst, s[start:i]...)
			dst = append(dst, '\\', 'u', '2', '0', '2', hexDigits[c&0xF])
			i += size
			start = i
			continue
		}
		i += size
	}
	dst = append(dst, s[start:]...)
	return append(dst, '"')
}

// appendFloat formats like encoding/json.
func appendFloat(b []byte, f float64) []byte {
	format := byte('f')
	if abs := math.Abs(f); abs != 0 && (abs < 1e-6 || abs >= 1e21) {
		format = 'e'
	}
	b = strconv.AppendFloat(b, f, format, -1, 64)
	if format == 'e' {
		if n := len(b); n >= 4 && b[n-4] == 'e' && b[n-3] == '-' && b[n-2] == '0' {
			b[n-2] = b[n-1]
			b = b[:n-1]
		}
	}
	return b
}
