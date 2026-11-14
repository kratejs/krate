package migrate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func countPlans(rep *Report, target string) int {
	n := 0
	for _, f := range rep.Files {
		if f.Target == target {
			n++
		}
	}
	return n
}

func aliasProject(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writeNextFile(t, root, "package.json", `{"dependencies":{"next":"16.0.0"}}`)
	writeNextFile(t, root, "tsconfig.base.json", `{
  // comments and trailing commas are fine
  "compilerOptions": { "baseUrl": ".", "paths": { "@/*": ["./src/*"], }, },
}`)
	writeNextFile(t, root, "tsconfig.json", `{"extends": "./tsconfig.base", "compilerOptions": {"strict": true}}`)
	writeNextFile(t, root, "src/app/page.tsx", `import Nav from "@/components/A";
export default function Page() { return <Nav />; }`)
	writeNextFile(t, root, "src/app/about/page.tsx", `import Nav from "@/components/A";
import "@/styles/site.css";
export default function About() { return <Nav />; }`)
	writeNextFile(t, root, "src/components/A.tsx", `"use client";
import Link from "next/link";
import { usePathname } from "next/navigation";
import { fmt } from "@/lib/b";
import logo from "./logo.svg";
export default function A() { return <Link href="/">{fmt(usePathname())}</Link>; }`)
	writeNextFile(t, root, "src/components/logo.svg", `<svg/>`)
	writeNextFile(t, root, "src/styles/site.css", `body{}`)
	writeNextFile(t, root, "src/lib/b.ts", `import { revalidatePath } from "next/cache";
import type { Metadata } from "next";
import { fmt as again } from "../components/A";
export const m: Metadata = {};
export function fmt(s: string) { revalidatePath("/"); return again ? s : s; }`)
	writeNextFile(t, root, "src/lib/untouched.ts", `export const x = 1;`)
	return root
}

func TestAliasChainModulesRewrittenInPlace(t *testing.T) {
	root := aliasProject(t)
	rep, err := ScanNext(root)
	if err != nil {
		t.Fatal(err)
	}
	a := findPlan(rep, "src/components/A.tsx")
	if a == nil || a.Kind != KindModule || a.Source != "src/components/A.tsx" {
		t.Fatalf("A.tsx not planned in place: %+v", a)
	}
	if countPlans(rep, "src/components/A.tsx") != 1 {
		t.Errorf("module reached from two routes must be planned once")
	}
	body := string(a.Content)
	if strings.Contains(body, "next/link") || strings.Contains(body, "next/navigation") {
		t.Errorf("next imports kept:\n%s", body)
	}
	if !strings.Contains(body, `{ usePathname } from "@krate/runtime"`) {
		t.Errorf("navigation not rewritten:\n%s", body)
	}
	if !strings.Contains(body, `"@/lib/b"`) || !strings.Contains(body, `"./logo.svg"`) {
		t.Errorf("alias and asset specifiers must be unchanged:\n%s", body)
	}
	b := findPlan(rep, "src/lib/b.ts")
	if b == nil {
		t.Fatal("b.ts not planned")
	}
	bb := string(b.Content)
	if strings.Contains(bb, `"next/cache"`) || strings.Contains(bb, `from "next"`) {
		t.Errorf("next/cache or next kept:\n%s", bb)
	}
	if !strings.Contains(bb, `@krate/runtime/server`) {
		t.Errorf("server module not used:\n%s", bb)
	}
	if findPlan(rep, "src/lib/untouched.ts") != nil || findPlan(rep, "src/styles/site.css") != nil || findPlan(rep, "src/components/logo.svg") != nil {
		t.Errorf("unreachable, unchanged or non-source files must not be planned")
	}
	if !a.Overwrites {
		t.Errorf("in-place modules overwrite their source and need --force")
	}
}

func TestInPlaceModulesApplyAndRollback(t *testing.T) {
	root := aliasProject(t)
	before, _ := os.ReadFile(filepath.Join(root, "src/components/A.tsx"))
	rep, err := ScanNext(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Apply(rep, ApplyOptions{}); err == nil {
		t.Fatal("expected a refusal without --force")
	}
	res, err := Apply(rep, ApplyOptions{Force: true})
	if err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(filepath.Join(root, "src/components/A.tsx"))
	if string(after) == string(before) || strings.Contains(string(after), "next/navigation") {
		t.Fatalf("module not rewritten on disk:\n%s", after)
	}
	if err := Rollback(res.BackupDir); err != nil {
		t.Fatal(err)
	}
	restored, _ := os.ReadFile(filepath.Join(root, "src/components/A.tsx"))
	if string(restored) != string(before) {
		t.Errorf("rollback did not restore the original:\n%s", restored)
	}
}

func TestRelativeImportOutsideAppIsRewritten(t *testing.T) {
	root := t.TempDir()
	writeNextFile(t, root, "package.json", `{"dependencies":{"next":"16.0.0"}}`)
	writeNextFile(t, root, "app/blog/page.tsx", `import { go } from "../../lib/go";
export default function P() { go(); return <p/>; }`)
	writeNextFile(t, root, "lib/go.ts", `import { redirect } from "next/navigation";
export function go() { redirect("/"); }`)
	rep, err := ScanNext(root)
	if err != nil {
		t.Fatal(err)
	}
	page := findPlan(rep, "src/pages/blog.tsx")
	if page == nil || !strings.Contains(string(page.Content), `"../../lib/go"`) {
		t.Fatalf("relative import not repointed at the in-place module: %+v", page)
	}
	for _, m := range rep.Manual {
		if strings.Contains(m.Reason, "no copied target") {
			t.Errorf("unexpected manual item: %+v", m)
		}
	}
	mod := findPlan(rep, "lib/go.ts")
	if mod == nil || !strings.Contains(string(mod.Content), `{ redirect } from "@krate/runtime/server"`) {
		t.Fatalf("module not rewritten: %+v", mod)
	}
}

func TestWorkspacePackageNextImportsAreManual(t *testing.T) {
	root := t.TempDir()
	pkgDir := filepath.Join(root, "packages", "ui")
	app := filepath.Join(root, "apps", "web")
	writeNextFile(t, pkgDir, "src/Button.tsx", `import Link from "next/link";
export const Button = () => <Link href="/"/>;`)
	writeNextFile(t, app, "package.json", `{"dependencies":{"next":"16.0.0"}}`)
	writeNextFile(t, app, "app/page.tsx", `import { Button } from "@acme/ui/src/Button";
export default function P() { return <Button/>; }`)
	if err := os.MkdirAll(filepath.Join(app, "node_modules", "@acme"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(pkgDir, filepath.Join(app, "node_modules", "@acme", "ui")); err != nil {
		t.Skip("symlinks unavailable: " + err.Error())
	}
	rep, err := ScanNext(app)
	if err != nil {
		t.Fatal(err)
	}
	if !hasManual(rep, "app/page.tsx", "workspace package @acme/ui imports next/*") {
		t.Errorf("workspace package not reported: %+v", rep.Manual)
	}
	for _, f := range rep.Files {
		if strings.Contains(f.Target, "Button") {
			t.Errorf("workspace file must not be planned: %+v", f)
		}
	}
}

func TestPublicEnvReadsRenamedWithoutValues(t *testing.T) {
	root := t.TempDir()
	writeNextFile(t, root, "package.json", `{"dependencies":{"next":"16.0.0"}}`)
	writeNextFile(t, root, ".env", "NEXT_PUBLIC_API_URL=http://localhost:3001\nSECRET_TOKEN=hunter2\n")
	writeNextFile(t, root, ".env.production", "NEXT_PUBLIC_API_URL=https://api.example.com\nNEXT_PUBLIC_UNUSED=zzz\n")
	writeNextFile(t, root, "tsconfig.json", `{"compilerOptions":{"paths":{"@/*":["./src/*"]}}}`)
	writeNextFile(t, root, "app/page.tsx", `import { api } from "@/lib/client";
export default function P() { return <p>{api}{process.env["NEXT_PUBLIC_API_URL"]}{process.env.NEXT_PUBLIC_MISSING}</p>; }`)
	writeNextFile(t, root, "src/lib/client.ts", `export const api = process.env.NEXT_PUBLIC_API_URL;
export const s = process.env.SECRET_TOKEN;`)
	rep, err := ScanNext(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range rep.Files {
		c := string(f.Content)
		if f.Kind == KindConfig || f.Kind == KindPackage {
			continue
		}
		for _, secret := range []string{"localhost:3001", "api.example.com", "hunter2", "zzz"} {
			if strings.Contains(c, secret) {
				t.Errorf("%s contains env value %q:\n%s", f.Target, secret, c)
			}
		}
	}
	mod := findPlan(rep, "src/lib/client.ts")
	if mod == nil || !strings.Contains(string(mod.Content), "process.env.KRATE_PUBLIC_API_URL") || !strings.Contains(string(mod.Content), "process.env.SECRET_TOKEN") {
		t.Fatalf("client module env reads wrong: %+v", mod)
	}
	page := string(findPlan(rep, "src/pages/index.tsx").Content)
	if !strings.Contains(page, "process.env.KRATE_PUBLIC_API_URL") || !strings.Contains(page, "process.env.KRATE_PUBLIC_MISSING") {
		t.Errorf("page env reads wrong:\n%s", page)
	}
	var item *ManualItem
	for i := range rep.Manual {
		if strings.Contains(rep.Manual[i].Reason, "public environment variables") {
			item = &rep.Manual[i]
		}
	}
	if item == nil {
		t.Fatalf("no env manual item: %+v", rep.Manual)
	}
	for _, want := range []string{"NEXT_PUBLIC_API_URL -> KRATE_PUBLIC_API_URL (defined in .env, .env.production)", "NEXT_PUBLIC_MISSING -> KRATE_PUBLIC_MISSING (no .env definition found)", "NEXT_PUBLIC_UNUSED"} {
		if !strings.Contains(item.Reason, want) {
			t.Errorf("manual item missing %q: %s", want, item.Reason)
		}
	}
	if strings.Contains(item.Reason+item.Hint, "hunter2") || strings.Contains(item.Reason, "localhost:3001") {
		t.Errorf("manual item leaks a value: %+v", item)
	}
}

func TestNextConfigEnvKeysRenamedWithoutValues(t *testing.T) {
	root := t.TempDir()
	writeNextFile(t, root, "package.json", `{"dependencies":{"next":"16.0.0"}}`)
	writeNextFile(t, root, "next.config.js", `export default { env: { BUILD_LABEL: "super-secret-value" } };`)
	writeNextFile(t, root, "app/page.tsx", `export default function P() { return <p>{process.env.BUILD_LABEL}</p>; }`)
	rep, err := ScanNext(root)
	if err != nil {
		t.Fatal(err)
	}
	page := findPlan(rep, "src/pages/index.tsx")
	if page == nil || !strings.Contains(string(page.Content), "process.env.KRATE_PUBLIC_BUILD_LABEL") {
		t.Fatalf("config env read not renamed: %+v", page)
	}
	for _, f := range rep.Files {
		if strings.Contains(string(f.Content), "super-secret-value") && f.Kind != KindConfig {
			t.Errorf("%s inlines the config env value", f.Target)
		}
	}
	found := false
	for _, m := range rep.Manual {
		if strings.Contains(m.Reason, "BUILD_LABEL -> KRATE_PUBLIC_BUILD_LABEL (set in next.config env)") && !strings.Contains(m.Reason, "super-secret") {
			found = true
		}
	}
	if !found {
		t.Errorf("config env not reported: %+v", rep.Manual)
	}
}

// TestMigrateListsPreactForReactApps: an app on React without Preact gets a
// manual item with the install command, and --update-package adds preact.
func TestMigrateListsPreactForReactApps(t *testing.T) {
	root := t.TempDir()
	writeNextFile(t, root, "package.json", `{"dependencies":{"next":"16.0.0","react":"19.0.0","@radix-ui/react-dialog":"1.1.0"}}`)
	writeNextFile(t, root, "pnpm-lock.yaml", "lockfileVersion: '9.0'\n")
	writeNextFile(t, root, "app/page.tsx", `export default function P() { return <p>hi</p>; }`)
	rep, err := ScanNext(root)
	if err != nil {
		t.Fatal(err)
	}
	if !hasManual(rep, "package.json", "KR8009") {
		t.Fatalf("preact not listed: %+v", rep.Manual)
	}
	var hint string
	for _, m := range rep.Manual {
		if strings.Contains(m.Reason, "KR8009") {
			hint = m.Hint
		}
	}
	if !strings.Contains(hint, "pnpm add preact@^10.20.0") {
		t.Errorf("hint = %q", hint)
	}
	rep, err = ScanNextWithOptions(root, ScanOptions{UpdatePackage: true})
	if err != nil {
		t.Fatal(err)
	}
	pkg := findPlan(rep, "package.json")
	if pkg == nil || !strings.Contains(string(pkg.Content), `"preact": "^10.20.0"`) {
		t.Fatalf("package plan lacks preact: %+v", pkg)
	}
	if hasManual(rep, "package.json", "KR8009") {
		t.Errorf("preact listed as manual although it is added")
	}
}
