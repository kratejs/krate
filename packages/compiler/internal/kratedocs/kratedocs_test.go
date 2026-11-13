package kratedocs

import (
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
