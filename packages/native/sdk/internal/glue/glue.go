// Package glue holds the small helpers that SDK registration code shares:
// argument reading with uniform "bad_args" errors, and result encoding.
// It is the hand-written equivalent of what generated glue does.
package glue

import (
	"errors"
	"strconv"

	"github.com/kratejs/krate/packages/native/krate"
	"github.com/kratejs/krate/packages/native/wire"
)

// Args reads the arguments array of one call.
type Args struct {
	d  *wire.Decoder
	fn string
	n  int
}

// Open starts reading the arguments of fn.
func Open(fn string, d *wire.Decoder) (*Args, error) {
	if err := d.BeginArray(); err != nil {
		return nil, krate.Errorf("bad_args", "%s: %w", fn, err)
	}
	return &Args{d: d, fn: fn}, nil
}

// More reports whether another argument follows.
func (a *Args) More() bool { return a.d.More() }

// Decoder exposes the underlying decoder for custom shapes.
func (a *Args) Decoder() *wire.Decoder { return a.d }

// Fail wraps err as a bad_args error for the argument just read.
func (a *Args) Fail(name string, err error) error {
	return krate.Errorf("bad_args", "%s: argument %d (%s): %w", a.fn, a.n-1, name, err)
}

func (a *Args) next(name string) error {
	if !a.d.More() {
		return krate.Errorf("bad_args", "%s: missing argument %d (%s)", a.fn, a.n, name)
	}
	a.n++
	return nil
}

// String reads a required string argument.
func (a *Args) String(name string) (string, error) {
	if err := a.next(name); err != nil {
		return "", err
	}
	s, err := a.d.String()
	if err != nil {
		return "", a.Fail(name, err)
	}
	return s, nil
}

// OptString reads an optional string argument; absent or null gives def.
func (a *Args) OptString(name, def string) (string, error) {
	if !a.d.More() {
		return def, nil
	}
	a.n++
	if a.d.IsNull() {
		return def, nil
	}
	s, err := a.d.String()
	if err != nil {
		return "", a.Fail(name, err)
	}
	return s, nil
}

// OptBool reads an optional boolean argument; absent or null gives false.
func (a *Args) OptBool(name string) (bool, error) {
	if !a.d.More() {
		return false, nil
	}
	a.n++
	if a.d.IsNull() {
		return false, nil
	}
	b, err := a.d.Bool()
	if err != nil {
		return false, a.Fail(name, err)
	}
	return b, nil
}

// Bytes reads a required binary argument and copies it out of the frame.
func (a *Args) Bytes(name string) ([]byte, error) {
	if err := a.next(name); err != nil {
		return nil, err
	}
	b, err := a.d.Bytes()
	if err != nil {
		return nil, a.Fail(name, err)
	}
	return append([]byte(nil), b...), nil
}

// Next marks a required argument as being read by the caller through
// Decoder, and fails when it is missing.
func (a *Args) Next(name string) error { return a.next(name) }

// Done checks that no extra arguments remain.
func (a *Args) Done() error {
	if err := a.d.EndArray(); err != nil {
		return krate.Errorf("bad_args", "%s: %w", a.fn, err)
	}
	return nil
}

// Scalar reads one database-style value: null, bool (as 0 or 1), integer,
// float, string, bytes or a date (as an RFC 3339 string). Objects and arrays
// are rejected.
func Scalar(d *wire.Decoder) (any, error) {
	switch d.Kind() {
	case wire.KindNull:
		d.IsNull()
		return nil, nil
	case wire.KindBool:
		b, err := d.Bool()
		if b {
			return int64(1), err
		}
		return int64(0), err
	case wire.KindString:
		return d.String()
	case wire.KindBytes:
		b, err := d.Bytes()
		return append([]byte(nil), b...), err
	case wire.KindTime:
		t, err := d.Time()
		return t.Format("2006-01-02T15:04:05.000Z"), err
	case wire.KindNumber:
		raw, err := d.RawValue()
		if err != nil {
			return nil, err
		}
		if i, perr := strconv.ParseInt(string(raw), 10, 64); perr == nil {
			return i, nil
		}
		f, perr := strconv.ParseFloat(string(raw), 64)
		if perr != nil {
			return nil, perr
		}
		return f, nil
	}
	return nil, krate.Errorf("bad_args", "expected null, boolean, number, string or bytes")
}

// Code maps an SDK error to one the UI can act on: ErrUnsupported becomes
// code "unsupported"; everything else passes through.
func Code(err error) error {
	if err == nil {
		return nil
	}
	if ke, ok := err.(*krate.Error); ok {
		return ke
	}
	if isUnsupported(err) {
		return krate.Errorf("unsupported", "%w", err)
	}
	return err
}

func isUnsupported(err error) bool { return errors.Is(err, krate.ErrUnsupported) }
