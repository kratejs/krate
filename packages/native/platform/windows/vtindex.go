package windows

import "fmt"

// vtIndex returns the vtable slot of a method, from the tables in vtables.go.
func vtIndex(iface, method string) int {
	for i, m := range ifaceMethods[iface] {
		if m == method {
			return i
		}
	}
	panic(fmt.Sprintf("windows: no method %s.%s in vtable table", iface, method))
}

// Vtable slots used by the implementation.
var (
	envCreateController  = vtIndex("ICoreWebView2Environment", "CreateCoreWebView2Controller")
	envCreateResponse    = vtIndex("ICoreWebView2Environment", "CreateWebResourceResponse")
	ctlPutBounds         = vtIndex("ICoreWebView2Controller", "put_Bounds")
	ctlPutIsVisible      = vtIndex("ICoreWebView2Controller", "put_IsVisible")
	ctlMoveFocus         = vtIndex("ICoreWebView2Controller", "MoveFocus")
	ctlNotifyParentMoved = vtIndex("ICoreWebView2Controller", "NotifyParentWindowPositionChanged")
	ctlClose             = vtIndex("ICoreWebView2Controller", "Close")
	ctlGetCoreWebView2   = vtIndex("ICoreWebView2Controller", "get_CoreWebView2")

	wvGetSettings                = vtIndex("ICoreWebView2", "get_Settings")
	wvNavigate                   = vtIndex("ICoreWebView2", "Navigate")
	wvAddNavigationStarting      = vtIndex("ICoreWebView2", "add_NavigationStarting")
	wvAddFrameNavigationStarting = vtIndex("ICoreWebView2", "add_FrameNavigationStarting")
	wvAddNewWindowRequested      = vtIndex("ICoreWebView2", "add_NewWindowRequested")
	newWinArgsPutHandled         = vtIndex("ICoreWebView2NewWindowRequestedEventArgs", "put_Handled")
	wvExecuteScript              = vtIndex("ICoreWebView2", "ExecuteScript")
	wvAddWebResourceRequested    = vtIndex("ICoreWebView2", "add_WebResourceRequested")
	wvAddWebResourceFilter       = vtIndex("ICoreWebView2", "AddWebResourceRequestedFilter")
	setPutAreDevToolsEnabled     = vtIndex("ICoreWebView2Settings", "put_AreDevToolsEnabled")
	reqGetUri                    = vtIndex("ICoreWebView2WebResourceRequest", "get_Uri")
	reqGetMethod                 = vtIndex("ICoreWebView2WebResourceRequest", "get_Method")
	reqGetContent                = vtIndex("ICoreWebView2WebResourceRequest", "get_Content")
	reqGetHeaders                = vtIndex("ICoreWebView2WebResourceRequest", "get_Headers")
	hdrGetIterator               = vtIndex("ICoreWebView2HttpRequestHeaders", "GetIterator")
	iterGetCurrentHeader         = vtIndex("ICoreWebView2HttpHeadersCollectionIterator", "GetCurrentHeader")
	iterGetHasCurrentHeader      = vtIndex("ICoreWebView2HttpHeadersCollectionIterator", "get_HasCurrentHeader")
	iterMoveNext                 = vtIndex("ICoreWebView2HttpHeadersCollectionIterator", "MoveNext")
	argsGetRequest               = vtIndex("ICoreWebView2WebResourceRequestedEventArgs", "get_Request")
	argsPutResponse              = vtIndex("ICoreWebView2WebResourceRequestedEventArgs", "put_Response")
	argsGetDeferral              = vtIndex("ICoreWebView2WebResourceRequestedEventArgs", "GetDeferral")
	deferralComplete             = vtIndex("ICoreWebView2Deferral", "Complete")
	navArgsGetUri                = vtIndex("ICoreWebView2NavigationStartingEventArgs", "get_Uri")
	navArgsPutCancel             = vtIndex("ICoreWebView2NavigationStartingEventArgs", "put_Cancel")
	streamReadSlot               = vtIndex("IStream", "Read")
	handlerInvoke                = vtIndex("ICoreWebView2WebResourceRequestedEventHandler", "Invoke")

	// Memory tuning (see memory.go).
	wvAddPermissionRequested  = vtIndex("ICoreWebView2", "add_PermissionRequested")
	permArgsGetPermissionKind = vtIndex("ICoreWebView2PermissionRequestedEventArgs", "get_PermissionKind")
	permArgsPutState          = vtIndex("ICoreWebView2PermissionRequestedEventArgs", "put_State")
	wv19PutMemoryTargetLevel  = vtIndex("ICoreWebView2_19", "put_MemoryUsageTargetLevel")
	optGetArguments           = vtIndex("ICoreWebView2EnvironmentOptions", "get_AdditionalBrowserArguments")
)
