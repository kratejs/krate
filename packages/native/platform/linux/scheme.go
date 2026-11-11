//go:build linux

package linux

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"unsafe"

	"github.com/kratejs/krate/packages/native/platform/caps"
)

// schemeCallback runs on the main thread for each krate:// request. It only
// copies what it needs and starts a goroutine; the handler never runs here.
func schemeCallback(request, data uintptr) uintptr {
	a := theApp
	api := a.api
	uri := goString(api.wkReqURI(request))
	method := goString(api.wkReqMethod(request))
	if method == "" {
		method = http.MethodGet
	}
	hdr := http.Header{}
	if h := api.wkReqHeaders(request); h != 0 {
		curHeader = hdr
		api.soupHeadersForeach(h, cbHeader, 0)
		curHeader = nil
	}
	// webkit_uri_scheme_request_get_http_body returns a new reference. The
	// stream is read later from the handler goroutine with the synchronous
	// g_input_stream_read; WebKit hands out memory-backed streams for which
	// that is safe off the main thread.
	var body *streamBody
	if s := api.wkReqBody(request); s != 0 {
		body = &streamBody{api: api, stream: s}
	}
	api.gObjectRef(request)

	resp := &schemeResponder{app: a, req: request}
	u, err := url.Parse(uri)
	if err != nil || u.Scheme != "krate" || u.Host != "app" {
		if body != nil {
			body.Close()
		}
		resp.fail("bad request URI")
		return 0
	}
	go runRequest(a.handler, a.viewOfRequest(request), method, u, hdr, body, resp)
	return 0
}

func runRequest(h http.Handler, view caps.ViewRef, method string, u *url.URL, hdr http.Header, body *streamBody, resp *schemeResponder) {
	ctx, cancel := context.WithCancel(caps.WithView(context.Background(), view))
	var rb io.ReadCloser = http.NoBody
	if body != nil {
		rb = body
	}
	req, err := http.NewRequestWithContext(ctx, method, u.String(), rb)
	if err != nil {
		cancel()
		if body != nil {
			body.Close()
		}
		resp.fail(err.Error())
		return
	}
	req.Header = hdr
	req.Host = u.Host
	req.RequestURI = u.RequestURI()
	req.RemoteAddr = "krate-app"
	req.ContentLength = -1
	if body == nil {
		req.ContentLength = 0
	} else if n, err := strconv.ParseInt(hdr.Get("Content-Length"), 10, 64); err == nil {
		req.ContentLength = n
	}
	serve(h, req, cancel, resp)
	if body != nil {
		body.Close()
	}
}

// schemeResponder finishes a WebKitURISchemeRequest on the main thread.
type schemeResponder struct {
	app *App
	req uintptr
}

func (r *schemeResponder) respond(status int, header http.Header, readFD int) {
	a := r.app
	a.Dispatch(func() {
		api := a.api
		stream := api.gUnixInputStreamNew(int32(readFD), 1)
		resp := api.wkRespNew(stream, -1)
		api.gObjectUnref(stream)
		api.wkRespSetStatus(resp, uint32(status), 0)
		hdrs := api.soupHeadersNew(1) // SOUP_MESSAGE_HEADERS_RESPONSE
		if ct := header.Get("Content-Type"); ct != "" {
			api.wkRespSetCType(resp, splitContentType(ct))
			api.soupHeadersAppend(hdrs, "Content-Type", ct)
		}
		for name, vals := range header {
			if skipResponseHeader(name) {
				continue
			}
			for _, v := range vals {
				api.soupHeadersAppend(hdrs, name, v)
			}
		}
		api.wkRespSetHeaders(resp, hdrs) // takes ownership of hdrs
		api.wkReqFinish(r.req, resp)
		api.gObjectUnref(resp)
		api.gObjectUnref(r.req)
	})
}

func (r *schemeResponder) fail(msg string) {
	a := r.app
	a.Dispatch(func() {
		api := a.api
		gerr := api.gErrorNewLiteral(api.gQuarkFromString("krate-scheme-error"), 1, msg)
		api.wkReqFinishError(r.req, gerr)
		api.gErrorFree(gerr)
		api.gObjectUnref(r.req)
	})
}

// headerCallback receives each header from soup_message_headers_foreach.
func headerCallback(name, value, data uintptr) uintptr {
	if curHeader != nil {
		curHeader.Add(goString(name), goString(value))
	}
	return 0
}

// streamBody reads a GInputStream as an io.ReadCloser.
type streamBody struct {
	api    *api
	stream uintptr
	once   sync.Once
}

func (b *streamBody) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if b.stream == 0 {
		return 0, io.EOF
	}
	var gerr uintptr
	n := b.api.gInputStreamRead(b.stream, unsafe.Pointer(&p[0]), uint64(len(p)), 0, &gerr)
	switch {
	case n < 0:
		msg := gerrorMessage(gerr)
		b.api.gErrorFree(gerr)
		return 0, errors.New("krate: reading request body: " + msg)
	case n == 0:
		return 0, io.EOF
	}
	return int(n), nil
}

func (b *streamBody) Close() error {
	b.once.Do(func() {
		var gerr uintptr
		b.api.gInputStreamClose(b.stream, 0, &gerr)
		if gerr != 0 {
			b.api.gErrorFree(gerr)
		}
		b.api.gObjectUnref(b.stream)
	})
	return nil
}

// gerrorMessage reads GError.message (after the domain and code fields).
func gerrorMessage(e uintptr) string {
	if e == 0 {
		return ""
	}
	base := *(*unsafe.Pointer)(unsafe.Pointer(&e))
	return goString(*(*uintptr)(unsafe.Add(base, 8)))
}
