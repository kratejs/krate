package build

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

// uaOptIn reports whether User-Agent-based document selection is enabled
// (hybrid mode + compat.userAgent).
func uaOptIn(c config.CompatConfig) bool {
	return c.UserAgent && c.ModeOrDefault() == "hybrid"
}

// isHTMLDocRequest reports whether a request path maps to an HTML document
// (rather than a hashed asset or other file). Vary/Accept-CH are scoped to these
// so immutable content-hashed assets are not fragmented per-visitor in caches.
func isHTMLDocRequest(urlPath string) bool {
	return urlPath == "" || strings.HasSuffix(urlPath, "/") || strings.HasSuffix(urlPath, ".html")
}

// uaPrefersLegacyDoc reports whether a request should receive the legacy
// sibling document. It prefers Chromium's User-Agent Client Hint (`Sec-CH-UA`,
// when the browser sends it) over the UA string, then falls back to parsing the
// UA. It is conservative: only confidently-modern engines get the default/short
// document; anything it cannot classify gets the legacy document, which works on
// every browser.
func uaPrefersLegacyDoc(ua, secCHUA string) bool {
	if v, ok := chromiumMajor(ua, secCHUA); ok {
		return v < 105
	}
	// Firefox: :has() shipped in 121.
	if v, ok := uaMajor(ua, "Firefox/"); ok {
		return v < 121
	}
	if v, ok := uaMajor(ua, "FxiOS/"); ok {
		return v < 121
	}
	// Safari: :has() shipped in 15.4. The engine version is the Version/ token.
	if maj, min, ok := uaMajorMinor(ua, "Version/"); ok && strings.Contains(ua, "Safari/") {
		return maj < 15 || (maj == 15 && min < 4)
	}
	return true
}

// chromiumMajor returns the Chromium engine major version from the client hint
// (preferred) or the UA string.
func chromiumMajor(ua, secCHUA string) (int, bool) {
	for _, brand := range []string{"Chromium", "Google Chrome", "Microsoft Edge", "Opera"} {
		if v, ok := chBrandVersion(secCHUA, brand); ok {
			return v, true
		}
	}
	for _, tok := range []string{"Chrome/", "Chromium/", "CriOS/", "Edg/", "EdgiOS/", "OPR/"} {
		if v, ok := uaMajor(ua, tok); ok {
			return v, true
		}
	}
	return 0, false
}

// chBrandVersion parses `"<brand>";v="<major>"` out of a Sec-CH-UA header.
func chBrandVersion(secCHUA, brand string) (int, bool) {
	needle := brand + `";v="`
	i := strings.Index(secCHUA, needle)
	if i < 0 {
		return 0, false
	}
	rest := secCHUA[i+len(needle):]
	end := 0
	for end < len(rest) && rest[end] >= '0' && rest[end] <= '9' {
		end++
	}
	if end == 0 {
		return 0, false
	}
	v, err := strconv.Atoi(rest[:end])
	if err != nil {
		return 0, false
	}
	return v, true
}

// uaMajor extracts the leading integer after token in ua.
func uaMajor(ua, token string) (int, bool) {
	i := strings.Index(ua, token)
	if i < 0 {
		return 0, false
	}
	rest := ua[i+len(token):]
	end := 0
	for end < len(rest) && rest[end] >= '0' && rest[end] <= '9' {
		end++
	}
	if end == 0 {
		return 0, false
	}
	v, err := strconv.Atoi(rest[:end])
	if err != nil {
		return 0, false
	}
	return v, true
}

// uaMajorMinor extracts "major.minor" after token in ua.
func uaMajorMinor(ua, token string) (int, int, bool) {
	i := strings.Index(ua, token)
	if i < 0 {
		return 0, 0, false
	}
	rest := ua[i+len(token):]
	dot := strings.IndexByte(rest, '.')
	if dot < 0 {
		return 0, 0, false
	}
	maj, err1 := strconv.Atoi(leadingInt(rest[:dot]))
	min, err2 := strconv.Atoi(leadingInt(rest[dot+1:]))
	if err1 != nil || err2 != nil {
		return 0, 0, false
	}
	return maj, min, true
}

func leadingInt(s string) string {
	end := 0
	for end < len(s) && s[end] >= '0' && s[end] <= '9' {
		end++
	}
	return s[:end]
}

// siblingDocPath maps a request path to its UA-variant document on disk
// (`index.<kind>.html`), or "" when the request is not an HTML document path or
// the sibling does not exist.
func siblingDocPath(absOut, urlPath, kind string) string {
	p := filepath.FromSlash(urlPath)
	var rel string
	switch {
	case strings.HasSuffix(urlPath, "/") || urlPath == "":
		rel = filepath.Join(strings.TrimPrefix(p, string(filepath.Separator)), "index."+kind+".html")
	case strings.HasSuffix(urlPath, ".html"):
		rel = strings.TrimSuffix(p, ".html") + "." + kind + ".html"
	default:
		return ""
	}
	full := filepath.Join(absOut, rel)
	// Guard against traversal outside the output root.
	clean := filepath.Clean(full)
	if !strings.HasPrefix(clean, filepath.Clean(absOut)) {
		return ""
	}
	if fi, err := os.Stat(clean); err == nil && !fi.IsDir() {
		return clean
	}
	return ""
}
