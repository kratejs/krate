//go:build windows

package windows

// Vtable layouts for the window chrome code. The WebView2 entries are
// transcribed from WebView2.h (a derived interface repeats the methods of the
// ones it extends); the shell entries come from shobjidl_core.h, objidl.h and
// propsys.h. Slots 0..2 are IUnknown.
var chromeMethods = map[string][]string{
	// WebView2.h line 36633 (the vtable repeats every method of the interfaces it extends)
	"ICoreWebView2Controller2": {
		"QueryInterface",
		"AddRef",
		"Release",
		"get_IsVisible",
		"put_IsVisible",
		"get_Bounds",
		"put_Bounds",
		"get_ZoomFactor",
		"put_ZoomFactor",
		"add_ZoomFactorChanged",
		"remove_ZoomFactorChanged",
		"SetBoundsAndZoomFactor",
		"MoveFocus",
		"add_MoveFocusRequested",
		"remove_MoveFocusRequested",
		"add_GotFocus",
		"remove_GotFocus",
		"add_LostFocus",
		"remove_LostFocus",
		"add_AcceleratorKeyPressed",
		"remove_AcceleratorKeyPressed",
		"get_ParentWindow",
		"put_ParentWindow",
		"NotifyParentWindowPositionChanged",
		"Close",
		"get_CoreWebView2",
		"get_DefaultBackgroundColor",
		"put_DefaultBackgroundColor",
	},
	// WebView2.h line 58720 (the vtable repeats every method of the interfaces it extends)
	"ICoreWebView2Settings9": {
		"QueryInterface",
		"AddRef",
		"Release",
		"get_IsScriptEnabled",
		"put_IsScriptEnabled",
		"get_IsWebMessageEnabled",
		"put_IsWebMessageEnabled",
		"get_AreDefaultScriptDialogsEnabled",
		"put_AreDefaultScriptDialogsEnabled",
		"get_IsStatusBarEnabled",
		"put_IsStatusBarEnabled",
		"get_AreDevToolsEnabled",
		"put_AreDevToolsEnabled",
		"get_AreDefaultContextMenusEnabled",
		"put_AreDefaultContextMenusEnabled",
		"get_AreHostObjectsAllowed",
		"put_AreHostObjectsAllowed",
		"get_IsZoomControlEnabled",
		"put_IsZoomControlEnabled",
		"get_IsBuiltInErrorPageEnabled",
		"put_IsBuiltInErrorPageEnabled",
		"get_UserAgent",
		"put_UserAgent",
		"get_AreBrowserAcceleratorKeysEnabled",
		"put_AreBrowserAcceleratorKeysEnabled",
		"get_IsPasswordAutosaveEnabled",
		"put_IsPasswordAutosaveEnabled",
		"get_IsGeneralAutofillEnabled",
		"put_IsGeneralAutofillEnabled",
		"get_IsPinchZoomEnabled",
		"put_IsPinchZoomEnabled",
		"get_IsSwipeNavigationEnabled",
		"put_IsSwipeNavigationEnabled",
		"get_HiddenPdfToolbarItems",
		"put_HiddenPdfToolbarItems",
		"get_IsReputationCheckingRequired",
		"put_IsReputationCheckingRequired",
		"get_IsNonClientRegionSupportEnabled",
		"put_IsNonClientRegionSupportEnabled",
	},
	// shobjidl_core.h: ITaskbarList, ITaskbarList2 and ITaskbarList3 in one table.
	"ITaskbarList3": {
		"QueryInterface",
		"AddRef",
		"Release",
		"HrInit",
		"AddTab",
		"DeleteTab",
		"ActivateTab",
		"SetActiveAlt",
		"MarkFullscreenWindow",
		"SetProgressValue",
		"SetProgressState",
		"RegisterTab",
		"UnregisterTab",
		"SetTabOrder",
		"SetTabActive",
		"ThumbBarAddButtons",
		"ThumbBarUpdateButtons",
		"ThumbBarSetImageList",
		"SetOverlayIcon",
		"SetThumbnailTooltip",
		"SetThumbnailClip",
	},
	"ICustomDestinationList": {
		"QueryInterface",
		"AddRef",
		"Release",
		"SetAppID",
		"BeginList",
		"AppendCategory",
		"AppendKnownCategory",
		"AddUserTasks",
		"CommitList",
		"GetRemovedDestinations",
		"DeleteList",
		"AbortList",
	},
	"IObjectArray": {
		"QueryInterface",
		"AddRef",
		"Release",
		"GetCount",
		"GetAt",
	},
	"IObjectCollection": {
		"QueryInterface",
		"AddRef",
		"Release",
		"GetCount",
		"GetAt",
		"AddObject",
		"AddFromArray",
		"RemoveObjectAt",
		"Clear",
	},
	"IShellLinkW": {
		"QueryInterface",
		"AddRef",
		"Release",
		"GetPath",
		"GetIDList",
		"SetIDList",
		"GetDescription",
		"SetDescription",
		"GetWorkingDirectory",
		"SetWorkingDirectory",
		"GetArguments",
		"SetArguments",
		"GetHotkey",
		"SetHotkey",
		"GetShowCmd",
		"SetShowCmd",
		"GetIconLocation",
		"SetIconLocation",
		"SetRelativePath",
		"Resolve",
		"SetPath",
	},
	"IPropertyStore": {
		"QueryInterface",
		"AddRef",
		"Release",
		"GetCount",
		"GetAt",
		"GetValue",
		"SetValue",
		"Commit",
	},
}

func init() {
	for name, methods := range chromeMethods {
		ifaceMethods[name] = methods
	}
}

// chromeSlot returns the vtable slot of a method of a chrome interface.
func chromeSlot(iface, method string) int {
	for i, m := range chromeMethods[iface] {
		if m == method {
			return i
		}
	}
	panic("windows: no method " + iface + "." + method + " in the chrome vtable table")
}

// Vtable slots used by the chrome code.
var (
	c2PutDefaultBackground = chromeSlot("ICoreWebView2Controller2", "put_DefaultBackgroundColor")
	set9PutNonClientRegion = chromeSlot("ICoreWebView2Settings9", "put_IsNonClientRegionSupportEnabled")

	tbThumbBarAddButtons    = chromeSlot("ITaskbarList3", "ThumbBarAddButtons")
	tbThumbBarUpdateButtons = chromeSlot("ITaskbarList3", "ThumbBarUpdateButtons")

	cdlSetAppID     = chromeSlot("ICustomDestinationList", "SetAppID")
	cdlBeginList    = chromeSlot("ICustomDestinationList", "BeginList")
	cdlAddUserTasks = chromeSlot("ICustomDestinationList", "AddUserTasks")
	cdlCommitList   = chromeSlot("ICustomDestinationList", "CommitList")
	cdlDeleteList   = chromeSlot("ICustomDestinationList", "DeleteList")
	cdlAbortList    = chromeSlot("ICustomDestinationList", "AbortList")

	objCollAddObject = chromeSlot("IObjectCollection", "AddObject")

	linkSetDescription  = chromeSlot("IShellLinkW", "SetDescription")
	linkSetArguments    = chromeSlot("IShellLinkW", "SetArguments")
	linkSetIconLocation = chromeSlot("IShellLinkW", "SetIconLocation")
	linkSetPath         = chromeSlot("IShellLinkW", "SetPath")

	propStoreSetValue = chromeSlot("IPropertyStore", "SetValue")
	propStoreCommit   = chromeSlot("IPropertyStore", "Commit")
)

// Interface and class ids of the shell objects (shobjidl_core.h).
const (
	iidITaskbarList3                = "ea1afb91-9e28-4b86-90e9-9e9f8a5eefaf"
	clsidDestinationList            = "77f10cf0-3db5-4966-b520-b7c54fd35ed6"
	iidICustomDestinationList       = "6332debf-87b5-4670-90c0-5e57b408a49e"
	clsidEnumerableObjectCollection = "2d3468c1-36a7-43b6-ac24-d3f02fd9607a"
	iidIObjectCollection            = "5632b1a4-e38a-400a-928a-d4cd63230295"
	iidIObjectArray                 = "92ca9dcd-5622-4bba-a805-5e9f541bd8c9"
	clsidShellLink                  = "00021401-0000-0000-c000-000000000046"
	iidIShellLinkW                  = "000214f9-0000-0000-c000-000000000046"
	iidIPropertyStore               = "886d8eeb-8cf2-4446-8d02-cdba1dbdcf99"
	// WebView2.h: ICoreWebView2Controller2 and ICoreWebView2Settings9.
	iidWebView2Controller2 = "c979903e-d4ca-4228-92eb-47ee3fa96eab"
	iidWebView2Settings9   = "0528a73b-e92d-49f4-927a-e547dddaa37d"
	// pkeyTitle is PKEY_Title, the title of a jump list item.
	fmtidTitle = "f29f85e0-4ff9-1068-ab91-08002b27b3d9"
	pidTitle   = 2
)
