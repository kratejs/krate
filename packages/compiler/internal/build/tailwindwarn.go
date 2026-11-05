package build

import (
	"fmt"
	"os"

	"github.com/kratejs/krate/packages/compiler/internal/diag"
)

// warnTailwindOff warns, once per build, that Tailwind input exists while
// Tailwind is not enabled. file is the config file when that was the trigger.
func (b *Builder) warnTailwindOff(file, found string) {
	const key = "tailwind-off"
	b.twMu.Lock()
	if b.twWarned == nil {
		b.twWarned = map[string]bool{}
	}
	seen := b.twWarned[key]
	b.twWarned[key] = true
	b.twMu.Unlock()
	if seen {
		return
	}
	printWarningsOnce(os.Stderr, []diag.Diagnostic{{
		File:     file,
		Severity: diag.SeverityWarning,
		Code:     diag.CodeTailwindDisabled,
		Message:  fmt.Sprintf("%s found but Tailwind is not enabled, so no utility CSS is generated and Tailwind directives are removed", found),
		Hint:     "set `tailwind: { enabled: true }` in krate.config.ts, or remove the Tailwind config and directives if you do not use it",
	}})
}
