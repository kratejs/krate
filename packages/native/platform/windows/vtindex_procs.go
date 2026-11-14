//go:build windows

package windows

// Vtable slots used by the process and automation features.
var (
	wvCallCDP              = vtIndex("ICoreWebView2", "CallDevToolsProtocolMethod")
	wvGetCDPEventReceiver  = vtIndex("ICoreWebView2", "GetDevToolsProtocolEventReceiver")
	cdpRecvAdd             = vtIndex("ICoreWebView2DevToolsProtocolEventReceiver", "add_DevToolsProtocolEventReceived")
	cdpRecvRemove          = vtIndex("ICoreWebView2DevToolsProtocolEventReceiver", "remove_DevToolsProtocolEventReceived")
	cdpArgsGetParams       = vtIndex("ICoreWebView2DevToolsProtocolEventReceivedEventArgs", "get_ParameterObjectAsJson")
	profile7AddExtension   = vtIndex("ICoreWebView2Profile7", "AddBrowserExtension")
	profile7GetExtensions  = vtIndex("ICoreWebView2Profile7", "GetBrowserExtensions")
	extGetID               = vtIndex("ICoreWebView2BrowserExtension", "get_Id")
	extGetName             = vtIndex("ICoreWebView2BrowserExtension", "get_Name")
	extRemove              = vtIndex("ICoreWebView2BrowserExtension", "Remove")
	extGetIsEnabled        = vtIndex("ICoreWebView2BrowserExtension", "get_IsEnabled")
	extListGetCount        = vtIndex("ICoreWebView2BrowserExtensionList", "get_Count")
	extListGetValueAtIndex = vtIndex("ICoreWebView2BrowserExtensionList", "GetValueAtIndex")

	streamSeek = vtIndex("IStream", "Seek")
	streamRead = vtIndex("IStream", "Read")
)
