//go:build windows

package windows

// Moved from guid.go: used only by the Windows glue.
const (
	// WebView2.h line 45951
	iidEnvironment8 = "d6eb91dd-c3d2-45e5-bd29-6dc2bc4de9cf"
	// ICoreWebView2NavigationCompletedEventHandler
	iidNavigationCompletedHandler = "d33a35bf-1c49-4f98-93ab-006e0533fe1c"
	// ICoreWebView2WindowCloseRequestedEventHandler
	iidWindowCloseRequestedHandler = "5c19e9e0-092f-486b-affa-ca8231913039"
)

// Moved from toast.go: used only by the Windows glue.
const (
	iidAgileObject = "94ea2b94-e9cc-49e0-c0ff-ee64ca8f5b90"
	// IActivationFactory
	iidActivationFactory = "00000035-0000-0000-C000-000000000046"
	// IToastNotificationManagerStatics
	iidToastManagerStatics = "50ac103f-d235-4598-bbef-98fe4d1a3ad4"
	// IToastNotificationFactory
	iidToastFactory = "04124b20-82c6-4229-b109-fd9ed4662b53"
	// IXmlDocument, IXmlDocumentIO
	iidXMLDocument   = "f7f3a506-1e87-42d6-bcfb-b8c809fa5494"
	iidXMLDocumentIO = "6cd0e74e-ee65-4489-9ebf-ca43e87ba637"
)

// Moved from tray.go: used only by the Windows glue.
const (
	nimAdd             = 0
	nimModify          = 1
	nimDelete          = 2
	nimSetVersion      = 4
	nifMessage         = 0x01
	nifIcon            = 0x02
	nifTip             = 0x04
	nifShowTip         = 0x80
	notifyIconVersion4 = 4
	// wmTrayCallback is the callback message of the tray icon.
	wmTrayCallback = 0x8000 + 0x4C // WM_APP + 0x4C
	trayIconID     = 1
	// szTip holds 128 UTF-16 units including the NUL.
	trayTipUnits = 128
)

// Moved from vtindex.go: used only by the Windows glue.
var (
	ctlPutIsVisible           = vtIndex("ICoreWebView2Controller", "put_IsVisible")
	wvGetSource               = vtIndex("ICoreWebView2", "get_Source")
	wvAddNavigationCompleted  = vtIndex("ICoreWebView2", "add_NavigationCompleted")
	wvAddWindowCloseRequested = vtIndex("ICoreWebView2", "add_WindowCloseRequested")
	streamReadSlot            = vtIndex("IStream", "Read")
	toastMgrCreateNotifier    = vtIndex("IToastNotificationManagerStatics", "CreateToastNotifier")
	notifierHide              = vtIndex("IToastNotifier", "Hide")
	activatedArgsGetArguments = vtIndex("IToastActivatedEventArgs", "get_Arguments")
	dismissedArgsGetReason    = vtIndex("IToastDismissedEventArgs", "get_Reason")
	failedArgsGetErrorCode    = vtIndex("IToastFailedEventArgs", "get_ErrorCode")
)

// Moved from uri.go.
//
// resourceFilter is the WebResourceRequested filter pattern.
const resourceFilter = Origin + "/*"
