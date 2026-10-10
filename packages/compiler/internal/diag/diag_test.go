package diag

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestDiagnosticError(t *testing.T) {
	d := Diagnostic{File: "a.tsx", Line: 3, Col: 5, Message: "boom"}
	if got, want := d.Error(), "a.tsx:3:5: boom"; got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
	// Without a file, the location still renders line:col.
	d = Diagnostic{Line: 1, Col: 2, Message: "x"}
	if got, want := d.Error(), "1:2: x"; got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

func TestFromError(t *testing.T) {
	// A wrapped Diagnostic is unwrapped and reported as structured.
	d := Diagnostic{File: "a.tsx", Line: 1, Col: 1, Message: "m"}
	wrapped := fmt.Errorf("stage: %w", d)
	got, ok := FromError(wrapped)
	if !ok || got.Message != "m" {
		t.Errorf("FromError(wrapped) = %+v, ok=%v", got, ok)
	}

	// A plain error becomes a message-only Diagnostic, ok=false.
	plain := errors.New("plain")
	got, ok = FromError(plain)
	if ok || got.Message != "plain" {
		t.Errorf("FromError(plain) = %+v, ok=%v", got, ok)
	}

	if _, ok := FromError(nil); ok {
		t.Error("FromError(nil) must report ok=false")
	}
}

func TestFormatDiagnostics(t *testing.T) {
	errs := []error{
		New("a.tsx", 2, 3, "bad thing", "fix it", "let x = ;"),
		errors.New("bare error"),
	}
	out := FormatDiagnostics(errs)
	if !strings.Contains(out, "a.tsx:2:3: bad thing") {
		t.Errorf("missing location line:\n%s", out)
	}
	if !strings.Contains(out, "let x = ;") {
		t.Errorf("missing source line:\n%s", out)
	}
	// Caret is indented to the column (col 3 -> 2 spaces then ^).
	if !strings.Contains(out, "\n  ^") {
		t.Errorf("missing caret at column:\n%s", out)
	}
	if !strings.Contains(out, "hint: fix it") {
		t.Errorf("missing hint:\n%s", out)
	}
	if !strings.Contains(out, "bare error") {
		t.Errorf("missing bare error:\n%s", out)
	}
}
