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
//
// A name without a ":" is a project-local icon resolved from
// <root>/icons/<name>.svg — the filename registers the icon name. A name with
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
		// Stale/corrupt cache — drop it and re-fetch on the slow path.
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

	url := fmt.Sprintf("https://api.iconify.design/%s/%s.svg", prefix, name)
	resp, err := httpClient.Get(url)
	if err != nil {
		return Icon{}, fmt.Errorf("failed to reach icon API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return Icon{}, fmt.Errorf("icon %s:%s not found in registry", prefix, name)
	} else if resp.StatusCode != http.StatusOK {
		return Icon{}, fmt.Errorf("api returned status %d", resp.StatusCode)
	}

	bodyBytes, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return Icon{}, err
	}
	if len(bodyBytes) == 1<<20 {
		return Icon{}, fmt.Errorf("icon %s:%s response exceeds 1MB", prefix, name)
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

// parseSVG extracts the inner markup and viewBox from a full SVG document and
// sanitizes the inner markup for safe insertion into page HTML.
//
// The disk cache may contain either a full `<svg>...</svg>` document (the raw
// Iconify API response) or bare inner markup (written by older cache writers).
// Bare inner markup — content with no <svg> root — is treated as already-inner
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
		// XML/HTML comment — drop it (and anything up to its terminator).
		if i+4 <= len(s) && strings.HasPrefix(s[i:], "<!--") {
			end := strings.Index(s[i+4:], "-->")
			if end < 0 {
				return b.String() // unterminated comment — drop the tail
			}
			i += 4 + end + 3
			continue
		}
		// Read a single tag, honoring quoted attribute values.
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
			return b.String() // unterminated tag — drop the tail
		}
		tag := s[i : j+1]
		name := svgTagName(tag)
		selfClosing := strings.HasSuffix(strings.TrimSpace(tag[:len(tag)-1]), "/")
		if name == "script" || name == "foreignobject" {
			// Drop the whole block. Non-self-closing blocks consume through
			// their matching close tag.
			if !selfClosing {
				close := "</" + name + ">"
				rest := strings.ToLower(s[j+1:])
				ci := strings.Index(rest, close)
				if ci < 0 {
					return b.String() // unterminated block — drop the tail
				}
				i = j + 1 + ci + len(close)
				continue
			}
			i = j + 1
			continue
		}
		b.WriteString(sanitizeTag(tag))
		i = j + 1
	}
	return b.String()
}

// svgTagName returns the lowercased element name of a single tag.
func svgTagName(tag string) string {
	t := strings.TrimLeft(tag[1:], "/!?")
	end := 0
	for end < len(t) && !isSVGSpace(t[end]) && t[end] != '>' && t[end] != '/' {
		end++
	}
	return strings.ToLower(t[:end])
}

// isSVGSpace reports whether b is HTML whitespace.
func isSVGSpace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\r'
}

// sanitizeTag rebuilds a single SVG start tag, dropping event handler
// attributes (on*) and href/src/action attributes whose value resolves to an
// executable or data: URL. The tag name and all other attributes are kept.
func sanitizeTag(tag string) string {
	if len(tag) < 2 || tag[0] != '<' {
		return tag
	}
	// Closing/declaration tags carry no attributes to sanitize.
	if tag[1] == '/' || tag[1] == '!' || tag[1] == '?' {
		return tag
	}
	p := 1
	for p < len(tag) && !isSVGSpace(tag[p]) && tag[p] != '>' && tag[p] != '/' {
		p++
	}
	name := tag[:p]

	var kept []string
	for p < len(tag)-1 {
		for p < len(tag)-1 && isSVGSpace(tag[p]) {
			p++
		}
		if p >= len(tag)-1 {
			break
		}
		attrStart := p
		var quote byte
		for p < len(tag)-1 {
			ch := tag[p]
			if quote != 0 {
				if ch == quote {
					quote = 0
				}
			} else if ch == '"' || ch == '\'' {
				quote = ch
			} else if isSVGSpace(ch) || ch == '>' {
				break
			}
			p++
		}
		attr := tag[attrStart:p]
		attrName := strings.ToLower(strings.TrimSpace(strings.SplitN(attr, "=", 2)[0]))
		switch {
		case strings.HasPrefix(attrName, "on"):
			continue // event handler
		case (attrName == "href" || attrName == "xlink:href" || attrName == "src" || attrName == "action") && dangerousURLValue(attr):
			continue
		}
		kept = append(kept, attr)
	}

	var out strings.Builder
	out.WriteString(name)
	for _, a := range kept {
		out.WriteByte(' ')
		out.WriteString(a)
	}
	out.WriteByte('>')
	return out.String()
}

// dangerousURLValue reports whether an `href="..."`-style attribute carries an
// executable/data: URL. The value is HTML-unescaped (browsers decode entities
// in attribute values before resolving the URL) and scheme-checked.
func dangerousURLValue(attr string) bool {
	eq := strings.IndexByte(attr, '=')
	if eq < 0 {
		return false
	}
	val := strings.TrimSpace(attr[eq+1:])
	if len(val) >= 2 {
		val = val[1 : len(val)-1]
	}
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
