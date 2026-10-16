package content

import (
	"strings"
	"testing"
)

func TestParseSchemaShorthandAndObject(t *testing.T) {
	raw := map[string]any{
		"title": "string",
		"order": map[string]any{"type": "number", "required": true},
		"draft": map[string]any{"type": "boolean"},
		"tags":  "string[]",
	}
	s := ParseSchema(raw)
	if s["title"].Type != TypeString || s["title"].Required {
		t.Errorf("title = %+v", s["title"])
	}
	if s["order"].Type != TypeNumber || !s["order"].Required {
		t.Errorf("order = %+v", s["order"])
	}
	if s["tags"].Type != TypeStringA {
		t.Errorf("tags = %+v", s["tags"])
	}
}

func TestFieldTypeFromString(t *testing.T) {
	cases := map[string]FieldType{
		"string": TypeString, "number": TypeNumber, "boolean": TypeBoolean,
		"bool": TypeBoolean, "string[]": TypeStringA, "number[]": TypeNumberA,
		"date": TypeString, "weird": TypeAny,
	}
	for in, want := range cases {
		if got := FieldTypeFromString(in); got != want {
			t.Errorf("FieldTypeFromString(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestValidate(t *testing.T) {
	schema := map[string]Field{
		"title": {Type: TypeString, Required: true},
		"order": {Type: TypeNumber},
		"tags":  {Type: TypeStringA},
	}
	ok := map[string]any{"title": "Hi", "order": int64(3), "tags": []any{"a", "b"}}
	if errs := Validate(schema, ok); len(errs) != 0 {
		t.Errorf("expected no errors, got %v", errs)
	}
	bad := map[string]any{"order": "not-a-number", "tags": []any{"a", int64(2)}}
	errs := Validate(schema, bad)
	if len(errs) != 3 { // missing title + wrong order + bad tags
		t.Errorf("expected 3 errors, got %d: %v", len(errs), errs)
	}
}

func TestValidateEmptySchema(t *testing.T) {
	if errs := Validate(nil, map[string]any{"anything": 1}); errs != nil {
		t.Errorf("empty schema must not validate, got %v", errs)
	}
}

func TestGenerateEmpty(t *testing.T) {
	out := Generate(nil, nil)
	if !strings.Contains(out, "export interface ContentTypes {}") {
		t.Errorf("expected empty ContentTypes, got:\n%s", out)
	}
}

func TestGenerateCollections(t *testing.T) {
	cfg := &Config{Collections: map[string]Collection{
		"blog": {
			Dir: "src/content/blog",
			Schema: map[string]Field{
				"title": {Type: TypeString, Required: true},
				"order": {Type: TypeNumber},
			},
		},
		"docs": {
			Dir:    "src/content/docs",
			Schema: map[string]Field{"title": {Type: TypeString}},
		},
	}}
	entries := map[string][]Entry{
		"blog": {{Slug: "hello-world"}, {Slug: "second"}},
	}
	out := Generate(cfg, entries)
	for _, want := range []string{
		"export interface BlogData {",
		"title: string;",
		"order?: number;",
		"export interface BlogEntry {",
		"export interface DocsData {",
		"export interface ContentTypes {",
		"blog: BlogEntry;",
		"docs: DocsEntry;",
		`export type BlogSlug =`,
		`| "hello-world"`,
		`| "second"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("Generate missing %q, got:\n%s", want, out)
		}
	}
}

func TestGenerateNoSchemaPermissive(t *testing.T) {
	cfg := &Config{Collections: map[string]Collection{
		"pages": {Dir: "src/content/pages"},
	}}
	out := Generate(cfg, nil)
	if !strings.Contains(out, "[key: string]: unknown;") {
		t.Errorf("expected permissive index signature, got:\n%s", out)
	}
}

func TestExportedName(t *testing.T) {
	cases := map[string]string{
		"blog": "Blog", "blog-posts": "BlogPosts", "release_notes": "ReleaseNotes",
		"": "Content",
	}
	for in, want := range cases {
		if got := exportedName(in); got != want {
			t.Errorf("exportedName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestPropKeyQuoting(t *testing.T) {
	if got := propKey("title"); got != "title" {
		t.Errorf("propKey(title) = %q", got)
	}
	if got := propKey("kebab-key"); got != `"kebab-key"` {
		t.Errorf("propKey(kebab-key) = %q", got)
	}
}
