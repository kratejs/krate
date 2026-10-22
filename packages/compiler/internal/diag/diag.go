// Package diag provides the unified compiler diagnostic type and formatting.
// Every error that can point at a source location should use Diagnostic so
// users see a consistent `file:line:col: message\n source\n ^` shape.
package diag

import (
	"errors"
	"fmt"
	"strings"
)

// Diagnostic is a single source-located diagnostic (parse error, bundler
// error, etc.). It satisfies error so it can be returned from any stage.
type Diagnostic struct {
	File    string `json:"file,omitempty"`
	Line    int    `json:"line,omitempty"`
	Col     int    `json:"col,omitempty"`
	Message string `json:"message"`
	Hint    string `json:"hint,omitempty"`
	Source  string `json:"source,omitempty"` // the offending source line, for caret rendering
	// Severity classifies the diagnostic. The zero value means an error, so
	// existing producers keep their meaning.
	Severity Severity `json:"severity,omitempty"`
}

// Severity is the seriousness of a Diagnostic.
type Severity string

const (
	// SeverityError fails the build. It is also the zero value.
	SeverityError Severity = ""
	// SeverityWarning is reported but does not fail the build.
	SeverityWarning Severity = "warning"
)

// IsWarning reports whether the diagnostic is a non-fatal warning.
func (d Diagnostic) IsWarning() bool { return d.Severity == SeverityWarning }

// FromError extracts a Diagnostic from an error, unwrapping as needed. Non-
// Diagnostic errors are converted to a Diagnostic carrying only the message, so
// every build error can be surfaced to the browser overlay as structured data.
func FromError(err error) (Diagnostic, bool) {
	if err == nil {
		return Diagnostic{}, false
	}
	var d Diagnostic
	if !errors.As(err, &d) {
		return Diagnostic{Message: err.Error()}, false
	}
	return d, true
}

func (d Diagnostic) Error() string {
	loc := ""
	if d.File != "" {
		loc = d.File + ":"
	}
	return fmt.Sprintf("%s%d:%d: %s", loc, d.Line, d.Col, d.Message)
}

// New builds a Diagnostic from a source line for caret rendering.
func New(file string, line, col int, message, hint, source string) Diagnostic {
	return Diagnostic{File: file, Line: line, Col: col, Message: message, Hint: hint, Source: source}
}

// FormatDiagnostics renders a list of errors. Diagnostics render with a source
// line and a caret; other errors render with their Error() string.
func FormatDiagnostics(errs []error) string {
	var b strings.Builder
	for i, err := range errs {
		if i > 0 {
			b.WriteString("\n")
		}
		d, ok := err.(Diagnostic)
		if !ok {
			b.WriteString(err.Error())
			continue
		}
		loc := ""
		if d.File != "" {
			loc = d.File + ":"
		}
		fmt.Fprintf(&b, "%s%d:%d: %s", loc, d.Line, d.Col, d.Message)
		if d.Source != "" {
			b.WriteString("\n")
			b.WriteString(d.Source)
			b.WriteString("\n")
			b.WriteString(caretPrefix(d.Source, d.Col))
			b.WriteString("^")
		}
		if d.Hint != "" {
			b.WriteString("\n  hint: ")
			b.WriteString(d.Hint)
		}
	}
	return b.String()
}

// caretPrefix returns the padding that places a caret under column col of
// line. Columns are 1-based UTF-16 code units (an astral-plane character
// counts as two); tabs in the line are copied so the caret lines up in a
// terminal.
func caretPrefix(line string, col int) string {
	var b strings.Builder
	units := 0
	for _, r := range line {
		if units >= col-1 {
			break
		}
		if r == '\t' {
			b.WriteByte('\t')
		} else {
			b.WriteByte(' ')
		}
		if r >= 0x10000 {
			units += 2
		} else {
			units++
		}
	}
	for units < col-1 {
		b.WriteByte(' ')
		units++
	}
	return b.String()
}
