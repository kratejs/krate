package config

import (
	"encoding/json"
	"fmt"
	"strings"
)

// SecurityConfig overrides the security response headers `krate serve` and
// `krate dev` send (and the Node server handler of a deployed server build).
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
// Source is an exact path, a prefix wildcard ("/assets/*"), a path.Match glob,
// or a Next.js style pattern such as "/:path*" or "/:file(.*\\.js)".
type HeaderRule struct {
	Source  string            `json:"source"`
	Headers map[string]string `json:"headers"`
	// Has and Missing add conditions on the request, as on a redirect.
	Has     []RouteCondition `json:"has,omitempty"`
	Missing []RouteCondition `json:"missing,omitempty"`
}

// UnmarshalJSON accepts headers as an object ({ "X-A": "1" }) or as Next's
// array of { key, value } pairs. In the array form the order is kept while
// reading, and when a key repeats (compared case-insensitively, as HTTP header
// names are) the last value wins.
func (r *HeaderRule) UnmarshalJSON(data []byte) error {
	var raw struct {
		Source  string           `json:"source"`
		Headers json.RawMessage  `json:"headers"`
		Has     []RouteCondition `json:"has"`
		Missing []RouteCondition `json:"missing"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	out := HeaderRule{Source: raw.Source, Has: raw.Has, Missing: raw.Missing}
	trimmed := strings.TrimSpace(string(raw.Headers))
	switch {
	case trimmed == "" || trimmed == "null":
	case strings.HasPrefix(trimmed, "["):
		var pairs []struct {
			Key   string `json:"key"`
			Value string `json:"value"`
		}
		if err := json.Unmarshal(raw.Headers, &pairs); err != nil {
			return fmt.Errorf("headers: expected an object or an array of { key, value } objects: %w", err)
		}
		out.Headers = map[string]string{}
		for _, p := range pairs {
			for existing := range out.Headers {
				if strings.EqualFold(existing, p.Key) {
					delete(out.Headers, existing)
				}
			}
			out.Headers[p.Key] = p.Value
		}
	default:
		if err := json.Unmarshal(raw.Headers, &out.Headers); err != nil {
			return fmt.Errorf("headers: expected an object or an array of { key, value } objects: %w", err)
		}
	}
	*r = out
	return nil
}

// HeaderDisabled reports whether a configured header value means "do not send".
func HeaderDisabled(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "off", "none", "false":
		return true
	}
	return false
}
