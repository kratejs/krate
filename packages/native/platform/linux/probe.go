//go:build linux

package linux

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/ebitengine/purego"

	"github.com/kratejs/krate/packages/native/platform/probe"
)

// Probe reports whether WebKitGTK 4.1 and a display are available, without
// opening a window. It loads the libraries into the process.
func Probe() probe.Info {
	info := probe.Info{OS: "linux", Arch: runtime.GOARCH, OSVersion: osRelease()}
	wv := probe.Webview{Name: "WebKitGTK 4.1", MinVersion: fmt.Sprintf("%d.%d", minMajor, minMinor)}

	libs := []struct{ name, pkg string }{
		{"libglib-2.0.so.0", "libglib2.0-0"},
		{"libgtk-3.so.0", "libgtk-3-0"},
		{"libsoup-3.0.so.0", "libsoup-3.0-0"},
		{"libwebkit2gtk-4.1.so.0", "libwebkit2gtk-4.1-0"},
	}
	var missing, pkgs []string
	handles := map[string]uintptr{}
	for _, l := range libs {
		h, err := purego.Dlopen(l.name, rtldNow|rtldGlobal)
		if err != nil {
			missing = append(missing, l.name)
			pkgs = append(pkgs, l.pkg)
			continue
		}
		handles[l.name] = h
	}
	wv.Libraries = loadedLibs("libwebkit2gtk-4.1", "libgtk-3", "libsoup-3.0")
	switch wk := handles["libwebkit2gtk-4.1.so.0"]; {
	case len(missing) > 0:
		wv.Error = "cannot load " + strings.Join(missing, ", ")
		wv.Hint = "install " + strings.Join(pkgs, ", ") + " (Debian/Ubuntu: apt install libwebkit2gtk-4.1-0)"
		wv.Present = handles["libwebkit2gtk-4.1.so.0"] != 0
	default:
		wv.Present = true
		var maj, mnr, mic func() uint32
		if _, err := purego.Dlsym(wk, "webkit_get_major_version"); err != nil {
			wv.Error = "libwebkit2gtk-4.1 has no version functions"
			break
		}
		purego.RegisterLibFunc(&maj, wk, "webkit_get_major_version")
		purego.RegisterLibFunc(&mnr, wk, "webkit_get_minor_version")
		purego.RegisterLibFunc(&mic, wk, "webkit_get_micro_version")
		wv.Version = probe.Version(maj(), mnr(), mic())
		if versionOK(maj(), mnr()) {
			wv.OK = true
		} else {
			wv.Error = fmt.Sprintf("WebKitGTK %s is too old; %s or newer is required", wv.Version, wv.MinVersion)
			wv.Hint = "upgrade libwebkit2gtk-4.1-0"
		}
	}
	info.Webview = wv
	info.Display = displayInfo()
	return info
}

// displayInfo looks at the session environment the way GTK would.
func displayInfo() probe.Display {
	if w := os.Getenv("WAYLAND_DISPLAY"); w != "" {
		sock := w
		if !filepath.IsAbs(sock) {
			sock = filepath.Join(os.Getenv("XDG_RUNTIME_DIR"), w)
		}
		if _, err := os.Stat(sock); err == nil {
			return probe.Display{Available: true, Detail: "wayland (" + w + ")"}
		}
	}
	if d := os.Getenv("DISPLAY"); d != "" {
		if rest, ok := strings.CutPrefix(d, ":"); ok {
			num, _, _ := strings.Cut(rest, ".")
			if _, err := os.Stat("/tmp/.X11-unix/X" + num); err != nil {
				return probe.Display{Detail: "DISPLAY=" + d + " is set but its X socket does not exist"}
			}
		}
		return probe.Display{Available: true, Detail: "x11 (" + d + ")"}
	}
	return probe.Display{Detail: "neither WAYLAND_DISPLAY nor DISPLAY is set"}
}

// loadedLibs lists the files of /proc/self/maps whose name contains one of
// the fragments.
func loadedLibs(fragments ...string) []string {
	data, err := os.ReadFile("/proc/self/maps")
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for _, line := range strings.Split(string(data), "\n") {
		i := strings.Index(line, "/")
		if i < 0 {
			continue
		}
		p := line[i:]
		for _, f := range fragments {
			if strings.Contains(filepath.Base(p), f) && !seen[p] {
				seen[p] = true
				out = append(out, p)
			}
		}
	}
	sort.Strings(out)
	return out
}

// osRelease returns PRETTY_NAME of /etc/os-release.
func osRelease() string {
	data, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		if v, ok := strings.CutPrefix(line, "PRETTY_NAME="); ok {
			return strings.Trim(v, `"`)
		}
	}
	return ""
}
