package config

import (
	"encoding/json"
	"reflect"
	"strings"
)

// SchemaJSON returns a JSON Schema (draft 2020-12) describing krate.config.*.
// It is generated from the Config struct's JSON tags, so it stays in sync as
// keys are added. Editors can use it via a `$schema` reference.
func SchemaJSON() ([]byte, error) {
	schema := schemaForType(reflect.TypeOf(Config{}), map[reflect.Type]bool{})
	schema["$schema"] = "https://json-schema.org/draft/2020-12/schema"
	schema["title"] = "Krate config"
	return json.MarshalIndent(schema, "", "  ")
}

func schemaForType(t reflect.Type, seen map[reflect.Type]bool) map[string]any {
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	// json.RawMessage and []byte are "any".
	if t.Kind() == reflect.Slice && t.Elem().Kind() == reflect.Uint8 {
		return map[string]any{}
	}
	switch t.Kind() {
	case reflect.String:
		return map[string]any{"type": "string"}
	case reflect.Bool:
		return map[string]any{"type": "boolean"}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return map[string]any{"type": "integer"}
	case reflect.Float32, reflect.Float64:
		return map[string]any{"type": "number"}
	case reflect.Interface:
		return map[string]any{}
	case reflect.Map:
		return map[string]any{
			"type":                 "object",
			"additionalProperties": schemaForType(t.Elem(), seen),
		}
	case reflect.Slice, reflect.Array:
		return map[string]any{
			"type":  "array",
			"items": schemaForType(t.Elem(), seen),
		}
	case reflect.Struct:
		if seen[t] {
			return map[string]any{}
		}
		seen[t] = true
		defer delete(seen, t)
		props := map[string]any{}
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			if !f.IsExported() {
				continue
			}
			name, ok := schemaFieldName(f)
			if !ok {
				continue
			}
			props[name] = schemaForType(f.Type, seen)
		}
		return map[string]any{
			"type":                 "object",
			"properties":           props,
			"additionalProperties": false,
		}
	default:
		return map[string]any{}
	}
}

func schemaFieldName(f reflect.StructField) (string, bool) {
	tag := f.Tag.Get("json")
	if tag == "-" {
		return "", false
	}
	name := strings.SplitN(tag, ",", 2)[0]
	if name == "" {
		name = f.Name
	}
	if name == "-" {
		return "", false
	}
	return name, true
}
