package windows

import (
	"fmt"
	"strings"

	"github.com/kratejs/krate/packages/native/platform/caps"
)

// MessageBoxW has fixed buttons: one button maps to OK, two to Yes and No,
// three to Yes, No and Cancel, whatever the labels the app asked for. The
// labels must still reach the user, and screen readers read only the box, so
// the text names the mapping when a label differs from the system button.

var boxButtons = map[int][]string{
	1: {"OK"},
	2: {"Yes", "No"},
	3: {"Yes", "No", "Cancel"},
}

// messageBoxText is the text of the message box: the message, the detail, and
// a line saying which system button stands for which label when they differ.
func messageBoxText(o caps.MessageDialogOptions) string {
	text := o.Message
	if o.Detail != "" {
		text += "\n\n" + o.Detail
	}
	sys := boxButtons[len(o.Buttons)]
	var parts []string
	for i, label := range o.Buttons {
		if i >= len(sys) || strings.TrimSpace(label) == "" || strings.EqualFold(strings.TrimSpace(label), sys[i]) {
			continue
		}
		parts = append(parts, fmt.Sprintf("%s: %s", sys[i], label))
	}
	if len(parts) > 0 {
		text += "\n\n" + strings.Join(parts, ", ")
	}
	return text
}

// messageBoxTitle is the caption of the box. An empty caption would leave the
// box unnamed for assistive technology, so the app's name stands in.
func messageBoxTitle(o caps.MessageDialogOptions) string {
	if strings.TrimSpace(o.Title) != "" {
		return o.Title
	}
	return caps.AppDisplayName()
}

// messageBoxDefault is the 0-based default button the box can honor: 0 for
// the first, 1 for the second, 2 for the third. Out of range is the first.
func messageBoxDefault(o caps.MessageDialogOptions) int {
	n := len(o.Buttons)
	if n > 3 {
		n = 3
	}
	if o.Default > 0 && o.Default < n {
		return o.Default
	}
	return 0
}
