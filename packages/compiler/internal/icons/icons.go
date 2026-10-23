package icons

import (
	"fmt"
	"html"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

var (
	// Extract the inner elements of an SVG tag. Non-greedy so a crafted body
	// with multiple `</svg>` markers can't backtrack excessively.
	svgInnerRegex = regexp.MustCompile(`(?s)<svg\b[^>]*>(.*?)</svg>`)
	// viewBoxRegex pulls the viewBox off the source <svg> element so local
	// icons keep their own geometry instead of inheriting the 24x24 default.
	viewBoxRegex = regexp.MustCompile(`(?i)\bviewBox\s*=\s*["']([^"']+)["']`)
	httpClient   = &http.Client{Timeout: 5 * time.Second}
	fetchMutex   sync.Mutex // Prevents concurrent compilation steps from downloading the same icon simultaneously
)

type Icon struct {
	// Inner is the markup inside the source <svg> element (its paths, circles,
	// etc.), sanitized for safe insertion into page HTML.
	Inner string
	// ViewBox is the viewBox attribute from the source <svg> element. Empty for
	// Iconify-sourced icons, which always use the default 24x24 geometry.
	ViewBox string
}

// validIconName is a conservative character set for icon names/prefixes. It
// excludes path separators, "..", query strings, and control characters so a
// name can never escape the icons/ directory or inject into the Iconify URL or
// cache path.
func validIconName(s string) bool {
	if s == "" || len(s) > 64 {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9',
			r == '_', r == '-', r == '.':
		default:
			return false
		}
	}
	return true
}

// ResolveIcon resolves an <Icon name="..."> to sanitized SVG content.
// A name without a ":" is a project-local icon resolved from
// <root>/icons/<name>.svg - the filename registers the icon name. A name with
// a ":" is a `set:name` pair fetched from the Iconify API and cached under
// <root>/.krate/cache/icons/.
func ResolveIcon(root string, fullName string) (Icon, error) {
	if !strings.Contains(fullName, ":") {
		return resolveLocalIcon(root, fullName)
	}
	return resolveRemoteIcon(root, fullName)
}

// readCachedIcon reads and parses a cached icon file. A cache file that cannot
// be parsed (stale format, corruption) is removed so the caller re-fetches it.
func readCachedIcon(cachePath string) (Icon, bool) {
	data, err := os.ReadFile(cachePath)
	if err != nil {
		return Icon{}, false
	}
	ic, perr := parseSVG(string(data))
	if perr != nil {
		// Stale/corrupt cache - drop it and re-fetch on the slow path.
		_ = os.Remove(cachePath)
		return Icon{}, false
	}
	return ic, true
}

// resolveLocalIcon reads <root>/icons/<name>.svg, extracting the inner markup
// and preserving the file's own viewBox geometry.
func resolveLocalIcon(root string, name string) (Icon, error) {
	if !validIconName(name) {
		return Icon{}, fmt.Errorf("invalid local icon name: %q", name)
	}
	localPath := filepath.Join(root, "icons", name+".svg")
	data, err := os.ReadFile(localPath)
	if err != nil {
		return Icon{}, fmt.Errorf("local icon %q not found in icons/ (looked for %s)", name, localPath)
	}
	ic, err := parseSVG(string(data))
	if err != nil {
		return Icon{}, fmt.Errorf("local icon %q: %w", name, err)
	}
	return ic, nil
}

// resolveRemoteIcon fetches `set:name` from the Iconify API, caching the raw
// response under <root>/.krate/cache/icons/<set>/<name>.svg.
func resolveRemoteIcon(root string, fullName string) (Icon, error) {
	parts := strings.SplitN(fullName, ":", 2)
	if len(parts) != 2 {
		return Icon{}, fmt.Errorf("invalid icon name format: %s (expected set:name or a local icon name)", fullName)
	}
	prefix, name := parts[0], parts[1]
	if !validIconName(prefix) || !validIconName(name) {
		return Icon{}, fmt.Errorf("invalid icon name: %q", fullName)
	}

	cacheDir := filepath.Join(root, ".krate", "cache", "icons", prefix)
	cachePath := filepath.Join(cacheDir, name+".svg")

	// 1. Fast Path: check if it's already cached on disk.
	if ic, ok := readCachedIcon(cachePath); ok {
		return ic, nil
	}

	// 2. Slow Path: fetch from Iconify API on demand (Thread-safe).
	fetchMutex.Lock()
	defer fetchMutex.Unlock()

	// Double check if another thread downloaded it while we were waiting for
	// the lock.
	if ic, ok := readCachedIcon(cachePath); ok {
		return ic, nil
	}

	bodyBytes, err := fetchIconSVG(prefix, name)
	if err != nil {
		return Icon{}, err
	}

	ic, err := parseSVG(string(bodyBytes))
	if err != nil {
		return Icon{}, fmt.Errorf("malformed SVG received for %s: %w", fullName, err)
	}

	// Ensure the cache directory exists and cache the raw response.
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return Icon{}, err
	}
	_ = os.WriteFile(cachePath, bodyBytes, 0644)

	return ic, nil
}

// iconFetchAttempts bounds retries of transient API failures (network errors,
// 429 and 5xx); builds fetch many icons in parallel and a single dropped
// request should not fail the build.
const iconFetchAttempts = 3

// fetchIconSVG downloads one icon from the Iconify API, retrying transient
// failures with a short backoff. A 404 is final.
func fetchIconSVG(prefix, name string) ([]byte, error) {
	url := fmt.Sprintf("https://api.iconify.design/%s/%s.svg", prefix, name)
	var lastErr error
	for attempt := 0; attempt < iconFetchAttempts; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Duration(attempt) * 500 * time.Millisecond)
		}
		resp, err := httpClient.Get(url)
		if err != nil {
			lastErr = fmt.Errorf("failed to reach icon API: %w", err)
			continue
		}
		body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		resp.Body.Close()
		switch {
		case resp.StatusCode == http.StatusNotFound:
			return nil, fmt.Errorf("icon %s:%s not found in registry", prefix, name)
		case resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500:
			lastErr = fmt.Errorf("api returned status %d", resp.StatusCode)
			continue
		case resp.StatusCode != http.StatusOK:
			return nil, fmt.Errorf("api returned status %d", resp.StatusCode)
		case err != nil:
			lastErr = err
			continue
		case len(body) == 1<<20:
			return nil, fmt.Errorf("icon %s:%s response exceeds 1MB", prefix, name)
		}
		return body, nil
	}
	return nil, lastErr
}

// parseSVG extracts the inner markup and viewBox from a full SVG document and
// sanitizes the inner markup for safe insertion into page HTML.
// The disk cache may contain either a full `<svg>...</svg>` document (the raw
// Iconify API response) or bare inner markup (written by older cache writers).
// Bare inner markup - content with no <svg> root - is treated as already-inner
// so stale caches keep working.
func parseSVG(data string) (Icon, error) {
	matches := svgInnerRegex.FindStringSubmatch(data)
	if len(matches) < 2 {
		// No <svg> root: if the content looks like markup, treat it as the
		// inner markup itself (stale-cache format). Non-markup content is
		// still an error.
		if !looksLikeSVGMarkup(data) {
			return Icon{}, fmt.Errorf("malformed SVG structure (no <svg> element found)")
		}
		return Icon{Inner: sanitizeSVG(strings.TrimSpace(data))}, nil
	}
	ic := Icon{Inner: sanitizeSVG(strings.TrimSpace(matches[1]))}
	if vm := viewBoxRegex.FindStringSubmatch(data); len(vm) > 1 {
		ic.ViewBox = vm[1]
	}
	return ic, nil
}

// looksLikeSVGMarkup reports whether data contains SVG-ish element tags
// (<path, <g, <circle, ...) so parseSVG can distinguish a bare-inner cache file
// from truly malformed content.
func looksLikeSVGMarkup(data string) bool {
	for _, probe := range []string{"<path", "<g ", "<circle", "<rect", "<line", "<polyline", "<polygon", "<ellipse", "<svg"} {
		if strings.Contains(strings.ToLower(data), probe) {
			return true
		}
	}
	return false
}

// sanitizeSVG removes executable/embeddable content from SVG inner markup:
// <script> and <foreignObject> blocks (which can embed HTML), XML comments,
// event handler attributes, and javascript:/data: URLs (including
// entity-encoded forms). A small state machine walks the markup because
// regexes cannot safely match tag contents across elements.
func sanitizeSVG(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		if s[i] != '<' {
			b.WriteByte(s[i])
			i++
			continue
		}
		// XML/HTML comment - drop it (and anything up to its terminator).
		if i+4 <= len(s) && strings.HasPrefix(s[i:], "<!--") {
			end := strings.Index(s[i+4:], "-->")
			if end < 0 {
				return b.String() // unterminated comment - drop the tail
			}
			i += 4 + end + 3
			continue
		}
		// Start tags are tokenized like the HTML parser so the sanitizer and
		// the browser agree on where a tag and its attributes end.
		if i+1 < len(s) && isSVGAlpha(s[i+1]) {
			st, ok := parseStartTag(s, i)
			if !ok {
				return b.String() // unterminated tag - drop the tail
			}
			name := strings.ToLower(st.name)
			if name == "script" || name == "foreignobject" {
				// Drop the whole block. Non-self-closing blocks consume through
				// their matching close tag.
				if !st.selfClosing {
					closeTag := "</" + name + ">"
					rest := strings.ToLower(s[st.end:])
					ci := strings.Index(rest, closeTag)
					if ci < 0 {
						return b.String() // unterminated block - drop the tail
					}
					i = st.end + ci + len(closeTag)
					continue
				}
				i = st.end
				continue
			}
			b.WriteString(st.sanitized())
			i = st.end
			continue
		}
		if i+1 >= len(s) || !(s[i+1] == '/' || s[i+1] == '!' || s[i+1] == '?') {
			b.WriteString("&lt;") // a lone '<' is text, never markup
			i++
			continue
		}
		// End tags, declarations and processing instructions carry no
		// attributes; copy them through the first unquoted '>'.
		j := i + 1
		var quote byte
		for j < len(s) {
			ch := s[j]
			if quote != 0 {
				if ch == quote {
					quote = 0
				}
			} else if ch == '"' || ch == '\'' {
				quote = ch
			} else if ch == '>' {
				break
			}
			j++
		}
		if j >= len(s) {
			return b.String() // unterminated tag - drop the tail
		}
		b.WriteString(s[i : j+1])
		i = j + 1
	}
	return b.String()
}

func isSVGAlpha(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

// isSVGSpace reports whether b is HTML whitespace.
func isSVGSpace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\r' || b == '\f'
}

type svgAttr struct {
	name   string
	value  string
	hasVal bool
	quote  byte // 0 for an unquoted source value
}

type startTag struct {
	name        string
	attrs       []svgAttr
	selfClosing bool
	end         int // index just past the closing '>'
}

// parseStartTag tokenizes the start tag at s[i:] (s[i] == '<' followed by an
// ASCII letter) following the HTML tokenizer rules: attribute values end at
// the matching quote, or for unquoted values at whitespace or '>', so a
// trailing '/' is only a self-closing marker when it is not part of a value.
func parseStartTag(s string, i int) (startTag, bool) {
	var st startTag
	j := i + 1
	for j < len(s) && !isSVGSpace(s[j]) && s[j] != '/' && s[j] != '>' {
		j++
	}
	st.name = s[i+1 : j]
	for {
		slash := false
		for j < len(s) && (isSVGSpace(s[j]) || s[j] == '/') {
			slash = s[j] == '/'
			j++
		}
		if j >= len(s) {
			return st, false
		}
		if s[j] == '>' {
			st.selfClosing = slash
			st.end = j + 1
			return st, true
		}
		ns := j
		if s[j] == '=' {
			j++
		}
		for j < len(s) && !isSVGSpace(s[j]) && s[j] != '/' && s[j] != '>' && s[j] != '=' {
			j++
		}
		at := svgAttr{name: s[ns:j]}
		k := j
		for k < len(s) && isSVGSpace(s[k]) {
			k++
		}
		if k < len(s) && s[k] == '=' {
			k++
			for k < len(s) && isSVGSpace(s[k]) {
				k++
			}
			if k >= len(s) {
				return st, false
			}
			at.hasVal = true
			switch q := s[k]; q {
			case '"', '\'':
				e := strings.IndexByte(s[k+1:], q)
				if e < 0 {
					return st, false
				}
				at.quote = q
				at.value = s[k+1 : k+1+e]
				j = k + 2 + e
			case '>':
				j = k
			default:
				vs := k
				for k < len(s) && !isSVGSpace(s[k]) && s[k] != '>' {
					k++
				}
				at.value = s[vs:k]
				j = k
			}
		}
		st.attrs = append(st.attrs, at)
	}
}

// sanitized rebuilds the tag, dropping event handler attributes (on*) and
// href/src/action attributes whose value resolves to an executable or data:
// URL. Attribute values are always quoted when possible and a self-closing
// slash is emitted as a separate " />" so it can never fuse with a value.
func (st startTag) sanitized() string {
	var out strings.Builder
	out.WriteByte('<')
	out.WriteString(st.name)
	for _, a := range st.attrs {
		name := strings.ToLower(a.name)
		switch {
		case strings.HasPrefix(name, "on"):
			continue // event handler
		case (name == "href" || name == "xlink:href" || name == "src" || name == "action") && dangerousURL(a.value):
			continue
		}
		out.WriteByte(' ')
		out.WriteString(a.name)
		if !a.hasVal {
			continue
		}
		q := a.quote
		if q == 0 {
			switch {
			case !strings.Contains(a.value, `"`):
				q = '"'
			case !strings.Contains(a.value, "'"):
				q = '\''
			}
		}
		out.WriteByte('=')
		if q == 0 {
			out.WriteString(a.value)
			continue
		}
		out.WriteByte(q)
		out.WriteString(a.value)
		out.WriteByte(q)
	}
	if st.selfClosing {
		out.WriteString(" />")
	} else {
		out.WriteByte('>')
	}
	return out.String()
}

// sanitizeTag rebuilds a single SVG tag with unsafe attributes removed.
// Tags that are not start tags are returned unchanged.
func sanitizeTag(tag string) string {
	if len(tag) < 2 || tag[0] != '<' || !isSVGAlpha(tag[1]) {
		return tag
	}
	st, ok := parseStartTag(tag, 0)
	if !ok {
		return tag
	}
	return st.sanitized()
}

// dangerousURLValue reports whether an `href="..."`-style attribute carries an
// executable/data: URL.
func dangerousURLValue(attr string) bool {
	eq := strings.IndexByte(attr, '=')
	if eq < 0 {
		return false
	}
	val := strings.TrimSpace(attr[eq+1:])
	if len(val) >= 2 {
		val = val[1 : len(val)-1]
	}
	return dangerousURL(val)
}

// dangerousURL reports whether a raw attribute value is a javascript: or data:
// URL. The value is HTML-unescaped (browsers decode entities in attribute
// values before resolving the URL) and scheme-checked.
func dangerousURL(val string) bool {
	val = html.UnescapeString(val)
	val = strings.ToLower(strings.TrimSpace(val))
	return strings.HasPrefix(val, "javascript:") || strings.HasPrefix(val, "data:")
}

// GetIconContent is the legacy wrapper returning only the sanitized inner SVG
// markup. Prefer ResolveIcon when the caller needs the source viewBox.
func GetIconContent(rootDirs string, fullName string) (string, error) {
	ic, err := ResolveIcon(rootDirs, fullName)
	return ic.Inner, err
}
