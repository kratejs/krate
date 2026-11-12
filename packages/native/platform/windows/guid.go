package windows

import (
	"fmt"
	"strconv"
	"strings"
)

// Interface ids. The WebView2 ids come from the MIDL_INTERFACE lines of
// WebView2.h; the rest are the standard COM ids.
const (
	iidIUnknown          = "00000000-0000-0000-C000-000000000046"
	iidISequentialStream = "0C733A30-2A1C-11CE-ADE5-00AA0044773D"
	iidIStream           = "0000000C-0000-0000-C000-000000000046"

	// WebView2.h line 44351
	iidEnvCompletedHandler = "4e8a3389-c9d8-4bd2-b6b5-124fee6cc14d"
	// WebView2.h line 44663
	iidControllerCompletedHandler = "6c4819f3-c9b7-4260-8127-c9f5bde7f68c"
	// WebView2.h line 4820
	iidWebResourceRequestedHandler = "ab00b74c-15f1-4646-80e8-e76341d25d71"
	// WebView2.h line 4218
	iidNavigationStartingHandler = "9adbe429-f36d-432b-9ddc-f8881fbd76e3"
	// WebView2.h line 4300
	iidNewWindowRequestedHandler = "d4c185fe-c81c-4989-97af-2d3fa7ab5651"
	// WebView2.h line 5078
	iidExecuteScriptHandler = "49511172-cc67-4bca-9923-137112f4c4cc"
	// WebView2.h line 48591
	iidEnvironmentOptions = "2fde08a8-1e9a-4766-8c05-95a9ceb9d1c5"
	// ICoreWebView2WebMessageReceivedEventHandler
	iidWebMessageHandler = "57213f19-00e6-49fa-8e07-898ea01ecbd2"
	// ICoreWebView2WebMessageReceivedEventArgs2
	iidWebMessageArgs2 = "06fc7ab7-c90c-4297-9389-33ca01cf6d5e"
	// ICoreWebView2ObjectCollectionView
	iidObjectCollectionView = "0f36fd87-4f69-4415-98da-888f89fb9a33"
	// ICoreWebView2File
	iidWebView2File = "f2c19559-6bc1-4583-a757-90021be9afec"
	// WebView2.h line 4386
	iidPermissionRequestedHandler = "15e1c6a3-c72a-4df3-91d7-d097fbec6bfd"
	// WebView2.h line 21808 (ICoreWebView2_19, MemoryUsageTargetLevel)
	iidWebView2_19 = "6921f954-79b0-437f-a997-c85811897c68"
	// ICoreWebView2PermissionRequestedEventArgs3
	iidPermissionArgs3 = "e61670bc-3dce-4177-86d2-c629ae3cb6ac"
	// ICoreWebView2_27
	iidWebView2_27 = "00fbe33b-8c07-517c-aa23-0ddd4b5f6fa0"
	// ICoreWebView2ScreenCaptureStartingEventHandler
	iidScreenCaptureStartingHandler = "e24ff05a-1db5-59d9-89f3-3c864268db4a"

	// Web contents controls (page.go): zoom, print, capture, downloads, cookies.
	// ICoreWebView2_2
	iidWV2_2 = "9e8f0cf8-e670-4b5e-b2bc-73e061e3184c"
	// ICoreWebView2_4
	iidWV2_4 = "20d02d59-6df2-42dc-bd06-f98a694b1302"
	// ICoreWebView2_7
	iidWV2_7 = "79c24d83-09a3-45ae-9418-487f32a58740"
	// ICoreWebView2_13
	iidWV2_13 = "f75f09a8-667e-4983-88d6-c8773f315e84"
	// ICoreWebView2_16
	iidWV2_16 = "0eb34dc9-9f91-41e1-8639-95cd5943906b"
	// ICoreWebView2Environment6
	iidWV2Environment6 = "e59ee362-acbd-4857-9a8e-d3644d9459a9"
	// ICoreWebView2Profile2
	iidWV2Profile2 = "fa740d4b-5eae-4344-a8ad-74be31925397"
	// ICoreWebView2DownloadStartingEventHandler
	iidWV2DownloadStartingEventHandler = "efedc989-c396-41ca-83f7-07f845a55724"
	// ICoreWebView2GetCookiesCompletedHandler
	iidWV2GetCookiesCompletedHandler = "5a4f5069-5c15-47c3-8646-f4de1c116670"
	// ICoreWebView2PrintToPdfCompletedHandler
	iidWV2PrintToPdfCompletedHandler = "ccf1ef04-fd8e-4d5f-b2de-0983e41b8c36"
	// ICoreWebView2CapturePreviewCompletedHandler
	iidWV2CapturePreviewCompletedHandler = "697e05e9-3d8f-45fa-96f4-8ffe1ededaf5"
	// ICoreWebView2ClearBrowsingDataCompletedHandler
	iidWV2ClearBrowsingDataCompletedHandler = "e9710a06-1d1d-49b2-8234-226f35846ae5"
	// ICoreWebView2StateChangedEventHandler
	iidWV2StateChangedEventHandler = "81336594-7ede-4ba9-bf71-acf0a95b58dd"
	// ICoreWebView2BytesReceivedChangedEventHandler
	iidWV2BytesReceivedChangedEventHandler = "828e8ab6-d94c-4264-9cef-5217170d6251"
	// WebView2.h line 13620
	iidContextMenuRequestedHandler = "04d3fe1d-ab87-42fb-a898-da241d35b63c"
	// WebView2.h line 35340
	iidCustomItemSelectedHandler = "49e1d0bc-fe9e-4481-b7c2-32324aa21998"
	// ICoreWebView2_11 (add_ContextMenuRequested is on this interface)
	iidWebView2_11 = "0be78e56-c193-4051-b943-23b460c08bdb"
	// ICoreWebView2Environment9
	iidEnvironment9 = "f06f41bf-4b5a-49d8-b9f6-fa16cd29f274"
	// Process and automation interfaces (snapshot_windows.go, automation_windows.go,
	// extensions_windows.go). Ids from the MIDL_INTERFACE lines of WebView2.h.
	// ICoreWebView2CapturePreviewCompletedHandler
	iidCapturePreviewHandler = "697e05e9-3d8f-45fa-96f4-8ffe1ededaf5"
	// ICoreWebView2CallDevToolsProtocolMethodCompletedHandler
	iidCallCDPHandler = "5c4889f0-5ef6-4c5a-952c-d8f1b92d0574"
	// ICoreWebView2DevToolsProtocolEventReceivedEventHandler
	iidCDPEventHandler = "e2fda4be-5456-406c-a261-3d452138362c"
	// ICoreWebView2_13
	iidWebView2_13 = "f75f09a8-667e-4983-88d6-c8773f315e84"
	// ICoreWebView2Profile7
	iidProfile7 = "7b4c7906-a1aa-4cb4-b723-db09f813d541"
	// ICoreWebView2BrowserExtension
	iidBrowserExtension = "7ef7ffa0-fac5-462c-b189-3d9edbe575da"
	// ICoreWebView2BrowserExtensionList
	iidBrowserExtensionList = "2ef3d2dc-bd5f-4f4d-90af-fd67798f0c2f"
	// ICoreWebView2ProfileAddBrowserExtensionCompletedHandler
	iidAddExtensionHandler = "df1aab27-82b9-4ab6-aae8-017a49398c14"
	// ICoreWebView2ProfileGetBrowserExtensionsCompletedHandler
	iidGetExtensionsHandler = "fce16a1c-f107-4601-8b75-fc4940ae25d0"
	// ICoreWebView2BrowserExtensionRemoveCompletedHandler
	iidRemoveExtensionHandler = "8e41909a-9b18-4bb1-8cdf-930f467a50be"
	// ICoreWebView2EnvironmentOptions6
	iidEnvironmentOptions6 = "57d29cc3-c84f-42a0-b0e2-effbd5e179de"
)

// guid is a GUID in its in-memory (Windows GUID struct) layout: Data1, Data2
// and Data3 little endian, Data4 as is.
type guid [16]byte

// parseGUID parses the textual form xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx.
func parseGUID(s string) (guid, error) {
	var g guid
	parts := strings.Split(s, "-")
	if len(parts) != 5 || len(parts[0]) != 8 || len(parts[1]) != 4 ||
		len(parts[2]) != 4 || len(parts[3]) != 4 || len(parts[4]) != 12 {
		return g, fmt.Errorf("bad guid %q", s)
	}
	d1, err1 := strconv.ParseUint(parts[0], 16, 32)
	d2, err2 := strconv.ParseUint(parts[1], 16, 16)
	d3, err3 := strconv.ParseUint(parts[2], 16, 16)
	if err1 != nil || err2 != nil || err3 != nil {
		return g, fmt.Errorf("bad guid %q", s)
	}
	for i := 0; i < 4; i++ {
		g[i] = byte(d1 >> (8 * i))
	}
	g[4], g[5] = byte(d2), byte(d2>>8)
	g[6], g[7] = byte(d3), byte(d3>>8)
	tail := parts[3] + parts[4]
	for i := 0; i < 8; i++ {
		b, err := strconv.ParseUint(tail[2*i:2*i+2], 16, 8)
		if err != nil {
			return g, fmt.Errorf("bad guid %q", s)
		}
		g[8+i] = byte(b)
	}
	return g, nil
}

func mustGUID(s string) guid {
	g, err := parseGUID(s)
	if err != nil {
		panic(err)
	}
	return g
}
