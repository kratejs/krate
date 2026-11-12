package caps

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

// Remote content: views and windows that show pages from the web instead of
// the app. The security model is in docs/native/remote.md; the enforcement
// points are few on purpose:
//
//  1. Engine isolation. A remote webview is created in its own web context
//     (WebKitWebContext, WKWebViewConfiguration, WebView2 profile) that has no
//     handler for the app scheme and no user script of the app, so it can
//     neither reach the app origin nor the host bridge.
//  2. RemotePolicy.Decide. Every navigation and new-window request of a remote
//     view goes through this one function, on all platforms.
//  3. IsRemoteView. The shell maps a request tagged with a remote view to an
//     empty permission scope, whatever it claims.
//  4. The message channel (RemoteContent.PostToRemote and RemoteMessage
//     events): the only way the app and a remote page talk, opt in per view.

// BlockedMode says what happens to a navigation the allowlist refuses.
type BlockedMode string

// Blocked modes.
const (
	// BlockedExternal opens http, https, mailto and tel URLs in the system
	// handler and drops everything else.
	BlockedExternal BlockedMode = "external"
	// BlockedDrop drops the navigation.
	BlockedDrop BlockedMode = "block"
)

// RemoteOptions configures a remote view or window.
type RemoteOptions struct {
	// URL is the first page: an http or https URL.
	URL string
	// Partition names the storage partition; see ParsePartition. Empty is the
	// default remote partition, which is never the app's.
	Partition string
	// Allow lists the origins the view may navigate to besides the origin of
	// URL: "https://example.com", "https://*.example.com" (subdomains only),
	// "http://127.0.0.1:8080".
	Allow []string
	// AllowAnyHTTPS lets the view navigate to any https origin.
	AllowAnyHTTPS bool
	// Blocked says what happens to other navigations; empty is BlockedExternal.
	Blocked BlockedMode
	// Messages enables the message channel with the page of this view.
	Messages bool
	// UserAgent overrides the partition's user agent for this view.
	UserAgent string
}

// NavVerdict is the outcome of a navigation check.
type NavVerdict int

// Verdicts.
const (
	// NavAllow lets the navigation happen in the view.
	NavAllow NavVerdict = iota
	// NavExternal cancels it and opens the URL in the system handler.
	NavExternal
	// NavBlock cancels it.
	NavBlock
)

func (v NavVerdict) String() string {
	switch v {
	case NavAllow:
		return "allow"
	case NavExternal:
		return "external"
	}
	return "block"
}

// RemotePolicy is the resolved form of RemoteOptions. It is immutable.
type RemotePolicy struct {
	Start       string
	Partition   Partition
	Messages    bool
	UserAgent   string
	Blocked     BlockedMode
	anyHTTPS    bool
	exact       map[string]bool
	wildcards   []originPattern
	startOrigin string
}

type originPattern struct {
	scheme, suffix, port string
}

// Resolve validates the options. It is the only constructor of a policy.
func (o RemoteOptions) Resolve() (*RemotePolicy, error) {
	u, err := url.Parse(o.URL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil {
		return nil, fmt.Errorf("krate: remote URL %q must be an http or https URL without credentials", o.URL)
	}
	part, err := ParsePartition(o.Partition)
	if err != nil {
		return nil, err
	}
	p := &RemotePolicy{Start: u.String(), Partition: part, Messages: o.Messages, UserAgent: o.UserAgent,
		Blocked: o.Blocked, anyHTTPS: o.AllowAnyHTTPS, exact: map[string]bool{}}
	switch p.Blocked {
	case "":
		p.Blocked = BlockedExternal
	case BlockedExternal, BlockedDrop:
	default:
		return nil, fmt.Errorf("krate: remote blocked mode %q must be %q or %q", o.Blocked, BlockedExternal, BlockedDrop)
	}
	p.startOrigin = OriginOf(u)
	p.exact[p.startOrigin] = true
	if len(o.Allow) > 64 {
		return nil, errors.New("krate: at most 64 allowed origins")
	}
	for _, a := range o.Allow {
		if err := p.addOrigin(a); err != nil {
			return nil, err
		}
	}
	return p, nil
}

func (p *RemotePolicy) addOrigin(a string) error {
	u, err := url.Parse(a)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil ||
		(u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" {
		return fmt.Errorf("krate: allowed origin %q must look like https://example.com or https://*.example.com", a)
	}
	host := strings.ToLower(u.Hostname())
	if strings.HasPrefix(host, "*.") {
		suffix := host[1:]
		if strings.Contains(suffix[1:], "*") || strings.Count(suffix, ".") < 2 {
			return fmt.Errorf("krate: allowed origin %q: a wildcard needs a registrable domain after it", a)
		}
		p.wildcards = append(p.wildcards, originPattern{scheme: u.Scheme, suffix: suffix, port: defaultPort(u)})
		return nil
	}
	if strings.Contains(host, "*") {
		return fmt.Errorf("krate: allowed origin %q: * is only allowed as the first label", a)
	}
	p.exact[OriginOf(u)] = true
	return nil
}

func defaultPort(u *url.URL) string {
	if p := u.Port(); p != "" {
		return p
	}
	if u.Scheme == "https" {
		return "443"
	}
	return "80"
}

// OriginOf returns "scheme://host[:port]" in lower case, with the default
// port left out. It is "" for URLs without a host.
func OriginOf(u *url.URL) string {
	host := strings.ToLower(u.Hostname())
	if host == "" {
		return ""
	}
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	s := strings.ToLower(u.Scheme) + "://" + host
	if p := u.Port(); p != "" && !((u.Scheme == "https" && p == "443") || (u.Scheme == "http" && p == "80")) {
		s += ":" + p
	}
	return s
}

// OriginOfString is OriginOf for a URL string; "" when it does not parse.
func OriginOfString(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return OriginOf(u)
}

// StartOrigin is the origin of the first page.
func (p *RemotePolicy) StartOrigin() string { return p.startOrigin }

// Decide judges a navigation (a link, a redirect, window.open, a script
// setting location) of a remote view to target. Nothing but http and https
// pages of an allowed origin (and about:blank) can load: the app scheme,
// file, data, blob, javascript and every other scheme never do.
func (p *RemotePolicy) Decide(target string) NavVerdict {
	if target == "about:blank" {
		return NavAllow
	}
	u, err := url.Parse(target)
	if err != nil {
		return NavBlock
	}
	scheme := strings.ToLower(u.Scheme)
	switch scheme {
	case "http", "https":
		if u.Hostname() == "" || u.User != nil {
			return NavBlock
		}
		if p.originAllowed(u) {
			return NavAllow
		}
		return p.refused()
	case "mailto", "tel":
		return p.refused()
	}
	return NavBlock
}

func (p *RemotePolicy) refused() NavVerdict {
	if p.Blocked == BlockedDrop {
		return NavBlock
	}
	return NavExternal
}

func (p *RemotePolicy) originAllowed(u *url.URL) bool {
	if p.anyHTTPS && u.Scheme == "https" {
		return true
	}
	if p.exact[OriginOf(u)] {
		return true
	}
	host := strings.ToLower(u.Hostname())
	for _, w := range p.wildcards {
		if w.scheme == u.Scheme && w.port == defaultPort(u) && strings.HasSuffix(host, w.suffix) && len(host) > len(w.suffix) {
			return true
		}
	}
	return false
}

// Partition is a storage partition of remote content. Cookies, local storage,
// caches and credentials are never shared between partitions, and none of
// them is the app's.
type Partition struct {
	// Name is the partition name without the persist: prefix.
	Name string
	// Persistent partitions keep their data on disk, the others live in
	// memory and are gone when the app quits.
	Persistent bool
}

var partitionName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

// DefaultPartition is the partition of views that name none: persistent, and
// separate from the app's own storage.
const DefaultPartition = "default"

// ParsePartition parses an Electron-style partition string. "" is the default
// persistent remote partition, "persist:name" a named persistent one and
// "name" a named in-memory one.
func ParsePartition(s string) (Partition, error) {
	if s == "" {
		return Partition{Name: DefaultPartition, Persistent: true}, nil
	}
	persistent := false
	if strings.HasPrefix(s, "persist:") {
		persistent = true
		s = strings.TrimPrefix(s, "persist:")
	}
	if !partitionName.MatchString(s) {
		return Partition{}, fmt.Errorf("krate: partition name %q must be 1 to 64 letters, digits, dot, dash or underscore", s)
	}
	return Partition{Name: s, Persistent: persistent}, nil
}

// Key identifies the partition within the process; in-memory and persistent
// partitions of the same name are different.
func (p Partition) Key() string {
	if p.Persistent {
		return "p:" + p.Name
	}
	return "m:" + p.Name
}

// Dir is the directory name of a persistent partition below the app's data
// directory.
func (p Partition) Dir() string { return "remote-" + p.Name }

// String is the form ParsePartition accepts.
func (p Partition) String() string {
	if p.Persistent {
		if p.Name == DefaultPartition {
			return ""
		}
		return "persist:" + p.Name
	}
	return p.Name
}

// RuleAction is what a request rule does.
type RuleAction string

// Rule actions.
const (
	RuleBlock    RuleAction = "block"
	RuleRedirect RuleAction = "redirect"
	RuleHeaders  RuleAction = "headers"
)

// RequestRule is one declarative request rule of a partition (the common
// subset of Electron's webRequest that all engines can do; see InterceptCaps).
type RequestRule struct {
	// URLs are match patterns: "<all_urls>" or scheme://host/path with *
	// wildcards, the scheme "*" meaning http and https, a host "*.x.com"
	// meaning subdomains of x.com.
	URLs   []string
	Action RuleAction
	// Redirect is the URL a RuleRedirect sends the request to.
	Redirect string
	// Headers are set on the request by RuleHeaders.
	Headers map[string]string
}

// Validate checks the rule.
func (r RequestRule) Validate() error {
	if len(r.URLs) == 0 || len(r.URLs) > 64 {
		return errors.New("krate: a request rule needs 1 to 64 url patterns")
	}
	for _, p := range r.URLs {
		if _, err := compilePattern(p); err != nil {
			return err
		}
	}
	switch r.Action {
	case RuleBlock:
	case RuleRedirect:
		u, err := url.Parse(r.Redirect)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
			return fmt.Errorf("krate: redirect target %q must be an http or https URL", r.Redirect)
		}
	case RuleHeaders:
		if len(r.Headers) == 0 || len(r.Headers) > 32 {
			return errors.New("krate: a headers rule needs 1 to 32 headers")
		}
		for k, v := range r.Headers {
			if !validHeaderName(k) || strings.ContainsAny(v, "\r\n\x00") {
				return fmt.Errorf("krate: header %q is not valid", k)
			}
			switch strings.ToLower(k) {
			case "host", "content-length", "connection", "transfer-encoding":
				return fmt.Errorf("krate: header %q cannot be set", k)
			}
		}
	default:
		return fmt.Errorf("krate: rule action %q must be block, redirect or headers", r.Action)
	}
	return nil
}

func validHeaderName(s string) bool {
	if s == "" || len(s) > 128 {
		return false
	}
	for _, c := range s {
		if !(c == '-' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z') {
			return false
		}
	}
	return true
}

// ValidateRules checks a rule list.
func ValidateRules(rules []RequestRule) error {
	if len(rules) > 256 {
		return errors.New("krate: at most 256 request rules")
	}
	for i, r := range rules {
		if err := r.Validate(); err != nil {
			return fmt.Errorf("rule %d: %w", i, err)
		}
	}
	return nil
}

type pattern struct {
	scheme string // "*" for http and https
	host   string // "" for any host (<all_urls>); "*.x" keeps the leading star
	path   *regexp.Regexp
	any    bool
}

func compilePattern(p string) (*pattern, error) {
	if p == "<all_urls>" {
		return &pattern{any: true}, nil
	}
	i := strings.Index(p, "://")
	if i <= 0 {
		return nil, fmt.Errorf("krate: url pattern %q must be <all_urls> or scheme://host/path", p)
	}
	scheme, rest := p[:i], p[i+3:]
	if scheme != "*" && scheme != "http" && scheme != "https" {
		return nil, fmt.Errorf("krate: url pattern %q: scheme must be *, http or https", p)
	}
	slash := strings.IndexByte(rest, '/')
	if slash < 0 {
		return nil, fmt.Errorf("krate: url pattern %q needs a path (end it with /*)", p)
	}
	host, path := strings.ToLower(rest[:slash]), rest[slash:]
	if host == "" || (strings.Contains(host, "*") && host != "*" && !strings.HasPrefix(host, "*.")) || strings.Count(host, "*") > 1 {
		return nil, fmt.Errorf("krate: url pattern %q: a host is a name, * or *.name", p)
	}
	var sb strings.Builder
	sb.WriteString("^")
	for _, c := range path {
		if c == '*' {
			sb.WriteString(".*")
		} else {
			sb.WriteString(regexp.QuoteMeta(string(c)))
		}
	}
	sb.WriteString("$")
	re, err := regexp.Compile(sb.String())
	if err != nil {
		return nil, fmt.Errorf("krate: url pattern %q: %w", p, err)
	}
	return &pattern{scheme: scheme, host: host, path: re}, nil
}

func (p *pattern) match(u *url.URL) bool {
	if p.any {
		return u.Scheme == "http" || u.Scheme == "https"
	}
	if p.scheme == "*" {
		if u.Scheme != "http" && u.Scheme != "https" {
			return false
		}
	} else if p.scheme != u.Scheme {
		return false
	}
	if !p.hostMatches(u) {
		return false
	}
	return p.path.MatchString(u.EscapedPath() + queryPart(u))
}

func (p *pattern) hostMatches(u *url.URL) bool {
	host := strings.ToLower(u.Hostname())
	switch {
	case p.host == "*":
		return true
	case strings.HasPrefix(p.host, "*."):
		return strings.HasSuffix(host, p.host[1:]) && len(host) > len(p.host)-1
	}
	if h, port, ok := strings.Cut(p.host, ":"); ok && !strings.HasPrefix(p.host, "[") {
		return host == h && u.Port() == port
	}
	return host == p.host
}

func queryPart(u *url.URL) string {
	if u.RawQuery == "" {
		return ""
	}
	return "?" + u.RawQuery
}

// MatchURLPattern reports whether raw matches the pattern. A malformed
// pattern or URL never matches.
func MatchURLPattern(pat, raw string) bool {
	p, err := compilePattern(pat)
	if err != nil {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	return p.match(u)
}

// RuleResult is the combined effect of the rules on one request.
type RuleResult struct {
	Block    bool
	Redirect string
	Headers  map[string]string
}

// Any reports whether any rule applied.
func (r RuleResult) Any() bool { return r.Block || r.Redirect != "" || len(r.Headers) > 0 }

// EvalRules applies rules in order: a block rule wins over everything, the
// first matching redirect is taken, header rules accumulate (later rules
// replace earlier values of the same header).
func EvalRules(rules []RequestRule, raw string) RuleResult {
	var res RuleResult
	u, err := url.Parse(raw)
	if err != nil {
		return res
	}
	for _, r := range rules {
		hit := false
		for _, pat := range r.URLs {
			if p, err := compilePattern(pat); err == nil && p.match(u) {
				hit = true
				break
			}
		}
		if !hit {
			continue
		}
		switch r.Action {
		case RuleBlock:
			res.Block = true
		case RuleRedirect:
			if res.Redirect == "" {
				res.Redirect = r.Redirect
			}
		case RuleHeaders:
			if res.Headers == nil {
				res.Headers = map[string]string{}
			}
			for k, v := range r.Headers {
				res.Headers[k] = v
			}
		}
	}
	return res
}

// ContentBlockerJSON renders the block rules in WebKit's content blocker
// format (WebKitUserContentFilter, WKContentRuleList). It returns nil when no
// rule blocks. Redirect and header rules cannot be expressed in it.
func ContentBlockerJSON(rules []RequestRule) ([]byte, error) {
	var sb strings.Builder
	n := 0
	for _, r := range rules {
		if r.Action != RuleBlock {
			continue
		}
		for _, pat := range r.URLs {
			re, err := blockerRegex(pat)
			if err != nil {
				return nil, err
			}
			if n > 0 {
				sb.WriteByte(',')
			}
			n++
			fmt.Fprintf(&sb, `{"trigger":{"url-filter":%q},"action":{"type":"block"}}`, re)
		}
	}
	if n == 0 {
		return nil, nil
	}
	return []byte("[" + sb.String() + "]"), nil
}

// blockerRegex translates a match pattern into the regular expression subset
// content blockers accept (no alternation, no groups).
func blockerRegex(pat string) (string, error) {
	if pat == "<all_urls>" {
		return "^https?://", nil
	}
	p, err := compilePattern(pat)
	if err != nil {
		return "", err
	}
	var sb strings.Builder
	sb.WriteString("^")
	if p.scheme == "*" {
		sb.WriteString("https?")
	} else {
		sb.WriteString(p.scheme)
	}
	sb.WriteString("://")
	host := p.host
	switch {
	case host == "*":
		sb.WriteString("[^/]*")
	case strings.HasPrefix(host, "*."):
		sb.WriteString(`[^/]*\.` + escapeRegex(host[2:]) + "(:[0-9]*)?")
	default:
		sb.WriteString(escapeRegex(host))
		if !strings.Contains(host, ":") {
			sb.WriteString("(:[0-9]*)?")
		}
	}
	rest := pat[strings.Index(pat, "://")+3:]
	path := rest[strings.IndexByte(rest, '/'):]
	for _, c := range path {
		if c == '*' {
			sb.WriteString(".*")
		} else {
			sb.WriteString(escapeRegex(string(c)))
		}
	}
	if !strings.HasSuffix(path, "*") {
		sb.WriteString("$")
	}
	return sb.String(), nil
}

func escapeRegex(s string) string {
	var sb strings.Builder
	for _, c := range s {
		if strings.ContainsRune(`\.+*?()[]{}^$|`, c) {
			sb.WriteByte('\\')
		}
		sb.WriteRune(c)
	}
	return sb.String()
}

// InterceptLevel says how much of an interception feature an engine offers.
type InterceptLevel string

// Levels. "navigation" is the main frame only, "all" every request.
const (
	LevelNone       InterceptLevel = ""
	LevelNavigation InterceptLevel = "navigation"
	LevelAll        InterceptLevel = "all"
)

// RemoteCaps reports what a platform can do for remote content.
type RemoteCaps struct {
	// Observe says request events are delivered (for navigations at least).
	Observe InterceptLevel
	// Block, Redirect and RequestHeaders say which requests the rules can
	// act on.
	Block          InterceptLevel
	Redirect       InterceptLevel
	RequestHeaders InterceptLevel
	// UserAgent, Proxy, CertificateErrors, ClientCertificates and Auth say
	// whether those features work.
	UserAgent          bool
	Proxy              bool
	CertificateErrors  bool
	ClientCertificates bool
	Auth               bool
	// InMemory says non-persistent partitions are available.
	InMemory bool
}

// ProxyMode selects how a partition reaches the network.
type ProxyMode string

// Proxy modes.
const (
	ProxySystem ProxyMode = "system"
	ProxyDirect ProxyMode = "direct"
	ProxyFixed  ProxyMode = "fixed"
)

// ProxyConfig is the proxy of a partition.
type ProxyConfig struct {
	Mode ProxyMode
	// URL is the proxy for ProxyFixed: http://host:port or socks5://host:port.
	URL string
	// Bypass lists hosts that skip the proxy.
	Bypass []string
}

// Validate checks the configuration.
func (c ProxyConfig) Validate() error {
	switch c.Mode {
	case ProxySystem, ProxyDirect:
		return nil
	case ProxyFixed:
		u, err := url.Parse(c.URL)
		if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https" && u.Scheme != "socks5") {
			return fmt.Errorf("krate: proxy URL %q must be http://, https:// or socks5://host:port", c.URL)
		}
		return nil
	}
	return fmt.Errorf("krate: proxy mode %q must be system, direct or fixed", c.Mode)
}

// RemoteEventKind names a remote event.
type RemoteEventKind string

// Remote event kinds.
const (
	// RemoteMessage is a message the page of a view posted (opt in).
	RemoteMessage RemoteEventKind = "message"
	// RemoteNavBlocked is a navigation the allowlist refused. Verdict says
	// whether the platform wants the URL opened externally.
	RemoteNavBlocked RemoteEventKind = "navBlocked"
	// RemoteRequest is a request the view started (observation).
	RemoteRequest RemoteEventKind = "request"
	// RemoteLoaded is a page that finished loading.
	RemoteLoaded RemoteEventKind = "loaded"
)

// RemoteEvent is delivered to the handler set with OnRemoteEvent.
type RemoteEvent struct {
	View ViewRef
	Kind RemoteEventKind
	// URL is the page or request URL.
	URL string
	// Origin is the origin of the sender of a message, taken from the
	// engine, never from the page.
	Origin string
	// Data is the JSON of a message.
	Data    []byte
	Verdict NavVerdict
	Method  string
	// Action is what the rules did to a RemoteRequest: "", "block",
	// "redirect" or "headers".
	Action string
}

// CertError is a certificate the engine refused.
type CertError struct {
	View ViewRef
	URL  string
	Host string
	// Errors names the problems: "untrusted", "expired", "notYetValid",
	// "mismatch", "revoked", "insecure", "unknown".
	Errors []string
}

// AuthRequest is an HTTP authentication challenge (basic, digest, NTLM).
type AuthRequest struct {
	View   ViewRef
	URL    string
	Host   string
	Realm  string
	Scheme string
	// Proxy says the challenge is from a proxy.
	Proxy bool
	// Retry says an earlier answer was refused.
	Retry bool
}

// RemoteWindowSpec configures NewRemoteWindow.
type RemoteWindowSpec struct {
	Title     string
	Width     int
	Height    int
	Resizable bool
	Chrome    ChromeOptions
	Remote    RemoteOptions
}

// RemoteWindow is the window a RemoteWindows App returns. It is the core
// platform window interface.
type RemoteWindow interface {
	Navigate(path string)
	Eval(js string)
	SetTitle(title string)
	SetSize(width, height int)
	Close()
}

// RemoteWindows is implemented by Apps that can open a window whose primary
// view is remote content.
type RemoteWindows interface {
	NewRemoteWindow(spec RemoteWindowSpec) (RemoteWindow, error)
}

// RemoteViews is implemented by Window values that can add remote views.
type RemoteViews interface {
	// AddRemoteView adds a remote view; o.Path is ignored.
	AddRemoteView(o ViewOptions, r RemoteOptions) (ViewRef, error)
}

// RemoteContent is implemented by Apps that host remote views.
type RemoteContent interface {
	RemoteCapabilities() RemoteCaps
	// IsRemoteView reports whether a view shows remote content.
	IsRemoteView(v ViewRef) bool
	// RemoteOrigin is the origin of the page the view shows now.
	RemoteOrigin(v ViewRef) string
	// NavigateRemote loads a URL in the view if its policy allows it.
	NavigateRemote(v ViewRef, rawURL string) error
	// PostToRemote delivers a message to the page of a view whose messages
	// are enabled, only while the page's origin is targetOrigin ("*" skips
	// the check). data is JSON.
	PostToRemote(v ViewRef, targetOrigin string, data []byte) error
	// OnRemoteEvent sets the single handler for remote events. fn runs on
	// the main thread and must not block.
	OnRemoteEvent(fn func(RemoteEvent))
	// SetRequestRules replaces the request rules of a partition.
	SetRequestRules(partition string, rules []RequestRule) error
	// SetUserAgent sets the user agent of a partition; "" restores the
	// engine's.
	SetUserAgent(partition, ua string) error
	// SetProxy sets the proxy of a partition.
	SetProxy(partition string, c ProxyConfig) error
	// ClearPartition removes stored data of a partition.
	ClearPartition(partition string, o ClearSiteDataOptions) error
	// SetCertificateHandler sets the handler for refused certificates; it
	// calls allow exactly once (from any goroutine) with true to accept the
	// certificate for that host. Without a handler every error is final.
	SetCertificateHandler(fn func(e CertError, allow func(bool)))
	// SetAuthHandler sets the handler for HTTP authentication challenges; it
	// calls answer once (from any goroutine). Without a handler challenges
	// are cancelled.
	SetAuthHandler(fn func(r AuthRequest, answer func(user, password string, ok bool)))
	// SetClientCertificate makes the partition present a client certificate
	// (PEM files) to a host.
	SetClientCertificate(partition, host, certFile, keyFile string) error
}

// MaxRemoteMessage is the largest message in either direction, in bytes of
// JSON.
const MaxRemoteMessage = 1 << 20

// RemotePageScript is the script installed in the top frame of a remote view
// whose messages are enabled. send is a JavaScript function expression that
// hands a string to the host (the engine adds the frame's real origin).
func RemotePageScript(send string) string {
	return `(function(){if(window.krateRemote)return;var s=` + send + `;` +
		`Object.defineProperty(window,"krateRemote",{value:Object.freeze({postMessage:function(d){` +
		`var j=JSON.stringify(d);if(j===undefined)throw new TypeError("not JSON");s(j)}})})})();`
}

// DeliverToRemoteScript builds the script that delivers a JSON message to a
// remote page as a "message" event whose origin is the app's. The script does
// nothing when the page is not on targetOrigin ("*" skips the check).
func DeliverToRemoteScript(appOrigin, targetOrigin string, data []byte) string {
	return `(function(){var t=` + jsString(targetOrigin) + `;if(t!=="*"&&location.origin!==t)return;` +
		`try{window.dispatchEvent(new MessageEvent("message",{data:JSON.parse(` + jsString(string(data)) +
		`),origin:` + jsString(appOrigin) + `}))}catch(e){}})();`
}

func jsString(s string) string {
	var sb strings.Builder
	sb.WriteByte('"')
	for _, c := range s {
		switch {
		case c == '"' || c == '\\':
			sb.WriteByte('\\')
			sb.WriteRune(c)
		case c < 0x20 || c == 0x2028 || c == 0x2029 || c == '<' || c == '>' || c == '&':
			fmt.Fprintf(&sb, `\u%04x`, c)
		default:
			sb.WriteRune(c)
		}
	}
	sb.WriteByte('"')
	return sb.String()
}
