package pkg

import (
	"strings"
	"testing"
)

func sandboxMeta(t *testing.T) Metadata {
	m := testMeta(t)
	m.Permissions = []string{"net", "notify", "tray", "os:secrets", "media", "dialog", "fs"}
	m.MediaUses = []string{"camera", "microphone"}
	m.Summary = "Take notes"
	m.LongDescription = "Acme Notes keeps your notes.\n\nIt works offline."
	m.License = "MIT"
	m.Homepage = "https://acme.example"
	return m
}

func TestFlatpakFinishArgsFollowPermissions(t *testing.T) {
	args := strings.Join(flatpakFinishArgs(sandboxMeta(t)), " ")
	for _, want := range []string{"--share=ipc", "--socket=wayland", "--socket=fallback-x11", "--device=dri", "--share=network",
		"--talk-name=org.freedesktop.Notifications", "--talk-name=org.kde.StatusNotifierWatcher", "--talk-name=org.freedesktop.secrets",
		"--filesystem=xdg-run/pipewire-0"} {
		if !strings.Contains(args, want) {
			t.Errorf("missing %s in %s", want, args)
		}
	}
	for _, bad := range []string{"--filesystem=home", "--filesystem=host", "--device=all", "GeoClue"} {
		if strings.Contains(args, bad) {
			t.Errorf("unexpected %s in %s", bad, args)
		}
	}
	// A quiet app asks for little.
	q := testMeta(t)
	q.Permissions = []string{"kv", "window"}
	qa := strings.Join(flatpakFinishArgs(q), " ")
	if strings.Contains(qa, "network") || strings.Contains(qa, "Notifications") || strings.Contains(qa, "StatusNotifier") {
		t.Errorf("a quiet app must not get network, notifications or tray: %s", qa)
	}
	q.Flatpak.FinishArgs = []string{"--filesystem=xdg-documents", "--share=ipc"}
	got := flatpakFinishArgs(q)
	if got[len(got)-1] != "--filesystem=xdg-documents" || strings.Count(strings.Join(got, " "), "--share=ipc") != 1 {
		t.Errorf("extra args = %v", got)
	}
}
