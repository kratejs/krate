//go:build linux

package linux

import (
	"errors"
	"sync"

	"github.com/ebitengine/purego"
	"github.com/kratejs/krate/packages/native/krate"
	"github.com/kratejs/krate/packages/native/platform/caps"
)

var _ caps.Dialogs = (*App)(nil)

// DialogProbe lets a test inspect a dialog that was just created and answer
// it without a user. See App.DialogHook.
type DialogProbe struct {
	app    *App
	dialog uintptr
	native bool
	// Kind is "file" or "message".
	Kind string
}

// Title returns the dialog title.
func (p *DialogProbe) Title() string {
	if p.native {
		return goString(p.app.x.gtkNativeGetTitle(p.dialog))
	}
	return goString(p.app.api.gtkWindowGetTitle(p.dialog))
}

// Action returns the GtkFileChooserAction (0 open, 1 save, 2 select folder).
func (p *DialogProbe) Action() int { return int(p.app.x.gtkChooserGetAction(p.dialog)) }

// Multiple reports whether several files may be chosen.
func (p *DialogProbe) Multiple() bool { return p.app.x.gtkChooserGetMultiple(p.dialog) != 0 }

// FilterNames lists the filter names in order.
func (p *DialogProbe) FilterNames() []string {
	var out []string
	for _, f := range p.app.slistPtrs(p.app.x.gtkChooserListFilters(p.dialog)) {
		out = append(out, goString(p.app.x.gtkFileFilterGetName(f)))
	}
	return out
}

// Show maps the dialog so that a file chooser loads its folder; Accept with a
// file name needs that.
func (p *DialogProbe) Show() {
	if p.native {
		p.app.x.gtkNativeShow(p.dialog)
	} else {
		p.app.api.gtkWidgetShowAll(p.dialog)
	}
}

// Cancel answers the dialog as if the user pressed Cancel.
func (p *DialogProbe) Cancel() { p.app.x.gSignalEmitByName(p.dialog, "response", gtkResponseCancel) }

// Accept selects name (file dialogs) and answers as if the user pressed OK.
func (p *DialogProbe) Accept(names ...string) {
	if p.Kind == "file" {
		for _, n := range names {
			p.app.x.gtkChooserSelectFile(p.dialog, n)
		}
	}
	p.app.x.gSignalEmitByName(p.dialog, "response", gtkResponseAccept)
}

// Press answers a message dialog as if the button with this response id (the
// button index) was pressed.
func (p *DialogProbe) Press(index int) {
	p.app.x.gSignalEmitByName(p.dialog, "response", int32(index))
}

// DialogHook, when set, is called on the main thread with every new dialog
// before it is shown. If it returns true the dialog is not shown and the hook
// is expected to answer it (Cancel, Accept or Press), possibly later. Tests
// use it because a native dialog cannot be clicked headlessly.
var DialogHook func(p *DialogProbe) bool

type dialogWait struct {
	ch chan int32
}

var (
	cbResponse uintptr
	respMu     sync.Mutex
	respWaits  = map[uintptr]*dialogWait{}
	respNext   uintptr
)

func responseCallback(dialog, response, id uintptr) uintptr {
	respMu.Lock()
	w := respWaits[id]
	delete(respWaits, id)
	respMu.Unlock()
	if w != nil {
		w.ch <- int32(response)
	}
	return 0
}

func (a *App) watchResponse(dialog uintptr) *dialogWait {
	if cbResponse == 0 {
		cbResponse = purego.NewCallback(responseCallback)
	}
	w := &dialogWait{ch: make(chan int32, 1)}
	respMu.Lock()
	respNext++
	id := respNext
	respWaits[id] = w
	respMu.Unlock()
	a.api.gSignalConnectData(dialog, "response", cbResponse, id, 0, 0)
	return w
}

func (a *App) parentWin(ref caps.WindowRef) uintptr {
	if ref != 0 {
		if w := a.windowByID(uintptr(ref)); w != nil && w.alive {
			return w.win
		}
	}
	return 0
}

// FileDialog shows a GtkFileChooserNative and blocks until it is answered.
func (a *App) FileDialog(parent caps.WindowRef, o caps.FileDialogOptions) ([]string, error) {
	x := a.x
	if x.gtkFileChooserNative == nil {
		return nil, krate.ErrUnsupported
	}
	var wait *dialogWait
	var dlg uintptr
	var probeErr error
	if !a.callMain(func() {
		action := int32(chooserOpen)
		switch o.Mode {
		case caps.SaveFile:
			action = chooserSave
		case caps.OpenFolder:
			action = chooserFolder
		}
		accept := o.AcceptLabel
		if accept == "" {
			switch o.Mode {
			case caps.SaveFile:
				accept = "_Save"
			default:
				accept = "_Open"
			}
		}
		title := o.Title
		dlg = x.gtkFileChooserNative(title, a.parentWin(parent), action, accept, "_Cancel")
		if dlg == 0 {
			probeErr = errors.New("krate: cannot create the file dialog")
			return
		}
		x.gtkNativeModal(dlg, b2i(parent != 0))
		if o.Multiple && o.Mode != caps.SaveFile {
			x.gtkChooserMultiple(dlg, 1)
		}
		if o.Dir != "" {
			x.gtkChooserSetFolder(dlg, o.Dir)
		}
		if o.Name != "" && o.Mode == caps.SaveFile {
			x.gtkChooserSetName(dlg, o.Name)
		}
		if o.Mode == caps.SaveFile {
			x.gtkChooserOverwrite(dlg, b2i(o.ConfirmWrite))
		}
		x.gtkChooserShowHidden(dlg, b2i(o.ShowHidden))
		for _, f := range o.Filters {
			gf := x.gtkFileFilterNew()
			x.gtkFileFilterName(gf, f.Name)
			for _, pat := range f.Patterns {
				x.gtkFileFilterPattern(gf, caseInsensitiveGlob(pat))
			}
			x.gtkChooserAddFilter(dlg, gf)
		}
		wait = a.watchResponse(dlg)
		if h := DialogHook; h != nil && h(&DialogProbe{app: a, dialog: dlg, native: true, Kind: "file"}) {
			return
		}
		x.gtkNativeShow(dlg)
	}) {
		return nil, errors.New("krate: application is not running")
	}
	if probeErr != nil {
		return nil, probeErr
	}
	var resp int32
	select {
	case resp = <-wait.ch:
	case <-a.stopCh:
		return nil, errors.New("krate: application stopped")
	}
	var paths []string
	a.callMain(func() {
		if resp == gtkResponseAccept {
			paths = a.slistStrings(x.gtkChooserGetFilenames(dlg), true)
		}
		x.gtkNativeDestroy(dlg)
	})
	return paths, nil
}

// caseInsensitiveGlob turns "*.png" into "*.[pP][nN][gG]".
func caseInsensitiveGlob(p string) string {
	out := make([]byte, 0, len(p)*2)
	for i := 0; i < len(p); i++ {
		c := p[i]
		switch {
		case c >= 'a' && c <= 'z':
			out = append(out, '[', c, c-32, ']')
		case c >= 'A' && c <= 'Z':
			out = append(out, '[', c, c+32, ']')
		default:
			out = append(out, c)
		}
	}
	return string(out)
}

// MessageDialog shows a GtkMessageDialog and blocks until it is answered.
func (a *App) MessageDialog(parent caps.WindowRef, o caps.MessageDialogOptions) (int, error) {
	x := a.x
	if x.gtkMessageDialogNew == nil || x.gtkDialogAddButton == nil {
		return -1, krate.ErrUnsupported
	}
	buttons := o.Buttons
	if len(buttons) == 0 {
		buttons = []string{"OK"}
	}
	var wait *dialogWait
	var dlg uintptr
	if !a.callMain(func() {
		kind := int32(0) // GTK_MESSAGE_INFO
		switch o.Kind {
		case caps.MessageWarning:
			kind = 1
		case caps.MessageQuestion:
			kind = 2
		case caps.MessageError:
			kind = 3
		}
		dlg = x.gtkMessageDialogNew(a.parentWin(parent), 0, kind, 0, "%s", o.Message)
		if o.Title != "" {
			x.gtkWindowSetTitle(dlg, o.Title)
		}
		if o.Detail != "" {
			x.gtkMessageSecondary(dlg, "%s", o.Detail)
		}
		for i, b := range buttons {
			x.gtkDialogAddButton(dlg, b, int32(i))
		}
		if o.Default >= 0 && o.Default < len(buttons) {
			x.gtkDialogDefault(dlg, int32(o.Default))
		}
		x.gtkWindowSetModal(dlg, b2i(parent != 0))
		wait = a.watchResponse(dlg)
		if h := DialogHook; h != nil && h(&DialogProbe{app: a, dialog: dlg, Kind: "message"}) {
			return
		}
		a.api.gtkWidgetShowAll(dlg)
	}) {
		return -1, errors.New("krate: application is not running")
	}
	var resp int32
	select {
	case resp = <-wait.ch:
	case <-a.stopCh:
		return -1, errors.New("krate: application stopped")
	}
	a.callMain(func() { a.api.gtkWidgetDestroy(dlg) })
	if resp < 0 || int(resp) >= len(buttons) {
		return -1, nil
	}
	return int(resp), nil
}
