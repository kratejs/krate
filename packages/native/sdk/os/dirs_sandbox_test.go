package sdkos

import (
	"strings"
	"testing"
)

// Flatpak and Snap point the XDG variables into the sandbox; the app's
// directories follow them, so data stays in the sandbox.
func TestDirsFollowSandboxXDG(t *testing.T) {
	env := map[string]string{
		"XDG_DATA_HOME":   "/home/u/.var/app/com.acme.notes/data",
		"XDG_CONFIG_HOME": "/home/u/.var/app/com.acme.notes/config",
		"XDG_CACHE_HOME":  "/home/u/.var/app/com.acme.notes/cache",
		"XDG_STATE_HOME":  "/home/u/.var/app/com.acme.notes/.local/state",
		"XDG_RUNTIME_DIR": "/run/user/1000",
	}
	d, err := computeDirs(dirInput{GOOS: "linux", AppID: "com.acme.notes", Home: "/home/u", TempDir: "/tmp", UID: 1000, Getenv: func(k string) string { return env[k] }})
	if err != nil {
		t.Fatal(err)
	}
	for name, p := range map[string]string{"data": d.Data, "config": d.Config, "cache": d.Cache, "log": d.Log} {
		if !strings.HasPrefix(p, "/home/u/.var/app/com.acme.notes/") {
			t.Errorf("%s dir %s is outside the sandbox", name, p)
		}
	}
}
