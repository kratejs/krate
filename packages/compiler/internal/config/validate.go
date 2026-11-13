package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"reflect"
	"sort"
	"strings"

	"github.com/kratejs/krate/packages/compiler/internal/check"
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
	if c.Server.ShutdownTimeout < 0 {
		return nil, fmt.Errorf("server.shutdownTimeout must be >= 0 seconds, got %d", c.Server.ShutdownTimeout)
	}
	if c.Server.TrustedProxies < 0 {
		return nil, fmt.Errorf("server.trustedProxies must be >= 0, got %d", c.Server.TrustedProxies)
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
		if bp != "/" && strings.HasSuffix(bp, "/") {
			warnings = append(warnings, fmt.Sprintf("basePath %q should not end with /", bp))
		}
		if sp := strings.TrimRight(c.siteURLPath(), "/"); sp != "" && bp != "/" && sp != strings.TrimRight(bp, "/") {
			warnings = append(warnings, fmt.Sprintf("basePath %q differs from the path of the site URL (%q); basePath wins for routing and assets, so canonical URLs may not match where the site is served", bp, sp))
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
	if c.Cache.TimeoutMs < 0 || c.Cache.TTLSeconds < 0 {
		return nil, fmt.Errorf("cache.timeoutMs and cache.ttlSeconds must be >= 0")
	}
	if c.Cache.Adapter == "redis" && c.Cache.URL == "" && c.Cache.URLEnv == "" && os.Getenv("KRATE_CACHE_URL") == "" {
		return nil, fmt.Errorf("cache.adapter \"redis\" needs cache.url (redis://host:6379) or cache.urlEnv; none is set")
	}
	if u := c.Cache.URL; u != "" && c.Cache.Adapter == "redis" && !strings.HasPrefix(u, "redis://") && !strings.HasPrefix(u, "rediss://") {
		return nil, fmt.Errorf("cache.url must start with redis:// or rediss://, got %q", redactURL(u))
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

	nw, nerr := c.validateNative()
	if nerr != nil {
		return nil, nerr
	}
	warnings = append(warnings, nw...)

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
	// Unknown keys at any depth, including inside arrays of objects
	// (redirects[].permanant would otherwise be dropped silently).
	cfgType := reflect.TypeOf(Config{})
	for k, v := range m {
		// Plugin entries are descriptors that carry loader-only keys
		// (runtime, binaries, hooks), so their item keys are not checked.
		if k == "validate" || k == "$schema" || k == "plugins" || !knownTopLevelKeys[k] {
			continue
		}
		if ft, ok := fieldTypeByJSONName(cfgType, k); ok {
			unknown = append(unknown, unknownDeep(k, ft, v)...)
		}
	}
	sort.Strings(unknown)
	unknown = dedupeSorted(unknown)
	var out []string
	for _, k := range unknown {
		out = append(out, fmt.Sprintf("unknown config key %q (ignored)", k))
	}
	// `checks` is a free-form map, so the struct walk above cannot see into it.
	if rawChecks, ok := m["checks"]; ok {
		var checks map[string]any
		if json.Unmarshal(rawChecks, &checks) == nil {
			for _, u := range check.UnknownKeys(checks) {
				msg := fmt.Sprintf("unknown config key %q (ignored)", u.Path)
				if u.Suggestion != "" {
					msg = fmt.Sprintf("unknown config key %q (ignored; did you mean %q?)", u.Path, u.Suggestion)
				}
				out = append(out, msg)
			}
		}
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

func dedupeSorted(in []string) []string {
	out := in[:0]
	for i, v := range in {
		if i == 0 || v != in[i-1] {
			out = append(out, v)
		}
	}
	return out
}

// fieldTypeByJSONName finds the type of the struct field serialized as name.
func fieldTypeByJSONName(t reflect.Type, name string) (reflect.Type, bool) {
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return nil, false
	}
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		if n, ok := schemaFieldName(f); ok && n == name {
			return f.Type, true
		}
	}
	return nil, false
}

// unknownDeep walks raw alongside t and returns the unrecognized keys of every
// struct found, including those inside slices and maps of structs. Paths omit
// array indexes ("redirects.permanant").
func unknownDeep(path string, t reflect.Type, raw json.RawMessage) []string {
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	switch t.Kind() {
	case reflect.Struct:
		keys := structJSONKeys(t)
		if keys == nil {
			return nil
		}
		var obj map[string]json.RawMessage
		if json.Unmarshal(raw, &obj) != nil {
			return nil
		}
		var out []string
		for k, v := range obj {
			if !keys[k] {
				out = append(out, path+"."+k)
				continue
			}
			if ft, ok := fieldTypeByJSONName(t, k); ok {
				out = append(out, unknownDeep(path+"."+k, ft, v)...)
			}
		}
		return out
	case reflect.Slice:
		var arr []json.RawMessage
		if json.Unmarshal(raw, &arr) != nil {
			return nil
		}
		var out []string
		for _, item := range arr {
			out = append(out, unknownDeep(path, t.Elem(), item)...)
		}
		return out
	case reflect.Map:
		var obj map[string]json.RawMessage
		if json.Unmarshal(raw, &obj) != nil {
			return nil
		}
		var out []string
		for _, v := range obj {
			out = append(out, unknownDeep(path, t.Elem(), v)...)
		}
		return out
	}
	return nil
}

// describeUnmarshalError rewrites encoding/json type errors, which read like Go
// internals ("json: cannot unmarshal string into Go struct field
// Config.devServer.port of type int"), into a config-author message with the
// file and key path.
func describeUnmarshalError(file string, err error) error {
	var te *json.UnmarshalTypeError
	if !errors.As(err, &te) {
		return fmt.Errorf("%s: %w", file, err)
	}
	want := "a value of the right type"
	if te.Type != nil {
		switch te.Type.Kind() {
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
			reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
			reflect.Float32, reflect.Float64:
			want = "a number"
		case reflect.Bool:
			want = "a boolean"
		case reflect.String:
			want = "a string"
		case reflect.Slice, reflect.Array:
			want = "an array"
		case reflect.Struct, reflect.Map:
			want = "an object"
		}
	}
	key := strings.TrimPrefix(te.Field, "Config.")
	if key == "" {
		key = "value"
	}
	return fmt.Errorf("%s: %s must be %s (got %s)", file, key, want, te.Value)
}

// redactURL hides the credentials of a URL for messages.
func redactURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.User == nil {
		return raw
	}
	u.User = url.User("***")
	return u.String()
}
