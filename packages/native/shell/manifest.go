package shell

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"

	"github.com/kratejs/krate/packages/native/sdk/permission"
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
	// HostChildren are TypeScript host code programs that run in a child
	// process on Node, Bun, Deno or as a compiled executable (scriptc), each
	// serving its own ids. See README.md.
	HostChildren []HostChildSpec `json:"hostchildren,omitempty"`

	// Permissions lists what the UI may use of the SDK (see package
	// sdk/permission). Empty means the default set (os:info, kv, db, window,
	// app, menu, dialog, notify, clipboard). Unknown names fail at load.
	Permissions []string `json:"permissions,omitempty"`

	Debug bool `json:"debug"`
}

// HasPermission reports whether the manifest grants a permission, with the
// defaults for an empty list and implied names resolved.
func (m *Manifest) HasPermission(name string) bool {
	set, err := permission.Parse(m.Permissions)
	return err == nil && set.Allows(name)
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

// HostChildSpec describes one JavaScript host child: a program that speaks the
// gohost child protocol (package hostchild is its runtime) and serves the ids
// listed here, plus any it reports at run time.
type HostChildSpec struct {
	// Runtime is "node", "bun", "deno" or "scriptc". It labels the child in
	// logs; how it starts is decided by Command and Entry.
	Runtime string `json:"runtime"`
	// Entry is the child's file, relative to the app directory: the script
	// when Command is set, otherwise an executable (".exe" is appended on
	// Windows when missing).
	Entry string `json:"entry"`
	// Map is the entry's source map (informational).
	Map string `json:"map,omitempty"`
	// Command is the argv that starts a script entry. The element "{entry}" is
	// replaced by the absolute path of Entry. A first element without a path
	// separator is looked up on PATH. Empty means Entry is the executable.
	Command []string `json:"command,omitempty"`
	Funcs   []string `json:"funcs"`
	Streams []string `json:"streams"`
	Signals []string `json:"signals"`
}

// HostChildRuntimes are the values of HostChildSpec.Runtime.
var HostChildRuntimes = []string{"node", "bun", "deno", "scriptc"}

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
	if _, err := permission.Parse(m.Permissions); err != nil {
		return fmt.Errorf("permissions: %w", err)
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
	seen := map[string]bool{}
	for i := range m.HostChildren {
		h := &m.HostChildren[i]
		field := fmt.Sprintf("hostchildren[%d]", i)
		if !slices.Contains(HostChildRuntimes, h.Runtime) {
			return fmt.Errorf("%s.runtime %q must be one of %s", field, h.Runtime, strings.Join(HostChildRuntimes, ", "))
		}
		if err := local(field+".entry", h.Entry); err != nil {
			return err
		}
		if len(h.Command) == 0 && runtime.GOOS == "windows" && !strings.HasSuffix(strings.ToLower(h.Entry), ".exe") {
			h.Entry += ".exe"
		}
		if seen[h.Entry] {
			return fmt.Errorf("%s.entry %q is listed twice", field, h.Entry)
		}
		seen[h.Entry] = true
		if h.Map != "" {
			if err := local(field+".map", h.Map); err != nil {
				return err
			}
		}
		if len(h.Command) > 0 && (h.Command[0] == "" || !slices.Contains(h.Command, "{entry}")) {
			return fmt.Errorf("%s.command must name a program and contain {entry}", field)
		}
		for _, list := range [][]string{h.Funcs, h.Streams, h.Signals} {
			for _, id := range list {
				if id == "" {
					return fmt.Errorf("%s: empty function id", field)
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
