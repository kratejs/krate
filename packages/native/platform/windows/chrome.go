//go:build windows

package windows

import (
	"strings"

	"github.com/kratejs/krate/packages/native/platform/caps"
)

// Pure parts of the window chrome: style bits, frame geometry, structure
// layouts and the numbers Windows expects. They have no Win32 calls;
// chrome_windows.go does the calling.

// Window styles used by the chrome code.
const (
	wsCaptionBits  = 0x00C00000 // WS_CAPTION
	wsSysMenu      = 0x00080000
	wsMinimizeBox  = 0x00020000
	wsMaximizeBit  = 0x00010000
	wsThickFrameB  = 0x00040000
	dwmaBackdrop   = 38 // DWMWA_SYSTEMBACKDROP_TYPE
	dwmaCaptionClr = 35 // DWMWA_CAPTION_COLOR
	dwmaTextColor  = 36 // DWMWA_TEXT_COLOR
	dwmaButtonBnds = 5  // DWMWA_CAPTION_BUTTON_BOUNDS

	// minBackdropBuild is the first Windows 11 build with
	// DWMWA_SYSTEMBACKDROP_TYPE (22H2).
	minBackdropBuild = 22621
	// minCaptionColorBuild is the first Windows 11 build with
	// DWMWA_CAPTION_COLOR (21H2).
	minCaptionColorBuild = 22000
)

// backdropSupported reports whether a Windows build has system backdrops.
func backdropSupported(build uint32) bool { return build >= minBackdropBuild }

// captionColorSupported reports whether DWMWA_CAPTION_COLOR works.
func captionColorSupported(build uint32) bool { return build >= minCaptionColorBuild }

// colorRef converts a color to a COLORREF (0x00BBGGRR). Alpha is dropped:
// DWM caption colors are opaque.
func colorRef(c caps.RGBA) uint32 {
	return uint32(c.R) | uint32(c.G)<<8 | uint32(c.B)<<16
}

// webviewColor packs a color as COREWEBVIEW2_COLOR (bytes A, R, G, B in
// memory order), which a 4-byte struct passed by value arrives as in a
// register.
func webviewColor(c caps.RGBA) uint32 {
	return uint32(c.A) | uint32(c.R)<<8 | uint32(c.G)<<16 | uint32(c.B)<<24
}

// chromeStyle derives a window's style from the base style and the title bar
// style. "none" drops the system menu, which takes the caption buttons with
// it; "hidden" keeps the caption (the frame is extended over the client area
// by the NCCALCSIZE handler) so that Windows draws the buttons.
func chromeStyle(base uint32, titleBar string) uint32 {
	if titleBar == caps.TitleBarNone {
		base &^= wsSysMenu
	}
	return base
}

// customFrame reports whether a title bar style takes over the frame.
func customFrame(titleBar string) bool {
	return titleBar == caps.TitleBarHidden || titleBar == caps.TitleBarNone
}

// ncTop is the top of the client area after WM_NCCALCSIZE for a custom frame.
// A normal window gets the proposed top back, so the caption becomes client
// area; a maximized one keeps the inset the default procedure computed, which
// is the part of the frame that hangs over the screen edge.
func ncTop(proposedTop, defaultTop int32, maximized bool) int32 {
	if maximized {
		return defaultTop
	}
	return proposedTop
}

// margins is MARGINS for DwmExtendFrameIntoClientArea.
type margins struct{ left, right, top, bottom int32 }

// frameMargins is the part of the client area DWM paints as frame. A hidden
// title bar extends it by the caption height so the caption buttons have
// glass under them; every custom frame needs at least one row for the shadow.
// A transparent window with a backdrop extends it over everything.
func frameMargins(titleBar string, captionPx int, backdrop bool) margins {
	if backdrop {
		return margins{-1, -1, -1, -1}
	}
	switch titleBar {
	case caps.TitleBarHidden:
		if captionPx < 1 {
			captionPx = 1
		}
		return margins{0, 0, int32(captionPx), 0}
	case caps.TitleBarNone:
		return margins{0, 0, 1, 0}
	}
	return margins{}
}

// htForEdge maps a StartResize edge name to the hit-test code Windows
// expects in WM_NCLBUTTONDOWN.
func htForEdge(edge string) int {
	switch edge {
	case "top":
		return caps.HTTOP
	case "bottom":
		return caps.HTBOTTOM
	case "left":
		return caps.HTLEFT
	case "right":
		return caps.HTRIGHT
	case "topLeft":
		return caps.HTTOPLEFT
	case "topRight":
		return caps.HTTOPRIGHT
	case "bottomLeft":
		return caps.HTBOTTOMLEFT
	case "bottomRight":
		return caps.HTBOTTOMRIGHT
	}
	return -1
}

// Taskbar thumbnail buttons.
const (
	thbMask      = 0x0F // THB_BITMAP | THB_ICON | THB_TOOLTIP | THB_FLAGS
	thbIcon      = 0x02
	thbTooltip   = 0x04
	thbFlags     = 0x08
	thbfEnabled  = 0x00
	thbfDisabled = 0x01
	thbfDismiss  = 0x02
	thbfHidden   = 0x08
	thbnClicked  = 0x1800
	// thumbSlots is how many buttons the taskbar object is given once; the
	// shell cannot remove buttons later, so the unused ones are hidden.
	thumbSlots = caps.MaxThumbButtons
)

// thumbButton is THUMBBUTTON.
type thumbButton struct {
	mask  uint32
	id    uint32
	_     uint32 // iBitmap, unused
	icon  uintptr
	tip   [260]uint16
	flags uint32
}

// thumbID is the button id of the slot-th button; ids start at 1 so a click
// is never confused with a menu command of zero.
func thumbID(slot int) uint32 { return uint32(slot) + 1 }

// thumbSlot returns the slot of a button id, or -1.
func thumbSlot(id uint32) int {
	if id < 1 || id > thumbSlots {
		return -1
	}
	return int(id) - 1
}

// splitCommand splits the wParam of a WM_COMMAND into its notification code
// and id.
func splitCommand(wParam uintptr) (code, id uint32) {
	return uint32(wParam>>16) & 0xFFFF, uint32(wParam) & 0xFFFF
}

// makeThumb fills a THUMBBUTTON slot from a button, or hides it (b == nil).
func makeThumb(slot int, b *caps.ThumbButton, icon uintptr) thumbButton {
	t := thumbButton{mask: thbFlags, id: thumbID(slot)}
	if b == nil {
		t.flags = thbfHidden | thbfDisabled
		return t
	}
	t.mask |= thbTooltip
	copyUTF16(t.tip[:], caps.ThumbButtonName(*b))
	if icon != 0 {
		t.mask |= thbIcon
		t.icon = icon
	}
	switch {
	case b.Hidden:
		t.flags |= thbfHidden
	case b.Disabled:
		t.flags |= thbfDisabled
	default:
		t.flags |= thbfEnabled
	}
	if b.DismissOnClick {
		t.flags |= thbfDismiss
	}
	return t
}

// copyUTF16 writes s into dst as NUL-terminated UTF-16, truncating.
func copyUTF16(dst []uint16, s string) {
	i := 0
	for _, r := range strings.ReplaceAll(s, "\x00", "") {
		if r >= 0x10000 {
			r -= 0x10000
			if i+3 > len(dst) {
				break
			}
			dst[i], dst[i+1] = uint16(0xD800+(r>>10)), uint16(0xDC00+(r&0x3FF))
			i += 2
			continue
		}
		if i+2 > len(dst) {
			break
		}
		dst[i] = uint16(r)
		i++
	}
	if i < len(dst) {
		dst[i] = 0
	}
}

// ncHit builds the hit-test input of a point (screen coordinates) for a
// window with a custom frame.
func ncHit(winL, winT, winR, winB, px, py int, border int, resizable, maximized bool, buttons caps.HitRect, nButtons int) caps.HitInput {
	return caps.HitInput{
		W: winR - winL, H: winB - winT, X: px - winL, Y: py - winT,
		Border: border, Resizable: resizable, Maximized: maximized,
		Buttons: buttons, NButtons: nButtons,
	}
}
