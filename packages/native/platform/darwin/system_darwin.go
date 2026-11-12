//go:build darwin

package darwin

// OS integration: dock badge and progress, displays, power, theme, login item
// and web content process crashes. Compile-only until CI runs the GUI tests in
// system_gui_test.go.
//
//   - Badge: NSDockTile badgeLabel; progress is an NSProgressIndicator in a
//     view that replaces the dock tile's content (app icon plus a bar).
//   - Displays: NSScreen in top-left coordinates of the primary display;
//     changes from NSApplicationDidChangeScreenParametersNotification.
//   - Power: NSWorkspace sleep and wake notifications, distributed screen lock
//     notifications, IOPSCopyPowerSourcesInfo, CGEventSourceSecondsSinceLastEventType
//     and IOPMAssertionCreateWithName.
//   - Theme: AppleInterfaceStyle for the system appearance, NSApp.appearance
//     to override it, and the AppleInterfaceThemeChanged distributed
//     notification for changes.
//   - Login item: SMAppService.mainApp (macOS 13+).
//   - Crashes: webViewWebContentProcessDidTerminate: on the navigation delegate.

import (
	"errors"
	"fmt"
	"sync"
	"time"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
	"github.com/kratejs/krate/packages/native/platform/caps"
)

var (
	_ caps.Badger       = (*App)(nil)
	_ caps.Attention    = (*App)(nil)
	_ caps.Displays     = (*App)(nil)
	_ caps.Power        = (*App)(nil)
	_ caps.Themer       = (*App)(nil)
	_ caps.WindowThemer = (*App)(nil)
	_ caps.LoginItems   = (*App)(nil)
	_ caps.ViewCrashes  = (*App)(nil)
)

var sysState struct {
	mu        sync.Mutex
	attention int
	displays  func()
	power     func(caps.PowerEvent)
	theme     func(caps.ThemeInfo)
	crash     func(caps.ViewCrash)
	locked    bool
	observing struct{ screens, power, theme bool }
}

// ---- dock badge and progress ----

// SetBadge shows text on the dock icon; "" removes it.
func (a *App) SetBadge(text string) error {
	runOnMain(func() {
		withPool(func() {
			tile := a.nsApp.Send(sel("dockTile"))
			if text == "" {
				tile.Send(sel("setBadgeLabel:"), objc.ID(0))
			} else {
				tile.Send(sel("setBadgeLabel:"), nsString(text))
			}
		})
	})
	return nil
}

var dockBar struct {
	view, bar objc.ID
}

// SetProgress draws a progress bar on the dock icon. Error and paused look
// like normal progress: the dock has no such states.
func (a *App) SetProgress(p caps.Progress) error {
	runOnMain(func() {
		withPool(func() {
			tile := a.nsApp.Send(sel("dockTile"))
			if p.State == caps.ProgressNone {
				tile.Send(sel("setContentView:"), objc.ID(0))
				tile.Send(sel("display"))
				return
			}
			if dockBar.view == 0 {
				frame := nsRect{Origin: nsPoint{X: 0, Y: 0}, Size: nsSize{Width: 128, Height: 128}}
				v := cls("NSView").Send(sel("alloc")).Send(sel("initWithFrame:"), frame)
				icon := cls("NSImageView").Send(sel("alloc")).Send(sel("initWithFrame:"), frame)
				icon.Send(sel("setImage:"), a.nsApp.Send(sel("applicationIconImage")))
				barFrame := nsRect{Origin: nsPoint{X: 6, Y: 4}, Size: nsSize{Width: 116, Height: 14}}
				bar := cls("NSProgressIndicator").Send(sel("alloc")).Send(sel("initWithFrame:"), barFrame)
				bar.Send(sel("setStyle:"), 0) // NSProgressIndicatorStyleBar
				bar.Send(sel("setMinValue:"), 0.0)
				bar.Send(sel("setMaxValue:"), 1.0)
				v.Send(sel("addSubview:"), icon)
				v.Send(sel("addSubview:"), bar)
				dockBar.view, dockBar.bar = v, bar
			}
			bar := dockBar.bar
			if p.State == caps.ProgressIndeterminate {
				bar.Send(sel("setIndeterminate:"), true)
				bar.Send(sel("startAnimation:"), objc.ID(0))
			} else {
				bar.Send(sel("stopAnimation:"), objc.ID(0))
				bar.Send(sel("setIndeterminate:"), false)
				bar.Send(sel("setDoubleValue:"), p.Value)
			}
			tile.Send(sel("setContentView:"), dockBar.view)
			tile.Send(sel("display"))
		})
	})
	return nil
}

// RequestAttention bounces the dock icon until the app is activated.
func (a *App) RequestAttention(on bool) error {
	runOnMain(func() {
		withPool(func() {
			sysState.mu.Lock()
			id := sysState.attention
			sysState.mu.Unlock()
			switch {
			case on && id == 0:
				n := objc.Send[int](a.nsApp, sel("requestUserAttention:"), 0) // NSCriticalRequest
				sysState.mu.Lock()
				sysState.attention = n
				sysState.mu.Unlock()
			case !on && id != 0:
				a.nsApp.Send(sel("cancelUserAttentionRequest:"), id)
				sysState.mu.Lock()
				sysState.attention = 0
				sysState.mu.Unlock()
			}
		})
	})
	return nil
}

// ---- displays ----

// primaryHeight is the height of the primary display (the first screen).
func primaryHeight() float64 {
	screens := cls("NSScreen").Send(sel("screens"))
	if objc.Send[uint](screens, sel("count")) == 0 {
		return 0
	}
	return objc.Send[nsRect](screens.Send(sel("objectAtIndex:"), uint(0)), sel("frame")).Size.Height
}

// Displays lists the screens, primary first.
func (a *App) Displays() (out []caps.Display, err error) {
	runOnMain(func() {
		withPool(func() {
			screens := cls("NSScreen").Send(sel("screens"))
			n := objc.Send[uint](screens, sel("count"))
			if n == 0 {
				return
			}
			ph := objc.Send[nsRect](screens.Send(sel("objectAtIndex:"), uint(0)), sel("frame")).Size.Height
			for i := uint(0); i < n; i++ {
				s := screens.Send(sel("objectAtIndex:"), i)
				f := objc.Send[nsRect](s, sel("frame"))
				v := objc.Send[nsRect](s, sel("visibleFrame"))
				d := caps.Display{
					X: int(f.Origin.X), Y: caps.TopLeftY(ph, f.Origin.Y, f.Size.Height),
					Width: int(f.Size.Width), Height: int(f.Size.Height),
					WorkX: int(v.Origin.X), WorkY: caps.TopLeftY(ph, v.Origin.Y, v.Size.Height),
					WorkWidth: int(v.Size.Width), WorkHeight: int(v.Size.Height),
					Scale:   objc.Send[float64](s, sel("backingScaleFactor")),
					Primary: i == 0,
				}
				d.ID = fmt.Sprintf("m%d", i)
				if desc := s.Send(sel("deviceDescription")); desc != 0 {
					if num := desc.Send(sel("objectForKey:"), nsString("NSScreenNumber")); num != 0 {
						d.ID = fmt.Sprintf("d%d", objc.Send[uint32](num, sel("unsignedIntValue")))
					}
				}
				if respondsTo(s, sel("localizedName")) {
					d.Name = goString(s.Send(sel("localizedName")))
				}
				if d.Name == "" {
					d.Name = fmt.Sprintf("Display %d", i+1)
				}
				if respondsTo(s, sel("maximumFramesPerSecond")) {
					d.RefreshHz = float64(objc.Send[int](s, sel("maximumFramesPerSecond")))
				}
				out = append(out, d)
			}
		})
	})
	if len(out) == 0 {
		return nil, errors.New("darwin: NSScreen reports no screens")
	}
	return out, nil
}

// OnDisplays sets the handler for screen configuration changes.
func (a *App) OnDisplays(fn func()) {
	sysState.mu.Lock()
	sysState.displays = fn
	first := !sysState.observing.screens
	sysState.observing.screens = true
	sysState.mu.Unlock()
	if !first {
		return
	}
	runOnMain(func() {
		withPool(func() {
			observe(cls("NSNotificationCenter").Send(sel("defaultCenter")), "NSApplicationDidChangeScreenParametersNotification", func() {
				sysState.mu.Lock()
				f := sysState.displays
				sysState.mu.Unlock()
				if f != nil {
					f()
				}
			})
		})
	})
}

// observe registers fn for a notification name on a notification center. The
// block lives as long as the process.
func observe(center objc.ID, name string, fn func()) {
	block := objc.NewBlock(func(_ objc.Block, _ objc.ID) { fn() })
	center.Send(sel("addObserverForName:object:queue:usingBlock:"), nsString(name), objc.ID(0), objc.ID(0), objc.ID(block))
}

// ---- power ----

var powerLibs struct {
	once sync.Once
	err  error

	copyInfo   func() uintptr
	srcType    func(blob uintptr) uintptr
	cfRelease  func(uintptr)
	idleSecs   func(state int32, typ uint32) float64
	createAsrt func(typ uintptr, level uint32, name uintptr, id *uint32) int32
	release    func(id uint32) int32
}

func loadPowerLibs() error {
	powerLibs.once.Do(func() {
		open := func(path string) uintptr {
			h, err := purego.Dlopen(path, purego.RTLD_GLOBAL|purego.RTLD_NOW)
			if err != nil && powerLibs.err == nil {
				powerLibs.err = err
			}
			return h
		}
		iokit := open("/System/Library/Frameworks/IOKit.framework/IOKit")
		cf := open("/System/Library/Frameworks/CoreFoundation.framework/CoreFoundation")
		cg := open("/System/Library/Frameworks/CoreGraphics.framework/CoreGraphics")
		if powerLibs.err != nil {
			return
		}
		defer func() {
			if r := recover(); r != nil {
				powerLibs.err = fmt.Errorf("darwin: binding power symbols: %v", r)
			}
		}()
		purego.RegisterLibFunc(&powerLibs.copyInfo, iokit, "IOPSCopyPowerSourcesInfo")
		purego.RegisterLibFunc(&powerLibs.srcType, iokit, "IOPSGetProvidingPowerSourceType")
		purego.RegisterLibFunc(&powerLibs.createAsrt, iokit, "IOPMAssertionCreateWithName")
		purego.RegisterLibFunc(&powerLibs.release, iokit, "IOPMAssertionRelease")
		purego.RegisterLibFunc(&powerLibs.cfRelease, cf, "CFRelease")
		purego.RegisterLibFunc(&powerLibs.idleSecs, cg, "CGEventSourceSecondsSinceLastEventType")
	})
	return powerLibs.err
}

// PowerSample reads the power source and the idle time.
func (a *App) PowerSample() (caps.PowerSample, error) {
	if err := loadPowerLibs(); err != nil {
		return caps.PowerSample{}, err
	}
	s := caps.PowerSample{IdleSeconds: -1}
	withPool(func() {
		if blob := powerLibs.copyInfo(); blob != 0 {
			s.OnBattery = caps.OnBatteryFromSourceType(goString(objc.ID(powerLibs.srcType(blob))))
			powerLibs.cfRelease(blob)
		}
		// kCGEventSourceStateCombinedSessionState = 0, kCGAnyInputEventType = ~0.
		s.IdleSeconds = powerLibs.idleSecs(0, ^uint32(0))
	})
	sysState.mu.Lock()
	s.Locked = sysState.locked
	sysState.mu.Unlock()
	return s, nil
}

// OnPower sets the handler for suspend, resume, lock and unlock.
func (a *App) OnPower(fn func(caps.PowerEvent)) {
	sysState.mu.Lock()
	sysState.power = fn
	first := !sysState.observing.power
	sysState.observing.power = true
	sysState.mu.Unlock()
	if !first {
		return
	}
	emit := func(k caps.PowerEventKind) func() {
		return func() {
			sysState.mu.Lock()
			switch k {
			case caps.PowerLock:
				sysState.locked = true
			case caps.PowerUnlock:
				sysState.locked = false
			}
			f := sysState.power
			sysState.mu.Unlock()
			if f != nil {
				f(caps.PowerEvent{Kind: k})
			}
		}
	}
	runOnMain(func() {
		withPool(func() {
			ws := cls("NSWorkspace").Send(sel("sharedWorkspace")).Send(sel("notificationCenter"))
			observe(ws, "NSWorkspaceWillSleepNotification", emit(caps.PowerSuspend))
			observe(ws, "NSWorkspaceDidWakeNotification", emit(caps.PowerResume))
			dist := cls("NSDistributedNotificationCenter").Send(sel("defaultCenter"))
			observe(dist, "com.apple.screenIsLocked", emit(caps.PowerLock))
			observe(dist, "com.apple.screenIsUnlocked", emit(caps.PowerUnlock))
		})
	})
}

// PreventSleep creates IOPMAssertions: one against idle system sleep, and with
// display one against display sleep.
func (a *App) PreventSleep(reason string, display bool) (func(), error) {
	if err := loadPowerLibs(); err != nil {
		return nil, err
	}
	types := []string{"PreventUserIdleSystemSleep"}
	if display {
		types = append(types, "PreventUserIdleDisplaySleep")
	}
	var ids []uint32
	var err error
	withPool(func() {
		for _, t := range types {
			var id uint32
			// kIOPMAssertionLevelOn = 255.
			if rc := powerLibs.createAsrt(uintptr(nsString(t)), 255, uintptr(nsString(reason)), &id); rc != 0 {
				err = fmt.Errorf("darwin: IOPMAssertionCreateWithName(%s) failed with 0x%x", t, uint32(rc))
				return
			}
			ids = append(ids, id)
		}
	})
	if err != nil {
		for _, id := range ids {
			powerLibs.release(id)
		}
		return nil, err
	}
	return func() {
		for _, id := range ids {
			powerLibs.release(id)
		}
	}, nil
}

// ---- theme ----

// Theme reads the system appearance (not the override of SetWindowTheme).
func (a *App) Theme() (info caps.ThemeInfo, err error) {
	runOnMain(func() {
		withPool(func() {
			defaults := cls("NSUserDefaults").Send(sel("standardUserDefaults"))
			style := goString(defaults.Send(sel("stringForKey:"), nsString("AppleInterfaceStyle")))
			info.Dark = style == "Dark"
			ws := cls("NSWorkspace").Send(sel("sharedWorkspace"))
			if respondsTo(ws, sel("accessibilityDisplayShouldIncreaseContrast")) {
				info.HighContrast = objc.Send[bool](ws, sel("accessibilityDisplayShouldIncreaseContrast"))
			}
		})
	})
	return info, nil
}

// OnTheme sets the handler for system appearance changes.
func (a *App) OnTheme(fn func(caps.ThemeInfo)) {
	sysState.mu.Lock()
	sysState.theme = fn
	first := !sysState.observing.theme
	sysState.observing.theme = true
	sysState.mu.Unlock()
	if !first {
		return
	}
	changed := func() {
		// The defaults database catches up just after the notification.
		time.AfterFunc(150*time.Millisecond, func() {
			sysState.mu.Lock()
			f := sysState.theme
			sysState.mu.Unlock()
			if f == nil {
				return
			}
			if info, err := a.Theme(); err == nil {
				f(info)
			}
		})
	}
	runOnMain(func() {
		withPool(func() {
			observe(cls("NSDistributedNotificationCenter").Send(sel("defaultCenter")), "AppleInterfaceThemeChangedNotification", changed)
			observe(cls("NSWorkspace").Send(sel("sharedWorkspace")).Send(sel("notificationCenter")), "NSWorkspaceAccessibilityDisplayOptionsDidChangeNotification", changed)
		})
	})
}

// SetWindowTheme sets NSApp.appearance: Aqua, DarkAqua, or nil to follow the
// system.
func (a *App) SetWindowTheme(mode string) error {
	runOnMain(func() {
		withPool(func() {
			var ap objc.ID
			switch mode {
			case "dark":
				ap = cls("NSAppearance").Send(sel("appearanceNamed:"), nsString("NSAppearanceNameDarkAqua"))
			case "light":
				ap = cls("NSAppearance").Send(sel("appearanceNamed:"), nsString("NSAppearanceNameAqua"))
			}
			a.nsApp.Send(sel("setAppearance:"), ap)
		})
	})
	return nil
}

// ---- login item ----

var smOnce sync.Once

func smService() (objc.ID, error) {
	smOnce.Do(func() {
		_, _ = purego.Dlopen("/System/Library/Frameworks/ServiceManagement.framework/ServiceManagement", purego.RTLD_GLOBAL|purego.RTLD_NOW)
	})
	c := cls("SMAppService")
	if c == 0 {
		return 0, errors.New("darwin: SMAppService needs macOS 13 or later")
	}
	return c.Send(sel("mainAppService")), nil
}

// LoginItem reports whether SMAppService.mainApp is registered.
func (a *App) LoginItem() (enabled bool, method string, err error) {
	runOnMain(func() {
		withPool(func() {
			svc, e := smService()
			if e != nil {
				err = e
				return
			}
			// SMAppServiceStatusEnabled = 1.
			enabled = objc.Send[int](svc, sel("status")) == 1
		})
	})
	if enabled {
		method = "smappservice"
	}
	return
}

// SetLoginItem registers or unregisters SMAppService.mainApp. A process that
// is not a signed app bundle gets an error, and the SDK falls back to a
// LaunchAgent.
func (a *App) SetLoginItem(enabled bool) (err error) {
	runOnMain(func() {
		withPool(func() {
			svc, e := smService()
			if e != nil {
				err = e
				return
			}
			var nsErr objc.ID
			name := "unregisterAndReturnError:"
			if enabled {
				name = "registerAndReturnError:"
			}
			ok := objc.Send[bool](svc, sel(name), unsafe.Pointer(&nsErr))
			if !ok {
				err = fmt.Errorf("darwin: SMAppService: %s", errorText(nsErr))
			}
		})
	})
	return
}

// ---- web content process crashes ----

// OnViewCrash sets the handler for web content process terminations.
func (a *App) OnViewCrash(fn func(caps.ViewCrash)) {
	sysState.mu.Lock()
	sysState.crash = fn
	sysState.mu.Unlock()
}

// webContentTerminated runs on the main thread from the navigation delegate.
func (a *App) webContentTerminated(web objc.ID) {
	v := a.byWeb[web]
	if v == nil {
		return
	}
	sysState.mu.Lock()
	f := sysState.crash
	sysState.mu.Unlock()
	if f != nil {
		// WKWebView does not say why the process ended.
		f(caps.ViewCrash{View: v.ref, Window: v.w.Ref(), Reason: "crashed"})
	}
}

// ReloadView loads the view's current route again, which starts a new web
// content process.
func (a *App) ReloadView(ref caps.ViewRef) (err error) {
	runOnMain(func() {
		withPool(func() {
			for _, v := range a.byWeb {
				if v.ref == ref {
					v.load(v.currentRoute())
					return
				}
			}
			err = fmt.Errorf("darwin: no view %d", ref)
		})
	})
	return
}
