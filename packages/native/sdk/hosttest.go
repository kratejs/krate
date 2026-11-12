package sdk

import (
	"fmt"

	"github.com/kratejs/krate/packages/native/platform/caps"
	"github.com/kratejs/krate/packages/native/sdk/internal/bridge"
)

// ViewInfo describes one webview of an open window.
type ViewInfo struct {
	// ID is the view's SDK id; a window's primary view has the window's id.
	ID string `json:"id"`
	// Window is the SDK id of the window that holds the view.
	Window  string       `json:"window"`
	Primary bool         `json:"primary"`
	Ref     caps.ViewRef `json:"-"`
}

// Views lists the webviews of every open window, windows in creation order
// and each window's primary view first.
func (h *Host) Views() []ViewInfo {
	h.mu.Lock()
	env := h.env
	h.mu.Unlock()
	if env == nil {
		return nil
	}
	var out []ViewInfo
	for _, r := range env.Windows.ViewsOf("") {
		out = append(out, ViewInfo{ID: r.ID, Window: r.Window, Primary: r.Primary, Ref: r.Ref})
	}
	return out
}

// EvalView runs JavaScript in the view with this SDK id without waiting for
// a result.
func (h *Host) EvalView(id, js string) error {
	h.mu.Lock()
	env := h.env
	h.mu.Unlock()
	if env == nil {
		return fmt.Errorf("no platform is attached")
	}
	rec, ok := env.Windows.View(id)
	if !ok {
		return fmt.Errorf("no view %q", id)
	}
	win, _, ok := env.Windows.Get(rec.Window)
	if !ok {
		return fmt.Errorf("no window %q", rec.Window)
	}
	wv, ok := win.(caps.WindowViews)
	if !ok {
		return fmt.Errorf("window %q cannot address views", rec.Window)
	}
	return wv.EvalView(rec.Ref, js)
}

// Inject delivers a native event as if the platform had produced it, for
// tests that drive an app through the dev control channel. payload is a JSON
// object; area is "tray" ({"kind":"click","id":""}), "menu" ({"id":"file.open",
// "window":"main","checked":false}) or "shortcut" ({"accelerator":"CmdOrCtrl+K"}
// or {"id":"<handle>"}). An area a build left out cannot be injected.
func (h *Host) Inject(area string, payload []byte) error {
	h.mu.Lock()
	env := h.env
	h.mu.Unlock()
	if env == nil {
		return fmt.Errorf("no platform is attached")
	}
	f, ok := env.Providers[bridge.InjectKey(area)].(func([]byte) error)
	if !ok {
		return fmt.Errorf("cannot emit %q events (tray, menu and shortcut can be emitted, when the app includes them)", area)
	}
	return f(payload)
}
