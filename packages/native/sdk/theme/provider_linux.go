//go:build linux

package theme

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"

	"github.com/kratejs/krate/packages/native/platform/caps"
	"github.com/kratejs/krate/packages/native/sdk/internal/dbusiface"
	"github.com/kratejs/krate/packages/native/sdk/internal/sessionbus"
)

const (
	portalDest  = "org.freedesktop.portal.Desktop"
	portalPath  = "/org/freedesktop/portal/desktop"
	portalIface = "org.freedesktop.portal.Settings"
	appearance  = "org.freedesktop.appearance"
)

// portalProvider reads org.freedesktop.appearance from the desktop portal and
// falls back to GTK configuration when there is no portal.
type portalProvider struct{}

func newDefaultProvider() provider { return portalProvider{} }

func defaultAvailable() bool { return true }

// unwrap strips the nested variants a Settings.Read reply carries.
func unwrap(v any) any {
	for {
		vv, ok := v.(dbusiface.Variant)
		if !ok {
			return v
		}
		v = vv.Value
	}
}

func readPortal(key string) (uint32, bool) {
	conn, err := sessionbus.Get()
	if err != nil {
		return 0, false
	}
	out, err := conn.Call(portalDest, portalPath, portalIface, "Read", appearance, key)
	if err != nil || len(out) != 1 {
		return 0, false
	}
	n, ok := unwrap(out[0]).(uint32)
	return n, ok
}

func (portalProvider) read() (caps.ThemeInfo, error) {
	var info caps.ThemeInfo
	if cs, ok := readPortal("color-scheme"); ok {
		// 0 no preference, 1 prefer dark, 2 prefer light.
		info.Dark = cs == 1
		if c, ok := readPortal("contrast"); ok {
			info.HighContrast = c == 1
		}
		return info, nil
	}
	return gtkFallback(), nil
}

func (portalProvider) watch(fn func()) func() {
	conn, err := sessionbus.Get()
	if err != nil {
		return func() {}
	}
	cancel, err := conn.Subscribe("type='signal',interface='"+portalIface+"',member='SettingChanged'", func(s dbusiface.Signal) {
		if len(s.Body) >= 1 {
			if ns, _ := s.Body[0].(string); ns == appearance {
				fn()
			}
		}
	})
	if err != nil {
		return func() {}
	}
	return cancel
}

// gtkFallback reads GTK_THEME and the GTK 3 settings file.
func gtkFallback() caps.ThemeInfo {
	var info caps.ThemeInfo
	name := os.Getenv("GTK_THEME")
	prefer := false
	if name == "" {
		dir := os.Getenv("XDG_CONFIG_HOME")
		if dir == "" {
			if home, err := os.UserHomeDir(); err == nil {
				dir = filepath.Join(home, ".config")
			}
		}
		if dir != "" {
			name, prefer = parseGtkSettings(filepath.Join(dir, "gtk-3.0", "settings.ini"))
		}
	}
	lower := strings.ToLower(name)
	info.Dark = prefer || strings.HasSuffix(lower, ":dark") || strings.HasSuffix(lower, "-dark") || strings.Contains(lower, "dark")
	info.HighContrast = strings.HasPrefix(lower, "highcontrast")
	return info
}

// parseGtkSettings returns gtk-theme-name and gtk-application-prefer-dark-theme
// from a settings.ini.
func parseGtkSettings(path string) (name string, preferDark bool) {
	f, err := os.Open(path)
	if err != nil {
		return "", false
	}
	defer f.Close()
	return parseGtkIni(bufio.NewScanner(f))
}

func parseGtkIni(sc *bufio.Scanner) (name string, preferDark bool) {
	in := false
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if strings.HasPrefix(line, "[") {
			in = line == "[Settings]"
			continue
		}
		if !in {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k, v = strings.TrimSpace(k), strings.TrimSpace(v)
		switch k {
		case "gtk-theme-name":
			name = v
		case "gtk-application-prefer-dark-theme":
			preferDark = v == "1" || strings.EqualFold(v, "true")
		}
	}
	return name, preferDark
}
