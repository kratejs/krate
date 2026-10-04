package build

import (
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/kratejs/krate/packages/compiler/internal/escape"
)

func generateHTML(bodyHTML, headHTML, scriptHTML, styleHTML string, cssFiles []string, jsFile, runtimeJSFile, route string, devMode bool, devBootstrap, basePath string) string {
	var b strings.Builder

	b.WriteString("<!DOCTYPE html>\n<html lang=\"en\">\n<head>\n")
	b.WriteString("<meta charset=\"UTF-8\">\n")
	b.WriteString("<meta name=\"viewport\" content=\"width=device-width, initial-scale=1.0, viewport-fit=cover\">\n")

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
func generateHTMLWithLoading(bodyHTML, headHTML, scriptHTML, styleHTML, loadingHTML string, cssFiles []string, jsFile, runtimeJSFile, route string, devMode bool, devBootstrap, basePath string) string {
	html := generateHTML(bodyHTML, headHTML, scriptHTML, styleHTML, cssFiles, jsFile, runtimeJSFile, route, devMode, devBootstrap, basePath)
	if loadingHTML != "" {
		loadingTemplate := "<template data-krate-loading>" + loadingHTML + "</template>"
		html = strings.Replace(html, "</div>\n</body>", loadingTemplate+"</div>\n</body>", 1)
	}
	return html
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

	// A base 'self' policy is meaningful even with no inline assets, so CSP is
	// always enforced once enabled. Inline assets, when present, are allowlisted
	// individually via their SHA-256 hashes.
	var directives []string
	directives = append(directives, "default-src 'self'")

	scriptSrc := "script-src 'self'"
	for _, h := range scriptHashes {
		scriptSrc += " " + h
	}
	directives = append(directives, scriptSrc)

	styleSrc := "style-src 'self'"
	for _, h := range styleHashes {
		styleSrc += " " + h
	}
	directives = append(directives, styleSrc)

	return fmt.Sprintf("<meta http-equiv=\"Content-Security-Policy\" content=\"%s\">\n", strings.Join(directives, "; "))
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
func generateSEOTags(headHTML, route, baseURL, siteName, defaultDescription, defaultImage string) string {
	if baseURL == "" {
		return ""
	}
	baseURL = strings.TrimRight(baseURL, "/")

	canonical := baseURL + "/"
	if route != "" && route != "." {
		canonical = baseURL + "/" + strings.TrimPrefix(route, "/")
	}

	var tags []string

	if !strings.Contains(headHTML, "rel=\"canonical\"") && !strings.Contains(headHTML, "rel='canonical'") {
		tags = append(tags, fmt.Sprintf("<link rel=\"canonical\" href=\"%s\">", canonical))
	}

	// Extract title from headHTML if present (for OG tags)
	title := extractMetaContent(headHTML, "title")
	if title == "" {
		title = siteName
	}

	// Extract description from headHTML <meta name="description"> if present
	desc := extractMetaAttr(headHTML, "description")
	if desc == "" {
		desc = defaultDescription
	}

	// OpenGraph tags
	if !strings.Contains(headHTML, "og:title") && title != "" {
		tags = append(tags, fmt.Sprintf("<meta property=\"og:title\" content=\"%s\">", escape.HTMLAttr(title)))
	}
	if !strings.Contains(headHTML, "og:url") {
		tags = append(tags, fmt.Sprintf("<meta property=\"og:url\" content=\"%s\">", canonical))
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
	if !strings.Contains(headHTML, "og:image") && defaultImage != "" {
		img := defaultImage
		if !strings.HasPrefix(img, "http") {
			img = baseURL + "/" + strings.TrimPrefix(img, "/")
		}
		tags = append(tags, fmt.Sprintf("<meta property=\"og:image\" content=\"%s\">", img))
	}

	// Twitter Card tags
	if !strings.Contains(headHTML, "twitter:card") {
		tags = append(tags, "<meta name=\"twitter:card\" content=\"summary\">")
	}
	if !strings.Contains(headHTML, "twitter:title") && title != "" {
		tags = append(tags, fmt.Sprintf("<meta name=\"twitter:title\" content=\"%s\">", escape.HTMLAttr(title)))
	}
	if !strings.Contains(headHTML, "twitter:description") && desc != "" {
		tags = append(tags, fmt.Sprintf("<meta name=\"twitter:description\" content=\"%s\">", escape.HTMLAttr(desc)))
	}

	if len(tags) == 0 {
		return ""
	}
	return strings.Join(tags, "\n") + "\n"
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
	// Find content="..." after the name attribute
	after := headHTML[idx:]
	contentIdx := strings.Index(strings.ToLower(after), "content=\"")
	if contentIdx < 0 {
		contentIdx = strings.Index(strings.ToLower(after), "content='")
	}
	if contentIdx < 0 {
		return ""
	}
	start := contentIdx + len("content=\"")
	quote := after[contentIdx+len("content") : contentIdx+len("content")+1]
	end := strings.Index(after[start:], quote)
	if end < 0 {
		return ""
	}
	return strings.TrimSpace(after[start : start+end])
}
