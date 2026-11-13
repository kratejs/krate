package migrate

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// dropRouteConfig matches the Next.js route segment exports that only steer
// Vercel's deployment (runtime, maxDuration, preferredRegion) with a plain
// literal value on one line. Krate self-hosts on Node, where they do nothing.
var dropRouteConfig = regexp.MustCompile(`(?m)^export\s+const\s+(runtime|maxDuration|preferredRegion)\s*=\s*("[^"\n]*"|'[^'\n]*'|\d+|\[[^\]\n]*\])\s*(?:as\s+const\s*)?;?[ \t]*\r?\n?`)

const droppedConfigPrefix = "dropped route segment config "

// dropSegmentConfig removes runtime, maxDuration and preferredRegion exports
// and reports each removal as an info note. A name that is also used elsewhere
// in the file is kept, so a live reference is never broken.
func dropSegmentConfig(src string) (string, []string) {
	var notes []string
	out := dropRouteConfig.ReplaceAllStringFunc(src, func(line string) string {
		m := dropRouteConfig.FindStringSubmatch(line)
		name, value := m[1], m[2]
		if identifierUsedElsewhere(src, name, line) {
			return line
		}
		notes = append(notes, droppedConfigPrefix+name+" = "+value+": "+segmentConfigReason(name, value))
		return ""
	})
	return out, notes
}

func segmentConfigReason(name, value string) string {
	switch name {
	case "runtime":
		if strings.Trim(value, `"'`) == "edge" {
			return "Krate runs the handler on its own API runtime, which implements the web Request/Response APIs the edge runtime offers"
		}
		return "Krate API routes already run on Node (or the runtime set in api.runtime)"
	case "maxDuration":
		return "Krate has no per-route time limit; see the aggregated maxDuration warning"
	}
	return "Krate deploys to the target you choose, not to a Vercel region"
}

// identifierUsedElsewhere reports whether name appears in code outside the
// declaration line (comment-only lines are ignored).
func identifierUsedElsewhere(src, name, decl string) bool {
	re := regexp.MustCompile(`\b` + name + `\b`)
	rest := strings.Replace(src, decl, "", 1)
	for _, ln := range strings.Split(rest, "\n") {
		t := strings.TrimSpace(ln)
		if strings.HasPrefix(t, "//") || strings.HasPrefix(t, "*") || strings.HasPrefix(t, "/*") {
			continue
		}
		if re.MatchString(ln) {
			return true
		}
	}
	return false
}

const fetchCacheManual = "route segment config fetchCache has no Krate equivalent and was left in place"

// aggregateSegmentConfig turns the per-file segment config findings into one
// warning per kind (instead of one manual item per file) and one manual item
// for fetchCache, whose caching behavior a human has to decide.
func aggregateSegmentConfig(rep *Report) {
	counts := map[string]int{}
	maxDuration := 0
	for _, f := range rep.Files {
		for _, r := range f.Rewrites {
			if !strings.HasPrefix(r, droppedConfigPrefix) {
				continue
			}
			rest := strings.TrimPrefix(r, droppedConfigPrefix)
			name, value, _ := strings.Cut(rest, " = ")
			counts[name]++
			if name == "maxDuration" {
				v, _, _ := strings.Cut(value, ":")
				if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && n > maxDuration {
					maxDuration = n
				}
			}
		}
	}
	names := make([]string, 0, len(counts))
	for n := range counts {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		switch n {
		case "runtime":
			rep.Warnings = append(rep.Warnings, fmt.Sprintf("dropped export const runtime from %d files: Krate API routes run on Node (or the configured api.runtime), so it has no effect", counts[n]))
		case "maxDuration":
			msg := fmt.Sprintf("dropped export const maxDuration from %d files (longest %ds): Krate has no per-route time limit", counts[n], maxDuration)
			if maxDuration > 30 {
				msg += "; the API proxy fails a request whose handler sends no response headers within 30s (the embedded runtime also fails handlers after KRATE_API_TIMEOUT, 30s), so stream the response or move long work into after()"
			}
			rep.Warnings = append(rep.Warnings, msg)
		case "preferredRegion":
			rep.Warnings = append(rep.Warnings, fmt.Sprintf("dropped export const preferredRegion from %d files: region placement is a property of where you deploy Krate", counts[n]))
		}
	}

	var files []string
	kept := rep.Manual[:0]
	for _, m := range rep.Manual {
		if m.Reason == fetchCacheManual {
			files = append(files, m.Source)
			continue
		}
		kept = append(kept, m)
	}
	rep.Manual = kept
	if len(files) > 0 {
		sort.Strings(files)
		list := strings.Join(files, ", ")
		if len(files) > 8 {
			list = strings.Join(files[:8], ", ") + fmt.Sprintf(" and %d more", len(files)-8)
		}
		rep.Manual = append(rep.Manual, ManualItem{
			Source: files[0],
			Reason: fmt.Sprintf("route segment config fetchCache is not read by Krate (%d files: %s)", len(files), list),
			Hint:   "set the cache option on each fetch call (cache: 'no-store' or next: { revalidate }) where the segment default mattered",
		})
	}
}
