package inlineaction

import (
	"strings"
	"testing"
)

func testOpts() Options {
	return Options{
		Path: "src/pages/account.tsx", Rel: "src/pages/account.tsx", Dir: "/proj/src/pages",
		Call: func(a *Action) string {
			return `__act("` + a.ID + `", [` + strings.Join(a.Captures, ", ") + `])`
		},
	}
}

func TestHoistDeclarationCapturesLocals(t *testing.T) {
	src := `import { db } from "../lib/db";
import { redirect } from "@krate/runtime/server";
import Unused from "./unused";
const TABLE = "projects";
function audit(id: string) { return TABLE + id; }

export default async function Page({ params }: { params: Promise<{ id: string }> }) {
  const { id } = await params;
  const secret = process.env.TOKEN;
  async function rename(formData: FormData) {
    'use server';
    const name = String(formData.get("name"));
    await db.update(audit(id), name, secret);
    redirect("/projects/" + id);
  }
  return <form action={rename}><input name="name" /></form>;
}
`
	res, err := Hoist(src, testOpts())
	if err != nil {
		t.Fatal(err)
	}
	if res == nil || len(res.Actions) != 1 {
		t.Fatalf("want one action, got %+v", res)
	}
	a := res.Actions[0]
	if a.Name != "rename" || !a.EnclosingAsync {
		t.Errorf("action: %+v", a)
	}
	if got := strings.Join(a.Captures, ","); got != "id,secret" {
		t.Errorf("captures = %q, want id,secret", got)
	}
	for _, want := range []string{
		`import { db } from "/proj/src/lib/db";`,
		`import { redirect } from "@krate/runtime/server";`,
		`const TABLE = "projects";`,
		`function audit(id: string)`,
		`export const inline_0_rename = async function rename(id, secret, formData: FormData) {`,
	} {
		if !strings.Contains(res.Module, want) {
			t.Errorf("module missing %q:\n%s", want, res.Module)
		}
	}
	if strings.Contains(res.Module, "Unused") {
		t.Errorf("unused import copied:\n%s", res.Module)
	}
	if !strings.Contains(res.Source, `const rename = __act("`+a.ID+`", [id, secret]);`) {
		t.Errorf("source not rewritten:\n%s", res.Source)
	}
	if strings.Count(res.Source, "\n") != strings.Count(src, "\n") {
		t.Errorf("line count changed")
	}
	if strings.Contains(res.Source, "use server") {
		t.Errorf("directive left in source:\n%s", res.Source)
	}
}

func TestHoistArrowInJSXAttribute(t *testing.T) {
	src := `import { signOut } from "../auth";
export function SignOut() {
  return (
    <form action={async () => { "use server"; await signOut({ redirectTo: "/" }); }}>
      <button>Sign out</button>
    </form>
  );
}
export async function SignIn({ provider }: { provider: string }) {
  return <form action={async (fd) => { "use server"; await signOut(provider, fd); }} />;
}
`
	res, err := Hoist(src, testOpts())
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Actions) != 2 {
		t.Fatalf("want two actions, got %d", len(res.Actions))
	}
	if len(res.Actions[0].Captures) != 0 || res.Actions[0].EnclosingAsync {
		t.Errorf("sign out: %+v", res.Actions[0])
	}
	if got := strings.Join(res.Actions[1].Captures, ","); got != "provider" {
		t.Errorf("sign in captures = %q", got)
	}
	if !strings.Contains(res.Source, `<form action={__act("`+res.Actions[0].ID+`", [])}>`) {
		t.Errorf("arrow not replaced:\n%s", res.Source)
	}
	if !strings.Contains(res.Module, `export const inline_1 = async (provider, fd) => {`) {
		t.Errorf("module:\n%s", res.Module)
	}
	if !strings.Contains(res.Module, `export const inline_0 = async () => {`) {
		t.Errorf("module:\n%s", res.Module)
	}
}

func TestHoistRejections(t *testing.T) {
	cases := map[string]struct {
		src    string
		client bool
		want   string
	}{
		"client":           {`export default function C() { return <form action={async () => { "use server"; }} />; }`, true, `"use client"`},
		"class method":     {`export class A { async m() { "use server"; } }`, false, "class method"},
		"this class arrow": {`export class A { render() { return <form action={() => { "use server"; this.x(); }} />; } }`, false, `"this"`},
		"this arrow":       {`export default function C() { const o = { f() { return () => { "use server"; this.x(); }; } }; return null; }`, false, `"this"`},
	}
	for name, c := range cases {
		o := testOpts()
		o.Client = c.client
		_, err := Hoist(c.src, o)
		if err == nil || !strings.Contains(err.Error(), "KR7001") || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: want a KR7001 error mentioning %q, got %v", name, c.want, err)
		}
	}
}

func TestHoistIgnoresActionModulesAndPlainFiles(t *testing.T) {
	for _, src := range []string{
		"\"use server\";\nexport async function a() {}\n",
		"export const s = 'use server';\n",
		"export default function P() { return null; }\n",
	} {
		if res, err := Hoist(src, testOpts()); res != nil || err != nil {
			t.Errorf("%q: got %+v, %v", src, res, err)
		}
	}
}
