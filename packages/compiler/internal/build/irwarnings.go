package build

import (
	"fmt"
	"io"
	"strings"

	"github.com/kratejs/krate/packages/compiler/internal/diag"
)

// printTreeWarnings writes IR build warnings (positioned diagnostics) to w as
// `Warning: file:line:col: message` followed by the indented source line,
// caret and hint.
func printTreeWarnings(w io.Writer, warnings []diag.Diagnostic) {
	for _, d := range warnings {
		text := diag.FormatDiagnostics([]error{d})
		text = strings.ReplaceAll(text, "\n", "\n    ")
		fmt.Fprintf(w, "  %sWarning:%s %s\n", cYellow, cReset, text)
	}
}
