package build

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/migrate"
)

// TestNextMigrateReferenceApps is the N5 verification suite: three distinct
// Next.js projects migrate, build, pass checks, keep their full route
// inventory, and roll back cleanly.
func TestNextMigrateReferenceApps(t *testing.T) {
	cases := []struct {
		name   string
		files  map[string]string
		routes map[string]string // route -> kind
	}{
		{
			name: "app-router",
			files: map[string]string{
				"package.json":   `{"name":"app-ref","private":true,"dependencies":{"next":"15.0.0","react":"19.0.0"}}`,
				"app/layout.tsx": `export default function RootLayout({ children }) { return <html><body>{children}</body></html>; }`,
				"app/page.tsx": `export const metadata = { title: "Home", description: "App router home" };
export default function Home() { return <main><h1>Home</h1></main>; }`,
				"app/about/page.tsx": `export const metadata = { title: "About", description: "About the app" };
export default function About() { return <main><h1>About</h1></main>; }`,
				"app/api/health/route.ts": `export async function GET() { return Response.json({ ok: true }); }`,
			},
			routes: map[string]string{"/": "page", "/about": "page", "/api/health": "api"},
		},
		{
			name: "pages-router",
			files: map[string]string{
				"package.json": `{"name":"pages-ref","private":true,"dependencies":{"next":"15.0.0","react":"19.0.0"}}`,
				"pages/index.tsx": `import Head from "next/head";
export default function Home() { return <><Head><title>Home</title><meta name="description" content="Pages home" /></Head><main><h1>Home</h1></main></>; }`,
				"pages/about.tsx": `import Head from "next/head";
export default function About() { return <><Head><title>About</title><meta name="description" content="Pages about" /></Head><main><h1>About</h1></main></>; }`,
				"pages/docs/[slug].tsx": `import Head from "next/head";
export default function Doc() { return <><Head><title>Doc</title><meta name="description" content="A document" /></Head><main><h1>Document</h1></main></>; }`,
			},
			routes: map[string]string{"/": "page", "/about": "page", "/docs/[slug]": "page"},
		},
		{
			name: "client-and-dynamic",
			files: map[string]string{
				"package.json":   `{"name":"dynamic-ref","private":true,"dependencies":{"next":"15.0.0","react":"19.0.0"}}`,
				"app/layout.tsx": `export default function RootLayout({ children }) { return <html><body>{children}</body></html>; }`,
				"app/page.tsx": `export const metadata = { title: "Dashboard", description: "Dynamic app dashboard" };
import Counter from "./components/Counter";
export default function Home() { return <main><h1>Dashboard</h1><Counter /></main>; }`,
				"app/components/Counter.tsx": `"use client";
import { useState } from "react";
export default function Counter() { const [n, setN] = useState(0); return <button onClick={() => setN(n + 1)}>{n}</button>; }`,
				"app/blog/[slug]/page.tsx": `export function generateStaticParams() { return [{ slug: "hello" }]; }
export const metadata = { title: "Post", description: "A blog post" };
export default function Post() { return <main><h1>Post</h1></main>; }`,
			},
			routes: map[string]string{"/": "page", "/blog/[slug]": "page"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			for rel, content := range tc.files {
				writeFileRel(t, root, rel, content)
			}
			rep, err := migrate.ScanNext(root)
			if err != nil {
				t.Fatal(err)
			}
			for route, kind := range tc.routes {
				found := false
				for _, r := range rep.Routes {
					if r.Route == route && r.Kind == kind {
						found = true
						break
					}
				}
				if !found {
					t.Fatalf("route inventory missing %s (%s): %+v", route, kind, rep.Routes)
				}
			}
			// The only expected manual item is the package.json cleanup common
			// to every migrated Next app.
			for _, m := range rep.Manual {
				if m.Source != "package.json" {
					t.Fatalf("unexpected silent/manual route work: %+v", m)
				}
			}
			res, err := migrate.Apply(rep, migrate.ApplyOptions{})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(filepath.Join(res.BackupDir, "manifest.json")); err != nil {
				t.Fatalf("missing rollback manifest: %v", err)
			}
			prepareMigratedRuntime(t, root)
			ver, err := VerifyMigration(root, rep.Routes, MigrationVerifyOptions{})
			if err != nil {
				t.Fatalf("verify: %v", err)
			}
			if !ver.OK() {
				out, _ := FormatMigrationVerification(ver)
				t.Fatalf("verification failed:\n%s", out)
			}
			if len(ver.MissingRoutes) != 0 || len(ver.MissingTargets) != 0 {
				t.Fatalf("silent route drop: %+v", ver)
			}
			if err := migrate.Rollback(res.BackupDir); err != nil {
				t.Fatalf("rollback: %v", err)
			}

			for _, f := range rep.Files {
				if f.Kind == migrate.KindPage || f.Kind == migrate.KindConfig || f.Kind == migrate.KindPackage {
					if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(f.Target))); !os.IsNotExist(err) {
						t.Fatalf("rollback left generated target %s (err=%v)", f.Target, err)
					}
				}
			}
		})
	}
}

func prepareMigratedRuntime(t *testing.T, root string) {
	t.Helper()
	ensureRuntimeDist(t)
	withPreact(t, root)
	repoRoot, err := repoRootPath()
	if err != nil {
		t.Fatal(err)
	}
	scope := filepath.Join(root, "node_modules", "@krate")
	if err := os.MkdirAll(scope, 0755); err != nil {
		t.Fatal(err)
	}
	for _, pkg := range []string{"core", "plugin", "runtime"} {
		if err := os.Symlink(filepath.Join(repoRoot, "packages", pkg), filepath.Join(scope, pkg)); err != nil && !os.IsExist(err) {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(root, "packages"), 0755); err != nil {
		t.Fatal(err)
	}
	runtimeLink := filepath.Join(root, "packages", "runtime")
	if err := os.Symlink(filepath.Join(repoRoot, "packages", "runtime"), runtimeLink); err != nil && !os.IsExist(err) {
		t.Fatal(err)
	}
}
