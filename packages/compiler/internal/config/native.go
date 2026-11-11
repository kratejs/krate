package config

import (
	_ "embed"
	"encoding/json"
	"fmt"
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
}

// NativeBundleWindows holds Windows packaging options.
type NativeBundleWindows struct {
	Publisher string `json:"publisher,omitempty"`
	// Installer is "msix" or "nsis".
	Installer string `json:"installer,omitempty"`
}

// NativeBundleLinux holds Linux packaging options.
type NativeBundleLinux struct {
	// Formats lists "deb", "rpm" and "appimage".
	Formats []string `json:"formats,omitempty"`
}

// NativeUpdater configures in-app updates.
type NativeUpdater struct {
	// URL is the update manifest URL; {target} expands to the platform.
	URL string `json:"url,omitempty"`
	// PublicKey verifies update signatures.
	PublicKey string `json:"publicKey,omitempty"`
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
	nativeLinuxFormats   = []string{"deb", "rpm", "appimage"}
)

var (
	nativeAppIDRe  = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9-]*(\.[A-Za-z][A-Za-z0-9-]*)+$`)
	nativeSemverRe = regexp.MustCompile(`^\d+\.\d+\.\d+(-[0-9A-Za-z.-]+)?(\+[0-9A-Za-z.-]+)?$`)
	nativeSchemeRe = regexp.MustCompile(`^[a-z][a-z0-9+.-]*$`)
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
		if !oneOf(p, NativePermissions) {
			return nil, nativeErr("permissions", fmt.Sprintf("has unknown permission %q", p),
				"known permissions: "+strings.Join(NativePermissions, ", "))
		}
	}
	if wp := n.Window.Permissions; wp != nil {
		for _, p := range *wp {
			if !oneOf(p, NativePermissions) {
				return nil, nativeErr("window.permissions", fmt.Sprintf("has unknown permission %q", p),
					"known permissions: "+strings.Join(NativePermissions, ", "))
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
	if n.Release.Compile != "" && !oneOf(n.Release.Compile, nativeCompileModes) {
		return nil, nativeErr("release.compile", fmt.Sprintf("must be \"auto\", \"always\" or \"never\", got %q", n.Release.Compile),
			"\"auto\" compiles host code when the runtime supports it")
	}
	if in := n.Bundle.Windows.Installer; in != "" && !oneOf(in, nativeInstallers) {
		return nil, nativeErr("bundle.windows.installer", fmt.Sprintf("must be \"msix\" or \"nsis\", got %q", in), "pick one installer format")
	}
	for _, f := range n.Bundle.Linux.Formats {
		if !oneOf(f, nativeLinuxFormats) {
			return nil, nativeErr("bundle.linux.formats", fmt.Sprintf("has unknown format %q", f), "use deb, rpm or appimage")
		}
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
	}
	return warnings, nil
}
