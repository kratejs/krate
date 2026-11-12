package fakeplat

import (
	"errors"
	"fmt"
	"sync"

	"github.com/kratejs/krate/packages/native/platform/caps"
)

var (
	_ caps.RemoteContent = (*App)(nil)
	_ caps.RemoteWindows = (*App)(nil)
	_ caps.RemoteViews   = (*Window)(nil)
)

// remoteState is the fake's remote content: which views are remote and what
// was asked of them.
type remoteState struct {
	mu       sync.Mutex
	policies map[caps.ViewRef]*caps.RemotePolicy
	fn       func(caps.RemoteEvent)
	certFn   func(caps.CertError, func(bool))
	authFn   func(caps.AuthRequest, func(string, string, bool))
	Rules    map[string][]caps.RequestRule
	UA       map[string]string
	Proxies  map[string]caps.ProxyConfig
	Cleared  []string
	Posts    []RemotePost
	Navs     []string
	Origins  map[caps.ViewRef]string
	Certs    []string
}

// RemotePost is a message delivered to a remote view.
type RemotePost struct {
	View   caps.ViewRef
	Target string
	Data   string
}

func (a *App) rs() *remoteState {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.remote == nil {
		a.remote = &remoteState{policies: map[caps.ViewRef]*caps.RemotePolicy{}, Rules: map[string][]caps.RequestRule{},
			UA: map[string]string{}, Proxies: map[string]caps.ProxyConfig{}, Origins: map[caps.ViewRef]string{}}
	}
	return a.remote
}

// Remote returns the fake's record of remote content.
func (a *App) Remote() *remoteState { return a.rs() }

// NewRemoteWindow implements caps.RemoteWindows.
func (a *App) NewRemoteWindow(spec caps.RemoteWindowSpec) (caps.RemoteWindow, error) {
	pol, err := spec.Remote.Resolve()
	if err != nil {
		return nil, err
	}
	w := &Window{app: a, St: caps.WindowState{Width: spec.Width, Height: spec.Height, Title: spec.Title, Decorated: true, Visible: true}}
	a.mu.Lock()
	a.nextRef++
	w.ref = a.nextRef
	a.Windows = append(a.Windows, w)
	a.mu.Unlock()
	pv := w.primary()
	rs := a.rs()
	rs.mu.Lock()
	rs.policies[pv.ref] = pol
	rs.Origins[pv.ref] = caps.OriginOfString(pol.Start)
	rs.mu.Unlock()
	return w, nil
}

// AddRemoteView implements caps.RemoteViews.
func (w *Window) AddRemoteView(o caps.ViewOptions, r caps.RemoteOptions) (caps.ViewRef, error) {
	pol, err := r.Resolve()
	if err != nil {
		return 0, err
	}
	o.Path = pol.Start
	ref, err := w.AddView(o)
	if err != nil {
		return 0, err
	}
	rs := w.app.rs()
	rs.mu.Lock()
	rs.policies[ref] = pol
	rs.Origins[ref] = caps.OriginOfString(pol.Start)
	rs.mu.Unlock()
	return ref, nil
}

func (a *App) RemoteCapabilities() caps.RemoteCaps {
	return caps.RemoteCaps{Observe: caps.LevelAll, Block: caps.LevelAll, Redirect: caps.LevelNavigation,
		RequestHeaders: caps.LevelNavigation, UserAgent: true, Proxy: true, CertificateErrors: true, Auth: true, InMemory: true}
}

func (a *App) IsRemoteView(v caps.ViewRef) bool {
	rs := a.rs()
	rs.mu.Lock()
	defer rs.mu.Unlock()
	return rs.policies[v] != nil
}

func (a *App) RemoteOrigin(v caps.ViewRef) string {
	rs := a.rs()
	rs.mu.Lock()
	defer rs.mu.Unlock()
	return rs.Origins[v]
}

func (a *App) NavigateRemote(v caps.ViewRef, raw string) error {
	rs := a.rs()
	rs.mu.Lock()
	defer rs.mu.Unlock()
	p := rs.policies[v]
	if p == nil {
		return errors.New("not a remote view")
	}
	if p.Decide(raw) != caps.NavAllow {
		return fmt.Errorf("navigation to %q is not allowed", raw)
	}
	rs.Navs = append(rs.Navs, raw)
	rs.Origins[v] = caps.OriginOfString(raw)
	return nil
}

func (a *App) PostToRemote(v caps.ViewRef, target string, data []byte) error {
	rs := a.rs()
	rs.mu.Lock()
	defer rs.mu.Unlock()
	p := rs.policies[v]
	switch {
	case p == nil:
		return errors.New("not a remote view")
	case !p.Messages:
		return errors.New("messages are not enabled")
	case target != "*" && rs.Origins[v] != target:
		return errors.New("wrong target origin")
	}
	rs.Posts = append(rs.Posts, RemotePost{View: v, Target: target, Data: string(data)})
	return nil
}

func (a *App) OnRemoteEvent(fn func(caps.RemoteEvent)) {
	rs := a.rs()
	rs.mu.Lock()
	rs.fn = fn
	rs.mu.Unlock()
}

// EmitRemote delivers a remote event as the platform would.
func (a *App) EmitRemote(e caps.RemoteEvent) {
	rs := a.rs()
	rs.mu.Lock()
	fn := rs.fn
	rs.mu.Unlock()
	if fn != nil {
		fn(e)
	}
}

// AskCertificate runs the certificate handler and returns its decision
// channel.
func (a *App) AskCertificate(e caps.CertError) <-chan bool {
	rs := a.rs()
	rs.mu.Lock()
	fn := rs.certFn
	rs.mu.Unlock()
	ch := make(chan bool, 1)
	if fn == nil {
		ch <- false
		return ch
	}
	fn(e, func(ok bool) { ch <- ok })
	return ch
}

// AskAuth runs the auth handler; the channel gets "user:password" or "".
func (a *App) AskAuth(r caps.AuthRequest) <-chan string {
	rs := a.rs()
	rs.mu.Lock()
	fn := rs.authFn
	rs.mu.Unlock()
	ch := make(chan string, 1)
	if fn == nil {
		ch <- ""
		return ch
	}
	fn(r, func(u, p string, ok bool) {
		if ok {
			ch <- u + ":" + p
		} else {
			ch <- ""
		}
	})
	return ch
}

func (a *App) SetRequestRules(partition string, rules []caps.RequestRule) error {
	if err := caps.ValidateRules(rules); err != nil {
		return err
	}
	rs := a.rs()
	rs.mu.Lock()
	rs.Rules[partition] = rules
	rs.mu.Unlock()
	return nil
}

func (a *App) SetUserAgent(partition, ua string) error {
	rs := a.rs()
	rs.mu.Lock()
	rs.UA[partition] = ua
	rs.mu.Unlock()
	return nil
}

func (a *App) SetProxy(partition string, c caps.ProxyConfig) error {
	if err := c.Validate(); err != nil {
		return err
	}
	rs := a.rs()
	rs.mu.Lock()
	rs.Proxies[partition] = c
	rs.mu.Unlock()
	return nil
}

func (a *App) ClearPartition(partition string, o caps.ClearSiteDataOptions) error {
	rs := a.rs()
	rs.mu.Lock()
	rs.Cleared = append(rs.Cleared, partition)
	rs.mu.Unlock()
	return nil
}

func (a *App) SetCertificateHandler(fn func(caps.CertError, func(bool))) {
	rs := a.rs()
	rs.mu.Lock()
	rs.certFn = fn
	rs.mu.Unlock()
}

func (a *App) SetAuthHandler(fn func(caps.AuthRequest, func(string, string, bool))) {
	rs := a.rs()
	rs.mu.Lock()
	rs.authFn = fn
	rs.mu.Unlock()
}

func (a *App) SetClientCertificate(partition, host, certFile, keyFile string) error {
	rs := a.rs()
	rs.mu.Lock()
	rs.Certs = append(rs.Certs, partition+"|"+host)
	rs.mu.Unlock()
	return nil
}
