package build

import (
	"fmt"
	"os"
	"strings"

	"github.com/kratejs/krate/packages/compiler/internal/diag"
)

// passErrors collects the errors of build passes that run before page
// bundling (server-action validation, metadata routes), so one build reports
// every failing pass instead of stopping at the first. Page bundling already
// aggregates; BuildAll seeds its failure list from this.
type passErrors struct {
	errs []error
}

// add prints err under label and records it. A joined error is split so each
// diagnostic is printed and counted on its own.
func (p *passErrors) add(label string, err error) {
	for _, e := range flattenErrors(err) {
		fmt.Fprintf(os.Stderr, "  %s%s:%s %s\n", cRed, label, cReset, strings.ReplaceAll(diag.FormatDiagnostics([]error{e}), "\n", "\n    "))
		p.errs = append(p.errs, e)
	}
}

// finish returns the error an early exit should report: the collected errors,
// followed by cause (the error that stopped the build, if any).
func (p *passErrors) finish(cause error) error {
	if len(p.errs) == 0 {
		return cause
	}
	errs := p.errs
	if cause != nil {
		errs = append(append([]error(nil), errs...), cause)
	}
	return newMultiError(errs)
}

// flattenErrors expands joined errors (errors.Join, multiError) into leaves.
func flattenErrors(err error) []error {
	if err == nil {
		return nil
	}
	switch u := err.(type) {
	case interface{ Unwrap() []error }:
		var out []error
		for _, e := range u.Unwrap() {
			out = append(out, flattenErrors(e)...)
		}
		return out
	}
	return []error{err}
}

// newMultiError builds the aggregate build error for errs.
func newMultiError(errs []error) *multiError {
	msgs := make([]string, len(errs))
	for i, e := range errs {
		msgs[i] = "  " + e.Error()
	}
	return &multiError{summary: fmt.Sprintf("build failed: %d error(s):\n%s", len(errs), strings.Join(msgs, "\n")), errs: errs}
}
