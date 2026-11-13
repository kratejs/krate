//go:build darwin

package darwin

import (
	"errors"

	"github.com/ebitengine/purego/objc"

	"github.com/kratejs/krate/packages/native/platform/caps"
)

var _ caps.RichClipboard = (*App)(nil)

func setPasteboardData(utiType string, b []byte) {
	data := cls("NSData").Send(sel("dataWithBytes:length:"), &b[0], uint(len(b)))
	pasteboard().Send(sel("setData:forType:"), data, nsString(utiType))
}

// WriteRich puts text, HTML and RTF on one pasteboard entry.
func (a *App) WriteRich(d caps.ClipboardData) error {
	if d.Empty() {
		return a.Clear()
	}
	runOnMain(func() {
		withPool(func() {
			pasteboard().Send(sel("clearContents"))
			if d.Text != "" {
				setPasteboardData("public.utf8-plain-text", []byte(d.Text))
			}
			if d.HTML != "" {
				setPasteboardData("public.html", []byte(d.HTML))
			}
			if d.RTF != "" {
				setPasteboardData("public.rtf", []byte(d.RTF))
			}
		})
	})
	return nil
}

func (a *App) readUTI(uti string) (b []byte) {
	runOnMain(func() {
		withPool(func() { b = dataBytes(pasteboard().Send(sel("dataForType:"), nsString(uti))) })
	})
	return
}

// ReadHTML returns public.html as text.
func (a *App) ReadHTML() (string, error) {
	return caps.DecodeClipboardText(a.readUTI("public.html")), nil
}

// ReadRTF returns public.rtf as text.
func (a *App) ReadRTF() (string, error) {
	return caps.DecodeClipboardText(a.readUTI("public.rtf")), nil
}

// ClipboardFormats lists the pasteboard types, UTIs mapped to MIME names.
func (a *App) ClipboardFormats() (out []string, err error) {
	runOnMain(func() {
		withPool(func() {
			types := pasteboard().Send(sel("types"))
			if types == 0 {
				return
			}
			seen := map[string]bool{}
			n := objcCount(types)
			for i := uint(0); i < n; i++ {
				name := caps.FormatForUTI(goString(types.Send(sel("objectAtIndex:"), i)))
				if name == "NSStringPboardType" {
					name = caps.FormatText
				}
				if !seen[name] {
					seen[name] = true
					out = append(out, name)
				}
			}
		})
	})
	return
}

// ReadFormat returns one pasteboard type's bytes.
func (a *App) ReadFormat(name string) ([]byte, error) {
	return a.readUTI(caps.UTIForFormat(name)), nil
}

// WriteFormat replaces the pasteboard with one type.
func (a *App) WriteFormat(name string, data []byte) error {
	if len(data) == 0 {
		return errors.New("darwin: empty clipboard data for " + name)
	}
	runOnMain(func() {
		withPool(func() {
			pasteboard().Send(sel("clearContents"))
			setPasteboardData(caps.UTIForFormat(name), data)
		})
	})
	return nil
}

func objcCount(arr objc.ID) uint { return objc.Send[uint](arr, sel("count")) }
