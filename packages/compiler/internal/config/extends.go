package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// StringList accepts either a single string or an array of strings in JSON.
type StringList []string

func (s *StringList) UnmarshalJSON(data []byte) error {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || string(trimmed) == "null" {
		*s = nil
		return nil
	}
	if trimmed[0] == '"' {
		var v string
		if err := json.Unmarshal(data, &v); err != nil {
			return err
		}
		*s = StringList{v}
		return nil
	}
	var arr []string
	if err := json.Unmarshal(data, &arr); err != nil {
		return err
	}
	*s = arr
	return nil
}

// errStaticFallback signals that the embedded evaluator could not run a
// non-module config, so Load should parse it statically instead.
var errStaticFallback = errors.New("static fallback")

// evaluateConfigJSON returns a config file's raw JSON. `.json` files are read
// directly; everything else is evaluated in the embedded runtime.
func evaluateConfigJSON(path string, opts LoadOptions) ([]byte, error) {
	if strings.EqualFold(filepath.Ext(path), ".json") {
		return os.ReadFile(path)
	}
	return evaluateEmbeddedTSConfig(path, opts)
}

// loadConfigMerged evaluates path and recursively merges any `extends` bases
// underneath it (bases first, this config last).
func loadConfigMerged(path string, opts LoadOptions, seen map[string]bool) ([]byte, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	if seen[abs] {
		return nil, fmt.Errorf("config extends cycle at %s", path)
	}
	seen[abs] = true
	defer delete(seen, abs)

	raw, err := evaluateConfigJSON(path, opts)
	if err != nil {
		src, _ := os.ReadFile(path)
		if configUsesModules(string(src)) {
			return nil, configNotExecutableError(path, err)
		}
		return nil, errStaticFallback
	}
	// Resolve relative module paths against the config that declares them, before
	// merging, so a base config's plugins/themes still resolve correctly.
	raw, err = absolutizeConfigPaths(raw, filepath.Dir(path))
	if err != nil {
		return nil, fmt.Errorf("parsing config %s: %w", path, err)
	}

	var head struct {
		Extends StringList `json:"extends"`
	}
	if err := json.Unmarshal(raw, &head); err != nil {
		return nil, fmt.Errorf("parsing config %s: %w", path, err)
	}
	if len(head.Extends) == 0 {
		return raw, nil
	}

	dir := filepath.Dir(path)
	merged := map[string]any{}
	for _, spec := range head.Extends {
		basePath, perr := resolveExtendsPath(dir, spec)
		if perr != nil {
			return nil, perr
		}
		baseRaw, berr := loadConfigMerged(basePath, opts, seen)
		if berr != nil {
			return nil, fmt.Errorf("extends %q: %w", spec, berr)
		}
		var baseObj map[string]any
		if err := json.Unmarshal(baseRaw, &baseObj); err != nil {
			return nil, fmt.Errorf("extends %q: %w", spec, err)
		}
		merged = deepMergeJSON(merged, baseObj)
	}

	var childObj map[string]any
	if err := json.Unmarshal(raw, &childObj); err != nil {
		return nil, fmt.Errorf("parsing config %s: %w", path, err)
	}
	delete(childObj, "extends")
	return json.Marshal(deepMergeJSON(merged, childObj))
}

// additiveArrayKeys are array-valued keys that concatenate when merging configs
// (base entries first, then the extending config's). Other arrays are replaced.
var additiveArrayKeys = map[string]bool{
	"plugins":           true,
	"redirects":         true,
	"rewrites":          true,
	"serverComponents":  true,
	"runtimeComponents": true,
	"serverDirs":        true,
	"runtimeDirs":       true,
}

func deepMergeJSON(base, override map[string]any) map[string]any {
	out := make(map[string]any, len(base)+len(override))
	for k, v := range base {
		out[k] = v
	}
	for k, v := range override {
		if bv, ok := out[k]; ok {
			if bm, ok := bv.(map[string]any); ok {
				if om, ok := v.(map[string]any); ok {
					out[k] = deepMergeJSON(bm, om)
					continue
				}
			}
			if additiveArrayKeys[k] {
				if ba, ok := bv.([]any); ok {
					if oa, ok := v.([]any); ok {
						if k == "plugins" {
							out[k] = mergePlugins(ba, oa)
						} else {
							merged := make([]any, 0, len(ba)+len(oa))
							merged = append(merged, ba...)
							merged = append(merged, oa...)
							out[k] = merged
						}
						continue
					}
				}
			}
		}
		out[k] = v
	}
	return out
}

// mergePlugins concatenates plugin entries, de-duplicating by name (or module)
// with the extending config's entry winning while keeping the base position.
func mergePlugins(base, override []any) []any {
	out := make([]any, 0, len(base)+len(override))
	index := map[string]int{}
	keyOf := func(v any) string {
		m, ok := v.(map[string]any)
		if !ok {
			return ""
		}
		if name, _ := m["name"].(string); name != "" {
			return "n:" + name
		}
		if mod, _ := m["module"].(string); mod != "" {
			return "m:" + mod
		}
		return ""
	}
	add := func(v any) {
		key := keyOf(v)
		if key == "" {
			out = append(out, v)
			return
		}
		if i, ok := index[key]; ok {
			out[i] = v
			return
		}
		index[key] = len(out)
		out = append(out, v)
	}
	for _, v := range base {
		add(v)
	}
	for _, v := range override {
		add(v)
	}
	return out
}

// resolveExtendsPath resolves an `extends` spec relative to dir, then to
// node_modules (for package presets).
func resolveExtendsPath(dir, spec string) (string, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return "", fmt.Errorf("extends: empty path")
	}
	cand := spec
	if !filepath.IsAbs(cand) {
		cand = filepath.Join(dir, filepath.FromSlash(spec))
	}
	for _, p := range configFileCandidates(cand) {
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			return p, nil
		}
	}
	if !strings.HasPrefix(spec, ".") && !filepath.IsAbs(spec) {
		for d := dir; ; d = filepath.Dir(d) {
			base := filepath.Join(d, "node_modules", filepath.FromSlash(spec))
			for _, p := range configFileCandidates(base) {
				if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
					return p, nil
				}
			}
			// Fall back to the package's declared entry (main/module/exports or
			// a `krate` field), so published presets with dist/ entry points work.
			if entry := resolvePackageEntry(base); entry != "" {
				cand := filepath.Join(base, filepath.FromSlash(entry))
				for _, p := range configFileCandidates(cand) {
					if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
						return p, nil
					}
				}
			}
			parent := filepath.Dir(d)
			if parent == d {
				break
			}
		}
	}
	return "", fmt.Errorf("extends %q not found (resolved relative to %s)", spec, dir)
}

// resolvePackageEntry reads a package's `krate` field, then `exports["."]`,
// `module`, or `main`, returning the config entry path (relative to the package).
func resolvePackageEntry(base string) string {
	data, err := os.ReadFile(filepath.Join(base, "package.json"))
	if err != nil {
		return ""
	}
	var pkg struct {
		Krate   json.RawMessage `json:"krate"`
		Module  string          `json:"module"`
		Main    string          `json:"main"`
		Exports json.RawMessage `json:"exports"`
	}
	if json.Unmarshal(data, &pkg) != nil {
		return ""
	}
	if s := krateEntry(pkg.Krate); s != "" {
		return s
	}
	if s := exportEntry(pkg.Exports); s != "" {
		return s
	}
	if pkg.Module != "" {
		return pkg.Module
	}
	return pkg.Main
}

// krateEntry accepts `"krate": "./krate.config.ts"` or
// `"krate": { "config": "./krate.config.ts" }`.
func krateEntry(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var obj map[string]json.RawMessage
	if json.Unmarshal(raw, &obj) == nil {
		var cfg string
		if json.Unmarshal(obj["config"], &cfg) == nil {
			return cfg
		}
	}
	return ""
}

// exportEntry extracts a usable path from `exports` (string, "." entry, or a
// condition object with import/default/require).
func exportEntry(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var obj map[string]json.RawMessage
	if json.Unmarshal(raw, &obj) != nil {
		return ""
	}
	if v, ok := obj["."]; ok {
		if s := exportEntry(v); s != "" {
			return s
		}
	}
	for _, key := range []string{"import", "default", "require"} {
		if v, ok := obj[key]; ok {
			var s string
			if json.Unmarshal(v, &s) == nil {
				return s
			}
		}
	}
	return ""
}

// absolutizeConfigPaths resolves a config's relative module paths (plugin
// modules, theme modules, markdown plugins) against dir, so a base config's
// relative paths still resolve to the base config's directory after merging.
func absolutizeConfigPaths(raw []byte, dir string) ([]byte, error) {
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil, err
	}
	changed := false
	abs := func(p string) string {
		if p == "" || filepath.IsAbs(p) {
			return p
		}
		return filepath.Join(dir, p)
	}
	if plugins, ok := obj["plugins"].([]any); ok {
		for _, pe := range plugins {
			pm, ok := pe.(map[string]any)
			if !ok {
				continue
			}
			if mod, ok := pm["module"].(string); ok {
				if next := abs(mod); next != mod {
					pm["module"] = next
					changed = true
				}
			}
			if opts, ok := pm["options"].(map[string]any); ok {
				if theme, ok := opts["theme"].(map[string]any); ok {
					if tmod, ok := theme["module"].(string); ok {
						if next := abs(tmod); next != tmod {
							theme["module"] = next
							changed = true
						}
					}
				}
			}
		}
	}
	if md, ok := obj["markdown"].(map[string]any); ok {
		if list, ok := md["plugins"].([]any); ok {
			for i, v := range list {
				if s, ok := v.(string); ok {
					if next := abs(s); next != s {
						list[i] = next
						changed = true
					}
				}
			}
		}
	}
	if !changed {
		return raw, nil
	}
	return json.Marshal(obj)
}

func configFileCandidates(p string) []string {
	if filepath.Ext(p) != "" {
		return []string{p}
	}
	return []string{
		p,
		p + ".ts",
		p + ".js",
		p + ".mjs",
		p + ".json",
		filepath.Join(p, "krate.config.ts"),
		filepath.Join(p, "krate.config.js"),
		filepath.Join(p, "index.ts"),
		filepath.Join(p, "index.js"),
	}
}
