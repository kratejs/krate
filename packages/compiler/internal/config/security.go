package config

import "strings"

// SecurityConfig overrides the security response headers `krate serve` sends.
// Empty fields keep the framework default; the value "off"/"none"/"false"
// disables the header entirely.
type SecurityConfig struct {
	// ContentTypeOptions toggles `X-Content-Type-Options: nosniff` (default on).
	ContentTypeOptions *bool `json:"contentTypeOptions,omitempty"`
	// FrameOptions is `X-Frame-Options` (default "DENY").
	FrameOptions string `json:"frameOptions,omitempty"`
	// ReferrerPolicy is `Referrer-Policy` (default "strict-origin-when-cross-origin").
	ReferrerPolicy string `json:"referrerPolicy,omitempty"`
	// PermissionsPolicy is `Permissions-Policy` (default "camera=(), microphone=(), geolocation=()").
	PermissionsPolicy string `json:"permissionsPolicy,omitempty"`
	// HSTS is `Strict-Transport-Security` (default emitted for https base URLs).
	HSTS string `json:"hsts,omitempty"`
}

// HeaderRule sets response headers for requests whose path matches Source.
// Source is an exact path, a prefix wildcard ("/assets/*"), or a path.Match glob.
type HeaderRule struct {
	Source  string            `json:"source"`
	Headers map[string]string `json:"headers"`
}

// HeaderDisabled reports whether a configured header value means "do not send".
func HeaderDisabled(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "off", "none", "false":
		return true
	}
	return false
}
