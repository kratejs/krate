package build

import (
	"bytes"
	"image"
	"image/color"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
	"github.com/kratejs/krate/packages/compiler/internal/diag"
)

func staticRouteTestProject(t *testing.T, files map[string]string, staticOutput bool) (string, *config.Config) {
	t.Helper()
	if _, err := exec.LookPath("node"); err != nil {
		requireE2E(t, "node is not installed")
		t.Skip("node is not installed")
	}
	root := t.TempDir()
	writeTree(t, root, files)
	ensureHandlerDist(t)
	if err := linkRuntimePackage(t, filepath.Join(root, "node_modules", "@krate")); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.PagesDir = filepath.Join(root, "src", "pages")
	cfg.OutDir = filepath.Join(root, "dist")
	if staticOutput {
		cfg.Output = "static"
	}
	cfg.LoadTSConfigPaths(root)
	return root, cfg
}

func TestStaticRouteHandlersBuildTextDynamicAndImage(t *testing.T) {
	t.Parallel()
	root, cfg := staticRouteTestProject(t, map[string]string{
		"tsconfig.json": `{"compilerOptions":{"jsx":"preserve","jsxImportSource":"@krate/runtime","paths":{"@/*":["./*"]}}}`,
		"lib/data.ts": `export const items = [
  { slug: "alpha", title: "Alpha Guide", bg: "#0b3d91" },
  { slug: "beta/deep", title: "Deep Guide", bg: "#065f46" },
];
`,
		"src/pages/index.tsx": `export default function Home() { return <main>Home</main>; }`,
		"src/pages/index-feed.txt/route.ts": `import { items } from "@/lib/data";
export const dynamic = "force-static";
export async function GET() {
  return new Response(items.map((i) => i.title).join("\n") + "\n", {
    headers: { "Content-Type": "text/plain; charset=utf-8" },
  });
}
`,
		"src/pages/notes/[[...slug]]/route.ts": `import { items } from "@/lib/data";
export const dynamic = "force-static";
export function generateStaticParams() {
  return [{ slug: [] }, ...items.map((i) => ({ slug: i.slug.split("/") }))];
}
export async function GET(_req: Request, ctx: { params: Promise<{ slug?: string[] }> }) {
  const { slug } = await ctx.params;
  if (!slug || slug.length === 0) {
    return new Response("# All Notes\n", { headers: { "Content-Type": "text/markdown; charset=utf-8" } });
  }
  const key = slug.join("/");
  const found = items.find((i) => i.slug === key);
  return new Response("# " + (found ? found.title : key) + "\n", {
    headers: { "Content-Type": "text/markdown; charset=utf-8" },
  });
}
`,
		"src/pages/cards/[...slug]/route.tsx": `import { ImageResponse } from "@krate/runtime/og";
import { items } from "@/lib/data";
export const dynamic = "force-static";
export function generateStaticParams() {
  return items.map((i) => ({ slug: i.slug.split("/") }));
}
export async function GET(_req: Request, ctx: { params: Promise<{ slug: string[] }> }) {
  const { slug } = await ctx.params;
  const item = items.find((i) => i.slug === slug.join("/")) || items[0];
  return new ImageResponse(
    <div style={{ width: "100%", height: "100%", display: "flex", alignItems: "center", justifyContent: "center", backgroundColor: item.bg, color: "#ffffff", fontSize: 48 }}>
      {item.title}
    </div>,
    { width: 400, height: 200 },
  );
}
`,
		"src/api/catalog.ts": `import { items } from "@/lib/data";
export async function GET() {
  return Response.json({ count: items.length });
}
`,
	}, true)

	if err := New(root, cfg).BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}

	readOut := func(rel string) string {
		data, err := os.ReadFile(filepath.Join(root, "dist", filepath.FromSlash(rel)))
		if err != nil {
			t.Fatalf("read %s: %v", rel, err)
		}
		return string(data)
	}

	if got := readOut("index-feed.txt"); got != "Alpha Guide\nDeep Guide\n" {
		t.Errorf("index-feed.txt = %q", got)
	}
	// Parent/child collision: /notes collides with /notes/alpha so it writes notes/index.md.
	if got := readOut("notes/index.md"); got != "# All Notes\n" {
		t.Errorf("notes/index.md = %q", got)
	}
	if got := readOut("notes/alpha"); got != "# Alpha Guide\n" {
		t.Errorf("notes/alpha = %q", got)
	}
	if got := readOut("notes/beta/deep"); got != "# Deep Guide\n" {
		t.Errorf("notes/beta/deep = %q", got)
	}
	if got := readOut("api/catalog"); !strings.Contains(got, `"count":2`) {
		t.Errorf("api/catalog = %q", got)
	}

	// Verify ImageResponse route wrote real PNGs with the expected colors.
	checkCard := func(rel string, want color.NRGBA) {
		data, err := os.ReadFile(filepath.Join(root, "dist", filepath.FromSlash(rel)))
		if err != nil {
			t.Fatalf("read %s: %v", rel, err)
		}
		img, format, err := image.Decode(bytes.NewReader(data))
		if err != nil || format != "png" {
			t.Fatalf("%s is not PNG: %v %s", rel, err, format)
		}
		if b := img.Bounds(); b.Dx() != 400 || b.Dy() != 200 {
			t.Errorf("%s bounds = %v, want 400x200", rel, b)
		}
		if c := pixel(img, 5, 5); c != want {
			t.Errorf("%s pixel(5,5) = %v, want %v", rel, c, want)
		}
	}
	checkCard("cards/alpha", color.NRGBA{0x0b, 0x3d, 0x91, 255})
	checkCard("cards/beta/deep", color.NRGBA{0x06, 0x5f, 0x46, 255})

	// No runtime API bundles should be emitted for static routes.
	for _, dir := range []string{"api", "api-node"} {
		if _, err := os.Stat(filepath.Join(root, "dist", ".krate", dir)); !os.IsNotExist(err) {
			t.Errorf("unexpected server bundle dir dist/.krate/%s in static build", dir)
		}
	}

	// static-routes.json records extensionless Content-Types and collision rewrites.
	idx := loadStaticRoutes(filepath.Join(root, "dist"))
	if idx == nil {
		t.Fatal("static-routes.json was not written")
	}
	if e := idx["/notes"]; e.File != "/notes/index.md" || !strings.Contains(e.ContentType, "text/markdown") {
		t.Errorf("manifest /notes = %+v", e)
	}
	if e := idx["/notes/alpha"]; e.File != "/notes/alpha" || !strings.Contains(e.ContentType, "text/markdown") {
		t.Errorf("manifest /notes/alpha = %+v", e)
	}
	if e := idx["/cards/alpha"]; e.ContentType != "image/png" {
		t.Errorf("manifest /cards/alpha = %+v", e)
	}
	if e := idx["/api/catalog"]; !strings.Contains(e.ContentType, "application/json") {
		t.Errorf("manifest /api/catalog = %+v", e)
	}
}

func TestStaticRouteForceStaticAlongsideRequestTimeAPI(t *testing.T) {
	t.Parallel()
	root, cfg := staticRouteTestProject(t, map[string]string{
		"src/pages/index.tsx": `export default function Home() { return <main>Home</main>; }`,
		"src/api/frozen.ts": `export const dynamic = "force-static";
export function GET() { return Response.json({ static: true }); }
`,
		"src/api/live.ts": `export function GET(req: Request) {
  return Response.json({ host: req.headers.get("host") || "none" });
}
`,
	}, false)

	if err := New(root, cfg).BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(root, "dist", "api", "frozen"))
	if err != nil || !strings.Contains(string(data), `"static":true`) {
		t.Fatalf("dist/api/frozen = %q, err=%v", string(data), err)
	}
	if _, err := os.Stat(filepath.Join(root, "dist", ".krate", "api", "live.js")); err != nil {
		t.Errorf("request-time API route live.js missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "dist", ".krate", "api", "frozen.js")); !os.IsNotExist(err) {
		t.Errorf("force-static route should not emit a runtime bundle frozen.js")
	}
}

func TestStaticRouteDiagnosticsForRequestDependentAndNonGET(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		file     string
		src      string
		wantSub  string
		wantCode diag.Code
	}{
		{
			name:     "non-GET POST under output static",
			file:     "src/api/submit.ts",
			src:      `export async function POST() { return Response.json({ ok: true }); }`,
			wantSub:  "POST",
			wantCode: diag.CodeStaticRouteHandler,
		},
		{
			name:     "request.headers read",
			file:     "src/pages/echo/route.ts",
			src:      `export async function GET(req: Request) { return new Response(req.headers.get("x-token") || ""); }`,
			wantSub:  "request.headers",
			wantCode: diag.CodeStaticRouteHandler,
		},
		{
			name:     "request.cookies read",
			file:     "src/pages/session/route.ts",
			src:      `export async function GET(req: any) { return new Response(String(req.cookies.get("sid"))); }`,
			wantSub:  "request.cookies",
			wantCode: diag.CodeStaticRouteHandler,
		},
		{
			name:     "URL searchParams read",
			file:     "src/pages/search/route.ts",
			src:      `export async function GET(req: Request) { const q = new URL(req.url).searchParams.get("q"); return new Response(q || ""); }`,
			wantSub:  "searchParams",
			wantCode: diag.CodeStaticRouteHandler,
		},
		{
			name:     "nextUrl searchParams read",
			file:     "src/pages/filter/route.ts",
			src:      `export async function GET(req: any) { return new Response(req.nextUrl.searchParams.get("tag") || ""); }`,
			wantSub:  "searchParams",
			wantCode: diag.CodeStaticRouteHandler,
		},
		{
			name: "runtime headers() helper",
			file: "src/pages/hdr/route.ts",
			src: `import { headers } from "@krate/runtime/server";
export async function GET() { const h = await headers(); return new Response(h.get("host") || ""); }`,
			wantSub:  "headers()",
			wantCode: diag.CodeStaticRouteHandler,
		},
		{
			name:     "dynamic route without generateStaticParams",
			file:     "src/pages/docs/[slug]/route.ts",
			src:      `export async function GET() { return new Response("ok"); }`,
			wantSub:  "generateStaticParams",
			wantCode: diag.CodeStaticRouteHandler,
		},
		{
			name:     "force-dynamic under output static",
			file:     "src/api/now.ts",
			src:      `export const dynamic = "force-dynamic"; export function GET() { return new Response("now"); }`,
			wantSub:  "force-dynamic",
			wantCode: diag.CodeStaticRouteHandler,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root, cfg := staticRouteTestProject(t, map[string]string{
				"src/pages/index.tsx": `export default function Home() { return <main>Home</main>; }`,
				tc.file:               tc.src,
			}, true)
			err := New(root, cfg).BuildAll()
			if err == nil {
				t.Fatalf("expected build error for %s, got nil", tc.name)
			}
			msg := err.Error()
			if !strings.Contains(msg, string(tc.wantCode)) || !strings.Contains(msg, tc.wantSub) {
				t.Errorf("error = %q, want code %s and %q", msg, tc.wantCode, tc.wantSub)
			}
		})
	}
}

func TestMetadataRoutesMigratedShapesAliasCwdAndAsync(t *testing.T) {
	t.Parallel()
	root, cfg := staticRouteTestProject(t, map[string]string{
		"tsconfig.json":    `{"compilerOptions":{"jsx":"preserve","jsxImportSource":"@krate/runtime","paths":{"@/*":["./*"]}}}`,
		"data/routes.json": `["/", "/docs/start"]`,
		"lib/site.ts":      `export const site = { name: "Acme Docs", url: "https://acme.example", tagline: "Clear guides" };`,
		"components/card.tsx": `import { ImageResponse } from "@krate/runtime/og";
export const cardSize = { width: 800, height: 400 };
export function buildCard(title: string) {
  return new ImageResponse(
    <div style={{ width: "100%", height: "100%", display: "flex", alignItems: "center", justifyContent: "center", backgroundColor: "#1e293b", color: "#f8fafc", fontSize: 56 }}>
      {title}
    </div>,
    cardSize,
  );
}
`,
		"src/pages/index.tsx": `export default function Home() { return <main>Home</main>; }`,
		"src/pages/sitemap.ts": `import fs from "node:fs";
import path from "node:path";
import type { MetadataRoute } from "@krate/runtime/server";
import { site } from "@/lib/site";

export const dynamic = "force-static";

export default async function sitemap(): Promise<MetadataRoute.Sitemap> {
  const routes: string[] = JSON.parse(fs.readFileSync(path.join(process.cwd(), "data/routes.json"), "utf8"));
  return routes.map((r) => ({ url: site.url + r, changeFrequency: "weekly", priority: 0.8 }));
}
`,
		"src/pages/opengraph-image.tsx": `import { buildCard, cardSize } from "@/components/card";
import { site } from "@/lib/site";

export const dynamic = "force-static";
export const alt = site.name + " - " + site.tagline;
export const size = cardSize;
export const contentType = "image/png";

export default async function Image() {
  return buildCard(site.name);
}
`,
		"src/pages/guides/[slug]/opengraph-image.tsx": `import { buildCard, cardSize } from "@/components/card";
export const alt = "Guide";
export const size = cardSize;
export const contentType = "image/png";
export function generateStaticParams() {
  return [{ slug: "intro" }];
}
export default async function Image({ params }: { params: Promise<{ slug: string }> }) {
  const { slug } = await params;
  return buildCard("Guide: " + slug);
}
`,
	}, true)

	if err := New(root, cfg).BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}

	sm, err := os.ReadFile(filepath.Join(root, "dist", "sitemap.xml"))
	if err != nil {
		t.Fatalf("sitemap.xml missing: %v", err)
	}
	for _, want := range []string{"<loc>https://acme.example/</loc>", "<loc>https://acme.example/docs/start</loc>"} {
		if !strings.Contains(string(sm), want) {
			t.Errorf("sitemap.xml lacks %q:\n%s", want, string(sm))
		}
	}

	og := decodePNG(t, root, "opengraph-image.png")
	if b := og.Bounds(); b.Dx() != 800 || b.Dy() != 400 {
		t.Errorf("opengraph-image.png bounds = %v, want 800x400", b)
	}
	if c := pixel(og, 5, 5); c != (color.NRGBA{0x1e, 0x29, 0x3b, 255}) {
		t.Errorf("opengraph-image.png bg = %v", c)
	}

	guideOG := decodePNG(t, root, "guides/intro/opengraph-image.png")
	if b := guideOG.Bounds(); b.Dx() != 800 || b.Dy() != 400 {
		t.Errorf("guides/intro/opengraph-image.png bounds = %v", b)
	}

	homeHTML, err := os.ReadFile(filepath.Join(root, "dist", "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(homeHTML), "opengraph-image.png") {
		t.Errorf("index.html head missing opengraph-image.png:\n%s", string(homeHTML))
	}
}

// A page component in a file named route.tsx (default export, no HTTP method)
// is still a page at /route, not a route handler.
func TestPagesRouteFileWithDefaultExportStaysAPage(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	page := filepath.Join(dir, "route.tsx")
	if err := os.WriteFile(page, []byte("export default function Route() { return <p>route page</p>; }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if isPagesRouteHandler(page) {
		t.Fatal("a default-exported route.tsx was taken for a route handler")
	}
	handler := filepath.Join(dir, "sub", "route.ts")
	if err := os.MkdirAll(filepath.Dir(handler), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(handler, []byte("export async function GET() { return new Response(\"x\"); }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !isPagesRouteHandler(handler) {
		t.Fatal("a GET-exporting route.ts was not taken for a route handler")
	}
}
