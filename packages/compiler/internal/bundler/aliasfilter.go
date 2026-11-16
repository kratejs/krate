package bundler

import (
	"regexp"
	"sort"
	"strings"
)

// aliasNeverMatch is a plugin filter that matches no specifier.
const aliasNeverMatch = `[^\x00-\x{10FFFF}]`

// AliasFilter returns a Go regexp that matches every import specifier
// ResolveAlias can resolve, and no other bare one. Whether an alias target
// exists is still decided by ResolveAlias. Aliases starting with "." or "/"
// are left out: ResolveAlias never resolves a relative specifier, and the
// plugin filter this feeds only covers bare ones. With no aliases the filter
// matches nothing.
func (r *ImportResolver) AliasFilter() string {
	if r == nil || len(r.aliases) == 0 || r.baseDir == "" {
		return aliasNeverMatch
	}
	var alts []string
	for _, a := range r.aliases {
		prefix := a.prefix
		if prefix == "" || prefix[0] == '.' || prefix[0] == '/' {
			continue
		}
		if base, wild := strings.CutSuffix(prefix, "/*"); wild {
			alts = append(alts, regexp.QuoteMeta(base)+`(?:$|/)`)
		} else {
			alts = append(alts, regexp.QuoteMeta(prefix)+`$`)
		}
	}
	if len(alts) == 0 {
		return aliasNeverMatch
	}
	sort.Strings(alts)
	return `^(?:` + strings.Join(alts, "|") + `)`
}
