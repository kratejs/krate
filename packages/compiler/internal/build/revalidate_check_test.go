package build

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/diag"
)

func TestStaticRevalidateTargetWarnings(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src", "actions")
	if err := os.MkdirAll(src, 0755); err != nil {
		t.Fatal(err)
	}
	code := `"use server";
import { revalidatePath } from "@krate/runtime/cache";
export async function save() {
  revalidatePath("/about");
  revalidatePath('/blog/hello');
  revalidatePath("/blog", "layout");
  revalidatePath(` + "`/x/${id}`" + `);
}
`
	if err := os.WriteFile(filepath.Join(src, "save.ts"), []byte(code), 0644); err != nil {
		t.Fatal(err)
	}
	// node_modules is never scanned.
	nm := filepath.Join(root, "src", "node_modules", "dep")
	_ = os.MkdirAll(nm, 0755)
	_ = os.WriteFile(filepath.Join(nm, "i.js"), []byte(`revalidatePath("/about")`), 0644)

	pages := []PageMeta{
		{Route: "/about", Mode: RenderSSG},
		{Route: "/blog", Mode: RenderISR},
		{Route: "/blog/[slug]", Mode: RenderISR},
		{Route: "/blog/archive", Mode: RenderSSG},
	}
	ds := staticRevalidateTargetWarnings(filepath.Join(root, "src"), root, pages)
	if len(ds) != 2 {
		for _, d := range ds {
			t.Log(d.Error())
		}
		t.Fatalf("got %d warnings, want 2 (/about and the /blog layout)", len(ds))
	}
	first := ds[0]
	if first.Code != diag.CodeRevalidateStaticTarget || !first.IsWarning() || first.Line != 4 || first.File != "src/actions/save.ts" {
		t.Fatalf("first warning = %+v", first)
	}
	if !strings.Contains(first.Message, "/about") || first.Hint == "" {
		t.Fatalf("first warning text = %s / %s", first.Message, first.Hint)
	}
	if !strings.Contains(ds[1].Message, "/blog/archive") || ds[1].Line != 6 {
		t.Fatalf("layout warning = %+v", ds[1])
	}
}
