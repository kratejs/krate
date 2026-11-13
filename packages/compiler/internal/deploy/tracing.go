package deploy

import (
	"path"
	"sort"
	"strings"
)

// traceIncludeDirs reduces the outputFileTracingIncludes globs to the shell-
// quoted project paths a Dockerfile copies: each glob's leading directory
// without wildcards (the glob itself when it has none). Paths under
// node_modules are left out, the image already has the installed packages.
// Globs with no leading directory are returned in skipped.
func traceIncludeDirs(includes map[string][]string) (dirs, skipped []string) {
	seen := map[string]bool{}
	for _, globs := range includes {
		for _, g := range globs {
			g = path.Clean(strings.ReplaceAll(g, "\\", "/"))
			var lead []string
			for _, seg := range strings.Split(g, "/") {
				if strings.ContainsAny(seg, "*?[") {
					break
				}
				lead = append(lead, seg)
			}
			p := strings.Join(lead, "/")
			if p == "" || p == "." {
				if !seen["glob:"+g] {
					seen["glob:"+g] = true
					skipped = append(skipped, g)
				}
				continue
			}
			if p == "node_modules" || strings.HasPrefix(p, "node_modules/") || strings.Contains(p, "/node_modules/") || strings.ContainsAny(p, "'\"`$\n") {
				continue
			}
			if !seen[p] {
				seen[p] = true
				dirs = append(dirs, "'"+p+"'")
			}
		}
	}
	sort.Strings(dirs)
	sort.Strings(skipped)
	return dirs, skipped
}
