//go:build linux

package linux

import (
	"fmt"
	"os"

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
	return caps.Endpoint{Kind: "webkit-inspector", URL: "http://" + addr, Port: port}
}

// AutomationEndpoint reports the remote inspector endpoint, if any.
func (a *App) AutomationEndpoint() caps.Endpoint { return a.endpoint }
