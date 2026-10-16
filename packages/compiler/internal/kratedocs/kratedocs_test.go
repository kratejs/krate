package kratedocs

import (
	"strings"
	"testing"
)

func TestEmbeddedDocsPresent(t *testing.T) {
	// A fresh checkout only has .gitkeep until scripts/sync-krate-docs.mjs runs;
	// `pnpm test`/`build` sync first, so a real run must have content.
	if Count() == 0 {
		t.Skip("framework docs not synced (run node scripts/sync-krate-docs.mjs)")
	}
	if Count() < 30 {
		t.Errorf("expected the full framework docs, got %d files", Count())
	}
	for _, slug := range []string{"cli", "configuration", "getting-started", "features/mcp"} {
		if _, ok := Lookup(slug); !ok {
			t.Errorf("missing expected framework doc %q", slug)
		}
	}
}

func TestLookupNormalizes(t *testing.T) {
	if Count() == 0 {
		t.Skip("framework docs not synced")
	}
	for _, in := range []string{"cli", "/cli", "cli.md", "/cli.md/"} {
		d, ok := Lookup(in)
		if !ok || d.Slug != "cli" {
			t.Errorf("Lookup(%q) = %q, %v; want cli", in, d.Slug, ok)
		}
	}
	if _, ok := Lookup("does/not/exist"); ok {
		t.Error("Lookup of a missing slug unexpectedly succeeded")
	}
}

func TestPagesParseTitles(t *testing.T) {
	if Count() == 0 {
		t.Skip("framework docs not synced")
	}
	for _, p := range Pages() {
		if strings.TrimSpace(p.Title) == "" {
			t.Errorf("page %q has no title", p.Path)
		}
	}
}
