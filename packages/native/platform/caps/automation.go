package caps

// RemoteDebug asks the engine for a remote debugging endpoint for tests and
// debugging sessions. The shell sets it only in dev runs and for apps built
// with native.debug.
type RemoteDebug struct {
	Enabled bool
	// Port is the TCP port to listen on (loopback only); zero picks 9222.
	Port int
}

// Endpoint describes the remote debugging endpoint an App started.
type Endpoint struct {
	// Kind is "cdp" (Chrome DevTools Protocol over HTTP and WebSocket, WebView2),
	// "webkit-inspector" (the WebKit remote inspector served over HTTP, which
	// Playwright and Puppeteer cannot drive), "safari" (macOS: Safari's Develop
	// menu, no network endpoint) or "" when there is none.
	Kind string
	// URL is where to point a client: http://127.0.0.1:9222 for "cdp" (use it
	// with connectOverCDP), the inspector page for "webkit-inspector".
	URL string
	// Port is the TCP port, zero for "safari".
	Port int
}

// Automation is implemented by Apps that expose a remote debugging endpoint
// and can send DevTools protocol commands.
type Automation interface {
	// AutomationEndpoint reports the endpoint, or the zero Endpoint when
	// remote debugging is off.
	AutomationEndpoint() Endpoint
}

// DevToolsProtocol is implemented by Apps whose engine speaks the Chrome
// DevTools Protocol in process (WebView2: CallDevToolsProtocolMethod).
type DevToolsProtocol interface {
	// CDP runs one protocol method in a view; paramsJSON is "{}" for none. It
	// returns the result object as JSON.
	CDP(v ViewRef, method, paramsJSON string) (resultJSON string, err error)
	// OnCDPEvent calls fn with the parameters (JSON) of each event with this
	// name, until cancel is called.
	OnCDPEvent(v ViewRef, event string, fn func(paramsJSON string)) (cancel func(), err error)
}
