//go:build darwin

package darwin

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/kratejs/krate/packages/native/krate"
	"github.com/kratejs/krate/packages/native/platform/caps"
)

// Pure parts of the UNUserNotificationCenter integration (notify_darwin.go).

// Identifiers the framework uses for the two actions that have no category
// action of their own (UNNotificationResponse.actionIdentifier).
const (
	unDefaultActionID = "com.apple.UNNotificationDefaultActionIdentifier"
	unDismissActionID = "com.apple.UNNotificationDismissActionIdentifier"
)

// UNNotificationPresentationOptions.
const (
	unPresentSound  uint = 1 << 1
	unPresentList   uint = 1 << 3
	unPresentBanner uint = 1 << 4
)

// UNNotificationInterruptionLevel values (macOS 12+).
const (
	unLevelPassive       = 0
	unLevelTimeSensitive = 2
)

// plainCategory is the category of a notification without actions. Every
// notification gets a category so dismissals are reported (the category
// option "custom dismiss action").
const plainCategory = "krate.plain"

// errNotBundled says why notifications cannot work and what to do. It wraps
// krate.ErrUnsupported so callers that test for it behave as on other
// platforms.
func errNotBundled(detail string) error {
	return fmt.Errorf("%w: macOS only delivers notifications to an app with a bundle identifier, and this process is not running from an .app bundle (%s); "+
		"run the app with `krate native dev` (it starts the shell from a generated dev .app bundle) or from the packaged .app",
		krate.ErrUnsupported, detail)
}

// bundleProblem reports why a process cannot use UNUserNotificationCenter: it
// needs a main bundle that is an .app and has a bundle identifier. It returns
// nil when both hold.
func bundleProblem(bundleID, bundlePath string) error {
	switch {
	case bundleID == "":
		return errNotBundled("it has no bundle identifier; executable directory " + bundlePath)
	case !strings.HasSuffix(strings.TrimRight(bundlePath, "/"), ".app"):
		return errNotBundled("bundle identifier " + bundleID + " but the main bundle " + bundlePath + " is not an .app")
	}
	return nil
}

// errNotifyDenied is the error for an authorization the user refused.
func errNotifyDenied(bundleID string) error {
	return krate.Errorf("permission_denied", "notifications are turned off for %s: enable them in System Settings > Notifications, then try again", bundleID)
}

// categoryID returns the category identifier for an action list: stable for
// the same actions, so equal lists share one registered category.
func categoryID(actions []caps.NotificationAction) string {
	if len(actions) == 0 {
		return plainCategory
	}
	h := sha1.New()
	for _, a := range actions {
		fmt.Fprintf(h, "%d:%s\x00%d:%s\x00", len(a.ID), a.ID, len(a.Label), a.Label)
	}
	return "krate.actions." + hex.EncodeToString(h.Sum(nil))[:16]
}

// requestID is the UNNotificationRequest identifier. An empty id gets a
// generated one that never reaches the app (see eventID). Posting the same
// identifier again replaces the earlier notification.
func requestID(id string, seq uint64) string {
	if id != "" {
		return id
	}
	return fmt.Sprintf("krate-auto-%d", seq)
}

// eventID maps a request identifier back to the Notification.ID: generated
// identifiers become "".
func eventID(reqID string) string {
	if strings.HasPrefix(reqID, "krate-auto-") {
		return ""
	}
	return reqID
}

// notifyEvent converts a user response to a NotifyEvent.
func notifyEvent(actionID, reqID string) caps.NotifyEvent {
	ev := caps.NotifyEvent{ID: eventID(reqID)}
	switch actionID {
	case unDefaultActionID:
	case unDismissActionID:
		ev.Closed = true
	default:
		ev.Action = actionID
	}
	return ev
}

// interruptionLevel maps Notification.Urgency (0 low, 1 normal, 2 critical).
// Normal sets nothing. Critical is time sensitive, not "critical": the
// critical level needs an Apple entitlement.
func interruptionLevel(urgency int) (level int, set bool) {
	switch {
	case urgency <= 0:
		return unLevelPassive, true
	case urgency >= 2:
		return unLevelTimeSensitive, true
	}
	return 0, false
}

// foregroundPresentation is what to show when the app is frontmost (without
// this macOS shows nothing for an app in the foreground).
func foregroundPresentation(silent bool) uint {
	o := unPresentBanner | unPresentList
	if !silent {
		o |= unPresentSound
	}
	return o
}
