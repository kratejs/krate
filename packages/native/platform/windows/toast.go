package windows

import (
	"crypto/sha1"
	"encoding/xml"
	"fmt"
	"net/url"
	"strings"

	"github.com/kratejs/krate/packages/native/platform/caps"
)

// Toast notification logic (pure; the WinRT glue is in toast_windows.go).

// WinRT interface ids used by the toast code. The values come from the Windows
// SDK metadata (Windows.UI.Notifications.h, Windows.Data.Xml.Dom.h).
const (
	// IToastNotification, the default interface of ToastNotification
	iidToastNotification = "997e2675-059e-4e60-8b06-1760917c8b80"
	// IToastDismissedEventArgs, IToastFailedEventArgs, IToastActivatedEventArgs
	iidToastDismissedArgs = "3f89d935-d9cb-4538-a0f0-ffe7659938f8"
	iidToastFailedArgs    = "35176862-cfd4-44f8-ad64-f500fd896c3b"
	iidToastActivatedArgs = "e3bf92f3-c197-436f-8265-0625824f8dac"

	// ITypedEventHandler`2
	iidTypedEventHandler = "9de1c534-6ae1-11e0-84e1-18a905bcc53f"

	// Namespace of parameterized interface ids (WinRT type system).
	pinterfaceNamespace = "11f47ad5-7b73-42c0-abae-878b1e16adee"
)

// Runtime class names.
const (
	classToastManager = "Windows.UI.Notifications.ToastNotificationManager"
	classToast        = "Windows.UI.Notifications.ToastNotification"
	classXMLDocument  = "Windows.Data.Xml.Dom.XmlDocument"
)

// pinterfaceIID computes the id of a parameterized WinRT interface from its
// signature: the SHA-1 of the namespace id (big-endian) and the UTF-8
// signature, formatted as a version 5 UUID.
func pinterfaceIID(signature string) string {
	ns, err := parseGUID(pinterfaceNamespace)
	if err != nil {
		panic(err)
	}
	// parseGUID returns the Windows in-memory layout; the hash wants RFC 4122
	// byte order (Data1..Data3 big endian).
	be := ns
	be[0], be[1], be[2], be[3] = ns[3], ns[2], ns[1], ns[0]
	be[4], be[5] = ns[5], ns[4]
	be[6], be[7] = ns[7], ns[6]
	h := sha1.New()
	h.Write(be[:])
	h.Write([]byte(signature))
	sum := h.Sum(nil)[:16]
	sum[6] = sum[6]&0x0f | 0x50
	sum[8] = sum[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", sum[0:4], sum[4:6], sum[6:8], sum[8:10], sum[10:16])
}

// toastHandlerIID is the id of TypedEventHandler<ToastNotification, arg>
// where argSig is the signature of the second type argument.
func toastHandlerIID(argSig string) string {
	return pinterfaceIID("pinterface({" + iidTypedEventHandler + "};" + toastSig + ";" + argSig + ")")
}

const toastSig = "rc(" + classToast + ";{" + iidToastNotification + "})"

var (
	// Activated passes an IInspectable.
	iidToastActivatedHandler = toastHandlerIID("cinterface(IInspectable)")
	iidToastDismissedHandler = toastHandlerIID("rc(Windows.UI.Notifications.ToastDismissedEventArgs;{" + iidToastDismissedArgs + "})")
	iidToastFailedHandler    = toastHandlerIID("rc(Windows.UI.Notifications.ToastFailedEventArgs;{" + iidToastFailedArgs + "})")
)

// ToastDismissalReason values (ToastDismissalReason enum).
const (
	dismissUserCanceled     = 0
	dismissApplicationHid   = 1
	dismissTimedOut         = 2
	notificationEnabled     = 0 // NotificationSetting.Enabled
	toastMaxAUMIDLength     = 128
	toastLongDurationAbove  = 10000 // ms; WinRT has only "short" (7 s) and "long" (25 s)
	toastAUMIDRegistryRoot  = `Software\Classes\AppUserModelId`
	toastDefaultAUMIDPrefix = "Krate."
)

// dismissEvent reports whether a dismissal should reach the app as a
// notification event. A toast the app hid itself is not reported.
func dismissEvent(reason int32) bool {
	return reason == dismissUserCanceled || reason == dismissTimedOut
}

// settingProblem explains a NotificationSetting other than Enabled; "" means
// notifications will be shown.
func settingProblem(setting int32) string {
	switch setting {
	case notificationEnabled:
		return ""
	case 1:
		return "notifications are turned off for this app in Windows settings"
	case 2:
		return "notifications are turned off for this user in Windows settings"
	case 3:
		return "notifications are turned off by group policy"
	case 4:
		return "notifications are turned off by the app manifest"
	}
	return fmt.Sprintf("notifications are not enabled (setting %d)", setting)
}

// aumidFor derives the AppUserModelID of an unpackaged app from its app id:
// letters, digits, dots, hyphens and underscores only, at most 128 units.
func aumidFor(appID string) string {
	var b strings.Builder
	for _, r := range appID {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('.')
		}
	}
	s := strings.Trim(b.String(), ".")
	if s == "" {
		s = "App"
	}
	// An app id is already reverse-DNS ("com.acme.app"): keep it as is when
	// it has a dot, otherwise namespace it.
	if !strings.Contains(s, ".") {
		s = toastDefaultAUMIDPrefix + s
	}
	if len(s) > toastMaxAUMIDLength {
		s = s[:toastMaxAUMIDLength]
	}
	return s
}

// aumidRegistryPath is the HKCU key that names the display name and icon of an
// unpackaged app's toasts.
func aumidRegistryPath(aumid string) string { return toastAUMIDRegistryRoot + `\` + aumid }

// aumidValues are the registry values of an unpackaged app's AppUserModelId
// key. DisplayName is the app name Windows shows on the toast; IconUri is the
// default toast icon (a PNG, JPG or ICO path) and is omitted when empty.
func aumidValues(displayName, iconPath string) map[string]string {
	v := map[string]string{"DisplayName": displayName}
	if iconPath != "" {
		v["IconUri"] = iconPath
	}
	return v
}

// toastMode says how toasts are addressed.
type toastMode int

const (
	// toastRegistered: an unpackaged app registers its AUMID under HKCU and
	// passes it to CreateToastNotifierWithId.
	toastRegistered toastMode = iota
	// toastPackaged: an MSIX app takes the AUMID from its package; the
	// parameterless CreateToastNotifier is used and nothing is registered.
	toastPackaged
)

func toastModeFor(packaged bool) toastMode {
	if packaged {
		return toastPackaged
	}
	return toastRegistered
}

// fileURI returns the file:/// URI for an absolute Windows path, or "" when
// path is not absolute (an icon name, not a file).
func fileURI(path string) string {
	if path == "" {
		return ""
	}
	p := strings.ReplaceAll(path, `\`, "/")
	switch {
	case len(p) >= 3 && p[1] == ':' && p[2] == '/':
		// C:/dir/file
	case strings.HasPrefix(p, "//"):
		// \\server\share\file becomes file://server/share/file
		u := url.URL{Scheme: "file", Host: strings.SplitN(p[2:], "/", 2)[0]}
		if i := strings.IndexByte(p[2:], '/'); i >= 0 {
			u.Path = p[2+i:]
		}
		return u.String()
	case strings.HasPrefix(p, "/"):
		// A rooted path without a drive is not an absolute Windows path.
		return ""
	default:
		return ""
	}
	u := url.URL{Scheme: "file", Path: "/" + p}
	return u.String()
}

// toastArgs encodes what a click returns: the notification id and the action
// id ("" for the body). The activation arguments string is opaque to Windows.
func toastArgs(id, action string) string {
	v := url.Values{}
	v.Set("krate", "1")
	v.Set("id", id)
	if action != "" {
		v.Set("action", action)
	}
	return v.Encode()
}

// parseToastArgs reverses toastArgs. ok is false for arguments this package
// did not write.
func parseToastArgs(s string) (id, action string, ok bool) {
	v, err := url.ParseQuery(s)
	if err != nil || v.Get("krate") != "1" {
		return "", "", false
	}
	return v.Get("id"), v.Get("action"), true
}

func xmlText(s string) string {
	var b strings.Builder
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}

// buildToastXML returns the toast XML for n. iconURI is the file URI of the
// image (empty for none). The text is escaped, so notification content can
// never inject markup. The element and attribute names are fixed; every
// caller-supplied value goes through xmlText.
func buildToastXML(n caps.Notification, iconURI string) string {
	var b strings.Builder
	b.WriteString(`<toast`)
	b.WriteString(` launch="` + xmlText(toastArgs(n.ID, "")) + `" activationType="foreground"`)
	switch {
	case n.TimeoutMS < 0 && len(n.Actions) > 0, n.Urgency >= 2 && len(n.Actions) > 0:
		// A reminder stays on screen until dismissed; Windows requires a button.
		b.WriteString(` scenario="reminder"`)
	case n.TimeoutMS > toastLongDurationAbove, n.TimeoutMS < 0, n.Urgency >= 2:
		b.WriteString(` duration="long"`)
	}
	b.WriteString(`><visual><binding template="ToastGeneric">`)
	if n.Title != "" {
		b.WriteString("<text>" + xmlText(n.Title) + "</text>")
	}
	if n.Body != "" {
		b.WriteString("<text>" + xmlText(n.Body) + "</text>")
	}
	if n.Title == "" && n.Body == "" {
		b.WriteString("<text> </text>")
	}
	if iconURI != "" {
		b.WriteString(`<image placement="appLogoOverride" src="` + xmlText(iconURI) + `"/>`)
	}
	b.WriteString("</binding></visual>")
	if len(n.Actions) > 0 {
		b.WriteString("<actions>")
		for i, a := range n.Actions {
			if i == 5 {
				break // Windows shows at most five buttons
			}
			b.WriteString(`<action content="` + xmlText(a.Label) + `" arguments="` + xmlText(toastArgs(n.ID, a.ID)) + `" activationType="foreground"/>`)
		}
		b.WriteString("</actions>")
	}
	if n.Silent {
		b.WriteString(`<audio silent="true"/>`)
	}
	b.WriteString("</toast>")
	return b.String()
}
