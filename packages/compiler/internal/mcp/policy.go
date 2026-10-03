package mcp

import (
	"encoding/json"
	"os"
	"path"
	"path/filepath"
	"strings"
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

	// loaded is true when a config file was found (informational).
	loaded bool
}

// defaultDeniedPatterns are always denied unless an `allow` entry matches.
// These hold credentials or internal VCS state an agent must not rewrite.
var defaultDeniedPatterns = []string{
	".env", ".env.*", "*.env",
	"*.pem", "*.key", "*.p12", "*.pfx", "*.jks", "*.keystore",
	"id_rsa", "id_ed25519", "id_ecdsa", "id_dsa",
	".git", ".git/**",
	".krate/mcp.json",
	"*.npmrc", "*.pypirc", "*.netrc",
}

// policyFileName is the per-project MCP policy path (relative to the root).
const policyFileName = ".krate/mcp.json"

// loadPolicy reads `<root>/.krate/mcp.json`. A missing or malformed file yields
// the default policy (empty lists) rather than an error, so the server always
// starts.
func loadPolicy(root string) *Policy {
	p := &Policy{}
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(policyFileName)))
	if err != nil {
		return p
	}
	if err := json.Unmarshal(data, p); err != nil {
		return &Policy{}
	}
	p.loaded = true
	return p
}

// denied reports whether rel is refused for both reading and writing. An
// explicit `allow` pattern overrides the default sensitive list but not an
// explicit `deny` entry.
func (p *Policy) denied(rel string) bool {
	rel = normalizeRel(rel)
	for _, pat := range p.Deny {
		if globMatch(pat, rel) {
			return true
		}
	}
	for _, pat := range p.Allow {
		if globMatch(pat, rel) {
			return false
		}
	}
	for _, pat := range defaultDeniedPatterns {
		if globMatch(pat, rel) {
			return true
		}
	}
	return false
}

// writable reports whether rel may be written: not denied, and not marked
// read-only.
func (p *Policy) writable(rel string) bool {
	if p.denied(rel) {
		return false
	}
	rel = normalizeRel(rel)
	for _, pat := range p.ReadOnly {
		if globMatch(pat, rel) {
			return false
		}
	}
	return true
}

// describe returns a short human-readable summary of why a path is denied.
func (p *Policy) deniedReason(rel string) string {
	rel = normalizeRel(rel)
	for _, pat := range p.Deny {
		if globMatch(pat, rel) {
			return "matches the project deny rule " + strconvQuote(pat)
		}
	}
	for _, pat := range defaultDeniedPatterns {
		if globMatch(pat, rel) {
			return "is a protected path (" + strconvQuote(pat) + "); add it to allow in .krate/mcp.json to permit access"
		}
	}
	for _, pat := range p.ReadOnly {
		if globMatch(pat, rel) {
			return "is marked read-only in .krate/mcp.json"
		}
	}
	return "is not permitted"
}

func normalizeRel(rel string) string {
	rel = filepath.ToSlash(rel)
	rel = strings.TrimPrefix(rel, "./")
	return rel
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
