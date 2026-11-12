//go:build linux

package linux

import (
	"errors"
	"net/url"
	"runtime"
	"strings"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/kratejs/krate/packages/native/krate"
	"github.com/kratejs/krate/packages/native/platform/caps"
)

var _ caps.Clipboard = (*App)(nil)

var (
	cbClipGet, cbClipClear uintptr
	clipOnce               sync.Once

	clipMu sync.Mutex
)

func (a *App) clip() (uintptr, error) {
	x := a.x
	if x.gtkClipboardGet == nil {
		return 0, krate.ErrUnsupported
	}
	return x.gtkClipboardGet(gdkSelectionClipboard), nil
}

func (a *App) onMainErr(fn func() error) error {
	var err error
	if !a.callMain(func() { err = fn() }) {
		return errors.New("krate: application is not running")
	}
	return err
}

// ReadText returns the clipboard text.
func (a *App) ReadText() (s string, err error) {
	err = a.onMainErr(func() error {
		c, err := a.clip()
		if err != nil {
			return err
		}
		p := a.x.gtkClipboardWaitText(c)
		if p != 0 {
			s = goString(p)
			a.api.gFree(p)
		}
		return nil
	})
	return
}

// WriteText replaces the clipboard with text.
func (a *App) WriteText(s string) error {
	return a.onMainErr(func() error {
		c, err := a.clip()
		if err != nil {
			return err
		}
		a.x.gtkClipboardSetText(c, s, int32(len(s)))
		return nil
	})
}

// ReadImage returns the clipboard image as PNG bytes.
func (a *App) ReadImage() (png []byte, err error) {
	err = a.onMainErr(func() error {
		c, err := a.clip()
		if err != nil {
			return err
		}
		x := a.x
		if x.pixbufSaveBuffer == nil {
			return krate.ErrUnsupported
		}
		pb := x.gtkClipboardWaitImg(c)
		if pb == 0 {
			return nil
		}
		defer a.api.gObjectUnref(pb)
		var buf uintptr
		var size uint64
		var gerr uintptr
		if x.pixbufSaveBuffer(pb, &buf, &size, "png", 0, 0, &gerr) == 0 {
			return errors.New("krate: encoding the clipboard image: " + gerrMessage(gerr))
		}
		png = append([]byte(nil), unsafe.Slice((*byte)(*(*unsafe.Pointer)(unsafe.Pointer(&buf))), int(size))...)
		a.api.gFree(buf)
		return nil
	})
	return
}

// WriteImage puts a PNG image on the clipboard.
func (a *App) WriteImage(png []byte) error {
	return a.onMainErr(func() error {
		c, err := a.clip()
		if err != nil {
			return err
		}
		x := a.x
		if x.pixbufLoaderNew == nil {
			return krate.ErrUnsupported
		}
		var gerr uintptr
		l := x.pixbufLoaderNew("png", &gerr)
		if l == 0 {
			return errors.New("krate: no PNG loader: " + gerrMessage(gerr))
		}
		defer a.api.gObjectUnref(l)
		if len(png) == 0 || x.pixbufLoaderWrite(l, unsafe.Pointer(&png[0]), uint64(len(png)), &gerr) == 0 {
			x.pixbufLoaderClose(l, nil)
			return errors.New("krate: the image is not a valid PNG")
		}
		if x.pixbufLoaderClose(l, &gerr) == 0 {
			return errors.New("krate: the image is not a valid PNG: " + gerrMessage(gerr))
		}
		pb := x.pixbufLoaderGet(l)
		if pb == 0 {
			return errors.New("krate: the image is not a valid PNG")
		}
		x.gtkClipboardSetImage(c, pb)
		return nil
	})
}

// clipEntry is one representation we serve while we own the clipboard. The
// same data answers every name in targets.
type clipEntry struct {
	targets []string
	data    []byte
	text    bool // served with gtk_selection_data_set_text, which converts encodings
}

var clipOwned []clipEntry // what we serve; replaced by each write

func clipGetCallback(clipboard, sel, info, data uintptr) uintptr {
	clipMu.Lock()
	var e clipEntry
	ok := int(info) < len(clipOwned)
	if ok {
		e = clipOwned[info]
	}
	clipMu.Unlock()
	if !ok || len(e.data) == 0 {
		return 0
	}
	x := theApp.x
	if e.text {
		x.gtkSelSetText(sel, unsafe.Pointer(&e.data[0]), int32(len(e.data)))
		return 0
	}
	x.gtkSelSet(sel, x.gdkAtomIntern(e.targets[0], 0), 8, unsafe.Pointer(&e.data[0]), int32(len(e.data)))
	return 0
}

func clipClearCallback(clipboard, data uintptr) uintptr {
	// The previous owner's clear can arrive after a new write, so the entries
	// are kept; the newest write always wins.
	return 0
}

// ownClipboard makes the entries the clipboard content. Main thread only.
func (a *App) ownClipboard(entries []clipEntry) error {
	c, err := a.clip()
	if err != nil {
		return err
	}
	x := a.x
	if x.gtkClipboardSetData == nil {
		return krate.ErrUnsupported
	}
	clipOnce.Do(func() {
		cbClipGet = purego.NewCallback(clipGetCallback)
		cbClipClear = purego.NewCallback(clipClearCallback)
	})
	clipMu.Lock()
	clipOwned = entries
	clipMu.Unlock()
	// GtkTargetEntry { char *target; guint flags; guint info; }
	type entry struct {
		target uintptr
		flags  uint32
		info   uint32
	}
	var ents []entry
	var pins [][]byte
	for i, e := range entries {
		for _, t := range e.targets {
			n := cstr(t)
			pins = append(pins, n)
			ents = append(ents, entry{uintptr(unsafe.Pointer(&n[0])), 0, uint32(i)})
		}
	}
	if len(ents) == 0 {
		return nil
	}
	ok := x.gtkClipboardSetData(c, unsafe.Pointer(&ents[0]), uint32(len(ents)), cbClipGet, cbClipClear, 0)
	runtime.KeepAlive(pins)
	if ok == 0 {
		return errors.New("krate: cannot own the clipboard")
	}
	return nil
}

var (
	textTargets = []string{"UTF8_STRING", "text/plain;charset=utf-8", "text/plain", "STRING", "TEXT"}
	htmlTargets = []string{"text/html"}
	rtfTargets  = []string{"text/rtf", "application/rtf"}
)

// WriteFiles puts a file list on the clipboard (text/uri-list and the GNOME
// copied-files format).
func (a *App) WriteFiles(paths []string) error {
	var uris []string
	for _, f := range paths {
		uris = append(uris, (&url.URL{Scheme: "file", Path: f}).String())
	}
	if len(uris) == 0 {
		return a.Clear()
	}
	return a.onMainErr(func() error {
		return a.ownClipboard([]clipEntry{
			{targets: []string{"text/uri-list"}, data: []byte(strings.Join(uris, "\r\n") + "\r\n")},
			{targets: []string{"x-special/gnome-copied-files"}, data: []byte("copy\n" + strings.Join(uris, "\n"))},
		})
	})
}

var _ caps.RichClipboard = (*App)(nil)

// WriteRich puts text, HTML and RTF on the clipboard together.
func (a *App) WriteRich(d caps.ClipboardData) error {
	if d.Empty() {
		return a.Clear()
	}
	var es []clipEntry
	if d.Text != "" {
		es = append(es, clipEntry{targets: textTargets, data: []byte(d.Text), text: true})
	}
	if d.HTML != "" {
		es = append(es, clipEntry{targets: htmlTargets, data: []byte(d.HTML)})
	}
	if d.RTF != "" {
		es = append(es, clipEntry{targets: rtfTargets, data: []byte(d.RTF)})
	}
	return a.onMainErr(func() error { return a.ownClipboard(es) })
}

// WriteFormat replaces the clipboard with one format.
func (a *App) WriteFormat(name string, data []byte) error {
	if len(data) == 0 {
		return errors.New("krate: empty clipboard data for " + name)
	}
	var e clipEntry
	switch name {
	case caps.FormatText:
		e = clipEntry{targets: textTargets, data: data, text: true}
	case caps.FormatHTML:
		e = clipEntry{targets: htmlTargets, data: data}
	case caps.FormatRTF:
		e = clipEntry{targets: rtfTargets, data: data}
	default:
		e = clipEntry{targets: []string{name}, data: data}
	}
	return a.onMainErr(func() error { return a.ownClipboard([]clipEntry{e}) })
}

// target returns the bytes of one clipboard target, or nil. Main thread only.
func (a *App) clipTarget(name string) []byte {
	c, err := a.clip()
	if err != nil {
		return nil
	}
	x := a.x
	sd := x.gtkClipboardWaitData(c, x.gdkAtomIntern(name, 0))
	if sd == 0 {
		return nil
	}
	defer x.gtkSelFree(sd)
	p := x.gtkSelGetData(sd)
	n := int(x.gtkSelGetLength(sd))
	if p == 0 || n <= 0 {
		return nil
	}
	return append([]byte(nil), unsafe.Slice((*byte)(*(*unsafe.Pointer)(unsafe.Pointer(&p))), n)...)
}

func (a *App) readFirst(names []string) (b []byte, err error) {
	err = a.onMainErr(func() error {
		for _, n := range names {
			if b = a.clipTarget(n); b != nil {
				return nil
			}
		}
		return nil
	})
	return
}

// ReadHTML returns the HTML on the clipboard.
func (a *App) ReadHTML() (string, error) {
	b, err := a.readFirst(htmlTargets)
	return caps.DecodeClipboardText(b), err
}

// ReadRTF returns the RTF on the clipboard.
func (a *App) ReadRTF() (string, error) {
	b, err := a.readFirst(rtfTargets)
	return caps.DecodeClipboardText(b), err
}

// ReadFormat returns one format's bytes.
func (a *App) ReadFormat(name string) ([]byte, error) {
	switch name {
	case caps.FormatText:
		s, err := a.ReadText()
		if s == "" {
			return nil, err
		}
		return []byte(s), err
	case caps.FormatHTML:
		return a.readFirst(htmlTargets)
	case caps.FormatRTF:
		return a.readFirst(rtfTargets)
	}
	return a.readFirst([]string{name})
}

// ClipboardFormats lists the targets on the clipboard, with the text targets
// folded into text/plain.
func (a *App) ClipboardFormats() (out []string, err error) {
	err = a.onMainErr(func() error {
		c, err := a.clip()
		if err != nil {
			return err
		}
		x := a.x
		var atoms uintptr
		var n int32
		if x.gtkClipWaitTargets(c, &atoms, &n) == 0 || atoms == 0 {
			return nil
		}
		base := *(*unsafe.Pointer)(unsafe.Pointer(&atoms))
		seen := map[string]bool{}
		for i := 0; i < int(n); i++ {
			np := x.gdkAtomName(*(*uintptr)(unsafe.Add(base, i*8)))
			if np == 0 {
				continue
			}
			name := goString(np)
			a.api.gFree(np)
			switch name {
			case "TARGETS", "TIMESTAMP", "MULTIPLE", "SAVE_TARGETS", "DELETE":
				continue
			case "UTF8_STRING", "STRING", "TEXT", "COMPOUND_TEXT", "text/plain;charset=utf-8":
				name = caps.FormatText
			case "application/rtf":
				name = caps.FormatRTF
			}
			if !seen[name] {
				seen[name] = true
				out = append(out, name)
			}
		}
		a.api.gFree(atoms)
		return nil
	})
	return
}

// ReadFiles returns the file paths on the clipboard.
func (a *App) ReadFiles() (paths []string, err error) {
	err = a.onMainErr(func() error {
		c, err := a.clip()
		if err != nil {
			return err
		}
		x := a.x
		sd := x.gtkClipboardWaitData(c, x.gdkAtomIntern("text/uri-list", 0))
		if sd == 0 {
			return nil
		}
		defer x.gtkSelFree(sd)
		strv := x.gtkSelGetUris(sd)
		if strv == 0 {
			return nil
		}
		base := *(*unsafe.Pointer)(unsafe.Pointer(&strv))
		for i := 0; ; i++ {
			p := *(*uintptr)(unsafe.Add(base, i*8))
			if p == 0 {
				break
			}
			u, perr := url.Parse(goString(p))
			if perr == nil && u.Scheme == "file" {
				paths = append(paths, u.Path)
			}
		}
		x.gStrfreev(strv)
		return nil
	})
	return
}

// Clear empties the clipboard.
func (a *App) Clear() error {
	return a.onMainErr(func() error {
		c, err := a.clip()
		if err != nil {
			return err
		}
		a.x.gtkClipboardClear(c)
		return nil
	})
}
