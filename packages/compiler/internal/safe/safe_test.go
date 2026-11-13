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
