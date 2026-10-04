package config

import (
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"
)

// knownNestedKeys maps a known top-level object key to the nested keys we
// recognize, so typos inside nested config objects surface too (JSON unmarshal
// otherwise drops them silently).
var knownNestedKeys = map[string]map[string]bool{
	"devServer": {"port": true, "open": true, "overlay": true, "toolbar": true, "editor": true},
	"server":    {"host": true, "port": true, "maxBodySize": true},
	"cors":      {"enabled": true, "origins": true, "methods": true, "headers": true, "credentials": true, "maxAge": true},
	"api":       {"sidecar": true},
	"ssr":       {"rendererPort": true, "timeout": true, "maxCacheSize": true, "middlewareRuntime": true, "apiRuntime": true, "ssrRuntime": true, "streaming": true},
	"markdown":  {"root": true, "gfm": true, "headingAnchors": true, "admonitions": true, "codeHighlight": true, "codeTheme": true, "math": true},
	"tailwind":  {"enabled": true, "scanDirs": true, "content": true, "preflight": true, "strict": true, "darkMode": true, "executeConfig": true},
	"csp":       {"enabled": true, "directive": true},
	"seo":       {"baseUrl": true, "siteName": true, "description": true, "image": true},
	"robots":    {"allow": true, "disallow": true, "sitemap": true},
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
var knownTopLevelKeys = map[string]bool{
	"entry": true, "outDir": true, "pagesDir": true, "publicDir": true,
	"minify": true, "minifyHTML": true, "minifyCSS": true, "minifyJS": true,
	"sourcemap": true, "devServer": true, "plugins": true, "emitReact": true,
	"markdown": true, "tailwind": true, "csp": true, "runtime": true, "ssr": true,
	"pathAliases": true, "tsBaseDir": true, "redirects": true, "rewrites": true,
	"seo": true, "robots": true, "output": true, "serverComponents": true,
	"runtimeComponents": true, "serverDirs": true, "runtimeDirs": true,
	"content": true, "checks": true,
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
	if bp := c.BasePath; bp != "" {
		if strings.Contains(bp, "://") || !strings.HasPrefix(bp, "/") {
			return nil, fmt.Errorf("basePath must be a path starting with / (e.g. \"/docs\"), got %q", bp)
		}
		if strings.HasSuffix(bp, "/") {
			warnings = append(warnings, fmt.Sprintf("basePath %q should not end with /", bp))
		}
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
		if k == "validate" { // user-supplied hook, intentionally not in Config
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
