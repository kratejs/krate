package login

import "strings"

// windowsCommandLine builds the command line for a Run value, quoting like
// CommandLineToArgvW expects.
func windowsCommandLine(exe string, args []string) string {
	parts := []string{winQuote(exe, true)}
	for _, a := range args {
		parts = append(parts, winQuote(a, false))
	}
	return strings.Join(parts, " ")
}

func winQuote(a string, always bool) string {
	if !always && a != "" && !strings.ContainsAny(a, " \t\"") {
		return a
	}
	var b strings.Builder
	b.WriteByte('"')
	slashes := 0
	for _, r := range a {
		switch r {
		case '\\':
			slashes++
			b.WriteRune(r)
		case '"':
			b.WriteString(strings.Repeat(`\`, slashes+1))
			b.WriteRune(r)
			slashes = 0
		default:
			slashes = 0
			b.WriteRune(r)
		}
	}
	b.WriteString(strings.Repeat(`\`, slashes))
	b.WriteByte('"')
	return b.String()
}
