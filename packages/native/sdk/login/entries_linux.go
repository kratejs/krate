package login

import "strings"

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
