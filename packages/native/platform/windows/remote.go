package windows

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"github.com/kratejs/krate/packages/native/platform/caps"
)

// Pure helpers of the remote content support (remote_windows.go).

// profileName is the WebView2 profile of a storage partition. WebView2 limits
// profile names to 64 characters; a longer partition name is hashed. The
// prefix keeps the names apart from the app's own (default) profile, and
// in-memory partitions from persistent ones.
func profileName(p caps.Partition) string {
	kind := "m"
	if p.Persistent {
		kind = "p"
	}
	name := "kr-" + kind + "-" + p.Name
	if len(name) > 64 {
		h := sha256.Sum256([]byte(p.Name))
		name = "kr-" + kind + "-" + hex.EncodeToString(h[:])[:40]
	}
	return name
}

// certErrorNames maps COREWEBVIEW2_WEB_ERROR_STATUS of a server certificate
// error to the names caps.CertError uses.
func certErrorNames(status int32) []string {
	switch status {
	case 1:
		return []string{"mismatch"}
	case 2:
		return []string{"expired"}
	case 4:
		return []string{"revoked"}
	case 5:
		return []string{"untrusted"}
	}
	return []string{"unknown"}
}

// parseChallenge splits a WWW-Authenticate challenge ("Basic realm=\"x\"")
// into its scheme and realm.
func parseChallenge(c string) (scheme, realm string) {
	c = strings.TrimSpace(c)
	scheme, rest, _ := strings.Cut(c, " ")
	scheme = strings.ToLower(scheme)
	if scheme == "" {
		scheme = "other"
	}
	i := strings.Index(strings.ToLower(rest), "realm=")
	if i < 0 {
		return scheme, ""
	}
	v := strings.TrimSpace(rest[i+len("realm="):])
	if strings.HasPrefix(v, `"`) {
		if j := strings.Index(v[1:], `"`); j >= 0 {
			return scheme, v[1 : 1+j]
		}
		return scheme, strings.Trim(v, `"`)
	}
	if j := strings.IndexAny(v, ", "); j >= 0 {
		v = v[:j]
	}
	return scheme, v
}

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
