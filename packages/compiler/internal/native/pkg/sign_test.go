package pkg

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func machoFile(t *testing.T, p string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	// A 64-bit little-endian Mach-O magic is enough for isMachO.
	if err := os.WriteFile(p, []byte{0xcf, 0xfa, 0xed, 0xfe, 0, 0, 0, 0}, 0o755); err != nil {
		t.Fatal(err)
	}
}

func stagedApp(t *testing.T) (stage, app string) {
	t.Helper()
	stage = t.TempDir()
	app = filepath.Join(stage, "Acme Notes.app")
	machoFile(t, filepath.Join(app, "Contents/MacOS/Acme Notes"))
	machoFile(t, filepath.Join(app, "Contents/Resources/app/gohost/krate-gohost"))
	machoFile(t, filepath.Join(app, "Contents/Frameworks/libx.dylib"))
	if err := os.WriteFile(filepath.Join(app, "Contents/Resources/app/app.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	return
}

func TestSignMacAppDeepSignsInnerFirst(t *testing.T) {
	stage, app := stagedApp(t)
	fr := &fakeRunner{goos: "darwin", tools: map[string]bool{"codesign": true}}
	err := SignMacApp(SignConfig{Runner: fr, MacIdentity: "Developer ID Application: Acme (ABCDE12345)", HostRuntime: "node"}, app)
	if err != nil {
		t.Fatal(err)
	}
	if len(fr.calls) != 4 { // two nested, the bundle, verify
		t.Fatalf("calls:\n%s", strings.Join(fr.calls, "\n"))
	}
	for _, c := range fr.calls[:3] {
		for _, want := range []string{"codesign --force --options runtime --timestamp --entitlements ", "--sign Developer ID Application: Acme (ABCDE12345) "} {
			if !strings.Contains(c, want) {
				t.Errorf("%q lacks %q", c, want)
			}
		}
	}
	if !strings.HasSuffix(fr.calls[2], "Acme Notes.app") {
		t.Errorf("the bundle must be signed last: %s", fr.calls[2])
	}
	if strings.HasSuffix(fr.calls[0], "Acme Notes") || strings.HasSuffix(fr.calls[1], "Acme Notes") {
		t.Errorf("the main executable is signed with the bundle, not before")
	}
	if !strings.HasPrefix(fr.calls[3], "codesign --verify --deep --strict") {
		t.Errorf("no verification: %s", fr.calls[3])
	}
	ent, err := os.ReadFile(filepath.Join(stage, "entitlements.plist"))
	if err != nil || !strings.Contains(string(ent), "allow-jit") {
		t.Fatalf("node host runtime needs the JIT entitlement: %s %v", ent, err)
	}
}
