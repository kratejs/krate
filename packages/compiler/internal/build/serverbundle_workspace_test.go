package build

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// wsServe builds a project in apps/web of a pnpm-like workspace (files are
// relative to the workspace top; "symlink:" values link like pnpm does), serves
// it with the real serve path, and returns the base URL and the app root.
func wsServe(t *testing.T, files map[string]string) (string, string) {
	t.Helper()
	root, cfg := externalsProjectPrep(t, files, "apps/web", nil, func(root string) { withPreact(t, root) })
	base, err := startKrateServer(t, root, cfg)
	if err != nil {
		t.Fatalf("serve: %v", err)
	}
	return base, root
}

const wsStorage = `{"name":"@dd/storage","version":"0.0.0","type":"module","main":"src/index.ts"}`

func TestServerBundleResolvesTsconfigPaths(t *testing.T) {
	t.Parallel()
	e2eEnv(t)
	base, root := wsServe(t, map[string]string{
		"tsconfig.base.json":              `{"compilerOptions":{"strict":true}}`,
		"apps/web/tsconfig.json":          `{"extends":"../../tsconfig.base.json","compilerOptions":{"jsx":"react-jsx","paths":{"@/*":["./src/*"]}}}`,
		"apps/web/src/components/Msg.tsx": `export function Msg() { return <p>from-alias</p>; }`,
		"apps/web/src/pages/index.tsx": `import { Msg } from "@/components/Msg";
export default async function Home() { await Promise.resolve(); return <main><Msg /></main>; }`,
	})
	status, _, html := getBody(t, base+"/")
	if status != 200 || !strings.Contains(html, "from-alias") {
		t.Fatalf("status %d, want the aliased component rendered:\n%s", status, html)
	}
	bundles, _ := filepath.Glob(filepath.Join(root, "dist", ".krate", "server-bundles", "*.server.mjs"))
	for _, f := range bundles {
		data, _ := os.ReadFile(f)
		if strings.Contains(string(data), `"@/components`) {
			t.Errorf("%s still imports the alias at run time", f)
		}
	}
}

func TestServerBundleExternalsResolveFromWorkspacePackage(t *testing.T) {
	t.Parallel()
	e2eEnv(t)
	base, _ := wsServe(t, map[string]string{
		"packages/storage/package.json":                       wsStorage,
		"packages/storage/src/index.ts":                       `import sdk from "fake-sdk"; export function store(k: string) { return sdk.put(k); }`,
		"packages/storage/node_modules/fake-sdk/package.json": `{"name":"fake-sdk","version":"1.0.0","main":"index.js"}`,
		"packages/storage/node_modules/fake-sdk/index.js":     `module.exports = { put: (k) => "stored-" + k };`,
		"apps/web/node_modules/@dd/storage":                   "symlink:packages/storage",
		"apps/web/src/pages/index.tsx": `import { store } from "@dd/storage";
export default async function Home() { await Promise.resolve(); return <main>{store("a")}</main>; }`,
	})
	status, _, html := getBody(t, base+"/")
	if status != 200 || !strings.Contains(html, "stored-a") {
		t.Fatalf("status %d, want the workspace dependency loaded from packages/storage:\n%s", status, html)
	}
}

func TestServerBundleBundledCommonJSCanRequireJSXRuntime(t *testing.T) {
	t.Parallel()
	e2eEnv(t)
	base, _ := wsServe(t, map[string]string{
		"apps/web/node_modules/fake-script/package.json": `{"name":"fake-script","version":"1.0.0","main":"index.js"}`,
		"apps/web/node_modules/fake-script/index.js": `const { jsx } = require("react/jsx-runtime");
module.exports = function Script() { return jsx("i", { children: "script-ok" }); };`,
		"apps/web/src/components/Tag.tsx": `"use client";
import Script from "fake-script";
export default function Tag() { return <Script />; }`,
		"apps/web/src/pages/index.tsx": `import Tag from "../components/Tag";
export default async function Home() { await Promise.resolve(); return <main><Tag /></main>; }`,
	})
	status, _, html := getBody(t, base+"/")
	if status != 200 || !strings.Contains(html, "script-ok") {
		t.Fatalf("status %d, want the CommonJS dependency rendered:\n%s", status, html)
	}
}
