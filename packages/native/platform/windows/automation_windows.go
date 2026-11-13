//go:build windows

package windows

import (
	"errors"
	"fmt"
	"sync"
	"time"
	"unsafe"

	xw "golang.org/x/sys/windows"

	"github.com/kratejs/krate/packages/native/platform/caps"
)

// Remote debugging and DevTools protocol commands. WebView2 is Chromium:
// --remote-debugging-port makes the runtime serve the Chrome DevTools
// Protocol over HTTP and WebSocket on loopback, which Playwright
// (chromium.connectOverCDP) and Puppeteer attach to, and
// CallDevToolsProtocolMethod runs one method in process.
//
// Not run on a real Windows machine by the author: the CI native job runs
// TestGUICDP.

var (
	_ caps.Automation       = (*App)(nil)
	_ caps.DevToolsProtocol = (*App)(nil)
)

// remoteArguments returns the browser arguments that start the endpoint.
func remoteArguments(r caps.RemoteDebug) string {
	if !r.Enabled {
		return ""
	}
	return fmt.Sprintf("--remote-debugging-port=%d", remotePort(r))
}

func remotePort(r caps.RemoteDebug) int {
	if r.Port == 0 {
		return 9222
	}
	return r.Port
}

// AutomationEndpoint reports the CDP endpoint, if remote debugging is on.
func (a *App) AutomationEndpoint() caps.Endpoint {
	if !a.cfg.Remote.Enabled {
		return caps.Endpoint{}
	}
	p := remotePort(a.cfg.Remote)
	return caps.Endpoint{Kind: "cdp", URL: fmt.Sprintf("http://127.0.0.1:%d", p), Port: p}
}

// takeString reads and frees an LPWSTR out value.
func takeString(p uintptr) string {
	if p == 0 {
		return ""
	}
	s := xw.UTF16PtrToString((*uint16)(ptr(p)))
	xw.CoTaskMemFree(ptr(p))
	return s
}

// CDP runs one DevTools protocol method in a view.
func (a *App) CDP(ref caps.ViewRef, method, paramsJSON string) (string, error) {
	type result struct {
		json string
		err  error
	}
	done := make(chan result, 1)
	var startErr error
	a.callMain(func() {
		v := a.findView(ref)
		switch {
		case v == nil:
			startErr = fmt.Errorf("windows: view %d does not exist", ref)
			return
		case v.w.released || v.w.destroyed:
			startErr = errReleased
			return
		}
		if paramsJSON == "" {
			paramsJSON = "{}"
		}
		v.do(func() {
			h := newHandler(iidCallCDPHandler, true, func(hr, res uintptr) uintptr {
				// The result string belongs to the runtime for the call.
				if failed(hr) {
					done <- result{err: fmt.Errorf("windows: %s failed: 0x%08x", method, uint32(hr))}
					return sOK
				}
				done <- result{json: xw.UTF16PtrToString((*uint16)(ptr(res)))}
				return sOK
			})
			if hr := vcall(v.webview, wvCallCDP, uintptr(unsafe.Pointer(wstr(method))), uintptr(unsafe.Pointer(wstr(paramsJSON))), uintptr(h)); failed(hr) {
				done <- result{err: fmt.Errorf("windows: CallDevToolsProtocolMethod %s: 0x%08x", method, uint32(hr))}
			}
		})
	})
	if startErr != nil {
		return "", startErr
	}
	select {
	case r := <-done:
		return r.json, r.err
	case <-time.After(30 * time.Second):
		return "", errors.New("windows: the DevTools protocol call did not answer within 30 seconds")
	}
}

// OnCDPEvent subscribes to a protocol event of a view.
func (a *App) OnCDPEvent(ref caps.ViewRef, event string, fn func(string)) (func(), error) {
	var (
		cancelMu sync.Mutex
		cancel   = func() {}
		subErr   error
		ready    = make(chan struct{})
	)
	a.callMain(func() {
		v := a.findView(ref)
		if v == nil {
			subErr = fmt.Errorf("windows: view %d does not exist", ref)
			close(ready)
			return
		}
		v.do(func() {
			defer close(ready)
			var recv uintptr
			if hr := vcall(v.webview, wvGetCDPEventReceiver, uintptr(unsafe.Pointer(wstr(event))), uintptr(unsafe.Pointer(&recv))); failed(hr) || recv == 0 {
				subErr = fmt.Errorf("windows: GetDevToolsProtocolEventReceiver %s: 0x%08x", event, uint32(hr))
				return
			}
			h := newHandler(iidCDPEventHandler, false, func(_, args uintptr) uintptr {
				var p uintptr
				if !failed(vcall(ptr(args), cdpArgsGetParams, uintptr(unsafe.Pointer(&p)))) {
					fn(takeString(p))
				}
				return sOK
			})
			var token int64
			if hr := vcall(ptr(recv), cdpRecvAdd, uintptr(h), uintptr(unsafe.Pointer(&token))); failed(hr) {
				comRelease(h)
				comRelease(ptr(recv))
				subErr = fmt.Errorf("windows: add_DevToolsProtocolEventReceived %s: 0x%08x", event, uint32(hr))
				return
			}
			cancelMu.Lock()
			cancel = func() {
				a.Dispatch(func() {
					vcall(ptr(recv), cdpRecvRemove, uintptr(token))
					comRelease(h)
					comRelease(ptr(recv))
				})
			}
			cancelMu.Unlock()
		})
	})
	select {
	case <-ready:
	case <-time.After(10 * time.Second):
		return nil, errors.New("windows: subscribing to the protocol event timed out")
	}
	if subErr != nil {
		return nil, subErr
	}
	return func() {
		cancelMu.Lock()
		c := cancel
		cancelMu.Unlock()
		c()
	}, nil
}
