package media

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/kratejs/krate/packages/native/platform/caps"
)

// Kind is what a page asks for; see caps.PermissionKind.
type Kind = caps.PermissionKind

// Kinds lists the kinds the policy covers, in a fixed order.
var Kinds = []Kind{caps.PermCamera, caps.PermMicrophone, caps.PermScreen, caps.PermLocation, caps.PermNotifications, caps.PermClipboardRead}

// Policy is what happens to a request nobody decided yet.
type Policy string

// Policies.
const (
	Prompt Policy = "prompt"
	Allow  Policy = "allow"
	Deny   Policy = "deny"
)

// ValidPolicy reports whether p is one of the three policies.
func ValidPolicy(p string) bool {
	return p == string(Prompt) || p == string(Allow) || p == string(Deny)
}

// ValidKind reports whether k is a kind the policy covers.
func ValidKind(k string) bool {
	for _, x := range Kinds {
		if string(x) == k {
			return true
		}
	}
	return false
}

// RequiredPermission is the app.json permission a window or view needs in its
// scope for a request of this kind. ok is false for a kind no permission
// covers, which is always denied.
func RequiredPermission(k Kind) (perm string, ok bool) {
	switch k {
	case caps.PermCamera, caps.PermMicrophone, caps.PermScreen:
		return "media", true
	case caps.PermLocation:
		return "location", true
	case caps.PermNotifications:
		return "notify", true
	case caps.PermClipboardRead:
		return "clipboard:read", true
	}
	return "", false
}

// Config is native.media.
type Config struct {
	// Policy maps a kind to a policy; a missing kind prompts.
	Policy map[string]string
	// NoRemember stops the answers of the native prompt and of the request
	// stream from being stored unless the answer asks for it.
	NoRemember bool
	// StorePath is the JSON file the app-wide decisions live in; empty keeps
	// them in memory.
	StorePath string
}

// Decision is one remembered answer.
type Decision struct {
	Kind Kind
	// Scope is "app" for an app-wide decision or the id of a window or view.
	Scope string
	Allow bool
	At    time.Time
}

// Request is a permission request as the app sees it.
type Request struct {
	ID uint64
	// View is the SDK id of the window or view that asked.
	View   string
	Origin string
	Kinds  []Kind
}

// Outcome says how a request ended.
type Outcome struct {
	Request
	Allowed bool
	// Reason is "remembered", "policy", "prompt", "handler", "scope",
	// "timeout", "unknown view", "unsupported", "os" or "no handler".
	Reason string
}

// Host is what the manager needs from the process around it. Every field may
// be nil.
type Host struct {
	// ViewID maps a platform view to its SDK id.
	ViewID func(caps.ViewRef) (string, bool)
	// Allows reports whether the window or view has the permission in its
	// scope. Nil denies everything.
	Allows func(view, permission string) bool
	// Handlers is the number of subscribers of the request stream; above zero
	// the manager asks them instead of the policy.
	Handlers func() int
	// Publish sends a pending request to the request stream.
	Publish func(Request)
	// Notify reports how a request ended.
	Notify func(Outcome)
	// Prompt asks the user. ok is false when no prompt can be shown.
	Prompt func(Request) (allow, ok bool)
	// Privacy is the operating system's consent state, when readable.
	Privacy caps.Privacy
	// HandlerTimeout is how long a request waits for the request stream to
	// answer. Zero means 60 seconds.
	HandlerTimeout time.Duration
	Now            func() time.Time
}

// Manager decides permission requests.
type Manager struct {
	cfg  Config
	host Host

	mu       sync.Mutex
	policy   map[Kind]Policy
	app      map[Kind]Decision
	views    map[string]map[Kind]Decision
	pending  map[uint64]chan answer
	loadErr  error
	storeErr error
}

type answer struct {
	allow    bool
	remember string // "app", "view" or "none"
}

// NewManager returns a manager. A StorePath that exists is read; a file that
// cannot be read or parsed is ignored and reported by LoadError.
func NewManager(cfg Config, h Host) *Manager {
	if h.Now == nil {
		h.Now = time.Now
	}
	m := &Manager{cfg: cfg, host: h, policy: map[Kind]Policy{}, app: map[Kind]Decision{},
		views: map[string]map[Kind]Decision{}, pending: map[uint64]chan answer{}}
	for k, p := range cfg.Policy {
		if ValidKind(k) && ValidPolicy(p) {
			m.policy[Kind(k)] = Policy(p)
		}
	}
	m.load()
	return m
}

// LoadError reports why the stored decisions were not read, nil if they were.
func (m *Manager) LoadError() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.loadErr
}

type storeFile struct {
	Decisions []storeEntry `json:"decisions"`
}

type storeEntry struct {
	Kind  Kind      `json:"kind"`
	Allow bool      `json:"allow"`
	At    time.Time `json:"at"`
}

func (m *Manager) load() {
	if m.cfg.StorePath == "" {
		return
	}
	b, err := os.ReadFile(m.cfg.StorePath)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			m.loadErr = err
		}
		return
	}
	var f storeFile
	if err := json.Unmarshal(b, &f); err != nil {
		m.loadErr = fmt.Errorf("media: %s: %w", m.cfg.StorePath, err)
		return
	}
	for _, e := range f.Decisions {
		if ValidKind(string(e.Kind)) {
			m.app[e.Kind] = Decision{Kind: e.Kind, Scope: "app", Allow: e.Allow, At: e.At}
		}
	}
}

// save writes the app-wide decisions. Callers hold mu.
func (m *Manager) save() {
	if m.cfg.StorePath == "" {
		return
	}
	var f storeFile
	for _, d := range m.app {
		f.Decisions = append(f.Decisions, storeEntry{Kind: d.Kind, Allow: d.Allow, At: d.At})
	}
	sort.Slice(f.Decisions, func(i, j int) bool { return f.Decisions[i].Kind < f.Decisions[j].Kind })
	b, err := json.MarshalIndent(f, "", "  ")
	if err == nil {
		err = os.MkdirAll(filepath.Dir(m.cfg.StorePath), 0o700)
	}
	if err == nil {
		tmp := m.cfg.StorePath + ".tmp"
		if err = os.WriteFile(tmp, b, 0o600); err == nil {
			err = os.Rename(tmp, m.cfg.StorePath)
		}
	}
	m.storeErr = err
}

// Policy returns the policy of each kind.
func (m *Manager) Policy() map[Kind]Policy {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make(map[Kind]Policy, len(Kinds))
	for _, k := range Kinds {
		out[k] = m.policyLocked(k)
	}
	return out
}

func (m *Manager) policyLocked(k Kind) Policy {
	if p, ok := m.policy[k]; ok {
		return p
	}
	return Prompt
}

// SetPolicy changes what happens to undecided requests of a kind.
func (m *Manager) SetPolicy(k Kind, p Policy) error {
	if !ValidKind(string(k)) {
		return fmt.Errorf("media: unknown permission kind %q", k)
	}
	if !ValidPolicy(string(p)) {
		return fmt.Errorf("media: unknown policy %q (use prompt, allow or deny)", p)
	}
	m.mu.Lock()
	m.policy[k] = p
	m.mu.Unlock()
	return nil
}

// Decisions lists the remembered decisions, app-wide ones first.
func (m *Manager) Decisions() []Decision {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Decision
	for _, d := range m.app {
		out = append(out, d)
	}
	for _, vs := range m.views {
		for _, d := range vs {
			out = append(out, d)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if (out[i].Scope == "app") != (out[j].Scope == "app") {
			return out[i].Scope == "app"
		}
		if out[i].Scope != out[j].Scope {
			return out[i].Scope < out[j].Scope
		}
		return out[i].Kind < out[j].Kind
	})
	return out
}

// Remember stores a decision. scope is "app" or a window or view id.
func (m *Manager) Remember(k Kind, scope string, allow bool) error {
	if !ValidKind(string(k)) {
		return fmt.Errorf("media: unknown permission kind %q", k)
	}
	if scope == "" {
		scope = "app"
	}
	m.mu.Lock()
	m.rememberLocked(k, scope, allow)
	m.mu.Unlock()
	return nil
}

func (m *Manager) rememberLocked(k Kind, scope string, allow bool) {
	d := Decision{Kind: k, Scope: scope, Allow: allow, At: m.host.Now().UTC().Truncate(time.Second)}
	if scope == "app" {
		m.app[k] = d
		m.save()
		return
	}
	if m.views[scope] == nil {
		m.views[scope] = map[Kind]Decision{}
	}
	m.views[scope][k] = d
}

// Reset forgets remembered decisions: of one kind, or of all when kind is
// empty. The next request of the kind asks again.
func (m *Manager) Reset(kind Kind) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if kind == "" {
		m.app = map[Kind]Decision{}
		m.views = map[string]map[Kind]Decision{}
	} else {
		delete(m.app, kind)
		for _, vs := range m.views {
			delete(vs, kind)
		}
	}
	m.save()
}

// ForgetView drops the decisions of a window or view that went away.
func (m *Manager) ForgetView(id string) {
	m.mu.Lock()
	delete(m.views, id)
	m.mu.Unlock()
}

// remembered looks a kind up: a decision for the view wins over the app's.
func (m *Manager) remembered(k Kind, view string) (Decision, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if d, ok := m.views[view][k]; ok {
		return d, true
	}
	d, ok := m.app[k]
	return d, ok
}

// Handle is the caps.PermissionHandler of the platform. It answers on its own
// goroutine, because mapping the view can wait for the SDK to register it.
func (m *Manager) Handle(req caps.PermissionRequest, answer func(bool)) {
	go func() {
		out := m.Decide(req)
		if m.host.Notify != nil {
			m.host.Notify(out)
		}
		answer(out.Allowed)
	}()
}

// Decide runs the decision pipeline for one request and blocks until it is
// decided.
func (m *Manager) Decide(req caps.PermissionRequest) Outcome {
	r := Request{ID: req.ID, Origin: req.Origin}
	deny := func(reason string) Outcome { return Outcome{Request: r, Reason: reason} }
	if m.host.ViewID != nil {
		id, ok := m.host.ViewID(req.View)
		if !ok {
			return deny("unknown view")
		}
		r.View = id
	}
	seen := map[Kind]bool{}
	for _, k := range req.Kinds {
		if !seen[k] {
			seen[k] = true
			r.Kinds = append(r.Kinds, k)
		}
	}
	if len(r.Kinds) == 0 {
		return deny("unsupported")
	}
	for _, k := range r.Kinds {
		perm, ok := RequiredPermission(k)
		if !ok {
			return deny("unsupported")
		}
		if m.host.Allows == nil || !m.host.Allows(r.View, perm) {
			return deny("scope")
		}
	}

	reason := "remembered"
	var unresolved []Kind
	for _, k := range r.Kinds {
		d, ok := m.remembered(k, r.View)
		switch {
		case !ok:
			unresolved = append(unresolved, k)
		case !d.Allow:
			return deny("remembered")
		}
	}
	if len(unresolved) > 0 {
		var allow bool
		allow, reason = m.askAbout(r, unresolved)
		if !allow {
			return deny(reason)
		}
	}
	if st := m.osCheck(r.Kinds); st != "" {
		return deny("os")
	}
	return Outcome{Request: r, Allowed: true, Reason: reason}
}

// askAbout decides the kinds without a remembered decision.
func (m *Manager) askAbout(r Request, kinds []Kind) (allow bool, reason string) {
	if m.host.Handlers != nil && m.host.Handlers() > 0 && m.host.Publish != nil {
		return m.askHandler(r, kinds)
	}
	var prompt []Kind
	m.mu.Lock()
	for _, k := range kinds {
		switch m.policyLocked(k) {
		case Deny:
			m.mu.Unlock()
			return false, "policy"
		case Prompt:
			prompt = append(prompt, k)
		}
	}
	m.mu.Unlock()
	if len(prompt) == 0 {
		return true, "policy"
	}
	if m.host.Prompt == nil {
		return false, "no handler"
	}
	pr := r
	pr.Kinds = prompt
	ok, shown := m.host.Prompt(pr)
	if !shown {
		return false, "no handler"
	}
	if !m.cfg.NoRemember {
		m.mu.Lock()
		for _, k := range prompt {
			m.rememberLocked(k, "app", ok)
		}
		m.mu.Unlock()
	}
	return ok, "prompt"
}

func (m *Manager) askHandler(r Request, kinds []Kind) (bool, string) {
	ch := make(chan answer, 1)
	m.mu.Lock()
	m.pending[r.ID] = ch
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		delete(m.pending, r.ID)
		m.mu.Unlock()
	}()
	pr := r
	pr.Kinds = kinds
	m.host.Publish(pr)
	d := m.host.HandlerTimeout
	if d <= 0 {
		d = 60 * time.Second
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case a := <-ch:
		scope := ""
		switch a.remember {
		case "app":
			scope = "app"
		case "view":
			scope = r.View
		case "":
			if !m.cfg.NoRemember {
				scope = "app"
			}
		}
		if scope != "" {
			m.mu.Lock()
			for _, k := range kinds {
				m.rememberLocked(k, scope, a.allow)
			}
			m.mu.Unlock()
		}
		return a.allow, "handler"
	case <-t.C:
		return false, "timeout"
	}
}

// Respond answers a request the request stream published. remember is "app",
// "view" or "none"; empty follows native.media (remember unless NoRemember).
func (m *Manager) Respond(id uint64, allow bool, remember string) error {
	switch remember {
	case "", "app", "view", "none":
	default:
		return fmt.Errorf("media: remember must be \"app\", \"view\" or \"none\", got %q", remember)
	}
	m.mu.Lock()
	ch := m.pending[id]
	m.mu.Unlock()
	if ch == nil {
		return fmt.Errorf("media: no pending request %d (answered or timed out)", id)
	}
	select {
	case ch <- answer{allow: allow, remember: remember}:
		return nil
	default:
		return fmt.Errorf("media: request %d is already answered", id)
	}
}

// osCheck asks the operating system's consent for the kinds that have one.
// It returns a non-empty reason when a kind is refused.
func (m *Manager) osCheck(kinds []Kind) string {
	if m.host.Privacy == nil {
		return ""
	}
	for _, k := range kinds {
		switch k {
		case caps.PermCamera, caps.PermMicrophone, caps.PermScreen, caps.PermLocation:
		default:
			continue
		}
		st, err := m.host.Privacy.PrivacyStatus(k)
		if err != nil {
			continue
		}
		if st == caps.PrivacyNotDetermined {
			if st, err = m.host.Privacy.RequestPrivacy(k); err != nil {
				return "os"
			}
		}
		if st == caps.PrivacyDenied || st == caps.PrivacyRestricted || st == caps.PrivacyNotDetermined {
			return string(st)
		}
	}
	return ""
}
