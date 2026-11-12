package caps

// InspectorOpener is implemented by Apps that can open the web inspector of a
// view. It works only when the app was created with Debug; otherwise it
// returns an error. Safe to call from any goroutine.
type InspectorOpener interface {
	OpenInspector(v ViewRef) error
}
