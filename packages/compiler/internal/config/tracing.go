package config

import (
	"fmt"
	"path/filepath"
	"strings"
)

// checkTraceGlob rejects an include glob that is absolute or climbs out of the
// project root. The build checks every matched file against the root again.
func checkTraceGlob(g string) error {
	if g == "" {
		return fmt.Errorf("empty glob")
	}
	slash := filepath.ToSlash(g)
	if strings.HasPrefix(slash, "/") || filepath.IsAbs(g) || (len(slash) > 1 && slash[1] == ':') {
		return fmt.Errorf("glob %q must be relative to the project root", g)
	}
	for _, seg := range strings.Split(slash, "/") {
		if seg == ".." {
			return fmt.Errorf("glob %q must stay inside the project root (no .. segments)", g)
		}
	}
	return nil
}
