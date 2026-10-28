package migrate

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

var publicEnvRe = regexp.MustCompile(`process\.env(?:\.([A-Za-z_][A-Za-z0-9_]*)|\[\s*["']([^"']+)["']\s*\])`)

// readPublicEnv reads NEXT_PUBLIC_* values from the standard .env files, later
// files overriding earlier ones.
func readPublicEnv(root string) map[string]string {
	out := map[string]string{}
	for _, name := range []string{".env", ".env.local", ".env.development", ".env.development.local", ".env.production", ".env.production.local"} {
		data, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			line = strings.TrimPrefix(line, "export ")
			key, value, ok := strings.Cut(line, "=")
			if !ok {
				continue
			}
			key = strings.TrimSpace(key)
			if !strings.HasPrefix(key, "NEXT_PUBLIC_") {
				continue
			}
			value = strings.TrimSpace(value)
			if len(value) >= 2 && ((value[0] == '"' && value[len(value)-1] == '"') || (value[0] == '\'' && value[len(value)-1] == '\'')) {
				value = value[1 : len(value)-1]
			}
			out[key] = value
		}
	}
	return out
}

// rewritePublicEnv inlines Next's build-time NEXT_PUBLIC_* values. Krate does
// not expose server env values to client code, so unresolved public refs stay
// manual rather than silently becoming undefined.
func rewritePublicEnv(rep *Report, values map[string]string) {
	for i := range rep.Files {
		f := &rep.Files[i]
		if len(f.Content) == 0 {
			continue
		}
		body := string(f.Content)
		replaced := false
		var unresolved []string
		body = publicEnvRe.ReplaceAllStringFunc(body, func(match string) string {
			sub := publicEnvRe.FindStringSubmatch(match)
			key := sub[1]
			if key == "" {
				key = sub[2]
			}
			if !strings.HasPrefix(key, "NEXT_PUBLIC_") {
				return match
			}
			value, ok := values[key]
			if !ok {
				unresolved = append(unresolved, key)
				return match
			}
			replaced = true
			return strconv.Quote(value)
		})
		f.Content = []byte(body)
		if replaced {
			f.Rewrites = append(f.Rewrites, "inlined NEXT_PUBLIC_* environment values")
		}
		for _, key := range unresolved {
			rep.Manual = append(rep.Manual, ManualItem{Source: f.Source, Reason: "public env " + key + " has no value in .env or next.config", Hint: "add it to .env or replace it manually"})
		}
	}
}
