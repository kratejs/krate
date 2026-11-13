package caps

// PermissionKind names what a page asked the webview for. The platform layers
// translate the engine's permission types into these; a request the engine
// reports that has no kind here is PermOther.
type PermissionKind string

// Permission kinds.
const (
	PermCamera        PermissionKind = "camera"
	PermMicrophone    PermissionKind = "microphone"
	PermScreen        PermissionKind = "screen"
	PermLocation      PermissionKind = "location"
	PermNotifications PermissionKind = "notifications"
	PermClipboardRead PermissionKind = "clipboardRead"
	PermOther         PermissionKind = "other"
)

// PermissionRequest is one permission prompt of a page. A getUserMedia call
// for camera and microphone is one request with two kinds.
type PermissionRequest struct {
	// ID is unique within the process.
	ID uint64
	// View is the webview that asked.
	View ViewRef
	// Origin is the origin of the asking page or frame, for example
	// "krate://app".
	Origin string
	Kinds  []PermissionKind
}

// PermissionHandler decides a request. It runs on the platform's main thread
// and must not block: it keeps the answer function and calls it later, from
// any goroutine, exactly once (later calls are ignored). A request nobody
// answers stays denied when the view goes away.
type PermissionHandler func(req PermissionRequest, answer func(allow bool))

// PermissionRequests is implemented by Apps that route the webview's
// permission prompts to the app instead of the engine's own policy. Without a
// handler every request is denied.
type PermissionRequests interface {
	SetPermissionHandler(h PermissionHandler)
}

// PrivacyStatus is the state of an operating system privacy switch.
type PrivacyStatus string

// Privacy states.
const (
	PrivacyGranted       PrivacyStatus = "granted"
	PrivacyDenied        PrivacyStatus = "denied"
	PrivacyNotDetermined PrivacyStatus = "notDetermined"
	PrivacyRestricted    PrivacyStatus = "restricted"
	// PrivacyUnknown means the system has no switch the app can read (a
	// desktop without a portal, an OS version without the API).
	PrivacyUnknown PrivacyStatus = "unknown"
)

// Privacy is implemented by Apps that can read and ask for the operating
// system's camera, microphone, screen recording and location consent.
type Privacy interface {
	// PrivacyStatus reads the state without prompting.
	PrivacyStatus(kind PermissionKind) (PrivacyStatus, error)
	// RequestPrivacy shows the system prompt when the state is
	// PrivacyNotDetermined and blocks until the user answers. It returns the
	// state afterwards. Do not call it from the main thread.
	RequestPrivacy(kind PermissionKind) (PrivacyStatus, error)
}

// CaptureSource is a screen or window that can be captured.
type CaptureSource struct {
	ID string
	// Kind is "screen" or "window".
	Kind string
	Name string
	// App is the owning application of a window, when known.
	App           string
	Width, Height int
	// Thumbnail is a PNG, nil when the platform could not make one.
	Thumbnail []byte
}

// CaptureSources is implemented by Apps that can list capturable screens and
// windows (the desktopCapturer of this SDK).
type CaptureSources interface {
	// CaptureSources lists screens and windows. thumbWidth is the width in
	// pixels of the thumbnails; 0 means no thumbnails.
	CaptureSources(thumbWidth int) ([]CaptureSource, error)
}

// CaptureChooser is implemented by Apps whose engine can be told which source
// the next getDisplayMedia call captures. No engine supported by the platform
// layers offers this today (the system picker always decides), so nothing
// implements it; the SDK answers krate.ErrUnsupported.
type CaptureChooser interface {
	ChooseCaptureSource(view ViewRef, id string) error
}
