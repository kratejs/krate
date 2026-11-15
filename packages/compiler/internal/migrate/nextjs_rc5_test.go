package migrate

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/jseval"
)

const umamiScript = `import Script from 'next/script';
import { analyticsConfig } from '@/lib/analytics';

/** Loads the self-hosted Umami tracker. Renders nothing unless the URL and website id are configured. */
export function UmamiScript() {
  const cfg = analyticsConfig();
  if (!cfg) return null;
  return (
    <Script
      src={cfg.scriptUrl}
      data-website-id={cfg.websiteId}
      // Umami itself also bails out when the browser sends Do Not Track.
      data-do-not-track="true"
      strategy="afterInteractive"
    />
  );
}
`

func TestMigrateNextScriptBecomesKrateScript(t *testing.T) {
	root := gapsProject(t, map[string]string{
		"tsconfig.json":                   `{"compilerOptions":{"baseUrl":".","paths":{"@/*":["src/*"]}}}`,
		"src/lib/analytics.ts":            `export function analyticsConfig() { return { scriptUrl: "https://u.test/s.js", websiteId: "w" }; }`,
		"src/components/umami-script.tsx": umamiScript,
		"src/components/early.tsx":        "import Script from 'next/script';\nexport const Early = () => <Script id=\"boot\" strategy=\"beforeInteractive\">{`window.boot = 1;`}</Script>;\n",
		"src/components/idle.tsx":         "import Script from 'next/script';\nexport const Idle = () => <Script src=\"/chat.js\" strategy=\"lazyOnload\" onLoad={() => {}} />;\n",
		"src/components/worker.tsx":       "import Script from 'next/script';\nexport const W = () => <Script src=\"/w.js\" strategy=\"worker\" />;\n",
		"src/app/layout.tsx":              "import { UmamiScript } from '@/components/umami-script';\nimport { Early } from '@/components/early';\nimport { Idle } from '@/components/idle';\nimport { W } from '@/components/worker';\nexport default function L({ children }: { children: any }) { return <html><body><Early /><UmamiScript /><Idle /><W />{children}</body></html>; }\n",
		"src/app/page.tsx":                `export default function P() { return <main/>; }`,
	})
	rep, err := ScanNext(root)
	if err != nil {
		t.Fatal(err)
	}
	body := func(source string) string {
		for _, f := range rep.Files {
			if f.Source == source {
				return string(f.Content)
			}
		}
		t.Fatalf("no planned file for %s", source)
		return ""
	}
	umami := body("src/components/umami-script.tsx")
	for _, want := range []string{"<Script", "src={cfg.scriptUrl}", `data-website-id={cfg.websiteId}`, `data-do-not-track="true"`, " defer />"} {
		if !strings.Contains(umami, want) {
			t.Errorf("umami script missing %q:\n%s", want, umami)
		}
	}
	if strings.Contains(umami, "next/script") || strings.Contains(umami, "strategy") {
		t.Errorf("next/script was not removed:\n%s", umami)
	}
	if early := body("src/components/early.tsx"); !strings.Contains(early, "<Head><script id=\"boot\">{`window.boot = 1;`}</script></Head>") || strings.Contains(early, "next/script") {
		t.Errorf("beforeInteractive did not move into <Head>:\n%s", early)
	}
	if idle := body("src/components/idle.tsx"); !strings.Contains(idle, `<Script src="/chat.js" async />`) || strings.Contains(idle, "onLoad") {
		t.Errorf("lazyOnload did not become async:\n%s", idle)
	}
	if w := body("src/components/worker.tsx"); !strings.Contains(w, `<Script src="/w.js" defer />`) {
		t.Errorf("worker script was not converted:\n%s", w)
	}
	if !hasManual(rep, "src/components/worker.tsx", "strategy worker is not supported") || !hasManual(rep, "src/components/idle.tsx", "onLoad handler was dropped") {
		t.Errorf("worker strategy and onLoad must be manual items: %+v", rep.Manual)
	}
}

func TestMigrateAfterFromNextServerInPages(t *testing.T) {
	root := gapsProject(t, map[string]string{
		"src/app/(marketing)/s/[slug]/page.tsx": `import { after } from 'next/server';
export default async function Share({ params }: { params: Promise<{ slug: string }> }) {
  const { slug } = await params;
  after(() => console.log(slug));
  return <main>{slug}</main>;
}
`,
		"src/app/mixed/page.tsx": `import type { NextRequest } from 'next/server';
import { NextResponse, unstable_after as later, type NextFetchEvent } from 'next/server';
export default function Mixed() { later(() => {}); return <main/>; }
`,
	})
	rep, err := ScanNext(root)
	if err != nil {
		t.Fatal(err)
	}
	share := string(findPlan(rep, "src/pages/(marketing)/s/[slug].tsx").Content)
	if !strings.Contains(share, `import { after } from "@krate/runtime/server";`) || strings.Contains(share, "next/server") {
		t.Errorf("page after() was not moved to the runtime:\n%s", share)
	}
	if hasManual(rep, "src/app/(marketing)/s/[slug]/page.tsx", "next/server") {
		t.Errorf("a page that only imports after() has no next/server work left: %+v", rep.Manual)
	}
	mixed := string(findPlan(rep, "src/pages/mixed.tsx").Content)
	for _, want := range []string{`import type { NextRequest } from 'next/server';`, `import { NextResponse, type NextFetchEvent } from "next/server";`, `import { after as later } from "@krate/runtime/server";`} {
		if !strings.Contains(mixed, want) {
			t.Errorf("mixed imports missing %q:\n%s", want, mixed)
		}
	}
}

func TestMigrateDynamicWithTypeArgumentKeepsFallback(t *testing.T) {
	root := gapsProject(t, map[string]string{
		"src/components/viewer/index.tsx": `'use client';

import dynamic from 'next/dynamic';
import { Skeleton } from '@/components/ui/skeleton';
import { cn } from '@/lib/utils';
import type { VehicleViewerProps } from './types';

/**
 * Shared 3D vehicle viewer. Client-only and lazy.
 */
export const VehicleViewer = dynamic<VehicleViewerProps>(() => import('./viewer'), {
  ssr: false,
  loading: () => (
    <div className={cn('dd-late bg-card flex min-h-64 items-center justify-center rounded-xl border')}>
      <Skeleton className="h-1/3 w-2/3 rounded-xl" />
    </div>
  ),
});
`,
		"src/app/page.tsx": `import { VehicleViewer } from '@/components/viewer';
export default function P() { return <main><VehicleViewer color="red" /></main>; }`,
		"tsconfig.json": `{"compilerOptions":{"baseUrl":".","paths":{"@/*":["src/*"]}}}`,
	})
	rep, err := ScanNext(root)
	if err != nil {
		t.Fatal(err)
	}
	var got string
	for _, f := range rep.Files {
		if f.Source == "src/components/viewer/index.tsx" {
			got = string(f.Content)
		}
	}
	for _, want := range []string{
		"const VehicleViewerLazy = lazy<VehicleViewerProps>(() => import('./viewer'));",
		"export const VehicleViewer = (props: VehicleViewerProps) => {",
		"useEffect(() => { setMounted(true); }, []);",
		"return mounted ? <VehicleViewerLazy {...props} /> : (",
		"<div className={cn('dd-late bg-card",
		"import { lazy, useEffect, useState } from \"react\";",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("migrated viewer missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "dynamic") || strings.Contains(got, "next/dynamic") {
		t.Errorf("dynamic was not fully rewritten:\n%s", got)
	}
	// The doc comment stays attached to the export.
	if strings.Index(got, "const VehicleViewerLazy") > strings.Index(got, "Shared 3D vehicle viewer") {
		t.Errorf("hoisted lazy declaration split the doc comment from the export:\n%s", got)
	}
}

func TestMigratedMiddlewareMatcherKeepsRegexGroups(t *testing.T) {
	const nextDefault = `/((?!_next/static|_next/image|favicon\\.ico|.*\\.(?:png|jpe?g|gif|svg|webp|ico|css|js|map|woff2?|glb|onnx|wasm|txt|xml|json)$).*)`
	root := gapsProject(t, map[string]string{
		"src/app/page.tsx": `export default function P() { return <main/>; }`,
		"src/middleware.ts": `import { NextResponse } from "next/server";
export function middleware() { return NextResponse.next(); }
export const config = { matcher: ['` + nextDefault + `', '/account/:path*', '/blog/:slug(\\d+)'] };
`,
	})
	rep, err := ScanNext(root)
	if err != nil {
		t.Fatal(err)
	}
	mw := string(findPlan(rep, "src/middleware.ts").Content)
	start := strings.Index(mw, "function __krateMatcher")
	end := strings.Index(mw, "export function middleware(request")
	if start < 0 || end < start {
		t.Fatalf("matcher shim missing:\n%s", mw)
	}
	var matchers []string
	{
		i := strings.Index(mw, "const __krateMatchers = ")
		j := strings.Index(mw[i:], ";\n")
		if err := json.Unmarshal([]byte(mw[i+len("const __krateMatchers = "):i+j]), &matchers); err != nil {
			t.Fatalf("matchers: %v\n%s", err, mw)
		}
	}
	src := mw[start:end] + "\nconst m = " + string(mustJSON(t, matchers)) + ";\n" +
		"const hit = (p: string) => m.map((x: string) => __krateMatcher(x, p));\n" +
		"(globalThis as any).__krateEvalResult = { store: hit('/store'), asset: hit('/_next/static/x.js'), logo: hit('/logo.png'), acct: hit('/account/a/b'), blog: hit('/blog/12'), blogBad: hit('/blog/abc') };\n"
	out, err := jseval.Eval(jseval.Options{Source: src, ResolveDir: root})
	if err != nil {
		t.Fatalf("evaluating the shim: %v", err)
	}
	var res map[string][]bool
	if err := json.Unmarshal(out, &res); err != nil {
		t.Fatal(err)
	}
	// Index 0 is Next's default matcher, 1 the :path* rule, 2 the custom regex rule.
	if !res["store"][0] || res["asset"][0] || res["logo"][0] {
		t.Errorf("default matcher: /store=%v /_next/static/x.js=%v /logo.png=%v", res["store"][0], res["asset"][0], res["logo"][0])
	}
	if !res["acct"][1] || res["store"][1] || !res["blog"][2] || res["blogBad"][2] {
		t.Errorf("path-to-regexp params: %+v", res)
	}
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestMigrateKeepsNextURLFormAndTailwind(t *testing.T) {
	root := gapsProject(t, map[string]string{
		"src/app/page.tsx":   `export default function P() { return <main/>; }`,
		"next.config.ts":     "export default { reactStrictMode: true };\n",
		"postcss.config.mjs": "const config = { plugins: { '@tailwindcss/postcss': {} } };\nexport default config;\n",
	})
	rep, err := ScanNext(root)
	if err != nil {
		t.Fatal(err)
	}
	cfg := string(findPlan(rep, "krate.config.ts").Content)
	if !strings.Contains(cfg, `"trailingSlash": "never"`) || !strings.Contains(cfg, `"tailwind": {`) || !strings.Contains(cfg, `"enabled": true`) {
		t.Errorf("config must keep Next's URL form and enable Tailwind:\n%s", cfg)
	}

	root = gapsProject(t, map[string]string{
		"src/app/page.tsx": `export default function P() { return <main/>; }`,
		"next.config.ts":   "export default { trailingSlash: true };\n",
	})
	rep, err = ScanNext(root)
	if err != nil {
		t.Fatal(err)
	}
	cfg = string(findPlan(rep, "krate.config.ts").Content)
	if !strings.Contains(cfg, `"trailingSlash": "always"`) || strings.Contains(cfg, "tailwind") {
		t.Errorf("trailingSlash: true must write always and no Tailwind:\n%s", cfg)
	}
	if hasManual(rep, "next.config.ts", "trailingSlash") {
		t.Errorf("trailingSlash is translated now: %+v", rep.Manual)
	}
}
