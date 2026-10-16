package build

import (
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
		return block[:idx] + "font-display:" + display + ";" + block[idx:]
	})
}

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
		switch strings.ToLower(filepathExt(u)) {
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
