package mcp

import (
	"encoding/json"
	"net"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

// Policy governs which project paths the MCP write/read tools may touch. It is
// loaded from `<root>/.krate/mcp.json`:
//
//	{
//	  "allow":    ["secrets/public.env"],   // override the default-deny list
//	  "deny":     ["src/generated/**"],      // always denied (wins over allow)
//	  "readOnly": ["krate.config.ts"]        // readable, never writable
//	}
//
// With no config, the agent may read and write any project file EXCEPT a small
// set of sensitive paths (environment files, private keys, .git, and the policy
// file itself) which require an explicit `allow` entry.
type Policy struct {
	Allow    []string `json:"allow"`
	Deny     []string `json:"deny"`
	ReadOnly []string `json:"readOnly"`
	// WriteRoots limits writes to these project-relative paths. Empty keeps
	// the legacy project-wide behavior (minus denied paths).
	WriteRoots []string `json:"writeRoots,omitempty"`
	// MaxFileBytes caps a single read/write. Zero uses the default.
	MaxFileBytes int64 `json:"maxFileBytes,omitempty"`
	// MaxBatchFiles caps a transaction. Zero uses the default.
	MaxBatchFiles int `json:"maxBatchFiles,omitempty"`
	// AllowBinary permits writing binary files. Read tools already return a
	// binary marker instead of content.
	AllowBinary bool `json:"allowBinary,omitempty"`
	// ReadOnlyMode disables every write tool and run_task, for a shared or
	// remote MCP profile that should only inspect the project.
	ReadOnlyMode bool `json:"readOnlyMode,omitempty"`
	// AllowTasks gates run_task.
	AllowTasks []string `json:"allowTasks,omitempty"`
	// AuditLog is the project-relative JSONL audit log path. Empty uses
	// .krate/mcp-audit.jsonl; "off" disables audit logging.
	AuditLog string `json:"auditLog,omitempty"`
	// RateLimit bounds HTTP requests per client. Nil uses the default.
	RateLimit *RateLimitPolicy `json:"rateLimit,omitempty"`
	// AllowedHosts lets a reverse proxy expose MCP under names other than
	// localhost. Loopback hosts are always accepted.
	AllowedHosts []string `json:"allowedHosts,omitempty"`
	// Redact lists literal values to mask in tool output and audit logs.
	Redact []string `json:"redact,omitempty"`

	// root is the project root the policy was loaded from. It lets path checks
	// see through symlinks: a link to a protected file is that file.
	root string
	// loaded is true when a config file was found (informational).
	loaded bool
	// invalid is true when a policy file exists but could not be parsed. The
	// policy then fails closed instead of silently granting the default access.
	invalid bool
}

// RateLimitPolicy configures the HTTP request budget per client address.
type RateLimitPolicy struct {
	// Disabled turns rate limiting off.
	Disabled bool `json:"disabled,omitempty"`
	// Requests is the number of requests allowed per window (default 120).
	Requests int `json:"requests,omitempty"`
	// WindowSeconds is the window length in seconds (default 60).
	WindowSeconds int `json:"windowSeconds,omitempty"`
}

const defaultAuditLog = ".krate/mcp-audit.jsonl"

// auditLogPath returns the audit log path, or "" when audit logging is off.
func (p *Policy) auditLogPath() string {
	if p == nil || p.invalid {
		return defaultAuditLog
	}
	switch strings.ToLower(strings.TrimSpace(p.AuditLog)) {
	case "off", "false", "none", "disabled":
		return ""
	case "":
		return defaultAuditLog
	}
	rel := normalizeRel(p.AuditLog)
	if filepath.IsAbs(p.AuditLog) || rel == "" || strings.HasPrefix(rel, "../") || rel == ".." {
		return defaultAuditLog
	}
	return rel
}

// rateLimit returns the configured request budget. A zero limit disables
// rate limiting.
func (p *Policy) rateLimit() (int, time.Duration) {
	limit, seconds := 120, 60
	if p != nil && p.RateLimit != nil {
		if p.RateLimit.Disabled {
			return 0, 0
		}
		if p.RateLimit.Requests > 0 {
			limit = p.RateLimit.Requests
		}
		if p.RateLimit.WindowSeconds > 0 {
			seconds = p.RateLimit.WindowSeconds
		}
	}
	return limit, time.Duration(seconds) * time.Second
}

// allowsHost reports whether an HTTP Host header may reach the MCP handler.
// Loopback names are always allowed; a reverse proxy deployment can add its
// public hostnames via allowedHosts.
func (p *Policy) allowsHost(hostport string) bool {
	host := hostport
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		host = h
	}
	host = strings.Trim(host, "[]")
	if host == "" {
		return true
	}
	if strings.EqualFold(host, "localhost") || host == "127.0.0.1" || host == "::1" {
		return true
	}
	if p == nil {
		return false
	}
	for _, allowed := range p.AllowedHosts {
		allowed = strings.TrimSpace(allowed)
		if allowed == "" {
			continue
		}
		if strings.EqualFold(allowed, hostport) || strings.EqualFold(strings.Trim(allowed, "[]"), host) {
			return true
		}
	}
	return false
}

// defaultWriteDeniedPatterns are generated/output trees that must never be
// edited by an agent.
var defaultWriteDeniedPatterns = []string{
	"node_modules", "node_modules/**",
	"dist", "dist/**",
	"build", "build/**",
	".krate", ".krate/**",
}

// defaultDeniedPatterns are always denied unless an `allow` entry matches.
// These hold credentials or internal VCS state an agent must not rewrite.
var defaultDeniedPatterns = []string{
	".env", ".env.*", "*.env", ".envrc", ".dev.vars",
	"*.pem", "*.key", "*.p12", "*.pfx", "*.jks", "*.keystore", "*.p8",
	"id_rsa", "id_ed25519", "id_ecdsa", "id_dsa",
	"credentials.json", "service-account*.json",
	".aws", ".aws/**", ".docker", ".docker/config.json",
	".git", ".git/**",
	".krate", ".krate/**",
	".krate/mcp.json",
	"*.npmrc", "*.pypirc", "*.netrc",
}

// policyFileName is the per-project MCP policy path (relative to the root).
const policyFileName = ".krate/mcp.json"

// loadPolicy reads `<root>/.krate/mcp.json`. A missing or malformed file yields
// the default policy (empty lists) rather than an error, so the server always
// starts.
func loadPolicy(root string) *Policy {
	p := &Policy{root: root}
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(policyFileName)))
	if err != nil {
		return p
	}
	if err := json.Unmarshal(data, p); err != nil {
		return &Policy{root: root, invalid: true}
	}
	p.root = root
	p.loaded = true
	return p
}

// denied reports whether rel is refused for both reading and writing. An
// explicit `allow` pattern overrides the default sensitive list but not an
// explicit `deny` entry. A symlink is judged by its own name and by the path
// it resolves to, so a link cannot reach a protected file.
func (p *Policy) denied(rel string) bool {
	if p.deniedLexical(rel) {
		return true
	}
	alias := p.aliasOf(rel)
	return alias != "" && p.deniedLexical(alias)
}

func (p *Policy) deniedLexical(rel string) bool {
	if p.invalid {
		return true
	}
	rel = normalizeRel(rel)
	for _, pat := range p.Deny {
		if globMatchFold(pat, rel) {
			return true
		}
	}
	for _, pat := range p.Allow {
		if globMatch(pat, rel) {
			return false
		}
	}
	for _, pat := range defaultDeniedPatterns {
		if globMatchFold(pat, rel) {
			return true
		}
	}
	return false
}

// writable reports whether rel may be written: not denied, not read-only,
// inside writeRoots when configured, and not in a generated output tree.
// Like denied, it also judges the path a symlink resolves to.
func (p *Policy) writable(rel string) bool {
	if !p.writableLexical(rel) {
		return false
	}
	alias := p.aliasOf(rel)
	return alias == "" || p.writableLexical(alias)
}

func (p *Policy) writableLexical(rel string) bool {
	if p.ReadOnlyMode {
		return false
	}
	if p.deniedLexical(rel) {
		return false
	}
	rel = normalizeRel(rel)
	if !p.AllowBinary && isBinaryPath(rel) {
		return false
	}
	for _, pat := range p.ReadOnly {
		if globMatchFold(pat, rel) {
			return false
		}
	}
	for _, pat := range defaultWriteDeniedPatterns {
		if globMatchFold(pat, rel) {
			return false
		}
	}
	if len(p.WriteRoots) > 0 && !p.withinWriteRoots(rel) {
		return false
	}
	return true
}

func (p *Policy) withinWriteRoots(rel string) bool {
	rel = normalizeRel(rel)
	for _, root := range p.WriteRoots {
		root = strings.Trim(normalizeRel(root), "/")
		if root == "" || root == "." {
			return true
		}
		if strings.ContainsAny(root, "*?[") {
			if globMatch(root, rel) {
				return true
			}
			continue
		}
		if rel == root || strings.HasPrefix(rel, root+"/") {
			return true
		}
	}
	return false
}

// deniedReason returns a short human-readable summary of why a path is denied.
func (p *Policy) deniedReason(rel string) string {
	reason := p.reasonLexical(rel)
	if reason == notPermitted {
		if alias := p.aliasOf(rel); alias != "" {
			if r := p.reasonLexical(alias); r != notPermitted {
				return "is a link to " + alias + ", which " + r
			}
		}
	}
	return reason
}

const notPermitted = "is not permitted"

func (p *Policy) reasonLexical(rel string) string {
	if p.invalid {
		return "the project policy file is malformed; fix .krate/mcp.json"
	}
	if p.ReadOnlyMode {
		return "is read-only because the project policy enables readOnlyMode"
	}
	rel = normalizeRel(rel)
	if !p.AllowBinary && isBinaryPath(rel) {
		return "is a binary file; set allowBinary in .krate/mcp.json to permit writing it"
	}
	for _, pat := range p.Deny {
		if globMatchFold(pat, rel) {
			return "matches the project deny rule " + strconvQuote(pat)
		}
	}
	for _, pat := range defaultDeniedPatterns {
		if globMatchFold(pat, rel) {
			return "is a protected path (" + strconvQuote(pat) + "); add it to allow in .krate/mcp.json to permit access"
		}
	}
	for _, pat := range p.ReadOnly {
		if globMatchFold(pat, rel) {
			return "is marked read-only in .krate/mcp.json"
		}
	}
	for _, pat := range defaultWriteDeniedPatterns {
		if globMatchFold(pat, rel) {
			return "is a generated or output path (" + strconvQuote(pat) + ") and cannot be edited"
		}
	}
	if len(p.WriteRoots) > 0 && !p.withinWriteRoots(rel) {
		return "is outside writeRoots in .krate/mcp.json"
	}
	return notPermitted
}

// aliasOf returns the project-relative path rel really names once symlinks are
// resolved, or "" when it is the same path, does not resolve, or the policy
// has no project root. The part of rel that does not exist yet (a file about to
// be created) is kept, so a write through a linked directory is judged by its
// real location.
func (p *Policy) aliasOf(rel string) string {
	if p == nil || p.root == "" || rel == "" {
		return ""
	}
	rootReal, err := filepath.EvalSymlinks(p.root)
	if err != nil {
		return ""
	}
	abs := filepath.Join(rootReal, filepath.FromSlash(normalizeRel(rel)))
	real, ok := evalDeepest(abs)
	if !ok {
		return ""
	}
	back, err := filepath.Rel(rootReal, real)
	if err != nil || back == ".." || strings.HasPrefix(back, ".."+string(filepath.Separator)) {
		return ""
	}
	back = filepath.ToSlash(back)
	if back == "." || back == normalizeRel(rel) {
		return ""
	}
	return back
}

// evalDeepest resolves symlinks in the longest existing prefix of p and
// appends the rest unchanged.
func evalDeepest(p string) (string, bool) {
	rest := ""
	for probe := p; ; {
		if real, err := filepath.EvalSymlinks(probe); err == nil {
			return filepath.Join(real, rest), true
		}
		parent := filepath.Dir(probe)
		if parent == probe {
			return "", false
		}
		rest = filepath.Join(filepath.Base(probe), rest)
		probe = parent
	}
}

func normalizeRel(rel string) string {
	rel = filepath.ToSlash(rel)
	rel = strings.TrimPrefix(rel, "./")
	return rel
}

var binaryExtensions = map[string]bool{
	".avif": true, ".br": true, ".eot": true, ".gif": true, ".gz": true,
	".ico": true, ".jpeg": true, ".jpg": true, ".mp3": true, ".mp4": true,
	".ogg": true, ".otf": true, ".pdf": true, ".png": true, ".tar": true,
	".ttf": true, ".wasm": true, ".wav": true, ".webm": true, ".webp": true,
	".woff": true, ".woff2": true, ".zip": true,
}

func isBinaryPath(rel string) bool {
	return binaryExtensions[strings.ToLower(filepath.Ext(rel))]
}

// globMatch matches a slash path against a glob supporting `**` (any number of
// path segments), `*`/`?`/`[...]` within a segment. A pattern without a slash
// also matches against the basename, so `.env` matches `config/.env`.
func globMatch(pattern, rel string) bool {
	pattern = normalizeRel(pattern)
	rel = normalizeRel(rel)
	if pattern == "" || rel == "" {
		return false
	}
	if segmentMatch(pattern, rel) {
		return true
	}
	if !strings.Contains(pattern, "/") {
		// A slash-less pattern names a file or directory anywhere in the tree:
		// match the basename (config/.env) or any path segment (.git/config).
		if segmentMatch(pattern, path.Base(rel)) {
			return true
		}
		for _, seg := range strings.Split(rel, "/") {
			if ok, _ := path.Match(pattern, seg); ok {
				return true
			}
		}
	}
	return false
}

// globMatchFold is globMatch ignoring case. The deny side of the policy uses it:
// on macOS and Windows .ENV and .Git name the same files as .env and .git, so
// a protected name must stay protected in any spelling.
func globMatchFold(pattern, rel string) bool {
	return globMatch(strings.ToLower(pattern), strings.ToLower(rel))
}

func segmentMatch(pattern, rel string) bool {
	pat := strings.Split(pattern, "/")
	name := strings.Split(rel, "/")
	return matchSegments(pat, name)
}

func matchSegments(pat, name []string) bool {
	if len(pat) == 0 {
		return len(name) == 0
	}
	if pat[0] == "**" {
		for i := 0; i <= len(name); i++ {
			if matchSegments(pat[1:], name[i:]) {
				return true
			}
		}
		return false
	}
	if len(name) == 0 {
		return false
	}
	if ok, _ := path.Match(pat[0], name[0]); !ok {
		return false
	}
	return matchSegments(pat[1:], name[1:])
}

// strconvQuote is a tiny helper to avoid importing strconv just for quoting.
func strconvQuote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
