package build

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/kratejs/krate/packages/compiler/internal/native/pkg"
)

// A macOS process only has a bundle identifier (which UNUserNotificationCenter
// needs, and which names the app in the menu bar, Dock and notification
// settings) when it runs from an .app bundle. `krate native dev` therefore
// runs the shell from a minimal generated bundle under <root>/.krate/dev.

// devBundleID derives the dev bundle identifier from the app id: "<appId>.dev".
// Characters a bundle identifier may not hold are replaced.
func devBundleID(appID string) string {
	var b strings.Builder
	for _, r := range appID {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	id := strings.Trim(b.String(), ".-")
	if id == "" {
		id = "com.example.app"
	}
	if strings.HasSuffix(id, ".dev") {
		return id
	}
	return id + ".dev"
}

// devBundleName is the bundle and executable name: the product name without
// characters that are unsafe in a file name.
func devBundleName(productName string) string {
	r := strings.NewReplacer("/", "-", "\\", "-", ":", "-", "*", "", "?", "", "\"", "", "<", "", ">", "", "|", "-")
	s := strings.Map(func(c rune) rune {
		if c < 0x20 || c == 0x7f {
			return -1
		}
		return c
	}, r.Replace(productName))
	s = strings.Trim(strings.TrimSpace(s), ". ")
	if s == "" {
		return "App"
	}
	return s
}

// devBundlePlist is the Info.plist of the dev bundle.
func devBundlePlist(appID, productName string) []byte {
	return pkg.InfoPlist(pkg.Metadata{
		AppID:       devBundleID(appID),
		ProductName: devBundleName(productName),
		Version:     "0.0.0",
	}, devBundleName(productName))
}

// macDevBundleWanted reports whether a dev session runs from a bundle: only
// on a macOS host, with a window (browser mode has none), for a native build.
func macDevBundleWanted(hostOS, targetOS string, browser bool) bool {
	return hostOS == "darwin" && targetOS == "darwin" && !browser
}

// prepareMacDevBundle creates or refreshes <dir>/<Name>.app around a copy of
// the shell binary and returns the path of the bundle's executable. It is
// cheap when nothing changed (the shell and plist are compared first).
func prepareMacDevBundle(dir, shell, appID, productName string) (string, error) {
	name := devBundleName(productName)
	app := filepath.Join(dir, name+".app")
	exe := filepath.Join(app, "Contents", "MacOS", name)
	plistPath := filepath.Join(app, "Contents", "Info.plist")

	if err := os.MkdirAll(filepath.Dir(exe), 0o755); err != nil {
		return "", err
	}
	changed := false
	plist := devBundlePlist(appID, productName)
	if old, err := os.ReadFile(plistPath); err != nil || string(old) != string(plist) {
		if err := os.WriteFile(plistPath, plist, 0o644); err != nil {
			return "", err
		}
		changed = true
	}
	_ = os.WriteFile(filepath.Join(app, "Contents", "PkgInfo"), []byte("APPL????"), 0o644)

	src, err := os.Stat(shell)
	if err != nil {
		return "", fmt.Errorf("the shell binary: %w", err)
	}
	if dst, err := os.Stat(exe); err != nil || dst.Size() != src.Size() || !dst.ModTime().Equal(src.ModTime()) {
		if err := copyExecutable(shell, exe); err != nil {
			return "", err
		}
		changed = true
	}
	if changed {
		// Best effort: bind the Info.plist to the executable so the system
		// treats the bundle as one app. An unsigned bundle still runs.
		if cs, err := exec.LookPath("codesign"); err == nil {
			_ = exec.Command(cs, "--force", "--sign", "-", app).Run()
		}
	}
	return exe, nil
}

// copyExecutable copies src to dst through a temporary file so a running copy
// is replaced rather than overwritten, and keeps src's modification time.
func copyExecutable(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp := dst + ".tmp"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, dst); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if st, err := os.Stat(src); err == nil {
		_ = os.Chtimes(dst, st.ModTime(), st.ModTime())
	}
	return nil
}
