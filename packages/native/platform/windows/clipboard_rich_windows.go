//go:build windows

package windows

import (
	"errors"
	"net/url"
	"strings"
	"unicode/utf16"
	"unsafe"

	"github.com/kratejs/krate/packages/native/platform/caps"
	xw "golang.org/x/sys/windows"
)

var (
	pEnumClipboardFormats = user32.NewProc("EnumClipboardFormats")
	pGetClipboardFmtName  = user32.NewProc("GetClipboardFormatNameW")
)

var _ caps.RichClipboard = (*App)(nil)

func regFormat(name string) uintptr {
	f, _, _ := pRegisterClipboardFmt.Call(uintptr(unsafe.Pointer(wstr(name))))
	return f
}

func nulTerminated(b []byte) []byte { return append(append([]byte(nil), b...), 0) }

func utf16Bytes(s string) []byte {
	u := utf16.Encode([]rune(s + "\x00"))
	return append([]byte(nil), unsafe.Slice((*byte)(unsafe.Pointer(&u[0])), len(u)*2)...)
}

// WriteRich puts text, HTML (as CF_HTML) and RTF on one clipboard entry.
func (a *App) WriteRich(d caps.ClipboardData) error {
	if d.Empty() {
		return a.Clear()
	}
	return a.withClipboard(func() error {
		_, _, _ = pEmptyClipboard.Call()
		if d.Text != "" {
			if err := setClip(cfUnicodeText, utf16Bytes(d.Text)); err != nil {
				return err
			}
		}
		if d.HTML != "" {
			if err := setClip(regFormat("HTML Format"), nulTerminated(caps.EncodeCFHTML(d.HTML, ""))); err != nil {
				return err
			}
		}
		if d.RTF != "" {
			if err := setClip(regFormat("Rich Text Format"), nulTerminated([]byte(d.RTF))); err != nil {
				return err
			}
		}
		return nil
	})
}

// ReadHTML returns the fragment of the "HTML Format" entry.
func (a *App) ReadHTML() (s string, err error) {
	err = a.withClipboard(func() error { s = caps.DecodeCFHTML(getClip(regFormat("HTML Format"))); return nil })
	return
}

// ReadRTF returns the "Rich Text Format" entry.
func (a *App) ReadRTF() (s string, err error) {
	err = a.withClipboard(func() error {
		s = string(trimNul(getClip(regFormat("Rich Text Format"))))
		return nil
	})
	return
}

func trimNul(b []byte) []byte {
	for len(b) > 0 && b[len(b)-1] == 0 {
		b = b[:len(b)-1]
	}
	return b
}

// ClipboardFormats lists the formats with registered names mapped to MIME
// names; CF_UNICODETEXT is text/plain and CF_HDROP is text/uri-list.
func (a *App) ClipboardFormats() (out []string, err error) {
	err = a.withClipboard(func() error {
		seen := map[string]bool{}
		add := func(n string) {
			if n != "" && !seen[n] {
				seen[n] = true
				out = append(out, n)
			}
		}
		var f uintptr
		for {
			f, _, _ = pEnumClipboardFormats.Call(f)
			if f == 0 {
				break
			}
			switch {
			case f == cfUnicodeText || f == 1 || f == 7: // CF_TEXT, CF_OEMTEXT
				add(caps.FormatText)
			case f == cfHDrop:
				add(caps.FormatURIs)
			case f >= 0xC000:
				buf := make([]uint16, 256)
				n, _, _ := pGetClipboardFmtName.Call(f, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
				if n > 0 {
					add(caps.FormatForWinFormat(xw.UTF16ToString(buf[:n])))
				}
			}
		}
		return nil
	})
	return
}

// ReadFormat returns one format's bytes. text/plain is UTF-8, text/html the
// fragment, text/uri-list file: URIs; any other name is a registered format.
func (a *App) ReadFormat(name string) (b []byte, err error) {
	err = a.withClipboard(func() error {
		switch name {
		case caps.FormatText:
			if t := getClip(cfUnicodeText); len(t) >= 2 {
				b = []byte(xw.UTF16ToString(unsafe.Slice((*uint16)(unsafe.Pointer(&t[0])), len(t)/2)))
			}
		case caps.FormatHTML:
			if s := caps.DecodeCFHTML(getClip(regFormat("HTML Format"))); s != "" {
				b = []byte(s)
			}
		case caps.FormatURIs:
			var uris []string
			for _, p := range a.hdropPaths() {
				uris = append(uris, (&url.URL{Scheme: "file", Path: "/" + strings.ReplaceAll(p, "\\", "/")}).String())
			}
			if len(uris) > 0 {
				b = []byte(strings.Join(uris, "\r\n") + "\r\n")
			}
		case caps.FormatRTF:
			b = trimNul(getClip(regFormat("Rich Text Format")))
		default:
			b = getClip(regFormat(caps.WinFormatForFormat(name)))
		}
		return nil
	})
	return
}

// WriteFormat replaces the clipboard with one format.
func (a *App) WriteFormat(name string, data []byte) error {
	if len(data) == 0 {
		return errors.New("windows: empty clipboard data for " + name)
	}
	return a.withClipboard(func() error {
		_, _, _ = pEmptyClipboard.Call()
		switch name {
		case caps.FormatText:
			return setClip(cfUnicodeText, utf16Bytes(string(data)))
		case caps.FormatHTML:
			return setClip(regFormat("HTML Format"), nulTerminated(caps.EncodeCFHTML(string(data), "")))
		case caps.FormatRTF:
			return setClip(regFormat("Rich Text Format"), nulTerminated(data))
		}
		return setClip(regFormat(caps.WinFormatForFormat(name)), data)
	})
}
