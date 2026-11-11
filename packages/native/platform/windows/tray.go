package windows

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/png"
	"unicode/utf16"
)

// System tray logic (pure; the Shell_NotifyIconW glue is in tray_windows.go).

// Shell_NotifyIcon messages, flags and versions (shellapi.h).
const (
	nimAdd        = 0
	nimModify     = 1
	nimDelete     = 2
	nimSetVersion = 4

	nifMessage = 0x01
	nifIcon    = 0x02
	nifTip     = 0x04
	nifShowTip = 0x80

	notifyIconVersion4 = 4

	// wmTrayCallback is the callback message of the tray icon.
	wmTrayCallback = 0x8000 + 0x4C // WM_APP + 0x4C

	wmLButtonDblClk = 0x0203
	wmContextMenu   = 0x007B
	ninSelect       = 0x0400
	ninKeySelect    = 0x0401

	trayIconID = 1

	// szTip holds 128 UTF-16 units including the NUL.
	trayTipUnits = 128
)

// notifyIconData is NOTIFYICONDATAW for Windows Vista and later. Its size is
// 976 bytes on amd64 and arm64 (both have 8-byte pointers and the same
// alignment rules); TestNotifyIconDataLayout pins the offsets.
type notifyIconData struct {
	size            uint32
	hwnd            uintptr
	id              uint32
	flags           uint32
	callbackMessage uint32
	icon            uintptr
	tip             [128]uint16
	state           uint32
	stateMask       uint32
	info            [256]uint16
	version         uint32 // union with uTimeout
	infoTitle       [64]uint16
	infoFlags       uint32
	guidItem        [16]byte
	balloonIcon     uintptr
}

// trayEventKind maps the low word of lParam of a version 4 callback message to
// a caps.TrayEvent kind. Version 4 sends a left click as NIN_SELECT (and, for
// keyboard activation, NIN_KEYSELECT) next to the raw mouse message, and a
// right click as WM_CONTEXTMENU, so the raw WM_LBUTTONUP and WM_RBUTTONUP are
// ignored to report each click once. ok is false for events that are not
// reported.
func trayEventKind(ev uint32) (kind string, ok bool) {
	switch ev {
	case ninSelect, ninKeySelect:
		return "click", true
	case wmLButtonDblClk:
		return "double-click", true
	case wmContextMenu:
		return "right-click", true
	}
	return "", false
}

// decodeTrayMessage splits a version 4 callback message: the event and icon id
// are the low and high word of lParam; wParam carries the anchor point as two
// signed 16-bit coordinates.
func decodeTrayMessage(wParam, lParam uintptr) (event uint32, id uint32, x, y int32) {
	event = uint32(lParam & 0xFFFF)
	id = uint32((lParam >> 16) & 0xFFFF)
	x = int32(int16(wParam & 0xFFFF))
	y = int32(int16((wParam >> 16) & 0xFFFF))
	return
}

// tipUnits encodes s as a NUL-terminated UTF-16 tooltip of at most max units
// (including the NUL), cutting on a code point boundary so a surrogate pair
// is never split.
func tipUnits(s string, max int) []uint16 {
	u := utf16.Encode([]rune(s))
	if len(u) > max-1 {
		u = u[:max-1]
		if n := len(u); n > 0 && u[n-1] >= 0xD800 && u[n-1] < 0xDC00 {
			u = u[:n-1]
		}
	}
	return append(u, 0)
}

// trayTooltip prefers the tooltip and falls back to the title.
func trayTooltip(tooltip, title string) string {
	if tooltip != "" {
		return tooltip
	}
	return title
}

// iconBits is a square 32-bit BGRA image (non-premultiplied alpha, top row
// first) ready for an icon's color bitmap.
type iconBits struct {
	size int
	bgra []byte
}

// pngToIconBits decodes png and scales it to size x size, preserving the
// aspect ratio on a transparent square. It is the fallback for icon creation
// when CreateIconFromResourceEx rejects the PNG data.
func pngToIconBits(data []byte, size int) (iconBits, error) {
	if size <= 0 || size > 512 {
		return iconBits{}, fmt.Errorf("windows: bad icon size %d", size)
	}
	if len(data) == 0 {
		return iconBits{}, errors.New("windows: empty tray icon image")
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return iconBits{}, fmt.Errorf("windows: the tray icon is not a valid PNG: %w", err)
	}
	b := img.Bounds()
	sw, sh := b.Dx(), b.Dy()
	if sw == 0 || sh == 0 {
		return iconBits{}, errors.New("windows: the tray icon image is empty")
	}
	// Fit inside the square.
	dw, dh := size, size
	if sw > sh {
		dh = max(1, size*sh/sw)
	} else {
		dw = max(1, size*sw/sh)
	}
	ox, oy := (size-dw)/2, (size-dh)/2
	out := make([]byte, size*size*4)
	for y := 0; y < dh; y++ {
		for x := 0; x < dw; x++ {
			r, g, bl, a := boxSample(img, b, x, y, dw, dh)
			o := ((y+oy)*size + x + ox) * 4
			out[o], out[o+1], out[o+2], out[o+3] = bl, g, r, a
		}
	}
	return iconBits{size: size, bgra: out}, nil
}

// boxSample averages the source pixels that fall in destination pixel
// (x, y), weighting color by alpha so transparent pixels do not darken edges.
func boxSample(img image.Image, b image.Rectangle, x, y, dw, dh int) (r, g, bl, a uint8) {
	sw, sh := b.Dx(), b.Dy()
	x0, x1 := x*sw/dw, (x+1)*sw/dw
	y0, y1 := y*sh/dh, (y+1)*sh/dh
	if x1 <= x0 {
		x1 = x0 + 1
	}
	if y1 <= y0 {
		y1 = y0 + 1
	}
	var sr, sg, sb, sa, n uint64
	for yy := y0; yy < y1 && yy < sh; yy++ {
		for xx := x0; xx < x1 && xx < sw; xx++ {
			pr, pg, pb, pa := img.At(b.Min.X+xx, b.Min.Y+yy).RGBA() // premultiplied, 16 bit
			sr += uint64(pr)
			sg += uint64(pg)
			sb += uint64(pb)
			sa += uint64(pa)
			n++
		}
	}
	if n == 0 || sa == 0 {
		return 0, 0, 0, 0
	}
	// Un-premultiply the summed color.
	return uint8(sr * 255 / sa), uint8(sg * 255 / sa), uint8(sb * 255 / sa), uint8(sa / n >> 8)
}
