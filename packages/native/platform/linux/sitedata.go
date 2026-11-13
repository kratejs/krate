//go:build linux

package linux

import (
	"errors"
	"fmt"
	"strings"
	"time"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/kratejs/krate/packages/native/krate"
	"github.com/kratejs/krate/packages/native/platform/caps"
)

var _ caps.SiteData = (*App)(nil)

// siteAPI holds the optional cookie and website data bindings.
type siteAPI struct {
	ctxDataMgr  func(ctx uintptr) uintptr
	cookieMgr   func(mgr uintptr) uintptr
	getCookies  func(m uintptr, uri string, cancel, cb, data uintptr)
	getFinish   func(m, res uintptr, gerr *uintptr) uintptr
	getAll      func(m uintptr, cancel, cb, data uintptr)
	getAllFin   func(m, res uintptr, gerr *uintptr) uintptr
	addCookie   func(m, cookie, cancel, cb, data uintptr)
	addFinish   func(m, res uintptr, gerr *uintptr) int32
	delCookie   func(m, cookie, cancel, cb, data uintptr)
	delFinish   func(m, res uintptr, gerr *uintptr) int32
	setPolicy   func(m uintptr, policy int32)
	setStorage  func(m uintptr, file string, kind int32)
	clearData   func(mgr uintptr, types uint32, span int64, cancel, cb, data uintptr)
	clearFinish func(mgr, res uintptr, gerr *uintptr) int32

	cookieNew   func(name, value, domain, path string, maxAge int32) uintptr
	cookieFree  func(c uintptr)
	cName       func(c uintptr) uintptr
	cValue      func(c uintptr) uintptr
	cDomain     func(c uintptr) uintptr
	cPath       func(c uintptr) uintptr
	cExpires    func(c uintptr) uintptr
	cSecure     func(c uintptr) int32
	cHTTPOnly   func(c uintptr) int32
	cSameSite   func(c uintptr) int32
	setSecure   func(c uintptr, on int32)
	setHTTPOnly func(c uintptr, on int32)
	setSameSite func(c uintptr, p int32)
	setExpires  func(c, dt uintptr)
	dtFromUnix  func(t int64) uintptr
	dtToUnix    func(dt uintptr) int64
	dtUnref     func(dt uintptr)
}

func loadSiteAPI() *siteAPI {
	s := &siteAPI{}
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
	wk, soup, glib := open("libwebkit2gtk-4.1.so.0"), open("libsoup-3.0.so.0"), open("libglib-2.0.so.0")
	bind(wk, &s.ctxDataMgr, "webkit_web_context_get_website_data_manager")
	bind(wk, &s.cookieMgr, "webkit_website_data_manager_get_cookie_manager")
	bind(wk, &s.getCookies, "webkit_cookie_manager_get_cookies")
	bind(wk, &s.getFinish, "webkit_cookie_manager_get_cookies_finish")
	bind(wk, &s.getAll, "webkit_cookie_manager_get_all_cookies")
	bind(wk, &s.getAllFin, "webkit_cookie_manager_get_all_cookies_finish")
	bind(wk, &s.addCookie, "webkit_cookie_manager_add_cookie")
	bind(wk, &s.addFinish, "webkit_cookie_manager_add_cookie_finish")
	bind(wk, &s.delCookie, "webkit_cookie_manager_delete_cookie")
	bind(wk, &s.delFinish, "webkit_cookie_manager_delete_cookie_finish")
	bind(wk, &s.setPolicy, "webkit_cookie_manager_set_accept_policy")
	bind(wk, &s.setStorage, "webkit_cookie_manager_set_persistent_storage")
	bind(wk, &s.clearData, "webkit_website_data_manager_clear")
	bind(wk, &s.clearFinish, "webkit_website_data_manager_clear_finish")
	bind(soup, &s.cookieNew, "soup_cookie_new")
	bind(soup, &s.cookieFree, "soup_cookie_free")
	bind(soup, &s.cName, "soup_cookie_get_name")
	bind(soup, &s.cValue, "soup_cookie_get_value")
	bind(soup, &s.cDomain, "soup_cookie_get_domain")
	bind(soup, &s.cPath, "soup_cookie_get_path")
	bind(soup, &s.cExpires, "soup_cookie_get_expires")
	bind(soup, &s.cSecure, "soup_cookie_get_secure")
	bind(soup, &s.cHTTPOnly, "soup_cookie_get_http_only")
	bind(soup, &s.cSameSite, "soup_cookie_get_same_site_policy")
	bind(soup, &s.setSecure, "soup_cookie_set_secure")
	bind(soup, &s.setHTTPOnly, "soup_cookie_set_http_only")
	bind(soup, &s.setSameSite, "soup_cookie_set_same_site_policy")
	bind(soup, &s.setExpires, "soup_cookie_set_expires")
	bind(glib, &s.dtFromUnix, "g_date_time_new_from_unix_utc")
	bind(glib, &s.dtToUnix, "g_date_time_to_unix")
	bind(glib, &s.dtUnref, "g_date_time_unref")
	return s
}

// Values of WebKitCookieAcceptPolicy and WebKitCookiePersistentStorage.
const (
	cookieAcceptAlways = 0
	cookieStorageSQL   = 1
)

// Bits of WebKitWebsiteDataTypes.
const (
	dataMemoryCache = 1 << 0
	dataDiskCache   = 1 << 1
	dataSession     = 1 << 3
	dataLocal       = 1 << 4
	dataIndexedDB   = 1 << 6
	dataCookies     = 1 << 8
	dataServiceWork = 1 << 12
	dataDOMCache    = 1 << 13
)

func (a *App) initSiteData() {
	a.site = loadSiteAPI()
}

// cookieManager returns the cookie manager of the app's data manager, and
// makes cookies persistent and accepted on first use. Main thread.
func (a *App) cookieManager() (m uintptr, err error) {
	s := a.site
	if s.ctxDataMgr == nil || s.cookieMgr == nil || s.cookieNew == nil || s.addCookie == nil {
		return 0, fmt.Errorf("%w: cookies need WebKitGTK 2.40 with libsoup 3", krate.ErrUnsupported)
	}
	if a.cookieMgr == 0 {
		mgr := s.ctxDataMgr(a.ctx)
		if mgr == 0 {
			return 0, errors.New("krate: the web context has no website data manager")
		}
		cm := s.cookieMgr(mgr)
		if cm == 0 {
			return 0, errors.New("krate: the website data manager has no cookie manager")
		}
		s.setPolicy(cm, cookieAcceptAlways)
		if s.setStorage != nil {
			s.setStorage(cm, a.dataDir+"/cookies.sqlite", cookieStorageSQL)
		}
		a.cookieMgr = cm
	}
	return a.cookieMgr, nil
}

// await starts an asynchronous call on the main thread and waits for its
// completion callback, which runs finish on the main thread.
func (a *App) await(what string, start func(m uintptr, cb, data uintptr) error, finish func(m, res uintptr) (any, error)) (any, error) {
	type result struct {
		v   any
		err error
	}
	ch := make(chan result, 1)
	var startErr error
	if !a.callMain(func() {
		m, err := a.cookieManager()
		if err != nil {
			startErr = err
			return
		}
		cb, data := a.async(func(src, res uintptr) {
			v, err := finish(src, res)
			ch <- result{v, err}
		})
		startErr = start(m, cb, data)
	}) {
		return nil, errors.New("krate: application is not running")
	}
	if startErr != nil {
		return nil, startErr
	}
	select {
	case r := <-ch:
		return r.v, r.err
	case <-time.After(30 * time.Second):
		return nil, fmt.Errorf("krate: %s did not finish in time", what)
	}
}

func (a *App) gerr(prefix string, gerr uintptr) error {
	msg := gerrMessage(gerr)
	if gerr != 0 {
		a.api.gErrorFree(gerr)
	}
	return fmt.Errorf("krate: %s: %s", prefix, msg)
}

func originURI(origin string) (string, string, error) {
	return caps.CookieOrigin(origin, krate.ErrUnsupported)
}

// Cookies lists the cookies of an origin.
func (a *App) Cookies(origin string) ([]caps.Cookie, error) {
	uri, host, err := originURI(origin)
	if err != nil {
		return nil, err
	}
	// The cookie jar only answers a URL query for http(s) URLs, so the whole
	// jar is listed and filtered by host. Older WebKitGTK lack that call.
	all := a.site.getAll != nil
	v, err := a.await("listing cookies", func(m, cb, data uintptr) error {
		if all {
			a.site.getAll(m, 0, cb, data)
		} else {
			a.site.getCookies(m, uri, 0, cb, data)
		}
		return nil
	}, func(m, res uintptr) (any, error) {
		var gerr uintptr
		var list uintptr
		if all {
			list = a.site.getAllFin(m, res, &gerr)
		} else {
			list = a.site.getFinish(m, res, &gerr)
		}
		if list == 0 && gerr != 0 {
			return nil, a.gerr("cannot list cookies", gerr)
		}
		var out []caps.Cookie
		for _, c := range a.slistPtrs0(list) {
			ck := a.readCookie(c)
			a.site.cookieFree(c)
			if all && !caps.CookieDomainMatches(ck.Domain, host) {
				continue
			}
			out = append(out, ck)
		}
		if a.pg.glistFree != nil {
			a.pg.glistFree(list)
		}
		return out, nil
	})
	if err != nil {
		return nil, err
	}
	return v.([]caps.Cookie), nil
}

func (a *App) readCookie(c uintptr) caps.Cookie {
	s := a.site
	out := caps.Cookie{
		Name: goString(s.cName(c)), Value: goString(s.cValue(c)),
		Domain: goString(s.cDomain(c)), Path: goString(s.cPath(c)),
		Secure: s.cSecure(c) != 0, HTTPOnly: s.cHTTPOnly(c) != 0,
	}
	if dt := s.cExpires(c); dt != 0 {
		out.Expires = time.Unix(s.dtToUnix(dt), 0).UTC()
	}
	switch s.cSameSite(c) {
	case 0:
		out.SameSite = caps.SameSiteNone
	case 1:
		out.SameSite = caps.SameSiteLax
	case 2:
		out.SameSite = caps.SameSiteStrict
	}
	return out
}

func (a *App) newCookie(origin string, c caps.Cookie) (uintptr, error) {
	_, host, err := originURI(origin)
	if err != nil {
		return 0, err
	}
	if c.Name == "" || strings.ContainsAny(c.Name, "=;, \t\r\n") {
		return 0, krate.Errorf("bad_args", "cookie name %q is not valid", c.Name)
	}
	if strings.ContainsAny(c.Value, ";\r\n") {
		return 0, krate.Errorf("bad_args", "cookie value cannot contain ';' or line breaks")
	}
	domain, path := c.Domain, c.Path
	if domain == "" {
		domain = host
	}
	if path == "" {
		path = "/"
	}
	s := a.site
	ck := s.cookieNew(c.Name, c.Value, domain, path, -1)
	if ck == 0 {
		return 0, errors.New("krate: soup_cookie_new failed")
	}
	s.setSecure(ck, b2i(c.Secure))
	s.setHTTPOnly(ck, b2i(c.HTTPOnly))
	if !c.Expires.IsZero() {
		if dt := s.dtFromUnix(c.Expires.Unix()); dt != 0 {
			s.setExpires(ck, dt)
			s.dtUnref(dt)
		}
	}
	switch c.SameSite {
	case caps.SameSiteNone:
		s.setSameSite(ck, 0)
	case caps.SameSiteLax:
		s.setSameSite(ck, 1)
	case caps.SameSiteStrict:
		s.setSameSite(ck, 2)
	}
	return ck, nil
}

// SetCookie stores a cookie.
func (a *App) SetCookie(origin string, c caps.Cookie) error {
	var ck uintptr
	_, err := a.await("setting a cookie", func(m, cb, data uintptr) error {
		var err error
		if ck, err = a.newCookie(origin, c); err != nil {
			return err
		}
		a.site.addCookie(m, ck, 0, cb, data)
		return nil
	}, func(m, res uintptr) (any, error) {
		var gerr uintptr
		ok := a.site.addFinish(m, res, &gerr)
		a.site.cookieFree(ck)
		if ok == 0 {
			return nil, a.gerr("cannot set the cookie", gerr)
		}
		return nil, nil
	})
	return err
}

// DeleteCookie removes the cookies of the origin with this name (and path and
// domain when c names them). The jar matches a deletion on the cookie's value
// too, so the cookies are looked up first.
func (a *App) DeleteCookie(origin string, c caps.Cookie) error {
	all, err := a.Cookies(origin)
	if err != nil {
		return err
	}
	for _, have := range all {
		if have.Name != c.Name || (c.Path != "" && have.Path != c.Path) || (c.Domain != "" && have.Domain != c.Domain) {
			continue
		}
		var ck uintptr
		_, err := a.await("deleting a cookie", func(m, cb, data uintptr) error {
			var err error
			if ck, err = a.newCookie(origin, have); err != nil {
				return err
			}
			a.site.delCookie(m, ck, 0, cb, data)
			return nil
		}, func(m, res uintptr) (any, error) {
			var gerr uintptr
			ok := a.site.delFinish(m, res, &gerr)
			a.site.cookieFree(ck)
			if ok == 0 {
				return nil, a.gerr("cannot delete the cookie", gerr)
			}
			return nil, nil
		})
		if err != nil {
			return err
		}
	}
	return nil
}

// ClearSiteData removes cookies, caches and storage.
func (a *App) ClearSiteData(o caps.ClearSiteDataOptions) error {
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
	if s.clearData == nil || s.ctxDataMgr == nil {
		return fmt.Errorf("%w: clearing site data is not available in this WebKitGTK", krate.ErrUnsupported)
	}
	_, err := a.await("clearing site data", func(m, cb, data uintptr) error {
		s.clearData(s.ctxDataMgr(a.ctx), types, 0, 0, cb, data)
		return nil
	}, func(_, res uintptr) (any, error) {
		var gerr uintptr
		if s.clearFinish(s.ctxDataMgr(a.ctx), res, &gerr) == 0 {
			return nil, a.gerr("cannot clear site data", gerr)
		}
		return nil, nil
	})
	return err
}

var _ = unsafe.Pointer(nil)
