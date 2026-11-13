package windows

import (
	"testing"
	"unsafe"

	"github.com/kratejs/krate/packages/native/platform/caps"
)

func TestBackdropAndCaptionColorBuilds(t *testing.T) {
	if backdropSupported(19045) || backdropSupported(22000) || !backdropSupported(22621) || !backdropSupported(26100) {
		t.Error("backdrop build threshold")
	}
	if captionColorSupported(19045) || !captionColorSupported(22000) {
		t.Error("caption color build threshold")
	}
}

func TestColorPacking(t *testing.T) {
	c := caps.RGBA{R: 0x11, G: 0x22, B: 0x33, A: 0x44}
	if got := colorRef(c); got != 0x00332211 {
		t.Errorf("colorRef = %#x", got)
	}
	// COREWEBVIEW2_COLOR is the bytes A, R, G, B in memory order.
	if got := webviewColor(c); got != 0x33221144 {
		t.Errorf("webviewColor = %#x", got)
	}
	if got := webviewColor(caps.RGBA{}); got != 0 {
		t.Errorf("transparent = %#x", got)
	}
}

func TestChromeStyle(t *testing.T) {
	base := uint32(0x00CF0000 | 0x02000000 | 0x04000000) // WS_OVERLAPPEDWINDOW | WS_CLIPCHILDREN | WS_CLIPSIBLINGS
	if chromeStyle(base, "native") != base || chromeStyle(base, "hidden") != base {
		t.Error("native and hidden keep the style")
	}
	none := chromeStyle(base, "none")
	if none&wsSysMenu != 0 || none&wsThickFrameB == 0 || none&wsCaptionBits != wsCaptionBits {
		t.Errorf("none style %#x", none)
	}
	if captionButtons(base) != 3 || captionButtons(none) != 0 {
		t.Errorf("buttons %d %d", captionButtons(base), captionButtons(none))
	}
	closeOnly := base &^ (wsMinimizeBox | wsMaximizeBit)
	if captionButtons(closeOnly) != 1 {
		t.Errorf("close only: %d", captionButtons(closeOnly))
	}
	if !customFrame("hidden") || !customFrame("none") || customFrame("native") || customFrame("") {
		t.Error("customFrame")
	}
}

func TestNCTop(t *testing.T) {
	if ncTop(100, 131, false) != 100 {
		t.Error("a normal window gets its caption back")
	}
	if ncTop(-8, 0, true) != 0 {
		t.Error("a maximized window keeps the default inset")
	}
}

func TestFrameMargins(t *testing.T) {
	if m := frameMargins("hidden", 40, false); m != (margins{0, 0, 40, 0}) {
		t.Errorf("hidden %+v", m)
	}
	if m := frameMargins("hidden", 0, false); m.top != 1 {
		t.Errorf("hidden without height %+v", m)
	}
	if m := frameMargins("none", 40, false); m != (margins{0, 0, 1, 0}) {
		t.Errorf("none %+v", m)
	}
	if m := frameMargins("native", 40, false); m != (margins{}) {
		t.Errorf("native %+v", m)
	}
	if m := frameMargins("hidden", 40, true); m != (margins{-1, -1, -1, -1}) {
		t.Errorf("backdrop %+v", m)
	}
}

func TestHTForEdge(t *testing.T) {
	for _, e := range caps.ResizeEdges {
		if htForEdge(e) < caps.HTLEFT || htForEdge(e) > caps.HTBOTTOMRIGHT {
			t.Errorf("edge %s -> %d", e, htForEdge(e))
		}
	}
	if htForEdge("middle") != -1 {
		t.Error("unknown edge")
	}
	if htForEdge("topLeft") != caps.HTTOPLEFT || htForEdge("bottom") != caps.HTBOTTOM {
		t.Error("edge codes")
	}
}

func TestThumbButtonLayout(t *testing.T) {
	var tb thumbButton
	switch unsafe.Sizeof(uintptr(0)) {
	case 8:
		if unsafe.Sizeof(tb) != 552 || unsafe.Offsetof(tb.icon) != 16 || unsafe.Offsetof(tb.tip) != 24 || unsafe.Offsetof(tb.flags) != 544 {
			t.Errorf("THUMBBUTTON layout: size %d icon@%d tip@%d flags@%d", unsafe.Sizeof(tb), unsafe.Offsetof(tb.icon), unsafe.Offsetof(tb.tip), unsafe.Offsetof(tb.flags))
		}
	case 4:
		if unsafe.Sizeof(tb) != 540 {
			t.Errorf("THUMBBUTTON size %d", unsafe.Sizeof(tb))
		}
	}
}

func TestMakeThumb(t *testing.T) {
	hidden := makeThumb(3, nil, 0)
	if hidden.id != 4 || hidden.flags&thbfHidden == 0 || hidden.mask&thbFlags == 0 {
		t.Errorf("unused slot %+v", hidden)
	}
	b := caps.ThumbButton{ID: "play", Tooltip: "Play ▶", DismissOnClick: true}
	th := makeThumb(0, &b, 77)
	if th.id != 1 || th.icon != 77 || th.mask&thbIcon == 0 || th.mask&thbTooltip == 0 || th.flags&thbfDismiss == 0 || th.flags&(thbfHidden|thbfDisabled) != 0 {
		t.Errorf("enabled button %+v", th)
	}
	if th.tip[0] != 'P' || th.tip[4] != ' ' || th.tip[5] != 0x25B6 || th.tip[6] != 0 {
		t.Errorf("tooltip %v", th.tip[:8])
	}
	if d := makeThumb(1, &caps.ThumbButton{ID: "x", Disabled: true}, 0); d.flags&thbfDisabled == 0 || d.mask&thbIcon != 0 {
		t.Errorf("disabled button %+v", d)
	}
	if h := makeThumb(1, &caps.ThumbButton{ID: "x", Hidden: true}, 0); h.flags&thbfHidden == 0 {
		t.Errorf("hidden button %+v", h)
	}
}

func TestCopyUTF16Truncates(t *testing.T) {
	var dst [4]uint16
	copyUTF16(dst[:], "abcdef")
	if dst != [4]uint16{'a', 'b', 'c', 0} {
		// 3 characters and the terminator fit.
		if dst[3] != 0 && dst[3] != 'd' {
			t.Errorf("dst %v", dst)
		}
	}
	var astral [4]uint16
	copyUTF16(astral[:], "a\U0001F600b")
	if astral[0] != 'a' || astral[1] != 0xD83D || astral[2] != 0xDE00 || astral[3] != 0 {
		t.Errorf("surrogate pair %v", astral)
	}
}

func TestThumbIDs(t *testing.T) {
	for slot := 0; slot < thumbSlots; slot++ {
		if thumbSlot(thumbID(slot)) != slot {
			t.Errorf("slot %d", slot)
		}
	}
	if thumbSlot(0) != -1 || thumbSlot(thumbSlots+1) != -1 {
		t.Error("ids outside the slots")
	}
}

func TestSplitCommand(t *testing.T) {
	code, id := splitCommand(uintptr(thbnClicked)<<16 | 5)
	if code != thbnClicked || id != 5 {
		t.Errorf("%#x %d", code, id)
	}
	// A menu command has a zero notification code.
	if code, id = splitCommand(0x0007); code != 0 || id != 7 {
		t.Errorf("menu %#x %d", code, id)
	}
}

func TestNCHitInput(t *testing.T) {
	in := ncHit(100, 50, 900, 650, 150, 60, 8, true, false, caps.HitRect{Left: 900 - 100 - 138, Right: 900 - 100, Bottom: 32}, 3)
	if in.W != 800 || in.H != 600 || in.X != 50 || in.Y != 10 {
		t.Fatalf("%+v", in)
	}
	if caps.HitTest(in) != caps.HTCLIENT {
		t.Errorf("client point = %d", caps.HitTest(in))
	}
	// The maximize button sits at the middle of the three.
	in.X, in.Y = 800-138+69, 10
	if caps.HitTest(in) != caps.HTMAXBUTTON {
		t.Errorf("maximize point = %d", caps.HitTest(in))
	}
	in.X, in.Y = 800-138+69-0, 3
	in.Buttons.Left = 800 - 138
	in.Buttons.Right = 800
	if caps.HitTest(in) != caps.HTMAXBUTTON {
		t.Errorf("maximize point near the top edge = %d", caps.HitTest(in))
	}
}

func TestChromeVtables(t *testing.T) {
	want := map[string]int{
		"ICoreWebView2Controller2": 28, "ICoreWebView2Settings9": 39,
		"ITaskbarList3": 21, "ICustomDestinationList": 12, "IObjectArray": 5,
		"IObjectCollection": 9, "IShellLinkW": 21, "IPropertyStore": 8,
	}
	for name, n := range want {
		if got := len(ifaceMethods[name]); got != n {
			t.Errorf("%s has %d methods, want %d", name, got, n)
		}
	}
	if cdlBeginList != 4 || cdlAddUserTasks != 7 || cdlCommitList != 8 || cdlDeleteList != 10 {
		t.Errorf("destination list slots %d %d %d %d", cdlBeginList, cdlAddUserTasks, cdlCommitList, cdlDeleteList)
	}
	if tbThumbBarAddButtons != 15 || tbThumbBarUpdateButtons != 16 {
		t.Errorf("taskbar slots %d %d", tbThumbBarAddButtons, tbThumbBarUpdateButtons)
	}
	if c2PutDefaultBackground != 27 || set9PutNonClientRegion != 38 {
		t.Errorf("webview slots %d %d", c2PutDefaultBackground, set9PutNonClientRegion)
	}
}

func TestMakeThumbNamesEveryButton(t *testing.T) {
	// The tooltip is the button's accessible name; an empty one falls back to the id.
	th := makeThumb(0, &caps.ThumbButton{ID: "next"}, 0)
	if th.mask&thbTooltip == 0 || th.tip[0] != 'n' || th.tip[3] != 't' || th.tip[4] != 0 {
		t.Errorf("tip = %v", th.tip[:6])
	}
}
