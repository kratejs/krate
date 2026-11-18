package build

import (
	"bytes"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// splitSSRFiles is a server page rendering three client components that share
// a module. The shared module reads, at module level, a value another module
// sets at module level, so it renders right only if the side effects of the
// chunks run in import order.
func splitSSRFiles() map[string]string {
	files := map[string]string{
		"src/lib/setup.ts": `export const config = { prefix: "unset" };
config.prefix = "pre";
`,
		"src/lib/shared.ts": `import { config } from "./setup";
const PREFIX = config.prefix;
export function label(s: string) { return "SHARED_LABEL_MARK:" + PREFIX + ":" + s; }
`,
		"src/pages/index.tsx": `import A from "../components/A";
import B from "../components/B";
import C from "../components/C";
export default async function Home() {
  const n = await Promise.resolve(3);
  return <main><A n={n} /><B /><C>child</C></main>;
}`,
	}
	for _, name := range []string{"A", "B", "C"} {
		files["src/components/"+name+".tsx"] = `"use client";
import { useState } from "react";
import { label } from "../lib/shared";
export default function ` + name + `({ n, children }: any) {
  const [v] = useState(n ?? 0);
  return <p class="` + strings.ToLower(name) + `">{label("` + name + `")} {v}{children}</p>;
}`
	}
	return files
}

// renderSplitSSR builds the project of proj with a new Builder, with the
// server render modules built together or separately, and returns the
// server-rendered page.
func renderSplitSSR(t *testing.T, proj *Builder, combined bool) string {
	t.Helper()
	b := New(proj.Root, proj.Cfg)
	old := combinedSSRModules
	combinedSSRModules = combined
	defer func() { combinedSSRModules = old }()
	before := esbuildStatsSnapshot()
	if err := b.BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	calls := esbuildStatsSince(before)
	combinedCalls, separate := calls["(*clientSSRImpls).buildCombined"].n, calls["(*clientSSRImpls).buildOneOutput"].n
	if combined && (combinedCalls != 1 || separate != 0) {
		t.Errorf("want one combined server render build and no separate ones, got %d combined and %d separate", combinedCalls, separate)
	}
	if !combined && (combinedCalls != 0 || separate != 3) {
		t.Errorf("want three separate server render builds, got %d combined and %d separate", combinedCalls, separate)
	}
	base, err := startKrateServer(t, b.Root, b.Cfg)
	if err != nil {
		t.Fatalf("serve: %v", err)
	}
	resp, err := http.Get(base + "/")
	if err != nil {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status %d:\n%s", resp.StatusCode, data)
	}
	return string(data)
}

// TestClientSSRModulesShareChunks: a full build compiles the server render
// modules of its client components in one code-split build. Each entry stays
// at its own path, the module they share is emitted once in a chunk, and the
// page renders the same HTML as with separate builds.
func TestClientSSRModulesShareChunks(t *testing.T) {
	e2eEnv(t)
	b, root := scProject(t, splitSSRFiles())
	withPreact(t, root)
	if err := linkRuntimePackage(t, filepath.Join(root, "node_modules", "@krate")); err != nil {
		t.Fatal(err)
	}
	html := renderSplitSSR(t, b, true)
	for _, want := range []string{"SHARED_LABEL_MARK:pre:A 3", "SHARED_LABEL_MARK:pre:B 0", "SHARED_LABEL_MARK:pre:C 0", "-->child<!--/krate-slot:"} {
		if !strings.Contains(html, want) {
			t.Errorf("server HTML missing %q:\n%s", want, html)
		}
	}
	clientDir := filepath.Join(root, "dist", ".krate", "server-bundles", "client")
	for _, name := range []string{"A", "B", "C"} {
		id := clientModuleID(root, filepath.Join(root, "src", "components", name+".tsx"))
		entry, err := os.ReadFile(filepath.Join(clientDir, id+".ssr.mjs"))
		if err != nil {
			t.Fatalf("entry of %s: %v", name, err)
		}
		if bytes.Contains(entry, []byte(".krate-batch-")) {
			t.Errorf("%s names its batch entry path; it must name its own module as a separate build does:\n%s", name, entry)
		}
		if !regexp.MustCompile(`from "\./chunks/[^"]+-[A-Z0-9]+\.mjs"`).Match(entry) {
			t.Errorf("entry of %s should import a hashed shared chunk:\n%s", name, entry)
		}
	}
	var holders []string
	_ = filepath.Walk(clientDir, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		data, _ := os.ReadFile(p)
		if strings.Contains(string(data), "SHARED_LABEL_MARK") {
			holders = append(holders, p)
		}
		return nil
	})
	if len(holders) != 1 || filepath.Base(filepath.Dir(holders[0])) != "chunks" {
		t.Errorf("the shared module should be emitted once, in a chunk, got %v", holders)
	}
	dist := distOutsideSSRModules(t, root)
	// The deploy bundle follows the entries into their chunks.
	if _, err := BuildServerBundle(root, b.Cfg, "node"); err != nil {
		t.Fatalf("node deploy bundle: %v", err)
	}

	separate := renderSplitSSR(t, b, false)
	// Only the server render modules differ.
	for rel, data := range distOutsideSSRModules(t, root) {
		if dist[rel] != data {
			t.Errorf("dist/%s differs between combined and separate server render modules", rel)
		}
		delete(dist, rel)
	}
	for rel := range dist {
		t.Errorf("dist/%s is written only with combined server render modules", rel)
	}
	// Slot ids are random per request.
	slotID := regexp.MustCompile(`(__krateSlotRef":"|krate-slot:)[0-9a-z]+`)
	html, separate = slotID.ReplaceAllString(html, "${1}ID"), slotID.ReplaceAllString(separate, "${1}ID")
	if html != separate {
		t.Errorf("combined and separate server render modules render different HTML:\n--- combined\n%s\n--- separate\n%s", html, separate)
	}
}

// distOutsideSSRModules reads every file of root's dist except the server
// render modules.
func distOutsideSSRModules(t *testing.T, root string) map[string]string {
	t.Helper()
	dist := filepath.Join(root, "dist")
	skip := filepath.Join(dist, ".krate", "server-bundles", "client")
	out := map[string]string{}
	err := filepath.Walk(dist, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			if p == skip {
				return filepath.SkipDir
			}
			return nil
		}
		data, err := os.ReadFile(p)
		rel, _ := filepath.Rel(dist, p)
		out[filepath.ToSlash(rel)] = string(data)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// TestCombinedSSRFallsBackPerModule: when one module of the combined build
// fails, nothing of it is written, every module builds on its own, and the
// error is the failing module's own, as with separate builds.
func TestCombinedSSRFallsBackPerModule(t *testing.T) {
	b, root := scProject(t, map[string]string{
		"src/lib/shared.ts":      "export const word = 'shared';\n",
		"src/components/A.tsx":   "\"use client\";\nimport { word } from '../lib/shared';\nexport default function A() { return <p>a {word}</p>; }\n",
		"src/components/B.tsx":   "\"use client\";\nimport { word } from '../lib/shared';\nexport default function B() { return <p>b {word}</p>; }\n",
		"src/components/Bad.tsx": "\"use client\";\nimport { gone } from './missing-module';\nexport default function Bad() { return <p>{gone}</p>; }\n",
		"src/pages/index.tsx":    "export default function Home() { return <main />; }\n",
	})
	var cbs []ClientBoundary
	for _, name := range []string{"A", "B", "Bad"} {
		p := filepath.Join(root, "src", "components", name+".tsx")
		cbs = append(cbs, ClientBoundary{ModuleID: clientModuleID(root, p), SourcePath: p, Exports: []string{"default"}})
	}
	impls := newClientSSRImpls(b.importResolver(), nil, root, b.Cfg.OutDir, nil, "")
	before := esbuildStatsSnapshot()
	impls.buildAll(cbs)
	calls := esbuildStatsSince(before)
	if n, m := calls["(*clientSSRImpls).buildCombined"].n, calls["(*clientSSRImpls).buildOneOutput"].n; n != 1 || m != 3 {
		t.Errorf("want one combined build and three separate ones after it failed, got %d and %d", n, m)
	}
	for _, cb := range cbs[:2] {
		if err := impls.build(cb); err != nil {
			t.Errorf("%s: %v", cb.SourcePath, err)
		}
		data, err := os.ReadFile(impls.implPath(cb))
		if err != nil || !strings.Contains(string(data), "shared") || strings.Contains(string(data), "./chunks/") {
			t.Errorf("%s should be a module of its own: %v\n%s", cb.SourcePath, err, data)
		}
	}
	err := impls.build(cbs[2])
	if err == nil || !strings.Contains(err.Error(), "missing-module") || !strings.Contains(err.Error(), "Bad.tsx") {
		t.Errorf("want the failing module's error, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(b.Cfg.OutDir, ".krate", "server-bundles", "client", "chunks")); err == nil {
		t.Error("the failed combined build wrote its chunks")
	}
}
