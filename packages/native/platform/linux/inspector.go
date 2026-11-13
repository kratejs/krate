//go:build linux

package linux

import (
	"errors"
	"fmt"

	"github.com/kratejs/krate/packages/native/platform/caps"
)

var _ caps.InspectorOpener = (*App)(nil)

// OpenInspector shows the WebKit inspector of a view.
func (a *App) OpenInspector(ref caps.ViewRef) (err error) {
	if !a.debug {
		return errors.New("krate: the developer tools are off (they are on in krate native dev, or with native.debug)")
	}
	if a.api.wkViewGetInspector == nil || a.api.wkInspectorShow == nil {
		return errors.New("krate: this WebKitGTK has no web inspector")
	}
	a.callMain(func() {
		v := a.viewByRef(uintptr(ref))
		switch {
		case v == nil:
			err = fmt.Errorf("krate: view %d does not exist", ref)
		case v.widget == 0:
			err = errReleased
		default:
			if insp := a.api.wkViewGetInspector(v.widget); insp != 0 {
				a.api.wkInspectorShow(insp)
			} else {
				err = errors.New("krate: the view has no inspector")
			}
		}
	})
	return
}
