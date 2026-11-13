// Package automation exposes the engine's remote debugging endpoint and, on
// WebView2, in-process DevTools protocol commands, for tests and debugging
// sessions.
//
// It is off unless the app runs in dev or was built with native.debug; a
// release build without it never starts an endpoint, and the ids answer
// krate.ErrUnsupported. What is on offer depends on the engine:
//
//   - WebView2 (Windows): the Chrome DevTools Protocol. The endpoint is
//     --remote-debugging-port on loopback, which Playwright
//     (chromium.connectOverCDP) and Puppeteer (puppeteer.connect) attach to,
//     and CDP sends one protocol method in process through
//     CallDevToolsProtocolMethod.
//   - WebKitGTK (Linux): the WebKit remote inspector over HTTP
//     (WEBKIT_INSPECTOR_HTTP_SERVER). It serves the inspector UI to a browser;
//     it is not CDP, so Playwright and Puppeteer cannot attach, and CDP is
//     ErrUnsupported.
//   - WKWebView (macOS): isInspectable, so Safari's Develop menu can inspect
//     the app. There is no network endpoint and no CDP.
//
// Permission: "automation".
package automation

import (
	"context"
	"encoding/json"
	"sync/atomic"

	"github.com/kratejs/krate/packages/native/sdk/internal/bridge"
	"github.com/kratejs/krate/packages/native/sdk/internal/jsonwire"
	"github.com/kratejs/krate/packages/native/wire"
)

// Permission is the app permission the UI needs.
const Permission = "automation"

// Host function ids.
const (
	IDEndpoint  = "krate/automation.endpoint"
	IDCDP       = "krate/automation.cdp"
	IDCDPEvents = "krate/automation.cdpEvents"
)

var enabled atomic.Bool

// Configure says whether automation is allowed: the shell passes true for dev
// runs and apps built with native.debug.
func Configure(on bool) { enabled.Store(on) }

// Endpoint describes what a client can attach to.
type Endpoint struct {
	// Enabled is false in a release build without native.debug.
	Enabled bool `json:"enabled"`
	// Kind is "cdp", "webkit-inspector", "safari" or "" when there is none.
	Kind string `json:"kind"`
	URL  string `json:"url"`
	Port int    `json:"port"`
	// CDP is true when CDP can send protocol methods in process.
	CDP bool `json:"cdp"`
}

// GetEndpoint returns the debugging endpoint of the running app.
func GetEndpoint() (Endpoint, error) { return jsonwire.Query[Endpoint](IDEndpoint) }

// CDP runs a DevTools protocol method in the window or view ("" is the main
// window) and returns the result object as JSON. params may be nil. It
// returns krate.ErrUnsupported unless the engine speaks CDP (WebView2).
func CDP(view, method string, params any) (json.RawMessage, error) {
	pb := []byte("{}")
	if params != nil {
		var err error
		if pb, err = json.Marshal(params); err != nil {
			return nil, err
		}
	}
	d, err := bridge.Call(context.Background(), IDCDP, func(e *wire.Encoder) { e.String(method); e.Raw(pb); e.String(view) })
	if err != nil {
		return nil, err
	}
	raw, err := d.RawValue()
	return append(json.RawMessage(nil), raw...), err
}

// OnCDPEvent calls fn with the parameters of each CDP event with this name in
// the view, until cancel is called.
func OnCDPEvent(view, event string, fn func(params json.RawMessage)) (cancel func(), err error) {
	return bridge.SubscribeArgs(context.Background(), IDCDPEvents, func(e *wire.Encoder) { e.String(event); e.String(view) }, func(d *wire.Decoder) {
		if raw, err := d.RawValue(); err == nil {
			fn(append(json.RawMessage(nil), raw...))
		}
	})
}
