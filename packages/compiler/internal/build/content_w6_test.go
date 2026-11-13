package build

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

func contentConfigFor(root string, schema map[string]any) *config.Config {
	cfg := config.Default()
	cfg.PagesDir = filepath.Join(root, "src", "pages")
	cfg.OutDir = filepath.Join(root, "dist")
	cfg.Content = map[string]any{"blog": map[string]any{"dir": "content/blog", "schema": schema}}
	return cfg
}

func TestUnfoldableCollectionChainDoesNotShipDrafts(t *testing.T) {
	root := t.TempDir()
	writeFileRel(t, root, "src/pages/index.tsx", `import { getCollection } from "krate/content";
import { createSignal } from "@krate/runtime";

export default function Home() {
  const [tag, setTag] = createSignal("a");
  const posts = getCollection("blog").filter((p) => p.data.tag === tag());
  return (
    <main>
      <button onClick={() => setTag("b")}>b</button>
      <ul>{posts.map((p) => <li>{p.data.title}</li>)}</ul>
    </main>
  );
}
`)
	writeFileRel(t, root, "content/blog/hello.md", "---\ntitle: Hello\ntag: a\n---\nPUBLICBODYMARK\n")
	writeFileRel(t, root, "content/blog/secret.md", "---\ntitle: Secret\ntag: b\ndraft: true\n---\nDRAFTSECRETMARK\n")
	cfg := contentConfigFor(root, map[string]any{"title": "string", "tag": "string?", "draft": "boolean?"})
	out, err := captureOutput(func() error { return New(root, cfg).BuildAll() })
	if err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	var js string
	_ = filepath.WalkDir(cfg.OutDir, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(p, ".js") {
			data, _ := os.ReadFile(p)
			js += string(data)
		}
		return nil
	})
	if !strings.Contains(out, "src/pages/index.tsx:6:") || !strings.Contains(out, "KR8810") || !strings.Contains(out, "draft entries included: 1") {
		t.Errorf("expected a located KR8810 warning:\n%s", out)
	}
	for _, bad := range []string{"DRAFTSECRETMARK", "PUBLICBODYMARK"} {
		if strings.Contains(js, bad) {
			t.Errorf("%s shipped to the browser", bad)
		}
	}
}
