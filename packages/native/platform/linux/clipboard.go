//go:build linux

package linux

import (
	"errors"
	"net/url"
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

	clipMu    sync.Mutex
	clipFiles [][]string // the file lists we own, newest last
	clipPin   [][]byte   // keeps target-name C strings alive
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

func clipGetCallback(clipboard, sel, info, data uintptr) uintptr {
	a := theApp
	clipMu.Lock()
	var files []string
	if n := len(clipFiles); n > 0 {
		files = clipFiles[n-1]
	}
	clipMu.Unlock()
	if len(files) == 0 {
		return 0
	}
	x := a.x
	var uris []string
	for _, f := range files {
		uris = append(uris, (&url.URL{Scheme: "file", Path: f}).String())
	}
	var body string
	var typ string
	switch info {
	case 1:
		typ = "text/uri-list"
		body = strings.Join(uris, "\r\n") + "\r\n"
	default:
		typ = "x-special/gnome-copied-files"
		body = "copy\n" + strings.Join(uris, "\n")
	}
	b := []byte(body)
	if len(b) == 0 {
		return 0
	}
	x.gtkSelSet(sel, x.gdkAtomIntern(typ, 0), 8, unsafe.Pointer(&b[0]), int32(len(b)))
	return 0
}

func clipClearCallback(clipboard, data uintptr) uintptr {
	// The previous owner's clear can arrive after a new WriteFiles, so the
	// list is kept; the newest entry always wins.
	return 0
}

// WriteFiles puts a file list on the clipboard (text/uri-list and the GNOME
// copied-files format).
func (a *App) WriteFiles(paths []string) error {
	return a.onMainErr(func() error {
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
		clipFiles = [][]string{append([]string(nil), paths...)}
		clipMu.Unlock()
		// GtkTargetEntry { char *target; guint flags; guint info; }
		type entry struct {
			target uintptr
			flags  uint32
			info   uint32
		}
		n1, n2 := cstr("text/uri-list"), cstr("x-special/gnome-copied-files")
		clipMu.Lock()
		clipPin = append(clipPin, n1, n2)
		clipMu.Unlock()
		ents := []entry{
			{uintptr(unsafe.Pointer(&n1[0])), 0, 1},
			{uintptr(unsafe.Pointer(&n2[0])), 0, 2},
		}
		if x.gtkClipboardSetData(c, unsafe.Pointer(&ents[0]), 2, cbClipGet, cbClipClear, 0) == 0 {
			return errors.New("krate: cannot own the clipboard")
		}
		return nil
	})
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
