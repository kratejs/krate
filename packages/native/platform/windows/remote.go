package windows

import (
	"strings"

	"github.com/kratejs/krate/packages/native/platform/caps"
)

// frameNavAllowed says whether a navigation of a frame (not the top level) of
// a remote view may proceed: the page's allowlist applies, and in-document
// schemes that cannot reach the app are fine.
func frameNavAllowed(p *caps.RemotePolicy, uri string) bool {
	if i := strings.IndexByte(uri, ':'); i > 0 {
		switch strings.ToLower(uri[:i]) {
		case "about", "data", "blob":
			return true
		}
	}
	return p.Decide(uri) == caps.NavAllow
}
