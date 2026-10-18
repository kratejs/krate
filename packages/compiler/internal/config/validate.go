package config

import (
	"encoding/json"
	"fmt"
	"net/url"
	"reflect"
	"sort"
	"strings"
)

// knownNestedKeys maps a known top-level object key to the nested keys we
// recognize. It is derived from the Config struct's field types so adding a
// struct field registers its nested keys automatically.
var knownNestedKeys = buildKnownNestedKeys()

func buildKnownNestedKeys() map[string]map[string]bool {
	out := map[string]map[string]bool{}
	t := reflect.TypeOf(Config{})
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		name, ok := schemaFieldName(f)
		if !ok {
			continue
		}
		if keys := structJSONKeys(f.Type); len(keys) > 0 {
			out[name] = keys
		}
	}
	return out
}

// structJSONKeys returns the JSON field names of a struct type (dereferencing
// pointers), or nil for non-structs (maps/slices/interfaces accept arbitrary
// keys).
func structJSONKeys(t reflect.Type) map[string]bool {
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return nil
	}
	keys := map[string]bool{}
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		if name, ok := schemaFieldName(f); ok {
			keys[name] = true
		}
	}
	if len(keys) == 0 {
		return nil
	}
	return keys
}

// knownSidecarKeys is the recognized nested key set for api.sidecar.
var knownSidecarKeys = map[string]bool{
	"command": true, "args": true, "cwd": true, "env": true,
	"port": true, "target": true, "prefix": true,
}

// Validation issues are split into hard errors (invalid values that will
// misbehave) and warnings (unknown keys, suspicious values). Load surfaces both.

// knownTopLevelKeys is the set of recognized top-level config keys, derived from
// the Config struct's JSON tags. Used to warn on typos/unknown keys that JSON
// unmarshalling would otherwise drop silently.
var knownTopLevelKeys = buildKnownTopLevelKeys()

// buildKnownTopLevelKeys derives the recognized top-level keys from Config's
// JSON tags (plus keys parsed specially with no JSON field), so adding a struct
// field registers it in one place.
func buildKnownTopLevelKeys() map[string]bool {
	m := map[string]bool{
		"viewTransitions": true,
		"ppr":             true,
		"emitReact":       true,
	}
	t := reflect.TypeOf(Config{})
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		if name, ok := schemaFieldName(f); ok {
			m[name] = true
		}
	}
	return m
}

// Validate performs semantic/bounds checks that JSON typing alone can't. It
// returns a fatal error for values that would break a build, and a list of
// warnings for suspicious-but-tolerable values.
func (c *Config) Validate() (warnings []string, err error) {
	if c.Output != "" && c.Output != "static" {
		return nil, fmt.Errorf("output must be \"\" or \"static\", got %q", c.Output)
	}
	if c.DevServer.Port < 0 || c.DevServer.Port > 65535 {
		return nil, fmt.Errorf("devServer.port must be between 0 and 65535, got %d", c.DevServer.Port)
	}
	if c.Server.Port < 0 || c.Server.Port > 65535 {
		return nil, fmt.Errorf("server.port must be between 0 and 65535, got %d", c.Server.Port)
	}
	if c.Server.MaxBodySize < 0 {
		return nil, fmt.Errorf("server.maxBodySize must be >= 0, got %d", c.Server.MaxBodySize)
	}
	if c.Site != (SiteConfig{}) && c.SEO != (SEOConfig{}) {
		warnings = append(warnings, "both `site` and `seo` are set; `site` wins - migrate to `site` and remove `seo`")
	}
	if c.GoAPI.Enabled != nil || c.GoAPI.Module != "" || len(c.GoAPI.Deps) > 0 || len(c.GoAPI.Replaces) > 0 || c.GoAPI.Tidy != nil {
		warnings = append(warnings, "`goApi` is deprecated; use the unified `go` block")
	}
	if bp := c.BasePath; bp != "" {
		if strings.Contains(bp, "://") || !strings.HasPrefix(bp, "/") {
			return nil, fmt.Errorf("basePath must be a path starting with / (e.g. \"/docs\"), got %q", bp)
		}
		if strings.HasSuffix(bp, "/") {
			warnings = append(warnings, fmt.Sprintf("basePath %q should not end with /", bp))
		}
	}
	switch c.Compat.ModeOrDefault() {
	case "modern", "legacy", "hybrid":
	default:
		return nil, fmt.Errorf("compat.mode must be \"modern\", \"legacy\", or \"hybrid\", got %q", c.Compat.Mode)
	}
	if c.Compat.UserAgent && c.Compat.ModeOrDefault() != "hybrid" {
		warnings = append(warnings, "compat.userAgent only applies to compat.mode \"hybrid\"; ignored")
	}
	if c.CORS.Enabled && c.CORS.Credentials {
		for _, o := range c.CORS.Origins {
			if o == "*" {
				warnings = append(warnings, "cors.credentials with origins [\"*\"] is rejected by browsers; list explicit origins")
			}
		}
	}
	if sc := c.API.Sidecar; sc != nil {
		if sc.Port < 0 || sc.Port > 65535 {
			return nil, fmt.Errorf("api.sidecar.port must be between 0 and 65535, got %d", sc.Port)
		}
		if sc.Command != "" && sc.Port == 0 && sc.Target == "" {
			return nil, fmt.Errorf("api.sidecar.command requires a port (the supervised process must listen somewhere)")
		}
		if sc.Target != "" {
			u, perr := url.Parse(sc.Target)
			if perr != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
				return nil, fmt.Errorf("api.sidecar.target must be an http(s) URL, got %q", sc.Target)
			}
		}
		if sc.Prefix != "" && !strings.HasPrefix(sc.Prefix, "/") {
			return nil, fmt.Errorf("api.sidecar.prefix must start with /, got %q", sc.Prefix)
		}
	}
	if c.SSR.RendererPort < 0 || c.SSR.RendererPort > 65535 {
		return nil, fmt.Errorf("ssr.rendererPort must be between 0 and 65535, got %d", c.SSR.RendererPort)
	}
	if c.SSR.Timeout < 0 {
		return nil, fmt.Errorf("ssr.timeout must be >= 0, got %d", c.SSR.Timeout)
	}
	if c.SSR.MaxCacheSize < 0 {
		return nil, fmt.Errorf("ssr.maxCacheSize must be >= 0, got %d", c.SSR.MaxCacheSize)
	}
	if c.SSR.APIWorkers < 0 {
		return nil, fmt.Errorf("ssr.apiWorkers must be >= 0, got %d", c.SSR.APIWorkers)
	}
	if c.SSR.NodeWorkers < 0 {
		return nil, fmt.Errorf("ssr.nodeWorkers must be >= 0, got %d", c.SSR.NodeWorkers)
	}
	if c.SSR.RendererWorkers < 0 {
		return nil, fmt.Errorf("ssr.rendererWorkers must be >= 0, got %d", c.SSR.RendererWorkers)
	}
	if c.SSR.APIWorkers > 4096 || c.SSR.NodeWorkers > 4096 || c.SSR.RendererWorkers > 4096 {
		warnings = append(warnings, "ssr worker counts above 4096 are likely a mistake")
	}
	if c.SSR.Timeout > 600000 {
		warnings = append(warnings, "ssr.timeout above 600000ms (10m) is unusually large")
	}
	if c.SSR.Streaming && c.Output == "static" {
		warnings = append(warnings, "ssr.streaming is ignored when output is \"static\"")
	}

	for _, rd := range c.Redirects {
		if !strings.HasPrefix(rd.Source, "/") {
			warnings = append(warnings, fmt.Sprintf("redirect source %q should start with /", rd.Source))
		}
	}
	for _, rw := range c.Rewrites {
		if !strings.HasPrefix(rw.Source, "/") {
			warnings = append(warnings, fmt.Sprintf("rewrite source %q should start with /", rw.Source))
		}
	}

	if c.SSR.RendererPort != 0 && c.SSR.RendererPort == c.DevServer.Port {
		warnings = append(warnings, "ssr.rendererPort equals devServer.port; choose a distinct port")
	}

	return warnings, nil
}

// UnknownKeyWarnings compares the raw config JSON against the known key sets and
// returns a warning per unrecognized top-level or nested key. This catches typos
// that the typed unmarshal silently drops.
func UnknownKeyWarnings(raw []byte) []string {
	if len(raw) == 0 {
		return nil
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil
	}
	var unknown []string
	for k, v := range m {
		if k == "validate" || k == "$schema" {
			// `validate` is a user-supplied hook (intentionally not in Config);
			// `$schema` is a JSON-Schema editor hint (e.g. pointing at
			// .krate/config.schema.json).
			continue
		}
		if !knownTopLevelKeys[k] {
			unknown = append(unknown, k)
			continue
		}
		if nested, ok := knownNestedKeys[k]; ok {
			unknown = append(unknown, unknownNestedKeys(k, nested, v)...)
		}
	}
	// api.sidecar is a second-level object with its own key set.
	if rawAPI, ok := m["api"]; ok {
		var apiObj map[string]json.RawMessage
		if json.Unmarshal(rawAPI, &apiObj) == nil {
			if rawSC, ok := apiObj["sidecar"]; ok {
				unknown = append(unknown, unknownNestedKeys("api.sidecar", knownSidecarKeys, rawSC)...)
			}
		}
	}
	// go.deps / go.replaces / go.exclude array item keys (typos in the object
	// items are otherwise silently dropped).
	if rawGo, ok := m["go"]; ok {
		unknown = append(unknown, unknownArrayItemKeys("go.deps", rawGo, "deps", []string{"path", "version"})...)
		unknown = append(unknown, unknownArrayItemKeys("go.replaces", rawGo, "replaces", []string{"from", "to"})...)
		unknown = append(unknown, unknownArrayItemKeys("go.exclude", rawGo, "exclude", []string{"path", "version"})...)
	}
	sort.Strings(unknown)
	var out []string
	for _, k := range unknown {
		out = append(out, fmt.Sprintf("unknown config key %q (ignored)", k))
	}
	return out
}

// unknownNestedKeys returns the keys in raw that are not in known, prefixed with
// parent (e.g. "ssr.typo"). Non-object raw values are ignored here (type errors
// are caught by applyConfigProp / the typed unmarshal).
func unknownNestedKeys(parent string, known map[string]bool, raw json.RawMessage) []string {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil
	}
	var out []string
	for k := range obj {
		if !known[k] {
			out = append(out, parent+"."+k)
		}
	}
	return out
}

// unknownArrayItemKeys returns unrecognized keys inside a named array field of
// the parent object (e.g. go.deps[].path). Non-array/non-object values are
// ignored.
func unknownArrayItemKeys(parent string, raw json.RawMessage, field string, known []string) []string {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil
	}
	rawArr, ok := obj[field]
	if !ok {
		return nil
	}
	var arr []map[string]json.RawMessage
	if err := json.Unmarshal(rawArr, &arr); err != nil {
		return nil
	}
	knownSet := make(map[string]bool, len(known))
	for _, k := range known {
		knownSet[k] = true
	}
	var out []string
	for _, item := range arr {
		for k := range item {
			if !knownSet[k] {
				out = append(out, parent+"."+k)
			}
		}
	}
	return out
}
