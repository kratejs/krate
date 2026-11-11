// Package desktopentry generates the freedesktop .desktop launcher of an app.
// It has no dependencies so that the compiler can use it when packaging;
// package sdk/app re-exports it for host code.
package desktopentry

import (
	"fmt"
	"sort"
	"strings"
)

// Options describes the Linux launcher entry of an app.
type Options struct {
	// Name is the display name.
	Name string
	// Exec is the command line without arguments, e.g. "/opt/acme/acme".
	Exec string
	// Icon is an icon name or absolute path.
	Icon string
	// Comment is a one-line description.
	Comment string
	// Schemes are the URL schemes the app handles ("acme" for acme://...).
	Schemes []string
	// Categories such as "Utility;" (default "Utility;").
	Categories string
	// StartupWMClass should match the window class (the app id).
	StartupWMClass string
}

// Entry returns the text of a freedesktop .desktop file that launches
// the app and registers it as the handler of its URL schemes
// (MimeType=x-scheme-handler/<scheme>). Install it as
// ~/.local/share/applications/<app id>.desktop (or /usr/share/applications)
// and run update-desktop-database. When schemes are set Exec gets a %u
// placeholder so the URL arrives as the last argument.
func Entry(o Options) string {
	var b strings.Builder
	esc := func(s string) string {
		s = strings.ReplaceAll(s, "\\", "\\\\")
		s = strings.ReplaceAll(s, "\n", "\\n")
		return s
	}
	b.WriteString("[Desktop Entry]\nType=Application\nVersion=1.0\n")
	fmt.Fprintf(&b, "Name=%s\n", esc(o.Name))
	if o.Comment != "" {
		fmt.Fprintf(&b, "Comment=%s\n", esc(o.Comment))
	}
	exec := quoteExec(o.Exec)
	if len(o.Schemes) > 0 {
		exec += " %u"
	}
	fmt.Fprintf(&b, "Exec=%s\n", exec)
	if o.Icon != "" {
		fmt.Fprintf(&b, "Icon=%s\n", esc(o.Icon))
	}
	cat := o.Categories
	if cat == "" {
		cat = "Utility;"
	}
	fmt.Fprintf(&b, "Categories=%s\n", cat)
	if o.StartupWMClass != "" {
		fmt.Fprintf(&b, "StartupWMClass=%s\n", esc(o.StartupWMClass))
	}
	if len(o.Schemes) > 0 {
		s := append([]string(nil), o.Schemes...)
		sort.Strings(s)
		var mimes []string
		for _, sc := range s {
			mimes = append(mimes, "x-scheme-handler/"+strings.ToLower(sc))
		}
		fmt.Fprintf(&b, "MimeType=%s;\n", strings.Join(mimes, ";"))
	}
	b.WriteString("Terminal=false\n")
	return b.String()
}

// quoteExec quotes a program path for the Exec key when it has spaces.
func quoteExec(p string) string {
	if !strings.ContainsAny(p, " \t\"'\\<>~|&;$*?#()`") {
		return p
	}
	r := strings.NewReplacer("\\", "\\\\", "\"", "\\\"", "`", "\\`", "$", "\\$")
	return "\"" + r.Replace(p) + "\""
}
