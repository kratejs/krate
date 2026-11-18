package migrate

import (
	"strings"
	"testing"
)

// --update-package pins the Krate packages, maps next typegen, keeps the
// key order of package.json and tsconfig.json, and drops Next's TS plugin.
func TestUpdatePackageKeepsOrderAndPins(t *testing.T) {
	root := gapsProject(t, map[string]string{
		"src/app/page.tsx": `export default function P() { return <main/>; }`,
		"package.json": `{
  "name": "site",
  "private": true,
  "scripts": { "dev": "next dev", "typecheck": "next typegen && tsc --noEmit", "lint": "eslint ." },
  "dependencies": { "zod": "^4.0.0", "next": "16.0.0", "react": "19.0.0" }
}`,
		"tsconfig.json": `{
  "compilerOptions": { "strict": true, "plugins": [{ "name": "next" }], "baseUrl": "." },
  "include": ["src"]
}`,
	})
	rep, err := ScanNextWithOptions(root, ScanOptions{UpdatePackage: true, KrateVersion: "v0.5.0-rc.7"})
	if err != nil {
		t.Fatal(err)
	}
	pkg := findPlan(rep, "package.json")
	if pkg == nil {
		t.Fatal("no package.json plan")
	}
	got := string(pkg.Content)
	for _, want := range []string{`"@krate/core": "0.5.0-rc.7"`, `"typecheck": "krate types && tsc --noEmit"`, `"lint": "eslint ."`} {
		if !strings.Contains(got, want) {
			t.Errorf("package.json missing %s:\n%s", want, got)
		}
	}
	order := []string{`"name"`, `"private"`, `"scripts"`, `"dev"`, `"typecheck"`, `"lint"`, `"dependencies"`, `"zod"`, `"next"`, `"react"`, `"@krate/components"`}
	last := -1
	for _, k := range order {
		i := strings.Index(got, k)
		if i <= last {
			t.Fatalf("key %s out of order:\n%s", k, got)
		}
		last = i
	}
	ts := findPlan(rep, "tsconfig.json")
	if ts == nil {
		t.Fatal("no tsconfig.json plan")
	}
	tsc := string(ts.Content)
	if strings.Contains(tsc, `"next"`) || strings.Index(tsc, `"strict"`) > strings.Index(tsc, `"baseUrl"`) || strings.Index(tsc, `"compilerOptions"`) > strings.Index(tsc, `"include"`) {
		t.Errorf("tsconfig.json:\n%s", tsc)
	}
}
