//go:build darwin

package darwin

// Cookies and site data on WKWebsiteDataStore (compile-only until CI runs
// gui_page_test.go on macos-latest). WebKit keeps no cookies for the app's
// custom scheme, so the calls take http(s) origins.

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ebitengine/purego/objc"

	"github.com/kratejs/krate/packages/native/krate"
	"github.com/kratejs/krate/packages/native/platform/caps"
)

var _ caps.SiteData = (*App)(nil)

func jsQuote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// dataStore returns the default website data store, which every web view of
// the app uses.
func dataStore() objc.ID {
	return cls("WKWebsiteDataStore").Send(sel("defaultDataStore"))
}

// storeCall starts an asynchronous call on the main thread and waits for the
// completion block to send a result.
func (a *App) storeCall(what string, start func(store objc.ID, done func(any)) error) (any, error) {
	ch := make(chan any, 1)
	err := a.mainCall(func() error {
		store := dataStore()
		if store == 0 {
			return errNoStore
		}
		return start(store, func(v any) { ch <- v })
	})
	if err != nil {
		return nil, err
	}
	select {
	case v := <-ch:
		return v, nil
	case <-time.After(30 * time.Second):
		return nil, fmt.Errorf("darwin: %s did not finish in time", what)
	}
}

// listCookies calls fn with every cookie of the store, on the main thread.
func listCookies(store objc.ID, fn func(cookies []objc.ID)) {
	jar := store.Send(sel("httpCookieStore"))
	blk := objc.NewBlock(func(_ objc.Block, arr objc.ID) {
		var out []objc.ID
		if arr != 0 {
			n := objc.Send[uint](arr, sel("count"))
			for i := uint(0); i < n; i++ {
				c := arr.Send(sel("objectAtIndex:"), i)
				retain(c)
				out = append(out, c)
			}
		}
		fn(out)
	})
	jar.Send(sel("getAllCookies:"), objc.ID(blk))
	blk.Release()
}

func readCookie(c objc.ID) caps.Cookie {
	out := caps.Cookie{
		Name:     goString(c.Send(sel("name"))),
		Value:    goString(c.Send(sel("value"))),
		Domain:   goString(c.Send(sel("domain"))),
		Path:     goString(c.Send(sel("path"))),
		Secure:   objc.Send[bool](c, sel("isSecure")),
		HTTPOnly: objc.Send[bool](c, sel("isHTTPOnly")),
	}
	if d := c.Send(sel("expiresDate")); d != 0 {
		out.Expires = time.Unix(int64(objc.Send[float64](d, sel("timeIntervalSince1970"))), 0).UTC()
	}
	if respondsTo(c, sel("sameSitePolicy")) {
		if s := c.Send(sel("sameSitePolicy")); s != 0 {
			switch strings.ToLower(goString(s)) {
			case "lax":
				out.SameSite = caps.SameSiteLax
			case "strict":
				out.SameSite = caps.SameSiteStrict
			}
		}
	}
	return out
}

// Cookies lists the cookies that would be sent to origin.
func (a *App) Cookies(origin string) ([]caps.Cookie, error) {
	_, host, err := caps.CookieOrigin(origin, krate.ErrUnsupported)
	if err != nil {
		return nil, err
	}
	v, err := a.storeCall("listing cookies", func(store objc.ID, done func(any)) error {
		listCookies(store, func(cs []objc.ID) {
			out := []caps.Cookie{}
			for _, c := range cs {
				if ck := readCookie(c); caps.CookieDomainMatches(ck.Domain, host) {
					out = append(out, ck)
				}
				release(c)
			}
			done(out)
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	return v.([]caps.Cookie), nil
}

// SetCookie stores a cookie.
func (a *App) SetCookie(origin string, c caps.Cookie) error {
	_, host, err := caps.CookieOrigin(origin, krate.ErrUnsupported)
	if err != nil {
		return err
	}
	if c.Name == "" || strings.ContainsAny(c.Name, "=;, \t\r\n") {
		return krate.Errorf("bad_args", "cookie name %q is not valid", c.Name)
	}
	domain, path := c.Domain, c.Path
	if domain == "" {
		domain = host
	}
	if path == "" {
		path = "/"
	}
	_, err = a.storeCall("setting a cookie", func(store objc.ID, done func(any)) error {
		props := cls("NSMutableDictionary").Send(sel("dictionaryWithCapacity:"), 8)
		set := func(k string, v objc.ID) { props.Send(sel("setObject:forKey:"), v, nsString(k)) }
		set("Name", nsString(c.Name))
		set("Value", nsString(c.Value))
		set("Domain", nsString(domain))
		set("Path", nsString(path))
		if !c.Expires.IsZero() {
			set("Expires", cls("NSDate").Send(sel("dateWithTimeIntervalSince1970:"), float64(c.Expires.Unix())))
		}
		if c.Secure {
			set("Secure", nsString("TRUE"))
		}
		if c.HTTPOnly {
			set("HttpOnly", nsString("TRUE"))
		}
		switch c.SameSite {
		case caps.SameSiteLax:
			set("SameSite", nsString("lax"))
		case caps.SameSiteStrict:
			set("SameSite", nsString("strict"))
		}
		ck := cls("NSHTTPCookie").Send(sel("cookieWithProperties:"), props)
		if ck == 0 {
			return errors.New("darwin: the cookie properties are not valid")
		}
		blk := objc.NewBlock(func(_ objc.Block) { done(nil) })
		store.Send(sel("httpCookieStore")).Send(sel("setCookie:completionHandler:"), ck, objc.ID(blk))
		blk.Release()
		return nil
	})
	return err
}

// DeleteCookie removes the cookies of the origin with this name.
func (a *App) DeleteCookie(origin string, c caps.Cookie) error {
	_, host, err := caps.CookieOrigin(origin, krate.ErrUnsupported)
	if err != nil {
		return err
	}
	_, err = a.storeCall("deleting a cookie", func(store objc.ID, done func(any)) error {
		listCookies(store, func(cs []objc.ID) {
			jar := store.Send(sel("httpCookieStore"))
			var doomed []objc.ID
			for _, ck := range cs {
				r := readCookie(ck)
				if r.Name == c.Name && caps.CookieDomainMatches(r.Domain, host) {
					doomed = append(doomed, ck)
				} else {
					release(ck)
				}
			}
			if len(doomed) == 0 {
				done(nil)
				return
			}
			left := len(doomed)
			for _, ck := range doomed {
				ck := ck
				blk := objc.NewBlock(func(_ objc.Block) {
					release(ck)
					if left--; left == 0 {
						done(nil)
					}
				})
				jar.Send(sel("deleteCookie:completionHandler:"), ck, objc.ID(blk))
				blk.Release()
			}
		})
		return nil
	})
	return err
}

// ClearSiteData removes cookies, caches and storage.
func (a *App) ClearSiteData(o caps.ClearSiteDataOptions) error {
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
	_, err := a.storeCall("clearing site data", func(store objc.ID, done func(any)) error {
		set := cls("NSMutableSet").Send(sel("set"))
		for _, k := range kinds {
			set.Send(sel("addObject:"), nsString(k))
		}
		since := cls("NSDate").Send(sel("dateWithTimeIntervalSince1970:"), 0.0)
		blk := objc.NewBlock(func(_ objc.Block) { done(nil) })
		store.Send(sel("removeDataOfTypes:modifiedSince:completionHandler:"), set, since, objc.ID(blk))
		blk.Release()
		return nil
	})
	return err
}
