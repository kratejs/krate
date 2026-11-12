//go:build darwin

package darwin

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"

	"github.com/kratejs/krate/packages/native/krate"
	"github.com/kratejs/krate/packages/native/platform/caps"
)

// Remote content on WKWebView. See platform/caps/remote.go for the model.
//
// Engine isolation: a remote web view gets a WKWebViewConfiguration of its
// own with its own WKWebsiteDataStore and WKProcessPool and without the app's
// URL scheme handler, so a page in it cannot fetch the app origin, and without
// any of the app's user scripts. Compiled only, until the CI native job runs
// remote_gui_test.go on macos-latest.

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

// partition is one storage partition: a data store and a process pool.
type partition struct {
	p     caps.Partition
	store objc.ID
	pool  objc.ID
	ua    string
	rules []caps.RequestRule
	// list is the compiled content rule list of the block rules (0 for none).
	list    objc.ID
	proxy   *caps.ProxyConfig
	allowed map[string]bool // hosts whose certificate the app accepted
}

type remoteState struct {
	mu          sync.Mutex
	parts       map[string]*partition
	refs        map[caps.ViewRef]bool // remote views, readable from any goroutine
	handler     func(caps.RemoteEvent)
	certHandler func(caps.CertError, func(bool))
	authHandler func(caps.AuthRequest, func(string, string, bool))
}

type remoteSpec struct {
	policy *caps.RemotePolicy
}

func (a *App) initRemote() {
	a.rem = &remoteState{parts: map[string]*partition{}, refs: map[caps.ViewRef]bool{}}
}

// Values of the AppKit and WebKit constants used below.
const (
	remotePolicyCancel = 0
	remotePolicyAllow  = 1

	dispositionUseCredential = 0
	dispositionDefault       = 1
	dispositionCancel        = 2

	userScriptAtDocumentStart = 0
	permissionDeny            = 2 // WKPermissionDecisionDeny
)

func registerRemoteClasses(a *App) error {
	nsObject := objc.GetClass("NSObject")
	protos := func(names ...string) []*objc.Protocol {
		var out []*objc.Protocol
		for _, n := range names {
			if p := objc.GetProtocol(n); p != nil {
				out = append(out, p)
			}
		}
		return out
	}
	_, err := objc.RegisterClass("KrateRemoteNav", nsObject, protos("WKNavigationDelegate"), nil, []objc.MethodDef{
		{Cmd: sel("webView:decidePolicyForNavigationAction:decisionHandler:"), Fn: func(self objc.ID, _ objc.SEL, web, action, handler objc.ID) {
			invokeBlock1(handler, uintptr(a.decideRemoteNavigation(web, action)))
		}},
		{Cmd: sel("webView:decidePolicyForNavigationResponse:decisionHandler:"), Fn: func(self objc.ID, _ objc.SEL, web, resp, handler objc.ID) {
			// A response the view cannot show would become a download.
			if objc.Send[bool](resp, sel("canShowMIMEType")) {
				invokeBlock1(handler, remotePolicyAllow)
			} else {
				invokeBlock1(handler, remotePolicyCancel)
			}
		}},
		{Cmd: sel("webView:didCommitNavigation:"), Fn: func(self objc.ID, _ objc.SEL, web, nav objc.ID) {
			a.remoteCommitted(web)
		}},
		{Cmd: sel("webView:didFinishNavigation:"), Fn: func(self objc.ID, _ objc.SEL, web, nav objc.ID) {
			a.remoteFinished(web)
		}},
		{Cmd: sel("webView:didReceiveAuthenticationChallenge:completionHandler:"), Fn: func(self objc.ID, _ objc.SEL, web, challenge, handler objc.ID) {
			a.remoteChallenge(web, challenge, handler)
		}},
	})
	if err != nil {
		return err
	}
	_, err = objc.RegisterClass("KrateRemoteUI", nsObject, protos("WKUIDelegate"), nil, []objc.MethodDef{
		// window.open and target=_blank: no second web view is ever made. A
		// URL the policy allows loads in the view itself.
		{Cmd: sel("webView:createWebViewWithConfiguration:forNavigationAction:windowFeatures:"), Fn: func(self objc.ID, _ objc.SEL, web, config, action, features objc.ID) objc.ID {
			a.remoteCreate(web, action)
			return 0
		}},
		{Cmd: sel("webView:requestMediaCapturePermissionForOrigin:initiatedByFrame:type:decisionHandler:"),
			Fn: func(self objc.ID, _ objc.SEL, web, origin, frame objc.ID, typ int, handler objc.ID) {
				invokeBlock1(handler, permissionDeny)
			}},
		{Cmd: sel("webView:requestGeolocationPermissionForOrigin:initiatedByFrame:decisionHandler:"),
			Fn: func(self objc.ID, _ objc.SEL, web, origin, frame objc.ID, handler objc.ID) {
				invokeBlock1(handler, permissionDeny)
			}},
	})
	if err != nil {
		return err
	}
	_, err = objc.RegisterClass("KrateRemoteMsg", nsObject, protos("WKScriptMessageHandler"), nil, []objc.MethodDef{
		{Cmd: sel("userContentController:didReceiveScriptMessage:"), Fn: func(self objc.ID, _ objc.SEL, ucc, msg objc.ID) {
			a.remoteMessage(msg)
		}},
	})
	if err != nil {
		return err
	}
	a.remoteNav = cls("KrateRemoteNav").Send(sel("new"))
	a.remoteUI = cls("KrateRemoteUI").Send(sel("new"))
	a.remoteMsg = cls("KrateRemoteMsg").Send(sel("new"))
	return nil
}

// RemoteCapabilities reports what WKWebView can do. A WKWebView offers no
// hook for the requests of subresources, so observing and header changes are
// limited to navigations; blocking is a content rule list and covers all.
func (a *App) RemoteCapabilities() caps.RemoteCaps {
	ds := cls("WKWebsiteDataStore")
	return caps.RemoteCaps{
		Observe:           caps.LevelNavigation,
		Block:             caps.LevelAll,
		Redirect:          caps.LevelNavigation,
		RequestHeaders:    caps.LevelNavigation,
		UserAgent:         true,
		Proxy:             objc.Send[bool](ds, sel("instancesRespondToSelector:"), sel("setProxyConfigurations:")),
		CertificateErrors: true,
		Auth:              true,
		InMemory:          true,
		// Client certificates need a keychain identity; not implemented.
	}
}

// IsRemoteView reports whether a view shows remote content. Any goroutine.
func (a *App) IsRemoteView(ref caps.ViewRef) bool {
	a.rem.mu.Lock()
	defer a.rem.mu.Unlock()
	return a.rem.refs[ref]
}

func (a *App) partitionFor(p caps.Partition) *partition {
	a.rem.mu.Lock()
	defer a.rem.mu.Unlock()
	if pt := a.rem.parts[p.Key()]; pt != nil {
		return pt
	}
	pt := &partition{p: p, allowed: map[string]bool{}}
	a.rem.parts[p.Key()] = pt
	return pt
}

// partitionUUID derives the stable identifier of a persistent data store.
func partitionUUID(name string) string {
	h := sha256.Sum256([]byte("krate-remote-partition:" + name))
	b := h[:16]
	b[6] = b[6]&0x0f | 0x50 // version 5 layout
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// ensure creates the data store and process pool of a partition. Main thread.
func (a *App) ensure(pt *partition) error {
	if pt.store != 0 {
		return nil
	}
	ds := cls("WKWebsiteDataStore")
	var store objc.ID
	switch {
	case !pt.p.Persistent:
		store = ds.Send(sel("nonPersistentDataStore"))
	case objc.Send[bool](ds, sel("respondsToSelector:"), sel("dataStoreForIdentifier:")):
		uuid := cls("NSUUID").Send(sel("alloc")).Send(sel("initWithUUIDString:"), nsString(partitionUUID(pt.p.Name)))
		store = ds.Send(sel("dataStoreForIdentifier:"), uuid)
		release(uuid)
	case pt.p.Name == caps.DefaultPartition:
		// Before macOS 14 there are no named persistent stores. The default
		// remote partition then lives in memory rather than share the app's.
		store = ds.Send(sel("nonPersistentDataStore"))
	default:
		return fmt.Errorf("%w: persistent partitions need macOS 14; use a name without persist: for an in-memory one", krate.ErrUnsupported)
	}
	if store == 0 {
		return errors.New("darwin: cannot create the data store of a partition")
	}
	retain(store)
	pt.store = store
	pt.pool = cls("WKProcessPool").Send(sel("new"))
	if pt.proxy != nil {
		// A proxy the app asked for that cannot be applied (macOS before 14
		// has no per-store proxy) must not leave the partition going direct.
		if err := a.applyProxy(pt); err != nil {
			return err
		}
	}
	return nil
}

// newRemoteWebView creates the web view of a remote view. Main thread.
func (a *App) newRemoteWebView(rs *remoteSpec) (objc.ID, *partition, string, error) {
	pt := a.partitionFor(rs.policy.Partition)
	if err := a.ensure(pt); err != nil {
		return 0, nil, "", err
	}
	config := cls("WKWebViewConfiguration").Send(sel("new"))
	defer release(config)
	config.Send(sel("setWebsiteDataStore:"), pt.store)
	config.Send(sel("setProcessPool:"), pt.pool)
	// Deliberately no setURLSchemeHandler:forURLScheme: for the app scheme.
	a.applyMemoryConfig(config)
	token := ""
	ucc := config.Send(sel("userContentController"))
	if rs.policy.Messages && ucc != 0 {
		token = caps.NewRemoteToken()
		ucc.Send(sel("addScriptMessageHandler:name:"), a.remoteMsg, nsString("krateRemote"))
		src := caps.RemotePageScript("function(m){window.webkit.messageHandlers.krateRemote.postMessage(m)}", token)
		script := cls("WKUserScript").Send(sel("alloc")).Send(sel("initWithSource:injectionTime:forMainFrameOnly:"), nsString(src), userScriptAtDocumentStart, true)
		ucc.Send(sel("addUserScript:"), script)
		release(script)
	}
	if pt.list != 0 && ucc != 0 {
		ucc.Send(sel("addContentRuleList:"), pt.list)
	}
	web := cls(webViewClassName()).Send(sel("alloc")).Send(sel("initWithFrame:configuration:"), nsRect{}, config)
	if web == 0 {
		return 0, nil, "", errors.New("darwin: WKWebView creation failed")
	}
	web.Send(sel("setAutoresizingMask:"), nsViewWidthSizable|nsViewHeightSizable)
	web.Send(sel("setNavigationDelegate:"), a.remoteNav)
	web.Send(sel("setUIDelegate:"), a.remoteUI)
	ua := rs.policy.UserAgent
	if ua == "" {
		ua = pt.ua
	}
	if ua != "" {
		web.Send(sel("setCustomUserAgent:"), nsString(ua))
	}
	if a.opts.Debug {
		if s := sel("setInspectable:"); respondsTo(web, s) {
			web.Send(s, true)
		}
	}
	return web, pt, token, nil
}

// attachRemote marks a view remote once it is registered. Main thread.
func (a *App) attachRemote(v *view, pt *partition, rs *remoteSpec, token string) {
	cur := ""
	if v.rv != nil {
		cur = v.rv.cur
	}
	if token == "" && v.rv != nil {
		token = v.rv.token
	}
	v.rv = &remoteView{policy: rs.policy, part: pt, token: token, cur: cur}
	a.rem.mu.Lock()
	a.rem.refs[v.ref] = true
	a.rem.mu.Unlock()
}

func (v *view) loadRemote() {
	if v.web == 0 {
		return
	}
	u := cls("NSURL").Send(sel("URLWithString:"), nsString(v.rv.policy.Start))
	v.web.Send(sel("loadRequest:"), cls("NSURLRequest").Send(sel("requestWithURL:"), u))
}

func (a *App) emitRemote(ev caps.RemoteEvent) {
	a.rem.mu.Lock()
	fn := a.rem.handler
	a.rem.mu.Unlock()
	if fn != nil {
		fn(ev)
	}
}

// OnRemoteEvent sets the handler for remote events.
func (a *App) OnRemoteEvent(fn func(caps.RemoteEvent)) {
	a.rem.mu.Lock()
	a.rem.handler = fn
	a.rem.mu.Unlock()
}

// NewRemoteWindow opens a window whose primary view shows remote content.
func (a *App) NewRemoteWindow(spec caps.RemoteWindowSpec) (caps.RemoteWindow, error) {
	pol, err := spec.Remote.Resolve()
	if err != nil {
		return nil, err
	}
	var w *Window
	runOnMain(func() {
		withPool(func() {
			w, err = a.newWindowWith(WindowOptions{Title: spec.Title, Width: spec.Width, Height: spec.Height, Resizable: spec.Resizable, Chrome: spec.Chrome}, &remoteSpec{policy: pol})
		})
	})
	if err != nil {
		return nil, err
	}
	return w, nil
}

// AddRemoteView adds a remote view to the window.
func (w *Window) AddRemoteView(o caps.ViewOptions, r caps.RemoteOptions) (ref caps.ViewRef, err error) {
	pol, err := r.Resolve()
	if err != nil {
		return 0, err
	}
	runOnMain(func() { withPool(func() { ref, err = w.addViewWith(o, &remoteSpec{policy: pol}) }) })
	return
}

func (a *App) remoteView(ref caps.ViewRef) *view {
	for _, v := range a.byWeb {
		if v.ref == ref && v.rv != nil {
			return v
		}
	}
	return nil
}

// RemoteOrigin is the origin of the page the view shows now.
func (a *App) RemoteOrigin(ref caps.ViewRef) (origin string) {
	runOnMain(func() {
		if v := a.remoteView(ref); v != nil {
			origin = caps.OriginOfString(v.currentRoute())
		}
	})
	return
}

// NavigateRemote loads a URL in the view if its policy allows it.
func (a *App) NavigateRemote(ref caps.ViewRef, raw string) (err error) {
	runOnMain(func() {
		v := a.remoteView(ref)
		switch {
		case v == nil:
			err = krate.Errorf("not_found", "view %d is not a remote view", ref)
		case v.web == 0:
			err = errReleased
		case raw == "about:blank" || v.rv.policy.Decide(raw) != caps.NavAllow:
			err = krate.Errorf("permission_denied", "the view may not navigate to %q", raw)
		default:
			v.load(raw)
		}
	})
	return
}

// PostToRemote delivers a message to the page of a remote view.
func (a *App) PostToRemote(ref caps.ViewRef, target string, data []byte) (err error) {
	if len(data) > caps.MaxRemoteMessage {
		return krate.Errorf("bad_args", "the message is %d bytes; the limit is %d", len(data), caps.MaxRemoteMessage)
	}
	runOnMain(func() {
		v := a.remoteView(ref)
		switch {
		case v == nil:
			err = krate.Errorf("not_found", "view %d is not a remote view", ref)
		case !v.rv.policy.Messages:
			err = krate.Errorf("permission_denied", "messages are not enabled for this view")
		case v.web == 0:
			err = errReleased
		case target != "*" && caps.OriginOfString(v.currentRoute()) != target:
			err = krate.Errorf("permission_denied", "the page is not on %q", target)
		default:
			v.web.Send(sel("evaluateJavaScript:completionHandler:"), nsString(caps.DeliverToRemoteScript(OriginURL, target, data)), objc.ID(0))
		}
	})
	return
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
	runOnMain(func() {
		pt := a.partitionFor(p)
		pt.ua = ua
		for _, v := range a.byWeb {
			if v.rv != nil && v.rv.part == pt && v.web != 0 {
				if ua == "" {
					v.web.Send(sel("setCustomUserAgent:"), objc.ID(0))
				} else {
					v.web.Send(sel("setCustomUserAgent:"), nsString(ua))
				}
			}
		}
	})
	return nil
}

// SetProxy sets the proxy of a partition (macOS 14 and later).
func (a *App) SetProxy(partition string, c caps.ProxyConfig) (err error) {
	p, perr := caps.ParsePartition(partition)
	if perr != nil {
		return perr
	}
	if err := c.Validate(); err != nil {
		return err
	}
	if !objc.Send[bool](cls("WKWebsiteDataStore"), sel("instancesRespondToSelector:"), sel("setProxyConfigurations:")) {
		return fmt.Errorf("%w: partition proxies need macOS 14", krate.ErrUnsupported)
	}
	runOnMain(func() {
		pt := a.partitionFor(p)
		pt.proxy = &c
		err = a.applyProxy(pt)
	})
	return
}

var netFns struct {
	once       sync.Once
	ok         bool
	endpoint   func(host string, port string) uintptr
	httpProxy  func(endpoint, tls uintptr) uintptr
	socksProxy func(endpoint uintptr) uintptr
}

func loadNetFns() bool {
	netFns.once.Do(func() {
		h, err := purego.Dlopen("/usr/lib/libnetwork.dylib", purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err != nil {
			return
		}
		purego.RegisterLibFunc(&netFns.endpoint, h, "nw_endpoint_create_host")
		purego.RegisterLibFunc(&netFns.httpProxy, h, "nw_proxy_config_create_http_connect")
		purego.RegisterLibFunc(&netFns.socksProxy, h, "nw_proxy_config_create_socksv5")
		netFns.ok = true
	})
	return netFns.ok
}

// applyProxy sets WKWebsiteDataStore.proxyConfigurations from the partition's
// proxy: an empty list is the system setting.
func (a *App) applyProxy(pt *partition) error {
	if pt.store == 0 || pt.proxy == nil {
		return nil
	}
	arr := cls("NSMutableArray").Send(sel("array"))
	switch pt.proxy.Mode {
	case caps.ProxyFixed:
		if !loadNetFns() {
			return fmt.Errorf("%w: libnetwork is not available", krate.ErrUnsupported)
		}
		host, port, ok := splitHostPort(pt.proxy.URL)
		if !ok {
			return fmt.Errorf("darwin: proxy URL %q has no host and port", pt.proxy.URL)
		}
		ep := netFns.endpoint(host, port)
		var cfg uintptr
		if strings.HasPrefix(pt.proxy.URL, "socks5://") {
			cfg = netFns.socksProxy(ep)
		} else {
			cfg = netFns.httpProxy(ep, 0)
		}
		if cfg != 0 {
			arr.Send(sel("addObject:"), objc.ID(cfg))
		}
	case caps.ProxyDirect, caps.ProxySystem:
		// WKWebView has no switch for "direct" distinct from the system
		// setting; both leave the list empty.
	}
	pt.store.Send(sel("setProxyConfigurations:"), arr)
	return nil
}

func splitHostPort(raw string) (host, port string, ok bool) {
	i := strings.Index(raw, "://")
	if i < 0 {
		return "", "", false
	}
	rest := raw[i+3:]
	if j := strings.IndexByte(rest, '/'); j >= 0 {
		rest = rest[:j]
	}
	c := strings.LastIndexByte(rest, ':')
	if c <= 0 || c == len(rest)-1 {
		return "", "", false
	}
	return rest[:c], rest[c+1:], true
}

// SetClientCertificate is not available: a client certificate needs a
// keychain identity (SecIdentity), and this layer takes PEM files.
func (a *App) SetClientCertificate(partition, host, certFile, keyFile string) error {
	return fmt.Errorf("%w: client certificates on macOS need a keychain identity, which this layer does not create", krate.ErrUnsupported)
}

// ClearPartition removes stored data of a partition.
func (a *App) ClearPartition(partition string, o caps.ClearSiteDataOptions) error {
	p, err := caps.ParsePartition(partition)
	if err != nil {
		return err
	}
	var kinds []string
	all := o.Everything()
	if all || o.Cookies {
		kinds = append(kinds, "WKWebsiteDataTypeCookies")
	}
	if all || o.Cache {
		kinds = append(kinds, "WKWebsiteDataTypeDiskCache", "WKWebsiteDataTypeMemoryCache", "WKWebsiteDataTypeOfflineWebApplicationCache", "WKWebsiteDataTypeFetchCache")
	}
	if all || o.Storage {
		kinds = append(kinds, "WKWebsiteDataTypeLocalStorage", "WKWebsiteDataTypeSessionStorage", "WKWebsiteDataTypeIndexedDBDatabases",
			"WKWebsiteDataTypeWebSQLDatabases", "WKWebsiteDataTypeServiceWorkerRegistrations")
	}
	done := make(chan error, 1)
	var startErr error
	runOnMain(func() {
		pt := a.partitionFor(p)
		if startErr = a.ensure(pt); startErr != nil {
			return
		}
		set := cls("NSMutableSet").Send(sel("set"))
		for _, k := range kinds {
			set.Send(sel("addObject:"), nsString(k))
		}
		since := cls("NSDate").Send(sel("dateWithTimeIntervalSince1970:"), 0.0)
		blk := objc.NewBlock(func(_ objc.Block) { done <- nil })
		pt.store.Send(sel("removeDataOfTypes:modifiedSince:completionHandler:"), set, since, objc.ID(blk))
		blk.Release()
	})
	if startErr != nil {
		return startErr
	}
	if isMainThread() {
		return nil
	}
	return <-done
}

// SetRequestRules replaces the request rules of a partition. Block rules are
// compiled into a WKContentRuleList; redirects and header rules act on
// navigations.
func (a *App) SetRequestRules(partition string, rules []caps.RequestRule) error {
	p, err := caps.ParsePartition(partition)
	if err != nil {
		return err
	}
	if err := caps.ValidateRules(rules); err != nil {
		return err
	}
	blocker, err := caps.ContentBlockerJSON(rules)
	if err != nil {
		return err
	}
	rules = append([]caps.RequestRule(nil), rules...)
	done := make(chan error, 1)
	runOnMain(func() {
		pt := a.partitionFor(p)
		pt.rules = rules
		apply := func(list objc.ID) {
			old := pt.list
			pt.list = list
			for _, v := range a.byWeb {
				if v.rv == nil || v.rv.part != pt || v.web == 0 {
					continue
				}
				ucc := v.web.Send(sel("configuration")).Send(sel("userContentController"))
				ucc.Send(sel("removeAllContentRuleLists"))
				if list != 0 {
					ucc.Send(sel("addContentRuleList:"), list)
				}
			}
			if old != 0 {
				release(old)
			}
			done <- nil
		}
		if blocker == nil {
			apply(0)
			return
		}
		store := cls("WKContentRuleListStore").Send(sel("defaultStore"))
		blk := objc.NewBlock(func(_ objc.Block, list, e objc.ID) {
			if list == 0 {
				done <- fmt.Errorf("darwin: cannot compile the block rules: %s", remoteErrText(e))
				return
			}
			retain(list)
			apply(list)
		})
		store.Send(sel("compileContentRuleListForIdentifier:encodedContentRuleList:completionHandler:"),
			nsString("krate-rules-"+pt.p.Key()), nsString(string(blocker)), objc.ID(blk))
		blk.Release()
	})
	if isMainThread() {
		return nil
	}
	return <-done
}

func remoteErrText(e objc.ID) string {
	if e == 0 {
		return "unknown error"
	}
	return goString(e.Send(sel("localizedDescription")))
}

// SetCertificateHandler sets the handler for refused certificates.
func (a *App) SetCertificateHandler(fn func(caps.CertError, func(bool))) {
	a.rem.mu.Lock()
	a.rem.certHandler = fn
	a.rem.mu.Unlock()
}

// SetAuthHandler sets the handler for HTTP authentication challenges.
func (a *App) SetAuthHandler(fn func(caps.AuthRequest, func(string, string, bool))) {
	a.rem.mu.Lock()
	a.rem.authHandler = fn
	a.rem.mu.Unlock()
}

// decideRemoteNavigation is the navigation lock of remote views: every
// navigation and new-window request goes through RemotePolicy.Decide. It
// returns the policy for the decision handler.
func (a *App) decideRemoteNavigation(web, action objc.ID) int {
	v := a.byWeb[web]
	if v == nil || v.rv == nil {
		return remotePolicyCancel
	}
	req := action.Send(sel("request"))
	uri := ""
	if u := req.Send(sel("URL")); u != 0 {
		uri = goString(u.Send(sel("absoluteString")))
	}
	mainFrame := true
	newWindow := true
	if frame := action.Send(sel("targetFrame")); frame != 0 {
		mainFrame = objc.Send[bool](frame, sel("isMainFrame"))
		newWindow = false
	}
	if !mainFrame {
		// Frames: the allowlist applies, but a frame never opens the browser,
		// and in-document schemes cannot reach the app.
		switch schemeOf(uri) {
		case "about", "data", "blob":
			return remotePolicyAllow
		}
		if a.verdictFor(v, uri) != caps.NavAllow {
			a.emitRemote(caps.RemoteEvent{View: v.ref, Kind: caps.RemoteNavBlocked, URL: uri, Verdict: caps.NavBlock})
			return remotePolicyCancel
		}
		return remotePolicyAllow
	}
	verdict := v.rv.policy.Decide(uri)
	if verdict != caps.NavAllow {
		a.emitRemote(caps.RemoteEvent{View: v.ref, Kind: caps.RemoteNavBlocked, URL: uri, Verdict: verdict})
		return remotePolicyCancel
	}
	method := goString(req.Send(sel("HTTPMethod")))
	res := caps.EvalRules(v.rv.part.rules, uri)
	switch {
	case res.Block:
		a.emitRemote(caps.RemoteEvent{View: v.ref, Kind: caps.RemoteRequest, URL: uri, Method: method, Action: "block"})
		return remotePolicyCancel
	case res.Redirect != "":
		a.emitRemote(caps.RemoteEvent{View: v.ref, Kind: caps.RemoteRequest, URL: uri, Method: method, Action: "redirect"})
		a.loadLater(v, res.Redirect, nil)
		return remotePolicyCancel
	case len(res.Headers) > 0 && !hasHeaders(req, res.Headers):
		a.emitRemote(caps.RemoteEvent{View: v.ref, Kind: caps.RemoteRequest, URL: uri, Method: method, Action: "headers"})
		a.loadLater(v, uri, res.Headers)
		return remotePolicyCancel
	}
	a.emitRemote(caps.RemoteEvent{View: v.ref, Kind: caps.RemoteRequest, URL: uri, Method: method})
	if newWindow {
		// A target=_blank link the policy allows opens in the view itself.
		a.loadLater(v, uri, nil)
		return remotePolicyCancel
	}
	return remotePolicyAllow
}

func (a *App) verdictFor(v *view, uri string) caps.NavVerdict { return v.rv.policy.Decide(uri) }

func schemeOf(uri string) string {
	if i := strings.IndexByte(uri, ':'); i > 0 {
		return strings.ToLower(uri[:i])
	}
	return ""
}

func hasHeaders(req objc.ID, want map[string]string) bool {
	fields := goStringMap(req.Send(sel("allHTTPHeaderFields")))
	for k, v := range want {
		found := false
		for fk, fv := range fields {
			if strings.EqualFold(fk, k) && fv == v {
				found = true
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// loadLater loads a URL (with extra headers) after the current policy
// decision has returned.
func (a *App) loadLater(v *view, uri string, headers map[string]string) {
	dispatch(func() {
		if v.web == 0 {
			return
		}
		u := cls("NSURL").Send(sel("URLWithString:"), nsString(uri))
		if u == 0 {
			return
		}
		req := cls("NSMutableURLRequest").Send(sel("requestWithURL:"), u)
		for k, val := range headers {
			req.Send(sel("setValue:forHTTPHeaderField:"), nsString(val), nsString(k))
		}
		v.web.Send(sel("loadRequest:"), req)
	})
}

// remoteCreate handles window.open of a remote page.
func (a *App) remoteCreate(web, action objc.ID) {
	v := a.byWeb[web]
	if v == nil || v.rv == nil {
		return
	}
	uri := ""
	if u := action.Send(sel("request")).Send(sel("URL")); u != 0 {
		uri = goString(u.Send(sel("absoluteString")))
	}
	verdict := v.rv.policy.Decide(uri)
	if verdict != caps.NavAllow || uri == "" || uri == "about:blank" {
		a.emitRemote(caps.RemoteEvent{View: v.ref, Kind: caps.RemoteNavBlocked, URL: uri, Verdict: verdict})
		return
	}
	a.loadLater(v, uri, nil)
}

func (a *App) remoteCommitted(web objc.ID) {
	if v := a.byWeb[web]; v != nil && v.rv != nil {
		v.rv.cur = v.currentRoute()
	}
}

func (a *App) remoteFinished(web objc.ID) {
	v := a.byWeb[web]
	if v == nil || v.rv == nil {
		return
	}
	v.rv.cur = v.currentRoute()
	a.emitRemote(caps.RemoteEvent{View: v.ref, Kind: caps.RemoteLoaded, URL: v.rv.cur, Origin: caps.OriginOfString(v.rv.cur)})
	emitView(a, v, caps.ViewNavigated, v.rv.cur)
}

// remoteMessage handles a script message of a remote page. Only the main
// frame counts, the token must match, and the origin is the frame's security
// origin as WebKit reports it.
func (a *App) remoteMessage(msg objc.ID) {
	web := msg.Send(sel("webView"))
	v := a.byWeb[web]
	if v == nil || v.rv == nil || v.rv.token == "" {
		return
	}
	frame := msg.Send(sel("frameInfo"))
	if frame == 0 || !objc.Send[bool](frame, sel("isMainFrame")) {
		return
	}
	body := msg.Send(sel("body"))
	if body == 0 || !objc.Send[bool](body, sel("isKindOfClass:"), cls("NSString")) {
		return
	}
	data, ok := caps.ParseRemoteMessage(goString(body), v.rv.token)
	if !ok {
		return
	}
	so := frame.Send(sel("securityOrigin"))
	origin := originString(goString(so.Send(sel("protocol"))), goString(so.Send(sel("host"))), objc.Send[int](so, sel("port")))
	if origin == "" || !v.rv.policy.Messages {
		return
	}
	a.emitRemote(caps.RemoteEvent{View: v.ref, Kind: caps.RemoteMessage, Origin: origin, URL: v.rv.cur, Data: data})
}

var secFns struct {
	once     sync.Once
	ok       bool
	evaluate func(trust uintptr, err *uintptr) bool
}

func loadSecFns() bool {
	secFns.once.Do(func() {
		h, err := purego.Dlopen("/System/Library/Frameworks/Security.framework/Security", purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err != nil {
			return
		}
		purego.RegisterLibFunc(&secFns.evaluate, h, "SecTrustEvaluateWithError")
		secFns.ok = true
	})
	return secFns.ok
}

// invokeBlock2 calls a foreign block of type void (^)(NSInteger, id).
func invokeBlock2(block objc.ID, a1, a2 uintptr) {
	if block == 0 {
		return
	}
	invoke := *(*uintptr)(unsafe.Add(ptrFromID(block), 16))
	purego.SyscallN(invoke, uintptr(block), a1, a2)
}

// remoteChallenge answers server trust and HTTP authentication challenges.
// Server trust is evaluated by the system first; only a failure reaches the
// app's certificate handler, and an accepted host is remembered for the
// partition.
func (a *App) remoteChallenge(web, challenge, handler objc.ID) {
	v := a.byWeb[web]
	if v == nil || v.rv == nil {
		invokeBlock2(handler, dispositionCancel, 0)
		return
	}
	loadBlockFns()
	var block uintptr
	if handler != 0 && blockFns.copy != nil {
		block = blockFns.copy(uintptr(handler))
	}
	var once sync.Once
	finish := func(disposition int, cred objc.ID) {
		once.Do(func() {
			dispatch(func() {
				if block != 0 {
					invokeBlock2(objc.ID(block), uintptr(disposition), uintptr(cred))
					blockFns.release(block)
				}
			})
		})
	}
	space := challenge.Send(sel("protectionSpace"))
	method := goString(space.Send(sel("authenticationMethod")))
	host := strings.ToLower(goString(space.Send(sel("host"))))
	port := objc.Send[int](space, sel("port"))
	switch method {
	case "NSURLAuthenticationMethodServerTrust":
		trust := space.Send(sel("serverTrust"))
		if trust == 0 || !loadSecFns() {
			finish(dispositionDefault, 0)
			return
		}
		var cfErr uintptr
		if secFns.evaluate(uintptr(trust), &cfErr) {
			finish(dispositionDefault, 0)
			return
		}
		if v.rv.part.allowed[host] {
			finish(dispositionUseCredential, cls("NSURLCredential").Send(sel("credentialForTrust:"), trust))
			return
		}
		a.rem.mu.Lock()
		h := a.rem.certHandler
		a.rem.mu.Unlock()
		if h == nil {
			finish(dispositionDefault, 0)
			return
		}
		pt := v.rv.part
		retain(trust)
		h(caps.CertError{View: v.ref, URL: v.rv.cur, Host: host, Errors: []string{"untrusted"}}, func(ok bool) {
			dispatch(func() {
				if ok {
					pt.allowed[host] = true
					finish(dispositionUseCredential, cls("NSURLCredential").Send(sel("credentialForTrust:"), trust))
				} else {
					finish(dispositionCancel, 0)
				}
				release(trust)
			})
		})
	case "NSURLAuthenticationMethodHTTPBasic", "NSURLAuthenticationMethodHTTPDigest", "NSURLAuthenticationMethodNTLM", "NSURLAuthenticationMethodNegotiate":
		a.rem.mu.Lock()
		h := a.rem.authHandler
		a.rem.mu.Unlock()
		if h == nil {
			finish(dispositionCancel, 0)
			return
		}
		scheme := map[string]string{
			"NSURLAuthenticationMethodHTTPBasic": "basic", "NSURLAuthenticationMethodHTTPDigest": "digest",
			"NSURLAuthenticationMethodNTLM": "ntlm", "NSURLAuthenticationMethodNegotiate": "negotiate",
		}[method]
		req := caps.AuthRequest{View: v.ref, URL: v.rv.cur, Host: fmt.Sprintf("%s:%d", host, port), Realm: goString(space.Send(sel("realm"))),
			Scheme: scheme, Proxy: space.Send(sel("proxyType")) != 0, Retry: objc.Send[int](challenge, sel("previousFailureCount")) > 0}
		h(req, func(user, pass string, ok bool) {
			dispatch(func() {
				if !ok {
					finish(dispositionCancel, 0)
					return
				}
				cred := cls("NSURLCredential").Send(sel("credentialWithUser:password:persistence:"), nsString(user), nsString(pass), 0)
				finish(dispositionUseCredential, cred)
			})
		})
	default:
		// Client certificates and anything else: the system's default, which
		// presents none.
		finish(dispositionDefault, 0)
	}
}
