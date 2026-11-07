package build

import (
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"regexp"
	"strings"

	"golang.org/x/net/html"

	"github.com/kratejs/krate/packages/compiler/internal/config"
	"github.com/kratejs/krate/packages/compiler/internal/escape"
	"github.com/kratejs/krate/packages/compiler/internal/plugin"
)

// dynChunkURLRe matches the chunk URLs a page's hydration JS dynamically imports
// (the bundler rewrites `import("./X.tsx")` to `import("/chunks/X-<hash>.js")`).
var dynChunkURLRe = regexp.MustCompile(`["'](/chunks/[^"']+\.js)["']`)

// dynamicChunkURLs returns the unique dynamic-import chunk URLs referenced by a
// page's hydration JS, so their module fetches can start during HTML parse.
func dynamicChunkURLs(hydrationJS string) []string {
	if !strings.Contains(hydrationJS, "/chunks/") {
		return nil
	}
	matches := dynChunkURLRe.FindAllStringSubmatch(hydrationJS, -1)
	if len(matches) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(matches))
	var out []string
	for _, m := range matches {
		url := m[1]
		if seen[url] {
			continue
		}
		seen[url] = true
		out = append(out, url)
	}
	return out
}

var viewportMetaRe = regexp.MustCompile(`(?i)<meta[^>]+name\s*=\s*["']?viewport["'\s>/]`)

// devEarlyErrorsScript keeps the errors thrown before the dev overlay loads,
// which is after the page script: a page's first render would otherwise fail
// with nothing listening. The overlay takes the buffer and clears it.
const devEarlyErrorsScript = `<script>(function(){var q=window.__KRATE_EARLY_ERRORS__=[];` +
	`function k(e){if(window.__KRATE_EARLY_ERRORS__)q.push(e)}` +
	`addEventListener("error",k);addEventListener("unhandledrejection",k)})()</script>` + "\n"

func generateHTML(bodyHTML, headHTML, scriptHTML, styleHTML string, cssFiles []string, jsFile, runtimeJSFile, route string, devMode bool, devBootstrap, runtimeHead, basePath string, modulePreloads ...string) string {
	var b strings.Builder

	b.WriteString("<!DOCTYPE html>\n<html lang=\"en\">\n<head>\n")
	b.WriteString("<meta charset=\"UTF-8\">\n")
	if devMode {
		b.WriteString(devEarlyErrorsScript)
	}
	// The page's own viewport meta (a <Head> tag or a viewport export) replaces the default.
	if !viewportMetaRe.MatchString(headHTML) {
		b.WriteString("<meta name=\"viewport\" content=\"width=device-width, initial-scale=1.0, viewport-fit=cover\">\n")
	}

	// Site runtime config + View Transitions CSS (base path, SPA behavior).
	if runtimeHead != "" {
		b.WriteString(runtimeHead)
		if !strings.HasSuffix(runtimeHead, "\n") {
			b.WriteByte('\n')
		}
	}
	if headHTML != "" {
		b.WriteString(headHTML)
		b.WriteByte('\n')
	} else {
		b.WriteString("<title>Krate App</title>\n")
	}

	if styleHTML != "" {
		b.WriteString(styleHTML)
		b.WriteByte('\n')
	}

	for _, cssFile := range cssFiles {
		b.WriteString(fmt.Sprintf("<link rel=\"stylesheet\" href=\"%s\">\n", assetHref(basePath, cssFile)))
	}

	// Start hydration fetches during HTML parse instead of after the body
	// scripts are reached. The shared runtime chunk and the page hydration
	// script are classic scripts (emitted without type="module"), so
	// `rel="preload" as="script"` is the correct hint for them. Skipped in dev,
	// where the overlay/toolbar has its own loading path.
	if !devMode {
		if runtimeJSFile != "" {
			b.WriteString(fmt.Sprintf("<link rel=\"preload\" as=\"script\" href=\"%s\">\n", assetHref(basePath, runtimeJSFile)))
		}
		if jsFile != "" {
			b.WriteString(fmt.Sprintf("<link rel=\"preload\" as=\"script\" href=\"%s\">\n", pageScriptSrc(route, jsFile, basePath)))
		}
		// Dynamic-import chunks are ES modules, so modulepreload is the correct
		// hint (and the router's route prefetch picks them up too).
		for _, chunk := range modulePreloads {
			b.WriteString(fmt.Sprintf("<link rel=\"modulepreload\" href=\"%s\">\n", assetHref(basePath, chunk)))
		}
	}

	b.WriteString("</head>\n<body>\n")
	b.WriteString("<div id=\"root\">")
	b.WriteString(bodyHTML)
	b.WriteString("</div>\n")

	if scriptHTML != "" {
		b.WriteString(scriptHTML)
		b.WriteByte('\n')
	}

	// Shared runtime chunk (loaded before page-specific hydration)
	if runtimeJSFile != "" {
		b.WriteString(fmt.Sprintf("<script src=\"%s\"></script>\n", assetHref(basePath, runtimeJSFile)))
	}

	// Page hydration script. The src must be an absolute site path (not
	// relative to the page) so it resolves correctly both on first load and
	// when the SPA router injects it during client-side navigation.
	if jsFile != "" {
		b.WriteString(fmt.Sprintf("<script src=\"%s\"></script>\n", pageScriptSrc(route, jsFile, basePath)))
	}

	if devMode {
		// The dev overlay + toolbar live in a separate, dev-only bundle. The
		// inline bootstrap carries only the config the bundle needs so it can
		// start before the bundle finishes loading.
		if devBootstrap == "" {
			devBootstrap = `{"sse":"/__krate/hotreload"}`
		}
		b.WriteString("<script>window.__KRATE_DEV__=")
		b.WriteString(devBootstrap)
		b.WriteString(";</script>\n")
		b.WriteString(fmt.Sprintf("<script src=\"%s\"></script>\n", assetHref(basePath, "chunks/krate-dev.js")))
	}

	b.WriteString("</body>\n</html>\n")
	return b.String()
}

// assetHref prefixes a root-relative asset path with the site base path.
func assetHref(basePath, asset string) string {
	asset = strings.TrimPrefix(asset, "/")
	if basePath == "" {
		return "/" + asset
	}
	return basePath + "/" + asset
}

// generateHTMLWithLoading is like generateHTML but includes a loading template for SPA transitions.
func generateHTMLWithLoading(bodyHTML, headHTML, scriptHTML, styleHTML, loadingHTML string, cssFiles []string, jsFile, runtimeJSFile, route string, devMode bool, devBootstrap, runtimeHead, basePath string, modulePreloads ...string) string {
	// The template is the last child of #root (the router overlays it while a
	// navigation is in flight).
	if loadingHTML != "" {
		bodyHTML += "<template data-krate-loading>" + loadingHTML + "</template>"
	}
	return generateHTML(bodyHTML, headHTML, scriptHTML, styleHTML, cssFiles, jsFile, runtimeJSFile, route, devMode, devBootstrap, runtimeHead, basePath, modulePreloads...)
}

// pageScriptSrc returns the absolute site URL for a page's hydration script.
// jsFile is the bare filename (e.g. "index.a1b2c3.js"); route is the page's
// output name ("." for the root page, "about" for /about, "docs/guide" for
// /docs/guide). The result is root-absolute (prefixed with basePath) so it works
// for both first-page loads and SPA navigation from any other route, and so
// relative dynamic imports resolve under the base path.
func pageScriptSrc(route, jsFile, basePath string) string {
	if jsFile == "" {
		return ""
	}
	route = strings.Trim(route, "/")
	prefix := basePath
	if route != "" && route != "." {
		prefix += "/" + route
	}
	return assetHref("", prefix+"/"+jsFile)
}

// generateCSPMeta generates a Content-Security-Policy <meta> tag by hashing
// inline scripts and styles with SHA-256.
func generateCSPMeta(scriptHTML, styleHTML, hydrationJS, directive string) string {
	if directive != "" {
		return fmt.Sprintf("<meta http-equiv=\"Content-Security-Policy\" content=\"%s\">\n", directive)
	}

	var scriptHashes []string
	var styleHashes []string

	// Hash inline scripts from scriptHTML (extract content between <script>...</script>)
	for _, inline := range extractInlineContent(scriptHTML, "script") {
		scriptHashes = append(scriptHashes, sha256Base64(inline))
	}

	// Hash hydration JS
	if hydrationJS != "" {
		scriptHashes = append(scriptHashes, sha256Base64(hydrationJS))
	}

	// Hash inline styles from styleHTML (extract content between <style>...</style>)
	for _, inline := range extractInlineContent(styleHTML, "style") {
		styleHashes = append(styleHashes, sha256Base64(inline))
	}

	// style="..." attributes need 'unsafe-hashes' plus a hash of each value.
	attrStyles, specRules := inlineStyleAttrsAndSpecRules(styleHTML, scriptHTML)

	// A base 'self' policy is meaningful even with no inline assets, so CSP is
	// always enforced once enabled. Inline assets, when present, are allowlisted
	// individually via their SHA-256 hashes.
	var directives []string
	directives = append(directives, "default-src 'self'")

	scriptSrc := "script-src 'self'"
	for _, h := range scriptHashes {
		scriptSrc += " " + h
	}
	if specRules {
		scriptSrc += " 'inline-speculation-rules'"
	}
	directives = append(directives, scriptSrc)

	styleSrc := "style-src 'self'"
	for _, h := range styleHashes {
		styleSrc += " " + h
	}
	if len(attrStyles) > 0 {
		styleSrc += " 'unsafe-hashes'"
		for _, h := range attrStyles {
			styleSrc += " " + h
		}
	}
	directives = append(directives, styleSrc)

	return fmt.Sprintf("<meta http-equiv=\"Content-Security-Policy\" content=\"%s\">\n", strings.Join(directives, "; "))
}

// inlineStyleAttrsAndSpecRules returns the deduplicated hashes of every
// non-empty style="..." attribute value in styleDoc (decoded the way a browser
// hashes them) and whether scriptDoc has an inline speculation-rules script.
func inlineStyleAttrsAndSpecRules(styleDoc, scriptDoc string) ([]string, bool) {
	var hashes []string
	seen := map[string]bool{}
	z := html.NewTokenizer(strings.NewReader(styleDoc))
	for {
		tt := z.Next()
		if tt == html.ErrorToken {
			break
		}
		if tt != html.StartTagToken && tt != html.SelfClosingTagToken {
			continue
		}
		_, hasAttr := z.TagName()
		for hasAttr {
			var k, v []byte
			k, v, hasAttr = z.TagAttr()
			if string(k) != "style" || strings.TrimSpace(string(v)) == "" {
				continue
			}
			h := sha256Base64(string(v))
			if !seen[h] {
				seen[h] = true
				hashes = append(hashes, h)
			}
		}
	}
	spec := strings.Contains(strings.ToLower(scriptDoc), `type="speculationrules"`) ||
		strings.Contains(strings.ToLower(scriptDoc), `type=speculationrules`)
	return hashes, spec
}

// extractInlineContent finds content between <tag>...</tag> tags (inline, not src).
// Tags that reference external assets (src= or href= attributes) are skipped so
// their empty content is not hashed into a meaningless CSP directive.
func extractInlineContent(html, tag string) []string {
	var results []string
	lower := strings.ToLower(html)
	searchOpen := "<" + tag
	searchClose := "</" + tag + ">"
	pos := 0

	for {
		idx := strings.Index(lower[pos:], searchOpen)
		if idx < 0 {
			break
		}
		start := pos + idx
		// Find the end of the opening tag (the >)
		tagEnd := strings.Index(html[start:], ">")
		if tagEnd < 0 {
			break
		}
		openTag := html[start : start+tagEnd]
		contentStart := start + tagEnd + 1
		closeIdx := strings.Index(lower[contentStart:], searchClose)
		if closeIdx < 0 {
			break
		}
		content := html[contentStart : contentStart+closeIdx]
		// Skip external references: the tag points at a src/href, so its body is
		// not inline content to hash.
		if !referencesExternalAsset(openTag) {
			if content != "" {
				results = append(results, content)
			}
		}
		pos = contentStart + closeIdx + len(searchClose)
	}

	return results
}

// referencesExternalAsset reports whether a tag's opening markup points at an
// external resource via a src or href attribute.
func referencesExternalAsset(openTag string) bool {
	lower := strings.ToLower(openTag)
	return strings.Contains(lower, "src=") || strings.Contains(lower, "href=")
}

func sha256Base64(content string) string {
	h := sha256.Sum256([]byte(strings.TrimSpace(content)))
	return "'sha256-" + base64.StdEncoding.EncodeToString(h[:]) + "'"
}

// generateSEOTags produces canonical, OpenGraph, and Twitter Card meta tags.
// It skips tags that are already present in headHTML to avoid duplication.
func generateSEOTags(headHTML, route string, seo config.SEOConfig, ctx ...seoCtx) string {
	// The configured default description is a plain <meta name="description">
	// too (search engines and `krate check` read that tag), independent of
	// whether a base URL is set for the canonical/OpenGraph tags below.
	descTag := ""
	if seo.Description != "" && extractMetaAttr(headHTML, "description") == "" {
		descTag = fmt.Sprintf("<meta name=\"description\" content=\"%s\">\n", escape.HTMLAttr(seo.Description))
	}
	baseURL := strings.TrimRight(seo.BaseURL, "/")
	if baseURL == "" {
		return descTag
	}
	var sc seoCtx
	if len(ctx) > 0 {
		sc = ctx[0]
	}
	siteName := seo.SiteName
	defaultDescription := seo.Description
	defaultImage := seo.Image

	// Canonical and og:url use the form the site is actually served at
	// (trailing slash, base path), the same as the sitemap and hreflang links.
	canonical := plugin.CanonicalURL(baseURL, sc.BasePath, route)

	var tags []string
	if descTag != "" {
		tags = append(tags, strings.TrimSuffix(descTag, "\n"))
	}

	if !sc.ErrorPage && !strings.Contains(headHTML, "rel=\"canonical\"") && !strings.Contains(headHTML, "rel='canonical'") {
		tags = append(tags, fmt.Sprintf("<link rel=\"canonical\" href=\"%s\">", escape.HTMLAttr(canonical)))
	}

	// Extract title from headHTML if present (for OG tags)
	// The head's text is already HTML-escaped; decode it so the attribute
	// escaping below does not escape it twice.
	title := html.UnescapeString(extractMetaContent(headHTML, "title"))
	if title == "" {
		title = siteName
	}
	title = applyTitleTemplateText(title, siteName, seo.TitleTemplate)

	// Extract description from headHTML <meta name="description"> if present
	desc := html.UnescapeString(extractMetaAttr(headHTML, "description"))
	if desc == "" {
		desc = defaultDescription
	}

	// OpenGraph tags
	if !strings.Contains(headHTML, "og:title") && title != "" {
		tags = append(tags, fmt.Sprintf("<meta property=\"og:title\" content=\"%s\">", escape.HTMLAttr(title)))
	}
	if !sc.ErrorPage && !strings.Contains(headHTML, "og:url") {
		tags = append(tags, fmt.Sprintf("<meta property=\"og:url\" content=\"%s\">", escape.HTMLAttr(canonical)))
	}
	if !strings.Contains(headHTML, "og:type") {
		tags = append(tags, "<meta property=\"og:type\" content=\"website\">")
	}
	if !strings.Contains(headHTML, "og:site_name") && siteName != "" {
		tags = append(tags, fmt.Sprintf("<meta property=\"og:site_name\" content=\"%s\">", escape.HTMLAttr(siteName)))
	}
	if !strings.Contains(headHTML, "og:description") && desc != "" {
		tags = append(tags, fmt.Sprintf("<meta property=\"og:description\" content=\"%s\">", escape.HTMLAttr(desc)))
	}
	if !strings.Contains(headHTML, "og:locale") && seo.Locale != "" {
		tags = append(tags, fmt.Sprintf("<meta property=\"og:locale\" content=\"%s\">", escape.HTMLAttr(ogLocale(seo.Locale))))
	}
	// Resolve the default OG image once (absolute URL when a base is set).
	ogImage := defaultImage
	if ogImage != "" && !strings.HasPrefix(ogImage, "http") {
		ogImage = plugin.CanonicalURL(baseURL, sc.BasePath, "/"+strings.TrimPrefix(ogImage, "/"))
		ogImage = strings.TrimSuffix(ogImage, "/")
	}
	if !strings.Contains(headHTML, "og:image") && ogImage != "" {
		tags = append(tags, fmt.Sprintf("<meta property=\"og:image\" content=\"%s\">", ogImage))
	}

	// Twitter Card tags. A large card when a social image is configured.
	if !strings.Contains(headHTML, "twitter:card") {
		card := "summary"
		if ogImage != "" {
			card = "summary_large_image"
		}
		tags = append(tags, fmt.Sprintf("<meta name=\"twitter:card\" content=\"%s\">", card))
	}
	if seo.Twitter != "" && !strings.Contains(headHTML, "twitter:site") {
		tags = append(tags, fmt.Sprintf("<meta name=\"twitter:site\" content=\"%s\">", escape.HTMLAttr(twitterHandle(seo.Twitter))))
	}
	if !strings.Contains(headHTML, "twitter:title") && title != "" {
		tags = append(tags, fmt.Sprintf("<meta name=\"twitter:title\" content=\"%s\">", escape.HTMLAttr(title)))
	}
	if !strings.Contains(headHTML, "twitter:description") && desc != "" {
		tags = append(tags, fmt.Sprintf("<meta name=\"twitter:description\" content=\"%s\">", escape.HTMLAttr(desc)))
	}
	if ogImage != "" && !strings.Contains(headHTML, "twitter:image") {
		tags = append(tags, fmt.Sprintf("<meta name=\"twitter:image\" content=\"%s\">", ogImage))
	}

	if !strings.Contains(headHTML, "theme-color") && seo.ThemeColor != "" {
		tags = append(tags, fmt.Sprintf("<meta name=\"theme-color\" content=\"%s\">", escape.HTMLAttr(seo.ThemeColor)))
	}

	if len(tags) == 0 {
		return ""
	}
	return strings.Join(tags, "\n") + "\n"
}

// seoCtx carries per-page context for generateSEOTags.
type seoCtx struct {
	BasePath  string
	ErrorPage bool
}

// applyTitleTemplateText applies site.titleTemplate to a bare page title.
// Titles that already carry a site/section suffix (e.g. docs pages: "Guide -
// Site Docs") or the template's own text are left alone.
func applyTitleTemplateText(title, siteName, tmpl string) string {
	if tmpl == "" || !isBareTitle(title, siteName) {
		return title
	}
	if i := strings.Index(tmpl, "%s"); i >= 0 {
		prefix, suffix := tmpl[:i], tmpl[i+2:]
		if (prefix != "" || suffix != "") && strings.HasPrefix(title, prefix) && strings.HasSuffix(title, suffix) && len(title) > len(prefix)+len(suffix) {
			return title
		}
	}
	return strings.ReplaceAll(tmpl, "%s", title)
}

// headKey returns the dedupe key of a head tag, or "" for tags that may repeat.
func headKey(tag string) string {
	lower := strings.ToLower(tag)
	attr := func(name string) string {
		for _, q := range []string{`"`, `'`} {
			if i := strings.Index(lower, name+"="+q); i >= 0 {
				rest := lower[i+len(name)+2:]
				if j := strings.Index(rest, q); j >= 0 {
					return rest[:j]
				}
			}
		}
		return ""
	}
	switch {
	case strings.HasPrefix(lower, "<title"):
		return "title"
	case strings.HasPrefix(lower, "<meta"):
		if n := attr("name"); n != "" {
			return "meta:name:" + n
		}
		if p := attr("property"); p != "" {
			return "meta:property:" + p
		}
	case strings.HasPrefix(lower, "<link"):
		switch rel := attr("rel"); rel {
		case "canonical", "manifest", "icon", "shortcut icon", "apple-touch-icon":
			return "link:" + rel
		case "alternate":
			if h := attr("hreflang"); h != "" {
				return "link:alternate:" + h
			}
		}
	}
	return ""
}

// dedupeHead keeps the first <title>, <meta name|property> and singleton <link>
// per key. Page heads are placed before layout heads, so the page wins.
func dedupeHead(head string) string {
	if head == "" {
		return head
	}
	var out strings.Builder
	seen := map[string]bool{}
	i := 0
	for i < len(head) {
		lt := strings.IndexByte(head[i:], '<')
		if lt < 0 {
			out.WriteString(head[i:])
			break
		}
		out.WriteString(head[i : i+lt])
		start := i + lt
		var end int
		lower := strings.ToLower(head[start:])
		switch {
		case strings.HasPrefix(lower, "<!--"):
			if e := strings.Index(lower, "-->"); e >= 0 {
				end = start + e + 3
			} else {
				end = len(head)
			}
			out.WriteString(head[start:end])
			i = end
			continue
		case strings.HasPrefix(lower, "<script"), strings.HasPrefix(lower, "<style"), strings.HasPrefix(lower, "<title"), strings.HasPrefix(lower, "<noscript"):
			name := "script"
			for _, n := range []string{"style", "title", "noscript"} {
				if strings.HasPrefix(lower, "<"+n) {
					name = n
				}
			}
			if e := strings.Index(lower, "</"+name+">"); e >= 0 {
				end = start + e + len(name) + 3
			} else {
				end = len(head)
			}
		default:
			if e := htmlTagEnd(head, start); e > start {
				end = e
			} else {
				end = len(head)
			}
		}
		tag := head[start:end]
		if key := headKey(tag); key != "" {
			if seen[key] {
				i = end
				// Drop the newline that separated the removed tag.
				if i < len(head) && head[i] == '\n' {
					i++
				}
				continue
			}
			seen[key] = true
		}
		out.WriteString(tag)
		i = end
	}
	return out.String()
}

// templateHeadTitle applies site.titleTemplate to the document <title>.
func templateHeadTitle(head string, seo config.SEOConfig) string {
	if seo.TitleTemplate == "" {
		return head
	}
	lower := strings.ToLower(head)
	open := strings.Index(lower, "<title")
	if open < 0 {
		return head
	}
	gt := strings.IndexByte(head[open:], '>')
	if gt < 0 {
		return head
	}
	start := open + gt + 1
	end := strings.Index(lower[start:], "</title>")
	if end < 0 {
		return head
	}
	title := html.UnescapeString(strings.TrimSpace(head[start : start+end]))
	templated := applyTitleTemplateText(title, seo.SiteName, seo.TitleTemplate)
	if templated == title {
		return head
	}
	return head[:start] + escape.HTML(templated) + head[start+end:]
}

// isBareTitle reports whether a document title is a plain page title with no
// existing site/section suffix (in which case a title template should apply).
func isBareTitle(title, siteName string) bool {
	if title == "" || title == siteName {
		return false
	}
	for _, sep := range []string{" | ", " - ", " :: ", " \u2014 ", " \u2013 ", " \u00b7 "} {
		if strings.Contains(title, sep) {
			return false
		}
	}
	return true
}

// ogLocale converts an HTML lang tag ("en-US") to an og:locale value ("en_US").
func ogLocale(locale string) string {
	return strings.ReplaceAll(strings.TrimSpace(locale), "-", "_")
}

// twitterHandle normalizes a Twitter/X handle to its "@name" form.
func twitterHandle(handle string) string {
	handle = strings.TrimSpace(handle)
	if handle == "" || strings.HasPrefix(handle, "@") {
		return handle
	}
	return "@" + handle
}

// extractMetaContent extracts text content from <title>...</title> in headHTML.
func extractMetaContent(headHTML, tag string) string {
	lower := strings.ToLower(headHTML)
	open := "<" + tag + ">"
	close := "</" + tag + ">"
	idx := strings.Index(lower, open)
	if idx < 0 {
		return ""
	}
	start := idx + len(open)
	end := strings.Index(lower[start:], close)
	if end < 0 {
		return ""
	}
	return strings.TrimSpace(headHTML[start : start+end])
}

// extractMetaAttr extracts content from <meta name="name" content="value">.
func extractMetaAttr(headHTML, name string) string {
	lower := strings.ToLower(headHTML)
	search := "name=\"" + name + "\""
	idx := strings.Index(lower, search)
	if idx < 0 {
		search = "name='" + name + "'"
		idx = strings.Index(lower, search)
	}
	if idx < 0 {
		return ""
	}
	// Find content= after the name attribute, then read the opening quote that
	// actually delimits the value (either " or ').
	after := headHTML[idx:]
	eq := strings.Index(strings.ToLower(after), "content=")
	if eq < 0 {
		return ""
	}
	q := eq + len("content=")
	if q >= len(after) || (after[q] != '"' && after[q] != '\'') {
		return ""
	}
	quote := after[q : q+1]
	start := q + 1
	end := strings.Index(after[start:], quote)
	if end < 0 {
		return ""
	}
	return strings.TrimSpace(after[start : start+end])
}

// htmlTagEnd returns the index just past the tag starting at s[i], honoring
// quoted attribute values.
func htmlTagEnd(s string, i int) int {
	var q byte
	for j := i + 1; j < len(s); j++ {
		c := s[j]
		switch {
		case q != 0:
			if c == q {
				q = 0
			}
		case c == '"' || c == '\'':
			q = c
		case c == '>':
			return j + 1
		}
	}
	return len(s)
}
