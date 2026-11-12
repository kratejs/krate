package sdk_test

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kratejs/krate/packages/native/krate"
	"github.com/kratejs/krate/packages/native/platform"
	"github.com/kratejs/krate/packages/native/platform/caps"
	"github.com/kratejs/krate/packages/native/sdk"
	"github.com/kratejs/krate/packages/native/sdk/app"
	"github.com/kratejs/krate/packages/native/sdk/chrome"
	"github.com/kratejs/krate/packages/native/sdk/internal/fakeplat"
	"github.com/kratejs/krate/packages/native/sdk/window"
	"github.com/kratejs/krate/packages/native/wire"
)

func TestChromeWindowOps(t *testing.T) {
	r := newRig(t)
	w := chrome.Main()

	st, err := w.State()
	if err != nil || st.TitleBar != "" && st.TitleBar != "native" || st.Tabs != 1 {
		t.Fatalf("state %+v %v", st, err)
	}
	if err := w.SetTitleBar("hidden"); err != nil {
		t.Fatal(err)
	}
	if err := w.SetBackdrop("acrylic"); err != nil {
		t.Fatal(err)
	}
	if err := w.SetVibrancy("sidebar"); err != nil {
		t.Fatal(err)
	}
	if err := w.SetTitleBarOverlay(&chrome.Overlay{Color: "#101010", Height: 40}); err != nil {
		t.Fatal(err)
	}
	if err := w.SetTrafficLightPosition(14, 18); err != nil {
		t.Fatal(err)
	}
	if err := w.SetDocumentEdited(true); err != nil {
		t.Fatal(err)
	}
	if err := w.SetRepresentedFile("/tmp/notes.txt"); err != nil {
		t.Fatal(err)
	}
	if err := w.SetTabbing("docs", "preferred"); err != nil {
		t.Fatal(err)
	}
	st, err = w.State()
	if err != nil {
		t.Fatal(err)
	}
	if st.TitleBar != "hidden" || st.Backdrop != "acrylic" || st.Vibrancy != "sidebar" ||
		st.Overlay == nil || st.Overlay.Height != 40 || st.Overlay.Color != "#101010" ||
		st.TrafficLight == nil || st.TrafficLight.X != 14 || st.TrafficLight.Y != 18 ||
		!st.DocumentEdited || st.RepresentedFile != "/tmp/notes.txt" || st.TabbingID != "docs" || st.TabbingMode != "preferred" {
		t.Fatalf("state %+v", st)
	}
	if len(st.Features) < 5 {
		t.Fatalf("features %v", st.Features)
	}
	// Removing the overlay and vibrancy.
	if err := w.SetTitleBarOverlay(nil); err != nil {
		t.Fatal(err)
	}
	if err := w.SetVibrancy(""); err != nil {
		t.Fatal(err)
	}
	if st, _ = w.State(); st.Overlay != nil || st.Vibrancy != "" {
		t.Fatalf("not removed: %+v", st)
	}
	if err := w.StartDrag(); err != nil {
		t.Fatal(err)
	}
	if err := w.ToggleMaximize(); err != nil {
		t.Fatal(err)
	}
	called := strings.Join(r.main.Called(), ",")
	for _, want := range []string{"startdrag", "togglemaximize", "titlebar hidden", "backdrop acrylic"} {
		if !strings.Contains(called, want) {
			t.Errorf("platform calls lack %q: %s", want, called)
		}
	}
}

func TestChromeRejectsBadArguments(t *testing.T) {
	newRig(t)
	w := chrome.Main()
	for name, err := range map[string]error{
		"titlebar":   w.SetTitleBar("floating"),
		"empty bar":  w.SetTitleBar(""),
		"backdrop":   w.SetBackdrop("glass"),
		"vibrancy":   w.SetVibrancy("jelly"),
		"overlay":    w.SetTitleBarOverlay(&chrome.Overlay{Color: "blue"}),
		"overlayH":   w.SetTitleBarOverlay(&chrome.Overlay{Height: 900}),
		"traffic":    w.SetTrafficLightPosition(-1, 4),
		"tabbing":    w.SetTabbing("x", "many"),
		"file":       w.SetRepresentedFile("relative/path"),
		"self sheet": w.AttachSheet(w),
		"no sheet":   w.AttachSheet(chrome.Get("ghost")),
		"no window":  chrome.Get("ghost").SetTitleBar("none"),
		"thumbs":     w.SetThumbButtons(make([]chrome.ThumbButton, 8)),
		"thumb dup":  w.SetThumbButtons([]chrome.ThumbButton{{ID: "a"}, {ID: "a"}}),
		"share":      w.Share(chrome.ShareItems{}),
		"share url":  w.Share(chrome.ShareItems{URLs: []string{"javascript:1"}}),
		"policy":     chrome.SetActivationPolicy("agent"),
		"jump":       chrome.SetJumpList([]chrome.JumpTask{{Title: ""}}),
		"jump nul":   chrome.SetJumpList([]chrome.JumpTask{{Title: "a", Args: []string{"x\x00"}}}),
	} {
		if err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if err := chrome.Get("ghost").SetTitleBar("none"); err == nil || !strings.Contains(err.Error(), "no window") {
		t.Errorf("unknown window: %v", err)
	}
}

func TestChromeSheetsTabsThumbs(t *testing.T) {
	r := newRig(t)
	w2pw, err := r.app.NewWindow(platform.WindowOptions{Title: "two"})
	if err != nil {
		t.Fatal(err)
	}
	r.host.AddWindow("two", w2pw)
	main, two := chrome.Main(), chrome.Get("two")

	if err := main.AttachSheet(two); err != nil {
		t.Fatal(err)
	}
	if st, _ := main.State(); !st.Sheet {
		t.Fatal("sheet not attached")
	}
	if err := main.EndSheet(); err != nil {
		t.Fatal(err)
	}
	if st, _ := main.State(); st.Sheet {
		t.Fatal("sheet still attached")
	}

	if err := main.MergeAllWindows(); err != nil {
		t.Fatal(err)
	}
	tabs, err := main.Tabs()
	if err != nil || len(tabs) != 2 {
		t.Fatalf("tabs %v %v", tabs, err)
	}
	if err := two.MoveTabToNewWindow(); err != nil {
		t.Fatal(err)
	}
	if tabs, _ = two.Tabs(); len(tabs) != 1 || tabs[0] != "two" {
		t.Fatalf("tabs after moving %v", tabs)
	}

	icon := []byte{0x89, 'P', 'N', 'G'}
	if err := main.SetThumbButtons([]chrome.ThumbButton{{ID: "play", Tooltip: "Play", Icon: icon}, {ID: "next", Disabled: true, DismissOnClick: true}}); err != nil {
		t.Fatal(err)
	}
	got := r.main.ThumbButtons()
	if len(got) != 2 || got[0].ID != "play" || got[0].Tooltip != "Play" || string(got[0].Icon) != string(icon) || !got[1].Disabled || !got[1].DismissOnClick {
		t.Fatalf("buttons %+v", got)
	}
	if st, _ := main.State(); st.ThumbButtons != 2 {
		t.Fatalf("state %+v", st)
	}
	if err := main.SetThumbButtons(nil); err != nil {
		t.Fatal(err)
	}
	if len(r.main.ThumbButtons()) != 0 {
		t.Fatal("buttons not cleared")
	}
}

func TestChromeAppLevel(t *testing.T) {
	r := newRig(t)
	tasks := []chrome.JumpTask{
		{Title: "New window", Args: []string{"--new-window"}, Description: "Open a window", IconPath: `C:\app.exe`, IconIndex: 1},
		{Title: "Open file", Args: []string{"--open", "a b.txt"}},
	}
	if err := chrome.SetJumpList(tasks); err != nil {
		t.Fatal(err)
	}
	if len(r.app.Jump) != 2 || r.app.Jump[0].Title != "New window" || r.app.Jump[0].IconIndex != 1 || r.app.Jump[1].Args[1] != "a b.txt" {
		t.Fatalf("jump list %+v", r.app.Jump)
	}
	if err := chrome.ClearJumpList(); err != nil {
		t.Fatal(err)
	}
	if len(r.app.Jump) != 0 {
		t.Fatalf("jump list not cleared: %+v", r.app.Jump)
	}
	if err := chrome.SetActivationPolicy("accessory"); err != nil {
		t.Fatal(err)
	}
	if r.app.ActivationPolicy() != "accessory" {
		t.Fatalf("policy %q", r.app.ActivationPolicy())
	}
	if err := chrome.SetDockVisible(true); err != nil {
		t.Fatal(err)
	}
	if r.app.ActivationPolicy() != "regular" {
		t.Fatalf("policy %q", r.app.ActivationPolicy())
	}
	if err := chrome.Main().Share(chrome.ShareItems{Text: "hi", URLs: []string{"https://krate.js.org"}, Files: []string{"/tmp/x.txt"}}); err != nil {
		t.Fatal(err)
	}
	if len(r.app.Shared) != 1 || r.app.Shared[0].Text != "hi" || r.app.Shared[0].URLs[0] != "https://krate.js.org" {
		t.Fatalf("shared %+v", r.app.Shared)
	}
}

func TestChromeEvents(t *testing.T) {
	r := newRig(t)
	var mu sync.Mutex
	var got []chrome.Event
	cancel, err := chrome.OnEvent(func(e chrome.Event) { mu.Lock(); got = append(got, e); mu.Unlock() })
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()
	var clicks []string
	c2, err := chrome.Main().OnThumbClick(func(id string) { mu.Lock(); clicks = append(clicks, id); mu.Unlock() })
	if err != nil {
		t.Fatal(err)
	}
	defer c2()
	waitSubs(t, r, "chrome")
	r.app.EmitChrome(r.main, caps.ChromeThumbClick, "play")
	r.app.EmitChrome(r.main, caps.ChromeNewTab, "")
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(got)
		mu.Unlock()
		if n >= 2 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(got) != 2 || got[0].Window != "main" || got[0].Kind != chrome.EventThumbClick || got[0].ID != "play" || got[1].Kind != chrome.EventNewTab {
		t.Fatalf("events %+v", got)
	}
	if len(clicks) != 1 || clicks[0] != "play" {
		t.Fatalf("thumb clicks %v", clicks)
	}
}

func TestChromeSupports(t *testing.T) {
	newRig(t)
	for _, c := range []string{
		"chrome", "chrome:titleBar", "chrome:backdrop", "chrome:vibrancy", "chrome:titleBarOverlay",
		"chrome:trafficLights", "chrome:drag", "chrome:document", "chrome:sheets", "chrome:tabs",
		"chrome:thumbButtons", "chrome:jumpList", "chrome:share", "chrome:dock",
	} {
		if !app.Supports(c) {
			t.Errorf("Supports(%q) is false on the fake platform", c)
		}
	}
	if app.Supports("chrome:levitation") {
		t.Error("an unknown feature is supported")
	}
}

func TestChromeWindowOpenOptions(t *testing.T) {
	r := newRig(t)
	w, err := window.Open(window.Options{
		Title: "bar", Frameless: true,
		Chrome: caps.ChromeOptions{TitleBar: "hidden", Transparent: true, Vibrancy: "hudWindow", Overlay: &caps.TitleBarOverlay{Color: "#222", Height: 36},
			TrafficLight: &caps.Point{X: 10, Y: 12}, TabbingID: "grp", TabbingMode: "preferred"},
	})
	if err != nil {
		t.Fatal(err)
	}
	var opened *fakeplat.Window
	for _, fw := range r.app.Windows {
		if fw.Opts.Title == "bar" {
			opened = fw
		}
	}
	if opened == nil {
		t.Fatal("window not opened")
	}
	o := opened.Opts.Chrome
	if o.TitleBar != "hidden" || !o.Transparent || o.Vibrancy != "hudWindow" || o.Overlay == nil || o.Overlay.Height != 36 ||
		o.TrafficLight == nil || o.TrafficLight.Y != 12 || o.TabbingID != "grp" || o.TabbingMode != "preferred" {
		t.Fatalf("chrome options reached the platform as %+v", o)
	}
	_ = w
	// The older Frameless spelling means titleBar none.
	if _, err := window.Open(window.Options{Title: "bare", Frameless: true}); err != nil {
		t.Fatal(err)
	}
	for _, fw := range r.app.Windows {
		if fw.Opts.Title == "bare" && fw.Opts.Chrome.TitleBar != "none" {
			t.Fatalf("Frameless gave titleBar %q", fw.Opts.Chrome.TitleBar)
		}
	}
	// A bad option fails the open and creates no window.
	n := len(r.app.Windows)
	if _, err := window.Open(window.Options{Title: "bad", Chrome: caps.ChromeOptions{Backdrop: "glass"}}); err == nil {
		t.Fatal("a bad backdrop opened a window")
	}
	if len(r.app.Windows) != n {
		t.Fatal("a window was created for bad options")
	}
}

func TestChromePermissions(t *testing.T) {
	r := newRig(t)
	// A page with "window" can read the state but cannot share or touch the
	// dock; with "share" it can share.
	h := r.ui("window")
	if got := uiCall(t, h, "krate/chrome.state", func(e *wire.Encoder) { e.String("main") }); strings.Contains(got, "permission") || strings.Contains(got, "not_found") {
		t.Fatalf("state denied: %s", got)
	}
	if got := uiCall(t, h, "krate/chrome.share", func(e *wire.Encoder) {
		e.String("main")
		e.BeginObject()
		e.Key("text")
		e.String("x")
		e.EndObject()
	}); !strings.Contains(got, "not_found") && !strings.Contains(got, "permission") {
		t.Fatalf("share without the permission: %s", got)
	}
	if got := uiCall(t, h, "krate/chrome.setDockVisible", func(e *wire.Encoder) { e.Bool(false) }); !strings.Contains(got, "not_found") && !strings.Contains(got, "permission") {
		t.Fatalf("dock without the app permission: %s", got)
	}
	if len(r.app.Shared) != 0 || r.app.Policy != "" {
		t.Fatal("a denied call reached the platform")
	}
	h = r.ui("window", "share", "app")
	if got := uiCall(t, h, "krate/chrome.share", func(e *wire.Encoder) {
		e.String("main")
		e.BeginObject()
		e.Key("text")
		e.String("x")
		e.EndObject()
	}); strings.Contains(got, "error") {
		t.Fatalf("share with the permission: %s", got)
	}
	if len(r.app.Shared) != 1 {
		t.Fatal("share did not reach the platform")
	}
}

// plainApp and plainWindow expose only the core interfaces, as a platform
// without any chrome capability does.
type plainApp struct{ platform.App }
type plainWindow struct{ platform.Window }

func TestChromeUnsupportedWithoutPlatformSupport(t *testing.T) {
	fa := fakeplat.New()
	h := sdk.NewHost()
	h.Attach(plainApp{fa})
	w, err := fa.NewWindow(platform.WindowOptions{Title: "main"})
	if err != nil {
		t.Fatal(err)
	}
	h.AddWindow("main", plainWindow{w})
	t.Cleanup(h.Close)
	m := chrome.Main()
	for name, err := range map[string]error{
		"state":    func() error { _, e := m.State(); return e }(),
		"titlebar": m.SetTitleBar("none"),
		"backdrop": m.SetBackdrop("mica"),
		"vibrancy": m.SetVibrancy("sidebar"),
		"drag":     m.StartDrag(),
		"document": m.SetDocumentEdited(true),
		"sheet":    m.EndSheet(),
		"tabs":     m.MergeAllWindows(),
		"thumbs":   m.SetThumbButtons(nil),
		"share":    m.Share(chrome.ShareItems{Text: "x"}),
		"jump":     chrome.SetJumpList(nil),
		"dock":     chrome.SetDockVisible(false),
	} {
		if !errors.Is(err, krate.ErrUnsupported) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if app.Supports("chrome:backdrop") || app.Supports("chrome:dock") {
		t.Error("a platform without chrome reports support")
	}
}
