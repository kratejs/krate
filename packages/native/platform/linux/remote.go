//go:build linux

package linux

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/kratejs/krate/packages/native/krate"
	"github.com/kratejs/krate/packages/native/platform/caps"
)

// Remote content on WebKitGTK. See platform/caps/remote.go for the model.
//
// Engine isolation: every storage partition has its own WebKitWebContext (and
// website data manager), created here and never given the krate scheme, so a
// page in it cannot fetch the app origin, and its views are never related to
// an app view. The app's own views are created from the app context only.

var (
	_ caps.RemoteContent = (*App)(nil)
	_ caps.RemoteWindows = (*App)(nil)
	_ caps.RemoteViews   = (*Window)(nil)
)

// remoteView is the remote state of a view. Main thread only.
type remoteView struct {
	policy *caps.RemotePolicy
	part   *partition
	ua     string
	token  string
	cur    string // the last committed URL
}

// partition is one storage partition: a web context of its own.
type partition struct {
	p     caps.Partition
	ctx   uintptr
	mgr   uintptr
	ua    string
	rules []caps.RequestRule
	// filter is the compiled content blocker of the block rules (0 for none).
	filter uintptr
	proxy  *caps.ProxyConfig
	// certs are the client certificates by host (GTlsCertificate, owned).
	certs map[string]uintptr
}

// remoteState is the App's remote bookkeeping. Main thread only, except the
// handlers which are guarded by App.mu.
type remoteState struct {
	parts       map[string]*partition
	store       uintptr
	handler     func(caps.RemoteEvent)
	certHandler func(caps.CertError, func(bool))
	authHandler func(caps.AuthRequest, func(user, password string, ok bool))
}

// remoteAPI holds the WebKitGTK bindings remote views need; every field may
// be nil on an older WebKitGTK.
type remoteAPI struct {
	ucm             func(view uintptr) uintptr
	ucmRegister     func(ucm uintptr, name string, world uintptr) int32
	ucmAddScript    func(ucm, script uintptr)
	ucmAddFilter    func(ucm, filter uintptr)
	ucmRemoveFilter func(ucm uintptr)
	scriptNew       func(src string, frames, when int32, allow, block uintptr) uintptr
	scriptUnref     func(s uintptr)
	storeNew        func(path string) uintptr
	storeSave       func(store uintptr, id string, data, cancel, cb, d uintptr)
	storeSaveFin    func(store, res uintptr, gerr *uintptr) uintptr
	filterUnref     func(f uintptr)
	bytesNew        func(data unsafe.Pointer, n uint64) uintptr
	bytesUnref      func(b uintptr)
	allowTLS        func(ctx, cert uintptr, host string)
	proxyNew        func(def string, ignore uintptr) uintptr
	proxyFree       func(p uintptr)
	setProxy        func(mgr uintptr, mode int32, p uintptr)
	setUA           func(settings uintptr, ua string)
	resetUA         func(settings uintptr, ua uintptr)
	reqNew          func(uri string) uintptr
	reqHeaders      func(req uintptr) uintptr
	reqMethod       func(req uintptr) uintptr
	loadRequest     func(view, req uintptr)
	policyUse       func(d uintptr)
	resURI          func(res uintptr) uintptr
	jsResultValue   func(r uintptr) uintptr
	autoOpen        func(settings uintptr, on int32)
	headersGetOne   func(h uintptr, name string) uintptr
	headersReplace  func(h uintptr, name, value string)

	authHost    func(r uintptr) uintptr
	authPort    func(r uintptr) uint32
	authRealm   func(r uintptr) uintptr
	authScheme  func(r uintptr) int32
	authProxy   func(r uintptr) int32
	authRetry   func(r uintptr) int32
	authDo      func(r, cred uintptr)
	authCancel  func(r uintptr)
	credNew     func(user, pass string, persist int32) uintptr
	credNewCert func(cert uintptr, persist int32) uintptr
	credFree    func(c uintptr)
	certFiles   func(cert, key string, gerr *uintptr) uintptr
}

var cbRCreate, cbRPolicy, cbRLoad, cbRPerm, cbRAuth, cbRTLS, cbRRes, cbRMsg, cbRDl uintptr

// Values of the WebKit enums used below.
const (
	loadCommitted = 2 // WEBKIT_LOAD_COMMITTED

	authClientCert  = 7 // WEBKIT_AUTHENTICATION_SCHEME_CLIENT_CERTIFICATE_REQUESTED
	authServerTrust = 8

	proxyDefault = 0
	proxyNone    = 1
	proxyCustom  = 2
)

func loadRemoteAPI() *remoteAPI {
	r := &remoteAPI{}
	b := &binder{}
	open := func(n string) uintptr {
		h, err := purego.Dlopen(n, rtldNow|rtldGlobal)
		if err != nil {
			return 0
		}
		return h
	}
	bind := func(lib uintptr, fn any, sym string) {
		if lib != 0 {
			b.bind(fn, lib, sym)
		}
	}
	wk, soup, glib, gio := open("libwebkit2gtk-4.1.so.0"), open("libsoup-3.0.so.0"), open("libglib-2.0.so.0"), open("libgio-2.0.so.0")
	bind(wk, &r.ucm, "webkit_web_view_get_user_content_manager")
	bind(wk, &r.ucmRegister, "webkit_user_content_manager_register_script_message_handler")
	bind(wk, &r.ucmAddScript, "webkit_user_content_manager_add_script")
	bind(wk, &r.ucmAddFilter, "webkit_user_content_manager_add_filter")
	bind(wk, &r.ucmRemoveFilter, "webkit_user_content_manager_remove_all_filters")
	bind(wk, &r.scriptNew, "webkit_user_script_new")
	bind(wk, &r.scriptUnref, "webkit_user_script_unref")
	bind(wk, &r.storeNew, "webkit_user_content_filter_store_new")
	bind(wk, &r.storeSave, "webkit_user_content_filter_store_save")
	bind(wk, &r.storeSaveFin, "webkit_user_content_filter_store_save_finish")
	bind(wk, &r.filterUnref, "webkit_user_content_filter_unref")
	bind(glib, &r.bytesNew, "g_bytes_new")
	bind(glib, &r.bytesUnref, "g_bytes_unref")
	bind(wk, &r.allowTLS, "webkit_web_context_allow_tls_certificate_for_host")
	bind(wk, &r.proxyNew, "webkit_network_proxy_settings_new")
	bind(wk, &r.proxyFree, "webkit_network_proxy_settings_free")
	bind(wk, &r.setProxy, "webkit_website_data_manager_set_network_proxy_settings")
	bind(wk, &r.setUA, "webkit_settings_set_user_agent")
	bind(wk, &r.resetUA, "webkit_settings_set_user_agent")
	bind(wk, &r.reqNew, "webkit_uri_request_new")
	bind(wk, &r.reqHeaders, "webkit_uri_request_get_http_headers")
	bind(wk, &r.reqMethod, "webkit_uri_request_get_http_method")
	bind(wk, &r.loadRequest, "webkit_web_view_load_request")
	bind(wk, &r.policyUse, "webkit_policy_decision_use")
	bind(wk, &r.resURI, "webkit_web_resource_get_uri")
	bind(wk, &r.jsResultValue, "webkit_javascript_result_get_js_value")
	bind(wk, &r.autoOpen, "webkit_settings_set_javascript_can_open_windows_automatically")
	bind(soup, &r.headersGetOne, "soup_message_headers_get_one")
	bind(soup, &r.headersReplace, "soup_message_headers_replace")
	bind(wk, &r.authHost, "webkit_authentication_request_get_host")
	bind(wk, &r.authPort, "webkit_authentication_request_get_port")
	bind(wk, &r.authRealm, "webkit_authentication_request_get_realm")
	bind(wk, &r.authScheme, "webkit_authentication_request_get_scheme")
	bind(wk, &r.authProxy, "webkit_authentication_request_is_for_proxy")
	bind(wk, &r.authRetry, "webkit_authentication_request_is_retry")
	bind(wk, &r.authDo, "webkit_authentication_request_authenticate")
	bind(wk, &r.authCancel, "webkit_authentication_request_cancel")
	bind(wk, &r.credNew, "webkit_credential_new")
	bind(wk, &r.credNewCert, "webkit_credential_new_for_certificate")
	bind(wk, &r.credFree, "webkit_credential_free")
	bind(gio, &r.certFiles, "g_tls_certificate_new_from_files")
	return r
}

func (a *App) initRemote() {
	a.rapi = loadRemoteAPI()
	a.rem = &remoteState{parts: map[string]*partition{}}
	a.sweepMemPartitions()
	cbRPolicy = purego.NewCallback(remotePolicyCallback)
	cbRCreate = purego.NewCallback(remoteCreateCallback)
	cbRLoad = purego.NewCallback(remoteLoadCallback)
	cbRPerm = purego.NewCallback(remotePermissionCallback)
	cbRAuth = purego.NewCallback(remoteAuthCallback)
	cbRTLS = purego.NewCallback(remoteTLSCallback)
	cbRRes = purego.NewCallback(remoteResourceCallback)
	cbRMsg = purego.NewCallback(remoteMessageCallback)
	cbRDl = purego.NewCallback(remoteDownloadCallback)
}

// RemoteCapabilities reports what this WebKitGTK can do. Subresource
// redirects and request headers are not possible: WebKitGTK offers no hook
// that can change a subresource request without a web process extension.
func (a *App) RemoteCapabilities() caps.RemoteCaps {
	r := a.rapi
	c := caps.RemoteCaps{InMemory: true}
	if r.resURI != nil {
		c.Observe = caps.LevelAll
	}
	if r.storeSave != nil && r.bytesNew != nil && r.ucmAddFilter != nil {
		c.Block = caps.LevelAll
	} else {
		c.Block = caps.LevelNavigation
	}
	c.Redirect = caps.LevelNavigation
	if r.reqNew != nil && r.loadRequest != nil && r.headersReplace != nil {
		c.RequestHeaders = caps.LevelNavigation
	}
	c.UserAgent = r.setUA != nil
	c.Proxy = r.setProxy != nil && r.proxyNew != nil
	c.CertificateErrors = r.allowTLS != nil
	c.ClientCertificates = r.certFiles != nil && r.credNewCert != nil && r.authDo != nil
	c.Auth = r.authDo != nil && r.credNew != nil
	return c
}

// remote returns the remote state of a view, or nil.
func (a *App) remote(ref caps.ViewRef) *view {
	v := a.viewByRef(uintptr(ref))
	if v == nil || v.rv == nil {
		return nil
	}
	return v
}

// IsRemoteView reports whether a view shows remote content.
func (a *App) IsRemoteView(ref caps.ViewRef) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	v := a.views[ref]
	return v != nil && v.rv != nil
}

// partitionFor returns the partition, creating the bookkeeping (not the web
// context) on first use. Main thread.
func (a *App) partitionFor(p caps.Partition) *partition {
	if pt := a.rem.parts[p.Key()]; pt != nil {
		return pt
	}
	pt := &partition{p: p, certs: map[string]uintptr{}}
	a.rem.parts[p.Key()] = pt
	return pt
}

// ensure creates the web context of the partition. Main thread.
func (a *App) ensure(pt *partition) error {
	if pt.ctx != 0 {
		return nil
	}
	api := a.api
	// A partition that is not persistent gets a private temporary directory,
	// removed when the app quits (and swept at the next start if it crashed).
	// WebKit's ephemeral data managers would be simpler but share their
	// certificate exceptions with each other, which breaks the isolation of
	// partitions.
	dir := filepath.Join(a.dataDir, pt.p.Dir())
	if !pt.p.Persistent {
		dir = filepath.Join(a.dataDir, memPartitionsDir, fmt.Sprintf("%d-%s", os.Getpid(), pt.p.Name))
	}
	if err := os.MkdirAll(filepath.Join(dir, "cache"), 0o700); err != nil {
		return err
	}
	mgr := api.wkDataManagerNew("base-data-directory", dir, "base-cache-directory", filepath.Join(dir, "cache"), 0)
	if mgr == 0 {
		return errors.New("krate: cannot create the website data manager of a partition")
	}
	ctx := api.wkContextNewWithMgr(mgr)
	if ctx == 0 {
		return errors.New("krate: cannot create the web context of a partition")
	}
	pt.ctx, pt.mgr = ctx, a.site.ctxDataMgr(ctx)
	if pt.mgr == 0 {
		pt.mgr = mgr
	}
	if a.site.cookieMgr != nil {
		if cm := a.site.cookieMgr(pt.mgr); cm != 0 {
			a.site.setPolicy(cm, cookieAcceptAlways)
			if pt.p.Persistent && a.site.setStorage != nil {
				a.site.setStorage(cm, filepath.Join(dir, "cookies.sqlite"), cookieStorageSQL)
			}
		}
	}
	// Downloads are not part of remote content: cancel them.
	api.gSignalConnectData(ctx, "download-started", cbRDl, 0, 0, 0)
	if pt.proxy != nil {
		a.applyProxy(pt)
	}
	return nil
}

// memPartitionsDir holds the directories of in-memory partitions.
const memPartitionsDir = "remote-tmp"

// sweepMemPartitions removes the directories of runs that are gone.
func (a *App) sweepMemPartitions() {
	root := filepath.Join(a.dataDir, memPartitionsDir)
	entries, err := os.ReadDir(root)
	if err != nil {
		return
	}
	for _, e := range entries {
		pidStr, _, _ := strings.Cut(e.Name(), "-")
		pid, err := strconv.Atoi(pidStr)
		if err == nil && pid > 0 && (pid == os.Getpid() || syscall.Kill(pid, 0) == nil) {
			continue
		}
		_ = os.RemoveAll(filepath.Join(root, e.Name()))
	}
}

// cleanupRemote removes this run's in-memory partition directories.
func (a *App) cleanupRemote() {
	entries, _ := os.ReadDir(filepath.Join(a.dataDir, memPartitionsDir))
	prefix := strconv.Itoa(os.Getpid()) + "-"
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), prefix) {
			_ = os.RemoveAll(filepath.Join(a.dataDir, memPartitionsDir, e.Name()))
		}
	}
}

func remoteDownloadCallback(_, download, _ uintptr) uintptr {
	if a := theApp; a.dl != nil && a.dl.cancel != nil {
		a.dl.cancel(download)
	}
	return 0
}

func (a *App) applyProxy(pt *partition) {
	r := a.rapi
	if r.setProxy == nil || pt.mgr == 0 || pt.proxy == nil {
		return
	}
	switch pt.proxy.Mode {
	case caps.ProxySystem:
		r.setProxy(pt.mgr, proxyDefault, 0)
	case caps.ProxyDirect:
		r.setProxy(pt.mgr, proxyNone, 0)
	case caps.ProxyFixed:
		// The array is copied by WebKit; it only has to live for the call.
		strs := make([][]byte, 0, len(pt.proxy.Bypass))
		ptrs := make([]uintptr, 0, len(pt.proxy.Bypass)+1)
		for _, h := range pt.proxy.Bypass {
			c := cstr(h)
			strs = append(strs, c)
			ptrs = append(ptrs, uintptr(unsafe.Pointer(&c[0])))
		}
		ptrs = append(ptrs, 0)
		ignore := uintptr(0)
		if len(pt.proxy.Bypass) > 0 {
			ignore = uintptr(unsafe.Pointer(&ptrs[0]))
		}
		ps := r.proxyNew(pt.proxy.URL, ignore)
		if ps != 0 {
			r.setProxy(pt.mgr, proxyCustom, ps)
			r.proxyFree(ps)
		}
		runtime.KeepAlive(strs)
		runtime.KeepAlive(ptrs)
	}
}

// remoteSpec says a window or view is remote.
type remoteSpec struct {
	policy *caps.RemotePolicy
}

// newRemoteWidget creates the webview of a remote view in the partition's own
// context. It is never related to another view. Main thread.
func (a *App) newRemoteWidget(spec *remoteSpec) (*partition, uintptr, error) {
	pt := a.partitionFor(spec.policy.Partition)
	if err := a.ensure(pt); err != nil {
		return nil, 0, err
	}
	w := a.api.wkViewNewWithContext(pt.ctx)
	if w == 0 {
		return nil, 0, errors.New("krate: webkit_web_view_new_with_context failed")
	}
	return pt, w, nil
}

// setupRemote configures a freshly created remote view: settings, signals,
// message channel, content blocker. Main thread.
func (a *App) setupRemote(v *view, pt *partition, spec *remoteSpec) {
	api, r := a.api, a.rapi
	cur := ""
	if v.rv != nil {
		cur = v.rv.cur // a view recreated after a release keeps its page
	}
	v.rv = &remoteView{policy: spec.policy, part: pt, ua: spec.policy.UserAgent, cur: cur}
	if settings := api.wkViewGetSettings(v.widget); settings != 0 {
		a.applyProfileSettings(settings)
		a.applyRemoteUA(v, settings)
		if r.autoOpen != nil {
			// Popups reach the navigation policy instead of being dropped silently.
			r.autoOpen(settings, 1)
		}
	}
	ref := uintptr(v.ref)
	api.gSignalConnectData(v.widget, "decide-policy", cbRPolicy, ref, 0, 0)
	api.gSignalConnectData(v.widget, "create", cbRCreate, ref, 0, 0)
	api.gSignalConnectData(v.widget, "load-changed", cbRLoad, ref, 0, 0)
	api.gSignalConnectData(v.widget, "close", cbClose, ref, 0, 0)
	api.gSignalConnectData(v.widget, "permission-request", cbRPerm, ref, 0, 0)
	api.gSignalConnectData(v.widget, "authenticate", cbRAuth, ref, 0, 0)
	api.gSignalConnectData(v.widget, "load-failed-with-tls-errors", cbRTLS, ref, 0, 0)
	api.gSignalConnectData(v.widget, "resource-load-started", cbRRes, ref, 0, 0)
	a.connectCrash(v)
	if spec.policy.Messages && r.ucm != nil && r.ucmRegister != nil && r.scriptNew != nil {
		v.rv.token = caps.NewRemoteToken()
		ucm := r.ucm(v.widget)
		if ucm != 0 && r.ucmRegister(ucm, "krateRemote", 0) != 0 {
			api.gSignalConnectData(ucm, "script-message-received::krateRemote", cbRMsg, ref, 0, 0)
			src := caps.RemotePageScript("function(m){window.webkit.messageHandlers.krateRemote.postMessage(m)}", v.rv.token)
			// WEBKIT_USER_CONTENT_INJECT_TOP_FRAME, WEBKIT_USER_SCRIPT_INJECT_AT_DOCUMENT_START
			if sc := r.scriptNew(src, 1, 0, 0, 0); sc != 0 {
				r.ucmAddScript(ucm, sc)
				r.scriptUnref(sc)
			}
		}
	}
	a.addFilter(v)
}

func (a *App) applyRemoteUA(v *view, settings uintptr) {
	r := a.rapi
	ua := v.rv.ua
	if ua == "" {
		ua = v.rv.part.ua
	}
	switch {
	case r.setUA == nil:
	case ua != "":
		r.setUA(settings, ua)
	default:
		r.resetUA(settings, 0)
	}
}

func (a *App) addFilter(v *view) {
	r := a.rapi
	if r.ucm == nil || r.ucmRemoveFilter == nil {
		return
	}
	ucm := r.ucm(v.widget)
	if ucm == 0 {
		return
	}
	r.ucmRemoveFilter(ucm)
	if f := v.rv.part.filter; f != 0 {
		r.ucmAddFilter(ucm, f)
	}
}

// loadRemote starts the first load of a remote view.
func (v *view) loadRemote() {
	if v.widget != 0 {
		v.w.app.api.wkViewLoadURI(v.widget, v.rv.policy.Start)
	}
}

// remoteViews lists the live views of a partition. Main thread.
func (a *App) remoteViews(pt *partition) []*view {
	a.mu.Lock()
	defer a.mu.Unlock()
	var out []*view
	for _, v := range a.views {
		if v.rv != nil && v.rv.part == pt && v.widget != 0 {
			out = append(out, v)
		}
	}
	return out
}

func (a *App) emitRemote(ev caps.RemoteEvent) {
	a.mu.Lock()
	var fn func(caps.RemoteEvent)
	if a.rem != nil {
		fn = a.rem.handler
	}
	a.mu.Unlock()
	if fn != nil {
		fn(ev)
	}
}

// OnRemoteEvent sets the handler for remote events.
func (a *App) OnRemoteEvent(fn func(caps.RemoteEvent)) {
	a.mu.Lock()
	a.rem.handler = fn
	a.mu.Unlock()
}

// NewRemoteWindow opens a window whose primary view shows remote content.
func (a *App) NewRemoteWindow(spec caps.RemoteWindowSpec) (caps.RemoteWindow, error) {
	pol, err := spec.Remote.Resolve()
	if err != nil {
		return nil, err
	}
	var w *Window
	if !a.callMain(func() {
		w, err = a.newWindowRemote(WindowOptions{Title: spec.Title, Width: spec.Width, Height: spec.Height, Resizable: spec.Resizable, Chrome: spec.Chrome}, nil, &remoteSpec{policy: pol})
	}) {
		return nil, errors.New("krate: application is not running")
	}
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
	if !w.app.callMain(func() { ref, err = w.addView(o, &remoteSpec{policy: pol}) }) {
		return 0, errors.New("krate: application is not running")
	}
	return
}

// RemoteOrigin is the origin of the page the view shows now.
func (a *App) RemoteOrigin(ref caps.ViewRef) (origin string) {
	a.callMain(func() {
		if v := a.remote(ref); v != nil {
			origin = v.rv.origin(a)
		}
	})
	return
}

func (rv *remoteView) origin(a *App) string {
	uri := rv.cur
	if a.v.getURI != nil {
		for _, v := range a.views {
			if v.rv == rv && v.widget != 0 {
				if u := goString(a.v.getURI(v.widget)); u != "" {
					uri = u
				}
			}
		}
	}
	return caps.OriginOfString(uri)
}

// NavigateRemote loads a URL in the view if the view's policy allows it.
func (a *App) NavigateRemote(ref caps.ViewRef, raw string) (err error) {
	if !a.callMain(func() {
		v := a.remote(ref)
		switch {
		case v == nil:
			err = krate.Errorf("not_found", "view %d is not a remote view", ref)
		case v.widget == 0:
			err = errReleased
		case v.rv.policy.Decide(raw) != caps.NavAllow || raw == "about:blank":
			err = krate.Errorf("permission_denied", "the view may not navigate to %q", raw)
		default:
			a.api.wkViewLoadURI(v.widget, raw)
		}
	}) {
		return errors.New("krate: application is not running")
	}
	return
}

// PostToRemote delivers a message to the page of a remote view.
func (a *App) PostToRemote(ref caps.ViewRef, target string, data []byte) (err error) {
	if len(data) > caps.MaxRemoteMessage {
		return krate.Errorf("bad_args", "the message is %d bytes; the limit is %d", len(data), caps.MaxRemoteMessage)
	}
	if !a.callMain(func() {
		v := a.remote(ref)
		switch {
		case v == nil:
			err = krate.Errorf("not_found", "view %d is not a remote view", ref)
		case !v.rv.policy.Messages:
			err = krate.Errorf("permission_denied", "messages are not enabled for this view")
		case v.widget == 0:
			err = errReleased
		case target != "*" && v.rv.origin(a) != target:
			err = krate.Errorf("permission_denied", "the page is on %q, not %q", v.rv.origin(a), target)
		default:
			a.api.wkViewEval(v.widget, caps.DeliverToRemoteScript(Origin, target, data), -1, 0, 0, 0, 0, 0)
		}
	}) {
		return errors.New("krate: application is not running")
	}
	return
}

// SetUserAgent sets the user agent of a partition.
func (a *App) SetUserAgent(partition, ua string) (err error) {
	p, err := caps.ParsePartition(partition)
	if err != nil {
		return err
	}
	if strings.ContainsAny(ua, "\r\n\x00") {
		return krate.Errorf("bad_args", "the user agent cannot contain line breaks")
	}
	if a.rapi.setUA == nil {
		return fmt.Errorf("%w: user agents need WebKitGTK 2.x settings", krate.ErrUnsupported)
	}
	a.callMain(func() {
		pt := a.partitionFor(p)
		pt.ua = ua
		for _, v := range a.remoteViews(pt) {
			if settings := a.api.wkViewGetSettings(v.widget); settings != 0 {
				a.applyRemoteUA(v, settings)
			}
		}
	})
	return nil
}

// SetProxy sets the proxy of a partition.
func (a *App) SetProxy(partition string, c caps.ProxyConfig) error {
	p, err := caps.ParsePartition(partition)
	if err != nil {
		return err
	}
	if err := c.Validate(); err != nil {
		return err
	}
	if a.rapi.setProxy == nil || a.rapi.proxyNew == nil {
		return fmt.Errorf("%w: proxies need WebKitGTK 2.16", krate.ErrUnsupported)
	}
	a.callMain(func() {
		pt := a.partitionFor(p)
		pt.proxy = &c
		a.applyProxy(pt)
	})
	return nil
}

// SetClientCertificate makes the partition answer a client certificate
// request of host with the PEM files.
func (a *App) SetClientCertificate(partition, host, certFile, keyFile string) (err error) {
	p, perr := caps.ParsePartition(partition)
	if perr != nil {
		return perr
	}
	if host == "" || strings.ContainsAny(host, "/ ") {
		return krate.Errorf("bad_args", "client certificate host %q is not a host name", host)
	}
	if a.rapi.certFiles == nil {
		return fmt.Errorf("%w: client certificates need GIO TLS", krate.ErrUnsupported)
	}
	a.callMain(func() {
		var gerr uintptr
		cert := a.rapi.certFiles(certFile, keyFile, &gerr)
		if cert == 0 {
			err = a.gerr("cannot load the client certificate", gerr)
			return
		}
		pt := a.partitionFor(p)
		if old := pt.certs[strings.ToLower(host)]; old != 0 {
			a.api.gObjectUnref(old)
		}
		pt.certs[strings.ToLower(host)] = cert
	})
	return
}

// SetCertificateHandler sets the handler for refused certificates.
func (a *App) SetCertificateHandler(fn func(caps.CertError, func(bool))) {
	a.mu.Lock()
	a.rem.certHandler = fn
	a.mu.Unlock()
}

// SetAuthHandler sets the handler for HTTP authentication challenges.
func (a *App) SetAuthHandler(fn func(caps.AuthRequest, func(string, string, bool))) {
	a.mu.Lock()
	a.rem.authHandler = fn
	a.mu.Unlock()
}

// ClearPartition removes stored data of a partition.
func (a *App) ClearPartition(partition string, o caps.ClearSiteDataOptions) error {
	p, err := caps.ParsePartition(partition)
	if err != nil {
		return err
	}
	var types uint32
	all := o.Everything()
	if all || o.Cookies {
		types |= dataCookies
	}
	if all || o.Cache {
		types |= dataMemoryCache | dataDiskCache
	}
	if all || o.Storage {
		types |= dataSession | dataLocal | dataIndexedDB | dataServiceWork | dataDOMCache
	}
	s := a.site
	if s.clearData == nil {
		return fmt.Errorf("%w: clearing site data is not available in this WebKitGTK", krate.ErrUnsupported)
	}
	return a.waitMain("clearing partition data", func(done func(error)) {
		pt := a.partitionFor(p)
		if err := a.ensure(pt); err != nil {
			done(err)
			return
		}
		cb, data := a.async(func(_, res uintptr) {
			var gerr uintptr
			if s.clearFinish(pt.mgr, res, &gerr) == 0 {
				done(a.gerr("cannot clear partition data", gerr))
				return
			}
			done(nil)
		})
		s.clearData(pt.mgr, types, 0, 0, cb, data)
	})
}

// waitMain runs start on the main thread; start calls done once, possibly
// later from the main loop. waitMain returns what done got. Called on the
// main thread it does not wait.
func (a *App) waitMain(what string, start func(done func(error))) error {
	ch := make(chan error, 1)
	var once sync.Once
	done := func(err error) { once.Do(func() { ch <- err }) }
	if !a.callMain(func() { start(done) }) {
		return errors.New("krate: application is not running")
	}
	if a.onMain() {
		return nil
	}
	select {
	case err := <-ch:
		return err
	case <-time.After(30 * time.Second):
		return fmt.Errorf("krate: %s did not finish in time", what)
	}
}

// SetRequestRules replaces the request rules of a partition. Block rules
// become a content blocker (every request of the partition's views); redirect
// and header rules act on navigations.
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
	r := a.rapi
	return a.waitMain("compiling request rules", func(done func(error)) {
		pt := a.partitionFor(p)
		pt.rules = rules
		apply := func(f uintptr) {
			old := pt.filter
			pt.filter = f
			for _, v := range a.remoteViews(pt) {
				a.addFilter(v)
			}
			if old != 0 && r.filterUnref != nil {
				r.filterUnref(old)
			}
			done(nil)
		}
		if blocker == nil {
			apply(0)
			return
		}
		if r.storeSave == nil || r.bytesNew == nil || r.ucmAddFilter == nil || r.storeNew == nil {
			done(fmt.Errorf("%w: blocking subresources needs WebKitGTK 2.30", krate.ErrUnsupported))
			return
		}
		if a.rem.store == 0 {
			dir := filepath.Join(a.dataDir, "remote-filters")
			_ = os.MkdirAll(dir, 0o700)
			a.rem.store = r.storeNew(dir)
		}
		gb := r.bytesNew(unsafe.Pointer(&blocker[0]), uint64(len(blocker)))
		id := "rules-" + pt.p.Key()
		cb, data := a.async(func(src, res uintptr) {
			var gerr uintptr
			f := r.storeSaveFin(src, res, &gerr)
			if f == 0 {
				done(a.gerr("cannot compile the block rules", gerr))
				return
			}
			apply(f)
		})
		r.storeSave(a.rem.store, id, gb, 0, cb, data)
		r.bytesUnref(gb)
		runtime.KeepAlive(blocker)
	})
}

// remotePermissionCallback denies every permission request of a remote page.
func remotePermissionCallback(_, req, _ uintptr) uintptr {
	if a := theApp; a.perm != nil && a.perm.deny != nil {
		a.perm.deny(req)
		return 1
	}
	return 0
}

// remotePolicyCallback is the navigation lock of remote views: every
// navigation and new-window request goes through RemotePolicy.Decide.
func remotePolicyCallback(_, decision, kind, ref uintptr) uintptr {
	a := theApp
	api := a.api
	v := a.viewByRef(ref)
	if v == nil || v.rv == nil {
		api.wkPolicyIgnore(decision)
		return 1
	}
	if kind != policyNavigation && kind != policyNewWindow {
		return 0
	}
	var uri, method string
	var req uintptr
	if action := api.wkNavDecisionAction(decision); action != 0 {
		if req = api.wkNavActionRequest(action); req != 0 {
			uri = goString(api.wkURIRequestURI(req))
			if a.rapi.reqMethod != nil {
				method = goString(a.rapi.reqMethod(req))
			}
		}
	}
	rv := v.rv
	verdict := rv.policy.Decide(uri)
	if verdict != caps.NavAllow {
		api.wkPolicyIgnore(decision)
		a.emitRemote(caps.RemoteEvent{View: v.ref, Kind: caps.RemoteNavBlocked, URL: uri, Verdict: verdict})
		return 1
	}
	res := caps.EvalRules(rv.part.rules, uri)
	switch {
	case res.Block:
		api.wkPolicyIgnore(decision)
		a.emitRemote(caps.RemoteEvent{View: v.ref, Kind: caps.RemoteRequest, URL: uri, Method: method, Action: "block"})
		return 1
	case res.Redirect != "":
		api.wkPolicyIgnore(decision)
		a.emitRemote(caps.RemoteEvent{View: v.ref, Kind: caps.RemoteRequest, URL: uri, Method: method, Action: "redirect"})
		// The target goes through the same checks when it loads.
		api.wkViewLoadURI(v.widget, res.Redirect)
		return 1
	case len(res.Headers) > 0 && req != 0 && a.rapi.reqNew != nil && !a.hasHeaders(req, res.Headers):
		api.wkPolicyIgnore(decision)
		nreq := a.rapi.reqNew(uri)
		if h := a.rapi.reqHeaders(nreq); h != 0 {
			for k, val := range res.Headers {
				a.rapi.headersReplace(h, k, val)
			}
		}
		a.emitRemote(caps.RemoteEvent{View: v.ref, Kind: caps.RemoteRequest, URL: uri, Method: method, Action: "headers"})
		a.rapi.loadRequest(v.widget, nreq)
		api.gObjectUnref(nreq)
		return 1
	}
	if kind == policyNewWindow {
		// A popup the policy allows opens in the view itself.
		api.wkPolicyIgnore(decision)
		api.wkViewLoadURI(v.widget, uri)
		return 1
	}
	return 0
}

func (a *App) hasHeaders(req uintptr, want map[string]string) bool {
	h := a.rapi.reqHeaders(req)
	if h == 0 || a.rapi.headersGetOne == nil {
		return false
	}
	for k, v := range want {
		if goString(a.rapi.headersGetOne(h, k)) != v {
			return false
		}
	}
	return true
}

// remoteCreateCallback handles window.open and target=_blank. A remote view
// never gets a second webview: a URL the policy allows loads in the view
// itself, anything else is refused or handed to the system like a navigation.
// Returning NULL tells the page that no window was opened.
func remoteCreateCallback(_, action, ref uintptr) uintptr {
	a := theApp
	v := a.viewByRef(ref)
	if v == nil || v.rv == nil {
		return 0
	}
	uri := ""
	if req := a.api.wkNavActionRequest(action); req != 0 {
		uri = goString(a.api.wkURIRequestURI(req))
	}
	verdict := v.rv.policy.Decide(uri)
	if verdict != caps.NavAllow || uri == "" || uri == "about:blank" {
		a.emitRemote(caps.RemoteEvent{View: v.ref, Kind: caps.RemoteNavBlocked, URL: uri, Verdict: verdict})
		return 0
	}
	// Loaded after the handler returns: the load goes through the policy
	// callback like any navigation.
	a.Dispatch(func() {
		if v.widget != 0 {
			a.api.wkViewLoadURI(v.widget, uri)
		}
	})
	return 0
}

func remoteLoadCallback(widget, event, ref uintptr) uintptr {
	a := theApp
	v := a.viewByRef(ref)
	if v == nil || v.rv == nil {
		return 0
	}
	switch event {
	case loadCommitted:
		if a.v.getURI != nil {
			v.rv.cur = goString(a.v.getURI(widget))
		}
	case loadFinished:
		if a.v.getURI != nil {
			v.rv.cur = goString(a.v.getURI(widget))
		}
		a.emitRemote(caps.RemoteEvent{View: v.ref, Kind: caps.RemoteLoaded, URL: v.rv.cur, Origin: caps.OriginOfString(v.rv.cur)})
		a.emitView(v, caps.ViewNavigated, v.rv.cur)
	}
	return 0
}

func remoteResourceCallback(_, resource, request, ref uintptr) uintptr {
	a := theApp
	v := a.viewByRef(ref)
	if v == nil || v.rv == nil || a.rapi.resURI == nil {
		return 0
	}
	uri := goString(a.rapi.resURI(resource))
	method := ""
	if request != 0 && a.rapi.reqMethod != nil {
		method = goString(a.rapi.reqMethod(request))
	}
	action := ""
	if res := caps.EvalRules(v.rv.part.rules, uri); res.Block {
		action = "block"
	}
	a.emitRemote(caps.RemoteEvent{View: v.ref, Kind: caps.RemoteRequest, URL: uri, Method: method, Action: action})
	return 0
}

func remoteMessageCallback(_, value, ref uintptr) uintptr {
	a := theApp
	v := a.viewByRef(ref)
	if v == nil || v.rv == nil || v.rv.token == "" || a.v.jscString == nil {
		return 0
	}
	if a.rapi.jsResultValue == nil {
		return 0
	}
	val := a.rapi.jsResultValue(value)
	if val == 0 {
		return 0
	}
	p := a.v.jscString(val)
	if p == 0 {
		return 0
	}
	raw := goString(p)
	a.api.gFree(p)
	data, ok := caps.ParseRemoteMessage(raw, v.rv.token)
	if !ok {
		logf("krate: dropped a remote message without the view's token")
		return 0
	}
	origin := v.rv.origin(a)
	if origin == "" || !v.rv.policy.Messages {
		return 0
	}
	a.emitRemote(caps.RemoteEvent{View: v.ref, Kind: caps.RemoteMessage, Origin: origin, URL: v.rv.cur, Data: data})
	return 0
}

// tlsErrorNames names the bits of GTlsCertificateFlags.
func tlsErrorNames(flags uint32) []string {
	var out []string
	for _, e := range []struct {
		bit  uint32
		name string
	}{{1, "untrusted"}, {2, "mismatch"}, {4, "notYetValid"}, {8, "expired"}, {16, "revoked"}, {32, "insecure"}, {64, "unknown"}} {
		if flags&e.bit != 0 {
			out = append(out, e.name)
		}
	}
	if len(out) == 0 {
		out = []string{"unknown"}
	}
	return out
}

// remoteTLSCallback handles load-failed-with-tls-errors. Without a handler
// the engine's default applies (the load fails).
func remoteTLSCallback(widget, failing, cert, flags, ref uintptr) uintptr {
	a := theApp
	v := a.viewByRef(ref)
	a.mu.Lock()
	h := a.rem.certHandler
	a.mu.Unlock()
	if v == nil || v.rv == nil || h == nil || a.rapi.allowTLS == nil {
		return 0
	}
	uri := goString(failing)
	u, err := url.Parse(uri)
	if err != nil || u.Hostname() == "" {
		return 0
	}
	host := u.Hostname()
	a.api.gObjectRef(cert)
	pt := v.rv.part
	var once sync.Once
	allow := func(ok bool) {
		once.Do(func() {
			a.Dispatch(func() {
				if ok && v.widget != 0 && pt.ctx != 0 {
					a.rapi.allowTLS(pt.ctx, cert, host)
					a.api.wkViewLoadURI(v.widget, uri)
				}
				a.api.gObjectUnref(cert)
			})
		})
	}
	h(caps.CertError{View: v.ref, URL: uri, Host: host, Errors: tlsErrorNames(uint32(flags))}, allow)
	return 1
}

// remoteAuthCallback answers HTTP authentication and client certificate
// challenges.
func remoteAuthCallback(_, req, ref uintptr) uintptr {
	a := theApp
	r := a.rapi
	v := a.viewByRef(ref)
	if v == nil || v.rv == nil || r.authDo == nil {
		return 0
	}
	scheme := r.authScheme(req)
	host := strings.ToLower(goString(r.authHost(req)))
	if scheme == authServerTrust {
		return 0
	}
	if scheme == authClientCert {
		if cert := v.rv.part.certs[host]; cert != 0 && r.credNewCert != nil {
			cred := r.credNewCert(cert, 0)
			r.authDo(req, cred)
			r.credFree(cred)
		} else {
			r.authCancel(req)
		}
		return 1
	}
	a.mu.Lock()
	h := a.rem.authHandler
	a.mu.Unlock()
	if h == nil {
		r.authCancel(req)
		return 1
	}
	a.api.gObjectRef(req)
	var once sync.Once
	answer := func(user, pass string, ok bool) {
		once.Do(func() {
			a.Dispatch(func() {
				if ok {
					cred := r.credNew(user, pass, 0)
					r.authDo(req, cred)
					r.credFree(cred)
				} else {
					r.authCancel(req)
				}
				a.api.gObjectUnref(req)
			})
		})
	}
	schemeName := map[int32]string{2: "basic", 3: "digest", 4: "form", 5: "ntlm", 6: "negotiate"}[scheme]
	if schemeName == "" {
		schemeName = "other"
	}
	port := r.authPort(req)
	h(caps.AuthRequest{View: v.ref, Host: host + ":" + fmt.Sprint(port), URL: v.rv.cur, Realm: goString(r.authRealm(req)),
		Scheme: schemeName, Proxy: r.authProxy(req) != 0, Retry: r.authRetry(req) != 0}, answer)
	return 1
}
