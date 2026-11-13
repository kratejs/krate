//go:build windows

package windows

import (
	"context"
	"net/http"
	"unsafe"

	xw "golang.org/x/sys/windows"

	"github.com/kratejs/krate/packages/native/platform/caps"
)

// onNavigationStarting cancels top-level navigations away from the app
// origin. Runs on the main thread. args is borrowed.
func (a *App) onNavigationStarting(sender, args uintptr) uintptr {
	nav := ptr(args)
	if !allowedNavigation(comString(nav, navArgsGetUri)) {
		vcall(nav, navArgsPutCancel, 1)
	}
	return sOK
}

// onFrameNavigationStarting applies the same lock to iframes.
func (a *App) onFrameNavigationStarting(sender, args uintptr) uintptr {
	nav := ptr(args)
	if !allowedFrameNavigation(comString(nav, navArgsGetUri)) {
		vcall(nav, navArgsPutCancel, 1)
	}
	return sOK
}

// onNewWindowRequested refuses every window.open and target=_blank: without a
// handler WebView2 opens an unlocked popup, which could load any site. Runs on
// the main thread. args is borrowed.
func (a *App) onNewWindowRequested(sender, args uintptr) uintptr {
	vcall(ptr(args), newWinArgsPutHandled, 1)
	return sOK
}

// resourceHandlerFor returns the WebResourceRequested handler of one view.
// The request is tagged with that view's ref, so the handler's identity, not
// anything in the request, says where it came from.
func (a *App) resourceHandlerFor(ref caps.ViewRef) func(sender, args uintptr) uintptr {
	return func(sender, args uintptr) uintptr { return a.onResourceRequested(ref, sender, args) }
}

// onResourceRequested is the WebResourceRequested handler. It runs on the
// main thread: it copies the request out of COM, takes a deferral and hands
// the work to a goroutine. The response is installed back on the main thread.
func (a *App) onResourceRequested(view caps.ViewRef, sender, argsU uintptr) uintptr {
	args := ptr(argsU)
	var reqU uintptr
	if failed(vcall(args, argsGetRequest, uintptr(unsafe.Pointer(&reqU)))) || reqU == 0 {
		return sOK
	}
	req := ptr(reqU)
	defer comRelease(req)

	uri := comString(req, reqGetUri)
	method := comString(req, reqGetMethod)
	headers := readHeaders(req)
	body := readBody(req)

	var defU uintptr
	if failed(vcall(args, argsGetDeferral, uintptr(unsafe.Pointer(&defU)))) || defU == 0 {
		return sOK
	}
	comAddRef(args)
	go a.serve(view, args, ptr(defU), method, uri, headers, body)
	return sOK
}

// readHeaders walks ICoreWebView2HttpRequestHeaders with its iterator.
func readHeaders(req unsafe.Pointer) [][2]string {
	var hU uintptr
	if failed(vcall(req, reqGetHeaders, uintptr(unsafe.Pointer(&hU)))) || hU == 0 {
		return nil
	}
	hdrs := ptr(hU)
	defer comRelease(hdrs)
	var itU uintptr
	if failed(vcall(hdrs, hdrGetIterator, uintptr(unsafe.Pointer(&itU)))) || itU == 0 {
		return nil
	}
	it := ptr(itU)
	defer comRelease(it)

	var out [][2]string
	for {
		var has int32
		if failed(vcall(it, iterGetHasCurrentHeader, uintptr(unsafe.Pointer(&has)))) || has == 0 {
			return out
		}
		var nameP, valueP uintptr
		if !failed(vcall(it, iterGetCurrentHeader, uintptr(unsafe.Pointer(&nameP)), uintptr(unsafe.Pointer(&valueP)))) {
			out = append(out, [2]string{wideString(nameP), wideString(valueP)})
		}
		var next int32
		if failed(vcall(it, iterMoveNext, uintptr(unsafe.Pointer(&next)))) || next == 0 {
			return out
		}
	}
}

// readBody reads the request body (null for GET) from its IStream.
func readBody(req unsafe.Pointer) []byte {
	var sU uintptr
	if failed(vcall(req, reqGetContent, uintptr(unsafe.Pointer(&sU)))) || sU == 0 {
		return nil
	}
	s := ptr(sU)
	defer comRelease(s)
	var body []byte
	chunk := make([]byte, 64<<10)
	for {
		var n uint32
		hr := vcall(s, streamReadSlot, uintptr(unsafe.Pointer(&chunk[0])), uintptr(len(chunk)), uintptr(unsafe.Pointer(&n)))
		body = append(body, chunk[:n]...)
		if failed(hr) || n == 0 {
			return body
		}
	}
}

// serve runs the http.Handler off the main thread.
func (a *App) serve(view caps.ViewRef, args, def unsafe.Pointer, method, uri string, headers [][2]string, body []byte) {
	ctx, cancel := context.WithCancel(caps.WithView(context.Background(), view))
	defer cancel()

	req, err := buildRequest(ctx, method, uri, headers, body)
	if err != nil {
		a.Dispatch(func() {
			a.respond(args, def, http.StatusBadRequest,
				http.Header{"Content-Type": {"text/plain; charset=utf-8"}}, nil, []byte("bad request\n"))
		})
		return
	}
	rw := newResponseWriter(method == http.MethodHead,
		func(status int, h http.Header, s *blockBuf) {
			s.setOnCancel(cancel) // the webview dropped the stream: cancel the request context
			a.Dispatch(func() { a.respond(args, def, status, h, s, nil) })
		},
		func(status int, h http.Header, b []byte) {
			a.Dispatch(func() { a.respond(args, def, status, h, nil, b) })
		})
	rw.whole = true // streaming GETs take the web message route (msgstream.go)
	func() {
		defer func() {
			if r := recover(); r != nil {
				logf("handler panic for %s %s: %v", method, uri, r)
				rw.abort()
			}
		}()
		a.cfg.Handler.ServeHTTP(rw, req)
		rw.finish()
	}()
}

// respond installs the response and completes the deferral. Main thread.
// Exactly one of stream and body carries content.
func (a *App) respond(args, def unsafe.Pointer, status int, h http.Header, stream *blockBuf, body []byte) {
	defer comRelease(args)
	defer comRelease(def)
	defer vcall(def, deferralComplete)

	var content unsafe.Pointer
	switch {
	case stream != nil:
		content = newGoStream(a, stream)
	case len(body) > 0:
		content = shCreateMemStream(body)
		if content == nil {
			b := newBlockBuf(len(body))
			b.prime(body)
			b.Close()
			content = newGoStream(a, b)
		}
	}

	var resp uintptr
	hr := vcall(a.env, envCreateResponse, uintptr(content), uintptr(status),
		uintptr(unsafe.Pointer(wstr(reasonPhrase(status)))),
		uintptr(unsafe.Pointer(wstr(formatHeaders(h)))),
		uintptr(unsafe.Pointer(&resp)))
	comRelease(content) // CreateWebResourceResponse took its own reference
	if failed(hr) || resp == 0 {
		logf("CreateWebResourceResponse failed: 0x%08x", uint32(hr))
		return
	}
	vcall(args, argsPutResponse, resp)
	comRelease(ptr(resp))
}

// wideString reads and frees a CoTaskMem LPWSTR.
func wideString(p uintptr) string {
	if p == 0 {
		return ""
	}
	s := xw.UTF16PtrToString((*uint16)(ptr(p)))
	xw.CoTaskMemFree(ptr(p))
	return s
}
