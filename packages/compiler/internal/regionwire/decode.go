package regionwire

import (
	"bufio"
	"encoding/binary"
	"io"
	"slices"
)

// Reader decodes frames from a stream.
type Reader struct {
	r   *bufio.Reader
	hdr [4]byte
	buf []byte
}

// NewReader returns a Reader over r.
func NewReader(r io.Reader) *Reader {
	return &Reader{r: bufio.NewReaderSize(r, 4096)}
}

// Next returns the next frame. It returns io.EOF when the stream ends cleanly
// between frames, io.ErrUnexpectedEOF when it ends inside one, and an error
// wrapping ErrCorrupt when a frame does not parse. Strings of the frame share
// one copy of its bytes.
func (d *Reader) Next() (Frame, error) {
	if _, err := io.ReadFull(d.r, d.hdr[:]); err != nil {
		return Frame{}, err // EOF, or ErrUnexpectedEOF inside the length
	}
	n := int(binary.BigEndian.Uint32(d.hdr[:]))
	if n > MaxFrame || n < 2 {
		return Frame{}, ErrCorrupt
	}
	body, err := d.read(n)
	if err != nil {
		return Frame{}, err
	}
	return decodeBody(string(body))
}

// read returns n bytes. A large claimed length is read in growing pieces, so a
// corrupt length on a short stream does not allocate it up front.
func (d *Reader) read(n int) ([]byte, error) {
	const chunk = 1 << 20
	if n <= chunk {
		if cap(d.buf) < n {
			d.buf = make([]byte, n)
		}
		b := d.buf[:n]
		if _, err := io.ReadFull(d.r, b); err != nil {
			return nil, unexpected(err)
		}
		return b, nil
	}
	b := make([]byte, 0, chunk)
	for len(b) < n {
		step := min(n-len(b), max(len(b), chunk))
		old := len(b)
		b = slices.Grow(b, step)[:old+step]
		if _, err := io.ReadFull(d.r, b[old:]); err != nil {
			return nil, unexpected(err)
		}
	}
	return b, nil
}

func unexpected(err error) error {
	if err == io.EOF {
		return io.ErrUnexpectedEOF
	}
	return err
}

// Decode parses one frame body (the bytes after the length prefix).
func decodeBody(s string) (Frame, error) {
	p := parser{s: s}
	var f Frame
	code := p.byte()
	flags := p.byte()
	f.Type = typeName(code)
	if f.Type == "" || flags&^(flagNotFound|flagNoStore|flagErrorCaught|flagFatal|flagHasVary|flagVaryFull) != 0 {
		return Frame{}, ErrCorrupt
	}
	f.NotFound = flags&flagNotFound != 0
	f.NoStore = flags&flagNoStore != 0
	f.ErrorCaught = flags&flagErrorCaught != 0
	f.Fatal = flags&flagFatal != 0
	f.Status = p.int()
	f.Denied = p.int()
	f.Count = p.int()
	f.ID = p.str()
	f.Kind = p.str()
	f.CacheStatus = p.str()
	f.Redirect = p.str()
	f.Error = p.str()
	f.Digest = p.str()
	f.Stack = p.str()
	f.Title = p.str()
	f.Head = p.str()
	f.HTML = p.str()
	if flags&flagHasVary != 0 {
		v := &VaryLists{Full: flags&flagVaryFull != 0}
		v.Headers = p.list()
		v.Cookies = p.list()
		v.Query = p.list()
		v.Params = p.list()
		f.Vary = v
	}
	if p.bad || p.i != len(p.s) {
		return Frame{}, ErrCorrupt
	}
	return f, nil
}

// Decode parses one frame body, the bytes after the length prefix.
func Decode(body []byte) (Frame, error) { return decodeBody(string(body)) }

type parser struct {
	s   string
	i   int
	bad bool
}

func (p *parser) byte() byte {
	if p.i >= len(p.s) {
		p.bad = true
		return 0
	}
	b := p.s[p.i]
	p.i++
	return b
}

func (p *parser) uvarint() uint64 {
	var x uint64
	var shift uint
	for k := 0; k < 10; k++ {
		if p.i >= len(p.s) {
			break
		}
		b := p.s[p.i]
		p.i++
		x |= uint64(b&0x7f) << shift
		if b < 0x80 {
			return x
		}
		shift += 7
	}
	p.bad = true
	return 0
}

func (p *parser) int() int {
	v := p.uvarint()
	if v > 1<<31 {
		p.bad = true
		return 0
	}
	return int(v)
}

func (p *parser) str() string {
	n := p.uvarint()
	if p.bad || n > uint64(len(p.s)-p.i) {
		p.bad = true
		return ""
	}
	s := p.s[p.i : p.i+int(n)]
	p.i += int(n)
	return s
}

func (p *parser) list() []string {
	n := p.uvarint()
	// Every string takes at least one byte, which bounds a corrupt count.
	if p.bad || n > uint64(len(p.s)-p.i) {
		p.bad = true
		return nil
	}
	if n == 0 {
		return nil
	}
	out := make([]string, 0, n)
	for ; n > 0 && !p.bad; n-- {
		out = append(out, p.str())
	}
	return out
}
