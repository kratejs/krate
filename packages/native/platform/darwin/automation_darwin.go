//go:build darwin

package darwin

import "github.com/kratejs/krate/packages/native/platform/caps"

var _ caps.Automation = (*App)(nil)

// AutomationEndpoint reports Safari's remote inspection. WKWebView has no
// network endpoint and no DevTools protocol: with Debug on, every web view is
// inspectable (isInspectable) and appears in Safari's Develop menu under the
// computer's name. Safari's WebDriver (safaridriver) drives Safari itself, not
// an embedded web view.
func (a *App) AutomationEndpoint() caps.Endpoint {
	if a.opts.Remote.Enabled && a.opts.Debug {
		return caps.Endpoint{Kind: "safari"}
	}
	return caps.Endpoint{}
}
