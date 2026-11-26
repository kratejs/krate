package migrate

import (
	"strings"
	"testing"
)

func TestPrivateFoldersMoveAsModulesAndNeverRoutes(t *testing.T) {
	root := gapsProject(t, map[string]string{
		"app/layout.tsx":                         `export default function L({ children }: { children: any }) { return <html><body>{children}</body></html>; }`,
		"app/page.tsx":                           `export default function Home() { return <p>home</p>; }`,
		"app/compare/page.tsx":                   "import Card from \"./_components/Card\";\nimport { fmt } from \"./_lib/format\";\nexport default function Compare() { return <Card title={fmt(\"x\")} />; }\n",
		"app/compare/_components/Card.tsx":       "import { fmt } from \"../_lib/format\";\nexport default function Card({ title }: { title: string }) { return <div>{fmt(title)}</div>; }\n",
		"app/compare/_components/page.tsx":       `export default function NotARoute() { return <p>no</p>; }`,
		"app/compare/_components/layout.tsx":     `export default function NotALayout({ children }: { children: any }) { return <div>{children}</div>; }`,
		"app/compare/_components/route.ts":       `export function GET() { return new Response("no"); }`,
		"app/compare/_lib/format.ts":             `export const fmt = (s: string) => s.toUpperCase();`,
		"app/compare/_lib/deep/inner/sitemap.ts": `export default function sitemap() { return []; }`,
	})
	rep, err := ScanNext(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range rep.Manual {
		if strings.Contains(m.Reason, "private") {
			t.Errorf("unexpected manual item: %+v", m)
		}
	}
	for _, target := range []string{
		"src/migrated/app/compare/_components/Card.tsx",
		"src/migrated/app/compare/_components/page.tsx",
		"src/migrated/app/compare/_components/layout.tsx",
		"src/migrated/app/compare/_components/route.ts",
		"src/migrated/app/compare/_lib/format.ts",
		"src/migrated/app/compare/_lib/deep/inner/sitemap.ts",
	} {
		f := findPlan(rep, target)
		if f == nil || f.Kind != KindAsset {
			t.Errorf("%s not planned as a plain module: %+v", target, f)
		}
	}
	for _, f := range rep.Files {
		if strings.Contains(f.Target, "_components") || strings.Contains(f.Target, "_lib") {
			if !strings.HasPrefix(f.Target, "src/migrated/") {
				t.Errorf("private file became %s (%v)", f.Target, f.Kind)
			}
		}
		if f.Kind == KindAPI || f.Kind == KindMetadataRoute {
			t.Errorf("private file turned into %v: %s", f.Kind, f.Target)
		}
	}
	page := findPlan(rep, "src/pages/compare.tsx")
	if page == nil {
		page = findPlan(rep, "src/pages/compare/index.tsx")
	}
	if page == nil {
		t.Fatal("compare page not planned")
	}
	body := string(page.Content)
	for _, want := range []string{`"../migrated/app/compare/_components/Card"`, `"../migrated/app/compare/_lib/format"`} {
		if !strings.Contains(body, want) {
			t.Errorf("import not rewritten, missing %s:\n%s", want, body)
		}
	}
	if findPlan(rep, "src/pages/compare/_components/index.tsx") != nil {
		t.Error("private folder page became a route")
	}
	// Relative imports between private modules keep resolving in the copy.
	card := string(findPlan(rep, "src/migrated/app/compare/_components/Card.tsx").Content)
	if !strings.Contains(card, `"../_lib/format"`) {
		t.Errorf("card import changed:\n%s", card)
	}
}

func TestPrivateFolderUnderSrcApp(t *testing.T) {
	root := gapsProject(t, map[string]string{
		"src/app/page.tsx":            "import { Hero } from \"./_ui/Hero\";\nexport default function Home() { return <Hero />; }\n",
		"src/app/_ui/Hero.tsx":        `export function Hero() { return <h1>hi</h1>; }`,
		"src/app/_ui/Hero.module.css": `.a { color: red; }`,
	})
	rep, err := ScanNext(root)
	if err != nil {
		t.Fatal(err)
	}
	if f := findPlan(rep, "src/migrated/app/_ui/Hero.tsx"); f == nil || f.Kind != KindAsset {
		t.Fatalf("Hero not planned: %+v", f)
	}
	if f := findPlan(rep, "src/migrated/app/_ui/Hero.module.css"); f == nil {
		t.Error("css module not planned")
	}
	home := string(findPlan(rep, "src/pages/index.tsx").Content)
	if !strings.Contains(home, `"../migrated/app/_ui/Hero"`) {
		t.Errorf("home import not rewritten:\n%s", home)
	}
}
