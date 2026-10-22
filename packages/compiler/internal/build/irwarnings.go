package build

import (
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/kratejs/krate/packages/compiler/internal/bundler"

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

// moduleWarningsSeen dedupes module warnings within a build: a shared module
// is bundled once per page but its warnings should print once.
var moduleWarningsSeen = struct {
	mu   sync.Mutex
	seen map[string]bool
}{seen: map[string]bool{}}

func resetModuleWarnings() {
	moduleWarningsSeen.mu.Lock()
	moduleWarningsSeen.seen = map[string]bool{}
	moduleWarningsSeen.mu.Unlock()
}

// printModuleWarnings prints the coded warnings attached to bundled modules
// (React rewrite diagnostics), each once per build.
func printModuleWarnings(w io.Writer, mods []*bundler.Module) {
	var out []diag.Diagnostic
	moduleWarningsSeen.mu.Lock()
	for _, m := range mods {
		for _, d := range m.Diagnostics {
			key := d.Error()
			if moduleWarningsSeen.seen[key] {
				continue
			}
			moduleWarningsSeen.seen[key] = true
			out = append(out, d)
		}
	}
	moduleWarningsSeen.mu.Unlock()
	printTreeWarnings(w, out)
}
