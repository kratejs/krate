package markdown

import (
	"html"
	"regexp"
	"strings"

	"github.com/kratejs/krate/packages/compiler/internal/escape"
)

var (
	boldRe     = regexp.MustCompile(`\*\*(.+?)\*\*`)
	italicRe   = regexp.MustCompile(`\*(.+?)\*`)
	strikeRe   = regexp.MustCompile(`~~(.+?)~~`)
	codeRe     = regexp.MustCompile("`([^`]+)`")
	autoLinkRe = regexp.MustCompile(`https?://[^\s<">]+`)
	schemeRe   = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9+.\-]*$`)
)

func renderInline(text string, cfg Config) string {
	text = escape.HTML(text)

	// Images (before links). The URL is entity-escaped before this runs, so a
	// scheme hidden behind entities (e.g. javascript&#58;) can never reach the
	// browser as a colon - but a literal javascript:/data: href would. Reject
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

	// Emoji shortcodes (`:smile:` -> ), outside inline code spans.
	text = replaceEmoji(text)

	// Autolinks (GFM) - skip URLs already inside HTML attributes (e.g.
	// href="...") to avoid double-wrapping in <a> tags.
	if cfg.GFM {
		var buf strings.Builder
		rest := text
		for {
			loc := autoLinkRe.FindStringIndex(rest)
			if loc == nil {
				buf.WriteString(rest)
				break
			}
			// If the character before the match is a quote, this URL lives
			// inside an HTML attribute - leave it alone.
			if loc[0] > 0 && (rest[loc[0]-1] == '"' || rest[loc[0]-1] == '\'') {
				buf.WriteString(rest[:loc[1]])
				rest = rest[loc[1]:]
				continue
			}
			buf.WriteString(rest[:loc[0]])
			u := rest[loc[0]:loc[1]]
			buf.WriteString(`<a href="`)
			buf.WriteString(u)
			buf.WriteString(`">`)
			buf.WriteString(u)
			buf.WriteString(`</a>`)
			rest = rest[loc[1]:]
		}
		text = buf.String()
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

// --- Emoji shortcodes -------------------------------------------------------

var emojiRe = regexp.MustCompile(`:([a-z0-9_+-]+):`)
var codeSpanRe = regexp.MustCompile(`(?s)<code>.*?</code>`)

// emojiMap is a small built-in set of common GitHub-style shortcodes. Unknown
// names are left untouched.
var emojiMap = map[string]string{
	"smile":                "\U0001F604",
	"grin":                 "\U0001F601",
	"joy":                  "\U0001F602",
	"laughing":             "\U0001F606",
	"wink":                 "\U0001F609",
	"heart":                "\u2764\uFE0F",
	"thumbsup":             "\U0001F44D",
	"+1":                   "\U0001F44D",
	"thumbsdown":           "\U0001F44E",
	"-1":                   "\U0001F44E",
	"fire":                 "\U0001F525",
	"rocket":               "\U0001F680",
	"sparkles":             "\u2728",
	"star":                 "\u2B50",
	"tada":                 "\U0001F389",
	"warning":              "\u26A0\uFE0F",
	"bulb":                 "\U0001F4A1",
	"memo":                 "\U0001F4DD",
	"book":                 "\U0001F4D6",
	"books":                "\U0001F4DA",
	"link":                 "\U0001F517",
	"lock":                 "\U0001F512",
	"key":                  "\U0001F511",
	"gear":                 "\u2699\uFE0F",
	"zap":                  "\u26A1",
	"bug":                  "\U0001F41B",
	"package":              "\U0001F4E6",
	"wrench":               "\U0001F527",
	"hammer":               "\U0001F528",
	"check":                "\u2705",
	"white_check_mark":     "\u2705",
	"x":                    "\u274C",
	"warning2":             "\u26A0\uFE0F",
	"information_source":   "\u2139\uFE0F",
	"question":             "\u2753",
	"eyes":                 "\U0001F440",
	"clap":                 "\U0001F44F",
	"raised_hands":         "\U0001F64C",
	"wave":                 "\U0001F44B",
	"point_right":          "\U0001F449",
	"point_left":           "\U0001F448",
	"heavy_check_mark":     "\u2714\uFE0F",
	"arrow_right":          "\u27A1\uFE0F",
	"arrow_left":           "\u2B05\uFE0F",
	"computer":             "\U0001F4BB",
	"globe_with_meridians": "\U0001F310",
	"construction":         "\U0001F6A7",
	"recycle":              "\u267B\uFE0F",
}

// replaceEmoji substitutes known `:shortcode:` tokens outside inline code.
func replaceEmoji(text string) string {
	if !strings.Contains(text, ":") {
		return text
	}
	var b strings.Builder
	last := 0
	for _, loc := range codeSpanRe.FindAllStringIndex(text, -1) {
		b.WriteString(emojiReplace(text[last:loc[0]]))
		b.WriteString(text[loc[0]:loc[1]])
		last = loc[1]
	}
	b.WriteString(emojiReplace(text[last:]))
	return b.String()
}

func emojiReplace(s string) string {
	return emojiRe.ReplaceAllStringFunc(s, func(m string) string {
		name := m[1 : len(m)-1]
		if e, ok := emojiMap[name]; ok {
			return e
		}
		return m
	})
}
