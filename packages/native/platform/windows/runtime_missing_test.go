package windows

import (
	"strings"
	"testing"
)

func TestRuntimeDialogText(t *testing.T) {
	text, caption := runtimeDialogText("Tasks", true, "")
	if caption != "Tasks" || !strings.Contains(text, webview2DownloadPage) || !strings.Contains(text, "not installed") {
		t.Errorf("missing runtime text = %q, %q", text, caption)
	}
	text, _ = runtimeDialogText("", false, "HRESULT 0x80004005")
	if !strings.Contains(text, "This app") || !strings.Contains(text, "could not start") || !strings.Contains(text, "0x80004005") || !strings.Contains(text, webview2DownloadPage) {
		t.Errorf("broken runtime text = %q", text)
	}
}

func TestRuntimeErrorMessages(t *testing.T) {
	m := (&runtimeError{missing: true}).Error()
	if !strings.Contains(m, "not installed") || !strings.Contains(m, webview2DownloadPage) {
		t.Errorf("missing: %q", m)
	}
	b := (&runtimeError{detail: " [x]"}).Error()
	if !strings.Contains(b, "could not be started") || !strings.HasSuffix(b, "[x]") {
		t.Errorf("broken: %q", b)
	}
	if !runtimeMissingHRESULT(0x80070002) || runtimeMissingHRESULT(0x80004005) {
		t.Error("runtimeMissingHRESULT")
	}
}
