package remote

import (
	"fmt"
	"net/url"
	"os/exec"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/kratejs/krate/packages/native/krate"
	"github.com/kratejs/krate/packages/native/platform"
	"github.com/kratejs/krate/packages/native/platform/caps"
	"github.com/kratejs/krate/packages/native/sdk/internal/bridge"
	"github.com/kratejs/krate/packages/native/sdk/internal/codec"
	"github.com/kratejs/krate/packages/native/wire"
)

var idRe = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]{0,31}$`)

// answerTimeout is how long a certificate or login prompt waits for the app.
const answerTimeout = time.Minute

// Available reports whether the platform can host remote content.
func Available(env *bridge.Env) bool {
	_, ok := env.App.(caps.RemoteContent)
	return ok
}

// limiter allows a short burst of external opens per view, then one per
// two seconds, so a page cannot flood the system with browser tabs.
type limiter struct {
	mu   sync.Mutex
	last map[caps.ViewRef]*bucket
}

type bucket struct {
	tokens float64
	at     time.Time
}

func (l *limiter) allow(v caps.ViewRef, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.last == nil {
		l.last = map[caps.ViewRef]*bucket{}
	}
	b := l.last[v]
	if b == nil {
		b = &bucket{tokens: 3, at: now}
		l.last[v] = b
	}
	b.tokens += now.Sub(b.at).Seconds() / 2
	if b.tokens > 3 {
		b.tokens = 3
	}
	b.at = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// openExternal is replaced by tests.
var openExternal = startHandler

// startHandler opens http, https and mailto URLs in the system handler (the
// same rule as os.openExternal; the os package cannot be imported here).
func startHandler(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return err
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
		if u.Host == "" {
			return fmt.Errorf("remote: %q has no host", raw)
		}
	case "mailto":
	default:
		return fmt.Errorf("remote: scheme %q is not opened externally", u.Scheme)
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", raw)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", raw)
	default:
		cmd = exec.Command("xdg-open", raw)
	}
	if cmd.Err != nil {
		return cmd.Err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }() // reap the child
	return nil
}

// Register adds the operations to t.
func Register(t *bridge.Table, env *bridge.Env) {
	rc, has := env.App.(caps.RemoteContent)
	need := func() error {
		if !has {
			return fmt.Errorf("%w: remote content is not available on this platform", krate.ErrUnsupported)
		}
		return nil
	}
	events := env.Topic("remoteevents")
	messages := env.Topic("remotemessages")
	requests := env.Topic("remoterequests")
	certs := env.Topic("remotecerts")
	auths := env.Topic("remoteauth")
	t.Stream(IDEvents, Permission, true, events.Stream())
	t.Stream(IDMessages, Permission, true, messages.Stream())
	t.Stream(IDRequests, InterceptPermission, true, requests.Stream())
	t.Stream(IDCertificates, NetworkPermission, true, certs.Stream())
	t.Stream(IDAuth, NetworkPermission, true, auths.Stream())
	if !has {
		registerOps(t, env, nil, need, nil)
		return
	}

	lim := &limiter{}
	var mu sync.Mutex
	held := map[caps.ViewRef][]caps.RemoteEvent{}
	publish := func(rec bridge.ViewRec, ev caps.RemoteEvent) {
		switch ev.Kind {
		case caps.RemoteMessage:
			messages.Publish(func(e *wire.Encoder) {
				e.BeginObject()
				e.Key("view")
				e.String(rec.ID)
				e.Key("origin")
				e.String(ev.Origin)
				e.Key("data")
				e.Raw(ev.Data)
				e.EndObject()
			})
		case caps.RemoteRequest:
			requests.Publish(func(e *wire.Encoder) {
				e.BeginObject()
				e.Key("view")
				e.String(rec.ID)
				e.Key("url")
				e.String(ev.URL)
				e.Key("method")
				e.String(ev.Method)
				e.Key("action")
				e.String(ev.Action)
				e.EndObject()
			})
		case caps.RemoteLoaded, caps.RemoteNavBlocked:
			events.Publish(func(e *wire.Encoder) {
				e.BeginObject()
				e.Key("view")
				e.String(rec.ID)
				e.Key("kind")
				e.String(string(ev.Kind))
				e.Key("url")
				e.String(ev.URL)
				e.Key("verdict")
				e.String(ev.Verdict.String())
				e.EndObject()
			})
			if ev.Kind == caps.RemoteNavBlocked && ev.Verdict == caps.NavExternal && lim.allow(ev.View, time.Now()) {
				// The user's own browser handles it; a failure (no handler,
				// a scheme that is not allowed) is not the page's concern.
				go func() { _ = openExternal(ev.URL) }()
			}
		}
	}
	rc.OnRemoteEvent(func(ev caps.RemoteEvent) {
		rec, ok := env.Windows.ViewByRef(ev.View)
		if !ok {
			// The view is not registered yet; keep the first events.
			mu.Lock()
			if len(held[ev.View]) < 16 {
				held[ev.View] = append(held[ev.View], ev)
			}
			mu.Unlock()
			return
		}
		publish(rec, ev)
	})
	flush := func(ref caps.ViewRef) {
		rec, ok := env.Windows.ViewByRef(ref)
		if !ok {
			return
		}
		mu.Lock()
		evs := held[ref]
		delete(held, ref)
		mu.Unlock()
		for _, ev := range evs {
			publish(rec, ev)
		}
	}

	// Certificate errors and logins wait for the app while a handler is
	// subscribed and are refused otherwise.
	var pmu sync.Mutex
	var next uint64
	pendingCert := map[uint64]func(bool){}
	pendingAuth := map[uint64]func(user, pass string, ok bool){}
	viewID := func(ref caps.ViewRef) string {
		if rec, ok := env.Windows.ViewByRef(ref); ok {
			return rec.ID
		}
		return ""
	}
	rc.SetCertificateHandler(func(c caps.CertError, allow func(bool)) {
		if certs.Subscribers() == 0 {
			allow(false)
			return
		}
		pmu.Lock()
		next++
		id := next
		pendingCert[id] = allow
		pmu.Unlock()
		time.AfterFunc(answerTimeout, func() {
			pmu.Lock()
			f := pendingCert[id]
			delete(pendingCert, id)
			pmu.Unlock()
			if f != nil {
				f(false)
			}
		})
		certs.Publish(func(e *wire.Encoder) {
			e.BeginObject()
			e.Key("id")
			e.Uint(id)
			e.Key("view")
			e.String(viewID(c.View))
			e.Key("url")
			e.String(c.URL)
			e.Key("host")
			e.String(c.Host)
			e.Key("errors")
			codec.WriteStrings(e, c.Errors)
			e.EndObject()
		})
	})
	rc.SetAuthHandler(func(a caps.AuthRequest, answer func(string, string, bool)) {
		if auths.Subscribers() == 0 {
			answer("", "", false)
			return
		}
		pmu.Lock()
		next++
		id := next
		pendingAuth[id] = answer
		pmu.Unlock()
		time.AfterFunc(answerTimeout, func() {
			pmu.Lock()
			f := pendingAuth[id]
			delete(pendingAuth, id)
			pmu.Unlock()
			if f != nil {
				f("", "", false)
			}
		})
		auths.Publish(func(e *wire.Encoder) {
			e.BeginObject()
			e.Key("id")
			e.Uint(id)
			e.Key("view")
			e.String(viewID(a.View))
			e.Key("host")
			e.String(a.Host)
			e.Key("realm")
			e.String(a.Realm)
			e.Key("scheme")
			e.String(a.Scheme)
			e.Key("proxy")
			e.Bool(a.Proxy)
			e.Key("retry")
			e.Bool(a.Retry)
			e.EndObject()
		})
	})
	answers := &answerOps{
		cert: func(id uint64) func(bool) {
			pmu.Lock()
			defer pmu.Unlock()
			f := pendingCert[id]
			delete(pendingCert, id)
			return f
		},
		auth: func(id uint64) func(string, string, bool) {
			pmu.Lock()
			defer pmu.Unlock()
			f := pendingAuth[id]
			delete(pendingAuth, id)
			return f
		},
	}
	registerOps(t, env, rc, need, &opsHooks{flush: flush, answers: answers})
}

type answerOps struct {
	cert func(id uint64) func(bool)
	auth func(id uint64) func(string, string, bool)
}

type opsHooks struct {
	flush   func(caps.ViewRef)
	answers *answerOps
}

func decodeRemoteOptions(r *codec.R, extra func(k string) bool) (o Options) {
	r.Object(func(k string) bool {
		if decodeOption(r, &o, k) {
			return true
		}
		return extra(k)
	})
	return
}

func (o Options) caps() caps.RemoteOptions {
	return caps.RemoteOptions{URL: o.URL, Partition: o.Partition, Allow: o.Allow, AllowAnyHTTPS: o.AllowAnyHTTPS,
		Blocked: caps.BlockedMode(o.Blocked), Messages: o.Messages, UserAgent: o.UserAgent}
}

func registerOps(t *bridge.Table, env *bridge.Env, rc caps.RemoteContent, need func() error, h *opsHooks) {
	t.Func(IDCapabilities, Permission, true, func(ctx krate.Ctx, args *wire.Decoder, out *wire.Encoder) error {
		if err := codec.Args(args, IDCapabilities).Done(); err != nil {
			return err
		}
		if err := need(); err != nil {
			return err
		}
		c := rc.RemoteCapabilities()
		out.BeginObject()
		for _, f := range []struct {
			k string
			v caps.InterceptLevel
		}{{"observe", c.Observe}, {"block", c.Block}, {"redirect", c.Redirect}, {"requestHeaders", c.RequestHeaders}} {
			out.Key(f.k)
			out.String(string(f.v))
		}
		for _, f := range []struct {
			k string
			v bool
		}{{"userAgent", c.UserAgent}, {"proxy", c.Proxy}, {"certificateErrors", c.CertificateErrors},
			{"clientCertificates", c.ClientCertificates}, {"auth", c.Auth}, {"inMemory", c.InMemory}} {
			out.Key(f.k)
			out.Bool(f.v)
		}
		out.EndObject()
		return nil
	})
	if rc == nil {
		for _, id := range []string{IDOpen, IDAddView, IDNavigate, IDPost, IDSetRules, IDSetUserAgent, IDSetProxy, IDSetClientCert, IDClearData, IDAnswerCert, IDAnswerAuth} {
			perm := Permission
			switch id {
			case IDSetRules:
				perm = InterceptPermission
			case IDSetUserAgent, IDSetProxy, IDSetClientCert, IDClearData, IDAnswerCert, IDAnswerAuth:
				perm = NetworkPermission
			}
			t.Func(id, perm, true, func(krate.Ctx, *wire.Decoder, *wire.Encoder) error { return need() })
		}
		return
	}

	t.Func(IDOpen, Permission, true, func(ctx krate.Ctx, args *wire.Decoder, out *wire.Encoder) error {
		r := codec.Args(args, IDOpen)
		var wid, title string
		var width, height int
		var fixed bool
		o := decodeRemoteOptions(r, func(k string) bool {
			switch k {
			case "id":
				wid = r.String()
			case "title":
				title = r.String()
			case "width":
				width = int(r.Int())
			case "height":
				height = int(r.Int())
			case "fixed":
				fixed = r.Bool()
			default:
				return false
			}
			return true
		})
		if err := r.Done(); err != nil {
			return err
		}
		ro := o.caps()
		if _, err := ro.Resolve(); err != nil {
			return krate.Errorf("bad_args", "%s: %v", IDOpen, err)
		}
		rw, ok := env.App.(caps.RemoteWindows)
		if !ok {
			return fmt.Errorf("%w: remote windows are not available on this platform", krate.ErrUnsupported)
		}
		if width <= 0 {
			width = 1024
		}
		if height <= 0 {
			height = 768
		}
		if wid == "" {
			wid = env.Windows.Reserve()
		} else if !idRe.MatchString(wid) {
			return krate.Errorf("bad_args", "%s: id %q must match %s", IDOpen, wid, idRe)
		} else if env.Windows.IDTaken(wid) {
			return krate.Errorf("already_exists", "an id %q is already in use by a window or view", wid)
		}
		if env.Scope != nil {
			// A remote window has an empty scope: nothing it asks of the SDK
			// is granted, whatever the caller may do.
			if _, err := env.Scope(ctx.Window(), wid, []string{}, true); err != nil {
				return err
			}
		}
		w, err := rw.NewRemoteWindow(caps.RemoteWindowSpec{Title: title, Width: width, Height: height, Resizable: !fixed, Remote: ro})
		if err != nil {
			if env.Unscope != nil {
				env.Unscope(wid)
			}
			return err
		}
		wid = env.Windows.Add(wid, platform.Window(w))
		if wv, ok := w.(caps.WindowViews); ok {
			h.flush(wv.PrimaryView())
		}
		out.String(wid)
		return nil
	})

	t.Func(IDAddView, Permission, true, func(ctx krate.Ctx, args *wire.Decoder, out *wire.Encoder) error {
		r := codec.Args(args, IDAddView)
		winID := r.String()
		var vid, beside, side string
		var size float64
		var minW, minH int
		o := decodeRemoteOptions(r, func(k string) bool {
			switch k {
			case "id":
				vid = r.String()
			case "beside":
				beside = r.String()
			case "side":
				side = r.String()
			case "size":
				size = r.Float()
			case "minWidth":
				minW = int(r.Int())
			case "minHeight":
				minH = int(r.Int())
			default:
				return false
			}
			return true
		})
		if err := r.Done(); err != nil {
			return err
		}
		ro := o.caps()
		if _, err := ro.Resolve(); err != nil {
			return krate.Errorf("bad_args", "%s: %v", IDAddView, err)
		}
		win, winID, ok := env.Windows.Get(winID)
		if !ok {
			return krate.Errorf("not_found", "no window %q", winID)
		}
		rv, ok := win.(caps.RemoteViews)
		if !ok {
			return fmt.Errorf("%w: remote views are not available on this platform", krate.ErrUnsupported)
		}
		if size < 0 || size >= 1 || minW < 0 || minH < 0 {
			return krate.Errorf("bad_args", "%s: size must be between 0 and 1 and minimum sizes cannot be negative", IDAddView)
		}
		co := caps.ViewOptions{Axis: caps.Horizontal, Share: size, MinWidth: minW, MinHeight: minH}
		switch side {
		case "", "right":
		case "left":
			co.NewFirst = true
		case "bottom":
			co.Axis = caps.Vertical
		case "top":
			co.Axis, co.NewFirst = caps.Vertical, true
		default:
			return krate.Errorf("bad_args", "%s: side %q must be right, left, top or bottom", IDAddView, side)
		}
		if beside != "" {
			rec, ok := env.Windows.View(beside)
			if !ok || rec.Window != winID {
				return krate.Errorf("not_found", "no view %q in window %q", beside, winID)
			}
			co.Beside = rec.Ref
		}
		if vid == "" {
			vid = env.Windows.ReserveView()
		} else if !idRe.MatchString(vid) {
			return krate.Errorf("bad_args", "%s: id %q must match %s", IDAddView, vid, idRe)
		} else if env.Windows.IDTaken(vid) {
			return krate.Errorf("already_exists", "an id %q is already in use by a window or view", vid)
		}
		if env.Scope != nil {
			if _, err := env.Scope(ctx.Window(), vid, []string{}, true); err != nil {
				return err
			}
		}
		ref, err := rv.AddRemoteView(co, ro)
		if err != nil {
			if env.Unscope != nil {
				env.Unscope(vid)
			}
			return err
		}
		if err := env.Windows.AddView(vid, winID, ref, false); err != nil {
			if wv, ok := win.(caps.WindowViews); ok {
				_ = wv.RemoveView(ref) // best effort; the registration error is returned
			}
			if env.Unscope != nil {
				env.Unscope(vid)
			}
			return krate.Errorf("already_exists", "%v", err)
		}
		h.flush(ref)
		out.String(vid)
		return nil
	})

	viewRef := func(id string) (caps.ViewRef, error) {
		rec, ok := env.Windows.View(id)
		if !ok || !rc.IsRemoteView(rec.Ref) {
			return 0, krate.Errorf("not_found", "no remote view %q", id)
		}
		return rec.Ref, nil
	}

	t.Func(IDNavigate, Permission, true, func(ctx krate.Ctx, args *wire.Decoder, out *wire.Encoder) error {
		r := codec.Args(args, IDNavigate)
		id, url := r.String(), r.String()
		if err := r.Done(); err != nil {
			return err
		}
		ref, err := viewRef(id)
		if err != nil {
			return err
		}
		if err := rc.NavigateRemote(ref, url); err != nil {
			return err
		}
		out.Null()
		return nil
	})

	t.Func(IDPost, Permission, true, func(ctx krate.Ctx, args *wire.Decoder, out *wire.Encoder) error {
		r := codec.Args(args, IDPost)
		id, target := r.String(), r.String()
		var raw []byte
		if r.Err() == nil {
			b, err := args.RawValue()
			if err != nil {
				r.Failf("%v", err)
			}
			raw = b
		}
		if err := r.Done(); err != nil {
			return err
		}
		if target == "" {
			return krate.Errorf("bad_args", "%s: name the target origin of the page (or \"*\")", IDPost)
		}
		ref, err := viewRef(id)
		if err != nil {
			return err
		}
		if err := rc.PostToRemote(ref, target, raw); err != nil {
			return err
		}
		out.Null()
		return nil
	})

	t.Func(IDSetRules, InterceptPermission, true, func(ctx krate.Ctx, args *wire.Decoder, out *wire.Encoder) error {
		r := codec.Args(args, IDSetRules)
		part := r.String()
		var rules []caps.RequestRule
		r.Array(func() {
			var rule caps.RequestRule
			r.Object(func(k string) bool {
				switch k {
				case "urls":
					rule.URLs = r.Strings()
				case "action":
					rule.Action = caps.RuleAction(r.String())
				case "redirect":
					rule.Redirect = r.String()
				case "headers":
					rule.Headers = map[string]string{}
					r.Object(func(name string) bool { rule.Headers[name] = r.String(); return true })
				default:
					return false
				}
				return true
			})
			rules = append(rules, rule)
		})
		if err := r.Done(); err != nil {
			return err
		}
		if err := rc.SetRequestRules(part, rules); err != nil {
			return wrapBad(IDSetRules, err)
		}
		out.Null()
		return nil
	})

	t.Func(IDSetUserAgent, NetworkPermission, true, func(ctx krate.Ctx, args *wire.Decoder, out *wire.Encoder) error {
		r := codec.Args(args, IDSetUserAgent)
		part, ua := r.String(), r.String()
		if err := r.Done(); err != nil {
			return err
		}
		if err := rc.SetUserAgent(part, ua); err != nil {
			return wrapBad(IDSetUserAgent, err)
		}
		out.Null()
		return nil
	})

	t.Func(IDSetProxy, NetworkPermission, true, func(ctx krate.Ctx, args *wire.Decoder, out *wire.Encoder) error {
		r := codec.Args(args, IDSetProxy)
		part := r.String()
		var c caps.ProxyConfig
		r.Object(func(k string) bool {
			switch k {
			case "mode":
				c.Mode = caps.ProxyMode(r.String())
			case "url":
				c.URL = r.String()
			case "bypass":
				c.Bypass = r.Strings()
			default:
				return false
			}
			return true
		})
		if err := r.Done(); err != nil {
			return err
		}
		if err := rc.SetProxy(part, c); err != nil {
			return wrapBad(IDSetProxy, err)
		}
		out.Null()
		return nil
	})

	t.Func(IDSetClientCert, NetworkPermission, true, func(ctx krate.Ctx, args *wire.Decoder, out *wire.Encoder) error {
		r := codec.Args(args, IDSetClientCert)
		part, host, cert, key := r.String(), r.String(), r.String(), r.String()
		if err := r.Done(); err != nil {
			return err
		}
		if err := rc.SetClientCertificate(part, host, cert, key); err != nil {
			return wrapBad(IDSetClientCert, err)
		}
		out.Null()
		return nil
	})

	t.Func(IDClearData, NetworkPermission, true, func(ctx krate.Ctx, args *wire.Decoder, out *wire.Encoder) error {
		r := codec.Args(args, IDClearData)
		part := r.String()
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
		if err := rc.ClearPartition(part, o); err != nil {
			return wrapBad(IDClearData, err)
		}
		out.Null()
		return nil
	})

	t.Func(IDAnswerCert, NetworkPermission, true, func(ctx krate.Ctx, args *wire.Decoder, out *wire.Encoder) error {
		r := codec.Args(args, IDAnswerCert)
		id, allow := uint64(r.Int()), r.Bool()
		if err := r.Done(); err != nil {
			return err
		}
		f := h.answers.cert(id)
		if f == nil {
			return krate.Errorf("not_found", "no pending certificate error %d (answered or timed out)", id)
		}
		f(allow)
		out.Null()
		return nil
	})

	t.Func(IDAnswerAuth, NetworkPermission, true, func(ctx krate.Ctx, args *wire.Decoder, out *wire.Encoder) error {
		r := codec.Args(args, IDAnswerAuth)
		id, user, pass, ok := uint64(r.Int()), r.String(), r.String(), r.Bool()
		if err := r.Done(); err != nil {
			return err
		}
		f := h.answers.auth(id)
		if f == nil {
			return krate.Errorf("not_found", "no pending login %d (answered or timed out)", id)
		}
		f(user, pass, ok)
		out.Null()
		return nil
	})
}

// wrapBad reports a validation failure of the platform as bad_args and keeps
// the other errors (unsupported, not found) as they are.
func wrapBad(id string, err error) error {
	if krate.IsUnsupported(err) {
		return err
	}
	if _, ok := err.(*krate.Error); ok {
		return err
	}
	return krate.Errorf("bad_args", "%s: %v", id, err)
}
