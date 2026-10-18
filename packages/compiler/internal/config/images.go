package config

import "strings"

// ImageRemotePattern allows a remote image host (and optional path pattern) to be
// referenced from <Image src> without processing. Any other remote src is
// rejected with a build error.
type ImageRemotePattern struct {
	// Hostname supports exact matches, `*.example.com` (one label), and
	// `**.example.com` (any depth).
	Hostname string `json:"hostname"`
	// Pathname optionally restricts the path: `*` matches one segment, `**`
	// matches any depth (e.g. "/images/**").
	Pathname string `json:"pathname,omitempty"`
}

// ImagesConfig configures the responsive-image pipeline used by <Image>.
type ImagesConfig struct {
	// Breakpoints are the generated responsive widths (ascending).
	Breakpoints []int `json:"breakpoints,omitempty"`
	// Quality is the base encode quality (1-100, default 82).
	Quality int `json:"quality,omitempty"`
	// Sizes is the default `sizes` attribute (may be overridden per <Image>).
	Sizes string `json:"sizes,omitempty"`
	// Placeholder toggles the blur-up placeholder (default true).
	Placeholder *bool `json:"placeholder,omitempty"`
	// Formats selects output formats: any of "webp", "original". Empty = both.
	// (AVIF is not supported; it needs a separate encoder.)
	Formats []string `json:"formats,omitempty"`
	// Unoptimized emits the original image with no variants/processing.
	Unoptimized bool `json:"unoptimized,omitempty"`
	// RemotePatterns allowlists remote image hosts.
	RemotePatterns []ImageRemotePattern `json:"remotePatterns,omitempty"`
}

// PlaceholderEnabled reports whether the blur placeholder is on (default true).
func (c ImagesConfig) PlaceholderEnabled() bool { return c.Placeholder == nil || *c.Placeholder }

// RemoteAllowed reports whether a remote image URL is allowlisted.
func (c ImagesConfig) RemoteAllowed(host, path string) bool {
	for _, p := range c.RemotePatterns {
		if p.Hostname == "" || !matchImageHost(p.Hostname, host) {
			continue
		}
		if matchImagePath(p.Pathname, path) {
			return true
		}
	}
	return false
}

func matchImageHost(pattern, host string) bool {
	pattern = strings.ToLower(strings.TrimSpace(pattern))
	host = strings.ToLower(strings.TrimSpace(host))
	if pattern == host {
		return true
	}
	if strings.HasPrefix(pattern, "**.") {
		return strings.HasSuffix(host, pattern[2:])
	}
	if strings.HasPrefix(pattern, "*.") {
		suffix := pattern[1:] // ".example.com"
		if !strings.HasSuffix(host, suffix) {
			return false
		}
		return !strings.Contains(host[:len(host)-len(suffix)], ".")
	}
	return false
}

func matchImagePath(pattern, p string) bool {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" || pattern == "/" {
		return true
	}
	ps := strings.Split(strings.Trim(pattern, "/"), "/")
	xs := strings.Split(strings.Trim(p, "/"), "/")
	return matchImageSegments(ps, xs)
}

func matchImageSegments(ps, xs []string) bool {
	if len(ps) == 0 {
		return len(xs) == 0
	}
	if ps[0] == "**" {
		for i := 0; i <= len(xs); i++ {
			if matchImageSegments(ps[1:], xs[i:]) {
				return true
			}
		}
		return false
	}
	if len(xs) == 0 || !globSegment(ps[0], xs[0]) {
		return false
	}
	return matchImageSegments(ps[1:], xs[1:])
}

// globSegment matches one path segment where `*` stands for any run of chars.
func globSegment(pattern, s string) bool {
	pi, si := 0, 0
	star, match := -1, 0
	for si < len(s) {
		switch {
		case pi < len(pattern) && pattern[pi] == s[si]:
			pi++
			si++
		case pi < len(pattern) && pattern[pi] == '*':
			star = pi
			match = si
			pi++
		case star != -1:
			pi = star + 1
			match++
			si = match
		default:
			return false
		}
	}
	for pi < len(pattern) && pattern[pi] == '*' {
		pi++
	}
	return pi == len(pattern)
}
