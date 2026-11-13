//go:build windows

package windows

import (
	"errors"
	"fmt"

	"github.com/kratejs/krate/packages/native/platform/caps"
)

var _ caps.InspectorOpener = (*App)(nil)

// OpenInspector opens the DevTools window of a view. WebView2 also handles
// F12 and Ctrl+Shift+I itself while the developer tools are enabled.
func (a *App) OpenInspector(ref caps.ViewRef) (err error) {
	if !a.cfg.Debug {
		return errors.New("windows: the developer tools are off (they are on in krate native dev, or with native.debug)")
	}
	a.callMain(func() {
		for _, w := range a.windows {
			v := w.viewByRef(ref)
			if v == nil {
				continue
			}
			if w.released || w.destroyed {
				err = errReleased
				return
			}
			v.do(func() { vcall(v.webview, wvOpenDevToolsWindow) })
			return
		}
		err = fmt.Errorf("windows: view %d does not exist", ref)
	})
	return
}
