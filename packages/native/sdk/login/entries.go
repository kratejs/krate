package login

import (
	"fmt"
	"html"
	"strings"
)

// desktopQuote quotes one argument for an Exec line of a desktop entry
// (Desktop Entry Specification, "The Exec key").
func desktopQuote(a string) string {
	if a != "" && !strings.ContainsAny(a, " \t\n\"'\\><~|&;$*?#()`%") {
		return a
	}
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range a {
		switch r {
		case '"', '`', '$', '\\':
			b.WriteByte('\\')
			b.WriteRune(r)
		case '%':
			b.WriteString("%%")
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// autostartEntry is the text of an XDG autostart .desktop file.
func autostartEntry(name, exe string, args []string) string {
	parts := []string{desktopQuote(exe)}
	for _, a := range args {
		parts = append(parts, desktopQuote(a))
	}
	n := strings.NewReplacer("\n", " ", "\r", " ").Replace(name)
	return "[Desktop Entry]\nType=Application\nName=" + n + "\nExec=" + strings.Join(parts, " ") +
		"\nTerminal=false\nX-GNOME-Autostart-enabled=true\n"
}

// autostartEnabled reads an autostart entry: present, and neither Hidden nor
// disabled with X-GNOME-Autostart-enabled=false.
func autostartEnabled(text string) bool {
	for _, line := range strings.Split(text, "\n") {
		k, v, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok {
			continue
		}
		v = strings.TrimSpace(v)
		switch strings.TrimSpace(k) {
		case "Hidden":
			if strings.EqualFold(v, "true") {
				return false
			}
		case "X-GNOME-Autostart-enabled":
			if strings.EqualFold(v, "false") {
				return false
			}
		}
	}
	return true
}

// launchAgentPlist is the text of a LaunchAgent that runs the app at login.
func launchAgentPlist(label, exe string, args []string) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>`)
	b.WriteString(html.EscapeString(label))
	b.WriteString("</string>\n\t<key>ProgramArguments</key>\n\t<array>\n")
	for _, a := range append([]string{exe}, args...) {
		fmt.Fprintf(&b, "\t\t<string>%s</string>\n", html.EscapeString(a))
	}
	b.WriteString("\t</array>\n\t<key>RunAtLoad</key>\n\t<true/>\n</dict>\n</plist>\n")
	return b.String()
}

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
