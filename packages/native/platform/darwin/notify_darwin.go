//go:build darwin

package darwin

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
	"github.com/kratejs/krate/packages/native/platform/caps"
)

// UNAuthorizationOptions.
const (
	unAuthSound uint = 1 << 1
	unAuthAlert uint = 1 << 2
)

// Notifications through UNUserNotificationCenter. The framework requires the
// process to run from an .app bundle with a bundle identifier; otherwise
// every call returns an error wrapping krate.ErrUnsupported that names the
// reason and the fix (see errNotBundled). `krate native dev` starts the shell
// from a generated dev bundle for that reason.
//
// Notify and CloseNotification must not be called from the main thread: the
// authorization prompt and the completion callbacks need the main thread and
// other threads free, and these calls wait for them.

var (
	_ caps.Notifier             = (*App)(nil)
	_ caps.NotifierAvailability = (*App)(nil)
	_ caps.NotificationCloser   = (*App)(nil)
)

// Timeouts; variables so tests can shorten them.
var (
	notifyAuthTimeout = 2 * time.Minute
	notifyAddTimeout  = 15 * time.Second
)

var notif struct {
	mu       sync.Mutex // guards the fields below
	fn       func(caps.NotifyEvent)
	seq      uint64
	cats     map[string][]caps.NotificationAction
	granted  bool
	bundleID string

	setupOnce sync.Once
	setupErr  error
	center    objc.ID
	delegate  objc.ID

	authMu sync.Mutex // serializes authorization requests
}

// OnNotify sets the handler for clicks, actions and dismissals. It runs on a
// framework thread and must not block.
func (a *App) OnNotify(fn func(caps.NotifyEvent)) {
	notif.mu.Lock()
	notif.fn = fn
	notif.mu.Unlock()
}

func emitNotify(ev caps.NotifyEvent) {
	notif.mu.Lock()
	fn := notif.fn
	notif.mu.Unlock()
	if fn != nil {
		fn(ev)
	}
}

// mainBundle returns the identifier and path of the main bundle.
func (a *App) mainBundle() (id, path string, err error) {
	err = a.mainCall(func() error {
		b := cls("NSBundle").Send(sel("mainBundle"))
		if b != 0 {
			id = goString(b.Send(sel("bundleIdentifier")))
			path = goString(b.Send(sel("bundlePath")))
		}
		return nil
	})
	return
}

// NotifyAvailability returns nil when notifications can work in this process,
// or the error every Notify call would return.
func (a *App) NotifyAvailability() error {
	id, path, err := a.mainBundle()
	if err != nil {
		return err
	}
	return bundleProblem(id, path)
}

// notifySetup loads the framework, registers the delegate and returns the
// bundle identifier. It runs the Objective-C part on the main thread.
func (a *App) notifySetup() (string, error) {
	id, path, err := a.mainBundle()
	if err != nil {
		return "", err
	}
	if err := bundleProblem(id, path); err != nil {
		return "", err
	}
	notif.setupOnce.Do(func() {
		notif.setupErr = a.mainCall(func() error {
			if _, err := purego.Dlopen("/System/Library/Frameworks/UserNotifications.framework/UserNotifications", purego.RTLD_GLOBAL|purego.RTLD_NOW); err != nil {
				return fmt.Errorf("darwin: load UserNotifications: %w", err)
			}
			center := cls("UNUserNotificationCenter")
			if center == 0 {
				return errors.New("darwin: UNUserNotificationCenter is not available on this macOS")
			}
			if err := registerNotifyDelegate(); err != nil {
				return err
			}
			notif.delegate = cls("KrateNotifyDelegate").Send(sel("new"))
			notif.center = center.Send(sel("currentNotificationCenter"))
			if notif.center == 0 {
				return errors.New("darwin: UNUserNotificationCenter has no current center")
			}
			notif.center.Send(sel("setDelegate:"), notif.delegate)
			return nil
		})
		notif.mu.Lock()
		notif.bundleID = id
		notif.cats = map[string][]caps.NotificationAction{}
		notif.mu.Unlock()
	})
	return id, notif.setupErr
}

// registerNotifyDelegate registers the UNUserNotificationCenterDelegate class.
func registerNotifyDelegate() error {
	protos := []*objc.Protocol{}
	if p := objc.GetProtocol("UNUserNotificationCenterDelegate"); p != nil {
		protos = append(protos, p)
	}
	_, err := objc.RegisterClass("KrateNotifyDelegate", objc.GetClass("NSObject"), protos, nil, []objc.MethodDef{
		{Cmd: sel("userNotificationCenter:willPresentNotification:withCompletionHandler:"),
			Fn: func(self objc.ID, _ objc.SEL, center, note, handler objc.ID) {
				withPool(func() {
					silent := true
					if req := note.Send(sel("request")); req != 0 {
						if content := req.Send(sel("content")); content != 0 {
							silent = content.Send(sel("sound")) == 0
						}
					}
					invokeBlock1(handler, uintptr(foregroundPresentation(silent)))
				})
			}},
		{Cmd: sel("userNotificationCenter:didReceiveNotificationResponse:withCompletionHandler:"),
			Fn: func(self objc.ID, _ objc.SEL, center, response, handler objc.ID) {
				withPool(func() {
					action := goString(response.Send(sel("actionIdentifier")))
					req := response.Send(sel("notification")).Send(sel("request"))
					emitNotify(notifyEvent(action, goString(req.Send(sel("identifier")))))
				})
				invokeBlock1(handler, 0)
			}},
	})
	return err
}

type unResult struct {
	ok  bool
	msg string
}

// errorText describes an NSError (callbacks run inside withPool).
func errorText(e objc.ID) string {
	if e == 0 {
		return ""
	}
	return goString(e.Send(sel("localizedDescription")))
}

// authorize asks for permission once; a grant is remembered, a refusal is
// asked again next time (the system answers without a prompt then).
func (a *App) authorize(bundleID string) error {
	notif.authMu.Lock()
	defer notif.authMu.Unlock()
	notif.mu.Lock()
	granted := notif.granted
	notif.mu.Unlock()
	if granted {
		return nil
	}
	done := make(chan unResult, 1)
	err := a.mainCall(func() error {
		block := objc.NewBlock(func(_ objc.Block, ok bool, e objc.ID) {
			withPool(func() { done <- unResult{ok: ok, msg: errorText(e)} })
		})
		defer block.Release()
		notif.center.Send(sel("requestAuthorizationWithOptions:completionHandler:"), unAuthAlert|unAuthSound, objc.ID(block))
		return nil
	})
	if err != nil {
		return err
	}
	select {
	case r := <-done:
		if r.msg != "" {
			return fmt.Errorf("darwin: requesting notification permission failed: %s", r.msg)
		}
		if !r.ok {
			return errNotifyDenied(bundleID)
		}
	case <-time.After(notifyAuthTimeout):
		return fmt.Errorf("darwin: no answer to the notification permission prompt for %s within %v; answer it or enable notifications in System Settings > Notifications", bundleID, notifyAuthTimeout)
	}
	notif.mu.Lock()
	notif.granted = true
	notif.mu.Unlock()
	return nil
}

// registerCategory makes sure the category for actions exists with the
// framework (main thread). The framework replaces the whole set, so every
// category seen so far is sent again.
func registerCategory(id string, actions []caps.NotificationAction) {
	notif.mu.Lock()
	if _, ok := notif.cats[id]; ok {
		notif.mu.Unlock()
		return
	}
	notif.cats[id] = append([]caps.NotificationAction(nil), actions...)
	snapshot := make(map[string][]caps.NotificationAction, len(notif.cats))
	for k, v := range notif.cats {
		snapshot[k] = v
	}
	notif.mu.Unlock()

	set := cls("NSMutableSet").Send(sel("set"))
	for cid, acts := range snapshot {
		arr := cls("NSMutableArray").Send(sel("array"))
		for _, ac := range acts {
			label := ac.Label
			if label == "" {
				label = ac.ID
			}
			arr.Send(sel("addObject:"), cls("UNNotificationAction").Send(sel("actionWithIdentifier:title:options:"),
				nsString(ac.ID), nsString(label), uint(0)))
		}
		// Option 1 is UNNotificationCategoryOptionCustomDismissAction: without
		// it dismissals are not reported.
		cat := cls("UNNotificationCategory").Send(sel("categoryWithIdentifier:actions:intentIdentifiers:options:"),
			nsString(cid), arr, cls("NSArray").Send(sel("array")), uint(1))
		set.Send(sel("addObject:"), cat)
	}
	notif.center.Send(sel("setNotificationCategories:"), set)
}

// Notify posts a notification. Icon and TimeoutMS are ignored (macOS shows
// the app icon and decides how long a banner stays); Urgency 0 delivers
// quietly and 2 asks for time-sensitive delivery.
func (a *App) Notify(n caps.Notification) error {
	if isMainThread() {
		return errors.New("darwin: Notify must not be called from the main thread")
	}
	bundleID, err := a.notifySetup()
	if err != nil {
		return err
	}
	if err := a.authorize(bundleID); err != nil {
		return err
	}
	notif.mu.Lock()
	notif.seq++
	reqID := requestID(n.ID, notif.seq)
	notif.mu.Unlock()

	done := make(chan unResult, 1)
	err = a.mainCall(func() error {
		registerCategory(categoryID(n.Actions), n.Actions)

		content := cls("UNMutableNotificationContent").Send(sel("new"))
		if content == 0 {
			return errors.New("darwin: could not create notification content")
		}
		defer release(content)
		content.Send(sel("setTitle:"), nsString(n.Title))
		if n.Body != "" {
			content.Send(sel("setBody:"), nsString(n.Body))
		}
		if !n.Silent {
			content.Send(sel("setSound:"), cls("UNNotificationSound").Send(sel("defaultSound")))
		}
		content.Send(sel("setCategoryIdentifier:"), nsString(categoryID(n.Actions)))
		content.Send(sel("setUserInfo:"), nsDictionary(map[string]string{"krateID": n.ID}))
		if level, ok := interruptionLevel(n.Urgency); ok {
			if s := sel("setInterruptionLevel:"); respondsTo(content, s) {
				content.Send(s, uint(level))
			}
		}
		req := cls("UNNotificationRequest").Send(sel("requestWithIdentifier:content:trigger:"),
			nsString(reqID), content, objc.ID(0))
		if req == 0 {
			return errors.New("darwin: could not create the notification request")
		}
		block := objc.NewBlock(func(_ objc.Block, e objc.ID) {
			withPool(func() { done <- unResult{ok: e == 0, msg: errorText(e)} })
		})
		defer block.Release()
		notif.center.Send(sel("addNotificationRequest:withCompletionHandler:"), req, objc.ID(block))
		return nil
	})
	if err != nil {
		return err
	}
	select {
	case r := <-done:
		if !r.ok {
			msg := r.msg
			if msg == "" {
				msg = "unknown error"
			}
			if strings.Contains(strings.ToLower(msg), "not allowed") {
				return errNotifyDenied(bundleID)
			}
			return fmt.Errorf("darwin: posting the notification failed: %s", msg)
		}
	case <-time.After(notifyAddTimeout):
		return fmt.Errorf("darwin: the notification service did not answer within %v", notifyAddTimeout)
	}
	return nil
}

// CloseNotification withdraws a pending or delivered notification by its id.
func (a *App) CloseNotification(id string) error {
	if _, err := a.notifySetup(); err != nil {
		return err
	}
	return a.mainCall(func() error {
		ids := cls("NSArray").Send(sel("arrayWithObject:"), nsString(id))
		notif.center.Send(sel("removePendingNotificationRequestsWithIdentifiers:"), ids)
		notif.center.Send(sel("removeDeliveredNotificationsWithIdentifiers:"), ids)
		return nil
	})
}
