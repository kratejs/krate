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
	envCreateController = vtIndex("ICoreWebView2Environment", "CreateCoreWebView2Controller")
	envCreateResponse   = vtIndex("ICoreWebView2Environment", "CreateWebResourceResponse")
	ctlPutBounds        = vtIndex("ICoreWebView2Controller", "put_Bounds")
	ctlMoveFocus        = vtIndex("ICoreWebView2Controller", "MoveFocus")
	ctlClose            = vtIndex("ICoreWebView2Controller", "Close")
	ctlGetCoreWebView2  = vtIndex("ICoreWebView2Controller", "get_CoreWebView2")

	wvGetBrowserProcessId  = vtIndex("ICoreWebView2", "get_BrowserProcessId")
	env8GetProcessInfos    = vtIndex("ICoreWebView2Environment8", "GetProcessInfos")
	procColGetCount        = vtIndex("ICoreWebView2ProcessInfoCollection", "get_Count")
	procColGetValueAtIndex = vtIndex("ICoreWebView2ProcessInfoCollection", "GetValueAtIndex")
	procInfoGetProcessId   = vtIndex("ICoreWebView2ProcessInfo", "get_ProcessId")
	procInfoGetKind        = vtIndex("ICoreWebView2ProcessInfo", "get_Kind")

	wvGetSettings                = vtIndex("ICoreWebView2", "get_Settings")
	wvNavigate                   = vtIndex("ICoreWebView2", "Navigate")
	wvAddNavigationStarting      = vtIndex("ICoreWebView2", "add_NavigationStarting")
	wvAddFrameNavigationStarting = vtIndex("ICoreWebView2", "add_FrameNavigationStarting")
	wvAddNewWindowRequested      = vtIndex("ICoreWebView2", "add_NewWindowRequested")
	newWinArgsPutHandled         = vtIndex("ICoreWebView2NewWindowRequestedEventArgs", "put_Handled")
	wvAddContextMenuRequested    = vtIndex("ICoreWebView2_19", "add_ContextMenuRequested")
	wvOpenDevToolsWindow         = vtIndex("ICoreWebView2", "OpenDevToolsWindow")
	wvCallDevToolsProtocol       = vtIndex("ICoreWebView2", "CallDevToolsProtocolMethod")
	cmArgsGetMenuItems           = vtIndex("ICoreWebView2ContextMenuRequestedEventArgs", "get_MenuItems")
	cmArgsGetTarget              = vtIndex("ICoreWebView2ContextMenuRequestedEventArgs", "get_ContextMenuTarget")
	cmArgsGetLocation            = vtIndex("ICoreWebView2ContextMenuRequestedEventArgs", "get_Location")
	cmArgsPutHandled             = vtIndex("ICoreWebView2ContextMenuRequestedEventArgs", "put_Handled")
	cmTargetGetKind              = vtIndex("ICoreWebView2ContextMenuTarget", "get_Kind")
	cmTargetGetIsEditable        = vtIndex("ICoreWebView2ContextMenuTarget", "get_IsEditable")
	cmTargetGetHasLinkUri        = vtIndex("ICoreWebView2ContextMenuTarget", "get_HasLinkUri")
	cmTargetGetLinkUri           = vtIndex("ICoreWebView2ContextMenuTarget", "get_LinkUri")
	cmTargetGetHasSourceUri      = vtIndex("ICoreWebView2ContextMenuTarget", "get_HasSourceUri")
	cmTargetGetSourceUri         = vtIndex("ICoreWebView2ContextMenuTarget", "get_SourceUri")
	cmTargetGetHasSelection      = vtIndex("ICoreWebView2ContextMenuTarget", "get_HasSelection")
	cmTargetGetSelectionText     = vtIndex("ICoreWebView2ContextMenuTarget", "get_SelectionText")
	cmItemsGetCount              = vtIndex("ICoreWebView2ContextMenuItemCollection", "get_Count")
	cmItemsGetAt                 = vtIndex("ICoreWebView2ContextMenuItemCollection", "GetValueAtIndex")
	cmItemsInsertAt              = vtIndex("ICoreWebView2ContextMenuItemCollection", "InsertValueAtIndex")
	cmItemGetName                = vtIndex("ICoreWebView2ContextMenuItem", "get_Name")
	cmItemGetLabel               = vtIndex("ICoreWebView2ContextMenuItem", "get_Label")
	cmItemPutIsEnabled           = vtIndex("ICoreWebView2ContextMenuItem", "put_IsEnabled")
	cmItemPutIsChecked           = vtIndex("ICoreWebView2ContextMenuItem", "put_IsChecked")
	cmItemGetIsChecked           = vtIndex("ICoreWebView2ContextMenuItem", "get_IsChecked")
	cmItemGetChildren            = vtIndex("ICoreWebView2ContextMenuItem", "get_Children")
	cmItemAddCustomSelected      = vtIndex("ICoreWebView2ContextMenuItem", "add_CustomItemSelected")
	env9CreateContextMenuItem    = vtIndex("ICoreWebView2Environment9", "CreateContextMenuItem")
	wvExecuteScript              = vtIndex("ICoreWebView2", "ExecuteScript")
	wvAddScriptOnDocCreated      = vtIndex("ICoreWebView2", "AddScriptToExecuteOnDocumentCreated")
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
	handlerInvoke                = vtIndex("ICoreWebView2WebResourceRequestedEventHandler", "Invoke")

	// Memory tuning (see memory.go).
	wvAddPermissionRequested  = vtIndex("ICoreWebView2", "add_PermissionRequested")
	permArgsGetPermissionKind = vtIndex("ICoreWebView2PermissionRequestedEventArgs", "get_PermissionKind")
	permArgsPutState          = vtIndex("ICoreWebView2PermissionRequestedEventArgs", "put_State")
	wv19PutMemoryTargetLevel  = vtIndex("ICoreWebView2_19", "put_MemoryUsageTargetLevel")
	optGetArguments           = vtIndex("ICoreWebView2EnvironmentOptions", "get_AdditionalBrowserArguments")

	// Toast notifications (see toast.go).
	activationFactoryActivate = vtIndex("IActivationFactory", "ActivateInstance")
	toastMgrCreateNotifierID  = vtIndex("IToastNotificationManagerStatics", "CreateToastNotifierWithId")
	toastFactoryCreate        = vtIndex("IToastNotificationFactory", "CreateToastNotification")
	notifierShow              = vtIndex("IToastNotifier", "Show")
	notifierGetSetting        = vtIndex("IToastNotifier", "get_Setting")
	toastAddDismissed         = vtIndex("IToastNotification", "add_Dismissed")
	toastAddActivated         = vtIndex("IToastNotification", "add_Activated")
	toastAddFailed            = vtIndex("IToastNotification", "add_Failed")
	xmlIOLoadXML              = vtIndex("IXmlDocumentIO", "LoadXml")
)
