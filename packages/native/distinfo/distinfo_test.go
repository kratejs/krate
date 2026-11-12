package distinfo

import (
	"errors"
	"testing"
)

func env(goos string, vars map[string]string, files map[string]string) Env {
	return Env{
		Getenv: func(k string) string { return vars[k] },
		Exists: func(p string) bool { _, ok := files[p]; return ok },
		ReadFile: func(p string) ([]byte, error) {
			if s, ok := files[p]; ok {
				return []byte(s), nil
			}
			return nil, errors.New("missing")
		},
		Exe:  "/opt/app/App",
		GOOS: goos,
	}
}

func TestDetect(t *testing.T) {
	cases := []struct {
		name  string
		e     Env
		store Store
		tgt   string
		self  bool
	}{
		{"direct", env("linux", nil, nil), Direct, "", true},
		{"flatpak env", env("linux", map[string]string{"FLATPAK_ID": "org.acme.App"}, nil), Flatpak, "", false},
		{"flatpak info file", env("linux", nil, map[string]string{"/.flatpak-info": ""}), Flatpak, "", false},
		{"snap", env("linux", map[string]string{"SNAP_NAME": "acme", "SNAP": "/snap/acme/1"}, nil), Snap, "", false},
		{"snap name alone is not a snap", env("linux", map[string]string{"SNAP_NAME": "acme"}, nil), Direct, "", true},
		{"mac sandbox", env("darwin", map[string]string{"APP_SANDBOX_CONTAINER_ID": "com.acme.app"}, nil), MAS, "", false},
		{"universal marker in Resources", env("darwin", nil, map[string]string{"/opt/Resources/" + MarkerFile: `{"target":"darwin-universal"}`}), Direct, "darwin-universal", true},
		{"store marker next to exe", env("windows", nil, map[string]string{"/opt/app/" + MarkerFile: `{"store":"msstore"}`}), MSStore, "", false},
		{"broken marker is ignored", env("linux", nil, map[string]string{"/opt/app/" + MarkerFile: `{`}), Direct, "", true},
	}
	for _, c := range cases {
		got := DetectIn(c.e)
		if got.Store != c.store || got.Target != c.tgt || got.SelfUpdate() != c.self {
			t.Errorf("%s: got %+v selfUpdate=%v", c.name, got, got.SelfUpdate())
		}
		if !c.self && got.UpdateNote() == "" {
			t.Errorf("%s: no update note", c.name)
		}
	}
	if !(Info{Store: Flatpak}).Sandboxed() || (Info{Store: MSStore}).Sandboxed() || (Info{}).Sandboxed() {
		t.Error("Sandboxed is wrong")
	}
}
