package migrate

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

var publicEnvRe = regexp.MustCompile(`process\.env(?:\.([A-Za-z_][A-Za-z0-9_]*)|\[\s*["']([^"']+)["']\s*\])`)

// publicEnvDestructure matches `const { NEXT_PUBLIC_X } = process.env`.
var publicEnvDestructure = regexp.MustCompile(`\{[^}]*\bNEXT_PUBLIC_\w+[^}]*\}\s*=\s*process\.env\b`)

var identRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

var nextPublicNameRe = regexp.MustCompile(`NEXT_PUBLIC_\w+`)

const (
	nextPublicPrefix  = "NEXT_PUBLIC_"
	krateEnvPublicPre = "KRATE_PUBLIC_"
)

// krateEnvName maps a Next client-visible variable to the name Krate inlines.
func krateEnvName(key string) string {
	return krateEnvPublicPre + strings.TrimPrefix(key, nextPublicPrefix)
}

// publicEnvFiles are the env files scanned for public variable names.
// .env.example documents the variables a project expects even when the real
// values live elsewhere.
var publicEnvFiles = []string{".env", ".env.local", ".env.development", ".env.development.local", ".env.production", ".env.production.local", ".env.example"}

// readPublicEnv lists which env files define each NEXT_PUBLIC_* variable, in
// root and in each extra directory (the envDir loadEnvConfig reads). Files are
// named relative to root. Only names are kept: values are never read into the
// plan, because the files belong to the developer's machine and may hold
// development URLs or keys.
func readPublicEnv(root string, extraDirs ...string) map[string][]string {
	out := map[string][]string{}
	seen := map[string]bool{}
	for _, dir := range append([]string{root}, extraDirs...) {
		for _, name := range publicEnvFiles {
			path := filepath.Join(dir, name)
			if seen[path] {
				continue
			}
			seen[path] = true
			data, err := os.ReadFile(path)
			if err != nil {
				continue
			}
			shown := name
			if rel, err := filepath.Rel(root, path); err == nil {
				shown = filepath.ToSlash(rel)
			}
			for _, line := range strings.Split(string(data), "\n") {
				line = strings.TrimSpace(line)
				if line == "" || strings.HasPrefix(line, "#") {
					continue
				}
				line = strings.TrimPrefix(line, "export ")
				key, _, ok := strings.Cut(line, "=")
				if !ok {
					continue
				}
				key = strings.TrimSpace(key)
				if strings.HasPrefix(key, nextPublicPrefix) {
					out[key] = append(out[key], shown)
				}
			}
		}
	}
	return out
}

// rewritePublicEnv handles Next's client-visible variables. NEXT_PUBLIC_*
// reads keep their names: the generated config lists "NEXT_PUBLIC_" in
// envPrefix, so Krate inlines them from the same .env files and build
// environment as Next did. Variables that next.config `env` injects under other
// names are renamed to KRATE_PUBLIC_*, because no prefix covers them. No value
// is ever written into the migrated source. It reports whether the app reads
// a NEXT_PUBLIC_ variable, which is when envPrefix is needed.
func rewritePublicEnv(rep *Report, defined map[string][]string, cfgEnv map[string]string) (usesNextPublic bool) {
	reads := map[string]bool{}
	renamed := map[string]bool{}
	for i := range rep.Files {
		f := &rep.Files[i]
		if len(f.Content) == 0 || f.Kind == KindPublic || f.Kind == KindConfig || f.Kind == KindPackage {
			continue
		}
		body := string(f.Content)
		changed := false
		for _, sub := range publicEnvRe.FindAllStringSubmatch(body, -1) {
			key := sub[1]
			if key == "" {
				key = sub[2]
			}
			if strings.HasPrefix(key, nextPublicPrefix) {
				reads[key] = true
			}
		}
		body = publicEnvRe.ReplaceAllStringFunc(body, func(match string) string {
			sub := publicEnvRe.FindStringSubmatch(match)
			key := sub[1]
			if key == "" {
				key = sub[2]
			}
			if strings.HasPrefix(key, nextPublicPrefix) {
				if sub[1] == "" && identRe.MatchString(key) {
					// Krate inlines direct member reads only.
					changed = true
					return "process.env." + key
				}
				return match
			}
			if _, inCfg := cfgEnv[key]; !inCfg {
				return match
			}
			renamed[key] = true
			changed = true
			return "process.env." + krateEnvName(key)
		})
		if publicEnvDestructure.MatchString(body) {
			for _, m := range publicEnvDestructure.FindAllString(body, -1) {
				for _, name := range nextPublicNameRe.FindAllString(m, -1) {
					reads[name] = true
				}
			}
		}
		f.Content = []byte(body)
		if changed {
			f.Rewrites = append(f.Rewrites, "normalized process.env reads of public variables (next.config env names became KRATE_PUBLIC_*)")
		}
	}

	if len(reads) > 0 {
		names := make([]string, 0, len(reads))
		for k := range reads {
			names = append(names, k)
		}
		sort.Strings(names)
		files := map[string]bool{}
		var missing []string
		for _, k := range names {
			if len(defined[k]) == 0 {
				missing = append(missing, k)
				continue
			}
			for _, f := range defined[k] {
				files[f] = true
			}
		}
		var fl []string
		for f := range files {
			fl = append(fl, f)
		}
		sort.Strings(fl)
		msg := "public env vars keep their NEXT_PUBLIC_ names (envPrefix)"
		if len(fl) > 0 {
			msg += "; defined in " + strings.Join(fl, ", ") + ": " + strconv.Itoa(len(names)-len(missing)) + " of " + strconv.Itoa(len(names))
		}
		if len(missing) > 0 {
			msg += "; not found in any .env file (set them in the build environment): " + strings.Join(missing, ", ")
		}
		rep.Info = append(rep.Info, msg)
	}

	if len(renamed) == 0 {
		return len(reads) > 0
	}
	keys := make([]string, 0, len(renamed))
	for k := range renamed {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+" -> "+krateEnvName(k)+" (set in next.config env)")
	}
	rep.Manual = append(rep.Manual, ManualItem{
		Source: "environment",
		Reason: "next.config env variables are not copied; set them for the Krate build: " + strings.Join(parts, "; "),
		Hint:   "Krate inlines KRATE_PUBLIC_* (and the prefixes in envPrefix) at build time (process.env.KRATE_PUBLIC_NAME). Define the renamed variables in your .env files or the build environment; values were not written into source",
	})
	return len(reads) > 0
}
