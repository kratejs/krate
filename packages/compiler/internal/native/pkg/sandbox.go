package pkg

import (
	"sort"
	"strings"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

// effectivePermissions is the app's SDK permission list, or the defaults for
// an app.json that lists none.
func (m Metadata) effectivePermissions() []string {
	if len(m.Permissions) > 0 {
		return m.Permissions
	}
	return config.NativeDefaultPermissions()
}

// hasPerm reports whether the app has the permission name or one of its
// narrower forms ("os:secrets" covers "os:secrets.read").
func (m Metadata) hasPerm(name string) bool {
	for _, p := range m.effectivePermissions() {
		if p == name || strings.HasPrefix(p, name+":") || strings.HasPrefix(p, name+".") {
			return true
		}
	}
	return false
}

func (m Metadata) usesJSHost() bool {
	for _, r := range m.HostRuntimes {
		if r == "node" || r == "bun" || r == "deno" {
			return true
		}
	}
	return false
}

// needsNetwork reports whether the app talks to the network: the net SDK,
// location (WebKit asks GeoClue over the network), media (WebRTC) or the
// updater.
func (m Metadata) needsNetwork() bool {
	return m.hasPerm("net") || m.hasPerm("location") || m.hasPerm("media") || m.hasPerm("updater") ||
		m.usesMedia("camera") || m.usesMedia("microphone") || m.usesMedia("location")
}

// flatpakFinishArgs returns the finish-args of the manifest: the sandbox holes
// the app's permissions need, sorted, followed by native.bundle.linux.flatpak
// .finishArgs. The window and the web view need Wayland or X11, the render
// device and shared memory; everything else follows the permissions.
func flatpakFinishArgs(m Metadata) []string {
	args := map[string]bool{
		"--share=ipc":           true,
		"--socket=wayland":      true,
		"--socket=fallback-x11": true,
		"--device=dri":          true,
		// Audio of the page (WebKitGTK plays through PulseAudio or PipeWire).
		"--socket=pulseaudio": true,
	}
	if m.needsNetwork() {
		args["--share=network"] = true
	}
	if m.hasPerm("notify") {
		args["--talk-name=org.freedesktop.Notifications"] = true
	}
	if m.hasPerm("tray") {
		args["--talk-name=org.kde.StatusNotifierWatcher"] = true
	}
	if m.hasPerm("os:secrets") {
		args["--talk-name=org.freedesktop.secrets"] = true
	}
	if m.hasPerm("media") || m.usesMedia("camera") || m.usesMedia("microphone") || m.usesMedia("screen") {
		// Camera, microphone and screen capture reach WebKit through
		// PipeWire; the portals ask the user.
		args["--filesystem=xdg-run/pipewire-0"] = true
	}
	if m.hasPerm("location") || m.usesMedia("location") {
		args["--system-talk-name=org.freedesktop.GeoClue2"] = true
	}
	if m.hasPerm("power") {
		args["--talk-name=org.freedesktop.ScreenSaver"] = true
	}
	out := make([]string, 0, len(args))
	for a := range args {
		out = append(out, a)
	}
	sort.Strings(out)
	for _, a := range m.Flatpak.FinishArgs {
		if !args[a] {
			out = append(out, a)
		}
	}
	return out
}

// snapPlugs returns the plugs of the snap's app: the ones every desktop app
// needs, those the permissions need, then native.bundle.linux.snap.plugs.
func snapPlugs(m Metadata) []string {
	plugs := map[string]bool{
		"desktop": true, "desktop-legacy": true, "wayland": true, "x11": true,
		"opengl": true, "audio-playback": true, "home": true,
	}
	if m.needsNetwork() {
		plugs["network"] = true
	}
	if m.hasPerm("tray") || m.hasPerm("notify") {
		// unity7 carries the StatusNotifierItem and notification names that
		// a strict snap may own and talk to.
		plugs["unity7"] = true
	}
	if m.hasPerm("os:secrets") {
		plugs["password-manager-service"] = true
	}
	if m.hasPerm("power:inhibit") {
		plugs["screen-inhibit-control"] = true
	}
	if m.usesMedia("camera") {
		plugs["camera"] = true
	}
	if m.usesMedia("microphone") {
		plugs["audio-record"] = true
	}
	if m.hasPerm("fs") || m.hasPerm("dialog") {
		plugs["removable-media"] = true
	}
	out := make([]string, 0, len(plugs))
	for p := range plugs {
		out = append(out, p)
	}
	sort.Strings(out)
	for _, p := range m.Snap.Plugs {
		if !plugs[p] {
			out = append(out, p)
		}
	}
	return out
}

// masEntitlements returns the key/value entitlements of the sandboxed Mac App
// Store app (not the nested helpers; see masChildEntitlements). Values are
// true unless stated.
func masEntitlements(m Metadata) []plistEntry {
	var out []plistEntry
	add := func(k string) { out = append(out, plistEntry{Key: k}) }
	add("com.apple.security.app-sandbox")
	net := m.MAS.Network
	if len(net) == 0 {
		net = []string{"client"}
	}
	for _, n := range net {
		add("com.apple.security.network." + n)
	}
	switch m.MAS.Files {
	case "none":
	case "read-only":
		add("com.apple.security.files.user-selected.read-only")
	default:
		add("com.apple.security.files.user-selected.read-write")
	}
	if m.usesMedia("camera") || m.MediaUsage.Camera != "" {
		add("com.apple.security.device.camera")
	}
	if m.usesMedia("microphone") || m.MediaUsage.Microphone != "" {
		add("com.apple.security.device.audio-input")
	}
	if m.usesMedia("location") || m.MediaUsage.Location != "" {
		add("com.apple.security.personal-information.location")
	}
	if m.usesJSHost() {
		add("com.apple.security.cs.allow-jit")
		add("com.apple.security.cs.allow-unsigned-executable-memory")
	}
	if m.MAS.TeamID != "" {
		out = append(out,
			plistEntry{Key: "com.apple.application-identifier", Str: m.MAS.TeamID + "." + m.AppID},
			plistEntry{Key: "com.apple.developer.team-identifier", Str: m.MAS.TeamID})
	}
	return out
}

// masChildEntitlements are the entitlements of a helper executable inside the
// app (a compiled host child): the sandbox, inherited from the parent, and the
// JIT grants of a JavaScript runtime.
func masChildEntitlements(m Metadata, jit bool) []plistEntry {
	out := []plistEntry{{Key: "com.apple.security.app-sandbox"}, {Key: "com.apple.security.inherit"}}
	if jit {
		out = append(out, plistEntry{Key: "com.apple.security.cs.allow-jit"}, plistEntry{Key: "com.apple.security.cs.allow-unsigned-executable-memory"})
	}
	return out
}

// plistEntry is one entitlements entry: a boolean true, or a string.
type plistEntry struct {
	Key string
	Str string
}

// entitlementsPlist renders entries as a plist. Keys keep the given order.
func entitlementsPlist(entries []plistEntry) []byte {
	var b strings.Builder
	b.WriteString("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n")
	b.WriteString("<!DOCTYPE plist PUBLIC \"-//Apple//DTD PLIST 1.0//EN\" \"http://www.apple.com/DTDs/PropertyList-1.0.dtd\">\n")
	b.WriteString("<plist version=\"1.0\">\n<dict>\n")
	for _, e := range entries {
		b.WriteString("\t<key>" + xmlEscape(e.Key) + "</key>\n")
		if e.Str != "" {
			b.WriteString("\t<string>" + xmlEscape(e.Str) + "</string>\n")
		} else {
			b.WriteString("\t<true/>\n")
		}
	}
	b.WriteString("</dict>\n</plist>\n")
	return []byte(b.String())
}
