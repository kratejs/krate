package migrate

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/evanw/esbuild/pkg/api"
)

// TestNextResponseShimRuns executes the generated NextResponse shim on Node and
// checks the Set-Cookie headers and the cookies accessor it produces.
func TestNextResponseShimRuns(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed")
	}
	src := nextResponseShim + `
const res = NextResponse.redirect(new URL("/home", "http://x.test"), 302);
res.cookies.set("session", "a b", { httpOnly: true, maxAge: 60, sameSite: "lax" });
res.cookies.set({ name: "theme", value: "dark", path: "/app" });
res.cookies.delete("old");
const json = NextResponse.json({ ok: true }, { status: 201 });
json.cookies.set("x", "1");
// new NextResponse(...) keeps cookies (it used to become a bare Response).
const made = new NextResponse(null, { status: 303, headers: { Location: "/templates" } });
made.cookies.set("dd_ref", "ABC", { httpOnly: true, sameSite: "lax", maxAge: 2592000 });
made.headers.set("X-Robots-Tag", "noindex");
console.log(JSON.stringify({
  status: res.status,
  location: res.headers.get("location"),
  setCookie: res.headers.getSetCookie(),
  get: res.cookies.get("session"),
  has: res.cookies.has("theme"),
  all: res.cookies.getAll().length,
  jsonStatus: json.status,
  jsonCookie: json.headers.getSetCookie(),
  jsonType: json.headers.get("content-type"),
  jsonBody: await json.text(),
  made: [made.status, made.headers.get("location"), made.headers.get("x-robots-tag"), made instanceof Response, ...made.headers.getSetCookie()],
}));
`
	out := api.Transform(src, api.TransformOptions{Loader: api.LoaderTS, Format: api.FormatESModule})
	if len(out.Errors) > 0 {
		t.Fatalf("shim does not compile: %s", out.Errors[0].Text)
	}
	file := filepath.Join(t.TempDir(), "shim.mjs")
	if err := os.WriteFile(file, out.Code, 0644); err != nil {
		t.Fatal(err)
	}
	got, err := exec.Command(node, file).CombinedOutput()
	if err != nil {
		t.Fatalf("node: %v\n%s", err, got)
	}
	for _, want := range []string{
		`"status":302`,
		`"location":"http://x.test/home"`,
		`session=a%20b; Path=/; Max-Age=60; HttpOnly; SameSite=Lax`,
		`theme=dark; Path=/app`,
		`old=; Path=/; Expires=Thu, 01 Jan 1970 00:00:00 GMT; Max-Age=0`,
		`"get":{"name":"session","value":"a b"}`,
		`"has":true`,
		`"all":3`,
		`"jsonStatus":201`,
		`"jsonType":"application/json"`,
		`"jsonBody":"{\"ok\":true}"`,
		`"made":[303,"/templates","noindex",true,"dd_ref=ABC; Path=/; Max-Age=2592000; HttpOnly; SameSite=Lax"]`,
	} {
		if !strings.Contains(string(got), want) {
			t.Errorf("output missing %q:\n%s", want, got)
		}
	}
}

// TestMigratedImportsStayBelowDirectives checks that imports the migrator adds
// land after the directive prologue, so "use server" stays the first statement.
func TestMigratedImportsStayBelowDirectives(t *testing.T) {
	actions := "'use server';\nimport { after } from 'next/server';\nimport { db } from '../db';\nexport async function markRead(id: string) { after(() => log(id)); await db.markRead(id); }\n"
	out, _, _ := rewriteAPIServerSource(actions)
	if !strings.HasPrefix(out, "'use server';\nimport { after } from \"@krate/runtime/server\";") {
		t.Errorf("after import is not below the directive:\n%s", out)
	}

	dyn := "// header\n\"use client\";\n'use strict';\nimport dynamic from 'next/dynamic';\nconst Chart = dynamic(() => import('./chart'));\nexport default Chart;\n"
	out, _, ok := rewriteNextDynamic(dyn, true)
	if !ok || !strings.HasPrefix(out, "// header\n\"use client\";\n'use strict';\nimport { lazy } from \"react\";\n") {
		t.Errorf("lazy import is not below the directives (ok=%v):\n%s", ok, out)
	}

	named := "import dynamic from 'next/dynamic';\nconst A = dynamic(() => import('./A').then((m) => m.A), { ssr: true });\nconst B = dynamic(() => import('./B').then(m => ({ default: m.B })), { ssr: true });\n"
	out, _, ok = rewriteNextDynamic(named, true)
	if !ok || !strings.Contains(out, "const A = lazy(() => import('./A').then((m) => ({ default: m.A })));\n") ||
		!strings.Contains(out, "const B = lazy(() => import('./B').then(m => ({ default: m.B })));\n") {
		t.Errorf("named-export dynamic() loaders not normalized (ok=%v):\n%s", ok, out)
	}
}

// TestNewNextResponseKeepsCookies: a route handler that constructs
// NextResponse keeps it (with the class shim), so res.cookies works.
func TestNewNextResponseKeepsCookies(t *testing.T) {
	src := `import { NextResponse } from 'next/server';
export async function GET(req: Request) {
  const res = new NextResponse(null, { status: 303, headers: { Location: '/templates' } });
  res.cookies.set('dd_ref', 'X', { httpOnly: true });
  return res;
}
`
	out, _, manual := rewriteAPIServerSource(src)
	if len(manual) > 0 {
		t.Fatalf("unexpected manual items: %v", manual)
	}
	for _, want := range []string{"class NextResponse extends Response", "new NextResponse(null, { status: 303"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "new Response(null, { status: 303") {
		t.Errorf("new NextResponse was rewritten to a bare Response:\n%s", out)
	}
}

// TestNextResponseShimsTypecheckStrict: the shims the migrator inlines pass a
// strict project's typecheck (strict, noUncheckedIndexedAccess), including
// res.cookies on a constructed NextResponse.
func TestNextResponseShimsTypecheckStrict(t *testing.T) {
	tsc, _ := filepath.Abs(filepath.Join("..", "..", "..", "runtime", "node_modules", ".bin", "tsc"))
	if _, err := os.Stat(tsc); err != nil {
		t.Skip("typescript is not installed (run pnpm install)")
	}
	dir := t.TempDir()
	files := map[string]string{
		"route.ts": nextResponseShim + `
export function GET(): Response {
  const res = new NextResponse(null, { status: 303, headers: { Location: "/t" } });
  res.cookies.set("a", "1", { httpOnly: true, sameSite: "lax", maxAge: 60 });
  res.cookies.set({ name: "b", value: "2", path: "/x" });
  res.cookies.delete("c");
  const got: string | undefined = res.cookies.get("a")?.value;
  return got ? NextResponse.json({ got }) : NextResponse.redirect("/x", 302);
}
`,
		"middleware.ts": middlewareResponseShim + `
export function middleware(): Response {
  const res = NextResponse.next({ request: { headers: { "x-a": "1" } } });
  res.cookies.set("m", "1");
  return res;
}
`,
		"tsconfig.json": `{"compilerOptions":{"strict":true,"noUncheckedIndexedAccess":true,"noEmit":true,"target":"ES2022","module":"ESNext","moduleResolution":"bundler","lib":["ES2022","DOM","DOM.Iterable"],"skipLibCheck":true,"types":[]},"files":["route.ts","middleware.ts"]}`,
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command(tsc, "-p", dir)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("tsc: %v\n%s", err, out)
	}
}

// TestMigratedMiddlewareCookieJarDecodes runs a migrated middleware that reads
// request.cookies on Node. As in Next.js the value is the decoded one, so a
// cookie that NextResponse.cookies.set wrote percent-encoded
// ("mode%3Dlight%26nav%3Dopen") reads back as "mode=light&nav=open", and a
// value that holds "=" is not cut short.
func TestMigratedMiddlewareCookieJarDecodes(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed")
	}
	src := `import { NextRequest, NextResponse } from 'next/server';
export function middleware(request: NextRequest) {
  return NextResponse.json({
    ui: request.cookies.get('ui')?.value,
    all: request.cookies.getAll().map((c) => c.name + '=' + c.value),
  });
}
`
	out, _, manual := rewriteNextMiddleware(src)
	if len(manual) > 0 {
		t.Fatalf("manual steps: %v", manual)
	}
	out += `
for (const cookie of ["ui=mode=light&nav=open", "ui=mode%3Dlight%26nav%3Dopen", 'x=1; ui="mode=light&nav=open"; y=%E0%A4%A', "ui=a%2"]) {
  const res = await middleware(new Request("http://x.test/", { headers: { cookie } }));
  console.log(JSON.stringify(await res.json()));
}
const set = NextResponse.json({});
set.cookies.set("ui", "mode=light&nav=open");
console.log(JSON.stringify(set.headers.getSetCookie()));
`
	res := api.Transform(out, api.TransformOptions{Loader: api.LoaderTS, Format: api.FormatESModule})
	if len(res.Errors) > 0 {
		t.Fatalf("migrated middleware does not compile: %s\n%s", res.Errors[0].Text, out)
	}
	file := filepath.Join(t.TempDir(), "mw.mjs")
	if err := os.WriteFile(file, res.Code, 0644); err != nil {
		t.Fatal(err)
	}
	got, err := exec.Command(node, file).CombinedOutput()
	if err != nil {
		t.Fatalf("node: %v\n%s", err, got)
	}
	for _, want := range []string{
		`{"ui":"mode=light&nav=open","all":["ui=mode=light&nav=open"]}` + "\n" + `{"ui":"mode=light&nav=open","all":["ui=mode=light&nav=open"]}`,
		`{"ui":"mode=light&nav=open","all":["x=1","ui=mode=light&nav=open","y=%E0%A4%A"]}`,
		`{"ui":"a%2","all":["ui=a%2"]}`,
		`["ui=mode%3Dlight%26nav%3Dopen; Path=/"]`,
	} {
		if !strings.Contains(string(got), want) {
			t.Errorf("output missing %q:\n%s", want, got)
		}
	}
}
