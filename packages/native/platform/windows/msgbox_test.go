package windows

import (
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/native/platform/caps"
)

func TestMessageBoxTextNamesTheMapping(t *testing.T) {
	got := messageBoxText(caps.MessageDialogOptions{Message: "Save changes?", Detail: "Lost otherwise.", Buttons: []string{"Save", "Discard", "Cancel"}})
	if !strings.HasPrefix(got, "Save changes?\n\nLost otherwise.") {
		t.Errorf("message and detail: %q", got)
	}
	if !strings.HasSuffix(got, "Yes: Save, No: Discard") {
		t.Errorf("the mapping line is missing: %q", got)
	}
	if strings.Contains(got, "Cancel: Cancel") {
		t.Errorf("a label equal to the system button is repeated: %q", got)
	}
	// Standard labels need no explanation.
	if got := messageBoxText(caps.MessageDialogOptions{Message: "Sure?", Buttons: []string{"yes", "No"}}); got != "Sure?" {
		t.Errorf("standard labels: %q", got)
	}
	// A single custom button is OK.
	if got := messageBoxText(caps.MessageDialogOptions{Message: "Done", Buttons: []string{"Continue"}}); got != "Done\n\nOK: Continue" {
		t.Errorf("one button: %q", got)
	}
	if got := messageBoxText(caps.MessageDialogOptions{Message: "Done"}); got != "Done" {
		t.Errorf("no buttons: %q", got)
	}
}

func TestMessageBoxTitleAndDefault(t *testing.T) {
	t.Setenv("KRATE_APP_NAME", "Krate Notes")
	if got := messageBoxTitle(caps.MessageDialogOptions{}); got != "Krate Notes" {
		t.Errorf("empty title: %q", got)
	}
	if got := messageBoxTitle(caps.MessageDialogOptions{Title: "Oops"}); got != "Oops" {
		t.Errorf("title: %q", got)
	}
	three := []string{"a", "b", "c"}
	for _, c := range []struct{ def, want int }{{0, 0}, {1, 1}, {2, 2}, {3, 0}, {-1, 0}} {
		if got := messageBoxDefault(caps.MessageDialogOptions{Buttons: three, Default: c.def}); got != c.want {
			t.Errorf("default %d: %d, want %d", c.def, got, c.want)
		}
	}
	if got := messageBoxDefault(caps.MessageDialogOptions{Buttons: []string{"a"}, Default: 1}); got != 0 {
		t.Errorf("default beyond one button: %d", got)
	}
}
