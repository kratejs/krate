package content

import (
	"strings"
	"testing"
)

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
