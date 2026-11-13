package login

import (
	"fmt"
	"html"
	"strings"
)

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
