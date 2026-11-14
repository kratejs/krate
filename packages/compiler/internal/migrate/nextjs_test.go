package migrate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeNextFile(t *testing.T, root, rel, body string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
}

func findPlan(rep *Report, target string) *PlannedFile {
	for i := range rep.Files {
		if rep.Files[i].Target == target {
			return &rep.Files[i]
		}
	}
	return nil
}

func TestScanNextAppRouter(t *testing.T) {
	root := t.TempDir()
	writeNextFile(t, root, "package.json", `{"dependencies":{"next":"15.0.0"}}`)
	writeNextFile(t, root, "next.config.ts", `export default {
  basePath: "/docs",
  redirects: [{ source: "/old", destination: "/new", permanent: true }],
  rewrites: [{ source: "/legacy/:path*", destination: "/new/:path*" }],
  headers: [{ source: "/assets/*", headers: [{ key: "Cache-Control", value: "public, max-age=60" }] }],
};`)
	writeNextFile(t, root, "app/page.tsx", `"use client";
import Link from "next/link";
import Image from "next/image";
export default function Page() {
  return <main><Link href="/about">About</Link><Image src="/a.png" width={10} height={10} alt="a" /></main>;
}
`)
	writeNextFile(t, root, "app/layout.tsx", `import "./globals.css";
export const metadata = { title: "Example" };
export default function RootLayout({ children }: { children: any }) {
  return <html><body>{children}</body></html>;
}
`)
	writeNextFile(t, root, "app/about/page.tsx", `export default function About() { return <h1>About</h1>; }`)
	writeNextFile(t, root, "app/blog/[slug]/page.tsx", `export function generateStaticParams() { return [{ slug: "a" }]; }
export default function Post({ params }: any) { return <h1>{params.slug}</h1>; }`)
	writeNextFile(t, root, "app/(marketing)/pricing/page.tsx", `export default function Pricing() { return <h1>Pricing</h1>; }`)
	writeNextFile(t, root, "app/api/hello/route.ts", `import { NextRequest, NextResponse } from "next/server";
export async function GET(req: NextRequest) { return NextResponse.json({ ok: true }); }`)
	writeNextFile(t, root, "app/loading.tsx", `export default function Loading() { return <p>Loading</p>; }`)
	writeNextFile(t, root, "app/not-found.tsx", `export default function NotFound() { return <h1>Missing</h1>; }`)
	writeNextFile(t, root, "app/dashboard/error.tsx", `"use client"; export default function Error() { return <p>Error</p>; }`)

	rep, err := ScanNext(root)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"src/pages/index.tsx",
		"src/pages/_layout.tsx",
		"src/pages/about.tsx",
		"src/pages/blog/[slug].tsx",
		"src/pages/(marketing)/pricing.tsx",
		"src/api/hello.ts",
		"src/pages/loading.tsx",
		"src/pages/404.tsx",
		"krate.config.ts",
	}
	for _, target := range want {
		if findPlan(rep, target) == nil {
			t.Errorf("missing target %s", target)
		}
	}
	if plan := findPlan(rep, "src/pages/dashboard/error.tsx"); plan == nil || plan.Kind != KindError {
		t.Errorf("error.tsx should become the segment error page: %+v", plan)
	}

	page := findPlan(rep, "src/pages/index.tsx")
	if page == nil {
		t.Fatal("page missing")
	}
	if !strings.Contains(string(page.Content), "use client") {
		t.Errorf("use client directive must be kept:\n%s", page.Content)
	}
	if strings.Contains(string(page.Content), "next/link") || strings.Contains(string(page.Content), "next/image") {
		t.Errorf("Next globals import not removed:\n%s", page.Content)
	}
	if !strings.Contains(string(page.Content), "onClick") && !strings.Contains(string(page.Content), "<main>") {
		t.Errorf("page body lost:\n%s", page.Content)
	}

	layout := findPlan(rep, "src/pages/_layout.tsx")
	if layout == nil {
		t.Fatal("layout missing")
	}
	if strings.Contains(string(layout.Content), "<html") || strings.Contains(string(layout.Content), "<body") {
		t.Errorf("document shell not stripped:\n%s", layout.Content)
	}
	if hasManual(rep, "app/api/hello/route.ts", "NextRequest") {
		t.Errorf("standard NextRequest should be rewritten, not manual: %+v", rep.Manual)
	}
	if !hasWarning(rep, "translated static next.config") {
		t.Errorf("next.config translation warning missing: %+v", rep.Warnings)
	}
	if hasManual(rep, "app/layout.tsx", "metadata") {
		t.Errorf("layout metadata runs natively, it is not manual work: %+v", rep.Manual)
	}
	if layoutPlan := findPlan(rep, "src/pages/_layout.tsx"); layoutPlan == nil || !strings.Contains(string(layoutPlan.Content), "export const metadata") {
		t.Errorf("the layout keeps its metadata export: %+v", layoutPlan)
	}
	cfg := findPlan(rep, "krate.config.ts")
	if cfg == nil || !strings.Contains(string(cfg.Content), `"entry": "src/pages/index.tsx"`) {
		t.Fatalf("generated config = %+v", cfg)
	}
	for _, want := range []string{
		`"basePath": "/docs"`,
		`"source": "/old"`,
		`"destination": "/new"`,
		`"Cache-Control": "public, max-age=60"`,
		`"rewrites":`,
	} {
		if !strings.Contains(string(cfg.Content), want) {
			t.Errorf("config missing %s:\n%s", want, cfg.Content)
		}
	}
}

func TestApplyAndRollback(t *testing.T) {
	root := t.TempDir()
	writeNextFile(t, root, "package.json", `{"dependencies":{"next":"15.0.0"}}`)
	writeNextFile(t, root, "app/page.tsx", `export default function Page() { return <h1>Home</h1>; }`)
	rep, err := ScanNext(root)
	if err != nil {
		t.Fatal(err)
	}
	res, err := Apply(rep, ApplyOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "src", "pages", "index.tsx")); err != nil {
		t.Fatalf("page not written: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "krate.config.ts")); err != nil {
		t.Fatalf("config not written: %v", err)
	}
	if _, err := os.Stat(filepath.Join(res.BackupDir, "manifest.json")); err != nil {
		t.Fatalf("manifest not written: %v", err)
	}
	if err := Rollback(res.BackupDir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "src", "pages", "index.tsx")); !os.IsNotExist(err) {
		t.Fatalf("rollback should remove created page, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "krate.config.ts")); !os.IsNotExist(err) {
		t.Fatalf("rollback should remove created config, got %v", err)
	}
}

func TestNestedNotFoundBecomesSegmentPage(t *testing.T) {
	root := t.TempDir()
	writeNextFile(t, root, "app/page.tsx", `export default function Page() { return <h1>Home</h1>; }`)
	writeNextFile(t, root, "app/not-found.tsx", `export default function NF() { return <h1>root</h1>; }`)
	writeNextFile(t, root, "app/blog/not-found.tsx", `export default function NF() { return <h1>blog</h1>; }`)
	writeNextFile(t, root, "app/(shop)/cart/not-found.tsx", `export default function NF() { return <h1>cart</h1>; }`)
	writeNextFile(t, root, "app/@modal/not-found.tsx", `export default function NF() { return <h1>modal</h1>; }`)
	rep, err := ScanNext(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{"src/pages/404.tsx", "src/pages/blog/404.tsx", "src/pages/(shop)/cart/404.tsx"} {
		if plan := findPlan(rep, target); plan == nil || plan.Kind != KindNotFound {
			t.Errorf("missing %s: %+v", target, rep.Files)
		}
	}
	var slotItem bool
	for _, m := range rep.Manual {
		if strings.Contains(m.Source, "@modal") && strings.Contains(m.Reason, "slot") {
			slotItem = true
		}
		if strings.Contains(m.Reason, "nested not-found") {
			t.Errorf("nested not-found must be native: %+v", m)
		}
	}
	if !slotItem {
		t.Errorf("a not-found inside a slot should be a manual item: %+v", rep.Manual)
	}
}

func TestTargetPathRejectsEscape(t *testing.T) {
	if _, err := targetPath(t.TempDir(), "../escape.tsx"); err == nil {
		t.Fatal("expected path escape error")
	}
}

func hasWarning(rep *Report, substring string) bool {
	for _, w := range rep.Warnings {
		if strings.Contains(w, substring) {
			return true
		}
	}
	return false
}

func hasManual(rep *Report, source, substring string) bool {
	for _, m := range rep.Manual {
		if m.Source == source && strings.Contains(m.Reason, substring) {
			return true
		}
	}
	return false
}
