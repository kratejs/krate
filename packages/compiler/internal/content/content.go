// Package content implements typed content collections: a declarative schema
// (authored in `content.config.ts` via `defineContent`) is validated against
// the frontmatter of markdown/mdx entries, and TypeScript declarations are
// generated so content is fully typed.
// The package is pure (no filesystem access) for testability; the build
// pipeline wires config loading, directory walking, and file writing.
package content

import (
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// FieldType is a supported frontmatter field type.
type FieldType string

const (
	TypeString  FieldType = "string"
	TypeNumber  FieldType = "number"
	TypeBoolean FieldType = "boolean"
	TypeStringA FieldType = "string[]"
	TypeNumberA FieldType = "number[]"
	TypeAny     FieldType = "any"
)

// Field describes one schema field.
type Field struct {
	Type     FieldType
	Required bool
}

// Collection describes one content collection.
type Collection struct {
	// Dir is the collection's directory, relative to the project root.
	Dir string `json:"dir"`
	// Schema maps field names to fields. A nil/empty schema means "no
	// validation" - entries are still typed with a permissive data shape.
	Schema map[string]Field `json:"-"`
}

// Config is the parsed content configuration.
type Config struct {
	Collections map[string]Collection
}

// ParseConfig builds a Config from a raw `content` config value (the object
// shape authored in `krate.config.ts` via `defineContent({...})` or a plain
// object). Each key is a collection name mapping to `{ dir, schema }`.
// Unknown/malformed collections are skipped rather than failing the build so a
// type error surfaces via the schema instead.
func ParseConfig(raw map[string]any) *Config {
	if raw == nil {
		return nil
	}
	cfg := &Config{Collections: map[string]Collection{}}
	for name, v := range raw {
		colObj, ok := v.(map[string]any)
		if !ok {
			continue
		}
		col := Collection{}
		if dir, ok := colObj["dir"].(string); ok {
			col.Dir = dir
		}
		if schema, ok := colObj["schema"]; ok {
			col.Schema = ParseSchema(schema)
		}
		if col.Dir == "" {
			col.Dir = filepath.ToSlash(filepath.Join("src", "content", name))
		}
		cfg.Collections[name] = col
	}
	return cfg
}

// Entry is one content document discovered on disk.
type Entry struct {
	// Slug is the collection-relative path without extension, slash-separated
	// (e.g. "guides/advanced").
	Slug string
	// Path is the project-relative source path.
	Path string
	// Data is the parsed frontmatter.
	Data map[string]any
	// Body is the raw markdown source (frontmatter stripped).
	Body string
	// HTML is the rendered markdown body.
	HTML string
}

// collectionNames returns the collection names in deterministic order.
func (c *Config) collectionNames() []string {
	names := make([]string, 0, len(c.Collections))
	for name := range c.Collections {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// FieldTypeFromString normalizes a shorthand type name to a FieldType. Unknown
// types become TypeAny so an unrecognized schema never blocks a build.
func FieldTypeFromString(s string) FieldType {
	switch strings.TrimSpace(s) {
	case "string":
		return TypeString
	case "number":
		return TypeNumber
	case "boolean", "bool":
		return TypeBoolean
	case "string[]":
		return TypeStringA
	case "number[]":
		return TypeNumberA
	case "date":
		// Dates are authored as ISO strings in frontmatter.
		return TypeString
	default:
		return TypeAny
	}
}

// Validate checks an entry's frontmatter against a collection schema. It
// returns one error per violation, in field-name order.
func Validate(schema map[string]Field, data map[string]any) []error {
	if len(schema) == 0 {
		return nil
	}
	names := make([]string, 0, len(schema))
	for name := range schema {
		names = append(names, name)
	}
	sort.Strings(names)

	var errs []error
	for _, name := range names {
		field := schema[name]
		raw, present := data[name]
		if !present || raw == nil {
			if field.Required {
				errs = append(errs, fmt.Errorf("missing required field %q", name))
			}
			continue
		}
		if !valueMatches(field.Type, raw) {
			errs = append(errs, fmt.Errorf("field %q must be %s (got %s)", name, field.Type, jsTypeName(raw)))
		}
	}
	return errs
}

// valueMatches reports whether a frontmatter value satisfies a field type.
func valueMatches(t FieldType, v any) bool {
	switch t {
	case TypeAny:
		return true
	case TypeString:
		_, ok := v.(string)
		return ok
	case TypeNumber:
		switch v.(type) {
		case int64, float64, int:
			return true
		}
		return false
	case TypeBoolean:
		_, ok := v.(bool)
		return ok
	case TypeStringA:
		list, ok := v.([]any)
		if !ok {
			if ss, ok := v.([]string); ok {
				_ = ss
				return true
			}
			return false
		}
		for _, item := range list {
			if _, ok := item.(string); !ok {
				return false
			}
		}
		return true
	case TypeNumberA:
		list, ok := v.([]any)
		if !ok {
			return false
		}
		for _, item := range list {
			switch item.(type) {
			case int64, float64, int:
			default:
				return false
			}
		}
		return true
	}
	return true
}

func jsTypeName(v any) string {
	switch v.(type) {
	case string:
		return "string"
	case int64, float64, int:
		return "number"
	case bool:
		return "boolean"
	case []any, []string:
		return "array"
	default:
		return fmt.Sprintf("%T", v)
	}
}

// Generate renders `.krate/types/content.d.ts` from the config and discovered
// entries. Entry values themselves are not baked in (they may be large); only
// the collection/entry shapes and the schema-derived data interfaces are typed.
func Generate(cfg *Config, entries map[string][]Entry) string {
	var b strings.Builder
	b.WriteString("// AUTO-GENERATED by Krate — do not edit.\n")
	b.WriteString("// Regenerate with `krate build` (or `krate types`).\n\n")

	if cfg == nil || len(cfg.Collections) == 0 {
		b.WriteString("export interface ContentTypes {}\n")
		return b.String()
	}

	names := cfg.collectionNames()
	for _, name := range names {
		col := cfg.Collections[name]
		iface := exportedName(name)
		b.WriteString("export interface " + iface + "Data {\n")
		writeFields(&b, col.Schema)
		b.WriteString("}\n\n")
		b.WriteString("export interface " + iface + "Entry {\n")
		b.WriteString("  slug: string;\n")
		b.WriteString("  path: string;\n")
		b.WriteString("  body: string;\n")
		b.WriteString("  html: string;\n")
		b.WriteString("  data: " + iface + "Data;\n")
		b.WriteString("}\n\n")
	}

	b.WriteString("export interface ContentTypes {\n")
	for _, name := range names {
		b.WriteString("  " + propKey(name) + ": " + exportedName(name) + "Entry;\n")
	}
	b.WriteString("}\n\n")

	// A per-collection entry-slug union gives autocomplete for getCollection().
	for _, name := range names {
		entries := entries[name]
		b.WriteString("export type " + exportedName(name) + "Slug =\n")
		if len(entries) == 0 {
			b.WriteString("  string;\n\n")
			continue
		}
		sorted := make([]Entry, len(entries))
		copy(sorted, entries)
		sort.Slice(sorted, func(i, j int) bool { return sorted[i].Slug < sorted[j].Slug })
		for _, e := range sorted {
			b.WriteString("  | " + tsString(e.Slug) + "\n")
		}
		b.WriteString("  ;\n\n")
	}

	return b.String()
}

// GenerateModule renders the runtime module resolved for `import ... from
// "krate/content"`. It bakes every collection's entries (slug, path, body,
// rendered html, frontmatter data) as literals plus a typed `getCollection`.
// Because the entries are compile-time constants, `getCollection("blog")`
// folds into static HTML during SSR.
// The signature returns a JavaScript module body; it is emitted with a `.ts`
// extension so the bundler parses and folds it like any other source.
func GenerateModule(cfg *Config, entries map[string][]Entry) string {
	var b strings.Builder
	b.WriteString("// AUTO-GENERATED by Krate — do not edit.\n")
	b.WriteString("// Build-time content collections. `getCollection(name)` folds to a\n")
	b.WriteString("// literal array during SSR, so no runtime work is required.\n\n")

	if cfg == nil || len(cfg.Collections) == 0 {
		b.WriteString("export function getCollection(name) { return []; }\n")
		return b.String()
	}

	names := cfg.collectionNames()
	for _, name := range names {
		b.WriteString("const " + constName(name) + " = " + CollectionJS(entries[name]) + ";\n\n")
	}

	b.WriteString("const collections = {\n")
	for _, name := range names {
		b.WriteString("  " + propKey(name) + ": " + constName(name) + ",\n")
	}
	b.WriteString("};\n\n")

	b.WriteString("export function getCollection(name) {\n")
	b.WriteString("  return collections[name] || [];\n")
	b.WriteString("}\n\n")

	// Named exports so `import { blog } from "krate/content"` also works.
	for _, name := range names {
		b.WriteString("export { " + constName(name) + " };\n")
	}

	return b.String()
}

// CollectionJS renders a collection's entries as a JavaScript array literal in
// a stable (slug-sorted) order. Used both by the generated `krate/content`
// module and by build-time AST inlining of `getCollection("...")`.
func CollectionJS(entries []Entry) string {
	sorted := make([]Entry, len(entries))
	copy(sorted, entries)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Slug < sorted[j].Slug })
	var b strings.Builder
	b.WriteString("[\n")
	for _, e := range sorted {
		b.WriteString("  {")
		b.WriteString(" slug: " + tsString(e.Slug) + ",")
		b.WriteString(" path: " + tsString(e.Path) + ",")
		b.WriteString(" body: " + tsString(e.Body) + ",")
		b.WriteString(" html: " + tsString(e.HTML) + ",")
		b.WriteString(" data: " + goValueToJS(e.Data) + ",")
		b.WriteString(" },\n")
	}
	b.WriteString("]")
	return b.String()
}

// GenerateModuleDTS renders a self-contained ambient declaration file for the
// `krate/content` module. It is written as a script (no top-level imports or
// exports) so `declare module` is a true ambient module declaration, letting
// editors type-check `import { getCollection } from "krate/content"` before a
// build. The entry interfaces are inlined so the file needs no other module.
func GenerateModuleDTS(cfg *Config) string {
	var b strings.Builder
	b.WriteString("// AUTO-GENERATED by Krate — do not edit.\n")
	b.WriteString("// Ambient types for the codegen'd `krate/content` module.\n\n")

	names := []string{}
	if cfg != nil {
		names = cfg.collectionNames()
	}

	// Inline the per-collection Data interfaces inside the module block.
	var ifaces strings.Builder
	var entryNames []string
	for _, name := range names {
		col := cfg.Collections[name]
		iface := exportedName(name)
		ifaces.WriteString("  export interface " + iface + "Data {\n")
		writeFields(&ifaces, col.Schema)
		ifaces.WriteString("  }\n")
		ifaces.WriteString("  export interface " + iface + "Entry {\n")
		ifaces.WriteString("    slug: string;\n    path: string;\n    body: string;\n    html: string;\n")
		ifaces.WriteString("    data: " + iface + "Data;\n  }\n")
		entryNames = append(entryNames, iface+"Entry")
	}

	b.WriteString("declare module \"krate/content\" {\n")
	b.WriteString(ifaces.String())
	if len(names) > 0 {
		var union []string
		for _, n := range names {
			union = append(union, tsString(n))
		}
		b.WriteString("  export function getCollection(name: " + strings.Join(union, " | ") + "): (" + strings.Join(entryNames, " | ") + ")[];\n")
		for i, n := range names {
			b.WriteString("  export const " + constName(n) + ": " + entryNames[i] + "[];\n")
		}
	} else {
		b.WriteString("  export function getCollection(name: string): unknown[];\n")
	}
	b.WriteString("}\n\n")

	// Alias so both specifiers resolve.
	b.WriteString("declare module \"@krate/content\" {\n")
	b.WriteString("  export * from \"krate/content\";\n")
	b.WriteString("}\n")
	return b.String()
}

// constName converts a collection name to a valid JS identifier.

// constName converts a collection name to a valid JS identifier.
func constName(name string) string {
	parts := splitIdent(name)
	if len(parts) == 0 {
		return "collection"
	}
	var b strings.Builder
	b.WriteString(parts[0])
	for _, p := range parts[1:] {
		if p == "" {
			continue
		}
		b.WriteString(strings.ToUpper(p[:1]) + p[1:])
	}
	out := b.String()
	if out == "" || (out[0] >= '0' && out[0] <= '9') {
		return "_" + out
	}
	return out
}

// ValueToJS renders a decoded frontmatter value as a JavaScript literal.
func ValueToJS(v any) string { return goValueToJS(v) }

// goValueToJS renders a decoded frontmatter value as a JavaScript literal.
func goValueToJS(v any) string {
	switch t := v.(type) {
	case nil:
		return "null"
	case string:
		return tsString(t)
	case bool:
		if t {
			return "true"
		}
		return "false"
	case int:
		return fmt.Sprintf("%d", t)
	case int64:
		return fmt.Sprintf("%d", t)
	case float64:
		return trimNumber(t)
	case []any:
		parts := make([]string, len(t))
		for i, item := range t {
			parts[i] = goValueToJS(item)
		}
		return "[" + strings.Join(parts, ", ") + "]"
	case []string:
		parts := make([]string, len(t))
		for i, item := range t {
			parts[i] = tsString(item)
		}
		return "[" + strings.Join(parts, ", ") + "]"
	case map[string]any:
		names := make([]string, 0, len(t))
		for k := range t {
			names = append(names, k)
		}
		sort.Strings(names)
		parts := make([]string, 0, len(names))
		for _, k := range names {
			parts = append(parts, propKey(k)+": "+goValueToJS(t[k]))
		}
		return "{" + strings.Join(parts, ", ") + "}"
	default:
		return tsString(fmt.Sprintf("%v", t))
	}
}

func trimNumber(f float64) string {
	if f == float64(int64(f)) {
		return fmt.Sprintf("%d", int64(f))
	}
	return strconv.FormatFloat(f, 'f', -1, 64)
}

func writeFields(b *strings.Builder, schema map[string]Field) {
	if len(schema) == 0 {
		b.WriteString("  [key: string]: unknown;\n")
		return
	}
	names := make([]string, 0, len(schema))
	for name := range schema {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		field := schema[name]
		opt := "?"
		if field.Required {
			opt = ""
		}
		b.WriteString("  " + propKey(name) + opt + ": " + tsType(field.Type) + ";\n")
	}
}

// tsType maps a FieldType to its TypeScript type.
func tsType(t FieldType) string {
	switch t {
	case TypeString:
		return "string"
	case TypeNumber:
		return "number"
	case TypeBoolean:
		return "boolean"
	case TypeStringA:
		return "string[]"
	case TypeNumberA:
		return "number[]"
	default:
		return "unknown"
	}
}

// exportedName converts a collection name to a PascalCase identifier.
func exportedName(name string) string {
	parts := splitIdent(name)
	var b strings.Builder
	for _, p := range parts {
		if p == "" {
			continue
		}
		b.WriteString(strings.ToUpper(p[:1]) + p[1:])
	}
	if b.Len() == 0 {
		return "Content"
	}
	return b.String()
}

func splitIdent(name string) []string {
	return strings.FieldsFunc(name, func(r rune) bool {
		return r == '-' || r == '_' || r == ' ' || r == '.'
	})
}

// propKey quotes a property name when it is not a safe identifier.
func propKey(name string) string {
	if isIdentifier(name) {
		return name
	}
	return tsString(name)
}

func isIdentifier(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		if r == '_' || r == '$' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
			continue
		}
		if i > 0 && r >= '0' && r <= '9' {
			continue
		}
		return false
	}
	return true
}

func tsString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '\\':
			b.WriteString(`\\`)
		case '"':
			b.WriteString(`\"`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// ParseSchema converts the JSON-decoded schema value for one collection into a
// Field map. It accepts both shorthand strings ("string") and object form
// ({"type":"string","required":true}). required defaults to false; a field is
// also treated as required when `optional: false` is set.
func ParseSchema(raw any) map[string]Field {
	obj, ok := raw.(map[string]any)
	if !ok {
		return nil
	}
	out := make(map[string]Field, len(obj))
	for name, v := range obj {
		switch t := v.(type) {
		case string:
			out[name] = Field{Type: FieldTypeFromString(t)}
		case map[string]any:
			f := Field{Type: TypeAny}
			if ts, ok := t["type"].(string); ok {
				f.Type = FieldTypeFromString(ts)
			}
			if req, ok := t["required"].(bool); ok {
				f.Required = req
			}
			if opt, ok := t["optional"].(bool); ok && !opt {
				f.Required = true
			}
			out[name] = f
		default:
			out[name] = Field{Type: TypeAny}
		}
	}
	return out
}

// NormalizeData coerces a raw frontmatter map (from the parser) into the
// value shapes Validate expects. The parser already yields string/int64/
// float64/bool/[]any, so this is a light copy that avoids mutating the input.
func NormalizeData(data map[string]any) map[string]any {
	if data == nil {
		return nil
	}
	out := make(map[string]any, len(data))
	for k, v := range data {
		out[k] = v
	}
	return out
}
