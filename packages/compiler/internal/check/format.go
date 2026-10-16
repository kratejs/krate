package check

import (
	"fmt"
	"os"
	"strings"
)

// ANSI color codes, matching the compiler's build output palette.
const (
	ansiReset   = "\033[0m"
	ansiBold    = "\033[1m"
	ansiDim     = "\033[90m"
	ansiRed     = "\033[31m"
	ansiBrightR = "\033[91m"
	ansiYellow  = "\033[33m"
	ansiCyan    = "\033[36m"
)

// FormatOptions controls how a findings report is rendered.
type FormatOptions struct {
	// Color enables ANSI styling. Use DefaultFormatOptions to auto-detect.
	Color bool
}

// DefaultFormatOptions enables color unless the environment opts out. This
// matches the rest of the Krate CLI, which always emits ANSI styling; NO_COLOR
// (or TERM=dumb) disables it for logs and dumb terminals.
func DefaultFormatOptions() FormatOptions {
	return FormatOptions{Color: colorEnabled()}
}

// colorEnabled reports whether ANSI color should be emitted.
func colorEnabled() bool {
	if os.Getenv("NO_COLOR") != "" || os.Getenv("KRATE_NO_COLOR") != "" {
		return false
	}
	return !strings.EqualFold(os.Getenv("TERM"), "dumb")
}

// Format renders the report with auto-detected color.
func Format(fs []Finding) string {
	return FormatWith(fs, DefaultFormatOptions())
}

// FormatWith renders findings grouped by route, aligned by rule ID, with an
// optional colorized severity marker. Style is a no-op when opts.Color is false
// so CI logs stay clean.
func FormatWith(fs []Finding, opts FormatOptions) string {
	if len(fs) == 0 {
		return ""
	}
	st := styler{color: opts.Color}

	ruleWidth := 0
	for _, f := range fs {
		if n := len([]rune(f.Rule)); n > ruleWidth {
			ruleWidth = n
		}
	}

	var b strings.Builder
	lastRoute := ""
	for _, f := range fs {
		if f.Route != lastRoute {
			if lastRoute != "" {
				b.WriteByte('\n')
			}
			b.WriteString("  ")
			b.WriteString(st.bold(st.cyan(f.Route)))
			b.WriteByte('\n')
			lastRoute = f.Route
		}

		mark, markColor := "!", st.yellow
		if f.Severity == Error {
			mark, markColor = "✗", st.brightRed
		}
		rule := st.dim(f.Rule + strings.Repeat(" ", ruleWidth-len([]rune(f.Rule))))

		fmt.Fprintf(&b, "    %s %s  %s\n", markColor(mark), rule, f.Message)
		if f.Hint != "" {
			fmt.Fprintf(&b, "        %s %s\n", st.dim("↳"), st.dim("hint: "+f.Hint))
		}
	}
	return b.String()
}

// Summary renders a colorized one-line tally.
func Summary(errors, warnings int, opts FormatOptions) string {
	st := styler{color: opts.Color}
	parts := []string{
		st.brightRed(fmt.Sprintf("%d error(s)", errors)),
		st.yellow(fmt.Sprintf("%d warning(s)", warnings)),
	}
	return strings.Join(parts, st.dim(" · "))
}

// styler applies ANSI codes when enabled.
type styler struct{ color bool }

func (s styler) wrap(code, v string) string {
	if !s.color {
		return v
	}
	return code + v + ansiReset
}

func (s styler) bold(v string) string      { return s.wrap(ansiBold, v) }
func (s styler) dim(v string) string       { return s.wrap(ansiDim, v) }
func (s styler) brightRed(v string) string { return s.wrap(ansiBrightR, v) }
func (s styler) yellow(v string) string    { return s.wrap(ansiYellow, v) }
func (s styler) cyan(v string) string      { return s.wrap(ansiCyan, v) }
