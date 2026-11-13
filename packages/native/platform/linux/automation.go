//go:build linux

package linux

import (
	"fmt"
	"os"

	"github.com/ebitengine/purego"

	"github.com/kratejs/krate/packages/native/platform/caps"
)

var _ caps.Automation = (*App)(nil)

// startRemoteInspector enables the WebKit remote inspector HTTP server. WebKit
// reads WEBKIT_INSPECTOR_HTTP_SERVER when it starts its web processes, so it
// must be set before the first webview exists. The server speaks the WebKit
// Inspector protocol and serves the inspector UI: a browser can open it, but
// Playwright and Puppeteer cannot drive it (they need CDP, which WebKitGTK
// does not speak).
func startRemoteInspector(r caps.RemoteDebug) caps.Endpoint {
	if !r.Enabled {
		return caps.Endpoint{}
	}
	port := r.Port
	if port == 0 {
		port = 9222
	}
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	if err := os.Setenv("WEBKIT_INSPECTOR_HTTP_SERVER", addr); err != nil {
		return caps.Endpoint{}
	}
	preinitGIONetworking()
	return caps.Endpoint{Kind: "webkit-inspector", URL: "http://" + addr, Port: port}
}

// AutomationEndpoint reports the remote inspector endpoint, if any.
func (a *App) AutomationEndpoint() caps.Endpoint { return a.endpoint }

// preinitGIONetworking creates GIO's session bus connection, network monitor
// and proxy resolver on the main thread before GTK starts. With the inspector
// on, WebKit opens a socket while creating the website data manager, which
// resolves the system proxy; where that goes through libproxy (Ubuntu), the
// resolver asks for the session bus while the dconf worker thread GTK started
// is connecting it and waits for a GObject type lock the main thread holds:
// a deadlock (seen on the ubuntu-24.04 runners). Made here, before any other
// thread exists, the singletons are ready and nobody waits.
func preinitGIONetworking() {
	gio, err := purego.Dlopen("libgio-2.0.so.0", rtldNow|rtldGlobal)
	if err != nil {
		return
	}
	var busGetSync func(busType int32, cancellable, errp uintptr) uintptr
	var monitorDefault, resolverDefault func() uintptr
	b := &binder{}
	b.bind(&busGetSync, gio, "g_bus_get_sync")
	b.bind(&monitorDefault, gio, "g_network_monitor_get_default")
	b.bind(&resolverDefault, gio, "g_proxy_resolver_get_default")
	if len(b.missing) > 0 {
		return
	}
	const busSession = 2 // G_BUS_TYPE_SESSION
	// The connection is a process-wide singleton; failing without a session
	// bus is fine (the error is not read).
	busGetSync(busSession, 0, 0)
	monitorDefault()
	resolverDefault()
}
