package build

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

// TestDynamicPageWrapperForwardsParams checks that a default export which
// wraps the page body and forwards its props (`<Post {...props} />`) hands the
// route params on, both for generateStaticParams pages and for the
// placeholder template of a dynamic route.
func TestDynamicPageWrapperForwardsParams(t *testing.T) {
	const gsp = `export function generateStaticParams() { return [{ slug: "a" }]; }` + "\n"
	cases := []struct {
		name string
		src  string
		out  string // output file under dist
		want string
	}{
		{"destructured-body", gsp + `function Post({ params }: any) { return <h1>body-{params.slug}</h1>; }
export default function PageWithHead(props: any) { return <><Post {...props} /></>; }`, "p/a/index.html", "body-a"},
		{"props-body", gsp + `function Post(props: any) { return <h1>body-{props.params.slug}</h1>; }
export default function PageWithHead(props: any) { return <><Post {...props} /></>; }`, "p/a/index.html", "body-a"},
		{"explicit-forward", gsp + `function Post({ params }: any) { return <h1>body-{params.slug}</h1>; }
export default function PageWithHead(props: any) { return <><Post params={props.params} /></>; }`, "p/a/index.html", "body-a"},
		{"template", `function Post({ params }: any) { return <h1>body-{params.slug}</h1>; }
export default function PageWithHead(props: any) { return <><Post {...props} /></>; }`, "p/[slug]/index.html", "body-" + dynamicParamSentinel("slug")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			writeFileRel(t, root, "krate.config.ts", "import { defineConfig } from '@krate/core';\nexport default defineConfig({ pagesDir: 'src/pages', outDir: 'dist' });\n")
			writeFileRel(t, root, "package.json", `{"name":"x","private":true}`)
			writeFileRel(t, root, "src/pages/p/[slug].tsx", tc.src)
			prepareMigratedRuntime(t, root)
			cfg, err := config.Load(root)
			if err != nil {
				t.Fatal(err)
			}
			b := New(root, cfg)
			b.SkipQualityChecks = true
			if err := b.BuildAll(); err != nil {
				t.Fatalf("build: %v", err)
			}
			data, err := os.ReadFile(filepath.Join(root, "dist", filepath.FromSlash(tc.out)))
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(data), tc.want) {
				t.Fatalf("%s: missing %q in:\n%s", tc.out, tc.want, data)
			}
		})
	}
}
