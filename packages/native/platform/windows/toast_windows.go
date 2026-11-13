//go:build windows

package windows

// Toast notifications through WinRT (Windows.UI.Notifications) over raw COM,
// with the same hand-written vtable approach as the WebView2 code. The pure
// logic is in toast.go. The Windows CI tests call the real WinRT
// (TestToastXMLAcceptedByWinRT, TestToastDelegateInterfaces) and show a toast
// (TestGUIToast); the manual list covers what CI cannot see.
//
// A click on a toast uses protocol activation, so it works after the app has
// quit: the toast carries a URL of a per-app scheme (package toastscheme), the
// app registers that scheme under HKCU (a packaged app declares it in its
// manifest) and Windows runs the executable with the URL. The new process
// either hands the click to the running instance (WM_COPYDATA, see
// activate_windows.go) or keeps it and delivers it as a notification event
// when the page can receive it. This avoids a COM activator
// (INotificationActivationCallback), which needs a registered class factory
// and a Start Menu shortcut with the activator CLSID.

import (
	"errors"
	"fmt"
	"os"
	"runtime"
	"strings"
	"sync/atomic"
	"unicode/utf16"
	"unsafe"

	xw "golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"

	"github.com/kratejs/krate/packages/native/krate"
	"github.com/kratejs/krate/packages/native/platform/caps"
)

var (
	combase = xw.NewLazySystemDLL("combase.dll")

	pRoInitialize           = combase.NewProc("RoInitialize")
	pRoGetActivationFactory = combase.NewProc("RoGetActivationFactory")
	pWindowsCreateString    = combase.NewProc("WindowsCreateString")
	pWindowsDeleteString    = combase.NewProc("WindowsDeleteString")
	pWindowsGetStringRawBuf = combase.NewProc("WindowsGetStringRawBuffer")
	pGetCurrentPackageName  = kernel32.NewProc("GetCurrentPackageFullName")
	pSetCurrentProcessAUMID = shell32.NewProc("SetCurrentProcessExplicitAppUserModelID")

	_ caps.Notifier           = (*App)(nil)
	_ caps.NotificationCloser = (*App)(nil)

	notifyFn func(caps.NotifyEvent)
)

const appModelErrorNoPackage = 15700

// isPackaged reports whether the process runs with package identity (MSIX).
func isPackaged() bool {
	if pGetCurrentPackageName.Find() != nil {
		return false
	}
	var n uint32
	r, _, _ := pGetCurrentPackageName.Call(uintptr(unsafe.Pointer(&n)), 0)
	return r != appModelErrorNoPackage
}

// setProcessAUMID gives the process its AppUserModelID so the taskbar and
// toasts agree on the app's identity. Packaged apps already have one.
func setProcessAUMID(appID string) {
	if isPackaged() || pSetCurrentProcessAUMID.Find() != nil {
		return
	}
	_, _, _ = pSetCurrentProcessAUMID.Call(uintptr(unsafe.Pointer(wstr(aumidFor(appID)))))
}

// newHString creates an HSTRING; the caller frees it with deleteHString.
func newHString(s string) (uintptr, error) {
	u := utf16.Encode([]rune(strings.ReplaceAll(s, "\x00", "")))
	if len(u) == 0 {
		return 0, nil // the empty HSTRING is null
	}
	var h uintptr
	hr, _, _ := pWindowsCreateString.Call(uintptr(unsafe.Pointer(&u[0])), uintptr(len(u)), uintptr(unsafe.Pointer(&h)))
	runtime.KeepAlive(u)
	if failed(hr) {
		return 0, fmt.Errorf("windows: WindowsCreateString: 0x%08x", uint32(hr))
	}
	return h, nil
}

func deleteHString(h uintptr) {
	if h != 0 {
		_, _, _ = pWindowsDeleteString.Call(h)
	}
}

// hstringValue copies an HSTRING into a Go string (the HSTRING stays owned by
// the caller).
func hstringValue(h uintptr) string {
	if h == 0 {
		return ""
	}
	var n uint32
	p, _, _ := pWindowsGetStringRawBuf.Call(h, uintptr(unsafe.Pointer(&n)))
	if p == 0 || n == 0 {
		return ""
	}
	return string(utf16.Decode(unsafe.Slice((*uint16)(ptr(p)), n)))
}

func activationFactory(class, iid string) (unsafe.Pointer, error) {
	hs, err := newHString(class)
	if err != nil {
		return nil, err
	}
	defer deleteHString(hs)
	g := mustGUID(iid)
	var f uintptr
	hr, _, _ := pRoGetActivationFactory.Call(hs, uintptr(unsafe.Pointer(&g)), uintptr(unsafe.Pointer(&f)))
	if failed(hr) || f == 0 {
		return nil, fmt.Errorf("windows: RoGetActivationFactory(%s): 0x%08x", class, uint32(hr))
	}
	return ptr(f), nil
}

func comQuery(obj unsafe.Pointer, iid string) (unsafe.Pointer, error) {
	g := mustGUID(iid)
	var out uintptr
	hr := vcall(obj, iunknownSlotQI, uintptr(unsafe.Pointer(&g)), uintptr(unsafe.Pointer(&out)))
	if failed(hr) || out == 0 {
		return nil, fmt.Errorf("windows: QueryInterface %s: 0x%08x", iid, uint32(hr))
	}
	return ptr(out), nil
}

// newDelegate creates a WinRT delegate object (a TypedEventHandler) for the
// given interface id. It answers IAgileObject so WinRT calls it directly from
// whatever thread raises the event instead of marshalling to the creating
// apartment; fn must be safe to call from any thread.
func newDelegate(iid string, fn func(sender, args uintptr)) unsafe.Pointer {
	o := &comObj{
		vtbl: vtHandler,
		ids:  []guid{mustGUID(iidIUnknown), mustGUID(iidAgileObject), mustGUID(iid)},
		invoke: func(a, b uintptr) uintptr {
			fn(a, b)
			return sOK
		},
	}
	return register(o)
}

type toastState struct {
	ready    bool
	initErr  error
	mode     toastMode
	scheme   string // protocol activation scheme; "" for foreground activation
	aumid    string
	notifier unsafe.Pointer            // IToastNotifier
	factory  unsafe.Pointer            // IToastNotificationFactory
	live     map[string]unsafe.Pointer // id to IToastNotification
}

// OnNotify sets the handler for notification events.
func (a *App) OnNotify(fn func(caps.NotifyEvent)) {
	extMu.Lock()
	notifyFn = fn
	launch := a.launchEvent
	if fn != nil {
		a.launchEvent = nil
	}
	extMu.Unlock()
	// The click that started the app goes out as soon as someone listens.
	if launch != nil && fn != nil {
		fn(*launch)
	}
}

func emitNotify(ev caps.NotifyEvent) {
	extMu.Lock()
	fn := notifyFn
	extMu.Unlock()
	if fn != nil {
		fn(ev)
	}
}

// Notify shows a toast.
func (a *App) Notify(n caps.Notification) (err error) {
	a.callMain(func() { err = a.toastShow(n) })
	return err
}

// CloseNotification removes a toast the app showed (from the screen and the
// Action Center).
func (a *App) CloseNotification(id string) (err error) {
	a.callMain(func() {
		t := a.toast
		toast := t.live[id]
		if toast == nil || t.notifier == nil {
			return
		}
		if hr := vcall(t.notifier, notifierHide, uintptr(toast)); failed(hr) {
			err = fmt.Errorf("windows: hiding the notification: 0x%08x", uint32(hr))
		}
		delete(t.live, id)
		comRelease(toast)
	})
	return err
}

// toastInit does the one-time setup on the main thread.
func (a *App) toastInit() error {
	t := a.toast
	if t.ready {
		return nil
	}
	if t.initErr != nil {
		return t.initErr
	}
	fail := func(err error) error {
		t.initErr = err
		return err
	}
	if pRoGetActivationFactory.Find() != nil || pWindowsCreateString.Find() != nil {
		return fail(fmt.Errorf("%w: toast notifications need Windows 8 or later", krate.ErrUnsupported))
	}
	// RoInitialize(RO_INIT_SINGLETHREADED) on the STA main thread returns
	// S_FALSE; a thread already in another mode is fine too.
	if hr, _, _ := pRoInitialize.Call(0); failed(hr) && uint32(hr) != rpcEChangedMode {
		return fail(fmt.Errorf("windows: RoInitialize: 0x%08x", uint32(hr)))
	}
	t.mode = toastModeFor(isPackaged())
	t.aumid = aumidFor(a.cfg.AppID)
	t.scheme = a.toastScheme
	if t.mode == toastRegistered {
		if err := registerAUMID(t.aumid, a.displayName(), a.cfg.IconPath); err != nil {
			return fail(err)
		}
		// Clicks launch the executable through this scheme, so they work after
		// the app has quit. If the registration fails the toasts fall back to
		// foreground activation (clicks reach only a running app).
		if err := registerToastProtocol(t.scheme, a.displayName()); err != nil {
			logf("%v; clicking a notification after the app has quit will do nothing", err)
			t.scheme = ""
		}
	}
	mgr, err := activationFactory(classToastManager, iidToastManagerStatics)
	if err != nil {
		return fail(err)
	}
	defer comRelease(mgr)
	var notifier uintptr
	if t.mode == toastPackaged {
		hr := vcall(mgr, toastMgrCreateNotifier, uintptr(unsafe.Pointer(&notifier)))
		if failed(hr) || notifier == 0 {
			return fail(fmt.Errorf("windows: CreateToastNotifier: 0x%08x", uint32(hr)))
		}
	} else {
		hs, err := newHString(t.aumid)
		if err != nil {
			return fail(err)
		}
		hr := vcall(mgr, toastMgrCreateNotifierID, hs, uintptr(unsafe.Pointer(&notifier)))
		deleteHString(hs)
		if failed(hr) || notifier == 0 {
			return fail(fmt.Errorf("windows: CreateToastNotifierWithId: 0x%08x", uint32(hr)))
		}
	}
	t.notifier = ptr(notifier)
	factory, err := activationFactory(classToast, iidToastFactory)
	if err != nil {
		comRelease(t.notifier)
		t.notifier = nil
		return fail(err)
	}
	t.factory = factory
	t.live = map[string]unsafe.Pointer{}
	t.ready = true
	return nil
}

func (a *App) displayName() string {
	if a.cfg.AppName != "" {
		return a.cfg.AppName
	}
	return a.cfg.AppID
}

// registerAUMID writes the HKCU registration that lets an unpackaged app post
// toasts under its own name and icon.
func registerAUMID(aumid, name, icon string) error {
	k, _, err := registry.CreateKey(registry.CURRENT_USER, aumidRegistryPath(aumid), registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("windows: registering the app for notifications: %w", err)
	}
	defer k.Close()
	if icon != "" {
		if _, err := os.Stat(icon); err != nil {
			icon = ""
		}
	}
	for n, v := range aumidValues(name, icon) {
		if err := k.SetStringValue(n, v); err != nil {
			return fmt.Errorf("windows: registering the app for notifications: %w", err)
		}
	}
	return nil
}

// toastDocument parses xml into an IXmlDocument (refcount 1).
func toastDocument(xml string) (unsafe.Pointer, error) {
	af, err := activationFactory(classXMLDocument, iidActivationFactory)
	if err != nil {
		return nil, err
	}
	defer comRelease(af)
	var inst uintptr
	if hr := vcall(af, activationFactoryActivate, uintptr(unsafe.Pointer(&inst))); failed(hr) || inst == 0 {
		return nil, fmt.Errorf("windows: creating the XML document: 0x%08x", uint32(hr))
	}
	obj := ptr(inst)
	defer comRelease(obj)
	io, err := comQuery(obj, iidXMLDocumentIO)
	if err != nil {
		return nil, err
	}
	defer comRelease(io)
	hs, err := newHString(xml)
	if err != nil {
		return nil, err
	}
	hr := vcall(io, xmlIOLoadXML, hs)
	deleteHString(hs)
	if failed(hr) {
		return nil, fmt.Errorf("windows: the toast XML was rejected: 0x%08x", uint32(hr))
	}
	return comQuery(obj, iidXMLDocument)
}

func (a *App) toastShow(n caps.Notification) error {
	if err := a.toastInit(); err != nil {
		return err
	}
	t := a.toast

	var setting int32
	if hr := vcall(t.notifier, notifierGetSetting, uintptr(unsafe.Pointer(&setting))); !failed(hr) {
		if msg := settingProblem(setting); msg != "" {
			return errors.New("windows: " + msg)
		}
	}
	doc, err := toastDocument(buildToastXMLFor(n, fileURI(n.Icon), t.scheme))
	if err != nil {
		return err
	}
	defer comRelease(doc)
	var toastp uintptr
	if hr := vcall(t.factory, toastFactoryCreate, uintptr(doc), uintptr(unsafe.Pointer(&toastp))); failed(hr) || toastp == 0 {
		return fmt.Errorf("windows: CreateToastNotification: 0x%08x", uint32(hr))
	}
	toast := ptr(toastp)
	defer comRelease(toast)

	a.attachToastEvents(toast, n.ID)

	if old := t.live[n.ID]; old != nil && n.ID != "" {
		vcall(t.notifier, notifierHide, uintptr(old))
		delete(t.live, n.ID)
		comRelease(old)
	}
	if hr := vcall(t.notifier, notifierShow, uintptr(toast)); failed(hr) {
		return fmt.Errorf("windows: showing the notification: 0x%08x", uint32(hr))
	}
	if n.ID != "" {
		comAddRef(toast)
		t.live[n.ID] = toast
	}
	return nil
}

// toastFailures counts Failed events and lastDismissReason is the reason of
// the latest Dismissed event; the GUI test tells a failed toast from one the
// OS dismissed (no foreground UI, Focus Assist) with them.
var (
	toastFailures     atomic.Int32
	lastDismissReason atomic.Int32
)

// attachToastEvents connects Activated, Dismissed and Failed. The toast keeps
// its own reference to each handler, so ours are released right away. If an
// event cannot be connected the toast is still shown; its events are lost and
// a note is logged.
func (a *App) attachToastEvents(toast unsafe.Pointer, id string) {
	add := func(slot int, iid string, fn func(sender, args uintptr), what string) {
		h := newDelegate(iid, fn)
		var token int64
		if hr := vcall(toast, slot, uintptr(h), uintptr(unsafe.Pointer(&token))); failed(hr) {
			logf("toast %s handler was rejected (0x%08x); %s events will not be reported", what, uint32(hr), what)
		}
		comRelease(h)
	}
	forget := func() {
		a.Dispatch(func() {
			if id == "" || a.toast.live == nil {
				return
			}
			// Keep the entry if a newer toast replaced this one.
			if cur := a.toast.live[id]; cur == toast {
				delete(a.toast.live, id)
				comRelease(cur)
			}
		})
	}
	add(toastAddActivated, iidToastActivatedHandler, func(sender, args uintptr) {
		ev := caps.NotifyEvent{ID: id}
		if args != 0 {
			if ea, err := comQuery(ptr(args), iidToastActivatedArgs); err == nil {
				var hs uintptr
				if hr := vcall(ea, activatedArgsGetArguments, uintptr(unsafe.Pointer(&hs))); !failed(hr) {
					if gotID, action, ok := parseToastArgs(hstringValue(hs)); ok {
						ev.ID, ev.Action = gotID, action
					}
					deleteHString(hs)
				}
				comRelease(ea)
			}
		}
		emitNotify(ev)
		forget()
	}, "activation")
	add(toastAddDismissed, iidToastDismissedHandler, func(sender, args uintptr) {
		reason := int32(-1)
		if args != 0 {
			if da, err := comQuery(ptr(args), iidToastDismissedArgs); err == nil {
				vcall(da, dismissedArgsGetReason, uintptr(unsafe.Pointer(&reason)))
				comRelease(da)
			}
		}
		lastDismissReason.Store(reason)
		if dismissEvent(reason) || reason == -1 {
			emitNotify(caps.NotifyEvent{ID: id, Closed: true})
		}
		forget()
	}, "dismissal")
	add(toastAddFailed, iidToastFailedHandler, func(sender, args uintptr) {
		var code int32
		if args != 0 {
			if fa, err := comQuery(ptr(args), iidToastFailedArgs); err == nil {
				vcall(fa, failedArgsGetErrorCode, uintptr(unsafe.Pointer(&code)))
				comRelease(fa)
			}
		}
		logf("notification %q failed: 0x%08x", id, uint32(code))
		toastFailures.Add(1)
		emitNotify(caps.NotifyEvent{ID: id, Closed: true})
		forget()
	}, "failure")
}

// releaseToasts drops the notifier and live toast references (shutdown).
func (a *App) releaseToasts() {
	t := a.toast
	for id, toast := range t.live {
		comRelease(toast)
		delete(t.live, id)
	}
	comRelease(t.factory)
	comRelease(t.notifier)
	t.factory, t.notifier = nil, nil
	t.ready = false
}

// readAUMIDName returns the DisplayName registered for aumid ("" if none).
func readAUMIDName(aumid string) string {
	k, err := registry.OpenKey(registry.CURRENT_USER, aumidRegistryPath(aumid), registry.QUERY_VALUE)
	if err != nil {
		return ""
	}
	defer k.Close()
	v, _, _ := k.GetStringValue("DisplayName")
	return v
}

// deleteAUMID removes the registration of aumid (tests; an app leaves its
// registration in place).
func deleteAUMID(aumid string) {
	_ = registry.DeleteKey(registry.CURRENT_USER, aumidRegistryPath(aumid))
}
