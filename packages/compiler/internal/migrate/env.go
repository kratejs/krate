package migrate

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

var publicEnvRe = regexp.MustCompile(`process\.env(?:\.([A-Za-z_][A-Za-z0-9_]*)|\[\s*["']([^"']+)["']\s*\])`)

// publicEnvDestructure matches `const { NEXT_PUBLIC_X } = process.env`.
var publicEnvDestructure = regexp.MustCompile(`\{[^}]*\bNEXT_PUBLIC_\w+[^}]*\}\s*=\s*process\.env\b`)

const (
	nextPublicPrefix  = "NEXT_PUBLIC_"
	krateEnvPublicPre = "KRATE_PUBLIC_"
)

// krateEnvName maps a Next client-visible variable to the name Krate inlines.
func krateEnvName(key string) string {
	return krateEnvPublicPre + strings.TrimPrefix(key, nextPublicPrefix)
}

// readPublicEnv lists which .env files define each NEXT_PUBLIC_* variable.
// Only names are kept: values are never read into the plan, because the files
// belong to the developer's machine and may hold development URLs or keys.
func readPublicEnv(root string) map[string][]string {
	out := map[string][]string{}
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
			key, _, ok := strings.Cut(line, "=")
			if !ok {
				continue
			}
			key = strings.TrimSpace(key)
			if strings.HasPrefix(key, nextPublicPrefix) {
				out[key] = append(out[key], name)
			}
		}
	}
	return out
}

// rewritePublicEnv points reads of Next's client-visible variables at the
// KRATE_PUBLIC_* names Krate inlines at build time, and reports the variables
// to rename. cfgEnv holds the keys of next.config `env`, which Next inlines
// into every bundle; their reads get the same treatment. No value is ever
// written into the migrated source.
func rewritePublicEnv(rep *Report, defined map[string][]string, cfgEnv map[string]string) {
	used := map[string]bool{}
	for i := range rep.Files {
		f := &rep.Files[i]
		if len(f.Content) == 0 || f.Kind == KindPublic || f.Kind == KindConfig || f.Kind == KindPackage {
			continue
		}
		body := string(f.Content)
		changed := false
		body = publicEnvRe.ReplaceAllStringFunc(body, func(match string) string {
			sub := publicEnvRe.FindStringSubmatch(match)
			key := sub[1]
			if key == "" {
				key = sub[2]
			}
			_, inCfg := cfgEnv[key]
			if !strings.HasPrefix(key, nextPublicPrefix) && !inCfg {
				return match
			}
			used[key] = true
			changed = true
			return "process.env." + krateEnvName(key)
		})
		f.Content = []byte(body)
		if changed {
			f.Rewrites = append(f.Rewrites, "renamed NEXT_PUBLIC_* environment reads to KRATE_PUBLIC_*")
		}
		if publicEnvDestructure.MatchString(body) {
			rep.Manual = append(rep.Manual, ManualItem{Source: f.Source, Reason: "NEXT_PUBLIC_* variables are destructured from process.env", Hint: "read each one as process.env.KRATE_PUBLIC_NAME; Krate inlines only direct reads"})
		}
	}

	// Union of what code reads, what .env files define and what next.config sets.
	all := map[string]bool{}
	for k := range used {
		all[k] = true
	}
	for k := range defined {
		all[k] = true
	}
	for k := range cfgEnv {
		all[k] = true
	}
	if len(all) == 0 {
		return
	}
	keys := make([]string, 0, len(all))
	for k := range all {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		part := k + " -> " + krateEnvName(k)
		if files := defined[k]; len(files) > 0 {
			part += " (defined in " + strings.Join(files, ", ") + ")"
		} else if _, ok := cfgEnv[k]; ok {
			part += " (set in next.config env)"
		} else {
			part += " (no .env definition found)"
		}
		parts = append(parts, part)
	}
	rep.Manual = append(rep.Manual, ManualItem{
		Source: "environment",
		Reason: "public environment variables are not copied; rename or set them for the Krate build: " + strings.Join(parts, "; "),
		Hint:   "Krate inlines only KRATE_PUBLIC_* at build time (process.env.KRATE_PUBLIC_NAME). Rename them in your .env files or set them in the build environment; values were not written into source",
	})
}
