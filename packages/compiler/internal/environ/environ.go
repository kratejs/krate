// Package environ loads project `.env` files and exposes the resolved set to
// the rest of the compiler. Values are build/serve-time only and must never
// reach client HTML/JS/hydration.
// Precedence (increasing): `.env` -> `.env.<mode>` -> `.env.local` ->
// `.env.<mode>.local`. Shell environment always wins over files (dotenv
// `override=false` semantics).
package environ

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Verbose logs which named env files were found/loaded. Values are never
// logged. Mirrors plugin.SetVerbose.
var Verbose bool

// Current holds the resolved project env for this build/serve process. It is
// set once at startup (cmd/krate main) after Load, and read by serve-time
// consumers (plugin JS VMs, API route runtime, sidecars) that do not have
// access to the Builder.
var Current map[string]string

// Mode resolves the environment mode: KRATE_ENV wins, then NODE_ENV, then the
// command-specific default ("development" for dev, "production" otherwise).
func Mode(defaultMode string) string {
	if v := os.Getenv("KRATE_ENV"); v != "" {
		return v
	}
	if v := os.Getenv("NODE_ENV"); v != "" {
		return v
	}
	return defaultMode
}

// Load reads the project's `.env` files for the given mode and returns the
// merged result. Shell environment variables are never overridden by files.
func Load(root, mode string) (map[string]string, error) {
	merged := map[string]string{}
	files := []string{
		".env",
		".env." + mode,
		".env.local",
		".env." + mode + ".local",
	}
	for _, rel := range files {
		data, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			return nil, fmt.Errorf("reading %s: %w", rel, err)
		}
		if Verbose {
			fmt.Fprintf(os.Stderr, "env: loaded %s\n", rel)
		}
		if err := parse(data, merged); err != nil {
			return nil, fmt.Errorf("parsing %s: %w", rel, err)
		}
	}
	return merged, nil
}

// KVList returns the map as sorted KEY=VALUE entries suitable for appending to
// a command's environment. Sorting keeps output deterministic.
func KVList(env map[string]string) []string {
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]string, 0, len(env))
	for _, k := range keys {
		out = append(out, k+"="+env[k])
	}
	return out
}

var expandRe = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}|\$([A-Za-z_][A-Za-z0-9_]*)`)

// parse applies one file's KEY=VALUE lines onto merged. Keys already present
// in the shell environment are skipped (shell wins over every file).
func parse(data []byte, merged map[string]string) error {
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// Optional `export KEY=VALUE` prefix.
		if strings.HasPrefix(line, "export ") {
			line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
		}
		eq := strings.IndexByte(line, '=')
		if eq < 0 {
			continue
		}
		key := strings.TrimSpace(line[:eq])
		if key == "" {
			continue
		}
		if _, shellSet := os.LookupEnv(key); shellSet {
			continue
		}
		merged[key] = parseValue(strings.TrimSpace(line[eq+1:]), merged)
	}
	return nil
}

// parseValue strips surrounding quotes, cuts inline comments, and expands
// $VAR/${VAR} references. Single-quoted values are literal (no expansion).
func parseValue(raw string, merged map[string]string) string {
	if raw == "" {
		return ""
	}
	switch raw[0] {
	case '\'':
		if len(raw) >= 2 && raw[len(raw)-1] == '\'' {
			return raw[1 : len(raw)-1]
		}
		return raw
	case '"':
		if len(raw) >= 2 && raw[len(raw)-1] == '"' {
			return expand(raw[1:len(raw)-1], merged)
		}
		return raw
	default:
		// Unquoted values: a ` #` starts a comment, but a bare `#` inside the
		// value (e.g. URLs) is preserved.
		if i := strings.Index(raw, " #"); i >= 0 {
			raw = strings.TrimSpace(raw[:i])
		}
		return expand(raw, merged)
	}
}

// expand replaces $VAR and ${VAR} references, resolving from the merged map
// first (in-file chaining) and then the shell environment. Missing variables
// expand to the empty string.
func expand(s string, merged map[string]string) string {
	return expandRe.ReplaceAllStringFunc(s, func(m string) string {
		name := strings.TrimSuffix(strings.TrimPrefix(m, "${"), "}")
		name = strings.TrimPrefix(name, "$")
		if v, ok := merged[name]; ok {
			return v
		}
		return os.Getenv(name)
	})
}
