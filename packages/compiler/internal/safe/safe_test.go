package safe

import (
	"errors"
	"os"
	"testing"
)

func TestRunRecoversPanic(t *testing.T) {
	if os.Getenv("KRATE_DEBUG") != "" {
		t.Skip("KRATE_DEBUG set: panics propagate by design")
	}
	err := Run(func() error { panic("boom") })
	if err == nil {
		t.Fatal("expected an error from a panicking function")
	}
	var pe *PanicError
	if !errors.As(err, &pe) {
		t.Fatalf("want *PanicError, got %T", err)
	}
}

func TestRunPassesThroughError(t *testing.T) {
	want := errors.New("expected")
	if err := Run(func() error { return want }); !errors.Is(err, want) {
		t.Fatalf("error not passed through: %v", err)
	}
	if err := Run(func() error { return nil }); err != nil {
		t.Fatalf("nil error not preserved: %v", err)
	}
}

func TestGuardRecoversPanic(t *testing.T) {
	if os.Getenv("KRATE_DEBUG") != "" {
		t.Skip("KRATE_DEBUG set: panics propagate by design")
	}
	var got error
	Guard(func() { panic("guard") }, func(err error) { got = err })
	if got == nil {
		t.Fatal("expected onPanic to be called")
	}
}
