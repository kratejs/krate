package plugin

import (
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

// builtinOptionTypes maps a built-in plugin name to the struct that decodes its
// `options`. Keeping this list next to the plugin implementations lets
// ConfigWarnings reflect over the real json tags, so the "known keys" set can
// never drift from the code.
func builtinOptionTypes() map[string]reflect.Type {
	return map[string]reflect.Type{
		"docs":    reflect.TypeOf(DocsPluginOptions{}),
		"sitemap": reflect.TypeOf(SitemapPluginOptions{}),
		"feed":    reflect.TypeOf(FeedPluginOptions{}),
		"llms":    reflect.TypeOf(LLMsPluginOptions{}),
		"robots":  reflect.TypeOf(RobotsPluginOptions{}),
	}
}

// ConfigWarnings returns non-fatal plugin-configuration warnings: unknown option
// keys for built-in plugins (typos that would otherwise be silently ignored) and
// option-decode errors. The CLI prints these alongside config.Warnings.
func ConfigWarnings(cfg *config.Config) []string {
	if cfg == nil {
		return nil
	}
	types := builtinOptionTypes()
	var out []string
	for _, pc := range cfg.Plugins {
		if pc.Options == nil {
			continue
		}
		if t, ok := types[pc.Name]; ok {
			for _, k := range unknownObjectKeys(t, pc.Options, "") {
				out = append(out, fmt.Sprintf("unknown %s option %q (ignored)", pc.Name, k))
			}
		}
		// A decode failure means the plugin will not run at all; surface it
		// rather than letting the plugin silently do nothing.
		if t, ok := types[pc.Name]; ok {
			if err := pc.ParseOptions(reflect.New(t).Interface()); err != nil {
				out = append(out, fmt.Sprintf("%s options do not decode: %v", pc.Name, err))
			}
		}
		if pc.Name == "docs" {
			if v, ok := pc.Options["editLinkBase"].(string); ok && v != "" {
				out = append(out, "docs.editLinkBase is deprecated; use docs.editLink.base (or docs.editLink.pattern)")
			}
		}
	}
	sort.Strings(out)
	return out
}

// unknownObjectKeys returns the keys of raw that are not recognized fields of t
// (recursing into nested structs and slices of structs), prefixed with path.
func unknownObjectKeys(t reflect.Type, raw map[string]interface{}, path string) []string {
	t = deref(t)
	if t.Kind() != reflect.Struct {
		return nil
	}
	known := structFieldTypes(t)
	var out []string
	for k, v := range raw {
		ft, ok := known[k]
		if !ok {
			out = append(out, path+k)
			continue
		}
		out = append(out, unknownChildKeys(ft, v, path+k+".")...)
	}
	return out
}

func unknownChildKeys(t reflect.Type, v interface{}, path string) []string {
	for t.Kind() == reflect.Ptr || t.Kind() == reflect.Slice || t.Kind() == reflect.Array {
		t = t.Elem()
	}
	if deref(t).Kind() != reflect.Struct {
		return nil
	}
	switch val := v.(type) {
	case map[string]interface{}:
		return unknownObjectKeys(t, val, path)
	case []interface{}:
		var out []string
		for _, item := range val {
			if m, ok := item.(map[string]interface{}); ok {
				out = append(out, unknownObjectKeys(t, m, path)...)
			}
		}
		return out
	default:
		return nil
	}
}

func structFieldTypes(t reflect.Type) map[string]reflect.Type {
	m := make(map[string]reflect.Type, t.NumField())
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		name := jsonFieldName(f)
		if name == "" || name == "-" {
			continue
		}
		m[name] = f.Type
	}
	return m
}

func jsonFieldName(f reflect.StructField) string {
	tag := f.Tag.Get("json")
	if tag == "" {
		return f.Name
	}
	name := strings.SplitN(tag, ",", 2)[0]
	if name == "" {
		return f.Name
	}
	return name
}

func deref(t reflect.Type) reflect.Type {
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	return t
}
