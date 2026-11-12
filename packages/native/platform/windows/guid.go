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
