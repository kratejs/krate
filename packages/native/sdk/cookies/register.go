package cookies

import (
	"fmt"
	"net/url"

	"github.com/kratejs/krate/packages/native/krate"
	"github.com/kratejs/krate/packages/native/platform/caps"
	"github.com/kratejs/krate/packages/native/sdk/internal/bridge"
	"github.com/kratejs/krate/packages/native/sdk/internal/codec"
	"github.com/kratejs/krate/packages/native/wire"
)

// appOrigin is the origin of the app, for calls that name no URL.
func appOrigin(env *bridge.Env) string {
	if o, ok := env.App.(interface{ Origin() string }); ok {
		return o.Origin()
	}
	return ""
}

// checkURL validates a cookie URL: an origin with a scheme and host and no
// credentials.
func checkURL(name, raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" || u.User != nil {
		return krate.Errorf("bad_args", "%s: %q is not a URL with a scheme and host", name, raw)
	}
	return nil
}

var sameSites = map[string]bool{"": true, "none": true, "lax": true, "strict": true}

// Register adds the operations to t.
func Register(t *bridge.Table, env *bridge.Env) {
	sd, hasSD := env.App.(caps.SiteData)
	need := func() error {
		if !hasSD {
			return fmt.Errorf("%w: cookies are not available on this platform", krate.ErrUnsupported)
		}
		return nil
	}
	origin := func(name, given string) (string, error) {
		if given == "" {
			given = appOrigin(env)
		}
		if given == "" {
			return "", krate.Errorf("bad_args", "%s: pass a url; the app has no origin to default to", name)
		}
		return given, checkURL(name, given)
	}

	t.Func(IDList, Permission, true, func(ctx krate.Ctx, args *wire.Decoder, out *wire.Encoder) error {
		r := codec.Args(args, IDList)
		given := ""
		if r.More() {
			given = r.String()
		}
		if err := r.Done(); err != nil {
			return err
		}
		if err := need(); err != nil {
			return err
		}
		o, err := origin(IDList, given)
		if err != nil {
			return err
		}
		list, err := sd.Cookies(o)
		if err != nil {
			return err
		}
		out.BeginArray()
		for _, c := range list {
			encodeCookie(out, fromCaps(c))
		}
		out.EndArray()
		return nil
	})

	t.Func(IDGet, Permission, true, func(ctx krate.Ctx, args *wire.Decoder, out *wire.Encoder) error {
		r := codec.Args(args, IDGet)
		name := r.String()
		given := ""
		if r.More() {
			given = r.String()
		}
		if err := r.Done(); err != nil {
			return err
		}
		if err := need(); err != nil {
			return err
		}
		o, err := origin(IDGet, given)
		if err != nil {
			return err
		}
		list, err := sd.Cookies(o)
		if err != nil {
			return err
		}
		for _, c := range list {
			if c.Name == name {
				encodeCookie(out, fromCaps(c))
				return nil
			}
		}
		out.Null()
		return nil
	})

	t.Func(IDSet, Permission, true, func(ctx krate.Ctx, args *wire.Decoder, out *wire.Encoder) error {
		r := codec.Args(args, IDSet)
		c := decodeCookie(r)
		given := ""
		if r.More() {
			given = r.String()
		}
		if err := r.Done(); err != nil {
			return err
		}
		if c.Name == "" {
			return krate.Errorf("bad_args", "%s: the cookie needs a name", IDSet)
		}
		if !sameSites[c.SameSite] {
			return krate.Errorf("bad_args", "%s: sameSite %q must be none, lax or strict", IDSet, c.SameSite)
		}
		if err := need(); err != nil {
			return err
		}
		o, err := origin(IDSet, given)
		if err != nil {
			return err
		}
		if err := sd.SetCookie(o, toCaps(c)); err != nil {
			return err
		}
		out.Null()
		return nil
	})

	t.Func(IDDelete, Permission, true, func(ctx krate.Ctx, args *wire.Decoder, out *wire.Encoder) error {
		r := codec.Args(args, IDDelete)
		name := r.String()
		given := ""
		if r.More() {
			given = r.String()
		}
		if err := r.Done(); err != nil {
			return err
		}
		if name == "" {
			return krate.Errorf("bad_args", "%s: the cookie needs a name", IDDelete)
		}
		if err := need(); err != nil {
			return err
		}
		o, err := origin(IDDelete, given)
		if err != nil {
			return err
		}
		if err := sd.DeleteCookie(o, caps.Cookie{Name: name}); err != nil {
			return err
		}
		out.Null()
		return nil
	})

	t.Func(IDClearSiteData, Permission, true, func(ctx krate.Ctx, args *wire.Decoder, out *wire.Encoder) error {
		r := codec.Args(args, IDClearSiteData)
		var o caps.ClearSiteDataOptions
		if r.More() {
			r.Object(func(k string) bool {
				switch k {
				case "cookies":
					o.Cookies = r.Bool()
				case "cache":
					o.Cache = r.Bool()
				case "storage":
					o.Storage = r.Bool()
				default:
					return false
				}
				return true
			})
		}
		if err := r.Done(); err != nil {
			return err
		}
		if err := need(); err != nil {
			return err
		}
		if err := sd.ClearSiteData(o); err != nil {
			return err
		}
		out.Null()
		return nil
	})
}
