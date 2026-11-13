package diag

// Native (desktop) target: camera, microphone, screen capture and location
// (KR7290). A warning.
const (
	// CodeNativeMediaUsage: the UI asks the webview for the camera,
	// microphone, location or screen capture, and the macOS usage description
	// is missing.
	CodeNativeMediaUsage Code = "KR7290"
)

func init() {
	Register(
		CodeInfo{
			Code:  CodeNativeMediaUsage,
			Title: "Media or location used without a macOS usage description",
			Explanation: "The UI code calls `getUserMedia`, `getDisplayMedia` or `navigator.geolocation`, and " +
				"`native.media.usage` has no sentence for it. macOS ends a process that touches the camera, " +
				"microphone or location when its Info.plist lacks `NSCameraUsageDescription`, " +
				"`NSMicrophoneUsageDescription` or `NSLocationUsageDescription`, and it shows that sentence in its consent " +
				"prompt. Packaging writes a generic sentence so the app does not crash, but a prompt that says why " +
				"the app needs the device gets more yeses. Set `native.media.usage.camera`, `.microphone`, " +
				"`.location` or `.screenCapture`. The check runs for builds that target macOS and only when the scan " +
				"of the built UI finds the call; an API name built at run time is not found.",
		},
	)
}
