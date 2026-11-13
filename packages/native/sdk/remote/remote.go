// Package remote shows web pages from the internet in views and windows of
// the app, without giving them anything of the app.
//
// A remote view runs in a storage partition of its own with no handler for the
// app scheme and no host bridge: its page cannot call the SDK, read the app's
// storage or fetch the app origin. It can navigate only to the origins given
// when it was created; everything else opens in the system browser or is
// dropped. The only way the app and a remote page talk is the message channel
// (Post and OnMessage), which is off unless the view asks for it. The model,
// its limits per platform and the enforcement points are in docs/native/remote.
//
// Opening a view needs the "remote" permission, request rules and the request
// stream "remote:intercept", and the user agent, proxy, certificate, login and
// data operations "remote:network".
package remote

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/kratejs/krate/packages/native/sdk/internal/bridge"
	"github.com/kratejs/krate/packages/native/sdk/internal/codec"
	"github.com/kratejs/krate/packages/native/sdk/window"
	"github.com/kratejs/krate/packages/native/wire"
)

// Permissions of the area.
const (
	Permission          = "remote"
	InterceptPermission = "remote:intercept"
	NetworkPermission   = "remote:network"
)

// Host function ids.
const (
	IDOpen          = "krate/remote.open"
	IDAddView       = "krate/remote.addView"
	IDNavigate      = "krate/remote.navigate"
	IDPost          = "krate/remote.post"
	IDMessages      = "krate/remote.messages"
	IDEvents        = "krate/remote.events"
	IDCapabilities  = "krate/remote.capabilities"
	IDSetRules      = "krate/remote.setRules"
	IDRequests      = "krate/remote.requests"
	IDSetUserAgent  = "krate/remote.setUserAgent"
	IDSetProxy      = "krate/remote.setProxy"
	IDSetClientCert = "krate/remote.setClientCertificate"
	IDClearData     = "krate/remote.clearData"
	IDCertificates  = "krate/remote.certificates"
	IDAnswerCert    = "krate/remote.answerCertificate"
	IDAuth          = "krate/remote.auth"
	IDAnswerAuth    = "krate/remote.answerAuth"
)

// Blocked modes: what happens to a navigation the allowlist refuses.
const (
	External = "external"
	Drop     = "block"
)

// Options configures remote content.
type Options struct {
	// URL is the first page, http or https.
	URL string
	// Partition names the storage partition: "" is the default remote
	// partition (persistent, never the app's), "persist:name" a named
	// persistent one, "name" a named in-memory one.
	Partition string
	// Allow lists origins the view may navigate to besides the origin of
	// URL, for example "https://example.com" or "https://*.example.com".
	Allow []string
	// AllowAnyHTTPS lets the view navigate to any https origin.
	AllowAnyHTTPS bool
	// Blocked is External (default) or Drop.
	Blocked string
	// Messages turns on the message channel with the page.
	Messages bool
	// UserAgent overrides the partition's user agent for this view.
	UserAgent string
}

// WindowOptions configures OpenWindow.
type WindowOptions struct {
	Options
	// ID names the window; empty picks "w<N>".
	ID            string
	Title         string
	Width, Height int
	Fixed         bool
}

// ViewOptions configures AddView.
type ViewOptions struct {
	Options
	// ID names the view; empty picks "v<N>".
	ID string
	// Beside, Side, Size, MinWidth and MinHeight are those of window.ViewOptions.
	Beside              string
	Side                string
	Size                float64
	MinWidth, MinHeight int
}

// Message is a message a remote page posted.
type Message struct {
	// View is the id of the view.
	View string
	// Origin is the origin of the page, taken from the engine.
	Origin string
	// Data is the JSON the page posted.
	Data json.RawMessage
}

// Event is a remote view event: a page finished loading ("loaded") or a
// navigation the allowlist refused ("navBlocked").
type Event struct {
	View string
	Kind string
	URL  string
	// Verdict is "external" or "block" for navBlocked.
	Verdict string
}

// Request is a request of a remote view as seen by the platform. Action is
// "", "block", "redirect" or "headers": what the rules did.
type Request struct {
	View   string
	URL    string
	Method string
	Action string
}

// Rule is one request rule of a partition. Action is "block", "redirect"
// (with Redirect) or "headers" (with Headers).
type Rule struct {
	URLs     []string
	Action   string
	Redirect string
	Headers  map[string]string
}

// Level says which requests a feature covers: "" none, "navigation" or "all".
type Level string

// Capabilities report what the platform can do.
type Capabilities struct {
	Observe            Level
	Block              Level
	Redirect           Level
	RequestHeaders     Level
	UserAgent          bool
	Proxy              bool
	CertificateErrors  bool
	ClientCertificates bool
	Auth               bool
	InMemory           bool
}

// Proxy configures a partition's proxy. Mode is "system", "direct" or "fixed".
type Proxy struct {
	Mode   string
	URL    string
	Bypass []string
}

// ClearOptions selects what ClearData removes; all false means everything.
type ClearOptions struct {
	Cookies, Cache, Storage bool
}

// CertificateError is a certificate the engine refused.
type CertificateError struct {
	ID     uint64
	View   string
	URL    string
	Host   string
	Errors []string
}

// AuthChallenge is an HTTP authentication challenge.
type AuthChallenge struct {
	ID     uint64
	View   string
	Host   string
	Realm  string
	Scheme string
	Proxy  bool
	Retry  bool
}

// Credentials answer an AuthChallenge; OK false cancels it.
type Credentials struct {
	User, Password string
	OK             bool
}

func encodeOptions(e *wire.Encoder, o Options) {
	e.Key("url")
	e.String(o.URL)
	e.Key("partition")
	e.String(o.Partition)
	e.Key("allow")
	codec.WriteStrings(e, o.Allow)
	e.Key("allowAnyHttps")
	e.Bool(o.AllowAnyHTTPS)
	e.Key("blocked")
	e.String(o.Blocked)
	e.Key("messages")
	e.Bool(o.Messages)
	e.Key("userAgent")
	e.String(o.UserAgent)
}

// decodeOption reads one field of Options; it reports whether k was one.
func decodeOption(r *codec.R, o *Options, k string) bool {
	switch k {
	case "url":
		o.URL = r.String()
	case "partition":
		o.Partition = r.String()
	case "allow":
		o.Allow = r.Strings()
	case "allowAnyHttps":
		o.AllowAnyHTTPS = r.Bool()
	case "blocked":
		o.Blocked = r.String()
	case "messages":
		o.Messages = r.Bool()
	case "userAgent":
		o.UserAgent = r.String()
	default:
		return false
	}
	return true
}

// OpenWindow opens a window that shows remote content.
func OpenWindow(o WindowOptions) (*window.Window, error) {
	d, err := bridge.Call(context.Background(), IDOpen, func(e *wire.Encoder) {
		e.BeginObject()
		encodeOptions(e, o.Options)
		e.Key("id")
		e.String(o.ID)
		e.Key("title")
		e.String(o.Title)
		e.Key("width")
		e.Int(int64(o.Width))
		e.Key("height")
		e.Int(int64(o.Height))
		e.Key("fixed")
		e.Bool(o.Fixed)
		e.EndObject()
	})
	if err != nil {
		return nil, err
	}
	id, err := d.String()
	if err != nil {
		return nil, err
	}
	return &window.Window{ID: id}, nil
}

// AddView adds a remote view to a window (empty means the main window).
func AddView(windowID string, o ViewOptions) (*window.View, error) {
	d, err := bridge.Call(context.Background(), IDAddView, func(e *wire.Encoder) {
		e.String(windowID)
		e.BeginObject()
		encodeOptions(e, o.Options)
		e.Key("id")
		e.String(o.ID)
		e.Key("beside")
		e.String(o.Beside)
		e.Key("side")
		e.String(o.Side)
		e.Key("size")
		e.Float(o.Size)
		e.Key("minWidth")
		e.Int(int64(o.MinWidth))
		e.Key("minHeight")
		e.Int(int64(o.MinHeight))
		e.EndObject()
	})
	if err != nil {
		return nil, err
	}
	id, err := d.String()
	if err != nil {
		return nil, err
	}
	return &window.View{ID: id}, nil
}

// Navigate loads a URL in a remote view. The URL must be allowed by the
// view's allowlist like a navigation of the page.
func Navigate(view, url string) error {
	return bridge.CallDo(context.Background(), IDNavigate, func(e *wire.Encoder) { e.String(view); e.String(url) })
}

// Post sends a message to the page of a remote view that has Messages on. The
// page receives a "message" event whose origin is the app's. The message is
// delivered only while the page is on targetOrigin ("*" skips the check).
func Post(view string, msg any, targetOrigin string) error {
	b, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("remote: message is not JSON: %w", err)
	}
	return bridge.CallDo(context.Background(), IDPost, func(e *wire.Encoder) {
		e.String(view)
		e.String(targetOrigin)
		e.Raw(b)
	})
}

// OnMessage calls fn for each message a remote page posts until cancel is
// called.
func OnMessage(fn func(Message)) (cancel func(), err error) {
	return bridge.Subscribe(context.Background(), IDMessages, func(d *wire.Decoder) {
		r := &codec.R{D: d, Name: "remote message"}
		var m Message
		r.Object(func(k string) bool {
			switch k {
			case "view":
				m.View = r.String()
			case "origin":
				m.Origin = r.String()
			case "data":
				b, err := d.RawValue()
				if err != nil {
					r.Failf("%v", err)
				}
				m.Data = b
			default:
				return false
			}
			return true
		})
		if r.Err() == nil {
			fn(m)
		}
	})
}

// OnEvent calls fn for remote view events until cancel is called.
func OnEvent(fn func(Event)) (cancel func(), err error) {
	return bridge.Subscribe(context.Background(), IDEvents, func(d *wire.Decoder) {
		r := &codec.R{D: d, Name: "remote event"}
		var ev Event
		r.Object(func(k string) bool {
			switch k {
			case "view":
				ev.View = r.String()
			case "kind":
				ev.Kind = r.String()
			case "url":
				ev.URL = r.String()
			case "verdict":
				ev.Verdict = r.String()
			default:
				return false
			}
			return true
		})
		if r.Err() == nil {
			fn(ev)
		}
	})
}

// OnRequest calls fn for the requests of remote views until cancel is called.
func OnRequest(fn func(Request)) (cancel func(), err error) {
	return bridge.Subscribe(context.Background(), IDRequests, func(d *wire.Decoder) {
		r := &codec.R{D: d, Name: "remote request"}
		var q Request
		r.Object(func(k string) bool {
			switch k {
			case "view":
				q.View = r.String()
			case "url":
				q.URL = r.String()
			case "method":
				q.Method = r.String()
			case "action":
				q.Action = r.String()
			default:
				return false
			}
			return true
		})
		if r.Err() == nil {
			fn(q)
		}
	})
}

// SetRules replaces the request rules of a partition.
func SetRules(partition string, rules []Rule) error {
	return bridge.CallDo(context.Background(), IDSetRules, func(e *wire.Encoder) {
		e.String(partition)
		e.BeginArray()
		for _, r := range rules {
			e.BeginObject()
			e.Key("urls")
			codec.WriteStrings(e, r.URLs)
			e.Key("action")
			e.String(r.Action)
			e.Key("redirect")
			e.String(r.Redirect)
			e.Key("headers")
			e.BeginObject()
			for k, v := range r.Headers {
				e.Key(k)
				e.String(v)
			}
			e.EndObject()
			e.EndObject()
		}
		e.EndArray()
	})
}

// SetUserAgent sets the user agent of a partition; "" restores the default.
func SetUserAgent(partition, ua string) error {
	return bridge.CallDo(context.Background(), IDSetUserAgent, func(e *wire.Encoder) { e.String(partition); e.String(ua) })
}

// SetProxy sets the proxy of a partition.
func SetProxy(partition string, p Proxy) error {
	return bridge.CallDo(context.Background(), IDSetProxy, func(e *wire.Encoder) {
		e.String(partition)
		e.BeginObject()
		e.Key("mode")
		e.String(p.Mode)
		e.Key("url")
		e.String(p.URL)
		e.Key("bypass")
		codec.WriteStrings(e, p.Bypass)
		e.EndObject()
	})
}

// SetClientCertificate makes a partition present a client certificate (PEM
// files) to a host.
func SetClientCertificate(partition, host, certFile, keyFile string) error {
	return bridge.CallDo(context.Background(), IDSetClientCert, func(e *wire.Encoder) {
		e.String(partition)
		e.String(host)
		e.String(certFile)
		e.String(keyFile)
	})
}

// ClearData removes stored data of a partition.
func ClearData(partition string, o ClearOptions) error {
	return bridge.CallDo(context.Background(), IDClearData, func(e *wire.Encoder) {
		e.String(partition)
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

// GetCapabilities reports what the platform can do for remote content.
func GetCapabilities() (Capabilities, error) {
	d, err := bridge.Call(context.Background(), IDCapabilities, nil)
	if err != nil {
		return Capabilities{}, err
	}
	r := &codec.R{D: d, Name: "capabilities"}
	var c Capabilities
	r.Object(func(k string) bool {
		switch k {
		case "observe":
			c.Observe = Level(r.String())
		case "block":
			c.Block = Level(r.String())
		case "redirect":
			c.Redirect = Level(r.String())
		case "requestHeaders":
			c.RequestHeaders = Level(r.String())
		case "userAgent":
			c.UserAgent = r.Bool()
		case "proxy":
			c.Proxy = r.Bool()
		case "certificateErrors":
			c.CertificateErrors = r.Bool()
		case "clientCertificates":
			c.ClientCertificates = r.Bool()
		case "auth":
			c.Auth = r.Bool()
		case "inMemory":
			c.InMemory = r.Bool()
		default:
			return false
		}
		return true
	})
	return c, r.Err()
}

// OnCertificateError decides certificates the engine refused until cancel is
// called. While a handler is subscribed, fn is asked for every such error and
// returns whether to accept the certificate for that host (for this partition);
// without one every error is final. fn must answer within a minute or the
// certificate is refused.
func OnCertificateError(fn func(CertificateError) bool) (cancel func(), err error) {
	return bridge.Subscribe(context.Background(), IDCertificates, func(d *wire.Decoder) {
		r := &codec.R{D: d, Name: "certificate error"}
		var c CertificateError
		r.Object(func(k string) bool {
			switch k {
			case "id":
				c.ID = uint64(r.Int())
			case "view":
				c.View = r.String()
			case "url":
				c.URL = r.String()
			case "host":
				c.Host = r.String()
			case "errors":
				c.Errors = r.Strings()
			default:
				return false
			}
			return true
		})
		if r.Err() != nil {
			return
		}
		allow := fn(c)
		_ = bridge.CallDo(context.Background(), IDAnswerCert, func(e *wire.Encoder) { e.Uint(c.ID); e.Bool(allow) })
	})
}

// OnAuth answers HTTP authentication challenges until cancel is called. While
// a handler is subscribed, fn is asked for each challenge; without one they
// are cancelled. fn must answer within a minute.
func OnAuth(fn func(AuthChallenge) Credentials) (cancel func(), err error) {
	return bridge.Subscribe(context.Background(), IDAuth, func(d *wire.Decoder) {
		r := &codec.R{D: d, Name: "auth challenge"}
		var c AuthChallenge
		r.Object(func(k string) bool {
			switch k {
			case "id":
				c.ID = uint64(r.Int())
			case "view":
				c.View = r.String()
			case "host":
				c.Host = r.String()
			case "realm":
				c.Realm = r.String()
			case "scheme":
				c.Scheme = r.String()
			case "proxy":
				c.Proxy = r.Bool()
			case "retry":
				c.Retry = r.Bool()
			default:
				return false
			}
			return true
		})
		if r.Err() != nil {
			return
		}
		cr := fn(c)
		_ = bridge.CallDo(context.Background(), IDAnswerAuth, func(e *wire.Encoder) {
			e.Uint(c.ID)
			e.String(cr.User)
			e.String(cr.Password)
			e.Bool(cr.OK)
		})
	})
}
