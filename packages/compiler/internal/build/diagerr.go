package build

import (
	"regexp"
	"strconv"

	"github.com/kratejs/krate/packages/compiler/internal/diag"
)

// errLocRe finds a `path:line:col` location (Windows or POSIX) inside an error
// message whose underlying error carried no structured diagnostic. Source
// extensions are required so arbitrary `a:b:c` text is not misparsed.
var errLocRe = regexp.MustCompile(`((?:[A-Za-z]:)?[^\s]*\.(?:tsx|ts|jsx|js|mjs|cjs|mdx|md|css)):(\d+):(\d+)`)

// diagnosticFromText parses a `file:line:col` prefix out of a plain error
// string so build errors that were flattened to strings still surface a
// location (and therefore the overlay's "open in editor" action).
func diagnosticFromText(msg string) (diag.Diagnostic, bool) {
	m := errLocRe.FindStringSubmatch(msg)
	if m == nil {
		return diag.Diagnostic{}, false
	}
	line, _ := strconv.Atoi(m[2])
	col, _ := strconv.Atoi(m[3])
	return diag.Diagnostic{File: m[1], Line: line, Col: col, Message: msg}, true
}

// renderError preserves the individual diagnostics behind a render failure so
// the browser overlay can show structured file:line:col + source + hint instead
// of a single flattened string.
type renderError struct {
	page    string
	summary string
	errs    []error
}

func (e *renderError) Error() string { return e.summary }
func (e *renderError) Unwrap() []error {
	return e.errs
}

// multiError aggregates several independent build errors while keeping each
// one addressable for diagnostic extraction.
type multiError struct {
	summary string
	errs    []error
}

func (e *multiError) Error() string { return e.summary }
func (e *multiError) Unwrap() []error {
	return e.errs
}

// DiagnosticsFromError extracts structured diagnostics from a build error,
// unwrapping aggregates. Errors without positional information become a
// message-only diagnostic so nothing is lost.
func DiagnosticsFromError(err error) []diag.Diagnostic {
	if err == nil {
		return nil
	}
	var out []diag.Diagnostic
	var walk func(error)
	walk = func(e error) {
		if e == nil {
			return
		}
		if d, ok := e.(diag.Diagnostic); ok {
			out = append(out, d)
			return
		}
		if d, ok := e.(*diag.Diagnostic); ok {
			out = append(out, *d)
			return
		}
		if m, ok := e.(interface{ Unwrap() []error }); ok {
			for _, c := range m.Unwrap() {
				walk(c)
			}
			return
		}
		if s, ok := e.(interface{ Unwrap() error }); ok {
			if inner := s.Unwrap(); inner != nil {
				walk(inner)
				return
			}
		}
		if d, ok := diagnosticFromText(e.Error()); ok {
			out = append(out, d)
			return
		}
		out = append(out, diag.Diagnostic{Message: e.Error()})
	}
	walk(err)
	if len(out) == 0 {
		out = append(out, diag.Diagnostic{Message: err.Error()})
	}
	return out
}
