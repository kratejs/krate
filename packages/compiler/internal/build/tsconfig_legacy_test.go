package build

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

// A 0.4 project ("baseUrl": "./src" with "./src/*" targets) must keep building
// on 0.5, in page code and in generateStaticParams, with one KR7125 warning.
func TestBuildWithZeroFourBaseURLScaffold(t *testing.T) {
	b, root := scProject(t, map[string]string{
		"tsconfig.json":            `{"compilerOptions":{"baseUrl":"./src","paths":{"@/*":["./src/*"]}}}`,
		"src/components/Label.tsx": `export default function Label(props: { text: string }) { return <b>{props.text}</b>; }`,
		"src/lib/ids.ts":           `export const ids = ["one", "two"];`,
		"src/pages/index.tsx":      "import Label from '@/components/Label';\nexport default function Home() { return <main><Label text=\"legacy-ok\" /></main>; }",
		"src/pages/item/[id].tsx": "import { ids } from '@/lib/ids';\nexport function generateStaticParams() { return ids.map((id) => ({ id })); }\n" +
			"export default function Item({ params }: { params: { id: string } }) { return <p>item {params.id}</p>; }",
	})
	config.Warnings = nil
	b.Cfg.LoadTSConfigPaths(root)
	warned := 0
	for _, w := range config.Warnings {
		if strings.Contains(w, "KR7125") {
			warned++
		}
	}
	if warned != 1 {
		t.Fatalf("KR7125 warnings = %d (%q), want 1", warned, config.Warnings)
	}
	if err := b.BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	for file, want := range map[string]string{
		"index.html":          "legacy-ok",
		"item/two/index.html": "item two",
	} {
		data, err := os.ReadFile(filepath.Join(root, "dist", filepath.FromSlash(file)))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(data), want) {
			t.Errorf("%s lacks %q:\n%s", file, want, data)
		}
	}
}
