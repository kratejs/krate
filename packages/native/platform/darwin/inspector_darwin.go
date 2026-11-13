//go:build darwin

package darwin

import (
	"errors"
	"fmt"

	"github.com/kratejs/krate/packages/native/platform/caps"
)

var _ caps.InspectorOpener = (*App)(nil)

// OpenInspector shows the web inspector of a view. WKWebView has no public
// call for it (isInspectable only lets Safari attach), so this uses the
// _inspector accessor that WebKit itself and other shells use, and says so
// when a future macOS lacks it. The context menu's Inspect Element and Safari's
// Develop menu keep working either way.
func (a *App) OpenInspector(ref caps.ViewRef) (err error) {
	if !a.opts.Debug {
		return errors.New("darwin: the developer tools are off (they are on in krate native dev, or with native.debug)")
	}
	runOnMain(func() {
		for _, w := range a.windows {
			v := w.viewByRef(ref)
			if v == nil {
				continue
			}
			if v.web == 0 {
				err = errReleased
				return
			}
			if !respondsTo(v.web, sel("_inspector")) {
				err = errors.New("darwin: this macOS has no inspector accessor; use Inspect Element in the context menu")
				return
			}
			insp := v.web.Send(sel("_inspector"))
			if insp == 0 || !respondsTo(insp, sel("show")) {
				err = errors.New("darwin: this macOS cannot show the inspector from code; use Inspect Element in the context menu")
				return
			}
			insp.Send(sel("show"))
			return
		}
		err = fmt.Errorf("darwin: view %d does not exist", ref)
	})
	return
}
