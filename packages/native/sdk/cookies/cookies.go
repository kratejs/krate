// Package cookies reads and writes the cookies of the webview and clears its
// site data and cache.
//
// The URL of a call names the origin whose cookies are meant and defaults to
// the app origin. The engines on Linux and macOS keep no cookies for the
// custom scheme of the app origin, so there the calls answer
// krate.ErrUnsupported unless a URL of an http(s) site is given, for example
// the API the pages fetch. On Windows the app origin is an https origin and
// has cookies like any other. ClearSiteData works on every platform.
package cookies

import (
	"context"
	"time"

	"github.com/kratejs/krate/packages/native/platform/caps"
	"github.com/kratejs/krate/packages/native/sdk/internal/bridge"
	"github.com/kratejs/krate/packages/native/sdk/internal/codec"
	"github.com/kratejs/krate/packages/native/wire"
)

// Permission is the app permission the UI needs for these operations.
const Permission = "cookies"

// Host function ids.
const (
	IDList          = "krate/cookies.list"
	IDGet           = "krate/cookies.get"
	IDSet           = "krate/cookies.set"
	IDDelete        = "krate/cookies.delete"
	IDClearSiteData = "krate/cookies.clearSiteData"
)

// Cookie is one HTTP cookie.
type Cookie struct {
	Name, Value string
	Domain      string
	Path        string
	// Expires is the zero Time for a session cookie.
	Expires  time.Time
	Secure   bool
	HTTPOnly bool
	// SameSite is "none", "lax", "strict" or "" (not set).
	SameSite string
}

// ClearOptions selects what ClearSiteData removes; all false means all.
type ClearOptions struct {
	Cookies bool
	// Cache is the HTTP and memory cache.
	Cache bool
	// Storage is localStorage, sessionStorage, IndexedDB and service workers.
	Storage bool
}

// List returns the cookies of url (the app origin when empty).
func List(url string) ([]Cookie, error) {
	d, err := bridge.Call(context.Background(), IDList, func(e *wire.Encoder) { e.String(url) })
	if err != nil {
		return nil, err
	}
	r := &codec.R{D: d, Name: "cookie list"}
	var out []Cookie
	r.Array(func() { out = append(out, decodeCookie(r)) })
	return out, r.Err()
}

// Get returns the cookie with this name, or false.
func Get(url, name string) (Cookie, bool, error) {
	d, err := bridge.Call(context.Background(), IDGet, func(e *wire.Encoder) {
		e.String(name)
		e.String(url)
	})
	if err != nil {
		return Cookie{}, false, err
	}
	if d.IsNull() {
		return Cookie{}, false, nil
	}
	r := &codec.R{D: d, Name: "cookie"}
	c := decodeCookie(r)
	return c, true, r.Err()
}

// Set stores a cookie for url.
func Set(url string, c Cookie) error {
	return bridge.CallDo(context.Background(), IDSet, func(e *wire.Encoder) {
		encodeCookie(e, c)
		e.String(url)
	})
}

// Delete removes the cookies of url with this name.
func Delete(url, name string) error {
	return bridge.CallDo(context.Background(), IDDelete, func(e *wire.Encoder) {
		e.String(name)
		e.String(url)
	})
}

// ClearSiteData removes site data and caches.
func ClearSiteData(o ClearOptions) error {
	return bridge.CallDo(context.Background(), IDClearSiteData, func(e *wire.Encoder) {
		e.BeginObject()
		e.Key("cookies")
		e.Bool(o.Cookies)
		e.Key("cache")
		e.Bool(o.Cache)
		e.Key("storage")
		e.Bool(o.Storage)
		e.EndObject()
	})
}

func encodeCookie(e *wire.Encoder, c Cookie) {
	e.BeginObject()
	e.Key("name")
	e.String(c.Name)
	e.Key("value")
	e.String(c.Value)
	e.Key("domain")
	e.String(c.Domain)
	e.Key("path")
	e.String(c.Path)
	e.Key("expires")
	if c.Expires.IsZero() {
		e.Null()
	} else {
		e.Int(c.Expires.UnixMilli())
	}
	e.Key("secure")
	e.Bool(c.Secure)
	e.Key("httpOnly")
	e.Bool(c.HTTPOnly)
	e.Key("sameSite")
	e.String(c.SameSite)
	e.EndObject()
}

func decodeCookie(r *codec.R) (c Cookie) {
	r.Object(func(k string) bool {
		switch k {
		case "name":
			c.Name = r.String()
		case "value":
			c.Value = r.String()
		case "domain":
			c.Domain = r.String()
		case "path":
			c.Path = r.String()
		case "expires":
			if r.D.IsNull() {
				// IsNull consumed the null: a session cookie.
			} else if ms := r.Int(); ms != 0 {
				c.Expires = time.UnixMilli(ms).UTC()
			}
		case "secure":
			c.Secure = r.Bool()
		case "httpOnly":
			c.HTTPOnly = r.Bool()
		case "sameSite":
			c.SameSite = r.String()
		default:
			return false
		}
		return true
	})
	return c
}

func toCaps(c Cookie) caps.Cookie {
	return caps.Cookie{Name: c.Name, Value: c.Value, Domain: c.Domain, Path: c.Path, Expires: c.Expires,
		Secure: c.Secure, HTTPOnly: c.HTTPOnly, SameSite: caps.SameSite(c.SameSite)}
}

func fromCaps(c caps.Cookie) Cookie {
	return Cookie{Name: c.Name, Value: c.Value, Domain: c.Domain, Path: c.Path, Expires: c.Expires,
		Secure: c.Secure, HTTPOnly: c.HTTPOnly, SameSite: string(c.SameSite)}
}
