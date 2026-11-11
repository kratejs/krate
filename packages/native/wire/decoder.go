package wire

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"io"
	"strconv"
	"time"
	"unicode/utf16"
	"unicode/utf8"
)

var replacementChar = []byte("\xef\xbf\xbd")

// Default limits.
const (
	DefaultMaxFrame = 256 << 20
	DefaultMaxDepth = 512
)

// Error is a decode error. Offset is a byte offset into the frame (the JSON
// section starts at offset 4).
type Error struct {
	Offset int
	Msg    string
}

func (e *Error) Error() string {
	return fmt.Sprintf("wire: decode at byte %d: %s", e.Offset, e.Msg)
}

type config struct {
	maxFrame int
	maxDepth int
}

// Option adjusts decoding limits.
type Option func(*config)

// WithMaxFrame limits the total size of a frame in bytes.
func WithMaxFrame(n int) Option { return func(c *config) { c.maxFrame = n } }

// WithMaxDepth limits JSON nesting depth.
func WithMaxDepth(n int) Option { return func(c *config) { c.maxDepth = n } }

func newConfig(opts []Option) config {
	c := config{maxFrame: DefaultMaxFrame, maxDepth: DefaultMaxDepth}
	for _, o := range opts {
		o(&c)
	}
	return c
}

// Kind classifies the next value without consuming it.
type Kind uint8

// Value kinds. KindBytes and KindTime are the tagged objects {"$b":..} and
// {"$d":..}; every other object, including {"$m":..} and {"$s":..}, is
// KindObject.
const (
	KindInvalid Kind = iota
	KindObject
	KindArray
	KindString
	KindNumber
	KindBool
	KindNull
	KindBytes
	KindTime
)

const (
	stStart = iota // just after the opening bracket
	stAfter        // a member was read
	stComma        // a comma was consumed, a member must follow
	stKey          // objects: a key was read, its value must follow
)

type decFrame struct {
	obj     bool
	state   uint8
	wrapped bool // object was wrapped as {"$b$": object}
}

// Decoder pulls values out of one frame.
type Decoder struct {
	src      []byte
	pos      int
	base     int
	blobs    [][]byte
	stack    []decFrame
	root     bool
	maxDepth int
	err      error
}

// NewDecoder parses the frame envelope of frame and returns a Decoder over
// it. Bytes and RawValue results alias frame.
func NewDecoder(frame []byte, opts ...Option) (*Decoder, error) {
	c := newConfig(opts)
	if len(frame) > c.maxFrame {
		return nil, &Error{Msg: fmt.Sprintf("frame of %d bytes exceeds limit %d", len(frame), c.maxFrame)}
	}
	if len(frame) < 4 {
		return nil, &Error{Msg: "short frame header"}
	}
	n := int(binary.BigEndian.Uint32(frame))
	if n < 0 || n > len(frame)-4 || len(frame)-4-n < 2 {
		return nil, &Error{Offset: 0, Msg: "json length exceeds frame"}
	}
	d := &Decoder{src: frame[4 : 4+n], base: 4, maxDepth: c.maxDepth}
	off := 4 + n
	count := int(binary.BigEndian.Uint16(frame[off:]))
	off += 2
	for i := 0; i < count; i++ {
		if len(frame)-off < 4 {
			return nil, &Error{Offset: off, Msg: "short blob header"}
		}
		m := int(binary.BigEndian.Uint32(frame[off:]))
		off += 4
		if m < 0 || m > len(frame)-off {
			return nil, &Error{Offset: off - 4, Msg: "blob length exceeds frame"}
		}
		d.blobs = append(d.blobs, frame[off:off+m:off+m])
		off += m
	}
	if off != len(frame) {
		return nil, &Error{Offset: off, Msg: "trailing bytes after last blob"}
	}
	return d, nil
}

// ReadFrame reads one frame from r. It returns io.EOF if r is at a clean end
// before the first byte of a frame.
func ReadFrame(r io.Reader, opts ...Option) (*Decoder, error) {
	c := newConfig(opts)
	var hdr [4]byte
	if n, err := io.ReadFull(r, hdr[:]); err != nil {
		if n == 0 && err == io.EOF {
			return nil, io.EOF
		}
		return nil, fmt.Errorf("wire: read frame header: %w", unexpected(err))
	}
	n := int(binary.BigEndian.Uint32(hdr[:]))
	used := 4 + n + 2
	if n < 0 || used > c.maxFrame {
		return nil, &Error{Msg: fmt.Sprintf("frame json of %d bytes exceeds limit %d", n, c.maxFrame)}
	}
	js, err := readN(r, n)
	if err != nil {
		return nil, fmt.Errorf("wire: read frame json: %w", unexpected(err))
	}
	var cnt [2]byte
	if _, err := io.ReadFull(r, cnt[:]); err != nil {
		return nil, fmt.Errorf("wire: read blob count: %w", unexpected(err))
	}
	d := &Decoder{src: js, base: 4, maxDepth: c.maxDepth}
	count := int(binary.BigEndian.Uint16(cnt[:]))
	for i := 0; i < count; i++ {
		var lb [4]byte
		if _, err := io.ReadFull(r, lb[:]); err != nil {
			return nil, fmt.Errorf("wire: read blob header: %w", unexpected(err))
		}
		m := int(binary.BigEndian.Uint32(lb[:]))
		used += 4 + m
		if m < 0 || used > c.maxFrame {
			return nil, &Error{Msg: fmt.Sprintf("frame exceeds limit %d", c.maxFrame)}
		}
		b, err := readN(r, m)
		if err != nil {
			return nil, fmt.Errorf("wire: read blob: %w", unexpected(err))
		}
		d.blobs = append(d.blobs, b)
	}
	return d, nil
}

func unexpected(err error) error {
	if err == io.EOF {
		return io.ErrUnexpectedEOF
	}
	return err
}

// readN reads exactly n bytes without trusting n for the allocation size.
func readN(r io.Reader, n int) ([]byte, error) {
	if n <= 1<<20 {
		b := make([]byte, n)
		_, err := io.ReadFull(r, b)
		return b, err
	}
	var buf bytes.Buffer
	_, err := io.CopyN(&buf, r, int64(n))
	return buf.Bytes(), err
}

// ReadFrames calls fn for every frame in r until a clean EOF. It returns the
// first error from reading or from fn.
func ReadFrames(r io.Reader, fn func(*Decoder) error, opts ...Option) error {
	for {
		d, err := ReadFrame(r, opts...)
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		if err := fn(d); err != nil {
			return err
		}
	}
}

// Blobs returns the number of blobs in the frame.
func (d *Decoder) Blobs() int { return len(d.blobs) }

// Offset returns the current byte offset in the frame.
func (d *Decoder) Offset() int { return d.base + d.pos }

// Err returns the first decode error.
func (d *Decoder) Err() error { return d.err }

func (d *Decoder) fail(format string, args ...any) error {
	if d.err == nil {
		d.err = &Error{Offset: d.base + d.pos, Msg: fmt.Sprintf(format, args...)}
	}
	return d.err
}

func (d *Decoder) skipWS() {
	for d.pos < len(d.src) {
		switch d.src[d.pos] {
		case ' ', '\t', '\r', '\n':
			d.pos++
		default:
			return
		}
	}
}

func (d *Decoder) peek() byte {
	d.skipWS()
	if d.pos >= len(d.src) {
		return 0
	}
	return d.src[d.pos]
}

// beginValue checks that a value may start here and consumes a separating
// comma inside arrays.
func (d *Decoder) beginValue() error {
	if d.err != nil {
		return d.err
	}
	if len(d.stack) == 0 {
		if d.root {
			return d.fail("unexpected second root value")
		}
		return nil
	}
	t := &d.stack[len(d.stack)-1]
	if t.obj {
		if t.state != stKey {
			return d.fail("expected an object key, not a value")
		}
		return nil
	}
	if t.state == stAfter {
		if d.peek() != ',' {
			return d.fail("expected ',' or ']'")
		}
		d.pos++
		t.state = stComma
	}
	return nil
}

func (d *Decoder) endValue() {
	if len(d.stack) == 0 {
		d.root = true
		return
	}
	d.stack[len(d.stack)-1].state = stAfter
}

// Kind peeks at the next value. It returns KindInvalid at the end of a
// container or on malformed input.
func (d *Decoder) Kind() Kind {
	if d.err != nil {
		return KindInvalid
	}
	if len(d.stack) > 0 {
		t := d.stack[len(d.stack)-1]
		if t.obj && t.state != stKey {
			return KindInvalid
		}
	}
	save := d.pos
	defer func() { d.pos = save }()
	if len(d.stack) > 0 {
		t := &d.stack[len(d.stack)-1]
		if !t.obj && t.state == stAfter {
			if d.peek() != ',' {
				return KindInvalid
			}
			d.pos++
		}
	}
	switch c := d.peek(); {
	case c == '"':
		return KindString
	case c == '[':
		return KindArray
	case c == 't' || c == 'f':
		return KindBool
	case c == 'n':
		return KindNull
	case c == '-' || (c >= '0' && c <= '9'):
		return KindNumber
	case c == '{':
		rest := d.src[d.pos+1:]
		rest = bytes.TrimLeft(rest, " \t\r\n")
		switch {
		case bytes.HasPrefix(rest, []byte(`"$b":`)):
			return KindBytes
		case bytes.HasPrefix(rest, []byte(`"$d":`)):
			return KindTime
		}
		return KindObject
	}
	return KindInvalid
}

// BeginObject consumes '{'. An object wrapped as {"$b$": object} is
// unwrapped transparently.
func (d *Decoder) BeginObject() error {
	if err := d.beginValue(); err != nil {
		return err
	}
	if d.peek() != '{' {
		return d.fail("expected an object")
	}
	if len(d.stack) >= d.maxDepth {
		return d.fail("nesting deeper than %d", d.maxDepth)
	}
	d.pos++
	f := decFrame{obj: true}
	// Look for the {"$b$": {...}} escape.
	p := d.pos
	for p < len(d.src) && isWS(d.src[p]) {
		p++
	}
	if bytes.HasPrefix(d.src[p:], []byte(`"$b$"`)) {
		p += 5
		for p < len(d.src) && isWS(d.src[p]) {
			p++
		}
		if p < len(d.src) && d.src[p] == ':' {
			p++
			for p < len(d.src) && isWS(d.src[p]) {
				p++
			}
			if p < len(d.src) && d.src[p] == '{' {
				d.pos = p + 1
				f.wrapped = true
			}
		}
	}
	d.stack = append(d.stack, f)
	return nil
}

func isWS(c byte) bool { return c == ' ' || c == '\t' || c == '\r' || c == '\n' }

// More reports whether another member follows in the current container.
func (d *Decoder) More() bool {
	if d.err != nil || len(d.stack) == 0 {
		return false
	}
	t := &d.stack[len(d.stack)-1]
	closer := byte(']')
	if t.obj {
		closer = '}'
	}
	c := d.peek()
	switch t.state {
	case stStart:
		return d.pos < len(d.src) && c != closer
	case stAfter:
		if c == ',' {
			d.pos++
			t.state = stComma
			return true
		}
		return false
	case stComma:
		return true
	}
	return false
}

// Key reads the next object key and its colon.
func (d *Decoder) Key() (string, error) {
	if d.err != nil {
		return "", d.err
	}
	if len(d.stack) == 0 || !d.stack[len(d.stack)-1].obj {
		return "", d.fail("Key outside an object")
	}
	t := &d.stack[len(d.stack)-1]
	if t.state == stKey {
		return "", d.fail("key read twice; read or skip the value first")
	}
	if t.state == stAfter {
		if d.peek() != ',' {
			return "", d.fail("expected ',' or '}'")
		}
		d.pos++
	}
	if d.peek() != '"' {
		return "", d.fail("expected an object key")
	}
	k, err := d.parseString()
	if err != nil {
		return "", err
	}
	if d.peek() != ':' {
		return "", d.fail("expected ':' after key")
	}
	d.pos++
	t.state = stKey
	return k, nil
}

// EndObject consumes '}'. All members must have been read or skipped.
func (d *Decoder) EndObject() error {
	if d.err != nil {
		return d.err
	}
	if len(d.stack) == 0 || !d.stack[len(d.stack)-1].obj {
		return d.fail("EndObject outside an object")
	}
	t := d.stack[len(d.stack)-1]
	if t.state == stKey || t.state == stComma {
		return d.fail("object ends where a member was expected")
	}
	if d.peek() != '}' {
		return d.fail("unread members; Skip them before EndObject")
	}
	d.pos++
	if t.wrapped {
		if d.peek() != '}' {
			return d.fail("expected '}' closing the $b$ escape")
		}
		d.pos++
	}
	d.stack = d.stack[:len(d.stack)-1]
	d.endValue()
	return nil
}

// BeginArray consumes '['.
func (d *Decoder) BeginArray() error {
	if err := d.beginValue(); err != nil {
		return err
	}
	if d.peek() != '[' {
		return d.fail("expected an array")
	}
	if len(d.stack) >= d.maxDepth {
		return d.fail("nesting deeper than %d", d.maxDepth)
	}
	d.pos++
	d.stack = append(d.stack, decFrame{})
	return nil
}

// EndArray consumes ']'. All elements must have been read or skipped.
func (d *Decoder) EndArray() error {
	if d.err != nil {
		return d.err
	}
	if len(d.stack) == 0 || d.stack[len(d.stack)-1].obj {
		return d.fail("EndArray outside an array")
	}
	if d.stack[len(d.stack)-1].state == stComma {
		return d.fail("array ends where an element was expected")
	}
	if d.peek() != ']' {
		return d.fail("unread elements; Skip them before EndArray")
	}
	d.pos++
	d.stack = d.stack[:len(d.stack)-1]
	d.endValue()
	return nil
}

// String reads a string value.
func (d *Decoder) String() (string, error) {
	if err := d.beginValue(); err != nil {
		return "", err
	}
	if d.peek() != '"' {
		return "", d.fail("expected a string")
	}
	s, err := d.parseString()
	if err != nil {
		return "", err
	}
	d.endValue()
	return s, nil
}

// parseString parses the string at d.pos (which is a quote).
func (d *Decoder) parseString() (string, error) {
	start := d.pos + 1
	i := start
	for i < len(d.src) {
		c := d.src[i]
		if c == '"' {
			s := d.src[start:i]
			d.pos = i + 1
			if utf8.Valid(s) {
				return string(s), nil
			}
			return string(bytes.ToValidUTF8(s, replacementChar)), nil
		}
		if c == '\\' {
			break
		}
		if c < 0x20 {
			d.pos = i
			return "", d.fail("control character in string")
		}
		i++
	}
	if i >= len(d.src) {
		d.pos = len(d.src)
		return "", d.fail("unterminated string")
	}
	// Slow path with escapes.
	out := make([]byte, 0, i-start+16)
	out = append(out, d.src[start:i]...)
	for i < len(d.src) {
		c := d.src[i]
		switch {
		case c == '"':
			d.pos = i + 1
			if utf8.Valid(out) {
				return string(out), nil
			}
			return string(bytes.ToValidUTF8(out, replacementChar)), nil
		case c < 0x20:
			d.pos = i
			return "", d.fail("control character in string")
		case c != '\\':
			out = append(out, c)
			i++
			continue
		}
		i++
		if i >= len(d.src) {
			break
		}
		switch e := d.src[i]; e {
		case '"', '\\', '/':
			out = append(out, e)
			i++
		case 'b':
			out = append(out, '\b')
			i++
		case 'f':
			out = append(out, '\f')
			i++
		case 'n':
			out = append(out, '\n')
			i++
		case 'r':
			out = append(out, '\r')
			i++
		case 't':
			out = append(out, '\t')
			i++
		case 'u':
			r, ok := hex4(d.src, i+1)
			if !ok {
				d.pos = i
				return "", d.fail("bad \\u escape")
			}
			i += 5
			if utf16.IsSurrogate(r) {
				if i+1 < len(d.src) && d.src[i] == '\\' && d.src[i+1] == 'u' {
					if r2, ok := hex4(d.src, i+2); ok {
						if dec := utf16.DecodeRune(r, r2); dec != utf8.RuneError {
							r = dec
							i += 6
						} else {
							r = utf8.RuneError
						}
					} else {
						r = utf8.RuneError
					}
				} else {
					r = utf8.RuneError
				}
			}
			out = utf8.AppendRune(out, r)
		default:
			d.pos = i
			return "", d.fail("bad escape \\%c", e)
		}
	}
	d.pos = len(d.src)
	return "", d.fail("unterminated string")
}

func hex4(b []byte, i int) (rune, bool) {
	if i+4 > len(b) {
		return 0, false
	}
	var r rune
	for _, c := range b[i : i+4] {
		switch {
		case c >= '0' && c <= '9':
			r = r<<4 | rune(c-'0')
		case c >= 'a' && c <= 'f':
			r = r<<4 | rune(c-'a'+10)
		case c >= 'A' && c <= 'F':
			r = r<<4 | rune(c-'A'+10)
		default:
			return 0, false
		}
	}
	return r, true
}

// scanNumber returns the end of the JSON number starting at i, or -1.
func scanNumber(b []byte, i int) int {
	if i < len(b) && b[i] == '-' {
		i++
	}
	if i >= len(b) {
		return -1
	}
	switch {
	case b[i] == '0':
		i++
	case b[i] >= '1' && b[i] <= '9':
		for i < len(b) && b[i] >= '0' && b[i] <= '9' {
			i++
		}
	default:
		return -1
	}
	if i < len(b) && b[i] == '.' {
		i++
		j := i
		for i < len(b) && b[i] >= '0' && b[i] <= '9' {
			i++
		}
		if i == j {
			return -1
		}
	}
	if i < len(b) && (b[i] == 'e' || b[i] == 'E') {
		i++
		if i < len(b) && (b[i] == '+' || b[i] == '-') {
			i++
		}
		j := i
		for i < len(b) && b[i] >= '0' && b[i] <= '9' {
			i++
		}
		if i == j {
			return -1
		}
	}
	return i
}

// number consumes a number token and returns its text.
func (d *Decoder) number() (string, error) {
	if err := d.beginValue(); err != nil {
		return "", err
	}
	d.skipWS()
	end := scanNumber(d.src, d.pos)
	if end < 0 {
		return "", d.fail("expected a number")
	}
	tok := string(d.src[d.pos:end])
	d.pos = end
	d.endValue()
	return tok, nil
}

// Int reads an integer.
func (d *Decoder) Int() (int64, error) {
	start := d.pos
	tok, err := d.number()
	if err != nil {
		return 0, err
	}
	v, perr := strconv.ParseInt(tok, 10, 64)
	if perr != nil {
		d.pos = start
		return 0, d.fail("%q is not an int64", tok)
	}
	return v, nil
}

// Uint reads a non-negative integer.
func (d *Decoder) Uint() (uint64, error) {
	start := d.pos
	tok, err := d.number()
	if err != nil {
		return 0, err
	}
	v, perr := strconv.ParseUint(tok, 10, 64)
	if perr != nil {
		d.pos = start
		return 0, d.fail("%q is not a uint64", tok)
	}
	return v, nil
}

// Float reads a number as float64.
func (d *Decoder) Float() (float64, error) {
	start := d.pos
	tok, err := d.number()
	if err != nil {
		return 0, err
	}
	v, perr := strconv.ParseFloat(tok, 64)
	if perr != nil {
		d.pos = start
		return 0, d.fail("%q is not a float64", tok)
	}
	return v, nil
}

func (d *Decoder) literal(lit string) bool {
	d.skipWS()
	if !bytes.HasPrefix(d.src[d.pos:], []byte(lit)) {
		return false
	}
	if n := d.pos + len(lit); n < len(d.src) {
		if c := d.src[n]; c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' {
			return false
		}
	}
	d.pos += len(lit)
	return true
}

// Bool reads a boolean.
func (d *Decoder) Bool() (bool, error) {
	if err := d.beginValue(); err != nil {
		return false, err
	}
	switch {
	case d.literal("true"):
		d.endValue()
		return true, nil
	case d.literal("false"):
		d.endValue()
		return false, nil
	}
	return false, d.fail("expected a boolean")
}

// IsNull consumes a null if one is next and reports whether it did.
func (d *Decoder) IsNull() bool {
	if d.Kind() != KindNull {
		return false
	}
	if d.beginValue() != nil || !d.literal("null") {
		return false
	}
	d.endValue()
	return true
}

// Bytes reads a blob reference {"$b": i}, or a base64 string. The result
// aliases the frame; copy it to retain it beyond the frame's lifetime.
func (d *Decoder) Bytes() ([]byte, error) {
	if err := d.beginValue(); err != nil {
		return nil, err
	}
	switch d.peek() {
	case '"':
		s, err := d.parseString()
		if err != nil {
			return nil, err
		}
		b, err := base64.StdEncoding.DecodeString(s)
		if err != nil {
			b, err = base64.RawStdEncoding.DecodeString(s)
		}
		if err != nil {
			return nil, d.fail("bad base64: %v", err)
		}
		d.endValue()
		return b, nil
	case '{':
		if err := d.openTag("$b"); err != nil {
			return nil, err
		}
		idx, err := d.tagUint()
		if err != nil {
			return nil, err
		}
		if idx >= uint64(len(d.blobs)) {
			return nil, d.fail("blob index %d out of range (%d blobs)", idx, len(d.blobs))
		}
		if err := d.closeTag(); err != nil {
			return nil, err
		}
		d.endValue()
		return d.blobs[idx], nil
	}
	return nil, d.fail("expected bytes")
}

// Time reads {"$d": RFC 3339 string} (or a bare RFC 3339 string) as UTC.
func (d *Decoder) Time() (time.Time, error) {
	if err := d.beginValue(); err != nil {
		return time.Time{}, err
	}
	tagged := d.peek() == '{'
	if tagged {
		if err := d.openTag("$d"); err != nil {
			return time.Time{}, err
		}
	}
	if d.peek() != '"' {
		return time.Time{}, d.fail("expected a time string")
	}
	at := d.pos
	s, err := d.parseString()
	if err != nil {
		return time.Time{}, err
	}
	t, perr := time.Parse(time.RFC3339Nano, s)
	if perr != nil {
		d.pos = at
		return time.Time{}, d.fail("bad time %q", s)
	}
	if tagged {
		if err := d.closeTag(); err != nil {
			return time.Time{}, err
		}
	}
	d.endValue()
	return t.UTC(), nil
}

// openTag consumes {"tag": and leaves the value to the caller.
func (d *Decoder) openTag(tag string) error {
	d.pos++ // '{'
	if d.peek() != '"' {
		return d.fail("expected %q", tag)
	}
	k, err := d.parseString()
	if err != nil {
		return err
	}
	if k != tag {
		return d.fail("expected key %q, got %q", tag, k)
	}
	if d.peek() != ':' {
		return d.fail("expected ':'")
	}
	d.pos++
	return nil
}

func (d *Decoder) closeTag() error {
	if d.peek() != '}' {
		return d.fail("unexpected extra members in tagged value")
	}
	d.pos++
	return nil
}

func (d *Decoder) tagUint() (uint64, error) {
	d.skipWS()
	end := scanNumber(d.src, d.pos)
	if end < 0 {
		return 0, d.fail("expected a number")
	}
	v, err := strconv.ParseUint(string(d.src[d.pos:end]), 10, 64)
	if err != nil {
		return 0, d.fail("bad blob index")
	}
	d.pos = end
	return v, nil
}

// Skip consumes the next value, whatever it is.
func (d *Decoder) Skip() error {
	_, err := d.RawValue()
	return err
}

// RawValue consumes the next value and returns its JSON text. The result
// aliases the frame; blob references inside it stay indexes into this frame.
func (d *Decoder) RawValue() ([]byte, error) {
	if err := d.beginValue(); err != nil {
		return nil, err
	}
	d.skipWS()
	start := d.pos
	if err := d.skipValue(len(d.stack)); err != nil {
		return nil, err
	}
	d.endValue()
	return d.src[start:d.pos], nil
}

// SubDecoder consumes the next value and returns a Decoder positioned at its
// start, sharing this frame's blobs. Use it to hand one call's arguments to
// another goroutine.
func (d *Decoder) SubDecoder() (*Decoder, error) {
	d.skipWS()
	base := d.base + d.pos
	raw, err := d.RawValue()
	if err != nil {
		return nil, err
	}
	return &Decoder{src: raw, base: base, blobs: d.blobs, maxDepth: d.maxDepth}, nil
}

// Finish checks that the root value was read and nothing follows it.
func (d *Decoder) Finish() error {
	if d.err != nil {
		return d.err
	}
	if len(d.stack) != 0 || !d.root {
		return d.fail("value not fully read")
	}
	if d.peek() != 0 || d.pos < len(d.src) {
		return d.fail("trailing data after the value")
	}
	return nil
}

// skipValue validates and consumes one value; depth is the current nesting.
func (d *Decoder) skipValue(depth int) error {
	if depth >= d.maxDepth {
		return d.fail("nesting deeper than %d", d.maxDepth)
	}
	switch c := d.peek(); {
	case c == '"':
		_, err := d.parseString()
		return err
	case c == '{':
		d.pos++
		if d.peek() == '}' {
			d.pos++
			return nil
		}
		for {
			if d.peek() != '"' {
				return d.fail("expected an object key")
			}
			if _, err := d.parseString(); err != nil {
				return err
			}
			if d.peek() != ':' {
				return d.fail("expected ':' after key")
			}
			d.pos++
			if err := d.skipValue(depth + 1); err != nil {
				return err
			}
			switch d.peek() {
			case ',':
				d.pos++
			case '}':
				d.pos++
				return nil
			default:
				return d.fail("expected ',' or '}'")
			}
		}
	case c == '[':
		d.pos++
		if d.peek() == ']' {
			d.pos++
			return nil
		}
		for {
			if err := d.skipValue(depth + 1); err != nil {
				return err
			}
			switch d.peek() {
			case ',':
				d.pos++
			case ']':
				d.pos++
				return nil
			default:
				return d.fail("expected ',' or ']'")
			}
		}
	case c == 't':
		if d.literal("true") {
			return nil
		}
	case c == 'f':
		if d.literal("false") {
			return nil
		}
	case c == 'n':
		if d.literal("null") {
			return nil
		}
	case c == '-' || (c >= '0' && c <= '9'):
		if end := scanNumber(d.src, d.pos); end >= 0 {
			d.pos = end
			return nil
		}
	}
	return d.fail("unexpected input")
}
