package caps

import (
	"context"
	"net/http"
)

// ViewRef identifies one webview to the capability interfaces and to request
// handlers. Every window has a primary view; WindowViews adds more. A ViewRef
// is unique within the process and never reused.
type ViewRef uint64

type viewCtxKey struct{}

// WithView returns ctx carrying the view a request came from. The platform
// layers call it for every scheme request. A zero ref means the request came
// from the native layer but its view could not be determined.
func WithView(ctx context.Context, v ViewRef) context.Context {
	return context.WithValue(ctx, viewCtxKey{}, v)
}

// ViewFrom reports the view a request context was tagged with by the
// platform. ok is false when no platform attached an identity (browser mode,
// plain tests); the caller then has nothing but what the page claims.
func ViewFrom(ctx context.Context) (v ViewRef, ok bool) {
	v, ok = ctx.Value(viewCtxKey{}).(ViewRef)
	return v, ok
}

// ViewOf is ViewFrom for a request.
func ViewOf(r *http.Request) (ViewRef, bool) { return ViewFrom(r.Context()) }

// ViewEventKind names a view event.
type ViewEventKind string

// View events. Closed is a view that went away because its page called
// window.close() or its window closed; Removed is one taken out with
// RemoveView. Navigated is delivered each time a page finishes loading.
const (
	ViewAdded     ViewEventKind = "added"
	ViewRemoved   ViewEventKind = "removed"
	ViewNavigated ViewEventKind = "navigated"
	ViewClosed    ViewEventKind = "closed"
)

// ViewEvent is delivered to the handler set with ViewEvents.OnViewEvent.
type ViewEvent struct {
	View   ViewRef
	Window WindowRef
	Kind   ViewEventKind
	// Path is the route on the app origin (Added and Navigated).
	Path string
}

// ViewInfo describes one view of a window.
type ViewInfo struct {
	Ref     ViewRef
	Path    string
	Primary bool
}

// ViewOptions configures WindowViews.AddView. The new view is split off an
// existing one: Beside (the primary view when zero) is replaced by a split
// whose first or second half the new view becomes.
type ViewOptions struct {
	// Path is the route to open, "/" when empty.
	Path string
	// Beside is the view to split.
	Beside ViewRef
	// Axis says how the two panes sit; see Axis.
	Axis Axis
	// NewFirst puts the new view first (left or top).
	NewFirst bool
	// Share is the fraction of the split the new view gets, in (0,1); zero
	// means one half.
	Share float64
	// MinWidth and MinHeight are the smallest size of the new pane, in pixels.
	MinWidth, MinHeight int
}

// WindowViews is implemented by Window values that can hold several webviews
// laid out as split panes. All views of an app share one web process where the
// platform allows (WebKitGTK related views, one WebView2 environment, one
// WKProcessPool). Every method may be called from any goroutine.
type WindowViews interface {
	// PrimaryView is the view the window was created with.
	PrimaryView() ViewRef
	// AddView creates a view and splits it into the layout.
	AddView(o ViewOptions) (ViewRef, error)
	// RemoveView destroys a view; its sibling takes over the freed space.
	// The primary view cannot be removed (close the window instead).
	RemoveView(v ViewRef) error
	// SetLayout replaces the layout. It must name every view of the window
	// exactly once (see LayoutNode.Validate).
	SetLayout(root *LayoutNode) error
	// Layout returns the current layout with the ratios the user dragged the
	// dividers to.
	Layout() *LayoutNode
	// Views lists the views of the window.
	Views() []ViewInfo
	// FocusView gives keyboard focus to a view.
	FocusView(v ViewRef) error
	// NavigateView loads a route in a view.
	NavigateView(v ViewRef, path string) error
	// EvalView runs JavaScript in a view without waiting for a result.
	EvalView(v ViewRef, js string) error
}

// ViewEvents is implemented by Apps that report view events.
type ViewEvents interface {
	// OnViewEvent sets the single handler for every view's events. fn runs on
	// the main thread and must not block.
	OnViewEvent(fn func(ViewEvent))
}

// ViewLookup is implemented by Apps that can say which window owns a view.
// The shell uses it to bind the view a request came from to a window and its
// permission scope. It is safe to call from any goroutine.
type ViewLookup interface {
	WindowOfView(v ViewRef) (WindowRef, bool)
}
