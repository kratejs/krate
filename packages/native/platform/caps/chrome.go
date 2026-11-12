package caps

import (
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Window chrome: title bar styles, backdrops, drag regions, platform
// decorations and the pure logic behind them (validation, colors, hit
// testing, command lines). The per-OS packages implement the interfaces; the
// SDK type-asserts for them and answers krate.ErrUnsupported when one is
// missing or a platform method has nothing to do on that OS.

// Title bar styles.
const (
	// TitleBarNative is the operating system's title bar and border.
	TitleBarNative = "native"
	// TitleBarHidden hides the title bar but keeps the window controls: the
	// traffic lights on macOS (over the page, see TrafficLight), the caption
	// buttons on Windows (see TitleBarOverlay). On Linux it removes the
	// decorations like TitleBarNone; the page draws its own controls.
	TitleBarHidden = "hidden"
	// TitleBarNone removes the title bar and the window controls. The window
	// keeps its shadow and, where the OS allows, its resize edges.
	TitleBarNone = "none"
)

// TitleBarStyles lists the valid title bar styles.
var TitleBarStyles = []string{TitleBarNative, TitleBarHidden, TitleBarNone}

// NormalizeTitleBar maps "" to TitleBarNative and reports whether s is a
// valid style.
func NormalizeTitleBar(s string) (string, bool) {
	if s == "" {
		return TitleBarNative, true
	}
	for _, v := range TitleBarStyles {
		if s == v {
			return s, true
		}
	}
	return s, false
}

// Point is a position in device-independent pixels.
type Point struct{ X, Y int }

// TitleBarOverlay describes the caption buttons the OS draws over a hidden
// title bar (Windows). Colors are "#rgb", "#rrggbb" or "#rrggbbaa"; an empty
// color keeps the system's. Height is the height of the draggable strip in
// device-independent pixels; zero means DefaultOverlayHeight.
type TitleBarOverlay struct {
	Color       string
	SymbolColor string
	Height      int
}

// DefaultOverlayHeight is the height of the caption strip when none is given.
const DefaultOverlayHeight = 32

// MaxOverlayHeight bounds Height.
const MaxOverlayHeight = 200

// Normalize validates o and fills the default height.
func (o TitleBarOverlay) Normalize() (TitleBarOverlay, error) {
	if o.Height < 0 || o.Height > MaxOverlayHeight {
		return o, fmt.Errorf("titleBarOverlay.height %d is out of range 0..%d", o.Height, MaxOverlayHeight)
	}
	if o.Height == 0 {
		o.Height = DefaultOverlayHeight
	}
	for _, c := range []struct{ name, v string }{{"color", o.Color}, {"symbolColor", o.SymbolColor}} {
		if c.v == "" {
			continue
		}
		if _, err := ParseColor(c.v); err != nil {
			return o, fmt.Errorf("titleBarOverlay.%s: %w", c.name, err)
		}
	}
	return o, nil
}

// RGBA is a color with 8-bit channels.
type RGBA struct{ R, G, B, A uint8 }

// ParseColor reads "#rgb", "#rrggbb" or "#rrggbbaa".
func ParseColor(s string) (RGBA, error) {
	h, ok := strings.CutPrefix(s, "#")
	if !ok {
		return RGBA{}, fmt.Errorf("color %q must start with #", s)
	}
	switch len(h) {
	case 3:
		h = string([]byte{h[0], h[0], h[1], h[1], h[2], h[2]})
	case 6, 8:
	default:
		return RGBA{}, fmt.Errorf("color %q must be #rgb, #rrggbb or #rrggbbaa", s)
	}
	v, err := strconv.ParseUint(h, 16, 32)
	if err != nil {
		return RGBA{}, fmt.Errorf("color %q is not hexadecimal", s)
	}
	if len(h) == 6 {
		return RGBA{uint8(v >> 16), uint8(v >> 8), uint8(v), 255}, nil
	}
	return RGBA{uint8(v >> 24), uint8(v >> 16), uint8(v >> 8), uint8(v)}, nil
}

// Backdrop names (Windows 11 22H2 and later). They map to
// DWMWA_SYSTEMBACKDROP_TYPE.
const (
	BackdropNone    = "none"
	BackdropMica    = "mica"
	BackdropMicaAlt = "micaAlt"
	BackdropAcrylic = "acrylic"
)

// backdropTypes maps a backdrop name to its DWM_SYSTEMBACKDROP_TYPE value.
var backdropTypes = map[string]int{
	BackdropNone:    1, // DWMSBT_NONE
	BackdropMica:    2, // DWMSBT_MAINWINDOW
	BackdropAcrylic: 3, // DWMSBT_TRANSIENTWINDOW
	BackdropMicaAlt: 4, // DWMSBT_TABBEDWINDOW
}

// BackdropNames lists the valid backdrop names, sorted.
func BackdropNames() []string { return sortedKeys(backdropTypes) }

// BackdropType returns the DWM_SYSTEMBACKDROP_TYPE of a backdrop name. The
// empty name means BackdropNone.
func BackdropType(name string) (int, bool) {
	if name == "" {
		name = BackdropNone
	}
	v, ok := backdropTypes[name]
	return v, ok
}

// vibrancyMaterials maps a vibrancy name to its NSVisualEffectMaterial value
// (the non-deprecated materials).
var vibrancyMaterials = map[string]int{
	"titlebar": 3, "selection": 4, "menu": 5, "popover": 6, "sidebar": 7,
	"headerView": 10, "sheet": 11, "windowBackground": 12, "hudWindow": 13,
	"fullScreenUI": 15, "tooltip": 17, "contentBackground": 18,
	"underWindowBackground": 21, "underPageBackground": 22,
}

// VibrancyNames lists the valid vibrancy materials, sorted.
func VibrancyNames() []string { return sortedKeys(vibrancyMaterials) }

// VibrancyMaterial returns the NSVisualEffectMaterial of a vibrancy name.
func VibrancyMaterial(name string) (int, bool) {
	v, ok := vibrancyMaterials[name]
	return v, ok
}

func sortedKeys(m map[string]int) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Tabbing modes of a macOS window (NSWindowTabbingMode).
const (
	TabbingAuto       = "auto"
	TabbingPreferred  = "preferred"
	TabbingDisallowed = "disallowed"
)

// TabbingModeValue returns the NSWindowTabbingMode value of a mode name; the
// empty name means TabbingAuto.
func TabbingModeValue(mode string) (int, bool) {
	switch mode {
	case "", TabbingAuto:
		return 0, true
	case TabbingPreferred:
		return 1, true
	case TabbingDisallowed:
		return 2, true
	}
	return 0, false
}

// Activation policies of the app (macOS).
const (
	PolicyRegular    = "regular"
	PolicyAccessory  = "accessory"
	PolicyProhibited = "prohibited"
)

// ActivationPolicyValue returns the NSApplicationActivationPolicy value.
func ActivationPolicyValue(p string) (int, bool) {
	switch p {
	case PolicyRegular:
		return 0, true
	case PolicyAccessory:
		return 1, true
	case PolicyProhibited:
		return 2, true
	}
	return 0, false
}

// ChromeOptions are the chrome settings a window is created with. Linux needs
// Transparent before the window exists; the other fields can also be changed
// later through WindowChrome.
type ChromeOptions struct {
	// TitleBar is a title bar style; empty means TitleBarNative.
	TitleBar string
	// Transparent makes the window and its webview background transparent.
	Transparent bool
	// Backdrop is a backdrop name (Windows).
	Backdrop string
	// Vibrancy is a vibrancy material (macOS).
	Vibrancy string
	// Overlay, when set, draws the caption buttons over a hidden title bar
	// (Windows).
	Overlay *TitleBarOverlay
	// TrafficLight, when set, moves the traffic lights (macOS).
	TrafficLight *Point
	// TabbingID and TabbingMode set native window tabbing (macOS).
	TabbingID   string
	TabbingMode string
}

// IsZero reports whether o asks for nothing.
func (o ChromeOptions) IsZero() bool {
	return (o.TitleBar == "" || o.TitleBar == TitleBarNative) && !o.Transparent && o.Backdrop == "" &&
		o.Vibrancy == "" && o.Overlay == nil && o.TrafficLight == nil && o.TabbingID == "" && o.TabbingMode == ""
}

// Validate checks every field and returns the options with defaults applied.
func (o ChromeOptions) Validate() (ChromeOptions, error) {
	tb, ok := NormalizeTitleBar(o.TitleBar)
	if !ok {
		return o, fmt.Errorf("titleBar %q must be one of %s", o.TitleBar, strings.Join(TitleBarStyles, ", "))
	}
	o.TitleBar = tb
	if _, ok := BackdropType(o.Backdrop); !ok {
		return o, fmt.Errorf("backdrop %q must be one of %s", o.Backdrop, strings.Join(BackdropNames(), ", "))
	}
	if o.Vibrancy != "" {
		if _, ok := VibrancyMaterial(o.Vibrancy); !ok {
			return o, fmt.Errorf("vibrancy %q must be one of %s", o.Vibrancy, strings.Join(VibrancyNames(), ", "))
		}
	}
	if o.Overlay != nil {
		ov, err := o.Overlay.Normalize()
		if err != nil {
			return o, err
		}
		o.Overlay = &ov
	}
	if p := o.TrafficLight; p != nil && (p.X < 0 || p.Y < 0 || p.X > 1000 || p.Y > 1000) {
		return o, fmt.Errorf("trafficLightPosition (%d, %d) is out of range 0..1000", p.X, p.Y)
	}
	if _, ok := TabbingModeValue(o.TabbingMode); !ok {
		return o, fmt.Errorf("tabbingMode %q must be auto, preferred or disallowed", o.TabbingMode)
	}
	if utf8.RuneCountInString(o.TabbingID) > 128 {
		return o, errors.New("tabbingIdentifier is longer than 128 characters")
	}
	return o, nil
}

// ChromeState is a snapshot of a window's chrome.
type ChromeState struct {
	TitleBar     string
	Transparent  bool
	Backdrop     string
	Vibrancy     string
	Overlay      *TitleBarOverlay
	TrafficLight *Point
	// DocumentEdited and RepresentedFile are the macOS document state.
	DocumentEdited  bool
	RepresentedFile string
	// TabbingID, TabbingMode and Tabs describe the window's tab group; Tabs
	// is the number of windows in it (1 when the window is alone).
	TabbingID   string
	TabbingMode string
	Tabs        int
	// Sheet reports whether the window is shown as a sheet of another one.
	Sheet bool
	// ThumbButtons is the number of taskbar thumbnail buttons (Windows).
	ThumbButtons int
	// NativeRegions reports that the web engine itself honors the CSS
	// app-region property (WebView2), so the page script leaves it alone.
	NativeRegions bool
	// Resizable and Decorated are the window's frame state, which the drag
	// script needs to decide whether to draw resize edges.
	Resizable bool
	Decorated bool
	// Features names what the window supports, from the Feature constants.
	Features []string
	// ScriptResize reports that the OS gives a frameless window no resize
	// border (Linux), so the page script draws resize edges.
	ScriptResize bool
}

// Chrome features a window can report in ChromeState.Features, and the
// names app.supports answers as "chrome:<feature>".
const (
	FeatureTitleBar        = "titleBar"
	FeatureTransparent     = "transparent"
	FeatureBackdrop        = "backdrop"
	FeatureVibrancy        = "vibrancy"
	FeatureTitleBarOverlay = "titleBarOverlay"
	FeatureTrafficLights   = "trafficLights"
	FeatureDrag            = "drag"
	FeatureResize          = "resize"
	FeatureDocument        = "document"
	FeatureSheets          = "sheets"
	FeatureTabs            = "tabs"
	FeatureThumbButtons    = "thumbButtons"
)

// App-level features, answered by the App itself.
const (
	FeatureJumpList = "jumpList"
	FeatureShare    = "share"
	FeatureDock     = "dock"
)

// HasFeature reports whether the state lists a feature.
func (s ChromeState) HasFeature(f string) bool {
	for _, x := range s.Features {
		if x == f {
			return true
		}
	}
	return false
}

// ChromeEvent is delivered to the handler set with ChromeEvents.
type ChromeEvent struct {
	Window WindowRef
	// Kind is "new-tab" (the user asked for a new tab in a tabbed window),
	// "tabs-changed", "thumb-click" or "sheet-ended".
	Kind string
	// ID is the thumbnail button id for "thumb-click".
	ID string
}

// Chrome event kinds.
const (
	ChromeNewTab      = "new-tab"
	ChromeTabsChanged = "tabs-changed"
	ChromeThumbClick  = "thumb-click"
	ChromeSheetEnded  = "sheet-ended"
)

// WindowChrome is implemented by Window values that support the chrome
// features common to the platforms. A method that has nothing to do on the
// platform returns an error wrapping krate.ErrUnsupported.
type WindowChrome interface {
	ChromeState() ChromeState
	SetTitleBar(style string) error
	// SetBackdrop sets a Windows system backdrop.
	SetBackdrop(name string) error
	// SetVibrancy sets a macOS vibrancy material; "" removes it.
	SetVibrancy(material string) error
	// SetTitleBarOverlay installs (or with nil removes) the caption button
	// overlay of a hidden title bar.
	SetTitleBarOverlay(o *TitleBarOverlay) error
	SetTrafficLightPosition(p Point) error
	// StartDrag starts a native window move from the pointer press that is
	// happening now; the injected page script calls it on mousedown in a
	// drag region.
	StartDrag() error
	// StartResize starts a native resize from an edge ("top", "bottomRight"
	// and so on) for frameless windows.
	StartResize(edge string) error
	// ToggleMaximize maximizes or restores the window, as a double click on
	// a title bar does.
	ToggleMaximize() error
}

// TitleBarDoubleClicker is implemented by Windows whose operating system
// decides what a double click on the title bar does (macOS: zoom, minimize or
// nothing, by the user's setting). The page script uses it when it can and
// otherwise falls back to WindowChrome.ToggleMaximize.
type TitleBarDoubleClicker interface {
	TitleBarDoubleClick() error
}

// WindowDocument is implemented by Windows with a document state (macOS).
type WindowDocument interface {
	SetDocumentEdited(on bool) error
	// SetRepresentedFile sets the file whose icon the title bar shows (the
	// proxy icon); "" clears it.
	SetRepresentedFile(path string) error
}

// WindowSheets is implemented by Windows that can show another window of the
// app as a sheet (macOS).
type WindowSheets interface {
	// AttachSheet shows child as a sheet attached to the receiver.
	AttachSheet(child WindowRef) error
	// EndSheet ends the receiver's sheet, if it is one.
	EndSheet() error
}

// WindowTabs is implemented by Windows with native tabs (macOS).
type WindowTabs interface {
	SetTabbing(id, mode string) error
	// MergeAllWindows moves every window with the same tabbing identifier
	// into one tab group.
	MergeAllWindows() error
	// MoveTabToNewWindow takes the receiver out of its tab group.
	MoveTabToNewWindow() error
	// Tabs lists the windows in the receiver's tab group, in tab order.
	Tabs() []WindowRef
}

// MaxThumbButtons is the number of buttons a taskbar thumbnail can hold.
const MaxThumbButtons = 7

// ThumbButton is a button in the taskbar thumbnail of a window (Windows).
type ThumbButton struct {
	ID      string
	Tooltip string
	// Icon is a PNG image.
	Icon           []byte
	Disabled       bool
	Hidden         bool
	DismissOnClick bool
}

// ValidateThumbButtons checks a button list.
func ValidateThumbButtons(bs []ThumbButton) error {
	if len(bs) > MaxThumbButtons {
		return fmt.Errorf("a taskbar thumbnail holds at most %d buttons, got %d", MaxThumbButtons, len(bs))
	}
	seen := map[string]bool{}
	for i, b := range bs {
		if b.ID == "" {
			return fmt.Errorf("thumbnail button %d has no id", i)
		}
		if seen[b.ID] {
			return fmt.Errorf("thumbnail button id %q is used twice", b.ID)
		}
		seen[b.ID] = true
		if utf16Len(b.Tooltip) > 259 {
			return fmt.Errorf("thumbnail button %q: tooltip is longer than 259 characters", b.ID)
		}
		if len(b.Icon) > 1<<20 {
			return fmt.Errorf("thumbnail button %q: icon is larger than 1 MiB", b.ID)
		}
	}
	return nil
}

// isAbsAny reports whether p is absolute as a Windows or a Unix path, so the
// check gives the same answer on every host.
func isAbsAny(p string) bool {
	if strings.HasPrefix(p, "/") || strings.HasPrefix(p, `\\`) {
		return true
	}
	return len(p) >= 3 && p[1] == ':' && (p[2] == '\\' || p[2] == '/') &&
		(p[0] >= 'a' && p[0] <= 'z' || p[0] >= 'A' && p[0] <= 'Z')
}

func utf16Len(s string) int {
	n := 0
	for _, r := range s {
		if r >= 0x10000 {
			n += 2
		} else {
			n++
		}
	}
	return n
}

// WindowThumbBar is implemented by Windows with taskbar thumbnail buttons.
// Clicks arrive as ChromeThumbClick events.
type WindowThumbBar interface {
	// SetThumbButtons replaces the buttons; nil or empty removes them.
	SetThumbButtons(bs []ThumbButton) error
}

// JumpTask is a task in the taskbar jump list (Windows): choosing it starts
// the app with Args, which a running instance receives as a second-instance
// event.
type JumpTask struct {
	Title       string
	Args        []string
	Description string
	// IconPath is an .exe or .ico file; IconIndex picks the icon in it.
	IconPath  string
	IconIndex int
}

// MaxJumpTasks bounds a jump list.
const MaxJumpTasks = 20

// ValidateJumpTasks checks a task list.
func ValidateJumpTasks(ts []JumpTask) error {
	if len(ts) > MaxJumpTasks {
		return fmt.Errorf("a jump list holds at most %d tasks, got %d", MaxJumpTasks, len(ts))
	}
	for i, t := range ts {
		if strings.TrimSpace(t.Title) == "" {
			return fmt.Errorf("jump task %d has no title", i)
		}
		if utf16Len(t.Title) > 256 || utf16Len(t.Description) > 512 {
			return fmt.Errorf("jump task %q: title or description is too long", t.Title)
		}
		for _, s := range append([]string{t.Title, t.Description, t.IconPath}, t.Args...) {
			if strings.ContainsRune(s, 0) {
				return fmt.Errorf("jump task %q contains a NUL character", t.Title)
			}
		}
		if t.IconPath != "" && !isAbsAny(t.IconPath) && !strings.HasPrefix(t.IconPath, "%") {
			return fmt.Errorf("jump task %q: iconPath must be absolute", t.Title)
		}
	}
	return nil
}

// QuoteArgs joins arguments into a Windows command line that
// CommandLineToArgvW (and the Go runtime) split back into the same list.
func QuoteArgs(args []string) string {
	var b strings.Builder
	for i, a := range args {
		if i > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(quoteArg(a))
	}
	return b.String()
}

func quoteArg(s string) string {
	if s == "" {
		return `""`
	}
	if !strings.ContainsAny(s, " \t\n\v\"") {
		return s
	}
	var b strings.Builder
	b.WriteByte('"')
	slashes := 0
	for _, r := range s {
		switch r {
		case '\\':
			slashes++
			b.WriteByte('\\')
		case '"':
			// Double the backslashes before a quote, then escape the quote.
			for ; slashes > 0; slashes-- {
				b.WriteByte('\\')
			}
			b.WriteString(`\"`)
		default:
			slashes = 0
			b.WriteRune(r)
		}
	}
	// Backslashes before the closing quote must be doubled too.
	for ; slashes > 0; slashes-- {
		b.WriteByte('\\')
	}
	b.WriteByte('"')
	return b.String()
}

// JumpArgs returns the argument list a jump list task launches the app with.
// The shell reads flags of its own (--browser, --addr), so a task argument
// that starts with "-" goes after a "--", which ends the shell's flag
// parsing; LaunchArgs drops that leading "--" again.
func JumpArgs(args []string) []string {
	for _, a := range args {
		if strings.HasPrefix(a, "-") && a != "--" {
			return append([]string{"--"}, args...)
		}
	}
	return args
}

// JumpLister is implemented by Apps that can set the taskbar jump list
// (Windows).
type JumpLister interface {
	// SetJumpList replaces the user tasks; the list is committed before the
	// call returns.
	SetJumpList(tasks []JumpTask) error
	// ClearJumpList removes every custom jump list entry.
	ClearJumpList() error
}

// AppPresence is implemented by Apps that can change how they appear in the
// dock and app switcher (macOS).
type AppPresence interface {
	// SetActivationPolicy is PolicyRegular, PolicyAccessory (no dock icon
	// and no menu bar, a menu-bar-only app) or PolicyProhibited.
	SetActivationPolicy(policy string) error
	// ActivationPolicy reports the current policy.
	ActivationPolicy() string
	// SetDockVisible shows or hides the dock icon: PolicyRegular or
	// PolicyAccessory.
	SetDockVisible(on bool) error
}

// ShareItems is what the share menu offers.
type ShareItems struct {
	Text  string
	URLs  []string
	Files []string
}

// Validate checks the items: at least one, URLs with a safe scheme, absolute
// file paths.
func (s ShareItems) Validate() error {
	if s.Text == "" && len(s.URLs) == 0 && len(s.Files) == 0 {
		return errors.New("share needs text, a URL or a file")
	}
	if len(s.URLs)+len(s.Files) > 50 {
		return errors.New("share takes at most 50 URLs and files")
	}
	if len(s.Text) > 1<<20 {
		return errors.New("share text is larger than 1 MiB")
	}
	for _, u := range s.URLs {
		p, err := url.Parse(u)
		if err != nil || (p.Scheme != "http" && p.Scheme != "https" && p.Scheme != "mailto") {
			return fmt.Errorf("share URL %q must be an http, https or mailto URL", u)
		}
	}
	for _, f := range s.Files {
		if !filepath.IsAbs(f) || strings.ContainsRune(f, 0) {
			return fmt.Errorf("share file %q must be an absolute path", f)
		}
	}
	return nil
}

// Sharer is implemented by Apps that can show the system share menu (macOS).
type Sharer interface {
	// Share shows the share picker anchored on a window and returns without
	// waiting for the user's choice.
	Share(parent WindowRef, items ShareItems) error
}

// ChromeEvents is implemented by Apps that report chrome events.
type ChromeEvents interface {
	// OnChromeEvent sets the single handler; fn runs on the main thread and
	// must not block.
	OnChromeEvent(fn func(ChromeEvent))
}

// Win32 hit-test codes (the values of the HT constants).
const (
	HTNOWHERE     = 0
	HTCLIENT      = 1
	HTCAPTION     = 2
	HTLEFT        = 10
	HTRIGHT       = 11
	HTTOP         = 12
	HTTOPLEFT     = 13
	HTTOPRIGHT    = 14
	HTBOTTOM      = 15
	HTBOTTOMLEFT  = 16
	HTBOTTOMRIGHT = 17
	HTMINBUTTON   = 8
	HTMAXBUTTON   = 9
	HTCLOSE       = 20
)

// HitRect is a rectangle; Right and Bottom are exclusive.
type HitRect struct{ Left, Top, Right, Bottom int }

// Contains reports whether the point is inside r.
func (r HitRect) Contains(x, y int) bool {
	return x >= r.Left && x < r.Right && y >= r.Top && y < r.Bottom
}

// HitInput is the geometry of one hit test. All coordinates are in pixels
// relative to the window's top-left corner.
type HitInput struct {
	W, H int
	X, Y int
	// Border is the thickness of the resize border; ignored unless Resizable.
	Border    int
	Resizable bool
	Maximized bool
	// CaptionH is the height of the draggable strip along the top (0 for
	// none). Pages mark finer regions themselves; this is the OS-level strip
	// above the page.
	CaptionH int
	// Buttons is the bounds of the OS caption buttons (from
	// DWMWA_CAPTION_BUTTON_BOUNDS) and NButtons how many there are (1 for
	// close only, 3 for minimize, maximize and close).
	Buttons  HitRect
	NButtons int
}

// HitTest answers WM_NCHITTEST for a window with a hidden title bar and
// caption buttons over its client area: the buttons first (HTMAXBUTTON on the
// maximize button lets Windows 11 show the Snap Layouts flyout), then the
// resize border, then the caption strip, otherwise the client area.
func HitTest(in HitInput) int {
	if in.X < 0 || in.Y < 0 || in.X >= in.W || in.Y >= in.H {
		return HTNOWHERE
	}
	if in.NButtons > 0 && in.Buttons.Contains(in.X, in.Y) {
		w := in.Buttons.Right - in.Buttons.Left
		idx := (in.X - in.Buttons.Left) * in.NButtons / w
		switch in.NButtons {
		case 1:
			return HTCLOSE
		case 2:
			if idx == 0 {
				return HTMAXBUTTON
			}
			return HTCLOSE
		default:
			switch idx {
			case 0:
				return HTMINBUTTON
			case 1:
				return HTMAXBUTTON
			}
			return HTCLOSE
		}
	}
	if in.Resizable && !in.Maximized && in.Border > 0 {
		left, right := in.X < in.Border, in.X >= in.W-in.Border
		top, bottom := in.Y < in.Border, in.Y >= in.H-in.Border
		switch {
		case top && left:
			return HTTOPLEFT
		case top && right:
			return HTTOPRIGHT
		case bottom && left:
			return HTBOTTOMLEFT
		case bottom && right:
			return HTBOTTOMRIGHT
		case top:
			return HTTOP
		case bottom:
			return HTBOTTOM
		case left:
			return HTLEFT
		case right:
			return HTRIGHT
		}
	}
	if in.CaptionH > 0 && in.Y < in.CaptionH {
		return HTCAPTION
	}
	return HTCLIENT
}

// ResizeEdges are the names StartResize accepts, in the order of GDK's
// GdkWindowEdge values (north-west first).
var ResizeEdges = []string{"topLeft", "top", "topRight", "left", "right", "bottomLeft", "bottom", "bottomRight"}

// EdgeIndex returns the position of an edge name in ResizeEdges, or -1.
func EdgeIndex(name string) int {
	for i, e := range ResizeEdges {
		if e == name {
			return i
		}
	}
	return -1
}
