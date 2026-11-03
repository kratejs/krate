package mcp

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

var (
	bearerPattern     = regexp.MustCompile(`(?i)(bearer\s+)[A-Za-z0-9._~+/\-=]{8,}`)
	secretPairPattern = regexp.MustCompile(`(?i)("(?:api[_-]?key|access[_-]?token|refresh[_-]?token|secret|password|passwd|authorization)"\s*:\s*")([^"]{4,})(")`)
)

// auditRecord is one JSONL line in the MCP audit log. It intentionally stores
// a small, redacted argument summary rather than full file contents or patches.
type auditRecord struct {
	Time       string         `json:"time"`
	Event      string         `json:"event"`
	Tool       string         `json:"tool,omitempty"`
	Client     string         `json:"client,omitempty"`
	Remote     bool           `json:"remote,omitempty"`
	ReadOnly   bool           `json:"readOnly,omitempty"`
	Apply      bool           `json:"apply,omitempty"`
	Status     string         `json:"status"`
	Reason     string         `json:"reason,omitempty"`
	Method     string         `json:"method,omitempty"`
	Path       string         `json:"path,omitempty"`
	RemoteAddr string         `json:"remoteAddr,omitempty"`
	DurationMS int64          `json:"durationMs,omitempty"`
	Error      string         `json:"error,omitempty"`
	Args       map[string]any `json:"args,omitempty"`
	Paths      []string       `json:"paths,omitempty"`
}

func (s *Service) recordHTTPReject(r *http.Request, reason string) {
	s.recordAudit(auditRecord{
		Event: "security_reject", Status: "rejected", Reason: reason,
		Method: r.Method, Path: r.URL.Path, RemoteAddr: rateLimitKey(r.RemoteAddr),
	})
}

var auditArgKeys = map[string]bool{
	"route": true, "path": true, "source": true, "glob": true,
	"query": true, "task": true, "collection": true, "slug": true,
	"template": true, "kind": true, "name": true, "dir": true,
	"apply": true, "format": true, "limit": true, "cursor": true,
	"id": true, "force": true,
}

// auditArgs keeps only small, useful fields so a write's full content or patch
// never lands in the audit log.
func auditArgs(args map[string]any) map[string]any {
	if len(args) == 0 {
		return nil
	}
	out := map[string]any{}
	for key, value := range args {
		if !auditArgKeys[key] {
			continue
		}
		if s, ok := value.(string); ok {
			if len(s) > 200 {
				s = s[:200] + "...(truncated)"
			}
			out[key] = s
			continue
		}
		out[key] = value
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// auditPaths extracts likely path arguments for quick filtering.
func auditPaths(args map[string]any) []string {
	seen := map[string]bool{}
	var out []string
	add := func(value any) {
		s, _ := value.(string)
		if s == "" || seen[s] {
			return
		}
		seen[s] = true
		out = append(out, s)
	}
	for _, key := range []string{"path", "route", "source"} {
		add(args[key])
	}
	if list, ok := args["paths"].([]any); ok {
		for _, item := range list {
			add(item)
		}
	}
	if list, ok := args["edits"].([]any); ok {
		for _, item := range list {
			if edit, ok := item.(map[string]any); ok {
				add(edit["path"])
				add(edit["route"])
				add(edit["to"])
				add(edit["moveTo"])
			}
		}
	}
	if patch, ok := args["patch"].(string); ok {
		for _, line := range strings.Split(patch, "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "+++ ") || strings.HasPrefix(line, "--- ") {
				path := strings.TrimSpace(line[4:])
				if i := strings.IndexByte(path, '\t'); i >= 0 {
					path = path[:i]
				}
				path = strings.TrimPrefix(path, "a/")
				path = strings.TrimPrefix(path, "b/")
				if path != "/dev/null" {
					add(path)
				}
			}
		}
	}
	sort.Strings(out)
	return out
}

func (s *Service) recordAudit(rec auditRecord) {
	if s == nil || s.auditPath == "" {
		return
	}
	rec.Time = time.Now().UTC().Format(time.RFC3339Nano)
	rec.Error = s.redact(rec.Error)
	if rec.Args != nil {
		if redacted, ok := s.redactValue(rec.Args).(map[string]any); ok {
			rec.Args = redacted
		}
	}
	data, err := json.Marshal(rec)
	if err != nil {
		fmt.Fprintf(os.Stderr, "krate mcp audit: %v\n", err)
		return
	}
	s.auditMu.Lock()
	defer s.auditMu.Unlock()
	path := filepath.Join(s.root, filepath.FromSlash(s.auditPath))
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		fmt.Fprintf(os.Stderr, "krate mcp audit: %v\n", err)
		return
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		fmt.Fprintf(os.Stderr, "krate mcp audit: %v\n", err)
		return
	}
	if _, err := f.Write(append(data, '\n')); err != nil {
		fmt.Fprintf(os.Stderr, "krate mcp audit: %v\n", err)
	}
	_ = f.Close()
}

// redact masks known environment secrets, policy-listed literals, bearer
// tokens, and sensitive JSON key/value pairs in text that leaves the server.
func (s *Service) redact(text string) string {
	if s == nil || text == "" {
		return text
	}
	for _, value := range s.secretValues {
		if len(value) >= 6 {
			text = strings.ReplaceAll(text, value, "[REDACTED]")
		}
	}
	for _, value := range s.policyLiteralRedactions {
		if len(value) >= 3 {
			text = strings.ReplaceAll(text, value, "[REDACTED]")
		}
	}
	text = bearerPattern.ReplaceAllString(text, "${1}[REDACTED]")
	text = secretPairPattern.ReplaceAllString(text, "${1}[REDACTED]${3}")
	return text
}

// redactValue recursively redacts strings in maps and slices.
func (s *Service) redactValue(value any) any {
	switch v := value.(type) {
	case string:
		return s.redact(v)
	case map[string]any:
		out := make(map[string]any, len(v))
		for key, item := range v {
			if sensitiveArgKey(key) {
				out[key] = "[REDACTED]"
				continue
			}
			out[key] = s.redactValue(item)
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, item := range v {
			out[i] = s.redactValue(item)
		}
		return out
	default:
		return value
	}
}

func sensitiveArgKey(key string) bool {
	k := strings.ToLower(key)
	for _, frag := range []string{"token", "secret", "password", "passwd", "authorization", "credential", "apikey", "api_key", "privatekey", "private_key", "cookie"} {
		if strings.Contains(k, frag) {
			return true
		}
	}
	return false
}

func secretEnvValues(env map[string]string) []string {
	var out []string
	for key, value := range env {
		if len(value) < 6 || !secretEnvKey(key) {
			continue
		}
		out = append(out, value)
	}
	sort.Slice(out, func(i, j int) bool { return len(out[i]) > len(out[j]) })
	return out
}

func secretEnvKey(key string) bool {
	k := strings.ToUpper(key)
	for _, frag := range []string{
		"SECRET", "TOKEN", "PASSWORD", "PASSWD", "PRIVATE", "CREDENTIAL",
		"AUTHORIZATION", "API_KEY", "APIKEY", "SESSION", "COOKIE", "SIGNATURE",
		"DSN", "CONNECTION", "WEBHOOK",
	} {
		if strings.Contains(k, frag) {
			return true
		}
	}
	return strings.HasSuffix(k, "_KEY") || strings.HasSuffix(k, "_TOKEN")
}
