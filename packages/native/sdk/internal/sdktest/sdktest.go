// Package sdktest drives a scheme.Registry through the real call endpoint, so
// SDK tests see exactly what the UI would.
package sdktest

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kratejs/krate/packages/native/scheme"
	"github.com/kratejs/krate/packages/native/wire"
)

// Result is one call's reply.
type Result struct {
	// OK is the JSON of the result when the call succeeded; OKDec reads it
	// (blobs resolve).
	OK    string
	OKDec *wire.Decoder
	// Err fields are set when the call failed.
	ErrCode, ErrMessage, ErrData string
	Failed                       bool
}

// Caller sends calls to a registry.
type Caller struct {
	t testing.TB
	h http.Handler
}

// New returns a Caller for reg.
func New(t testing.TB, reg *scheme.Registry) *Caller {
	return &Caller{t: t, h: scheme.New(scheme.Options{Registry: reg})}
}

// Call invokes fn with the arguments written by args (the elements of the
// arguments array; may be nil).
func (c *Caller) Call(fn string, args func(*wire.Encoder)) Result {
	c.t.Helper()
	return c.CallFrom("test", fn, args)
}

// CallFrom is Call made by the window with this id (the x-krate-window header).
func (c *Caller) CallFrom(window, fn string, args func(*wire.Encoder)) Result {
	c.t.Helper()
	req := wire.NewEncoder()
	req.BeginObject()
	req.Key("calls")
	req.BeginArray()
	req.BeginObject()
	req.Key("id")
	req.Int(1)
	req.Key("fn")
	req.String(fn)
	req.Key("args")
	req.BeginArray()
	if args != nil {
		args(req)
	}
	req.EndArray()
	req.EndObject()
	req.EndArray()
	req.EndObject()
	frame, err := req.Finish()
	if err != nil {
		c.t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/__krate/call", bytes.NewReader(frame))
	r.Header.Set("x-krate-window", window)
	c.h.ServeHTTP(rec, r)
	d, err := wire.ReadFrame(rec.Body)
	if err != nil {
		c.t.Fatalf("reading the reply of %s: %v (status %d)", fn, err, rec.Code)
	}
	var res Result
	if err := d.BeginObject(); err != nil {
		c.t.Fatal(err)
	}
	for d.More() {
		k, _ := d.Key()
		switch k {
		case "ok":
			sub, err := d.SubDecoder()
			if err != nil {
				c.t.Fatal(err)
			}
			res.OKDec = sub
			cp := *sub // a fresh decoder is still at its start, so a copy is independent
			raw, _ := cp.RawValue()
			res.OK = string(raw)
		case "err":
			res.Failed = true
			if err := d.BeginObject(); err != nil {
				c.t.Fatal(err)
			}
			for d.More() {
				ek, _ := d.Key()
				switch ek {
				case "code":
					res.ErrCode, _ = d.String()
				case "message":
					res.ErrMessage, _ = d.String()
				case "data":
					raw, _ := d.RawValue()
					res.ErrData = string(raw)
				default:
					_ = d.Skip()
				}
			}
			_ = d.EndObject()
		default:
			_ = d.Skip()
		}
	}
	return res
}
