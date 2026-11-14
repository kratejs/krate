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
console.log(JSON.stringify({
  status: res.status,
  location: res.headers.get("location"),
  setCookie: res.headers.getSetCookie(),
  get: res.cookies.get("session"),
  has: res.cookies.has("theme"),
  all: res.cookies.getAll().length,
  jsonStatus: json.status,
  jsonCookie: json.headers.getSetCookie(),
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
	out, ok := rewriteNextDynamic(dyn)
	if !ok || !strings.HasPrefix(out, "// header\n\"use client\";\n'use strict';\nimport { lazy } from \"react\";\n") {
		t.Errorf("lazy import is not below the directives (ok=%v):\n%s", ok, out)
	}
}
