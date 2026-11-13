// Package distinfo tells a running app how it was distributed: directly (a zip,
// installer, AppImage), or through a store or sandbox that delivers updates
// and mediates system access itself (Flatpak, Snap, the Mac App Store, the
// Microsoft Store). The SDK uses it to turn off the self-updater inside a
// store, to route URL opening and notifications through the desktop portals,
// and to pick the update target of a macOS universal app.
//
// Detection reads the environment the sandbox sets (FLATPAK_ID, SNAP_NAME,
// APP_SANDBOX_CONTAINER_ID) and krate-dist.json, a marker the packager writes
// next to the executable (Contents/Resources on macOS).
package distinfo

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"sync"
)

// Store names a distribution channel. The empty Store is a direct download.
type Store string

// Distribution channels.
const (
	Direct  Store = ""
	Flatpak Store = "flatpak"
	Snap    Store = "snap"
	MAS     Store = "mas"
	MSStore Store = "msstore"
)

// MarkerFile is the file name of the packaging marker.
const MarkerFile = "krate-dist.json"

// Info describes the running distribution.
type Info struct {
	Store Store
	// Target replaces the runtime "<os>-<arch>" in the update feed URL (a
	// universal macOS app reports "darwin-universal"). Empty keeps the default.
	Target string
}

// Marker is the content of krate-dist.json.
type Marker struct {
	Store  Store  `json:"store,omitempty"`
	Target string `json:"target,omitempty"`
}

// Sandboxed reports whether the app runs in a store or desktop sandbox.
func (i Info) Sandboxed() bool { return i.Store != Direct && i.Store != MSStore }

// SelfUpdate reports whether the app may replace its own files. Every store
// delivers updates itself.
func (i Info) SelfUpdate() bool { return i.Store == Direct }

// UpdateNote is the sentence an updater error gives when SelfUpdate is false.
func (i Info) UpdateNote() string {
	switch i.Store {
	case Flatpak:
		return "this copy was installed from Flatpak, which delivers updates (flatpak update)"
	case Snap:
		return "this copy was installed from a snap, which updates itself (snap refresh)"
	case MAS:
		return "this copy came from the Mac App Store, which delivers updates"
	case MSStore:
		return "this copy came from the Microsoft Store, which delivers updates"
	}
	return ""
}

// Env is what detection reads; tests replace it.
type Env struct {
	Getenv   func(string) string
	Exists   func(string) bool
	ReadFile func(string) ([]byte, error)
	Exe      string
	GOOS     string
}

// Detect reads the real process environment.
func Detect() Info {
	exe, _ := os.Executable()
	if r, err := filepath.EvalSymlinks(exe); err == nil {
		exe = r
	}
	return DetectIn(Env{
		Getenv:   os.Getenv,
		Exists:   func(p string) bool { _, err := os.Stat(p); return err == nil },
		ReadFile: os.ReadFile,
		Exe:      exe,
		GOOS:     runtime.GOOS,
	})
}

var (
	mu     sync.Mutex
	cached *Info
)

// Current is Detect, computed once.
func Current() Info {
	mu.Lock()
	defer mu.Unlock()
	if cached == nil {
		i := Detect()
		cached = &i
	}
	return *cached
}

// Override makes Current return i until the returned function is called. Tests
// use it to run as a Flatpak, Snap or store build.
func Override(i Info) (restore func()) {
	mu.Lock()
	prev := cached
	cached = &i
	mu.Unlock()
	return func() {
		mu.Lock()
		cached = prev
		mu.Unlock()
	}
}

// DetectIn detects the distribution in e.
func DetectIn(e Env) Info {
	var info Info
	if e.Exe != "" && e.ReadFile != nil {
		dirs := []string{filepath.Dir(e.Exe)}
		if e.GOOS == "darwin" {
			dirs = append(dirs, filepath.Join(filepath.Dir(e.Exe), "..", "Resources"))
		}
		for _, d := range dirs {
			b, err := e.ReadFile(filepath.Join(d, MarkerFile))
			if err != nil {
				continue
			}
			var m Marker
			if json.Unmarshal(b, &m) == nil {
				info.Store, info.Target = m.Store, m.Target
			}
			break
		}
	}
	switch e.GOOS {
	case "linux":
		switch {
		case e.Getenv("FLATPAK_ID") != "" || (e.Exists != nil && e.Exists("/.flatpak-info")):
			info.Store = Flatpak
		case e.Getenv("SNAP_NAME") != "" && e.Getenv("SNAP") != "":
			info.Store = Snap
		}
	case "darwin":
		if e.Getenv("APP_SANDBOX_CONTAINER_ID") != "" {
			info.Store = MAS
		}
	}
	return info
}
