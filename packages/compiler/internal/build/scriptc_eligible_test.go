package build

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
	"github.com/kratejs/krate/packages/compiler/internal/diag"
)

// Each use of something the scriptc server lacks is found with its position,
// in the route or in a local module it imports.
func TestScriptcRouteFindings(t *testing.T) {
	cases := []struct {
		name  string
		files map[string]string
		file  string // module the finding is in
		line  int
		what  string
	}{
		{"krate import", map[string]string{"src/api/r.ts": "import { x } from '@krate/runtime';\nexport function GET() { return Response.json({ x }); }"}, "src/api/r.ts", 1, "@krate/runtime"},
		{"next import", map[string]string{"src/api/r.ts": "import { NextResponse } from 'next/server';\nexport function GET() { return NextResponse.json({}); }"}, "src/api/r.ts", 1, "next/server"},
		{"cookies member", map[string]string{"src/api/r.ts": "export function GET(request: Request) {\n  const c = request.cookies;\n  return Response.json({ c });\n}"}, "src/api/r.ts", 2, "request.cookies"},
		{"nextUrl member", map[string]string{"src/api/r.ts": "export function GET(request: any) {\n  return Response.json({ p: request.nextUrl.pathname });\n}"}, "src/api/r.ts", 2, "request.nextUrl"},
		{"after", map[string]string{"src/api/r.ts": "export function GET() {\n  after(() => {});\n  return Response.json({});\n}"}, "src/api/r.ts", 2, "after()"},
		{"revalidatePath", map[string]string{"src/api/r.ts": "export function POST() {\n  revalidatePath('/x');\n  return Response.json({});\n}"}, "src/api/r.ts", 2, "revalidatePath()"},
		{"revalidateTag", map[string]string{"src/api/r.ts": "export function POST() {\n  revalidateTag('x');\n  return Response.json({});\n}"}, "src/api/r.ts", 2, "revalidateTag()"},
		{"cookies()", map[string]string{"src/api/r.ts": "export function GET() {\n  const j = cookies();\n  return Response.json({ j });\n}"}, "src/api/r.ts", 2, "cookies()"},
		{"headers()", map[string]string{"src/api/r.ts": "export function GET() {\n  const h = headers();\n  return Response.json({ h });\n}"}, "src/api/r.ts", 2, "headers()"},
		{"draftMode()", map[string]string{"src/api/r.ts": "export function GET() {\n  const d = draftMode();\n  return Response.json({ d });\n}"}, "src/api/r.ts", 2, "draftMode()"},
		{"transitive", map[string]string{
			"src/api/r.ts": "import { h } from '../lib/a';\nexport function GET() { return Response.json({ h }); }",
			"src/lib/a.ts": "export { h } from './b.js';",
			"src/lib/b.ts": "export const h = 1;\nexport function f(r: Request) {\n  return r.nextUrl;\n}",
		}, "src/lib/b.ts", 3, "request.nextUrl"},
		{"transitive import", map[string]string{
			"src/api/r.ts":     "import { h } from '../lib/index';\nexport function GET() { return Response.json({ h }); }",
			"src/lib/index.ts": "import { redirect } from 'next/navigation';\nexport const h = redirect;",
		}, "src/lib/index.ts", 1, "next/navigation"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			root := t.TempDir()
			for rel, src := range c.files {
				p := filepath.Join(root, filepath.FromSlash(rel))
				if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			f := scriptcRouteFinding(root, filepath.Join(root, "src/api/r.ts"))
			if f == nil {
				t.Fatal("no finding")
			}
			if rel, _ := filepath.Rel(root, f.file); filepath.ToSlash(rel) != c.file || f.line != c.line || f.col < 1 {
				t.Errorf("finding at %s:%d:%d, want %s:%d", filepath.ToSlash(rel), f.line, f.col, c.file, c.line)
			}
			if !strings.Contains(f.what, c.what) {
				t.Errorf("what = %q, want it to name %q", f.what, c.what)
			}
		})
	}
}

func TestScriptcRouteFindingCleanRoute(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{
		"src/api/r.ts":     "import { greet } from '../lib/greet';\nimport { createHash } from 'node:crypto';\nexport function GET(request: Request) {\n  const u = new URL(request.url);\n  const c = request.headers.get('cookie');\n  return Response.json({ g: greet(u.searchParams.get('n') ?? 'x'), c });\n}",
		"src/lib/greet.ts": "export function greet(n: string) { return 'hi ' + n; }",
	}
	for rel, src := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if f := scriptcRouteFinding(root, filepath.Join(root, "src/api/r.ts")); f != nil {
		t.Errorf("a clean route has a finding: %+v", f)
	}
	// An import cycle ends.
	_ = os.WriteFile(filepath.Join(root, "src/lib/greet.ts"), []byte("import './greet';\nexport const g = 1;"), 0o644)
	if f := scriptcRouteFinding(root, filepath.Join(root, "src/api/r.ts")); f != nil {
		t.Errorf("cycle: %+v", f)
	}
}

func eligibilityFiles() map[string]string {
	return map[string]string{
		"package.json":         `{"type":"module"}`,
		"src/api/clean.ts":     "export function GET() { return Response.json({ ok: true }); }",
		"src/api/cookie.ts":    "export function GET(request: any) {\n  return Response.json({ c: request.cookies.get('a') });\n}",
		"src/api/viaimport.ts": "import { h } from '../lib/h';\nexport function GET() { return Response.json({ h }); }",
		"src/lib/h.ts":         "export const h = revalidateTag;\nexport function f() {\n  revalidateTag('x');\n}",
	}
}

func TestScriptcFindingsFallBackWithPosition(t *testing.T) {
	logPath := fakeScriptcOnPath(t)
	b, root := scriptcBuilder(t, eligibilityFiles(), nil)
	if err := b.CompileAPIRoutes(routeSources(t, root)); err != nil {
		t.Fatal(err)
	}
	var err error
	stderr := captureStderr(t, func() { err = b.buildScriptcAPI() })
	if err != nil {
		t.Fatal(err)
	}
	m := readScriptcManifest(t, root)
	if m["clean.js"].Runtime != scriptcRouteScriptc || m["cookie.js"].Runtime != scriptcRouteQuickJS || m["viaimport.js"].Runtime != scriptcRouteQuickJS {
		t.Errorf("manifest = %+v", m)
	}
	for _, frag := range []string{"KR7238", "src/api/cookie.ts:2:", "request.cookies", "src/lib/h.ts:3:", "revalidateTag()", "imported by src/api/viaimport.ts"} {
		if !strings.Contains(stderr, frag) {
			t.Errorf("stderr lacks %q:\n%s", frag, stderr)
		}
	}
	// Only the clean route reaches coverage.
	entry, _ := os.ReadFile(filepath.Join(root, ".krate", "api-scriptc", "main.ts"))
	if !strings.Contains(string(entry), "clean.ts") || strings.Contains(string(entry), "cookie.ts") || strings.Contains(string(entry), "viaimport.ts") {
		t.Errorf("entry:\n%s", entry)
	}
	_ = logPath
}

func TestScriptcFindingsAreErrorsWhenStrict(t *testing.T) {
	fakeScriptcOnPath(t)
	b, root := scriptcBuilder(t, eligibilityFiles(), func(c *config.Config) { c.SSR.ScriptcStrict = true })
	if err := b.CompileAPIRoutes(routeSources(t, root)); err != nil {
		t.Fatal(err)
	}
	err := b.buildScriptcAPI()
	if err == nil {
		t.Fatal("strict mode must fail")
	}
	text := diag.FormatDiagnostics(unwrapJoined(err))
	for _, frag := range []string{"KR7238", "src/api/cookie.ts:2", "src/lib/h.ts:3", "ssr.scriptcStrict"} {
		if !strings.Contains(text, frag) {
			t.Errorf("error lacks %q:\n%s", frag, text)
		}
	}
}
