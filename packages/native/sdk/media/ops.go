package media

import (
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/kratejs/krate/packages/native/krate"
	"github.com/kratejs/krate/packages/native/platform/caps"
	"github.com/kratejs/krate/packages/native/sdk/internal/bridge"
	"github.com/kratejs/krate/packages/native/sdk/internal/codec"
	"github.com/kratejs/krate/packages/native/wire"
)

var (
	cfgMu   sync.Mutex
	cfg     Config
	appName string
	prompt  func(Request) (allow, ok bool)
)

// Configure sets native.media for the app. The shell calls it before the
// platform is attached.
func Configure(c Config) {
	cfgMu.Lock()
	cfg = c
	cfgMu.Unlock()
}

// SetAppName sets the name the native prompt shows.
func SetAppName(n string) {
	cfgMu.Lock()
	appName = n
	cfgMu.Unlock()
}

// SetPrompt replaces the native message dialog of the "prompt" policy. fn
// shows its own UI and reports the answer; ok false means it could not ask,
// which denies the request. Host code linked into the shell calls it.
func SetPrompt(fn func(Request) (allow, ok bool)) {
	cfgMu.Lock()
	prompt = fn
	cfgMu.Unlock()
}

// NewForEnv builds the manager of an attached platform and registers its
// handler on it. Tests use it; Register is the normal entry.
func newForEnv(env *bridge.Env, ids *atomic.Uint64) (*Manager, *bridge.Topic, *bridge.Topic) {
	cfgMu.Lock()
	c := cfg
	cfgMu.Unlock()
	reqs, events := env.Topic("mediaRequests"), env.Topic("media")
	h := Host{
		ViewID: func(ref caps.ViewRef) (string, bool) {
			if ref == 0 {
				return "", false
			}
			if r, ok := env.Windows.ViewByRef(ref); ok {
				return r.ID, true
			}
			if r, ok := env.Windows.WaitView(ref, viewWait); ok {
				return r.ID, true
			}
			return "", false
		},
		Allows:   func(view, perm string) bool { return env.Allows != nil && env.Allows(view, perm) },
		Handlers: reqs.Subscribers,
		Publish:  func(r Request) { reqs.Publish(func(e *wire.Encoder) { encodeEvent(e, Outcome{Request: r}, true) }) },
		Notify:   func(o Outcome) { events.Publish(func(e *wire.Encoder) { encodeEvent(e, o, false) }) },
		Prompt: func(r Request) (bool, bool) {
			cfgMu.Lock()
			p, name := prompt, appName
			cfgMu.Unlock()
			if p != nil {
				return p(r)
			}
			return nativePrompt(env, name, r)
		},
	}
	if p, ok := env.App.(caps.Privacy); ok {
		h.Privacy = p
	} else {
		h.Privacy = defaultPrivacy()
	}
	m := NewManager(c, h)
	return m, reqs, events
}

// viewWait is how long a request waits for its view to be registered.
const viewWait = 3 * time.Second

func nativePrompt(env *bridge.Env, name string, r Request) (bool, bool) {
	d, ok := env.App.(caps.Dialogs)
	if !ok {
		return false, false
	}
	var parent caps.WindowRef
	if rec, ok := env.Windows.View(r.View); ok {
		parent = env.Windows.Ref(rec.Window)
	}
	if name == "" {
		name = "This app"
	}
	btn, err := d.MessageDialog(parent, caps.MessageDialogOptions{
		Kind:    caps.MessageQuestion,
		Title:   "Permission request",
		Message: fmt.Sprintf("%s wants to use %s.", name, describeKinds(r.Kinds)),
		Detail:  "The answer is remembered. The app can forget it later.",
		Buttons: []string{"Allow", "Deny"},
		Default: 1,
	})
	if err != nil {
		return false, false
	}
	return btn == 0, true
}

func describeKinds(ks []Kind) string {
	names := map[Kind]string{
		caps.PermCamera: "your camera", caps.PermMicrophone: "your microphone", caps.PermScreen: "screen capture",
		caps.PermLocation: "your location", caps.PermNotifications: "notifications", caps.PermClipboardRead: "the clipboard",
	}
	var parts []string
	for _, k := range ks {
		if n, ok := names[k]; ok {
			parts = append(parts, n)
		}
	}
	switch len(parts) {
	case 0:
		return "a permission"
	case 1:
		return parts[0]
	}
	return strings.Join(parts[:len(parts)-1], ", ") + " and " + parts[len(parts)-1]
}

var nextRequest atomic.Uint64

// Register adds the platform-side operations to t and routes the webview's
// permission requests to the manager.
func Register(t *bridge.Table, env *bridge.Env) {
	m, reqs, events := newForEnv(env, &nextRequest)
	if pr, ok := env.App.(caps.PermissionRequests); ok {
		pr.SetPermissionHandler(m.Handle)
	}
	env.Windows.Subscribe(nil, m.ForgetView)

	t.Stream(IDRequests, ManagePermission, true, reqs.Stream())
	t.Stream(IDEvents, ManagePermission, true, events.Stream())

	t.Func(IDPolicy, Permission, true, func(ctx krate.Ctx, args *wire.Decoder, out *wire.Encoder) error {
		if err := codec.Args(args, IDPolicy).Done(); err != nil {
			return err
		}
		pol := m.Policy()
		out.BeginObject()
		for _, k := range Kinds {
			out.Key(string(k))
			out.String(string(pol[k]))
		}
		out.EndObject()
		return nil
	})
	t.Func(IDSetPolicy, ManagePermission, true, func(ctx krate.Ctx, args *wire.Decoder, out *wire.Encoder) error {
		r := codec.Args(args, IDSetPolicy)
		kind, pol := r.String(), r.String()
		if err := r.Done(); err != nil {
			return err
		}
		out.Null()
		if err := m.SetPolicy(Kind(kind), Policy(pol)); err != nil {
			return krate.Errorf("bad_args", "%w", err)
		}
		return nil
	})
	t.Func(IDDecisions, Permission, true, func(ctx krate.Ctx, args *wire.Decoder, out *wire.Encoder) error {
		if err := codec.Args(args, IDDecisions).Done(); err != nil {
			return err
		}
		out.BeginArray()
		for _, d := range m.Decisions() {
			out.BeginObject()
			out.Key("kind")
			out.String(string(d.Kind))
			out.Key("scope")
			out.String(d.Scope)
			out.Key("allow")
			out.Bool(d.Allow)
			out.Key("at")
			out.Time(d.At)
			out.EndObject()
		}
		out.EndArray()
		return nil
	})
	t.Func(IDDecide, ManagePermission, true, func(ctx krate.Ctx, args *wire.Decoder, out *wire.Encoder) error {
		r := codec.Args(args, IDDecide)
		kind, scope, allow := r.String(), r.String(), r.Bool()
		if err := r.Done(); err != nil {
			return err
		}
		out.Null()
		if err := m.Remember(Kind(kind), scope, allow); err != nil {
			return krate.Errorf("bad_args", "%w", err)
		}
		return nil
	})
	t.Func(IDReset, ManagePermission, true, func(ctx krate.Ctx, args *wire.Decoder, out *wire.Encoder) error {
		r := codec.Args(args, IDReset)
		kind := ""
		if r.More() {
			if !args.IsNull() {
				kind = r.String()
			}
		}
		if err := r.Done(); err != nil {
			return err
		}
		if kind != "" && !ValidKind(kind) {
			return krate.Errorf("bad_args", "%s: unknown permission kind %q", IDReset, kind)
		}
		m.Reset(Kind(kind))
		out.Null()
		return nil
	})
	t.Func(IDRespond, ManagePermission, true, func(ctx krate.Ctx, args *wire.Decoder, out *wire.Encoder) error {
		r := codec.Args(args, IDRespond)
		id, allow := r.Int(), r.Bool()
		remember := ""
		if r.More() {
			remember = r.String()
		}
		if err := r.Done(); err != nil {
			return err
		}
		out.Null()
		if err := m.Respond(uint64(id), allow, remember); err != nil {
			return krate.Errorf("not_found", "%w", err)
		}
		return nil
	})

	privacy := func(args *wire.Decoder, id string) (caps.PermissionKind, caps.Privacy, error) {
		r := codec.Args(args, id)
		kind := r.String()
		if err := r.Done(); err != nil {
			return "", nil, err
		}
		switch kind {
		case "camera", "microphone", "screen", "location":
		default:
			return "", nil, krate.Errorf("bad_args", "%s: kind must be camera, microphone, screen or location, got %q", id, kind)
		}
		if m.host.Privacy == nil {
			return "", nil, fmt.Errorf("%w: this platform has no operating system privacy switch the app can read", krate.ErrUnsupported)
		}
		return caps.PermissionKind(kind), m.host.Privacy, nil
	}
	t.Func(IDOSStatus, Permission, true, func(ctx krate.Ctx, args *wire.Decoder, out *wire.Encoder) error {
		k, p, err := privacy(args, IDOSStatus)
		if err != nil {
			return err
		}
		st, err := p.PrivacyStatus(k)
		if err != nil {
			return err
		}
		out.String(string(st))
		return nil
	})
	t.Func(IDOSRequest, ManagePermission, true, func(ctx krate.Ctx, args *wire.Decoder, out *wire.Encoder) error {
		k, p, err := privacy(args, IDOSRequest)
		if err != nil {
			return err
		}
		st, err := p.RequestPrivacy(k)
		if err != nil {
			return err
		}
		out.String(string(st))
		return nil
	})

	t.Func(IDSources, ManagePermission, true, func(ctx krate.Ctx, args *wire.Decoder, out *wire.Encoder) error {
		r := codec.Args(args, IDSources)
		thumb := 0
		if r.More() {
			r.Object(func(k string) bool {
				if k != "thumbnailWidth" {
					return false
				}
				thumb = int(r.Int())
				return true
			})
		}
		if err := r.Done(); err != nil {
			return err
		}
		if thumb < 0 || thumb > 4096 {
			return krate.Errorf("bad_args", "%s: thumbnailWidth must be between 0 and 4096", IDSources)
		}
		cs, ok := env.App.(caps.CaptureSources)
		if !ok {
			return fmt.Errorf("%w: listing screens and windows is not available on this platform (the system picker of getDisplayMedia still works)", krate.ErrUnsupported)
		}
		list, err := cs.CaptureSources(thumb)
		if err != nil {
			return err
		}
		out.BeginArray()
		for _, s := range list {
			out.BeginObject()
			out.Key("id")
			out.String(s.ID)
			out.Key("kind")
			out.String(s.Kind)
			out.Key("name")
			out.String(s.Name)
			out.Key("app")
			out.String(s.App)
			out.Key("width")
			out.Int(int64(s.Width))
			out.Key("height")
			out.Int(int64(s.Height))
			out.Key("thumbnail")
			if s.Thumbnail == nil {
				out.Null()
			} else {
				out.Bytes(s.Thumbnail)
			}
			out.EndObject()
		}
		out.EndArray()
		return nil
	})
	t.Func(IDChooseSource, ManagePermission, true, func(ctx krate.Ctx, args *wire.Decoder, out *wire.Encoder) error {
		r := codec.Args(args, IDChooseSource)
		id := r.String()
		if err := r.Done(); err != nil {
			return err
		}
		if ch, ok := env.App.(caps.CaptureChooser); ok {
			out.Null()
			return ch.ChooseCaptureSource(0, id)
		}
		return fmt.Errorf("%w: the webview cannot be told which screen or window getDisplayMedia captures; the system picker decides", krate.ErrUnsupported)
	})
}

// Available reports whether the platform routes webview permission requests
// to the SDK.
func Available(env *bridge.Env) bool {
	_, ok := env.App.(caps.PermissionRequests)
	return ok
}
