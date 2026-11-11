package shell

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

// ManifestFile is the name of the manifest inside an app directory.
const ManifestFile = "app.json"

// Manifest describes an app directory. See README.md for the full spec.
type Manifest struct {
	AppID       string `json:"appId"`
	ProductName string `json:"productName"`
	Version     string `json:"version"`

	Window WindowSpec `json:"window"`

	// Assets is the built UI directory, relative to the app directory.
	// Default "dist".
	Assets string `json:"assets"`
	// Fallbacks map URL paths that have no file to a page.
	Fallbacks []Fallback `json:"fallbacks"`

	HostJS *HostJSSpec `json:"hostjs,omitempty"`
	GoHost *GoHostSpec `json:"gohost,omitempty"`

	Debug bool `json:"debug"`
}

// WindowSpec is the first window.
type WindowSpec struct {
	Title  string `json:"title"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
	// Resizable defaults to true.
	Resizable *bool `json:"resizable,omitempty"`
	// Path is the route to open. Default "/".
	Path string `json:"path"`
}

// Fallback maps a route pattern in Krate's segment syntax to the file that
// serves it, relative to the assets directory.
type Fallback struct {
	Pattern string `json:"pattern"`
	File    string `json:"file"`
}

// HostJSSpec locates the bundled TypeScript host code.
type HostJSSpec struct {
	// Bundle is the IIFE file, relative to the app directory.
	Bundle string `json:"bundle"`
	// Map is its source map (optional).
	Map string `json:"map"`
}

// GoHostSpec locates the Go host child and lists what it registers.
type GoHostSpec struct {
	// Binary is the child executable, relative to the app directory. ".exe"
	// is appended on Windows when missing.
	Binary  string   `json:"binary"`
	Funcs   []string `json:"funcs"`
	Streams []string `json:"streams"`
	Signals []string `json:"signals"`
}

var appIDRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// ReadManifest reads and validates dir/app.json and fills in defaults.
func ReadManifest(dir string) (*Manifest, error) {
	path := filepath.Join(dir, ManifestFile)
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("shell: reading the app manifest: %w", err)
	}
	return ParseManifest(b, path)
}

// ParseManifest parses and validates manifest JSON; name is used in errors.
func ParseManifest(b []byte, name string) (*Manifest, error) {
	var m Manifest
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("shell: %s: %w", name, err)
	}
	if err := m.normalize(); err != nil {
		return nil, fmt.Errorf("shell: %s: %w", name, err)
	}
	return &m, nil
}

func (m *Manifest) normalize() error {
	if !appIDRe.MatchString(m.AppID) {
		return fmt.Errorf("appId %q must be a reverse-DNS style id such as com.acme.app", m.AppID)
	}
	if m.ProductName == "" {
		m.ProductName = m.AppID
	}
	if m.Window.Title == "" {
		m.Window.Title = m.ProductName
	}
	if m.Window.Width <= 0 {
		m.Window.Width = 1024
	}
	if m.Window.Height <= 0 {
		m.Window.Height = 768
	}
	if m.Window.Path == "" {
		m.Window.Path = "/"
	}
	if !strings.HasPrefix(m.Window.Path, "/") {
		return fmt.Errorf("window.path %q must start with /", m.Window.Path)
	}
	if m.Assets == "" {
		m.Assets = "dist"
	}
	if err := local("assets", m.Assets); err != nil {
		return err
	}
	for i, f := range m.Fallbacks {
		if _, err := parsePattern(f.Pattern); err != nil {
			return fmt.Errorf("fallbacks[%d]: %w", i, err)
		}
		if err := local(fmt.Sprintf("fallbacks[%d].file", i), f.File); err != nil {
			return err
		}
	}
	if h := m.HostJS; h != nil {
		if err := local("hostjs.bundle", h.Bundle); err != nil {
			return err
		}
		if h.Map != "" {
			if err := local("hostjs.map", h.Map); err != nil {
				return err
			}
		}
	}
	if g := m.GoHost; g != nil {
		if err := local("gohost.binary", g.Binary); err != nil {
			return err
		}
		if runtime.GOOS == "windows" && !strings.HasSuffix(strings.ToLower(g.Binary), ".exe") {
			g.Binary += ".exe"
		}
		for _, list := range [][]string{g.Funcs, g.Streams, g.Signals} {
			for _, id := range list {
				if id == "" {
					return fmt.Errorf("gohost: empty function id")
				}
			}
		}
	}
	return nil
}

// local checks that p is a non-empty relative path that stays inside its
// base directory.
func local(field, p string) error {
	if p == "" {
		return fmt.Errorf("%s is required", field)
	}
	if !filepath.IsLocal(filepath.FromSlash(p)) {
		return fmt.Errorf("%s %q must be a relative path inside the app directory", field, p)
	}
	return nil
}

// IsResizable reports Resizable with its default (true).
func (w WindowSpec) IsResizable() bool { return w.Resizable == nil || *w.Resizable }

// AssetsDir returns the absolute assets directory of the app at dir.
func (m *Manifest) AssetsDir(dir string) string {
	return filepath.Join(dir, filepath.FromSlash(m.Assets))
}
