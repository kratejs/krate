package shell

import (
	"net/http"

	"github.com/kratejs/krate/packages/native/platform/caps"
)

// inspectorPath is the endpoint the keyboard shortcut script posts to. It is
// served only when the developer tools are on (krate native dev, or
// native.debug in the config).
const inspectorPath = "/__krate/inspector"

// inspectorKeyScript opens the web inspector of the view it runs in on F12,
// Ctrl+Shift+I, or Cmd+Option+I. The platforms open it themselves from the
// context menu (Inspect Element) and, on Windows, from F12 and Ctrl+Shift+I;
// WebKitGTK and WKWebView have no shortcut of their own. The key code, not
// the character, is checked because Option+I types a different character on
// macOS.
const inspectorKeyScript = `<script>(function(){try{addEventListener("keydown",function(e){` +
	`if(e.key==="F12"||(e.ctrlKey&&e.shiftKey&&e.code==="KeyI")||(e.metaKey&&e.altKey&&e.code==="KeyI")){` +
	`e.preventDefault();fetch("` + inspectorPath + `",{method:"POST"}).catch(function(){})}},true)}catch(e){}})()</script>`

// serveInspector opens the inspector of the view the request came from.
func (a *app) serveInspector(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	p := a.plat.Load()
	ref, ok := caps.ViewOf(r)
	if p == nil || !ok {
		http.Error(w, "the web inspector needs a native window", http.StatusNotImplemented)
		return
	}
	o, ok := (*p).(caps.InspectorOpener)
	if !ok {
		http.Error(w, "this platform cannot open the web inspector from code", http.StatusNotImplemented)
		return
	}
	if err := o.OpenInspector(ref); err != nil {
		a.logf("web inspector: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	a.logf("web inspector opened")
	w.WriteHeader(http.StatusNoContent)
}
