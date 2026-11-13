package windows

import "testing"

// TestProcsVtableSlots pins the slots and interface ids the capture, DevTools
// protocol and extension code use against WebView2.h (scratch check_wv2.py
// compares the whole tables).
func TestProcsVtableSlots(t *testing.T) {
	for _, c := range []struct {
		got, want int
		name      string
	}{
		{wvCapturePreview, 30, "CapturePreview"},
		{wvCallCDP, 36, "CallDevToolsProtocolMethod"},
		{wvGetCDPEventReceiver, 42, "GetDevToolsProtocolEventReceiver"},
		{cdpRecvAdd, 3, "add_DevToolsProtocolEventReceived"},
		{cdpRecvRemove, 4, "remove_DevToolsProtocolEventReceived"},
		{cdpArgsGetParams, 3, "get_ParameterObjectAsJson"},
		{wv13GetProfile, 105, "get_Profile"},
		{profile7AddExtension, 22, "AddBrowserExtension"},
		{profile7GetExtensions, 23, "GetBrowserExtensions"},
		{extGetID, 3, "BrowserExtension.get_Id"},
		{extGetName, 4, "BrowserExtension.get_Name"},
		{extRemove, 5, "BrowserExtension.Remove"},
		{extGetIsEnabled, 6, "BrowserExtension.get_IsEnabled"},
		{extListGetCount, 3, "BrowserExtensionList.get_Count"},
		{extListGetValueAtIndex, 4, "BrowserExtensionList.GetValueAtIndex"},
		{opt6GetExtensionsEnabled, 3, "get_AreBrowserExtensionsEnabled"},
		{opt6PutExtensionsEnabled, 4, "put_AreBrowserExtensionsEnabled"},
		{streamRead, 3, "IStream.Read"},
		{streamSeek, 5, "IStream.Seek"},
	} {
		if c.got != c.want {
			t.Errorf("%s: slot %d, want %d", c.name, c.got, c.want)
		}
	}
	for _, s := range []string{
		iidCapturePreviewHandler, iidCallCDPHandler, iidCDPEventHandler, iidWebView2_13, iidProfile7,
		iidBrowserExtension, iidBrowserExtensionList, iidAddExtensionHandler, iidGetExtensionsHandler,
		iidRemoveExtensionHandler, iidEnvironmentOptions6,
	} {
		if _, err := parseGUID(s); err != nil {
			t.Errorf("%s: %v", s, err)
		}
	}
}
