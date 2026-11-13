package windows

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestParseGUID(t *testing.T) {
	g := mustGUID(iidIUnknown)
	want := guid{0, 0, 0, 0, 0, 0, 0, 0, 0xC0, 0, 0, 0, 0, 0, 0, 0x46}
	if g != want {
		t.Fatalf("IUnknown: %x", g)
	}
	g = mustGUID(iidIStream) // 0000000C-0000-0000-C000-000000000046
	if g[0] != 0x0c || g[8] != 0xc0 || g[15] != 0x46 {
		t.Fatalf("IStream: %x", g)
	}
	// Data1/2/3 are little endian in memory.
	g = mustGUID("4e8a3389-c9d8-4bd2-b6b5-124fee6cc14d")
	if g[0] != 0x89 || g[3] != 0x4e || g[4] != 0xd8 || g[5] != 0xc9 || g[6] != 0xd2 || g[7] != 0x4b || g[8] != 0xb6 || g[15] != 0x4d {
		t.Fatalf("env handler: %x", g)
	}
	for _, s := range []string{
		iidISequentialStream, iidEnvCompletedHandler, iidControllerCompletedHandler,
		iidWebResourceRequestedHandler, iidNavigationStartingHandler, iidNewWindowRequestedHandler, iidExecuteScriptHandler, iidWebMessageHandler, iidWebView2_19, iidWebMessageArgs2, iidObjectCollectionView, iidWebView2File,
		iidPermissionArgs3, iidWebView2_27, iidScreenCaptureStartingHandler,
		iidWV2_2, iidWV2_4, iidWV2_7, iidWV2_13, iidWV2_16, iidWV2Environment6, iidWV2Profile2,
		iidWV2DownloadStartingEventHandler, iidWV2GetCookiesCompletedHandler, iidWV2PrintToPdfCompletedHandler,
		iidWV2CapturePreviewCompletedHandler, iidWV2ClearBrowsingDataCompletedHandler,
		iidWV2StateChangedEventHandler, iidWV2BytesReceivedChangedEventHandler,
		iidITaskbarList3, clsidDestinationList, iidICustomDestinationList, clsidEnumerableObjectCollection,
		iidIObjectCollection, iidIObjectArray, clsidShellLink, iidIShellLinkW, iidIPropertyStore,
		iidWebView2Controller2, iidWebView2Settings9, fmtidTitle,
		iidWV2Environment10, iidWV2ControllerOptions, iidWV2Settings2, iidWV2_14, iidWV2_10, iidWV2ServerCertHandler, iidWV2BasicAuthHandler, iidWV2ServerCertArgs,
	} {
		if _, err := parseGUID(s); err != nil {
			t.Errorf("%s: %v", s, err)
		}
	}
	if pidTitle != 2 {
		t.Errorf("PKEY_Title pid %d, want 2", pidTitle)
	}
	for _, bad := range []string{"", "1234", "zzzzzzzz-0000-0000-0000-000000000000", "00000000-0000-0000-0000-00000000000"} {
		if _, err := parseGUID(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

// TestVtableLayout pins the vtable method counts and a few slots against
// WebView2.h so a bad transcription is caught.
func TestVtableLayout(t *testing.T) {
	counts := map[string]int{
		"ICoreWebView2":                                              61,
		"ICoreWebView2Environment":                                   8,
		"ICoreWebView2Controller":                                    26,
		"ICoreWebView2Settings":                                      21,
		"ICoreWebView2WebResourceRequest":                            10,
		"ICoreWebView2WebResourceRequestedEventArgs":                 8,
		"ICoreWebView2WebResourceResponse":                           10,
		"ICoreWebView2HttpRequestHeaders":                            9,
		"ICoreWebView2HttpHeadersCollectionIterator":                 6,
		"ICoreWebView2Deferral":                                      4,
		"ICoreWebView2NavigationStartingEventArgs":                   10,
		"ICoreWebView2CreateCoreWebView2EnvironmentCompletedHandler": 4,
		"ICoreWebView2CreateCoreWebView2ControllerCompletedHandler":  4,
		"ICoreWebView2WebResourceRequestedEventHandler":              4,
		"ICoreWebView2NavigationStartingEventHandler":                4,
		"ICoreWebView2NewWindowRequestedEventHandler":                4,
		"ICoreWebView2NewWindowRequestedEventArgs":                   11,
		"ICoreWebView2ExecuteScriptCompletedHandler":                 4,
		"ICoreWebView2_19":                                           121,
		"IStream":                                                    14,
		"ICoreWebView2WebMessageReceivedEventArgs2":                  7,
		"ICoreWebView2ObjectCollectionView":                          5,
		"ICoreWebView2File":                                          4,
		"IToastNotificationManagerStatics":                           9,
		"IToastNotifier":                                             12,
		"IToastNotification":                                         15,
		"IXmlDocumentIO":                                             9,
	}
	for name, n := range counts {
		ms := ifaceMethods[name]
		if len(ms) != n {
			t.Errorf("%s: %d methods, want %d", name, len(ms), n)
		}
		if len(ms) >= 3 && (ms[0] != "QueryInterface" || ms[1] != "AddRef" || ms[2] != "Release") {
			t.Errorf("%s: IUnknown slots wrong: %v", name, ms[:3])
		}
	}
	slots := []struct {
		got, want int
		name      string
	}{
		{wvNavigate, 5, "Navigate"},
		{msgArgs2GetAdditionalObjects, 6, "get_AdditionalObjects"},
		{objViewGetCount, 3, "ObjectCollectionView.get_Count"},
		{objViewGetValueAtIndex, 4, "ObjectCollectionView.GetValueAtIndex"},
		{fileGetPath, 3, "File.get_Path"},
		{notifierShow, 6, "IToastNotifier.Show"},
		{notifierGetSetting, 8, "IToastNotifier.get_Setting"},
		{toastMgrCreateNotifierID, 7, "CreateToastNotifierWithId"},
		{toastFactoryCreate, 6, "CreateToastNotification"},
		{toastAddDismissed, 9, "add_Dismissed"},
		{toastAddActivated, 11, "add_Activated"},
		{toastAddFailed, 13, "add_Failed"},
		{xmlIOLoadXML, 6, "LoadXml"},
		{activationFactoryActivate, 6, "ActivateInstance"},
		{wvAddNavigationStarting, 7, "add_NavigationStarting"},
		{wvExecuteScript, 29, "ExecuteScript"},
		{wvAddScriptOnDocCreated, 27, "AddScriptToExecuteOnDocumentCreated"},
		{wv19PutMemoryTargetLevel, 120, "put_MemoryUsageTargetLevel"},
		{wvAddWebResourceRequested, 55, "add_WebResourceRequested"},
		{wvAddWebResourceFilter, 57, "AddWebResourceRequestedFilter"},
		{wvGetSettings, 3, "get_Settings"},
		{wvGetBrowserProcessId, 37, "get_BrowserProcessId"},
		{env8GetProcessInfos, 18, "GetProcessInfos"},
		{procColGetCount, 3, "ProcessInfoCollection.get_Count"},
		{procColGetValueAtIndex, 4, "GetValueAtIndex"},
		{procInfoGetProcessId, 3, "ProcessInfo.get_ProcessId"},
		{procInfoGetKind, 4, "ProcessInfo.get_Kind"},
		{envCreateController, 3, "CreateCoreWebView2Controller"},
		{envCreateResponse, 4, "CreateWebResourceResponse"},
		{ctlPutBounds, 6, "put_Bounds"},
		{ctlMoveFocus, 12, "MoveFocus"},
		{ctlClose, 24, "Close"},
		{ctlGetCoreWebView2, 25, "get_CoreWebView2"},
		{setPutAreDevToolsEnabled, 12, "put_AreDevToolsEnabled"},
		{reqGetUri, 3, "get_Uri"},
		{reqGetMethod, 5, "get_Method"},
		{reqGetContent, 7, "get_Content"},
		{reqGetHeaders, 9, "get_Headers"},
		{hdrGetIterator, 8, "GetIterator"},
		{iterGetCurrentHeader, 3, "GetCurrentHeader"},
		{iterGetHasCurrentHeader, 4, "get_HasCurrentHeader"},
		{iterMoveNext, 5, "MoveNext"},
		{argsGetRequest, 3, "get_Request"},
		{argsPutResponse, 5, "put_Response"},
		{argsGetDeferral, 6, "GetDeferral"},
		{deferralComplete, 3, "Complete"},
		{navArgsGetUri, 3, "get_Uri"},
		{navArgsPutCancel, 8, "put_Cancel"},
		{wvAddFrameNavigationStarting, 17, "add_FrameNavigationStarting"},
		{wvAddNewWindowRequested, 44, "add_NewWindowRequested"},
		{newWinArgsPutHandled, 6, "put_Handled"},
		{handlerInvoke, 3, "Invoke"},
		// Web contents controls; the numbers are the slots in WebView2.h.
		{ctlGetZoomFactor, 7, "get_ZoomFactor"},
		{ctlPutZoomFactor, 8, "put_ZoomFactor"},
		{wvCapturePreview, 30, "CapturePreview"},
		{wv2GetCookieManager, 66, "get_CookieManager"},
		{wv4AddDownloadStart, 75, "add_DownloadStarting"},
		{wv7PrintToPdf, 80, "PrintToPdf"},
		// Slots only the Windows files call; pinned here so every OS checks them.
		{wvOpenDevToolsWindow, 51, "OpenDevToolsWindow"},
		{permArgsGetUri, 3, "PermissionRequested.get_Uri"},
		{permArgsGetDeferral, 8, "PermissionRequested.GetDeferral"},
		{permArgs3PutSavesProfile, 12, "put_SavesInProfile"},
		{wv27AddScreenCapture, 133, "add_ScreenCaptureStarting"},
		{scrArgsPutCancel, 4, "ScreenCapture.put_Cancel"},
		{cdlSetAppID, 3, "SetAppID"},
		{cdlAbortList, 11, "AbortList"},
		{objCollAddObject, 5, "AddObject"},
		{linkSetDescription, 7, "SetDescription"},
		{linkSetArguments, 11, "SetArguments"},
		{linkSetIconLocation, 17, "SetIconLocation"},
		{linkSetPath, 20, "SetPath"},
		{propStoreSetValue, 6, "SetValue"},
		{propStoreCommit, 7, "Commit"},
		{scrArgsGetDeferral, 8, "scrArgsGetDeferral"},
		{psPutPageHeight, 10, "psPutPageHeight"},
		{psPutMarginTop, 12, "psPutMarginTop"},
		{psPutMarginBottom, 14, "psPutMarginBottom"},
		{psPutMarginLeft, 16, "psPutMarginLeft"},
		{psPutHeaderFooter, 24, "psPutHeaderFooter"},
		{cookMgrDeleteAll, 10, "cookMgrDeleteAll"},
		{cookieGetValue, 4, "cookieGetValue"},
		{cookieGetDomain, 6, "cookieGetDomain"},
		{cookieGetPath, 7, "cookieGetPath"},
		{cookieGetExpires, 8, "cookieGetExpires"},
		{cookieGetHTTPOnly, 10, "cookieGetHTTPOnly"},
		{cookiePutHTTPOnly, 11, "cookiePutHTTPOnly"},
		{cookieGetSameSite, 12, "cookieGetSameSite"},
		{cookiePutSameSite, 13, "cookiePutSameSite"},
		{cookieGetSecure, 14, "cookieGetSecure"},
		{cookiePutSecure, 15, "cookiePutSecure"},
		{cookieListGetCount, 3, "cookieListGetCount"},
		{dlArgsGetOperation, 3, "dlArgsGetOperation"},
		{dlArgsPutCancel, 5, "dlArgsPutCancel"},
		{dlArgsGetResultPath, 6, "dlArgsGetResultPath"},
		{dlArgsPutHandled, 9, "dlArgsPutHandled"},
		{dlOpAddBytes, 3, "dlOpAddBytes"},
		{dlOpGetURI, 9, "dlOpGetURI"},
		{dlOpGetMIME, 11, "dlOpGetMIME"},
		{dlOpGetTotal, 12, "dlOpGetTotal"},
		{dlOpGetReceived, 13, "dlOpGetReceived"},
		{dlOpGetState, 16, "dlOpGetState"},
		{dlOpGetInterrupt, 17, "dlOpGetInterrupt"},
		{wv13GetProfile, 105, "get_Profile"},
		{wv16ShowPrintUI, 114, "ShowPrintUI"},
		{env6CreatePrintSet, 14, "CreatePrintSettings"},
		{profile2ClearData, 10, "ClearBrowsingData"},
		{psPutOrientation, 4, "put_Orientation"},
		{psPutPageWidth, 8, "put_PageWidth"},
		{psPutMarginRight, 18, "put_MarginRight"},
		{psPutBackgrounds, 20, "put_ShouldPrintBackgrounds"},
		{cookMgrCreate, 3, "CreateCookie"},
		{cookMgrGetCookies, 5, "GetCookies"},
		{cookMgrAddOrUpdate, 6, "AddOrUpdateCookie"},
		{cookMgrDeleteCookies, 8, "DeleteCookies"},
		{cookieGetName, 3, "Cookie.get_Name"},
		{cookiePutExpires, 9, "put_Expires"},
		{cookieGetIsSession, 16, "get_IsSession"},
		{cookieListGetAt, 4, "CookieList.GetValueAtIndex"},
		{dlArgsPutResultPath, 7, "put_ResultFilePath"},
		{dlArgsGetDeferral, 10, "DownloadStarting.GetDeferral"},
		{dlOpAddState, 7, "add_StateChanged"},
		{dlOpCancel, 18, "DownloadOperation.Cancel"},
		{istreamRead, 3, "IStream.Read"},
		{istreamSeek, 5, "IStream.Seek"},
		// Remote content.
		{env10CreateOptions, 20, "CreateCoreWebView2ControllerOptions"},
		{env10CreateWithOptions, 21, "CreateCoreWebView2ControllerWithOptions"},
		{ctlOptPutProfileName, 4, "put_ProfileName"},
		{ctlOptPutPrivate, 6, "put_IsInPrivateModeEnabled"},
		{set2PutUserAgent, 22, "put_UserAgent"},
		{wv14AddServerCert, 106, "add_ServerCertificateErrorDetected"},
		{certArgsGetStatus, 3, "get_ErrorStatus"},
		{certArgsGetUri, 4, "get_RequestUri"},
		{certArgsPutAction, 7, "put_Action"},
		{certArgsGetDeferral, 8, "GetDeferral"},
		{wv10AddBasicAuth, 97, "add_BasicAuthenticationRequested"},
		{basicArgsGetUri, 3, "get_Uri"},
		{basicArgsGetChallenge, 4, "get_Challenge"},
		{basicArgsGetResponse, 5, "get_Response"},
		{basicArgsPutCancel, 7, "put_Cancel"},
		{basicArgsGetDeferral, 8, "GetDeferral"},
		{basicRespPutUser, 4, "put_UserName"},
		{basicRespPutPassword, 6, "put_Password"},
		{reqPutUri, 4, "put_Uri"},
		{hdrSetHeader, 6, "SetHeader"},
		{argsGetResourceContext, 7, "get_ResourceContext"},
		{newWinArgsGetUri, 3, "get_Uri"},
	}
	for _, s := range slots {
		if s.got != s.want {
			t.Errorf("%s slot %d, want %d", s.name, s.got, s.want)
		}
	}
}

func TestBlockBufStreaming(t *testing.T) {
	b := newBlockBuf(8)
	var got bytes.Buffer
	done := make(chan error, 1)
	go func() {
		_, err := io.Copy(&got, readerOf{b})
		done <- err
	}()
	data := bytes.Repeat([]byte("0123456789"), 100) // far above the bound
	n, err := b.Write(data)
	if n != len(data) || err != nil {
		t.Fatalf("write %d %v", n, err)
	}
	b.Close()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got.Bytes(), data) {
		t.Fatalf("data mismatch: %d bytes", got.Len())
	}
}

type readerOf struct{ b *blockBuf }

func (r readerOf) Read(p []byte) (int, error) { return r.b.Read(p) }

func TestBlockBufReadBlocksUntilWrite(t *testing.T) {
	b := newBlockBuf(64)
	res := make(chan string, 1)
	go func() {
		p := make([]byte, 16)
		n, _ := b.Read(p)
		res <- string(p[:n])
	}()
	select {
	case <-res:
		t.Fatal("Read returned before any write")
	case <-time.After(50 * time.Millisecond):
	}
	if _, err := b.Write([]byte("hi")); err != nil {
		t.Fatal(err)
	}
	if s := <-res; s != "hi" {
		t.Fatalf("got %q", s)
	}
}

func TestBlockBufTryRead(t *testing.T) {
	b := newBlockBuf(64)
	p := make([]byte, 4)
	if _, err := b.TryRead(p); !errors.Is(err, errWouldBlock) {
		t.Fatalf("want would-block, got %v", err)
	}
	if _, err := b.Write([]byte("abcdef")); err != nil {
		t.Fatal(err)
	}
	if n, err := b.TryRead(p); n != 4 || err != nil {
		t.Fatalf("%d %v", n, err)
	}
	b.Close()
	if n, err := b.TryRead(p); n != 2 || err != nil {
		t.Fatalf("%d %v", n, err)
	}
	if _, err := b.TryRead(p); err != io.EOF {
		t.Fatalf("want EOF, got %v", err)
	}
}

func TestBlockBufCloseWithError(t *testing.T) {
	b := newBlockBuf(64)
	boom := errors.New("boom")
	if _, err := b.Write([]byte("x")); err != nil {
		t.Fatal(err)
	}
	b.CloseWithError(boom)
	p := make([]byte, 8)
	if n, err := b.Read(p); n != 1 || err != nil {
		t.Fatalf("%d %v", n, err)
	}
	if _, err := b.Read(p); err != boom {
		t.Fatalf("got %v", err)
	}
	if _, err := b.Write([]byte("y")); err == nil {
		t.Fatal("write after close succeeded")
	}
}

func TestBlockBufCancelUnblocksWriter(t *testing.T) {
	b := newBlockBuf(4)
	var fired atomic.Int32
	b.setOnCancel(func() { fired.Add(1) })
	werr := make(chan error, 1)
	go func() {
		_, err := b.Write(make([]byte, 100))
		werr <- err
	}()
	time.Sleep(30 * time.Millisecond)
	b.Cancel()
	b.Cancel()
	select {
	case err := <-werr:
		if err == nil {
			t.Fatal("writer not failed")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("writer still blocked after Cancel")
	}
	if fired.Load() != 1 {
		t.Fatalf("onCancel fired %d times", fired.Load())
	}
	if _, err := b.Read(make([]byte, 1)); err == nil {
		t.Fatal("read after cancel succeeded")
	}
}

func TestBlockBufPrimeIgnoresBound(t *testing.T) {
	b := newBlockBuf(2)
	b.prime([]byte("hello world"))
	p := make([]byte, 32)
	n, _ := b.Read(p)
	if string(p[:n]) != "hello world" {
		t.Fatalf("got %q", p[:n])
	}
}

func TestQueueCoalescesWakes(t *testing.T) {
	var q queue
	var wakes, ran atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 1000; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if q.push(func() { ran.Add(1) }) {
				wakes.Add(1)
			}
		}()
	}
	wg.Wait()
	if wakes.Load() != 1 {
		t.Fatalf("wakes = %d, want 1", wakes.Load())
	}
	q.drain()
	if ran.Load() != 1000 {
		t.Fatalf("ran = %d", ran.Load())
	}
	if !q.push(func() {}) {
		t.Fatal("push after drain must request a wake")
	}
}

func TestQueueWakeFailedAndPanic(t *testing.T) {
	var q queue
	if !q.push(func() {}) {
		t.Fatal("first push must wake")
	}
	q.wakeFailed()
	if !q.push(func() { panic("x") }) {
		t.Fatal("push after wakeFailed must wake again")
	}
	var after bool
	q.push(func() { after = true })
	q.drain() // must survive the panic and run the rest
	if !after {
		t.Fatal("function after panicking one did not run")
	}
}

func TestQueueFuncPushedDuringDrain(t *testing.T) {
	var q queue
	var order []int
	q.push(func() {
		order = append(order, 1)
		q.push(func() { order = append(order, 2) })
	})
	q.drain()
	if len(order) != 2 {
		t.Fatalf("order %v", order)
	}
}

func TestFormatHeaders(t *testing.T) {
	h := http.Header{}
	h.Set("Content-Type", "text/plain")
	h.Add("Set-Cookie", "a=1")
	h.Add("Set-Cookie", "b=2")
	h.Set("X-Evil", "v\r\nInjected: 1")
	h.Set("Transfer-Encoding", "chunked")
	h.Set("Connection", "close")
	got := formatHeaders(h)
	want := "Content-Type: text/plain\r\nSet-Cookie: a=1\r\nSet-Cookie: b=2\r\nX-Evil: v  Injected: 1\r\n"
	if got != want {
		t.Fatalf("got %q\nwant %q", got, want)
	}
	if formatHeaders(nil) != "" {
		t.Fatal("nil header must be empty")
	}
}

func TestReasonPhrase(t *testing.T) {
	if reasonPhrase(200) != "OK" || reasonPhrase(404) != "Not Found" {
		t.Fatal("standard phrases")
	}
	if reasonPhrase(599) == "" || reasonPhrase(299) == "" {
		t.Fatal("unknown codes need a phrase")
	}
}

func TestBuildRequest(t *testing.T) {
	r, err := buildRequest(context.Background(), "POST", "https://app.localhost/echo?x=1",
		[][2]string{{"Content-Type", "application/octet-stream"}, {"Host", "evil"}}, []byte("abc"))
	if err != nil {
		t.Fatal(err)
	}
	if r.Method != "POST" || r.URL.Path != "/echo" || r.URL.Query().Get("x") != "1" ||
		r.RequestURI != "/echo?x=1" || r.Host != "app.localhost" ||
		r.Header.Get("Content-Type") != "application/octet-stream" || r.ContentLength != 3 {
		t.Fatalf("bad request %+v", r)
	}
	b, _ := io.ReadAll(r.Body)
	if string(b) != "abc" {
		t.Fatalf("body %q", b)
	}
	g, err := buildRequest(context.Background(), "", "https://app.localhost/", nil, nil)
	if err != nil || g.Method != "GET" || g.Body != http.NoBody {
		t.Fatalf("%v %+v", err, g)
	}
	if _, err := buildRequest(context.Background(), "GET", "http://[bad", nil, nil); err == nil {
		t.Fatal("bad uri accepted")
	}
}

type captured struct {
	mu       sync.Mutex
	commits  int
	status   int
	header   http.Header
	stream   *blockBuf
	done     int
	body     []byte
	doneCode int
}

func newCaptured() (*captured, *responseWriter, func()) {
	c := &captured{}
	w := newResponseWriter(false,
		func(st int, h http.Header, s *blockBuf) {
			c.mu.Lock()
			c.commits++
			c.status, c.header, c.stream = st, h, s
			c.mu.Unlock()
		},
		func(st int, h http.Header, b []byte) {
			c.mu.Lock()
			c.done++
			c.doneCode, c.header, c.body = st, h, append([]byte(nil), b...)
			c.mu.Unlock()
		})
	return c, w, w.finish
}

func TestResponseWriterSmallWholeBody(t *testing.T) {
	c, w, fin := newCaptured()
	w.Header().Set("X-A", "1")
	w.WriteHeader(201)
	w.Header().Set("X-Late", "no") // after WriteHeader: not sent
	if _, err := io.WriteString(w, "hello"); err != nil {
		t.Fatal(err)
	}
	if c.commits != 0 || c.done != 0 {
		t.Fatal("small write must not commit")
	}
	fin()
	if c.done != 1 || c.commits != 0 || c.doneCode != 201 || string(c.body) != "hello" {
		t.Fatalf("%+v", c)
	}
	if c.header.Get("X-A") != "1" || c.header.Get("X-Late") != "" {
		t.Fatalf("headers %v", c.header)
	}
	if c.header.Get("Content-Type") != "text/plain; charset=utf-8" {
		t.Fatalf("sniffed type %q", c.header.Get("Content-Type"))
	}
}

func TestResponseWriterFlushStreams(t *testing.T) {
	c, w, fin := newCaptured()
	if _, err := io.WriteString(w, "one"); err != nil {
		t.Fatal(err)
	}
	w.Flush()
	if c.commits != 1 || c.stream == nil {
		t.Fatal("Flush must commit")
	}
	p := make([]byte, 16)
	if n, _ := c.stream.Read(p); string(p[:n]) != "one" {
		t.Fatalf("first chunk %q", p[:n])
	}
	if _, err := io.WriteString(w, "two"); err != nil {
		t.Fatal(err)
	}
	w.Flush()
	if n, _ := c.stream.Read(p); string(p[:n]) != "two" {
		t.Fatalf("second chunk %q", p[:n])
	}
	fin()
	if _, err := c.stream.Read(p); err != io.EOF {
		t.Fatalf("want EOF, got %v", err)
	}
	if c.commits != 1 || c.done != 0 {
		t.Fatalf("commits=%d done=%d", c.commits, c.done)
	}
}

func TestResponseWriterThresholdCommit(t *testing.T) {
	c, w, fin := newCaptured()
	if _, err := w.Write(make([]byte, commitThreshold-1)); err != nil {
		t.Fatal(err)
	}
	if c.commits != 0 {
		t.Fatal("below threshold")
	}
	if _, err := w.Write([]byte("xx")); err != nil {
		t.Fatal(err)
	}
	if c.commits != 1 {
		t.Fatal("threshold must commit")
	}
	fin()
}

func TestResponseWriterEmptyAndHead(t *testing.T) {
	c, _, fin := newCaptured()
	fin()
	if c.done != 1 || c.doneCode != 200 || len(c.body) != 0 {
		t.Fatalf("%+v", c)
	}
	c2 := &captured{}
	w := newResponseWriter(true,
		func(int, http.Header, *blockBuf) { c2.commits++ },
		func(st int, h http.Header, b []byte) { c2.done++; c2.body = b })
	if _, err := w.Write([]byte(strings.Repeat("x", 10000))); err != nil {
		t.Fatal(err)
	}
	w.finish()
	if c2.commits != 0 || c2.done != 1 || len(c2.body) != 0 {
		t.Fatalf("HEAD: %+v", c2)
	}
}

func TestResponseWriterAbort(t *testing.T) {
	c, w, _ := newCaptured()
	w.abort()
	if c.doneCode != 500 {
		t.Fatalf("%+v", c)
	}
	c, w, _ = newCaptured()
	w.Flush()
	w.abort()
	if _, err := c.stream.Read(make([]byte, 1)); err != io.ErrUnexpectedEOF {
		t.Fatalf("got %v", err)
	}
}

func TestResponseWriterWriteAfterCancel(t *testing.T) {
	c, w, _ := newCaptured()
	w.Flush()
	c.stream.Cancel()
	if _, err := w.Write([]byte("x")); err == nil {
		t.Fatal("write after reader canceled must fail")
	}
}

func TestURLHelpers(t *testing.T) {
	for in, want := range map[string]string{
		"": "https://app.localhost/", "/": "https://app.localhost/",
		"a/b": "https://app.localhost/a/b", "/x?y=1": "https://app.localhost/x?y=1",
	} {
		if got := navURL(in); got != want {
			t.Errorf("navURL(%q) = %q", in, got)
		}
	}
	ok := []string{"https://app.localhost/", "https://app.localhost/a?b#c"}
	bad := []string{
		"http://app.localhost/", "https://example.com/", "https://app.localhost.evil.com/",
		"https://app.localhost:8443/", "https://user@app.localhost/", "about:blank",
		"file:///c:/x", "javascript:alert(1)", "", "https://evil.com/?https://app.localhost/",
	}
	for _, s := range ok {
		if !allowedNavigation(s) {
			t.Errorf("%q must be allowed", s)
		}
	}
	for _, s := range bad {
		if allowedNavigation(s) {
			t.Errorf("%q must be blocked", s)
		}
	}
}

func TestUserDataDir(t *testing.T) {
	got := strings.ReplaceAll(userDataDir("L", "com.acme.app"), "\\", "/")
	if got != "L/com.acme.app/WebView2" {
		t.Fatalf("got %q", got)
	}
	for _, id := range []string{"", "  ", "..", `..\..\evil`, `a/b:c`} {
		d := strings.ReplaceAll(userDataDir("L", id), "\\", "/")
		parts := strings.Split(d, "/")
		if len(parts) != 3 || parts[0] != "L" || parts[2] != "WebView2" || parts[1] == ".." || parts[1] == "." || parts[1] == "" {
			t.Errorf("id %q -> %q", id, d)
		}
	}
}

func TestLoaderDLLCandidates(t *testing.T) {
	if loaderDLLCandidates("", "1.0", "x64") != nil {
		t.Fatal("empty location")
	}
	got := loaderDLLCandidates(`C:\Edge\Application`, "120.0.1", "x64")
	if len(got) != 2 || !strings.HasSuffix(got[0], "EmbeddedBrowserWebView.dll") ||
		!strings.Contains(got[1], "120.0.1") {
		t.Fatalf("got %v", got)
	}
	// location that already ends in the version is not doubled.
	got = loaderDLLCandidates("/e/a/120.0.1", "120.0.1", "arm64")
	if len(got) != 1 {
		t.Fatalf("got %v", got)
	}
}

func TestAllowedFrameNavigation(t *testing.T) {
	for _, s := range []string{"https://app.localhost/x", "about:blank", "about:srcdoc"} {
		if !allowedFrameNavigation(s) {
			t.Errorf("%q must be allowed in a frame", s)
		}
	}
	for _, s := range []string{"https://example.com/", "javascript:alert(1)", "data:text/html,x", "file:///c:/x", "http://app.localhost/", ""} {
		if allowedFrameNavigation(s) {
			t.Errorf("%q must be blocked in a frame", s)
		}
	}
}

func TestContextMenuVtableSlots(t *testing.T) {
	// Slots from the C++ declaration order in WebView2.h.
	for _, c := range []struct {
		got, want int
		name      string
	}{
		{cmArgsGetMenuItems, 3, "get_MenuItems"},
		{cmArgsGetTarget, 4, "get_ContextMenuTarget"},
		{cmArgsGetLocation, 5, "get_Location"},
		{cmArgsPutHandled, 8, "put_Handled"},
		{cmTargetGetKind, 3, "get_Kind"},
		{cmTargetGetIsEditable, 4, "get_IsEditable"},
		{cmTargetGetHasLinkUri, 8, "get_HasLinkUri"},
		{cmTargetGetLinkUri, 9, "get_LinkUri"},
		{cmTargetGetHasSourceUri, 12, "get_HasSourceUri"},
		{cmTargetGetSourceUri, 13, "get_SourceUri"},
		{cmTargetGetHasSelection, 14, "get_HasSelection"},
		{cmItemGetLabel, 4, "get_Label"},
		{cmItemGetIsChecked, 12, "get_IsChecked"},
		{wvAddContextMenuRequested, 100, "add_ContextMenuRequested"},
		{wvCallDevToolsProtocol, 36, "CallDevToolsProtocolMethod"},
		{cmTargetGetSelectionText, 15, "get_SelectionText"},
		{cmItemsGetCount, 3, "get_Count"},
		{cmItemsGetAt, 4, "GetValueAtIndex"},
		{cmItemsInsertAt, 6, "InsertValueAtIndex"},
		{cmItemGetName, 3, "get_Name"},
		{cmItemPutIsEnabled, 9, "put_IsEnabled"},
		{cmItemPutIsChecked, 11, "put_IsChecked"},
		{cmItemGetChildren, 13, "get_Children"},
		{cmItemAddCustomSelected, 14, "add_CustomItemSelected"},
		{env9CreateContextMenuItem, 19, "CreateContextMenuItem"},
	} {
		if c.got != c.want {
			t.Errorf("%s: slot %d, want %d", c.name, c.got, c.want)
		}
	}
	for _, s := range []string{iidWebView2_11, iidContextMenuRequestedHandler, iidCustomItemSelectedHandler, iidEnvironment9} {
		if _, err := parseGUID(s); err != nil {
			t.Errorf("%s: %v", s, err)
		}
	}
}
