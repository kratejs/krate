package shell

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"

	"github.com/kratejs/krate/packages/native/internal/memory"
	"github.com/kratejs/krate/packages/native/plugin"
	"github.com/kratejs/krate/packages/native/sdk/media"
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

	// StrictPermissions makes an empty (or missing) Permissions list mean
	// "nothing" instead of the defaults. A derived release build sets it.
	StrictPermissions bool `json:"strictPermissions,omitempty"`

	// Plugins are the names of the native plugins the app uses. The shell must
	// have them linked (RunOptions.Plugins); their permissions are listed in
	// Permissions as "plugin:<name>.<permission>".
	Plugins []string `json:"plugins,omitempty"`

	// Updater is native.updater: the feed URL template and the base64
	// Ed25519 public key that verifies update files.
	Updater *UpdaterSpec `json:"updater,omitempty"`

	// Memory is the memory profile: "balanced" (default), "minimal" or
	// "performance". See README.md.
	Memory string `json:"memory,omitempty"`
	// Features says which optional webview features the UI uses. The
	// profiles other than "performance" switch the others off. A nil block
	// (an older app.json, not scanned) keeps every feature on.
	Features *Features `json:"features,omitempty"`

	// Media is native.media: the policy for the page's camera, microphone,
	// screen capture and location requests. See sdk/media.
	Media *MediaSpec `json:"media,omitempty"`

	Debug bool `json:"debug"`
}

// MediaSpec is the media block of app.json.
type MediaSpec struct {
	// Policy maps a kind (camera, microphone, screen, location,
	// notifications, clipboardRead) to prompt, allow or deny.
	Policy map[string]string `json:"policy,omitempty"`
	// NoRemember stops the native prompt from remembering answers.
	NoRemember bool `json:"noRemember,omitempty"`
}

// Features are the optional webview features an app uses.
type Features struct {
	Media      bool `json:"media"`
	WebAudio   bool `json:"webaudio"`
	WebGL      bool `json:"webgl"`
	WebRTC     bool `json:"webrtc"`
	SpellCheck bool `json:"spellcheck"`
}

// MemoryProfile is the manifest's profile with the default applied.
func (m *Manifest) MemoryProfile() memory.Profile { return memory.Parse(m.Memory) }

// PermissionSet parses the manifest's permission list.
func (m *Manifest) PermissionSet() (permission.Set, error) {
	if m.StrictPermissions && len(m.Permissions) == 0 {
		return permission.None(), nil
	}
	return permission.Parse(m.Permissions)
}

// UpdaterSpec configures in-app updates.
type UpdaterSpec struct {
	URL       string `json:"url"`
	PublicKey string `json:"publicKey"`
}

// HasPermission reports whether the manifest grants a permission, with the
// defaults for an empty list and implied names resolved.
func (m *Manifest) HasPermission(name string) bool {
	set, err := m.PermissionSet()
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
	// Permissions, when present, narrows the SDK permissions the page of the
	// main window may use to this list (a subset of the app's). An empty
	// list leaves the page nothing.
	Permissions *[]string `json:"permissions,omitempty"`
	// KeepAlive keeps the app running with no window open (a tray app):
	// closing the last window releases its webview instead of quitting, and
	// Show or reopening the window brings it back on the same route.
	KeepAlive bool `json:"keepAlive,omitempty"`
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
	// MemoryMB is the QuickJS heap limit in MiB. 0 means the profile's
	// default (64 balanced, 32 minimal, 256 performance).
	MemoryMB int `json:"memoryMB,omitempty"`
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
	// Windows when missing). An embedded (release) app has no app directory:
	// Entry is then relative to the directory of the running executable.
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

// ReadManifestFS is ReadManifest for an app directory held in a file system
// (a release build embeds it).
func ReadManifestFS(fsys fs.FS) (*Manifest, error) {
	b, err := fs.ReadFile(fsys, ManifestFile)
	if err != nil {
		return nil, fmt.Errorf("shell: reading the app manifest: %w", err)
	}
	return ParseManifest(b, ManifestFile)
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
	for _, n := range m.Plugins {
		if !plugin.ValidName(n) {
			return fmt.Errorf("plugins: %q is not a valid plugin name", n)
		}
	}
	set, err := m.PermissionSet()
	if err != nil {
		return fmt.Errorf("permissions: %w", err)
	}
	if wp := m.Window.Permissions; wp != nil {
		if _, err := permission.Parse(*wp); err != nil {
			return fmt.Errorf("window.permissions: %w", err)
		}
		for _, n := range *wp {
			if !set.Allows(n) {
				return fmt.Errorf("window.permissions: %q is not in the app's permissions", n)
			}
		}
	}
	if m.Media != nil {
		for k, p := range m.Media.Policy {
			if !media.ValidKind(k) {
				return fmt.Errorf("media.policy has unknown kind %q", k)
			}
			if !media.ValidPolicy(p) {
				return fmt.Errorf("media.policy.%s must be prompt, allow or deny, got %q", k, p)
			}
		}
	}
	switch m.Memory {
	case "", "balanced", "minimal", "performance":
	default:
		return fmt.Errorf("memory %q must be balanced, minimal or performance", m.Memory)
	}
	if m.HostJS != nil && m.HostJS.MemoryMB < 0 {
		return fmt.Errorf("hostjs.memoryMB %d must not be negative", m.HostJS.MemoryMB)
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
