package build

import (
	"regexp"
	"sort"
	"strings"
)

// neverMatchFilter is an esbuild plugin filter that matches no path. It keeps
// a callback registered (so plugin order is unchanged) while esbuild skips it
// natively.
const neverMatchFilter = `[^\x00-\x{10FFFF}]`

// externalPackageFilter matches the specifiers externalNodeModulesPlugin
// considers: bare ones (not relative or rooted) without a colon anywhere.
const externalPackageFilter = `^[^./:][^:]*$`

// exactSpecFilter returns a plugin filter that matches exactly the given
// specifiers and nothing else.
func exactSpecFilter(specs []string) string {
	if len(specs) == 0 {
		return neverMatchFilter
	}
	quoted := make([]string, 0, len(specs))
	for _, s := range specs {
		quoted = append(quoted, regexp.QuoteMeta(s))
	}
	sort.Strings(quoted)
	return `^(?:` + strings.Join(quoted, "|") + `)$`
}
