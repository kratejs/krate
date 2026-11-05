package build

import (
	"path"
	"regexp"
	"strings"
)

// CSS web-font helpers. `@font-face` is handled as opaque text elsewhere, so
// these operate on the final stylesheet string: injecting a default
// `font-display` and collecting `src` URLs for `<link rel="preload">`.

var (
	fontFaceRe = regexp.MustCompile(`(?is)@font-face\s*\{([^}]*)\}`)
	fontURLRe  = regexp.MustCompile(`(?i)url\(\s*['"]?([^'")]+)['"]?\s*\)`)
)

// injectFontDisplay inserts `font-display:<display>` into every `@font-face`
// block that does not already declare one. A blank display is a no-op.
func injectFontDisplay(cssText, display string) string {
	if display == "" || !strings.Contains(cssText, "@font-face") {
		return cssText
	}
	return fontFaceRe.ReplaceAllStringFunc(cssText, func(block string) string {
		if strings.Contains(strings.ToLower(block), "font-display") {
			return block
		}
		idx := strings.LastIndexByte(block, '}')
		if idx < 0 {
			return block
		}
		sep := ""
		if lastDeclUnterminated(block[:idx]) {
			sep = ";"
		}
		return block[:idx] + sep + "font-display:" + display + ";" + block[idx:]
	})
}

// lastDeclUnterminated reports whether a rule body ends in a declaration with
// no closing `;`, ignoring trailing whitespace and comments. Appending another
// declaration to such a body needs a `;` first.
func lastDeclUnterminated(body string) bool {
	t := strings.TrimRight(body, " \t\r\n")
	for strings.HasSuffix(t, "*/") {
		k := strings.LastIndex(t, "/*")
		if k < 0 {
			break
		}
		t = strings.TrimRight(t[:k], " \t\r\n")
	}
	return t != "" && !strings.HasSuffix(t, ";") && !strings.HasSuffix(t, "{")
}

var (
	fontSrcFormatRe = regexp.MustCompile(`(?i)format\(\s*['"]?([a-z0-9-]+)['"]?\s*\)`)
	noPreloadRe     = regexp.MustCompile(`(?i)/\*\s*(?:krate:\s*)?no-?preload\s*\*/`)
	unicodeRangeRe  = regexp.MustCompile(`(?i)unicode-range\s*:([^;}]*)`)
	defaultFontCap  = 4
)

// collectFontURLs returns the unique, non-data `url()` references inside
// `@font-face` blocks, in first-seen order.
func collectFontURLs(cssText string) []string {
	var urls []string
	seen := make(map[string]bool)
	for _, m := range fontFaceRe.FindAllStringSubmatch(cssText, -1) {
		for _, u := range fontURLRe.FindAllStringSubmatch(m[1], -1) {
			url := strings.TrimSpace(u[1])
			if url == "" || seen[url] || strings.HasPrefix(url, "data:") {
				continue
			}
			seen[url] = true
			urls = append(urls, url)
		}
	}
	return urls
}

// collectFontPreloads picks the fonts worth a `<link rel="preload">`: for each
// `@font-face` the first woff2 source (the file a modern browser will fetch;
// woff/ttf fallbacks are never preloaded unless they are the only source),
// skipping faces marked `/* no-preload */`, faces for non-Latin unicode-range
// subsets, and anything past limit. Relative URLs resolve against the
// stylesheet, which is served from the site root.
func collectFontPreloads(cssText string, limit int) []string {
	if limit <= 0 {
		limit = defaultFontCap
	}
	var out []string
	seen := map[string]bool{}
	for _, m := range fontFaceRe.FindAllStringSubmatch(cssText, -1) {
		block := m[1]
		if noPreloadRe.MatchString(block) {
			continue
		}
		if ur := unicodeRangeRe.FindStringSubmatch(block); ur != nil && !coversLatin(ur[1]) {
			continue
		}
		pick := ""
		first := ""
		for _, u := range fontURLRe.FindAllStringSubmatchIndex(block, -1) {
			url := strings.TrimSpace(block[u[2]:u[3]])
			if url == "" || strings.HasPrefix(url, "data:") {
				continue
			}
			if first == "" {
				first = url
			}
			// A format() hint wins over the extension.
			tail := block[u[1]:]
			if end := strings.IndexAny(tail, ",;"); end >= 0 {
				tail = tail[:end]
			}
			isWoff2 := strings.EqualFold(strings.ToLower(filepathExt(stripQuery(url))), ".woff2")
			if f := fontSrcFormatRe.FindStringSubmatch(tail); f != nil {
				isWoff2 = strings.EqualFold(f[1], "woff2")
			}
			if isWoff2 {
				pick = url
				break
			}
		}
		if pick == "" {
			pick = first
		}
		if pick == "" {
			continue
		}
		pick = resolveFontURL(pick)
		if seen[pick] {
			continue
		}
		seen[pick] = true
		out = append(out, pick)
		if len(out) >= limit {
			break
		}
	}
	return out
}

func stripQuery(u string) string {
	if i := strings.IndexAny(u, "?#"); i >= 0 {
		return u[:i]
	}
	return u
}

// coversLatin reports whether a unicode-range descriptor includes basic Latin.
func coversLatin(r string) bool {
	r = strings.ToUpper(r)
	return strings.Contains(r, "U+0000-00FF") || strings.Contains(r, "U+0000-0") || strings.Contains(r, "U+0020")
}

// resolveFontURL makes a font URL independent of the page it is linked from.
// Stylesheets are written at the site root, so a relative url() resolves there.
func resolveFontURL(u string) string {
	if strings.HasPrefix(u, "/") || strings.Contains(u, "://") {
		return u
	}
	q := ""
	if i := strings.IndexAny(u, "?#"); i >= 0 {
		u, q = u[:i], u[i:]
	}
	return path.Clean("/"+u) + q
}

// fontPreloadHTML builds the preload link tags for the given font URLs.
func fontPreloadHTML(urls []string, basePath string) string {
	if len(urls) == 0 {
		return ""
	}
	var b strings.Builder
	for _, u := range urls {
		href := u
		if basePath != "" && strings.HasPrefix(href, "/") && !strings.HasPrefix(href, basePath+"/") {
			href = basePath + href
		}
		typ := "font/woff2"
		switch strings.ToLower(filepathExt(stripQuery(u))) {
		case ".woff":
			typ = "font/woff"
		case ".ttf":
			typ = "font/ttf"
		case ".otf":
			typ = "font/otf"
		}
		b.WriteString(`<link rel="preload" as="font" type="` + typ + `" href="` + href + `" crossorigin>` + "\n")
	}
	return b.String()
}
