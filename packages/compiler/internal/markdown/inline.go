package markdown

import (
	"html"
	"regexp"
	"strings"

	"krate-compiler/internal/escape"
)

var (
	boldRe      = regexp.MustCompile(`\*\*(.+?)\*\*`)
	italicRe    = regexp.MustCompile(`\*(.+?)\*`)
	strikeRe    = regexp.MustCompile(`~~(.+?)~~`)
	codeRe      = regexp.MustCompile("`([^`]+)`")
	autoLinkRe  = regexp.MustCompile(`https?://[^\s<">]+`)
	schemeRe    = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9+.\-]*$`)
)

func renderInline(text string, cfg Config) string {
	text = escape.HTML(text)

	// Images (before links). The URL is entity-escaped before this runs, so a
	// scheme hidden behind entities (e.g. javascript&#58;) can never reach the
	// browser as a colon — but a literal javascript:/data: href would. Reject
	// script-capable schemes outright.
	text = imageRe.ReplaceAllStringFunc(text, func(match string) string {
		parts := imageRe.FindStringSubmatch(match)
		if len(parts) < 3 {
			return match
		}
		alt, src := parts[1], parts[2]
		if !safeURL(src) {
			return alt
		}
		return `<img src="` + src + `" alt="` + alt + `">`
	})

	// Links
	text = linkRe.ReplaceAllStringFunc(text, func(match string) string {
		parts := linkRe.FindStringSubmatch(match)
		if len(parts) < 3 {
			return match
		}
		label, dest := parts[1], parts[2]
		if !safeURL(dest) {
			return label
		}
		return `<a href="` + dest + `">` + label + `</a>`
	})

	// Inline code
	text = codeRe.ReplaceAllString(text, "<code>$1</code>")

	// Bold
	text = boldRe.ReplaceAllString(text, "<strong>$1</strong>")

	// Italic
	text = italicRe.ReplaceAllString(text, "<em>$1</em>")

	// Strikethrough (GFM)
	if cfg.GFM {
		text = strikeRe.ReplaceAllString(text, "<del>$1</del>")
	}

	// Autolinks (GFM)
	if cfg.GFM {
		text = autoLinkRe.ReplaceAllStringFunc(text, func(match string) string {
			return `<a href="` + match + `">` + match + `</a>`
		})
	}

	return text
}

// safeURL reports whether a link/image destination may be emitted as an href or
// src. It allows scheme-less URLs (relative paths, anchors, protocol-relative)
// and a small allowlist of safe schemes; anything else (javascript:, data:,
// vbscript:, file:, etc.) is rejected so markdown can never produce a
// script-capable destination.
func safeURL(dest string) bool {
	decoded := html.UnescapeString(dest)
	lower := strings.ToLower(strings.TrimSpace(decoded))
	if lower == "" || strings.HasPrefix(lower, "//") {
		return true
	}
	idx := strings.IndexByte(lower, ':')
	if idx < 0 {
		return true
	}
	scheme := lower[:idx]
	switch scheme {
	case "http", "https", "mailto", "tel", "ftp":
		return true
	}
	// A colon in a relative path like "docs/guide:3" is not a URI scheme unless
	// it matches the RFC 3986 scheme grammar ([a-zA-Z][a-zA-Z0-9+.-]*). Only a
	// scheme-like prefix is treated as a scheme to reject.
	return !schemeRe.MatchString(scheme)
}
