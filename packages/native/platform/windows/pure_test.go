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
		iidWebResourceRequestedHandler, iidNavigationStartingHandler, iidNewWindowRequestedHandler, iidExecuteScriptHandler,
	} {
		if _, err := parseGUID(s); err != nil {
			t.Errorf("%s: %v", s, err)
		}
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
		"IStream": 14,
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
		{wvAddNavigationStarting, 7, "add_NavigationStarting"},
		{wvExecuteScript, 29, "ExecuteScript"},
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
	b.Write([]byte("hi"))
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
	b.Write([]byte("abcdef"))
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
	b.Write([]byte("x"))
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
	io.WriteString(w, "hello")
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
	io.WriteString(w, "one")
	w.Flush()
	if c.commits != 1 || c.stream == nil {
		t.Fatal("Flush must commit")
	}
	p := make([]byte, 16)
	if n, _ := c.stream.Read(p); string(p[:n]) != "one" {
		t.Fatalf("first chunk %q", p[:n])
	}
	io.WriteString(w, "two")
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
	w.Write(make([]byte, commitThreshold-1))
	if c.commits != 0 {
		t.Fatal("below threshold")
	}
	w.Write([]byte("xx"))
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
	w.Write([]byte(strings.Repeat("x", 10000)))
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
