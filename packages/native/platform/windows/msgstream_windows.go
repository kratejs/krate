//go:build windows

package windows

import (
	"context"
	"strings"
	"unsafe"

	"github.com/kratejs/krate/packages/native/platform/caps"
)

// setupStreams connects the web message channel of a view and injects the
// fetch wrapper. Main thread, before the first navigation.
func (v *view) setupStreams() {
	a := v.w.app
	v.streams.reopen()
	v.hMsg = newHandler(iidWebMessageHandler, false, func(sender, args uintptr) uintptr {
		return a.onWebMessage(v, args)
	})
	var token int64
	if hr := vcall(v.webview, wvAddWebMessageReceived, uintptr(v.hMsg), uintptr(unsafe.Pointer(&token))); failed(hr) {
		logf("add_WebMessageReceived failed (0x%08x); streaming responses will not reach the page", uint32(hr))
		return
	}
	vcall(v.webview, wvAddScriptOnDocCreated, uintptr(unsafe.Pointer(wstr(streamShim))), 0)
}

// onWebMessage handles a message from the page. Main thread; args is borrowed.
func (a *App) onWebMessage(v *view, args uintptr) uintptr {
	nargs := ptr(args)
	if src := comString(nargs, msgArgsGetSource); src != Origin && !strings.HasPrefix(src, Origin+"/") {
		return sOK
	}
	text := comString(nargs, msgArgsTryGetString)
	m, err := parseStreamMsg(text)
	if err != nil {
		logf("ignoring a web message: %v", err)
		return sOK
	}
	switch m.K {
	case "cancel":
		v.streams.cancel(m.ID)
	case "open":
		ctx, cancel := context.WithCancel(caps.WithView(context.Background(), v.ref))
		if !v.streams.add(m.ID, cancel) {
			cancel()
			v.post(encodeMsg(map[string]any{"k": "err", "id": m.ID, "e": "too many open streams"}))
			return sOK
		}
		go func() {
			defer cancel()
			defer v.streams.remove(m.ID)
			runStream(ctx, a.cfg.Handler, m.ID, m.P, m.H, v.post)
		}()
	}
	return sOK
}

// post sends a message to the page. Safe from any goroutine; messages keep
// their order.
func (v *view) post(msg string) {
	v.w.app.Dispatch(func() {
		if v.webview == nil {
			return
		}
		vcall(v.webview, wvPostWebMessageAsString, uintptr(unsafe.Pointer(wstr(msg))))
	})
}
