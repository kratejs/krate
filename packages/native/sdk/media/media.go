// Package media is the camera, microphone, screen capture and location area
// of the Krate Native SDK.
//
// A page asks the webview for these with getUserMedia, getDisplayMedia and
// navigator.geolocation. The platform layer hands every such request to this
// package instead of letting the engine decide, and the answer comes from, in
// order:
//
//  1. the window or view's permission scope: a request whose scope lacks the
//     matching permission ("media" for camera, microphone and screen capture,
//     "location", "notify" for notifications, "clipboard:read") is denied;
//  2. a remembered decision of the view, then of the app;
//  3. the request stream, when the app has a subscriber (OnRequest, or the
//     TypeScript onRequest): it answers allow or deny;
//  4. the policy of the kind from native.media (allow, deny or prompt); a
//     prompt is a native message dialog.
//
// An allowed camera, microphone, screen or location request then goes through
// the operating system's own consent (macOS TCC, the Windows privacy
// settings, the Linux portals); a refusal there denies the request. The
// Info.plist usage descriptions and the MSIX capabilities are written by
// `krate native build` from native.media.usage.
//
// Screen sources: Sources lists screens and windows where the platform can
// (macOS, Windows). No engine accepts a source chosen by the app, so
// ChooseSource returns krate.ErrUnsupported everywhere and getDisplayMedia
// always shows the operating system's picker.
package media

import (
	"context"

	"github.com/kratejs/krate/packages/native/sdk/internal/bridge"
	"github.com/kratejs/krate/packages/native/sdk/internal/codec"
	"github.com/kratejs/krate/packages/native/wire"
)

// Permissions the area uses. Reading the state needs Permission, everything
// that changes it or lists the desktop needs ManagePermission.
const (
	Permission       = "media"
	ManagePermission = "media:manage"
)

// Host function ids.
const (
	IDPolicy       = "krate/media.policy"
	IDSetPolicy    = "krate/media.setPolicy"
	IDDecisions    = "krate/media.decisions"
	IDDecide       = "krate/media.decide"
	IDReset        = "krate/media.reset"
	IDRequests     = "krate/media.requests"
	IDRespond      = "krate/media.respond"
	IDEvents       = "krate/media.events"
	IDOSStatus     = "krate/media.osStatus"
	IDOSRequest    = "krate/media.osRequest"
	IDSources      = "krate/media.sources"
	IDChooseSource = "krate/media.chooseSource"
)

// Source is a screen or window that can be captured.
type Source struct {
	ID   string
	Kind string // "screen" or "window"
	Name string
	// App is the owner of a window, when known.
	App           string
	Width, Height int
	// Thumbnail is a PNG, nil when there is none.
	Thumbnail []byte
}

// Verdict is the answer of an OnRequest handler.
type Verdict struct {
	Allow bool
	// Remember is "app", "view" or "none"; empty follows native.media.
	Remember string
}

// GetPolicy returns the policy of each kind.
func GetPolicy() (map[string]string, error) {
	d, err := bridge.Call(context.Background(), IDPolicy, nil)
	if err != nil {
		return nil, err
	}
	r := &codec.R{D: d, Name: IDPolicy}
	out := map[string]string{}
	r.Object(func(k string) bool { out[k] = r.String(); return true })
	return out, r.Err()
}

// SetPolicy sets what happens to undecided requests of a kind: "prompt",
// "allow" or "deny".
func SetPolicy(kind, policy string) error {
	return bridge.CallDo(context.Background(), IDSetPolicy, func(e *wire.Encoder) { e.String(kind); e.String(policy) })
}

// Decisions lists the remembered decisions.
func Decisions() ([]Decision, error) {
	d, err := bridge.Call(context.Background(), IDDecisions, nil)
	if err != nil {
		return nil, err
	}
	r := &codec.R{D: d, Name: IDDecisions}
	var out []Decision
	r.Array(func() {
		var x Decision
		r.Object(func(k string) bool {
			switch k {
			case "kind":
				x.Kind = Kind(r.String())
			case "scope":
				x.Scope = r.String()
			case "allow":
				x.Allow = r.Bool()
			case "at":
				x.At, _ = r.D.Time()
			default:
				return false
			}
			return true
		})
		out = append(out, x)
	})
	return out, r.Err()
}

// Remember stores a decision for a kind. scope is "app" or the id of a window
// or view.
func Remember(kind, scope string, allow bool) error {
	return bridge.CallDo(context.Background(), IDDecide, func(e *wire.Encoder) { e.String(kind); e.String(scope); e.Bool(allow) })
}

// Reset forgets the remembered decisions of a kind, or of all kinds when kind
// is empty.
func Reset(kind string) error {
	return bridge.CallDo(context.Background(), IDReset, func(e *wire.Encoder) {
		if kind == "" {
			e.Null()
			return
		}
		e.String(kind)
	})
}

// OnRequest decides permission requests until cancel is called. While at
// least one handler is subscribed, a request that no remembered decision
// covers goes to the handlers instead of the policy and the prompt; every
// handler is asked and the first answer wins. fn must answer within a minute
// or the request is denied.
func OnRequest(fn func(Request) Verdict) (cancel func(), err error) {
	return bridge.Subscribe(context.Background(), IDRequests, func(d *wire.Decoder) {
		ev, err := decodeEvent(d)
		if err != nil {
			return
		}
		v := fn(ev.Request)
		_ = bridge.CallDo(context.Background(), IDRespond, func(e *wire.Encoder) {
			e.Uint(ev.ID)
			e.Bool(v.Allow)
			e.String(v.Remember)
		})
	})
}

// OnOutcome calls fn when a request ended, whoever decided it.
func OnOutcome(fn func(Outcome)) (cancel func(), err error) {
	return bridge.Subscribe(context.Background(), IDEvents, func(d *wire.Decoder) {
		if ev, err := decodeEvent(d); err == nil {
			fn(ev)
		}
	})
}

// OSStatus reads the operating system's consent for a kind ("camera",
// "microphone", "screen" or "location") without prompting. The result is one
// of "granted", "denied", "notDetermined", "restricted" or "unknown".
func OSStatus(kind string) (string, error) {
	return stringCall(IDOSStatus, kind)
}

// OSRequest shows the operating system's consent prompt for a kind when it
// has not been answered, and returns the state afterwards.
func OSRequest(kind string) (string, error) {
	return stringCall(IDOSRequest, kind)
}

func stringCall(id, kind string) (string, error) {
	d, err := bridge.Call(context.Background(), id, func(e *wire.Encoder) { e.String(kind) })
	if err != nil {
		return "", err
	}
	return d.String()
}

// Sources lists the screens and windows that can be captured. thumbWidth is
// the thumbnail width in pixels; 0 means none. It returns krate.ErrUnsupported
// where the platform cannot list sources.
func Sources(thumbWidth int) ([]Source, error) {
	d, err := bridge.Call(context.Background(), IDSources, func(e *wire.Encoder) {
		e.BeginObject()
		e.Key("thumbnailWidth")
		e.Int(int64(thumbWidth))
		e.EndObject()
	})
	if err != nil {
		return nil, err
	}
	r := &codec.R{D: d, Name: IDSources}
	var out []Source
	r.Array(func() {
		var s Source
		r.Object(func(k string) bool {
			switch k {
			case "id":
				s.ID = r.String()
			case "kind":
				s.Kind = r.String()
			case "name":
				s.Name = r.String()
			case "app":
				s.App = r.String()
			case "width":
				s.Width = int(r.Int())
			case "height":
				s.Height = int(r.Int())
			case "thumbnail":
				if !r.D.IsNull() {
					s.Thumbnail = r.Bytes()
				}
			default:
				return false
			}
			return true
		})
		out = append(out, s)
	})
	return out, r.Err()
}

// ChooseSource would pick the source of the next getDisplayMedia call. No
// supported engine can be told, so it returns krate.ErrUnsupported; the
// operating system's picker decides.
func ChooseSource(id string) error {
	return bridge.CallDo(context.Background(), IDChooseSource, func(e *wire.Encoder) { e.String(id) })
}

func encodeEvent(e *wire.Encoder, o Outcome, pending bool) {
	e.BeginObject()
	e.Key("id")
	e.Uint(o.ID)
	e.Key("view")
	e.String(o.View)
	e.Key("origin")
	e.String(o.Origin)
	e.Key("kinds")
	e.BeginArray()
	for _, k := range o.Kinds {
		e.String(string(k))
	}
	e.EndArray()
	e.Key("pending")
	e.Bool(pending)
	e.Key("allowed")
	e.Bool(o.Allowed)
	e.Key("reason")
	e.String(o.Reason)
	e.EndObject()
}

func decodeEvent(d *wire.Decoder) (o Outcome, err error) {
	r := &codec.R{D: d, Name: "media event"}
	r.Object(func(k string) bool {
		switch k {
		case "id":
			o.ID = uint64(r.Int())
		case "view":
			o.View = r.String()
		case "origin":
			o.Origin = r.String()
		case "kinds":
			for _, s := range r.Strings() {
				o.Kinds = append(o.Kinds, Kind(s))
			}
		case "pending":
			r.Bool()
		case "allowed":
			o.Allowed = r.Bool()
		case "reason":
			o.Reason = r.String()
		default:
			return false
		}
		return true
	})
	return o, r.Err()
}
