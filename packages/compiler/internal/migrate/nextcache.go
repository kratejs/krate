package migrate

import (
	"regexp"
	"strings"
)

// nextCacheNames are the next/cache exports Krate implements natively in
// @krate/runtime/server.
var nextCacheNames = map[string]bool{
	"revalidatePath":   true,
	"revalidateTag":    true,
	"unstable_cache":   true,
	"unstable_noStore": true,
}

// nextCacheUnsupported maps next/cache exports with no Krate equivalent to
// the hint shown in the migration report.
var nextCacheUnsupported = map[string]string{
	"updateTag":          "use revalidateTag from a server action; read-your-writes is already applied before the action responds",
	"refresh":            "use the router refresh() from the migrated next/navigation helper",
	"cacheLife":          "set export const revalidate on the page or the revalidate option of fetch/unstable_cache",
	"cacheTag":           "pass tags through fetch(url, { next: { tags } }) or unstable_cache(fn, keys, { tags })",
	"unstable_cacheLife": "set export const revalidate on the page or the revalidate option of fetch/unstable_cache",
	"unstable_cacheTag":  "pass tags through fetch(url, { next: { tags } }) or unstable_cache(fn, keys, { tags })",
}

var nextCacheImport = regexp.MustCompile(`(?m)^([ \t]*)import[ \t]+(type[ \t]+)?\{([^}]*)\}[ \t]*from[ \t]+(["'])next/cache["'][ \t]*;?`)

// rewriteNextCache maps next/cache imports onto @krate/runtime/server. Names
// Krate does not implement stay imported from next/cache (so the build fails
// loudly instead of silently changing behavior) and are reported as manual
// work.
func rewriteNextCache(src string) (string, []string, []ManualItem) {
	var rewrites []string
	var manual []ManualItem
	if !strings.Contains(src, "next/cache") {
		return src, nil, nil
	}
	out := nextCacheImport.ReplaceAllStringFunc(src, func(stmt string) string {
		m := nextCacheImport.FindStringSubmatch(stmt)
		indent, typeOnly, specs, quote := m[1], strings.TrimSpace(m[2]), m[3], m[4]
		var keep, move []string
		for _, spec := range strings.Split(specs, ",") {
			spec = strings.TrimSpace(spec)
			if spec == "" {
				continue
			}
			name := strings.TrimSpace(strings.TrimPrefix(spec, "type "))
			if i := strings.Index(name, " as "); i >= 0 {
				name = strings.TrimSpace(name[:i])
			}
			if nextCacheNames[name] {
				move = append(move, spec)
				continue
			}
			keep = append(keep, spec)
			hint, known := nextCacheUnsupported[name]
			if !known {
				hint = "no Krate equivalent; remove the call or replace it manually"
			}
			manual = append(manual, ManualItem{Reason: name + " from next/cache is not migrated", Hint: hint})
		}
		var lines []string
		prefix := indent + "import "
		if typeOnly != "" {
			prefix += "type "
		}
		if len(move) > 0 {
			lines = append(lines, prefix+"{ "+strings.Join(move, ", ")+" } from "+quote+"@krate/runtime/server"+quote+";")
			rewrites = append(rewrites, "rewrote next/cache imports to @krate/runtime/server")
		}
		if len(keep) > 0 {
			lines = append(lines, prefix+"{ "+strings.Join(keep, ", ")+" } from "+quote+"next/cache"+quote+";")
		}
		if len(lines) == 0 {
			return ""
		}
		return strings.Join(lines, "\n")
	})
	if strings.Contains(out, "next/cache") && len(manual) == 0 {
		manual = append(manual, ManualItem{Reason: "next/cache import could not be rewritten automatically", Hint: "import revalidatePath/revalidateTag/unstable_cache from @krate/runtime/server"})
	}
	return out, rewrites, manual
}
