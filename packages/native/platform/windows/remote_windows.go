//go:build windows

package windows

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"unsafe"

	"github.com/kratejs/krate/packages/native/krate"
	"github.com/kratejs/krate/packages/native/platform/caps"
)

// Remote content on WebView2. See platform/caps/remote.go for the model.
//
// Engine isolation: a remote view is a controller created with its own
// profile (ICoreWebView2ControllerOptions.ProfileName), so its cookies,
// storage and cache are not the app's. The app's origin is served by a
// WebResourceRequested handler that only app views have; a remote view has no
// such handler and none of the app's scripts, and its web messages go to a
// handler of its own that checks a token. Compiled only, until the CI native
// job runs gui_remote_test.go on windows-latest.

var (
	_ caps.RemoteContent = (*App)(nil)
	_ caps.RemoteWindows = (*App)(nil)
	_ caps.RemoteViews   = (*Window)(nil)
)

// remoteView is the remote state of a view. Main thread only.
type remoteView struct {
	policy *caps.RemotePolicy
	part   *partition
	token  string
	cur    string
}

func (rv *remoteView) restoreURL() string {
	if rv.cur != "" {
		return rv.cur
	}
	return rv.policy.Start
}

type partition struct {
	p     caps.Partition
	ua    string
	rules []caps.RequestRule
}

type remoteState struct {
	mu          sync.Mutex
	parts       map[string]*partition
	refs        map[caps.ViewRef]bool
	handler     func(caps.RemoteEvent)
	certHandler func(caps.CertError, func(bool))
	authHandler func(caps.AuthRequest, func(string, string, bool))
}

type remoteSpec struct {
	policy *caps.RemotePolicy
}

func (a *App) rstate() *remoteState {
	a.remMu.Lock()
	defer a.remMu.Unlock()
	if a.rem == nil {
		a.rem = &remoteState{parts: map[string]*partition{}, refs: map[caps.ViewRef]bool{}}
	}
	return a.rem
}

// Values of the WebView2 enums used below.
const (
	resourceContextDocument = 1 // COREWEBVIEW2_WEB_RESOURCE_CONTEXT_DOCUMENT
	certActionAllow         = 0 // COREWEBVIEW2_SERVER_CERTIFICATE_ERROR_ACTION_ALWAYS_ALLOW
	certActionCancel        = 1
)

// RemoteCapabilities reports what WebView2 can do. Everything is a
// WebResourceRequested rule on the remote view, so all requests are covered.
// A proxy is a browser argument of the whole environment and a client
// certificate has to come from the Windows certificate store, so neither is
// offered per partition.
func (a *App) RemoteCapabilities() caps.RemoteCaps {
	return caps.RemoteCaps{
		Observe: caps.LevelAll, Block: caps.LevelAll, Redirect: caps.LevelAll, RequestHeaders: caps.LevelAll,
		UserAgent: true, CertificateErrors: true, Auth: true, InMemory: true,
	}
}

// IsRemoteView reports whether a view shows remote content. Any goroutine.
func (a *App) IsRemoteView(ref caps.ViewRef) bool {
	rs := a.rstate()
	rs.mu.Lock()
	defer rs.mu.Unlock()
	return rs.refs[ref]
}

func (a *App) partitionFor(p caps.Partition) *partition {
	rs := a.rstate()
	rs.mu.Lock()
	defer rs.mu.Unlock()
	if pt := rs.parts[p.Key()]; pt != nil {
		return pt
	}
	pt := &partition{p: p}
	rs.parts[p.Key()] = pt
	return pt
}

// makeRemote marks a freshly registered view as remote.
func (a *App) makeRemote(v *view, rs *remoteSpec) {
	v.rv = &remoteView{policy: rs.policy, part: a.partitionFor(rs.policy.Partition)}
	if rs.policy.Messages {
		v.rv.token = caps.NewRemoteToken()
	}
	st := a.rstate()
	st.mu.Lock()
	st.refs[v.ref] = true
	st.mu.Unlock()
}

func (a *App) emitRemote(ev caps.RemoteEvent) {
	st := a.rstate()
	st.mu.Lock()
	fn := st.handler
	st.mu.Unlock()
	if fn != nil {
		fn(ev)
	}
}

// OnRemoteEvent sets the handler for remote events.
func (a *App) OnRemoteEvent(fn func(caps.RemoteEvent)) {
	st := a.rstate()
	st.mu.Lock()
	st.handler = fn
	st.mu.Unlock()
}

// NewRemoteWindow opens a window whose primary view shows remote content.
func (a *App) NewRemoteWindow(spec caps.RemoteWindowSpec) (caps.RemoteWindow, error) {
	pol, err := spec.Remote.Resolve()
	if err != nil {
		return nil, err
	}
	w, err := a.NewWindow(WindowConfig{Title: spec.Title, Width: spec.Width, Height: spec.Height, Resizable: spec.Resizable,
		Chrome: spec.Chrome, remote: &remoteSpec{policy: pol}})
	if err != nil {
		return nil, err
	}
	return w, nil
}

// AddRemoteView adds a remote view to the window.
func (w *Window) AddRemoteView(o caps.ViewOptions, r caps.RemoteOptions) (caps.ViewRef, error) {
	pol, err := r.Resolve()
	if err != nil {
		return 0, err
	}
	return w.addViewWait(o, &remoteSpec{policy: pol})
}

// createRemoteController creates the controller of a remote view in its own
// profile.
func (v *view) createRemoteController() {
	w := v.w
	a := w.app
	a.withEnv(func() {
		if w.destroyed {
			v.finish(errors.New("windows: window closed before the webview was created"))
			return
		}
		env10 := queryInterface(a.env, iidWV2Environment10)
		if env10 == nil {
			v.fail(fmt.Errorf("%w: remote content needs a newer WebView2 runtime (profiles, ICoreWebView2Environment10)", krate.ErrUnsupported))
			return
		}
		defer comRelease(env10)
		var optsU uintptr
		if failed(vcall(env10, env10CreateOptions, uintptr(unsafe.Pointer(&optsU)))) || optsU == 0 {
			v.fail(errors.New("windows: CreateCoreWebView2ControllerOptions failed"))
			return
		}
		opts := ptr(optsU)
		defer comRelease(opts)
		part := v.rv.part.p
		vcall(opts, ctlOptPutProfileName, uintptr(unsafe.Pointer(wstr(profileName(part)))))
		vcall(opts, ctlOptPutPrivate, uintptr(b2u(!part.Persistent)))
		h := newHandler(iidControllerCompletedHandler, true, func(hr, controller uintptr) uintptr {
			v.controllerCompleted(hr, controller)
			return sOK
		})
		if hr := vcall(env10, env10CreateWithOptions, w.hwnd, optsU, uintptr(h)); failed(hr) {
			v.fail(fmt.Errorf("windows: CreateCoreWebView2ControllerWithOptions: 0x%08x", uint32(hr)))
		}
	})
}

// setupRemote wires a remote view's webview. It deliberately installs none of
// the app's handlers: no origin handler, no message streams, no file drops,
// no permission routing to the app. Main thread.
func (a *App) setupRemote(v *view) {
	rv := v.rv
	var token int64
	// Settings: developer tools as for the app, and the user agent.
	var settings uintptr
	if !failed(vcall(v.webview, wvGetSettings, uintptr(unsafe.Pointer(&settings)))) && settings != 0 {
		s := ptr(settings)
		if a.cfg.Debug {
			vcall(s, setPutAreDevToolsEnabled, 1)
		}
		a.applyRemoteUA(v, s)
		comRelease(s)
	}
	// Navigation: every top-level navigation, frame navigation and popup goes
	// through RemotePolicy.Decide.
	hNav := newHandler(iidNavigationStartingHandler, false, func(sender, args uintptr) uintptr {
		return a.remoteNavigation(v, args, false)
	})
	vcall(v.webview, wvAddNavigationStarting, uintptr(hNav), uintptr(unsafe.Pointer(&token)))
	comRelease(hNav) // the webview holds it now
	hFrame := newHandler(iidNavigationStartingHandler, false, func(sender, args uintptr) uintptr {
		return a.remoteNavigation(v, args, true)
	})
	vcall(v.webview, wvAddFrameNavigationStarting, uintptr(hFrame), uintptr(unsafe.Pointer(&token)))
	comRelease(hFrame)
	hWin := newHandler(iidNewWindowRequestedHandler, false, func(sender, args uintptr) uintptr {
		return a.remoteNewWindow(v, args)
	})
	vcall(v.webview, wvAddNewWindowRequested, uintptr(hWin), uintptr(unsafe.Pointer(&token)))
	comRelease(hWin)
	v.hNav = newHandler(iidNavigationCompletedHandler, false, func(sender, args uintptr) uintptr {
		rv.cur = comString(v.webview, wvGetSource)
		a.emitRemote(caps.RemoteEvent{View: v.ref, Kind: caps.RemoteLoaded, URL: rv.cur, Origin: caps.OriginOfString(rv.cur)})
		emitView(a, v, caps.ViewNavigated, rv.cur)
		return sOK
	})
	vcall(v.webview, wvAddNavigationCompleted, uintptr(v.hNav), uintptr(unsafe.Pointer(&token)))
	v.hClose = newHandler(iidWindowCloseRequestedHandler, false, func(sender, args uintptr) uintptr {
		if v.primary {
			_, _, _ = pPostMessageW.Call(v.w.hwnd, wmClose, 0, 0)
		} else {
			_ = v.w.removeView(v.ref, caps.ViewClosed)
		}
		return sOK
	})
	vcall(v.webview, wvAddWindowCloseRequested, uintptr(v.hClose), uintptr(unsafe.Pointer(&token)))

	// Requests: rules and observation for every request of the view.
	v.hRes = newHandler(iidWebResourceRequestedHandler, false, func(sender, args uintptr) uintptr {
		return a.remoteResource(v, args)
	})
	vcall(v.webview, wvAddWebResourceRequested, uintptr(v.hRes), uintptr(unsafe.Pointer(&token)))
	vcall(v.webview, wvAddWebResourceFilter, uintptr(unsafe.Pointer(wstr("*"))), resourceContextAll)

	// Permissions are always denied and downloads cancelled.
	v.hPerm = newHandler(iidPermissionRequestedHandler, false, func(sender, args uintptr) uintptr {
		vcall(ptr(args), permArgsPutState, permStateDeny)
		return sOK
	})
	vcall(v.webview, wvAddPermissionRequested, uintptr(v.hPerm), uintptr(unsafe.Pointer(&token)))
	if wv4 := queryInterface(v.webview, iidWV2_4); wv4 != nil {
		h := newHandler(iidWV2DownloadStartingEventHandler, false, func(sender, args uintptr) uintptr {
			vcall(ptr(args), dlArgsPutCancel, 1)
			vcall(ptr(args), dlArgsPutHandled, 1)
			return sOK
		})
		vcall(wv4, wv4AddDownloadStart, uintptr(h), uintptr(unsafe.Pointer(&token)))
		comRelease(h)
		comRelease(wv4)
	}

	// Certificate errors and logins.
	if wv14 := queryInterface(v.webview, iidWV2_14); wv14 != nil {
		h := newHandler(iidWV2ServerCertHandler, false, func(sender, args uintptr) uintptr {
			return a.remoteCertError(v, args)
		})
		vcall(wv14, wv14AddServerCert, uintptr(h), uintptr(unsafe.Pointer(&token)))
		comRelease(h)
		comRelease(wv14)
	}
	if wv10 := queryInterface(v.webview, iidWV2_10); wv10 != nil {
		h := newHandler(iidWV2BasicAuthHandler, false, func(sender, args uintptr) uintptr {
			return a.remoteBasicAuth(v, args)
		})
		vcall(wv10, wv10AddBasicAuth, uintptr(h), uintptr(unsafe.Pointer(&token)))
		comRelease(h)
		comRelease(wv10)
	}

	// The message channel, only if the view asked for it. Its handler checks
	// the token; the app's own message handler is never installed here.
	if rv.token != "" {
		src := caps.RemotePageScript("function(m){window.chrome.webview.postMessage(m)}", rv.token)
		vcall(v.webview, wvAddScriptOnDocCreated, uintptr(unsafe.Pointer(wstr(src))), 0)
		v.hMsg = newHandler(iidWebMessageHandler, false, func(sender, args uintptr) uintptr {
			return a.remoteMessage(v, args)
		})
		vcall(v.webview, wvAddWebMessageReceived, uintptr(v.hMsg), uintptr(unsafe.Pointer(&token)))
	}
	a.connectProcessFailed(v)
}

func (a *App) applyRemoteUA(v *view, settings unsafe.Pointer) {
	ua := v.rv.policy.UserAgent
	if ua == "" {
		ua = v.rv.part.ua
	}
	if s2 := queryInterface(settings, iidWV2Settings2); s2 != nil {
		vcall(s2, set2PutUserAgent, uintptr(unsafe.Pointer(wstr(ua))))
		comRelease(s2)
	}
}

// remoteNavigation is the navigation lock of remote views. args is borrowed.
func (a *App) remoteNavigation(v *view, argsU uintptr, frame bool) uintptr {
	args := ptr(argsU)
	uri := comString(args, navArgsGetUri)
	if frame {
		if !frameNavAllowed(v.rv.policy, uri) {
			vcall(args, navArgsPutCancel, 1)
			a.emitRemote(caps.RemoteEvent{View: v.ref, Kind: caps.RemoteNavBlocked, URL: uri, Verdict: caps.NavBlock})
		}
		return sOK
	}
	verdict := v.rv.policy.Decide(uri)
	if verdict != caps.NavAllow {
		vcall(args, navArgsPutCancel, 1)
		a.emitRemote(caps.RemoteEvent{View: v.ref, Kind: caps.RemoteNavBlocked, URL: uri, Verdict: verdict})
		return sOK
	}
	if res := caps.EvalRules(v.rv.part.rules, uri); res.Block {
		vcall(args, navArgsPutCancel, 1)
		a.emitRemote(caps.RemoteEvent{View: v.ref, Kind: caps.RemoteRequest, URL: uri, Method: "GET", Action: "block"})
	} else if res.Redirect != "" {
		vcall(args, navArgsPutCancel, 1)
		a.emitRemote(caps.RemoteEvent{View: v.ref, Kind: caps.RemoteRequest, URL: uri, Method: "GET", Action: "redirect"})
		target := res.Redirect
		a.Dispatch(func() {
			if v.webview != nil {
				vcall(v.webview, wvNavigate, uintptr(unsafe.Pointer(wstr(target))))
			}
		})
	}
	return sOK
}

// remoteNewWindow handles window.open and target=_blank: no second webview is
// made, and a URL the policy allows loads in the view itself.
func (a *App) remoteNewWindow(v *view, argsU uintptr) uintptr {
	args := ptr(argsU)
	vcall(args, newWinArgsPutHandled, 1)
	uri := comString(args, newWinArgsGetUri)
	verdict := v.rv.policy.Decide(uri)
	if verdict != caps.NavAllow || uri == "about:blank" {
		a.emitRemote(caps.RemoteEvent{View: v.ref, Kind: caps.RemoteNavBlocked, URL: uri, Verdict: verdict})
		return sOK
	}
	a.Dispatch(func() {
		if v.webview != nil {
			vcall(v.webview, wvNavigate, uintptr(unsafe.Pointer(wstr(uri))))
		}
	})
	return sOK
}

// remoteResource applies the partition's rules to one request of a remote view
// and reports it. args is borrowed.
func (a *App) remoteResource(v *view, argsU uintptr) uintptr {
	args := ptr(argsU)
	var reqU uintptr
	if failed(vcall(args, argsGetRequest, uintptr(unsafe.Pointer(&reqU)))) || reqU == 0 {
		return sOK
	}
	req := ptr(reqU)
	defer comRelease(req)
	uri := comString(req, reqGetUri)
	method := comString(req, reqGetMethod)
	res := caps.EvalRules(v.rv.part.rules, uri)
	action := ""
	switch {
	case res.Block:
		action = "block"
		var resp uintptr
		hr := vcall(a.env, envCreateResponse, 0, 403, uintptr(unsafe.Pointer(wstr("Blocked"))), uintptr(unsafe.Pointer(wstr(""))), uintptr(unsafe.Pointer(&resp)))
		if !failed(hr) && resp != 0 {
			vcall(args, argsPutResponse, resp)
			comRelease(ptr(resp))
		}
	case res.Redirect != "":
		// A document navigation was redirected at NavigationStarting.
		var ctx uint32
		vcall(args, argsGetResourceContext, uintptr(unsafe.Pointer(&ctx)))
		if ctx != resourceContextDocument {
			action = "redirect"
			vcall(req, reqPutUri, uintptr(unsafe.Pointer(wstr(res.Redirect))))
		}
	}
	if len(res.Headers) > 0 && action != "block" {
		var hU uintptr
		if !failed(vcall(req, reqGetHeaders, uintptr(unsafe.Pointer(&hU)))) && hU != 0 {
			h := ptr(hU)
			for k, val := range res.Headers {
				vcall(h, hdrSetHeader, uintptr(unsafe.Pointer(wstr(k))), uintptr(unsafe.Pointer(wstr(val))))
			}
			comRelease(h)
			if action == "" {
				action = "headers"
			}
		}
	}
	a.emitRemote(caps.RemoteEvent{View: v.ref, Kind: caps.RemoteRequest, URL: uri, Method: method, Action: action})
	return sOK
}

// remoteMessage handles a web message of a remote view. The sender is the
// top-level document of this view's controller, the token proves the page ran
// the view's script, and the origin is the document's.
func (a *App) remoteMessage(v *view, argsU uintptr) uintptr {
	args := ptr(argsU)
	data, ok := caps.ParseRemoteMessage(comString(args, msgArgsTryGetString), v.rv.token)
	if !ok {
		return sOK
	}
	origin := caps.OriginOfString(comString(args, msgArgsGetSource))
	if origin == "" {
		return sOK
	}
	a.emitRemote(caps.RemoteEvent{View: v.ref, Kind: caps.RemoteMessage, Origin: origin, URL: v.rv.cur, Data: data})
	return sOK
}

// remoteCertError asks the app's handler about a server certificate error.
// Without a handler the default (the load fails) stays.
func (a *App) remoteCertError(v *view, argsU uintptr) uintptr {
	st := a.rstate()
	st.mu.Lock()
	h := st.certHandler
	st.mu.Unlock()
	if h == nil {
		return sOK
	}
	args := ptr(argsU)
	var defU uintptr
	if failed(vcall(args, certArgsGetDeferral, uintptr(unsafe.Pointer(&defU)))) || defU == 0 {
		return sOK
	}
	def := ptr(defU)
	comAddRef(args)
	var status int32
	vcall(args, certArgsGetStatus, uintptr(unsafe.Pointer(&status)))
	uri := comString(args, certArgsGetUri)
	host := uri
	if i := strings.Index(uri, "://"); i >= 0 {
		host = uri[i+3:]
	}
	if i := strings.IndexAny(host, "/:?"); i >= 0 {
		host = host[:i]
	}
	var once sync.Once
	answer := func(allow bool) {
		once.Do(func() {
			a.Dispatch(func() {
				action := uintptr(certActionCancel)
				if allow {
					action = certActionAllow
				}
				vcall(args, certArgsPutAction, action)
				vcall(def, deferralComplete)
				comRelease(def)
				comRelease(args)
			})
		})
	}
	h(caps.CertError{View: v.ref, URL: uri, Host: strings.ToLower(host), Errors: certErrorNames(status)}, answer)
	return sOK
}

// remoteBasicAuth asks the app's handler for credentials.
func (a *App) remoteBasicAuth(v *view, argsU uintptr) uintptr {
	args := ptr(argsU)
	st := a.rstate()
	st.mu.Lock()
	h := st.authHandler
	st.mu.Unlock()
	if h == nil {
		vcall(args, basicArgsPutCancel, 1)
		return sOK
	}
	var defU uintptr
	if failed(vcall(args, basicArgsGetDeferral, uintptr(unsafe.Pointer(&defU)))) || defU == 0 {
		return sOK
	}
	def := ptr(defU)
	comAddRef(args)
	uri := comString(args, basicArgsGetUri)
	scheme, realm := parseChallenge(comString(args, basicArgsGetChallenge))
	host := uri
	if i := strings.Index(uri, "://"); i >= 0 {
		host = uri[i+3:]
	}
	if i := strings.IndexAny(host, "/?"); i >= 0 {
		host = host[:i]
	}
	var once sync.Once
	answer := func(user, pass string, ok bool) {
		once.Do(func() {
			a.Dispatch(func() {
				if ok {
					var respU uintptr
					if !failed(vcall(args, basicArgsGetResponse, uintptr(unsafe.Pointer(&respU)))) && respU != 0 {
						resp := ptr(respU)
						vcall(resp, basicRespPutUser, uintptr(unsafe.Pointer(wstr(user))))
						vcall(resp, basicRespPutPassword, uintptr(unsafe.Pointer(wstr(pass))))
						comRelease(resp)
					}
				} else {
					vcall(args, basicArgsPutCancel, 1)
				}
				vcall(def, deferralComplete)
				comRelease(def)
				comRelease(args)
			})
		})
	}
	h(caps.AuthRequest{View: v.ref, URL: uri, Host: host, Realm: realm, Scheme: scheme}, answer)
	return sOK
}

func (a *App) remoteView(ref caps.ViewRef) *view {
	for _, w := range a.windows {
		for _, v := range w.allViews() {
			if v.ref == ref && v.rv != nil {
				return v
			}
		}
	}
	return nil
}

// RemoteOrigin is the origin of the page the view shows now.
func (a *App) RemoteOrigin(ref caps.ViewRef) (origin string) {
	a.callMain(func() {
		if v := a.remoteView(ref); v != nil {
			uri := v.rv.cur
			if v.webview != nil {
				if s := comString(v.webview, wvGetSource); s != "" {
					uri = s
				}
			}
			origin = caps.OriginOfString(uri)
		}
	})
	return
}

// NavigateRemote loads a URL in the view if its policy allows it.
func (a *App) NavigateRemote(ref caps.ViewRef, raw string) (err error) {
	a.callMain(func() {
		v := a.remoteView(ref)
		switch {
		case v == nil:
			err = krate.Errorf("not_found", "view %d is not a remote view", ref)
		case raw == "about:blank" || v.rv.policy.Decide(raw) != caps.NavAllow:
			err = krate.Errorf("permission_denied", "the view may not navigate to %q", raw)
		default:
			v.do(func() { vcall(v.webview, wvNavigate, uintptr(unsafe.Pointer(wstr(raw)))) })
		}
	})
	return
}

// PostToRemote delivers a message to the page of a remote view.
func (a *App) PostToRemote(ref caps.ViewRef, target string, data []byte) (err error) {
	if len(data) > caps.MaxRemoteMessage {
		return krate.Errorf("bad_args", "the message is %d bytes; the limit is %d", len(data), caps.MaxRemoteMessage)
	}
	a.callMain(func() {
		v := a.remoteView(ref)
		switch {
		case v == nil:
			err = krate.Errorf("not_found", "view %d is not a remote view", ref)
		case !v.rv.policy.Messages:
			err = krate.Errorf("permission_denied", "messages are not enabled for this view")
		case v.webview == nil:
			err = errReleased
		case target != "*" && caps.OriginOfString(comString(v.webview, wvGetSource)) != target:
			err = krate.Errorf("permission_denied", "the page is not on %q", target)
		default:
			js := caps.DeliverToRemoteScript(Origin, target, data)
			vcall(v.webview, wvExecuteScript, uintptr(unsafe.Pointer(wstr(js))), 0)
		}
	})
	return
}

// SetRequestRules replaces the request rules of a partition. They are read
// at every request, so live views follow at once.
func (a *App) SetRequestRules(partition string, rules []caps.RequestRule) error {
	p, err := caps.ParsePartition(partition)
	if err != nil {
		return err
	}
	if err := caps.ValidateRules(rules); err != nil {
		return err
	}
	rules = append([]caps.RequestRule(nil), rules...)
	a.callMain(func() { a.partitionFor(p).rules = rules })
	return nil
}

// SetUserAgent sets the user agent of a partition.
func (a *App) SetUserAgent(partition, ua string) error {
	p, err := caps.ParsePartition(partition)
	if err != nil {
		return err
	}
	if strings.ContainsAny(ua, "\r\n\x00") {
		return krate.Errorf("bad_args", "the user agent cannot contain line breaks")
	}
	a.callMain(func() {
		pt := a.partitionFor(p)
		pt.ua = ua
		for _, w := range a.windows {
			for _, v := range w.allViews() {
				if v.rv == nil || v.rv.part != pt || v.webview == nil {
					continue
				}
				var settings uintptr
				if !failed(vcall(v.webview, wvGetSettings, uintptr(unsafe.Pointer(&settings)))) && settings != 0 {
					a.applyRemoteUA(v, ptr(settings))
					comRelease(ptr(settings))
				}
			}
		}
	})
	return nil
}

// SetProxy is not available per partition: WebView2 takes a proxy as a
// --proxy-server browser argument of the whole environment.
func (a *App) SetProxy(partition string, c caps.ProxyConfig) error {
	if err := c.Validate(); err != nil {
		return err
	}
	return fmt.Errorf("%w: WebView2 takes a proxy as a browser argument of the whole environment, not per partition", krate.ErrUnsupported)
}

// SetClientCertificate is not available: WebView2 offers client certificates
// from the Windows certificate store, not from PEM files.
func (a *App) SetClientCertificate(partition, host, certFile, keyFile string) error {
	return fmt.Errorf("%w: client certificates on Windows come from the certificate store, not from files", krate.ErrUnsupported)
}

// SetCertificateHandler sets the handler for refused certificates.
func (a *App) SetCertificateHandler(fn func(caps.CertError, func(bool))) {
	st := a.rstate()
	st.mu.Lock()
	st.certHandler = fn
	st.mu.Unlock()
}

// SetAuthHandler sets the handler for HTTP authentication challenges.
func (a *App) SetAuthHandler(fn func(caps.AuthRequest, func(string, string, bool))) {
	st := a.rstate()
	st.mu.Lock()
	st.authHandler = fn
	st.mu.Unlock()
}

// ClearPartition removes stored data of a partition. WebView2 clears a
// profile through one of its webviews, so a view of the partition must be open.
func (a *App) ClearPartition(partition string, o caps.ClearSiteDataOptions) error {
	p, err := caps.ParsePartition(partition)
	if err != nil {
		return err
	}
	var kinds uint32
	all := o.Everything()
	if all || o.Cookies {
		kinds |= dataCookies
	}
	if all || o.Cache {
		kinds |= dataDiskCache
	}
	if all || o.Storage {
		kinds |= dataAllDOMStorage | dataServiceWork
	}
	done := make(chan error, 1)
	var startErr error
	a.callMain(func() {
		pt := a.partitionFor(p)
		var wv unsafe.Pointer
		for _, w := range a.windows {
			for _, v := range w.allViews() {
				if v.rv != nil && v.rv.part == pt && v.webview != nil && wv == nil {
					wv = v.webview
				}
			}
		}
		if wv == nil {
			startErr = krate.Errorf("not_found", "open a view of partition %q first: WebView2 clears a profile through one of its views", partition)
			return
		}
		wv13 := queryInterface(wv, iidWV2_13)
		if wv13 == nil {
			startErr = pageUnsupported("clearing a profile (ICoreWebView2_13)")
			return
		}
		defer comRelease(wv13)
		var pp uintptr
		if failed(vcall(wv13, wv13GetProfile, uintptr(unsafe.Pointer(&pp)))) || pp == 0 {
			startErr = errors.New("windows: get_Profile failed")
			return
		}
		profile := ptr(pp)
		defer comRelease(profile)
		p2 := queryInterface(profile, iidWV2Profile2)
		if p2 == nil {
			startErr = pageUnsupported("clearing a profile (ICoreWebView2Profile2)")
			return
		}
		defer comRelease(p2)
		h := newHandler(iidWV2ClearBrowsingDataCompletedHandler, true, func(hr, _ uintptr) uintptr {
			if failed(hr) {
				done <- fmt.Errorf("windows: ClearBrowsingData: 0x%08x", uint32(hr))
			} else {
				done <- nil
			}
			return sOK
		})
		if hr := vcall(p2, profile2ClearData, uintptr(kinds), uintptr(h)); failed(hr) {
			startErr = fmt.Errorf("windows: ClearBrowsingData: 0x%08x", uint32(hr))
		}
	})
	if startErr != nil {
		return startErr
	}
	return <-done
}

// forgetRemote drops a view from the remote set when it goes away.
func (a *App) forgetRemote(v *view) {
	if v.rv == nil {
		return
	}
	st := a.rstate()
	st.mu.Lock()
	delete(st.refs, v.ref)
	st.mu.Unlock()
}

// profileName is the WebView2 profile of a storage partition. WebView2 limits
// profile names to 64 characters; a longer partition name is hashed. The
// prefix keeps the names apart from the app's own (default) profile, and
// in-memory partitions from persistent ones.
func profileName(p caps.Partition) string {
	kind := "m"
	if p.Persistent {
		kind = "p"
	}
	name := "kr-" + kind + "-" + p.Name
	if len(name) > 64 {
		h := sha256.Sum256([]byte(p.Name))
		name = "kr-" + kind + "-" + hex.EncodeToString(h[:])[:40]
	}
	return name
}

// certErrorNames maps COREWEBVIEW2_WEB_ERROR_STATUS of a server certificate
// error to the names caps.CertError uses.
func certErrorNames(status int32) []string {
	switch status {
	case 1:
		return []string{"mismatch"}
	case 2:
		return []string{"expired"}
	case 4:
		return []string{"revoked"}
	case 5:
		return []string{"untrusted"}
	}
	return []string{"unknown"}
}

// parseChallenge splits a WWW-Authenticate challenge ("Basic realm=\"x\"")
// into its scheme and realm.
func parseChallenge(c string) (scheme, realm string) {
	c = strings.TrimSpace(c)
	scheme, rest, _ := strings.Cut(c, " ")
	scheme = strings.ToLower(scheme)
	if scheme == "" {
		scheme = "other"
	}
	i := strings.Index(strings.ToLower(rest), "realm=")
	if i < 0 {
		return scheme, ""
	}
	v := strings.TrimSpace(rest[i+len("realm="):])
	if strings.HasPrefix(v, `"`) {
		if j := strings.Index(v[1:], `"`); j >= 0 {
			return scheme, v[1 : 1+j]
		}
		return scheme, strings.Trim(v, `"`)
	}
	if j := strings.IndexAny(v, ", "); j >= 0 {
		v = v[:j]
	}
	return scheme, v
}
