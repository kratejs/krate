// Package codec has the small helpers the hand-written wire codecs of the SDK
// share: a reader with a sticky error and a few common shapes.
package codec

import (
	"github.com/kratejs/krate/packages/native/krate"
	"github.com/kratejs/krate/packages/native/wire"
)

// R reads call arguments. The first failure sticks; later reads return zero
// values, and Done reports the failure as a "bad_args" error.
type R struct {
	D    *wire.Decoder
	Name string // the function name, for messages
	err  error
	open bool
}

// Args begins the arguments array of a call.
func Args(d *wire.Decoder, name string) *R {
	r := &R{D: d, Name: name}
	r.fail(d.BeginArray())
	r.open = true
	return r
}

func (r *R) fail(err error) {
	if err != nil && r.err == nil {
		r.err = krate.Errorf("bad_args", "%s: %w", r.Name, err)
	}
}

// Failf records a validation failure.
func (r *R) Failf(format string, args ...any) {
	if r.err == nil {
		r.err = krate.Errorf("bad_args", r.Name+": "+format, args...)
	}
}

// More reports whether another argument follows.
func (r *R) More() bool { return r.err == nil && r.D.More() }

// String reads a string.
func (r *R) String() string {
	if r.err != nil {
		return ""
	}
	v, err := r.D.String()
	r.fail(err)
	return v
}

// Bool reads a boolean.
func (r *R) Bool() bool {
	if r.err != nil {
		return false
	}
	v, err := r.D.Bool()
	r.fail(err)
	return v
}

// Int reads an integer.
func (r *R) Int() int64 {
	if r.err != nil {
		return 0
	}
	v, err := r.D.Int()
	r.fail(err)
	return v
}

// Float reads a number.
func (r *R) Float() float64 {
	if r.err != nil {
		return 0
	}
	v, err := r.D.Float()
	r.fail(err)
	return v
}

// Bytes reads binary data.
func (r *R) Bytes() []byte {
	if r.err != nil {
		return nil
	}
	v, err := r.D.Bytes()
	r.fail(err)
	return v
}

// Strings reads an array of strings.
func (r *R) Strings() []string {
	if r.err != nil {
		return nil
	}
	if r.D.IsNull() {
		r.fail(r.D.Skip())
		return nil
	}
	r.fail(r.D.BeginArray())
	var out []string
	for r.err == nil && r.D.More() {
		out = append(out, r.String())
	}
	if r.err == nil {
		r.fail(r.D.EndArray())
	}
	return out
}

// Array reads an array, calling each for every element.
func (r *R) Array(each func()) {
	if r.err != nil {
		return
	}
	if r.D.IsNull() {
		r.fail(r.D.Skip())
		return
	}
	r.fail(r.D.BeginArray())
	for r.err == nil && r.D.More() {
		each()
	}
	if r.err == nil {
		r.fail(r.D.EndArray())
	}
}

// Object reads an object, calling field for every key. field must consume
// the value, or call Skip, and returns false for an unknown key. A null reads
// as an empty object.
func (r *R) Object(field func(key string) bool) {
	if r.err != nil {
		return
	}
	if r.D.IsNull() {
		r.fail(r.D.Skip())
		return
	}
	r.fail(r.D.BeginObject())
	for r.err == nil && r.D.More() {
		k, err := r.D.Key()
		if err != nil {
			r.fail(err)
			return
		}
		if !field(k) {
			r.fail(r.D.Skip())
		}
	}
	if r.err == nil {
		r.fail(r.D.EndObject())
	}
}

// Skip skips one value.
func (r *R) Skip() {
	if r.err == nil {
		r.fail(r.D.Skip())
	}
}

// Done ends the arguments array (rejecting extra arguments) and returns the
// first error.
func (r *R) Done() error {
	if r.err == nil && r.open {
		r.fail(r.D.EndArray())
	}
	return r.err
}

// Err returns the sticky error without finishing.
func (r *R) Err() error { return r.err }

// EncodeArgs builds a one-frame decoder over an arguments array written by fn.
func EncodeArgs(fn func(e *wire.Encoder)) (*wire.Decoder, error) {
	e := wire.NewEncoder()
	e.BeginArray()
	if fn != nil {
		fn(e)
	}
	e.EndArray()
	b, err := e.Finish()
	if err != nil {
		return nil, err
	}
	return wire.NewDecoder(b)
}

// Result runs fn into a fresh encoder and returns a decoder over the value.
func Result(fn func(out *wire.Encoder) error) (*wire.Decoder, error) {
	e := wire.NewEncoder()
	if err := fn(e); err != nil {
		return nil, err
	}
	b, err := e.Finish()
	if err != nil {
		return nil, krate.Errorf("internal", "encoding the result: %w", err)
	}
	return wire.NewDecoder(b)
}

// ReadStrings decodes an array of strings from d.
func ReadStrings(d *wire.Decoder) ([]string, error) {
	r := &R{D: d, Name: "result"}
	out := r.Strings()
	return out, r.Err()
}

// WriteStrings encodes a string slice as an array.
func WriteStrings(e *wire.Encoder, list []string) {
	e.BeginArray()
	for _, s := range list {
		e.String(s)
	}
	e.EndArray()
}
