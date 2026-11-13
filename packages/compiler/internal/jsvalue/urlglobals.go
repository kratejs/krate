package jsvalue

import (
	"net/url"
	"strings"
)

// uriUnreserved lists the characters encodeURIComponent leaves alone.
const uriUnreserved = "-_.!~*'()"

// uriReserved lists the extra characters encodeURI leaves alone.
const uriReserved = ";/?:@&=+$,#"

// encodeURIChars percent-encodes s as JavaScript's encodeURI family does.
func encodeURIChars(s, keep string) string {
	const hex = "0123456789ABCDEF"
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', strings.IndexByte(keep, c) >= 0:
			b.WriteByte(c)
		default:
			b.WriteByte('%')
			b.WriteByte(hex[c>>4])
			b.WriteByte(hex[c&15])
		}
	}
	return b.String()
}

// uriFunc wraps a string-to-string global so an unknown argument stays unknown.
func uriFunc(name string, f func(string) (string, bool)) Value {
	return Func(name, func(args []Value) Value {
		a := arg(args, 0)
		if a.IsUnknown() {
			return a
		}
		p := a.ToPrimitive()
		if p.IsUnknown() {
			return p
		}
		out, ok := f(p.ToString())
		if !ok {
			return Unsupported("URIError: malformed URI sequence")
		}
		return String(out)
	})
}

func decodeURIChars(s string) (string, bool) {
	out, err := url.PathUnescape(s)
	return out, err == nil
}

// urlCtor is the WHATWG URL class for absolute and base-relative URLs.
func urlCtor() Value {
	f := Func("URL", func([]Value) Value { return Unsupported("TypeError: Constructor URL requires 'new'") })
	f.fn.Construct = func(args []Value) Value {
		for _, a := range args {
			if a.IsUnknown() {
				return a
			}
		}
		raw := arg(args, 0).ToString()
		u, err := url.Parse(raw)
		if err != nil {
			return Unsupported("TypeError: Invalid URL")
		}
		if len(args) > 1 && !args[1].IsNullish() {
			base, berr := url.Parse(args[1].ToString())
			if berr != nil || !base.IsAbs() {
				return Unsupported("TypeError: Invalid URL")
			}
			u = base.ResolveReference(u)
		}
		if !u.IsAbs() || u.Host == "" {
			return Unsupported("TypeError: Invalid URL")
		}
		if u.Path == "" {
			u.Path = "/"
		}
		return urlObject(u)
	}
	return f
}

func urlObject(u *url.URL) Value {
	o := NewObject()
	search := ""
	if u.RawQuery != "" {
		search = "?" + u.RawQuery
	}
	hash := ""
	if u.Fragment != "" {
		hash = "#" + u.EscapedFragment()
	}
	port := u.Port()
	href := u.Scheme + "://" + u.Host + u.EscapedPath() + search + hash
	o.Set("href", String(href))
	o.Set("origin", String(u.Scheme+"://"+u.Host))
	o.Set("protocol", String(u.Scheme+":"))
	o.Set("host", String(u.Host))
	o.Set("hostname", String(u.Hostname()))
	o.Set("port", String(port))
	o.Set("pathname", String(u.EscapedPath()))
	o.Set("search", String(search))
	o.Set("hash", String(hash))
	o.Set("toString", Func("toString", func([]Value) Value { return String(href) }))
	o.Set("toJSON", Func("toJSON", func([]Value) Value { return String(href) }))
	params := NewObject()
	q := u.Query()
	params.Set("get", Func("get", func(args []Value) Value {
		if a := arg(args, 0); a.IsUnknown() {
			return a
		}
		vals, ok := q[arg(args, 0).ToString()]
		if !ok || len(vals) == 0 {
			return Null()
		}
		return String(vals[0])
	}))
	params.Set("has", Func("has", func(args []Value) Value {
		if a := arg(args, 0); a.IsUnknown() {
			return a
		}
		_, ok := q[arg(args, 0).ToString()]
		return Bool(ok)
	}))
	o.Set("searchParams", params)
	return o
}
