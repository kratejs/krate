// Package krate is the core API that Go host code in a Krate Native app uses:
// the call context, host signals and coded errors. Generated glue calls into
// it; user code imports it.
package krate

import (
	"context"
	"errors"
	"fmt"
)

// Ctx is the context every exported host function receives as its first
// parameter.
type Ctx interface {
	context.Context
	// Window is the id of the window that made the call. It is empty for
	// calls that do not come from a window.
	Window() string
}

type ctxImpl struct {
	context.Context
	window string
}

func (c ctxImpl) Window() string { return c.window }

// Background returns a Ctx for tests and build-time calls. It is never
// cancelled and has no window.
func Background() Ctx { return ctxImpl{Context: context.Background()} }

// WithWindow wraps ctx so that Window reports the given window id.
func WithWindow(ctx context.Context, window string) Ctx {
	return ctxImpl{Context: ctx, window: window}
}

// Error is a coded error that crosses the host boundary intact: the UI sees
// Code, Message and Data. Any other error reaches the UI with code "internal".
type Error struct {
	Code    string
	Message string
	// Data must be encodable by encoding/json. It is only used on the error
	// path, so no generated encoder is needed for it.
	Data any

	cause error
}

// Error implements error.
func (e *Error) Error() string {
	if e.Message == "" {
		return e.Code
	}
	return e.Code + ": " + e.Message
}

// Unwrap returns the error wrapped with %w by Errorf, if any.
func (e *Error) Unwrap() error { return e.cause }

// Errorf returns an *Error with the given code and a formatted message. It
// accepts %w like fmt.Errorf, so errors.Is and errors.As see the wrapped
// error, and errors.As finds the returned *Error.
func Errorf(code, format string, args ...any) error {
	cause := fmt.Errorf(format, args...)
	e := &Error{Code: code, Message: cause.Error()}
	// Keep the chain only when the format actually wrapped something.
	if errors.Unwrap(cause) != nil {
		e.cause = cause
	} else if m, ok := cause.(interface{ Unwrap() []error }); ok && len(m.Unwrap()) > 0 {
		e.cause = cause
	}
	return e
}
