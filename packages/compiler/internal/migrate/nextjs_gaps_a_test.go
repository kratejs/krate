package migrate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func gapsProject(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	writeNextFile(t, root, "package.json", `{"dependencies":{"next":"16.0.0"}}`)
	for rel, body := range files {
		writeNextFile(t, root, rel, body)
	}
	return root
}

func hasManualHint(rep *Report, source, contains string) bool {
	for _, m := range rep.Manual {
		if m.Source == source && strings.Contains(m.Hint, contains) {
			return true
		}
	}
	return false
}

func TestProxyFileIsMigratedAsMiddleware(t *testing.T) {
	root := gapsProject(t, map[string]string{
		"src/app/page.tsx": `export default function P() { return <main/>; }`,
		"src/proxy.ts": `import { NextResponse } from "next/server";
import type { NextRequest } from "next/server";
export async function proxy(request: NextRequest) {
  if (request.nextUrl.pathname === "/maintenance") return NextResponse.next();
  return NextResponse.redirect(new URL("/maintenance", request.url));
}
export const config = { matcher: ["/((?!api).*)"] };
`,
	})
	rep, err := ScanNext(root)
	if err != nil {
		t.Fatal(err)
	}
	mw := findPlan(rep, "src/middleware.ts")
	if mw == nil || mw.Kind != KindMiddleware || mw.Source != "src/proxy.ts" {
		t.Fatalf("proxy was not planned as middleware: %+v", mw)
	}
	body := string(mw.Content)
	if strings.Contains(body, "function proxy") || !strings.Contains(body, "function __krateNextMiddleware") || !strings.Contains(body, "export function middleware") {
		t.Errorf("proxy export was not renamed and wrapped:\n%s", body)
	}
}

func TestProxyDefaultExportAndRootProxy(t *testing.T) {
	root := gapsProject(t, map[string]string{
		"app/page.tsx": `export default function P() { return <main/>; }`,
		"proxy.js":     `export default function (request) { return Response.redirect("https://x.test/"); }`,
	})
	rep, err := ScanNext(root)
	if err != nil {
		t.Fatal(err)
	}
	if mw := findPlan(rep, "middleware.js"); mw == nil || mw.Source != "proxy.js" {
		t.Fatalf("root proxy.js not planned as middleware.js: %+v", rep.Files)
	}
}

func TestProxyAndMiddlewareBothExistIsReported(t *testing.T) {
	root := gapsProject(t, map[string]string{
		"app/page.tsx":  `export default function P() { return <main/>; }`,
		"proxy.ts":      `export function proxy(request: Request) { return new Response("p"); }`,
		"middleware.ts": `export function middleware(request: Request) { return new Response("m"); }`,
	})
	rep, err := ScanNext(root)
	if err != nil {
		t.Fatal(err)
	}
	if !hasManual(rep, "middleware.ts", "both exist") {
		t.Errorf("both-exist case not reported: %+v", rep.Manual)
	}
	var sources []string
	for _, f := range rep.Files {
		if f.Kind == KindMiddleware {
			sources = append(sources, f.Source)
		}
	}
	if len(sources) != 1 || sources[0] != "proxy.ts" {
		t.Errorf("middleware sources = %v, want only proxy.ts", sources)
	}
}

func TestProxyArrowExportIsManual(t *testing.T) {
	root := gapsProject(t, map[string]string{
		"app/page.tsx": `export default function P() { return <main/>; }`,
		"proxy.ts":     `export const proxy = (request: Request) => new Response("x");`,
	})
	rep, _ := ScanNext(root)
	if !hasManual(rep, "proxy.ts", "arrow-function proxy") {
		t.Errorf("arrow proxy not flagged: %+v", rep.Manual)
	}
}

func TestUnmigratedRootSourcesAreListed(t *testing.T) {
	root := gapsProject(t, map[string]string{
		"app/page.tsx":       `export default function P() { return <main/>; }`,
		"auth.ts":            `export const auth = 1;`,
		"src/env.ts":         `export const env = 1;`,
		"next.config.mjs":    `export default {};`,
		"eslint.config.mjs":  `export default [];`,
		"next-env.d.ts":      ``,
		"vitest.setup.ts":    ``,
		"src/a.test.ts":      ``,
		"instrumentation.ts": `export function register() {}`,
	})
	rep, err := ScanNext(root)
	if err != nil {
		t.Fatal(err)
	}
	var found string
	for _, w := range rep.Warnings {
		if strings.Contains(w, "were not migrated and stay where they are") {
			found = w
		}
	}
	if !strings.Contains(found, "auth.ts") || !strings.Contains(found, "src/env.ts") {
		t.Fatalf("auth.ts / src/env.ts not listed: %q", found)
	}
	for _, noise := range []string{"next.config", "eslint.config", "next-env", "vitest.setup", "a.test", "instrumentation"} {
		if strings.Contains(found, noise) {
			t.Errorf("warning lists tooling file %s: %q", noise, found)
		}
	}
}

func TestInstrumentationFilesAreManual(t *testing.T) {
	root := gapsProject(t, map[string]string{
		"src/app/page.tsx":              `export default function P() { return <main/>; }`,
		"src/instrumentation.ts":        `export async function register() { await import("./instrumentation-node"); }`,
		"src/instrumentation-node.ts":   `export const x = 1;`,
		"src/instrumentation-client.ts": `export const y = 1;`,
	})
	rep, err := ScanNext(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, src := range []string{"src/instrumentation.ts", "src/instrumentation-node.ts", "src/instrumentation-client.ts"} {
		if !hasManual(rep, src, "instrumentation file") {
			t.Errorf("%s not listed as manual: %+v", src, rep.Manual)
		}
	}
	if !hasManualHint(rep, "src/instrumentation.ts", "register()") {
		t.Errorf("instrumentation hint should mention register(): %+v", rep.Manual)
	}
}

func TestStaticIconsCopiedAndLinked(t *testing.T) {
	root := gapsProject(t, map[string]string{
		"src/app/page.tsx":       `export default function P() { return <main/>; }`,
		"src/app/icon.svg":       `<svg xmlns="http://www.w3.org/2000/svg"/>`,
		"src/app/apple-icon.png": "PNG",
		"src/app/favicon.ico":    "ICO",
		"src/app/blog/icon.png":  "PNG2",
		"src/app/blog/page.tsx":  `export default function P() { return <main/>; }`,
	})
	rep, err := ScanNext(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{"public/icon.svg", "public/apple-icon.png", "public/favicon.ico", "public/blog/icon.png"} {
		if f := findPlan(rep, target); f == nil || f.Kind != KindPublic {
			t.Errorf("%s not planned as a public file", target)
		}
	}
	cfg := findPlan(rep, "krate.config.ts")
	if cfg == nil {
		t.Fatal("no krate.config.ts")
	}
	body := string(cfg.Content)
	if !strings.Contains(body, `rel=\"icon\" href=\"/icon.svg\" type=\"image/svg+xml\"`) || !strings.Contains(body, `rel=\"apple-touch-icon\" href=\"/apple-icon.png\"`) {
		t.Errorf("head links missing from config:\n%s", body)
	}
	if strings.Contains(body, "blog/icon.png") {
		t.Errorf("segment icon must not be linked site-wide:\n%s", body)
	}
	if !hasWarning(rep, "only app-root icons are linked") {
		t.Errorf("segment icon not warned about: %v", rep.Warnings)
	}
}

func TestDynamicIconStaysManual(t *testing.T) {
	root := gapsProject(t, map[string]string{
		"app/page.tsx": `export default function P() { return <main/>; }`,
		"app/icon.tsx": `export default function Icon() { return null; }`,
	})
	rep, _ := ScanNext(root)
	if !hasManual(rep, "app/icon.tsx", "not migrated") {
		t.Errorf("dynamic icon not manual: %+v", rep.Manual)
	}
}

func TestRouteHandlerOutsideAPIGetsRewrite(t *testing.T) {
	root := gapsProject(t, map[string]string{
		"app/page.tsx":                          `export default function P() { return <main/>; }`,
		"app/r/[code]/route.ts":                 `export function GET() { return new Response("x"); }`,
		"app/files/[...path]/route.ts":          `export function GET() { return new Response("x"); }`,
		"app/og/route.ts":                       `export function GET() { return new Response("x"); }`,
		"app/api/hello/route.ts":                `export function GET() { return new Response("x"); }`,
		"app/.well-known/security.txt/route.ts": `export function GET() { return new Response("x"); }`,
	})
	rep, err := ScanNext(root)
	if err != nil {
		t.Fatal(err)
	}
	cfg := findPlan(rep, "krate.config.ts")
	if cfg == nil {
		t.Fatal("no config")
	}
	body := string(cfg.Content)
	for _, want := range []string{
		`"source": "/r/:code"`, `"destination": "/api/r/:code"`,
		`"source": "/files/:path+"`, `"destination": "/api/files/:path+"`,
		`"source": "/og"`, `"destination": "/api/og"`,
		`"source": "/.well-known/security.txt"`, `"destination": "/api/.well-known/security.txt"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("config lacks %s:\n%s", want, body)
		}
	}
	if strings.Contains(body, `"source": "/hello"`) {
		t.Errorf("app/api routes need no rewrite:\n%s", body)
	}
	for _, m := range rep.Manual {
		if strings.Contains(m.Reason, "route handler outside app/api") {
			t.Errorf("rewritten route still listed as manual: %+v", m)
		}
	}
}

func TestRewriteSegments(t *testing.T) {
	cases := map[string]string{
		"r/[code]":         "r/:code",
		"a/[...rest]":      "a/:rest+",
		"a/[[...rest]]":    "a/:rest*",
		"plain/route":      "plain/route",
		"[org]/x/[repo]/y": ":org/x/:repo/y",
	}
	for in, want := range cases {
		if got := strings.Join(rewriteSegments(in), "/"); got != want {
			t.Errorf("rewriteSegments(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRouteRewriteMergesIntoExistingConfig(t *testing.T) {
	root := gapsProject(t, map[string]string{
		"app/page.tsx":    `export default function P() { return <main/>; }`,
		"app/og/route.ts": `export function GET() { return new Response("x"); }`,
		"krate.config.ts": "import { defineConfig } from '@krate/core';\nexport default defineConfig({ outDir: 'dist' });\n",
	})
	rep, err := ScanNext(root)
	if err != nil {
		t.Fatal(err)
	}
	cfg := findPlan(rep, "krate.config.ts")
	if cfg == nil || !strings.Contains(string(cfg.Content), `"/api/og"`) {
		t.Fatalf("rewrite not merged into the existing config: %+v", cfg)
	}
}

func TestGeneratedConfigEntry(t *testing.T) {
	root := gapsProject(t, map[string]string{
		"src/app/(app)/account/page.tsx": `export default function P() { return <main/>; }`,
	})
	rep, _ := ScanNext(root)
	cfg := findPlan(rep, "krate.config.ts")
	if cfg == nil {
		t.Fatal("no config")
	}
	if strings.Contains(string(cfg.Content), `"entry"`) {
		t.Errorf("entry must be omitted without an index page:\n%s", cfg.Content)
	}
	root = gapsProject(t, map[string]string{
		"src/app/page.tsx":               `export default function P() { return <main/>; }`,
		"src/app/(app)/account/page.tsx": `export default function P() { return <main/>; }`,
	})
	rep, _ = ScanNext(root)
	cfg = findPlan(rep, "krate.config.ts")
	if !strings.Contains(string(cfg.Content), `"entry": "src/pages/index.tsx"`) {
		t.Errorf("entry should be the index page:\n%s", cfg.Content)
	}
}

func TestInlineUseServerIsFlagged(t *testing.T) {
	root := gapsProject(t, map[string]string{
		"app/page.tsx": `import { SignOut } from "./sign-out";
export default function P() { return <SignOut />; }`,
		"app/sign-out.tsx": `export function SignOut() {
  async function signOutAction() {
    'use server'
    await signOut();
  }
  return <form action={signOutAction}><button>out</button></form>;
}`,
		"app/actions.ts":  "'use server'\nexport async function ok() {}\n",
		"app/strings.tsx": "export const s = \"'use server' in a string\"; // 'use server' in a comment\nexport default function X() { return <p>{s}</p>; }",
	})
	rep, err := ScanNext(root)
	if err != nil {
		t.Fatal(err)
	}
	if !hasManual(rep, "app/sign-out.tsx", "KR7001") {
		t.Errorf("inline use server not flagged: %+v", rep.Manual)
	}
	if !hasManualHint(rep, "app/sign-out.tsx", `first statement is "use server"`) {
		t.Errorf("hint missing: %+v", rep.Manual)
	}
	for _, m := range rep.Manual {
		if strings.Contains(m.Reason, "KR7001") && (m.Source == "app/actions.ts" || m.Source == "app/strings.tsx") {
			t.Errorf("false positive: %+v", m)
		}
	}
}

func TestApplyMovesOriginalTreeAsideAndRollbackRestores(t *testing.T) {
	root := gapsProject(t, map[string]string{
		"src/app/page.tsx":        `export default function P() { return <main/>; }`,
		"src/app/actions.ts":      "'use server'\nexport async function a() {}\n",
		"src/app/notes.txt":       "keep me",
		"src/lib/util.ts":         `export const u = 1;`,
		"src/app/actions.test.ts": `import { a } from "./actions"; import P from "./page"; export const t = [a, P];`,
	})
	rep, err := ScanNext(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.MoveAside) != 1 || rep.MoveAside[0] != "src/app" {
		t.Fatalf("MoveAside = %v", rep.MoveAside)
	}
	if !hasWarning(rep, "notes.txt") {
		t.Errorf("unmigrated file in a moved tree not warned about: %v", rep.Warnings)
	}
	// The copied test is re-pointed at the migrated page.
	test := findPlan(rep, "src/migrated/app/actions.test.ts")
	if test == nil || !strings.Contains(string(test.Content), `"../../pages/index"`) {
		t.Fatalf("test import of ./page not rewritten: %+v", test)
	}
	res, err := Apply(rep, ApplyOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "src", "app")); !os.IsNotExist(err) {
		t.Errorf("src/app should have been moved aside, stat err = %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "src", "lib", "util.ts")); err != nil {
		t.Errorf("unrelated src/lib was touched: %v", err)
	}
	if _, err := os.Stat(filepath.Join(res.BackupDir, "moved", "src", "app", "notes.txt")); err != nil {
		t.Errorf("moved tree missing from the backup: %v", err)
	}
	if len(res.Moved) != 1 {
		t.Errorf("Moved = %v", res.Moved)
	}
	if _, err := os.Stat(filepath.Join(root, "src", "pages", "index.tsx")); err != nil {
		t.Fatalf("migrated page missing: %v", err)
	}
	if err := Rollback(res.BackupDir); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(filepath.Join(root, "src", "app", "notes.txt")); err != nil || string(b) != "keep me" {
		t.Errorf("rollback did not restore src/app: %v %q", err, b)
	}
	if _, err := os.Stat(filepath.Join(root, "src", "pages", "index.tsx")); !os.IsNotExist(err) {
		t.Errorf("rollback left the migrated page behind: %v", err)
	}
}

func TestApplyKeepsOriginalTreeWhenAWriteFails(t *testing.T) {
	root := gapsProject(t, map[string]string{
		"app/page.tsx":       `export default function P() { return <main/>; }`,
		"app/about/page.tsx": `export default function P() { return <main/>; }`,
	})
	// A regular file where a directory is needed makes a write fail.
	if err := os.MkdirAll(filepath.Join(root, "src"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "src", "pages"), []byte("file"), 0644); err != nil {
		t.Fatal(err)
	}
	rep, err := ScanNext(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Apply(rep, ApplyOptions{}); err == nil {
		t.Fatal("expected the apply to fail")
	}
	if _, err := os.Stat(filepath.Join(root, "app", "page.tsx")); err != nil {
		t.Errorf("original tree must stay when apply fails: %v", err)
	}
}

func TestMoveAsideSkippedWhenTargetsLiveInsideTheTree(t *testing.T) {
	root := t.TempDir()
	rep := &Report{Root: root, Files: []PlannedFile{{Kind: KindPage, Source: "src/app/x.tsx", Target: "src/pages/y.tsx"}}}
	planMoveAside(root, []string{"src/pages"}, rep)
	if len(rep.MoveAside) != 0 || !hasWarning(rep, "also holds migrated output") {
		t.Errorf("tree with migrated output must stay: %+v %v", rep.MoveAside, rep.Warnings)
	}
}
