package migrate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/lexer"
	"github.com/kratejs/krate/packages/compiler/internal/parser"
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
		"src/pages/pricing.tsx",
		"src/api/hello.ts",
		"src/pages/loading.tsx",
		"src/pages/404.tsx",
		"krate.config.ts",
	}
	for _, target := range want {
		if findPlan(rep, target) == nil {
			t.Errorf("missing target %s; got %+v", target, rep.Files)
		}
	}
	if findPlan(rep, "src/pages/dashboard/error.tsx") != nil {
		t.Error("error.tsx must not be copied as a page")
	}

	page := findPlan(rep, "src/pages/index.tsx")
	if page == nil {
		t.Fatal("index page missing")
	}
	if strings.Contains(string(page.Content), "use client") {
		t.Errorf("use client not removed:\n%s", page.Content)
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
	if !hasManual(rep, "app/api/hello/route.ts", "NextRequest") {
		t.Errorf("NextRequest manual item missing: %+v", rep.Manual)
	}
	if !hasWarning(rep, "translated static next.config") {
		t.Errorf("next.config translation warning missing: %+v", rep.Warnings)
	}
	if !hasWarning(rep, "mapped static root metadata") {
		t.Errorf("metadata warning missing: %+v", rep.Warnings)
	}
	if hasManual(rep, "app/layout.tsx", "metadata export is not translated") {
		t.Errorf("metadata should be mapped, not manual: %+v", rep.Manual)
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
		`"site": {`,
		`"name": "Example"`,
	} {
		if !strings.Contains(string(cfg.Content), want) {
			t.Errorf("config missing %s:\n%s", want, cfg.Content)
		}
	}
}

func TestNextConfigComputedStaysManual(t *testing.T) {
	root := t.TempDir()
	writeNextFile(t, root, "app/page.tsx", `export default function Page() { return <h1>Home</h1>; }`)
	writeNextFile(t, root, "next.config.js", `module.exports = makeConfig();`)
	rep, err := ScanNext(root)
	if err != nil {
		t.Fatal(err)
	}
	if !hasManual(rep, "next.config.js", "could not be proven static") {
		t.Fatalf("expected manual next config: %+v", rep.Manual)
	}
}

func TestNextConfigImages(t *testing.T) {
	root := t.TempDir()
	writeNextFile(t, root, "app/page.tsx", `export default function Page() { return <h1>Home</h1>; }`)
	writeNextFile(t, root, "next.config.js", `export default {
  images: {
    deviceSizes: [640, 1080],
    formats: ["image/avif", "image/webp"],
    qualities: [80],
    unoptimized: false,
    remotePatterns: [{ protocol: "https", hostname: "images.example.com", pathname: "/assets/**" }],
  },
  env: { NEXT_PUBLIC_X: "1" },
};`)
	rep, err := ScanNext(root)
	if err != nil {
		t.Fatal(err)
	}
	cfg := findPlan(rep, "krate.config.ts")
	if cfg == nil {
		t.Fatal("config missing")
	}
	body := string(cfg.Content)
	for _, want := range []string{`"breakpoints":`, "640", "1080", `"formats":`, `"webp"`, `"quality": 80`, `"images.example.com"`, `"pathname": "/assets/**"`} {
		if !strings.Contains(body, want) {
			t.Errorf("config missing %q:\n%s", want, body)
		}
	}
	if !hasWarning(rep, "AVIF") || !hasWarning(rep, "env is not translated") {
		t.Errorf("image/env warnings missing: %+v", rep.Warnings)
	}
}

func TestNextConfigFunctionAndLocalObject(t *testing.T) {
	for name, src := range map[string]string{
		"function": `module.exports = () => ({ redirects: [{ source: "/a", destination: "/b" }] });`,
		"local":    `const nextConfig = { redirects: [{ source: "/a", destination: "/b" }] }; export default nextConfig;`,
	} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			writeNextFile(t, root, "app/page.tsx", `export default function Page() { return <h1>Home</h1>; }`)
			writeNextFile(t, root, "next.config.js", src)
			rep, err := ScanNext(root)
			if err != nil {
				t.Fatal(err)
			}
			cfg := findPlan(rep, "krate.config.ts")
			if cfg == nil || !strings.Contains(string(cfg.Content), `"source": "/a"`) {
				t.Fatalf("config not translated: %+v", cfg)
			}
		})
	}
}

func TestNextConfigModuleExports(t *testing.T) {
	root := t.TempDir()
	writeNextFile(t, root, "app/page.tsx", `export default function Page() { return <h1>Home</h1>; }`)
	writeNextFile(t, root, "next.config.js", `module.exports = { redirects: [{ source: "/a", destination: "/b", statusCode: 301 }] };`)
	rep, err := ScanNext(root)
	if err != nil {
		t.Fatal(err)
	}
	cfg := findPlan(rep, "krate.config.ts")
	if cfg == nil || !strings.Contains(string(cfg.Content), `"source": "/a"`) || !strings.Contains(string(cfg.Content), `"permanent": true`) {
		t.Fatalf("module.exports config not translated: %+v", cfg)
	}
}

func TestScanNextPagesRouter(t *testing.T) {
	root := t.TempDir()
	writeNextFile(t, root, "package.json", `{"dependencies":{"next":"14.0.0"}}`)
	writeNextFile(t, root, "pages/index.tsx", `import Link from "next/link"; export default function Index() { return <Link href="/about">About</Link>; }`)
	writeNextFile(t, root, "pages/about.tsx", `export default function About() { return <h1>About</h1>; }`)
	writeNextFile(t, root, "pages/api/hello.ts", `export default function handler(req: any, res: any) { res.end("ok"); }`)
	writeNextFile(t, root, "pages/_app.tsx", `export default function App({ Component, pageProps }: any) { return <Component {...pageProps} />; }`)
	writeNextFile(t, root, "pages/404.tsx", `export default function Missing() { return <h1>Missing</h1>; }`)

	rep, err := ScanNext(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{"src/pages/index.tsx", "src/pages/about.tsx", "src/api/hello.ts", "src/pages/404.tsx"} {
		if findPlan(rep, target) == nil {
			t.Errorf("missing target %s; got %+v", target, rep.Files)
		}
	}
	if findPlan(rep, "src/migrated/pages/_app.tsx") == nil {
		t.Errorf("_app asset missing: %+v", rep.Files)
	}
	layout := findPlan(rep, "src/pages/_layout.tsx")
	if layout == nil || !strings.Contains(string(layout.Content), "AppShell") {
		t.Errorf("_app layout wrapper missing: %+v", layout)
	}
	page := findPlan(rep, "src/pages/index.tsx")
	if page == nil || strings.Contains(string(page.Content), "next/link") {
		t.Fatalf("page rewrite failed: %+v", page)
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

func TestApplyRefusesExistingWithoutForceAndRestoresWithRollback(t *testing.T) {
	root := t.TempDir()
	writeNextFile(t, root, "package.json", `{"dependencies":{"next":"15.0.0"}}`)
	writeNextFile(t, root, "app/page.tsx", `export default function Page() { return <h1>New</h1>; }`)
	writeNextFile(t, root, "src/pages/index.tsx", `original`)
	rep, err := ScanNext(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Apply(rep, ApplyOptions{}); err == nil || !strings.Contains(err.Error(), "refusing to overwrite") {
		t.Fatalf("expected conflict error, got %v", err)
	}
	got, _ := os.ReadFile(filepath.Join(root, "src", "pages", "index.tsx"))
	if string(got) != "original" {
		t.Fatalf("conflict wrote the target: %s", got)
	}
	res, err := Apply(rep, ApplyOptions{Force: true})
	if err != nil {
		t.Fatal(err)
	}
	got, _ = os.ReadFile(filepath.Join(root, "src", "pages", "index.tsx"))
	if !strings.Contains(string(got), "New") {
		t.Fatalf("force did not write: %s", got)
	}
	if err := Rollback(res.BackupDir); err != nil {
		t.Fatal(err)
	}
	got, _ = os.ReadFile(filepath.Join(root, "src", "pages", "index.tsx"))
	if string(got) != "original" {
		t.Fatalf("rollback did not restore original: %s", got)
	}
}

func TestNestedLoadingMapsToPageDirectory(t *testing.T) {
	root := t.TempDir()
	writeNextFile(t, root, "app/page.tsx", `export default function Page() { return <h1>Home</h1>; }`)
	writeNextFile(t, root, "app/loading.tsx", `export default function Loading() { return <p>root</p>; }`)
	writeNextFile(t, root, "app/blog/loading.tsx", `export default function BlogLoading() { return <p>blog</p>; }`)
	rep, err := ScanNext(root)
	if err != nil {
		t.Fatal(err)
	}
	if findPlan(rep, "src/pages/loading.tsx") == nil {
		t.Errorf("root loading target missing: %+v", rep.Files)
	}
	if findPlan(rep, "src/pages/blog/loading.tsx") == nil {
		t.Errorf("nested loading target missing: %+v", rep.Files)
	}
}

func TestScanNextSrcApp(t *testing.T) {
	root := t.TempDir()
	writeNextFile(t, root, "src/app/page.tsx", `export default function Page() { return <h1>Home</h1>; }`)
	rep, err := ScanNext(root)
	if err != nil {
		t.Fatal(err)
	}
	if findPlan(rep, "src/pages/index.tsx") == nil {
		t.Fatalf("src/app page not planned: %+v", rep.Files)
	}
}

func TestAppRelativeImportsAreCopiedAndRewritten(t *testing.T) {
	root := t.TempDir()
	writeNextFile(t, root, "app/page.tsx", `import { Hero } from "./components/Hero";
import "./styles.css";
export default function Page() { return <Hero />; }`)
	writeNextFile(t, root, "app/components/Hero.tsx", `import { inner } from "./inner"; export function Hero() { return <h1>{inner}</h1>; }`)
	writeNextFile(t, root, "app/components/inner.ts", `export const inner = "hi";`)
	writeNextFile(t, root, "app/styles.css", `.hero { color: red; }`)
	rep, err := ScanNext(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{
		"src/migrated/app/components/Hero.tsx",
		"src/migrated/app/components/inner.ts",
		"src/migrated/app/styles.css",
	} {
		if findPlan(rep, target) == nil {
			t.Errorf("missing copied asset %s: %+v", target, rep.Files)
		}
	}
	page := findPlan(rep, "src/pages/index.tsx")
	if page == nil {
		t.Fatal("page missing")
	}
	body := string(page.Content)
	if !strings.Contains(body, `from "../migrated/app/components/Hero"`) {
		t.Errorf("component import not rewritten:\n%s", body)
	}
	if !strings.Contains(body, `"../migrated/app/styles.css"`) {
		t.Errorf("css import not rewritten:\n%s", body)
	}
	if strings.Contains(string(findPlan(rep, "src/migrated/app/components/Hero.tsx").Content), `"../migrated/app/components/inner"`) {
		t.Error("asset-to-asset import should keep its original relative path")
	}
}

func TestUnresolvedRelativeImportIsManual(t *testing.T) {
	root := t.TempDir()
	writeNextFile(t, root, "app/page.tsx", `import { x } from "./missing"; export default function Page() { return <h1>{x}</h1>; }`)
	rep, err := ScanNext(root)
	if err != nil {
		t.Fatal(err)
	}
	if !hasManual(rep, "app/page.tsx", "./missing") {
		t.Fatalf("missing unresolved import manual: %+v", rep.Manual)
	}
}

func TestStaticPageMetadataBecomesHead(t *testing.T) {
	root := t.TempDir()
	writeNextFile(t, root, "app/page.tsx", `export const metadata = {
  title: "About",
  description: "About page",
  openGraph: { images: ["/og.png"] },
};
export default function Page(props: { params: any }) { return <h1>About</h1>; }`)
	rep, err := ScanNext(root)
	if err != nil {
		t.Fatal(err)
	}
	page := findPlan(rep, "src/pages/index.tsx")
	if page == nil {
		t.Fatal("page missing")
	}
	body := string(page.Content)
	for _, want := range []string{
		"function Page(props: { params: any })",
		"<Head>",
		"<title>About</title>",
		`<meta name="description" content="About page" />`,
		`<meta property="og:image" content="/og.png" />`,
		"export default function PageWithHead(props: any)",
		"<Page {...props} />",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("page missing %q:\n%s", want, body)
		}
	}
	p := parser.New(lexer.New(body).Tokenize())
	if prog := p.ParseProgram(); prog == nil || len(p.Errors()) > 0 {
		t.Fatalf("transformed page does not parse: %v\n%s", p.Errors(), body)
	}
	if hasManual(rep, "app/page.tsx", "metadata export is not translated") {
		t.Errorf("metadata should be wrapped, not manual: %+v", rep.Manual)
	}
}

func TestStaticGenerateMetadataBecomesHead(t *testing.T) {
	root := t.TempDir()
	writeNextFile(t, root, "app/page.tsx", `export async function generateMetadata({ params }: any) {
  return { title: "About", description: "About page" };
}
export default function Page() { return <h1>About</h1>; }
`)
	rep, err := ScanNext(root)
	if err != nil {
		t.Fatal(err)
	}
	page := findPlan(rep, "src/pages/index.tsx")
	if page == nil {
		t.Fatal("page missing")
	}
	body := string(page.Content)
	if !strings.Contains(body, "<title>About</title>") || !strings.Contains(body, `content="About page"`) {
		t.Fatalf("static generateMetadata not wrapped:\n%s", body)
	}
	if hasManual(rep, "app/page.tsx", "generateMetadata") {
		t.Errorf("static generateMetadata should be handled: %+v", rep.Manual)
	}
}

func TestDynamicSyncGenerateMetadataBecomesHead(t *testing.T) {
	root := t.TempDir()
	writeNextFile(t, root, "app/page.tsx", `export function generateMetadata({ params }: any) {
  return { title: params.slug, description: "Dynamic" };
}
export default function Page() { return <h1>About</h1>; }
`)
	rep, err := ScanNext(root)
	if err != nil {
		t.Fatal(err)
	}
	page := findPlan(rep, "src/pages/index.tsx")
	if page == nil {
		t.Fatal("page missing")
	}
	body := string(page.Content)
	for _, want := range []string{"<__KrateMetadata {...props} />", "function __KrateMetadata", "generateMetadata(props)"} {
		if !strings.Contains(body, want) {
			t.Errorf("dynamic metadata wrapper missing %q:\n%s", want, body)
		}
	}
	if hasManual(rep, "app/page.tsx", "generateMetadata") {
		t.Errorf("sync generateMetadata should be handled: %+v", rep.Manual)
	}
}

func TestDynamicGenerateMetadataStaysManual(t *testing.T) {
	root := t.TempDir()
	writeNextFile(t, root, "app/page.tsx", `export async function generateMetadata({ params }: any) {
  return { title: params.slug };
}
export default function Page() { return <h1>About</h1>; }
`)
	rep, err := ScanNext(root)
	if err != nil {
		t.Fatal(err)
	}
	if !hasManual(rep, "app/page.tsx", "generateMetadata") {
		t.Fatalf("dynamic generateMetadata should be manual: %+v", rep.Manual)
	}
}

func TestArrowPageMetadataBecomesHead(t *testing.T) {
	root := t.TempDir()
	writeNextFile(t, root, "app/page.tsx", `export const metadata = { title: "About" };
const Page = () => <h1>About</h1>;
export default Page;`)
	rep, err := ScanNext(root)
	if err != nil {
		t.Fatal(err)
	}
	page := findPlan(rep, "src/pages/index.tsx")
	if page == nil {
		t.Fatal("page missing")
	}
	body := string(page.Content)
	if !strings.Contains(body, "export default function PageWithHead") || !strings.Contains(body, "<Page {...props} />") {
		t.Fatalf("arrow page not wrapped:\n%s", body)
	}
	if hasManual(rep, "app/page.tsx", "metadata export is not translated") {
		t.Fatalf("arrow metadata should be wrapped: %+v", rep.Manual)
	}
}

func TestNextMiddlewareRewrites(t *testing.T) {
	root := t.TempDir()
	writeNextFile(t, root, "app/page.tsx", `export default function Page() { return <h1>Home</h1>; }`)
	writeNextFile(t, root, "middleware.ts", `import { NextRequest, NextResponse } from "next/server";
export function middleware(request: NextRequest) {
  const sid = request.cookies.get("sid")?.value;
  const ip = request.ip;
  const country = request.geo?.country;
  if (request.nextUrl.pathname === "/old") return NextResponse.redirect(new URL("/new", request.url));
  return NextResponse.next();
}
export const config = { matcher: ["/docs/:path*"] };
`)
	rep, err := ScanNext(root)
	if err != nil {
		t.Fatal(err)
	}
	mw := findPlan(rep, "middleware.ts")
	if mw == nil {
		t.Fatal("middleware plan missing")
	}
	body := string(mw.Content)
	for _, want := range []string{"request: Request", "new URL(request.url).pathname", "Response.redirect", "const NextResponse = {", "next: () => undefined", "const __krateMatchers", "export function middleware", "__krateCookieJar", "__krateIP", "__krateGeo"} {
		if !strings.Contains(body, want) {
			t.Errorf("middleware missing %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "next/server") || strings.Contains(body, "export const config") {
		t.Errorf("Next.js middleware scaffolding remains:\n%s", body)
	}
	for _, reason := range []string{"NextResponse.next"} {
		if !hasManual(rep, "middleware.ts", reason) {
			t.Errorf("middleware manual %q missing: %+v", reason, rep.Manual)
		}
	}
	p := parser.New(lexer.New(body).Tokenize())
	if prog := p.ParseProgram(); prog == nil || len(p.Errors()) > 0 {
		t.Fatalf("rewritten middleware does not parse: %v\n%s", p.Errors(), body)
	}
}

func TestUpdatePackagePlan(t *testing.T) {
	root := t.TempDir()
	writeNextFile(t, root, "app/page.tsx", `export default function Page() { return <h1>Home</h1>; }`)
	writeNextFile(t, root, "package.json", `{
  "scripts": {"dev": "next dev", "build": "next build", "test": "vitest"},
  "dependencies": {"next": "15.0.0", "react": "19.0.0"}
}`)
	rep, err := ScanNextWithOptions(root, ScanOptions{UpdatePackage: true})
	if err != nil {
		t.Fatal(err)
	}
	pkg := findPlan(rep, "package.json")
	if pkg == nil {
		t.Fatal("package.json plan missing")
	}
	var got struct {
		Scripts      map[string]string `json:"scripts"`
		Dependencies map[string]string `json:"dependencies"`
	}
	if err := json.Unmarshal(pkg.Content, &got); err != nil {
		t.Fatal(err)
	}
	if got.Scripts["dev"] != "krate dev" || got.Scripts["build"] != "krate build" || got.Scripts["serve"] != "krate serve" {
		t.Fatalf("scripts = %+v", got.Scripts)
	}
	if got.Scripts["test"] != "vitest" {
		t.Fatalf("unrelated script was lost: %+v", got.Scripts)
	}
	for _, dep := range []string{"@krate/core", "@krate/runtime", "@krate/components", "preact"} {
		if _, ok := got.Dependencies[dep]; !ok {
			t.Errorf("missing dependency %s: %+v", dep, got.Dependencies)
		}
	}
	if _, ok := got.Dependencies["next"]; !ok {
		t.Error("Next dependency should remain until the user verifies the migration")
	}
}

func TestScanRejectsNonNextProject(t *testing.T) {
	if _, err := ScanNext(t.TempDir()); err == nil {
		t.Fatal("expected a not-a-Next.js error")
	}
}

func TestErrorBoundaryBecomesLayout(t *testing.T) {
	root := t.TempDir()
	writeNextFile(t, root, "app/page.tsx", `export default function Page() { return <h1>Home</h1>; }`)
	writeNextFile(t, root, "app/dashboard/error.tsx", `"use client";
export default function ErrorPage({ error, reset }: any) { return <button onClick={reset}>{error.message}</button>; }
`)
	rep, err := ScanNext(root)
	if err != nil {
		t.Fatal(err)
	}
	if findPlan(rep, "src/migrated/app/dashboard/error.tsx") == nil {
		t.Errorf("error asset missing: %+v", rep.Files)
	}
	layout := findPlan(rep, "src/pages/dashboard/_layout.tsx")
	if layout == nil || !strings.Contains(string(layout.Content), "__KrateErrorBoundary") {
		t.Fatalf("error boundary layout missing: %+v", layout)
	}
	if strings.Contains(string(layout.Content), "use client") {
		t.Errorf("client directive leaked into generated layout:\n%s", layout.Content)
	}
}

func TestNextNavigationAndHeadersModuleRewrites(t *testing.T) {
	root := t.TempDir()
	writeNextFile(t, root, "app/page.tsx", `import { usePathname, useRouter, useSearchParams } from "next/navigation";
import { cookies, headers } from "next/headers";
export default function Page() {
  const path = usePathname();
  const router = useRouter();
  const params = useSearchParams();
  const sid = cookies().get("sid");
  const ua = headers().get("user-agent");
  return <h1 onClick={() => router.push("/x")}>{path}{params.get("q")}{sid}{ua}</h1>;
}`)
	rep, err := ScanNext(root)
	if err != nil {
		t.Fatal(err)
	}
	page := findPlan(rep, "src/pages/index.tsx")
	if page == nil {
		t.Fatal("page missing")
	}
	body := string(page.Content)
	if !strings.Contains(body, `"../migrated/next-navigation"`) {
		t.Errorf("next/navigation not rewritten:\n%s", body)
	}
	if !strings.Contains(body, `"@krate/runtime/server"`) {
		t.Errorf("next/headers not rewritten:\n%s", body)
	}
	helper := findPlan(rep, "src/migrated/next-navigation.ts")
	if helper == nil || !strings.Contains(string(helper.Content), "export function usePathname") {
		t.Fatalf("navigation helper missing: %+v", helper)
	}
}

func TestNextDynamicRewritesToLazy(t *testing.T) {
	root := t.TempDir()
	writeNextFile(t, root, "app/page.tsx", `import dynamic from "next/dynamic";
const Chart = dynamic(() => import("./Chart"), { ssr: false });
export default function Page() { return <Chart />; }
`)
	writeNextFile(t, root, "app/Chart.tsx", `export default function Chart() { return <p>chart</p>; }`)
	rep, err := ScanNext(root)
	if err != nil {
		t.Fatal(err)
	}
	page := findPlan(rep, "src/pages/index.tsx")
	if page == nil {
		t.Fatal("page missing")
	}
	body := string(page.Content)
	for _, want := range []string{`import { lazy } from "react";`, "lazy(() => import(", "<Chart />"} {
		if !strings.Contains(body, want) {
			t.Errorf("dynamic rewrite missing %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "next/dynamic") {
		t.Errorf("next/dynamic import remains:\n%s", body)
	}
	if !hasManual(rep, "app/page.tsx", "ssr/loading") {
		t.Errorf("dynamic option manual missing: %+v", rep.Manual)
	}
}

func TestNextAPIRouteServerRewrites(t *testing.T) {
	root := t.TempDir()
	writeNextFile(t, root, "app/api/hello/route.ts", `import { NextRequest, NextResponse } from "next/server";
export async function GET(request: NextRequest) {
  return NextResponse.json({ ok: true, url: request.url });
}
`)
	rep, err := ScanNext(root)
	if err != nil {
		t.Fatal(err)
	}
	api := findPlan(rep, "src/api/hello.ts")
	if api == nil {
		t.Fatal("api route missing")
	}
	body := string(api.Content)
	for _, want := range []string{"request: Request", "Response.json", "request.url"} {
		if !strings.Contains(body, want) {
			t.Errorf("api rewrite missing %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "next/server") {
		t.Errorf("next/server import remains:\n%s", body)
	}
}

func TestNextConfigPluginWrapper(t *testing.T) {
	root := t.TempDir()
	writeNextFile(t, root, "app/page.tsx", `export default function Page() { return <h1>Home</h1>; }`)
	writeNextFile(t, root, "next.config.js", `module.exports = withPlugin({ redirects: [{ source: "/a", destination: "/b" }] });`)
	rep, err := ScanNext(root)
	if err != nil {
		t.Fatal(err)
	}
	cfg := findPlan(rep, "krate.config.ts")
	if cfg == nil || !strings.Contains(string(cfg.Content), `"source": "/a"`) {
		t.Fatalf("wrapped config not translated: %+v", cfg)
	}
	if !hasWarning(rep, "plugin wrapper behavior is not migrated") {
		t.Fatalf("missing wrapper warning: %+v", rep.Warnings)
	}
}

func TestMiddlewareComputedMatcher(t *testing.T) {
	root := t.TempDir()
	writeNextFile(t, root, "app/page.tsx", `export default function Page() { return <h1>Home</h1>; }`)
	writeNextFile(t, root, "middleware.ts", `const paths = makePaths();
export const config = { matcher: paths };
export function middleware(request: Request) { return undefined; }
`)
	rep, err := ScanNext(root)
	if err != nil {
		t.Fatal(err)
	}
	mw := findPlan(rep, "middleware.ts")
	if mw == nil || !strings.Contains(string(mw.Content), "config.matcher") || !strings.Contains(string(mw.Content), "export const config") {
		t.Fatalf("computed matcher not wrapped: %+v", mw)
	}
	if !hasManual(rep, "middleware.ts", "evaluated at runtime") {
		t.Errorf("computed matcher warning missing: %+v", rep.Manual)
	}
}

func TestMiddlewareLocalMatcherVariable(t *testing.T) {
	root := t.TempDir()
	writeNextFile(t, root, "app/page.tsx", `export default function Page() { return <h1>Home</h1>; }`)
	writeNextFile(t, root, "middleware.ts", `const paths = ["/docs/:path*"];
export const config = { matcher: paths };
export function middleware(request: Request) { return undefined; }
`)
	rep, err := ScanNext(root)
	if err != nil {
		t.Fatal(err)
	}
	mw := findPlan(rep, "middleware.ts")
	if mw == nil || !strings.Contains(string(mw.Content), `"/docs/:path*"`) {
		t.Fatalf("local matcher not translated: %+v", mw)
	}
}

func TestExistingConfigMerge(t *testing.T) {
	root := t.TempDir()
	writeNextFile(t, root, "app/page.tsx", `export default function Page() { return <h1>Home</h1>; }`)
	writeNextFile(t, root, "next.config.js", `export default { redirects: [{ source: "/old", destination: "/new", permanent: true }] };`)
	writeNextFile(t, root, "krate.config.ts", `import { defineConfig } from "@krate/core";
export default defineConfig({
  minify: false,
  devServer: { port: 4000 },
});
`)
	rep, err := ScanNext(root)
	if err != nil {
		t.Fatal(err)
	}
	cfg := findPlan(rep, "krate.config.ts")
	if cfg == nil {
		t.Fatalf("merged config plan missing: %+v", rep.Files)
	}
	body := string(cfg.Content)
	for _, want := range []string{"minify: false", "port: 4000", `"redirects":`, `"source": "/old"`} {
		if !strings.Contains(body, want) {
			t.Errorf("merged config missing %q:\n%s", want, body)
		}
	}
	p := parser.New(lexer.New(body).Tokenize())
	if prog := p.ParseProgram(); prog == nil || len(p.Errors()) > 0 {
		t.Fatalf("merged config does not parse: %v\n%s", p.Errors(), body)
	}
	if hasManual(rep, "krate.config.ts", "could not be merged") {
		t.Errorf("merge should not be manual: %+v", rep.Manual)
	}
}

func TestExistingConfigFunctionMerge(t *testing.T) {
	root := t.TempDir()
	writeNextFile(t, root, "app/page.tsx", `export default function Page() { return <h1>Home</h1>; }`)
	writeNextFile(t, root, "next.config.js", `export default { basePath: "/docs" };`)
	writeNextFile(t, root, "krate.config.ts", `import { defineConfig } from "@krate/core";
export default defineConfig(({ isDev }) => ({
  minify: !isDev,
}));
`)
	rep, err := ScanNext(root)
	if err != nil {
		t.Fatal(err)
	}
	cfg := findPlan(rep, "krate.config.ts")
	if cfg == nil || !strings.Contains(string(cfg.Content), `"basePath": "/docs"`) {
		t.Fatalf("function config not merged: %+v", cfg)
	}
}

func TestExistingConfigKeysWin(t *testing.T) {
	root := t.TempDir()
	writeNextFile(t, root, "app/page.tsx", `export default function Page() { return <h1>Home</h1>; }`)
	writeNextFile(t, root, "next.config.js", `export default { redirects: [{ source: "/old", destination: "/new" }] };`)
	writeNextFile(t, root, "krate.config.ts", `export default { redirects: [{ source: "/existing", destination: "/kept" }] };`)
	rep, err := ScanNext(root)
	if err != nil {
		t.Fatal(err)
	}
	if cfg := findPlan(rep, "krate.config.ts"); cfg != nil {
		t.Fatalf("existing redirects should not be overwritten: %s", cfg.Content)
	}
	if !hasWarning(rep, "existing redirects was kept") {
		t.Fatalf("missing existing-key warning: %+v", rep.Warnings)
	}
}

func TestExistingJSONConfigMerge(t *testing.T) {
	root := t.TempDir()
	writeNextFile(t, root, "app/page.tsx", `export default function Page() { return <h1>Home</h1>; }`)
	writeNextFile(t, root, "next.config.js", `export default { basePath: "/docs" };`)
	writeNextFile(t, root, "krate.config.json", `{"minify": false}`)
	rep, err := ScanNext(root)
	if err != nil {
		t.Fatal(err)
	}
	cfg := findPlan(rep, "krate.config.json")
	if cfg == nil {
		t.Fatal("json config merge missing")
	}
	var got map[string]any
	if err := json.Unmarshal(cfg.Content, &got); err != nil {
		t.Fatal(err)
	}
	if got["minify"] != false || got["basePath"] != "/docs" {
		t.Fatalf("merged json = %+v", got)
	}
}

func TestScanRejectsKrateProject(t *testing.T) {
	root := t.TempDir()
	writeNextFile(t, root, "src/pages/index.tsx", `export default function Page() { return <h1>Home</h1>; }`)
	writeNextFile(t, root, "krate.config.ts", `export default {};`)
	if _, err := ScanNext(root); err == nil {
		t.Fatal("src/pages without Next evidence must not be treated as a Next.js project")
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
