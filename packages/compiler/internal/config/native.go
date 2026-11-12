package config

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

// NativeConfig is the `native` block: settings for the desktop target
// (`krate native ...`). Nil when the project does not configure it; use
// EffectiveNative for the block with defaults applied.
type NativeConfig struct {
	// AppID is the reverse-DNS application identifier, e.g. "com.acme.app".
	AppID string `json:"appId,omitempty"`
	// ProductName is the user-visible application name.
	ProductName string `json:"productName,omitempty"`
	// Version is the application version (semver).
	Version string `json:"version,omitempty"`
	// Platforms lists the build targets. Default: windows, macos, linux.
	Platforms []string `json:"platforms,omitempty"`
	// Icons is the path of the source icon image.
	Icons string `json:"icons,omitempty"`
	// Window configures the main window.
	Window NativeWindow `json:"window,omitempty"`
	// Permissions is an optional allowlist of the SDK permissions the app may
	// use. A native build derives the permissions the app needs from the code
	// (see internal/native/capability), writes that list to app.json, and
	// fails when the code reaches a permission this list does not grant. Unset
	// means no restriction. Names: see NativePermissions.
	Permissions []string `json:"permissions,omitempty"`
	// DeepLinks lists custom URL schemes the app registers.
	DeepLinks []string `json:"deepLinks,omitempty"`
	// FileAssociations lists the file types the app opens. Packaging turns
	// them into macOS document types, MSIX file type associations, NSIS
	// ProgID keys and Linux MimeType entries with a shared-mime-info file. An
	// app that lists any takes the single-instance lock, so opening a file
	// while it runs reaches the running instance. See docs native/files.
	FileAssociations []NativeFileAssociation `json:"fileAssociations,omitempty"`
	// Host configures the runtime for TypeScript host code.
	Host NativeHost `json:"host,omitempty"`
	// Experimental holds opt-ins for unfinished features.
	Experimental NativeExperimental `json:"experimental,omitempty"`
	// Release configures release builds.
	Release NativeRelease `json:"release,omitempty"`
	// Bundle holds per-platform packaging options.
	Bundle NativeBundle `json:"bundle,omitempty"`
	// Updater configures in-app updates. Nil disables them.
	Updater *NativeUpdater `json:"updater,omitempty"`
	// Plugins lists the native plugins the app uses: a directory with a
	// krate-plugin.json ("./native-plugins/hello"), an npm package that ships
	// one ("@acme/krate-sqlite") or a Go module ("go:github.com/acme/krate-sqlite@v1.2.0").
	// An app with plugins runs under a per-app shell built with the Go
	// toolchain. See docs native/plugins.
	Plugins []string `json:"plugins,omitempty"`
	// Memory is the memory profile: "balanced" (default), "minimal" or
	// "performance" (the engine defaults). See docs native/memory.
	Memory string `json:"memory,omitempty"`
	// Features overrides the detected engine features. A feature the UI code
	// uses is switched on under "balanced" and "minimal" unless set false.
	Features NativeFeatures `json:"features,omitempty"`
	// Media is the policy for the page's camera, microphone, screen capture
	// and location requests, and the usage descriptions packaging writes.
	Media NativeMedia `json:"media,omitempty"`
	// ViewTransitions is "auto" to keep View Transitions in a native build.
	// The default is off, whatever the top-level viewTransitions says: a
	// cross-document transition aborts the WebKitGTK web process in
	// software-rendered setups.
	ViewTransitions string `json:"viewTransitions,omitempty"`
	// Lean tunes the memory-hygiene checks of `krate native check` and
	// native builds.
	Lean NativeLean `json:"lean,omitempty"`
	// Debug keeps the webview's developer tools (the web inspector) on in a
	// release build. They are always on in `krate native dev`.
	Debug bool `json:"debug,omitempty"`
}

// NativeFileAssociation is one file type the app opens.
type NativeFileAssociation struct {
	// Ext lists the extensions without the dot: ["md", "markdown"].
	Ext []string `json:"ext"`
	// Name is the type's display name, for example "Markdown document".
	// Default: "<EXT> file".
	Name string `json:"name,omitempty"`
	// MIME is the media type, for example "text/markdown". Default:
	// application/x-<app>-<first extension>.
	MIME string `json:"mime,omitempty"`
	// Role is "editor" (default) or "viewer".
	Role string `json:"role,omitempty"`
	// Icon is the path of a PNG for files of this type; relative paths resolve
	// against the project root. Default: the app icon.
	Icon string `json:"icon,omitempty"`
}

// NativeFileRoles are the values of NativeFileAssociation.Role.
var NativeFileRoles = []string{"editor", "viewer"}

// nativeNeverAssociate lists extensions of programs and shortcuts: an app
// cannot claim them (they would become launchers for the app).
var nativeNeverAssociate = []string{"exe", "dll", "bat", "cmd", "com", "msi", "scr", "lnk", "ps1", "vbs", "sh", "app", "pif"}

// NativeLean tunes the lean-page checks (KR7270 to KR7275). Zero values keep
// the defaults.
type NativeLean struct {
	// DataURIKB is the size in KiB above which an inlined data: URL is
	// reported. Default 16.
	DataURIKB int `json:"dataUriKb,omitempty"`
	// ImageRatio is how many times larger (in pixels per side) an image file
	// may be than its rendered size before it is reported. Default 2.
	ImageRatio float64 `json:"imageRatio,omitempty"`
	// ListItems is the number of items a statically visible list may render
	// without a windowing component. Default 100.
	ListItems int `json:"listItems,omitempty"`
	// Ignore lists diagnostic codes (for example "KR7272") to silence.
	Ignore []string `json:"ignore,omitempty"`
}

// NativeMedia configures webview permission requests. Each policy is
// "prompt" (default: a native dialog), "allow" or "deny". An allowed request
// still needs the "media" or "location" permission in the window's scope and
// the operating system's consent.
type NativeMedia struct {
	Camera        string `json:"camera,omitempty"`
	Microphone    string `json:"microphone,omitempty"`
	Screen        string `json:"screen,omitempty"`
	Location      string `json:"location,omitempty"`
	Notifications string `json:"notifications,omitempty"`
	ClipboardRead string `json:"clipboardRead,omitempty"`
	// Remember keeps the answers of the native prompt (default true).
	Remember *bool `json:"remember,omitempty"`
	// Usage holds the sentences the operating system shows in its consent
	// prompts: macOS Info.plist usage descriptions. A native build that uses
	// the feature without one reports KR7290.
	Usage NativeMediaUsage `json:"usage,omitempty"`
}

// NativeMediaUsage are the usage descriptions.
type NativeMediaUsage struct {
	Camera        string `json:"camera,omitempty"`
	Microphone    string `json:"microphone,omitempty"`
	Location      string `json:"location,omitempty"`
	ScreenCapture string `json:"screenCapture,omitempty"`
}

// NativeMediaPolicies are the values of a media policy.
var NativeMediaPolicies = []string{"prompt", "allow", "deny"}

// NativeFeatures are explicit overrides for the webview features that the
// memory profiles switch off when the UI does not use them. Nil means
// "detect from the code".
type NativeFeatures struct {
	Media      *bool `json:"media,omitempty"`
	WebAudio   *bool `json:"webaudio,omitempty"`
	WebGL      *bool `json:"webgl,omitempty"`
	WebRTC     *bool `json:"webrtc,omitempty"`
	SpellCheck *bool `json:"spellcheck,omitempty"`
}

// NativeWindow configures the main window.
type NativeWindow struct {
	Width  int `json:"width,omitempty"`
	Height int `json:"height,omitempty"`
	// Resizable defaults to true.
	Resizable *bool `json:"resizable,omitempty"`
	// TitleBar is "native" (default), "hidden" or "none".
	TitleBar string `json:"titleBar,omitempty"`
	// Permissions, when set, narrows the SDK permissions the page of the main
	// window may use to this list (a subset of the app's). An empty list
	// leaves the page nothing. Windows opened with window.Open take the same
	// option and inherit the opener's scope.
	Permissions *[]string `json:"permissions,omitempty"`
	// KeepAlive keeps the app running with no window open, for tray apps.
	// Closing the last window then releases its webview instead of quitting,
	// and window.show() (or reopening from the tray) brings it back on the
	// same route. Default false.
	KeepAlive bool `json:"keepAlive,omitempty"`
}

// NativeHost configures where TypeScript host code runs.
type NativeHost struct {
	// Runtime is "quickjs" (default), "node", "bun", "deno" or "scriptc".
	Runtime string `json:"runtime,omitempty"`
	// Version pins the runtime version for node, bun and deno.
	Version string `json:"version,omitempty"`
	// Bundle ships the runtime inside the app. Default true.
	Bundle *bool `json:"bundle,omitempty"`
	// MemoryMB overrides the QuickJS heap limit of the in-process host
	// runtime. Default: 64 (balanced), 32 (minimal), 256 (performance).
	MemoryMB int `json:"memoryMB,omitempty"`
	// Islands maps a package to the runtime that runs it ("node", "bun",
	// "deno" or "quickjs") when it cannot run in the main host runtime.
	Islands map[string]string `json:"islands,omitempty"`
}

// NativeExperimental holds opt-ins for unfinished features.
type NativeExperimental struct {
	Scriptc bool `json:"scriptc,omitempty"`
}

// NativeRelease configures release builds.
type NativeRelease struct {
	// Compile is "auto" (default), "always" or "never".
	Compile string `json:"compile,omitempty"`
	// PGO is the path of a CPU profile for profile-guided optimization,
	// relative to the project root. Default "native.pgo"; `krate native
	// profile` writes it. A release build passes it to go build when the file
	// exists.
	PGO string `json:"pgo,omitempty"`
}

// NativeBundle holds per-platform packaging options.
type NativeBundle struct {
	MacOS   NativeBundleMacOS   `json:"macos,omitempty"`
	Windows NativeBundleWindows `json:"windows,omitempty"`
	Linux   NativeBundleLinux   `json:"linux,omitempty"`
}

// NativeBundleMacOS holds macOS packaging options.
type NativeBundleMacOS struct {
	Category string `json:"category,omitempty"`
	// Universal packages arm64 and x86_64 code in one .app (the same as
	// --arch universal). The release build then compiles both architectures.
	Universal bool `json:"universal,omitempty"`
	// MAS configures the Mac App Store variant (--target mas).
	MAS NativeMAS `json:"mas,omitempty"`
}

// NativeMAS configures the Mac App Store build: an app sandbox, an embedded
// provisioning profile, and a signed .pkg for App Store Connect.
type NativeMAS struct {
	// TeamID is the 10-character Apple developer team id. It forms the
	// application identifier TEAMID.<appId> in the entitlements.
	TeamID string `json:"teamId,omitempty"`
	// ProvisioningProfile is the path of the Mac App Store provisioning
	// profile (.provisionprofile) downloaded from the developer portal,
	// relative to the project root. Do not commit it.
	ProvisioningProfile string `json:"provisioningProfile,omitempty"`
	// Entitlements is the path of an extra entitlements plist whose keys are
	// added to the generated sandbox entitlements.
	Entitlements string `json:"entitlements,omitempty"`
	// Network lists "client" and/or "server": the network entitlements.
	// Default ["client"].
	Network []string `json:"network,omitempty"`
	// Files is the access to files the user picks: "read-write" (default),
	// "read-only" or "none". Pair "read-write" with security-scoped
	// bookmarks (fs.persistAccess) to keep access across launches.
	Files string `json:"files,omitempty"`
}

// NativeBundleWindows holds Windows packaging options.
type NativeBundleWindows struct {
	Publisher string `json:"publisher,omitempty"`
	// Installer is "msix" or "nsis".
	Installer string `json:"installer,omitempty"`
	// WebView2 says what the NSIS installer does when the Microsoft WebView2
	// Runtime is missing: "download" (default) fetches Microsoft's Evergreen
	// bootstrapper, "embed" ships the bootstrapper inside the installer,
	// "fixed" ships a Fixed Version runtime folder with the app, "none" does
	// nothing (the app then shows a message box with the download link).
	WebView2 string `json:"webview2,omitempty"`
	// WebView2Path is the bootstrapper file for "embed" (the
	// MicrosoftEdgeWebview2Setup.exe from Microsoft) or the extracted
	// runtime folder for "fixed", relative to the project root. The files
	// are Microsoft's and are not part of Krate; do not commit them.
	WebView2Path string `json:"webview2Path,omitempty"`
	// Store configures the Microsoft Store variant of the MSIX package
	// (--format msix-store). The three fields are copied from Partner Center
	// (Product identity).
	Store NativeWindowsStore `json:"store,omitempty"`
}

// NativeWindowsStore holds the Partner Center identity of the app.
type NativeWindowsStore struct {
	// IdentityName is Package/Identity/Name, for example
	// "12345Acme.AcmeNotes".
	IdentityName string `json:"identityName,omitempty"`
	// PublisherID is Package/Identity/Publisher, for example
	// "CN=01234567-89AB-CDEF-0123-456789ABCDEF".
	PublisherID string `json:"publisherId,omitempty"`
	// PublisherDisplayName is Package/Properties/PublisherDisplayName.
	PublisherDisplayName string `json:"publisherDisplayName,omitempty"`
}

// NativeBundleLinux holds Linux packaging options.
type NativeBundleLinux struct {
	// Formats lists "deb", "rpm", "appimage", "flatpak" and "snap".
	Formats []string `json:"formats,omitempty"`
	// Summary is the one-line description (AppStream summary, snap summary).
	Summary string `json:"summary,omitempty"`
	// Description is the long description, plain text (AppStream, snap).
	Description string `json:"description,omitempty"`
	// License is an SPDX expression for the app ("MIT", "Apache-2.0 OR MIT");
	// "LicenseRef-proprietary" for closed source. Flathub and the Snap Store
	// show it.
	License string `json:"license,omitempty"`
	// Homepage is the project URL.
	Homepage string `json:"homepage,omitempty"`
	// Flatpak configures the flatpak format.
	Flatpak NativeFlatpak `json:"flatpak,omitempty"`
	// Snap configures the snap format.
	Snap NativeSnap `json:"snap,omitempty"`
}

// NativeFlatpak configures the Flatpak manifest.
type NativeFlatpak struct {
	// Runtime is the runtime id; default "org.gnome.Platform", which carries
	// WebKitGTK 4.1.
	Runtime string `json:"runtime,omitempty"`
	// RuntimeVersion is the runtime branch, for example "48". Default "48".
	RuntimeVersion string `json:"runtimeVersion,omitempty"`
	// FinishArgs are extra flatpak finish-args (for example
	// "--filesystem=xdg-documents"), added to the ones derived from the
	// app's permissions.
	FinishArgs []string `json:"finishArgs,omitempty"`
}

// NativeSnap configures snapcraft.yaml.
type NativeSnap struct {
	// Grade is "stable" (default) or "devel".
	Grade string `json:"grade,omitempty"`
	// Confinement is "strict" (default), "classic" or "devmode".
	Confinement string `json:"confinement,omitempty"`
	// Plugs are extra plugs, added to the ones derived from the app's
	// permissions.
	Plugs []string `json:"plugs,omitempty"`
}

var nativeChannelRe = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,31}$`)

// NativeUpdater configures in-app updates.
type NativeUpdater struct {
	// URL is the update manifest URL; {target} expands to the platform.
	URL string `json:"url,omitempty"`
	// PublicKey verifies update signatures.
	PublicKey string `json:"publicKey,omitempty"`
	// Channel fills {channel} in URL; default "stable".
	Channel string `json:"channel,omitempty"`
	// MaxStarts is how many starts a freshly updated version gets to confirm
	// a healthy start before the next launch restores the previous version
	// (default 1). HealthTimeout is the seconds one start may take to
	// confirm (default 60).
	MaxStarts     int `json:"maxStarts,omitempty"`
	HealthTimeout int `json:"healthTimeout,omitempty"`
	// Confirm is "auto" (default: the shell confirms a healthy start after
	// the first page was served) or "app" (the app calls
	// updater.markHealthy()).
	Confirm string `json:"confirm,omitempty"`
}

// nativePermissionsJSON is a copy of packages/native/sdk/permission/names.json,
// the names the native SDK defines. TestNativePermissionsMatchSDK compares it
// with the original; refresh it with
// "cp packages/native/sdk/permission/names.json packages/compiler/internal/config/native_permissions.json".
//
//go:embed native_permissions.json
var nativePermissionsJSON []byte

type nativePermissionDoc struct {
	Names    []string `json:"names"`
	Defaults []string `json:"defaults"`
}

func nativePermissionList() nativePermissionDoc {
	var d nativePermissionDoc
	if err := json.Unmarshal(nativePermissionsJSON, &d); err != nil {
		panic("config: native_permissions.json: " + err.Error())
	}
	return d
}

func nativePermissionNames() []string { return nativePermissionList().Names }

// Native option values.
var (
	NativePlatforms      = []string{"windows", "macos", "linux"}
	NativePermissions    = nativePermissionNames()
	NativeHostRuntimes   = []string{"quickjs", "node", "bun", "deno", "scriptc"}
	NativeIslandRuntimes = []string{"node", "bun", "deno", "quickjs"}
	nativeTitleBars      = []string{"native", "hidden", "none"}
	nativeCompileModes   = []string{"auto", "always", "never"}
	nativeInstallers     = []string{"msix", "nsis"}
	nativeWebView2Modes  = []string{"download", "embed", "fixed", "none"}
	nativeLinuxFormats   = []string{"deb", "rpm", "appimage", "flatpak", "snap"}
	NativeMemoryProfiles = []string{"balanced", "minimal", "performance"}
)

// NativeDefaultPermissions are the SDK permissions of an app that lists none.
func NativeDefaultPermissions() []string { return nativePermissionList().Defaults }

// nativePluginPermRe matches the app.json name of a plugin permission,
// "plugin:<plugin>.<permission>". Which ones exist depends on the enabled
// plugins, so the config can only check the shape; the build checks the rest.
var nativePluginPermRe = regexp.MustCompile(`^plugin:[a-z][a-z0-9]*(-[a-z0-9]+)*\.[a-z][A-Za-z0-9]*$`)

// IsNativePermission reports whether p is an SDK permission or has the shape
// of a plugin permission.
func IsNativePermission(p string) bool {
	return oneOf(p, NativePermissions) || nativePluginPermRe.MatchString(p)
}

// NativePluginSpecKind classifies an entry of native.plugins: "path" for a
// directory, "go" for a Go module ("go:<module>[@version]"), "npm" for a
// package name, "" when it is none of them.
func NativePluginSpecKind(spec string) string {
	switch {
	case spec == "" || strings.TrimSpace(spec) != spec || strings.ContainsAny(spec, " \t\n"):
		return ""
	case isPathPluginSpec(spec):
		return "path"
	case strings.Contains(spec, "\\"):
		return ""
	case strings.HasPrefix(spec, "go:"):
		if len(spec) > 3 && !strings.HasPrefix(spec[3:], "@") {
			return "go"
		}
		return ""
	case npmNameRe.MatchString(spec):
		return "npm"
	}
	return ""
}

// isPathPluginSpec reports whether spec names a directory: relative (with
// either separator), rooted, or a Windows drive or UNC path.
func isPathPluginSpec(spec string) bool {
	if spec == "." || spec == ".." {
		return true
	}
	for _, p := range []string{"./", "../", "/", `.\`, `..\`, `\\`} {
		if strings.HasPrefix(spec, p) {
			return true
		}
	}
	return len(spec) >= 3 && spec[1] == ':' && (spec[2] == '\\' || spec[2] == '/') &&
		(spec[0] >= 'A' && spec[0] <= 'Z' || spec[0] >= 'a' && spec[0] <= 'z')
}

var npmNameRe = regexp.MustCompile(`^(@[a-z0-9][a-z0-9._-]*/)?[a-z0-9][a-z0-9._-]*(/[A-Za-z0-9._-]+)*$`)

var (
	nativeAppIDRe  = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9-]*(\.[A-Za-z][A-Za-z0-9-]*)+$`)
	nativeSemverRe = regexp.MustCompile(`^\d+\.\d+\.\d+(-[0-9A-Za-z.-]+)?(\+[0-9A-Za-z.-]+)?$`)
	nativeSchemeRe = regexp.MustCompile(`^[a-z][a-z0-9+.-]*$`)
	nativeExtRe    = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_+-]{0,31}$`)
	nativeMIMERe   = regexp.MustCompile(`^[a-z0-9][a-z0-9!#$&^_.+-]*/[a-z0-9][a-z0-9!#$&^_.+-]*$`)
)

// EffectiveNative returns the native block with defaults applied: a 1280x800
// resizable window with a native title bar, the quickjs host runtime, compile
// "auto" and all three platforms. It returns nil when no block is configured.
// The result is a copy.
func (c *Config) EffectiveNative() *NativeConfig {
	if c.Native == nil {
		return nil
	}
	n := *c.Native
	if n.Window.Width == 0 {
		n.Window.Width = 1280
	}
	if n.Window.Height == 0 {
		n.Window.Height = 800
	}
	if n.Window.Resizable == nil {
		t := true
		n.Window.Resizable = &t
	}
	if n.Window.TitleBar == "" {
		n.Window.TitleBar = "native"
	}
	if n.Host.Runtime == "" {
		n.Host.Runtime = "quickjs"
	}
	if n.Host.Bundle == nil {
		t := true
		n.Host.Bundle = &t
	}
	if n.Release.Compile == "" {
		n.Release.Compile = "auto"
	}
	if n.Release.PGO == "" {
		n.Release.PGO = "native.pgo"
	}
	if n.Memory == "" {
		n.Memory = "balanced"
	}
	if len(n.Platforms) == 0 {
		n.Platforms = append([]string(nil), NativePlatforms...)
	}
	return &n
}

// nativePermissionCovered reports whether the allowlist grants p (directly or
// through an implication: fs grants fs:read).
func nativePermissionCovered(allow []string, p string) bool {
	for _, a := range allow {
		if a == p || (a == "fs" && p == "fs:read") {
			return true
		}
	}
	return false
}

func oneOf(v string, set []string) bool {
	for _, s := range set {
		if s == v {
			return true
		}
	}
	return false
}

func nativeErr(key, msg, hint string) error {
	return fmt.Errorf("native.%s %s\n  hint: %s", key, msg, hint)
}

// validateNative checks the native block. Errors are values that would break a
// native build; warnings are tolerated.
func (c *Config) validateNative() (warnings []string, err error) {
	n := c.Native
	if n == nil {
		return nil, nil
	}
	if n.AppID == "" {
		warnings = append(warnings, "native.appId is not set; a native build needs a reverse-DNS id such as \"com.acme.app\"")
	} else if !nativeAppIDRe.MatchString(n.AppID) {
		return nil, nativeErr("appId", fmt.Sprintf("must be a reverse-DNS identifier, got %q", n.AppID),
			"use dot-separated segments of letters, digits and hyphens, for example \"com.acme.app\"")
	}
	if n.Version != "" && !nativeSemverRe.MatchString(n.Version) {
		return nil, nativeErr("version", fmt.Sprintf("must be a semantic version, got %q", n.Version), "use MAJOR.MINOR.PATCH, for example \"1.0.0\"")
	}
	for key, v := range map[string]string{"productName": n.ProductName, "bundle.windows.publisher": n.Bundle.Windows.Publisher} {
		if i := strings.IndexFunc(v, func(r rune) bool { return r < 0x20 || r == 0x7f }); i >= 0 {
			return nil, nativeErr(key, fmt.Sprintf("contains a control character (%U), got %q", rune(v[i]), v),
				"the name ends up in package metadata (deb control, desktop entry, installer scripts) and must be a single line")
		}
	}
	for _, p := range n.Platforms {
		if !oneOf(p, NativePlatforms) {
			return nil, nativeErr("platforms", fmt.Sprintf("has unknown platform %q", p), "use windows, macos or linux")
		}
	}
	if n.Window.Width < 0 || n.Window.Height < 0 {
		return nil, nativeErr("window", "width and height must be >= 0", "omit them for the 1280x800 default")
	}
	if n.Window.TitleBar != "" && !oneOf(n.Window.TitleBar, nativeTitleBars) {
		return nil, nativeErr("window.titleBar", fmt.Sprintf("must be \"native\", \"hidden\" or \"none\", got %q", n.Window.TitleBar),
			"\"native\" uses the system title bar, \"hidden\" keeps window controls over the page, \"none\" removes the frame")
	}
	for _, p := range n.Permissions {
		if !IsNativePermission(p) {
			return nil, nativeErr("permissions", fmt.Sprintf("has unknown permission %q", p),
				"known permissions: "+strings.Join(NativePermissions, ", ")+"; a plugin permission is written plugin:<plugin>.<permission>")
		}
	}
	if wp := n.Window.Permissions; wp != nil {
		for _, p := range *wp {
			if !IsNativePermission(p) {
				return nil, nativeErr("window.permissions", fmt.Sprintf("has unknown permission %q", p),
					"known permissions: "+strings.Join(NativePermissions, ", ")+"; a plugin permission is written plugin:<plugin>.<permission>")
			}
			if len(n.Permissions) > 0 && !nativePermissionCovered(n.Permissions, p) {
				return nil, nativeErr("window.permissions", fmt.Sprintf("lists %q, which native.permissions does not allow", p),
					"a window scope narrows the app's permissions; add it to native.permissions or remove it here")
			}
		}
	}
	for _, s := range n.DeepLinks {
		if !nativeSchemeRe.MatchString(s) {
			return nil, nativeErr("deepLinks", fmt.Sprintf("entry %q is not a URL scheme", s),
				"list the scheme only, lowercase and without \"://\", for example \"acme\"")
		}
	}
	if err := validateNativeFileAssociations(n.FileAssociations); err != nil {
		return nil, err
	}
	if n.Host.Runtime != "" && !oneOf(n.Host.Runtime, NativeHostRuntimes) {
		return nil, nativeErr("host.runtime", fmt.Sprintf("must be one of %s, got %q", strings.Join(NativeHostRuntimes, ", "), n.Host.Runtime),
			"\"quickjs\" is embedded and needs no install; node, bun and deno run as a bundled sidecar")
	}
	if n.Host.Runtime == "scriptc" && !n.Experimental.Scriptc {
		return nil, nativeErr("host.runtime", "\"scriptc\" is experimental",
			"set native.experimental.scriptc to true to opt in, or use \"quickjs\"")
	}
	for pkg, rt := range n.Host.Islands {
		if !oneOf(rt, NativeIslandRuntimes) {
			return nil, nativeErr("host.islands", fmt.Sprintf("runtime for %q must be node, bun, deno or quickjs, got %q", pkg, rt),
				"an island runs one package in a different runtime from the main host runtime")
		}
	}
	if n.Memory != "" && !oneOf(n.Memory, NativeMemoryProfiles) {
		return nil, nativeErr("memory", fmt.Sprintf("must be one of %s, got %q", strings.Join(NativeMemoryProfiles, ", "), n.Memory),
			"\"balanced\" (default) trims the webview, \"minimal\" trims harder at some CPU cost, \"performance\" keeps the engine defaults")
	}
	for key, v := range map[string]string{
		"camera": n.Media.Camera, "microphone": n.Media.Microphone, "screen": n.Media.Screen,
		"location": n.Media.Location, "notifications": n.Media.Notifications, "clipboardRead": n.Media.ClipboardRead,
	} {
		if v != "" && !oneOf(v, NativeMediaPolicies) {
			return nil, nativeErr("media."+key, fmt.Sprintf("must be one of %s, got %q", strings.Join(NativeMediaPolicies, ", "), v),
				"\"prompt\" asks with a native dialog, \"allow\" and \"deny\" decide without asking")
		}
	}
	for key, v := range map[string]string{
		"media.usage.camera": n.Media.Usage.Camera, "media.usage.microphone": n.Media.Usage.Microphone,
		"media.usage.location": n.Media.Usage.Location, "media.usage.screenCapture": n.Media.Usage.ScreenCapture,
	} {
		if i := strings.IndexFunc(v, func(r rune) bool { return r < 0x20 || r == 0x7f }); i >= 0 {
			return nil, nativeErr(key, fmt.Sprintf("contains a control character (%U), got %q", rune(v[i]), v),
				"a usage description is one line of text that ends up in Info.plist")
		}
	}
	if n.ViewTransitions != "" && n.ViewTransitions != "auto" {
		return nil, nativeErr("viewTransitions", fmt.Sprintf("must be \"auto\" or omitted, got %q", n.ViewTransitions),
			"a native build keeps View Transitions off unless you set \"auto\"")
	}
	if n.Host.MemoryMB < 0 {
		return nil, nativeErr("host.memoryMB", fmt.Sprintf("must be >= 0, got %d", n.Host.MemoryMB), "omit it for the profile default")
	}
	if n.Release.Compile != "" && !oneOf(n.Release.Compile, nativeCompileModes) {
		return nil, nativeErr("release.compile", fmt.Sprintf("must be \"auto\", \"always\" or \"never\", got %q", n.Release.Compile),
			"\"auto\" compiles host code when the runtime supports it")
	}
	if in := n.Bundle.Windows.Installer; in != "" && !oneOf(in, nativeInstallers) {
		return nil, nativeErr("bundle.windows.installer", fmt.Sprintf("must be \"msix\" or \"nsis\", got %q", in), "pick one installer format")
	}
	if m := n.Bundle.Windows.WebView2; m != "" && !oneOf(m, nativeWebView2Modes) {
		return nil, nativeErr("bundle.windows.webview2", fmt.Sprintf("must be \"download\", \"embed\", \"fixed\" or \"none\", got %q", m),
			"\"download\" (default) fetches Microsoft's bootstrapper at install time, \"embed\" ships it in the installer, \"fixed\" ships a runtime folder, \"none\" leaves it to the app")
	}
	if m, p := n.Bundle.Windows.WebView2, n.Bundle.Windows.WebView2Path; (m == "embed" || m == "fixed") && p == "" {
		return nil, nativeErr("bundle.windows.webview2Path", fmt.Sprintf("is required when webview2 is %q", m),
			"set it to MicrosoftEdgeWebview2Setup.exe (embed) or the extracted Fixed Version runtime folder (fixed); download them from https://developer.microsoft.com/microsoft-edge/webview2/")
	} else if p != "" && m != "embed" && m != "fixed" {
		return nil, nativeErr("bundle.windows.webview2Path", "is only used when webview2 is \"embed\" or \"fixed\"", "remove it or set webview2")
	}
	for _, f := range n.Bundle.Linux.Formats {
		if !oneOf(f, nativeLinuxFormats) {
			return nil, nativeErr("bundle.linux.formats", fmt.Sprintf("has unknown format %q", f), "use deb, rpm, appimage, flatpak or snap")
		}
	}
	if err := validateNativeDistribution(n); err != nil {
		return nil, err
	}
	seenPlugin := map[string]bool{}
	for i, spec := range n.Plugins {
		if NativePluginSpecKind(spec) == "" {
			return nil, nativeErr("plugins", fmt.Sprintf("entry %d (%q) is not a plugin reference", i, spec),
				"use a directory (\"./native-plugins/hello\"), an npm package (\"@acme/krate-sqlite\") or a Go module (\"go:github.com/acme/krate-sqlite@v1.2.0\")")
		}
		if seenPlugin[spec] {
			return nil, nativeErr("plugins", fmt.Sprintf("lists %q twice", spec), "remove the duplicate entry")
		}
		seenPlugin[spec] = true
	}
	if u := n.Updater; u != nil {
		if u.URL == "" || u.PublicKey == "" {
			return nil, nativeErr("updater", "needs both url and publicKey",
				"updates are verified with the public key; set both, or remove the updater block")
		}
		if !strings.HasPrefix(u.URL, "https://") {
			return nil, nativeErr("updater.url", fmt.Sprintf("must be an https URL, got %q", u.URL),
				"use a URL such as \"https://acme.com/updates/{target}.json\"")
		}
		if u.Channel != "" && !nativeChannelRe.MatchString(u.Channel) {
			return nil, nativeErr("updater.channel", fmt.Sprintf("%q is not a channel name", u.Channel),
				"use lowercase letters, digits, dots, underscores and dashes, for example \"beta\"")
		}
		if u.MaxStarts < 0 || u.HealthTimeout < 0 {
			return nil, nativeErr("updater", "maxStarts and healthTimeout cannot be negative", "omit them for the defaults (1 start, 60 seconds)")
		}
		if u.Confirm != "" && u.Confirm != "auto" && u.Confirm != "app" {
			return nil, nativeErr("updater.confirm", fmt.Sprintf("%q is not \"auto\" or \"app\"", u.Confirm),
				"\"auto\" lets the shell confirm a healthy start; \"app\" waits for updater.markHealthy()")
		}
	}
	return warnings, nil
}

// validateNativeFileAssociations checks native.fileAssociations.
func validateNativeFileAssociations(list []NativeFileAssociation) error {
	seenExt := map[string]int{}
	seenMIME := map[string]int{}
	for i, fa := range list {
		key := fmt.Sprintf("fileAssociations[%d]", i)
		if len(fa.Ext) == 0 {
			return nativeErr(key+".ext", "needs at least one extension", `list the extensions without the dot, for example ext: ["md", "markdown"]`)
		}
		for _, e := range fa.Ext {
			if !nativeExtRe.MatchString(e) {
				hint := "use letters, digits, \"_\", \"+\" and \"-\" only, without a dot or a wildcard, for example \"md\""
				if strings.HasPrefix(e, ".") || strings.HasPrefix(e, "*") {
					hint = fmt.Sprintf("write the extension without the leading dot or wildcard: %q", strings.TrimLeft(e, ".*"))
				}
				return nativeErr(key+".ext", fmt.Sprintf("entry %q is not a file extension", e), hint)
			}
			l := strings.ToLower(e)
			if oneOf(l, nativeNeverAssociate) {
				return nativeErr(key+".ext", fmt.Sprintf("%q is an executable or shortcut type the app cannot claim", e),
					"associate document types only; claiming launcher types would make every such file open in the app")
			}
			if j, dup := seenExt[l]; dup {
				return nativeErr(key+".ext", fmt.Sprintf("extension %q is already listed by fileAssociations[%d]", e, j), "list each extension once")
			}
			seenExt[l] = i
		}
		if i := strings.IndexFunc(fa.Name, func(r rune) bool { return r < 0x20 || r == 0x7f }); i >= 0 {
			return nativeErr(key+".name", fmt.Sprintf("contains a control character (%U), got %q", rune(fa.Name[i]), fa.Name),
				"the name ends up in the Info.plist, the registry and the MIME database and must be a single line")
		}
		if fa.MIME != "" {
			if !nativeMIMERe.MatchString(fa.MIME) {
				return nativeErr(key+".mime", fmt.Sprintf("is not a media type, got %q", fa.MIME),
					"use lowercase type/subtype such as \"text/markdown\" or \"application/x-acme-doc\"")
			}
			if j, dup := seenMIME[fa.MIME]; dup {
				return nativeErr(key+".mime", fmt.Sprintf("media type %q is already used by fileAssociations[%d]", fa.MIME, j),
					"merge the two entries into one with both extensions")
			}
			seenMIME[fa.MIME] = i
		}
		if fa.Role != "" && !oneOf(fa.Role, NativeFileRoles) {
			return nativeErr(key+".role", fmt.Sprintf("must be \"editor\" or \"viewer\", got %q", fa.Role),
				"\"editor\" can open and save the file, \"viewer\" only shows it")
		}
		if fa.Icon != "" {
			switch strings.ToLower(filepath.Ext(fa.Icon)) {
			case ".png", ".jpg", ".jpeg":
			default:
				return nativeErr(key+".icon", fmt.Sprintf("must be a PNG or JPEG image, got %q", fa.Icon),
					"packaging builds the .icns, .ico and PNG sizes from one image; use a 512x512 or larger PNG")
			}
		}
	}
	return nil
}


var (
	nativeTeamIDRe     = regexp.MustCompile(`^[A-Z0-9]{10}$`)
	nativeStoreNameRe  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9.-]{2,49}$`)
	nativeStorePubRe   = regexp.MustCompile(`^CN=[^,=]+(,\s*[A-Za-z]+=[^,]+)*$`)
	nativeRuntimeVerRe = regexp.MustCompile(`^[0-9]+(\.[0-9]+)?$`)
	nativeFlatpakRtRe  = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]*(\.[A-Za-z][A-Za-z0-9_-]*)+$`)
	nativeSPDXRe       = regexp.MustCompile(`^[A-Za-z0-9.+-]+( (AND|OR|WITH) [A-Za-z0-9.+-]+)*$`)
)

// validateNativeDistribution checks the store and sandbox packaging keys:
// bundle.macos.mas, bundle.windows.store, bundle.linux flatpak and snap.
func validateNativeDistribution(n *NativeConfig) error {
	mas := n.Bundle.MacOS.MAS
	if mas.TeamID != "" && !nativeTeamIDRe.MatchString(mas.TeamID) {
		return nativeErr("bundle.macos.mas.teamId", fmt.Sprintf("%q is not a 10-character team id", mas.TeamID),
			"copy the Team ID from developer.apple.com/account (ten capital letters and digits, such as ABCDE12345)")
	}
	for _, v := range mas.Network {
		if v != "client" && v != "server" {
			return nativeErr("bundle.macos.mas.network", fmt.Sprintf("has unknown entry %q", v), "use \"client\" and/or \"server\"")
		}
	}
	if f := mas.Files; f != "" && f != "read-write" && f != "read-only" && f != "none" {
		return nativeErr("bundle.macos.mas.files", fmt.Sprintf("must be \"read-write\", \"read-only\" or \"none\", got %q", f),
			"it sets the entitlement for files the user picks in an open or save panel")
	}
	st := n.Bundle.Windows.Store
	if st.IdentityName != "" && !nativeStoreNameRe.MatchString(st.IdentityName) {
		return nativeErr("bundle.windows.store.identityName", fmt.Sprintf("%q is not a package identity name", st.IdentityName),
			"copy Package/Identity/Name from Partner Center (3 to 50 letters, digits, dots and dashes, such as 12345Acme.AcmeNotes)")
	}
	if st.PublisherID != "" && !nativeStorePubRe.MatchString(st.PublisherID) {
		return nativeErr("bundle.windows.store.publisherId", fmt.Sprintf("%q is not a publisher distinguished name", st.PublisherID),
			"copy Package/Identity/Publisher from Partner Center, for example \"CN=01234567-89AB-CDEF-0123-456789ABCDEF\"")
	}
	if st.PublisherDisplayName != "" && strings.ContainsAny(st.PublisherDisplayName, "\r\n<>") {
		return nativeErr("bundle.windows.store.publisherDisplayName", "contains a line break or angle bracket",
			"use the publisher display name from Partner Center")
	}
	l := n.Bundle.Linux
	if l.License != "" && !nativeSPDXRe.MatchString(l.License) {
		return nativeErr("bundle.linux.license", fmt.Sprintf("%q is not an SPDX license expression", l.License),
			"use an id such as \"MIT\" or \"GPL-3.0-or-later\"; \"LicenseRef-proprietary\" for closed source")
	}
	if l.Homepage != "" && !strings.HasPrefix(l.Homepage, "https://") && !strings.HasPrefix(l.Homepage, "http://") {
		return nativeErr("bundle.linux.homepage", fmt.Sprintf("must be an http(s) URL, got %q", l.Homepage), "AppStream metadata links to it")
	}
	for k, v := range map[string]string{"summary": l.Summary} {
		if strings.ContainsAny(v, "\r\n") {
			return nativeErr("bundle.linux."+k, "must be one line", "the long text goes in bundle.linux.description")
		}
	}
	if r := l.Flatpak.Runtime; r != "" && !nativeFlatpakRtRe.MatchString(r) {
		return nativeErr("bundle.linux.flatpak.runtime", fmt.Sprintf("%q is not a runtime id", r), "use org.gnome.Platform (WebKitGTK 4.1 ships with it) or org.kde.Platform")
	}
	if v := l.Flatpak.RuntimeVersion; v != "" && !nativeRuntimeVerRe.MatchString(v) {
		return nativeErr("bundle.linux.flatpak.runtimeVersion", fmt.Sprintf("%q is not a runtime branch", v), "use a GNOME release number such as \"48\"")
	}
	for _, a := range l.Flatpak.FinishArgs {
		if !strings.HasPrefix(a, "--") {
			return nativeErr("bundle.linux.flatpak.finishArgs", fmt.Sprintf("%q is not a finish-arg", a), "finish-args start with --, for example \"--filesystem=xdg-documents\"")
		}
	}
	if g := l.Snap.Grade; g != "" && g != "stable" && g != "devel" {
		return nativeErr("bundle.linux.snap.grade", fmt.Sprintf("must be \"stable\" or \"devel\", got %q", g), "the Snap Store lists only stable grade in the stable channel")
	}
	if c := l.Snap.Confinement; c != "" && c != "strict" && c != "classic" && c != "devmode" {
		return nativeErr("bundle.linux.snap.confinement", fmt.Sprintf("must be \"strict\", \"classic\" or \"devmode\", got %q", c), "strict is what the store prefers")
	}
	return nil
}
