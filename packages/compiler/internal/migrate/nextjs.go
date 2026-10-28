// Package migrate plans and applies framework migrations into a Krate project.
//
// The Next.js migrator is deliberately conservative: it copies route files into
// Krate's src/pages and src/api trees, strips imports that Krate provides as
// globals, and reports every pattern it cannot safely rewrite. It never deletes
// the original Next.js tree and writes atomically from a preflighted plan.
package migrate

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/kratejs/krate/packages/compiler/internal/fsutil"
)

const nextFramework = "nextjs"

// FileKind classifies a planned copy.
type FileKind string

const (
	KindPage       FileKind = "page"
	KindLayout     FileKind = "layout"
	KindAPI        FileKind = "api"
	KindLoading    FileKind = "loading"
	KindNotFound   FileKind = "not-found"
	KindConfig     FileKind = "config"
	KindPackage    FileKind = "package"
	KindAsset      FileKind = "asset"
	KindMiddleware FileKind = "middleware"
)

// ManualItem is a migration decision the CLI will not make automatically.
type ManualItem struct {
	Source string `json:"source,omitempty"`
	Reason string `json:"reason"`
	Hint   string `json:"hint,omitempty"`
}

// PlannedFile is one file the migrator can copy or generate.
type PlannedFile struct {
	Kind     FileKind `json:"kind"`
	Source   string   `json:"source,omitempty"`
	Target   string   `json:"target"`
	Conflict bool     `json:"conflict,omitempty"`
	Rewrites []string `json:"rewrites,omitempty"`
	Content  []byte   `json:"-"`
}

// Report is a complete migration plan. It is safe to print as JSON.
type Report struct {
	Root      string        `json:"root"`
	Framework string        `json:"framework"`
	Detected  []string      `json:"detected,omitempty"`
	Files     []PlannedFile `json:"files"`
	Manual    []ManualItem  `json:"manual,omitempty"`
	Warnings  []string      `json:"warnings,omitempty"`
}

// ApplyOptions controls the write step.
type ApplyOptions struct {
	// Force permits overwriting existing target files. Without it, Apply returns
	// a conflict error before writing anything.
	Force bool
	// BackupDir overrides the backup location. The default is
	// .krate/migrate/nextjs-<timestamp>.
	BackupDir string
}

// ApplyResult describes a completed migration and its rollback point.
type ApplyResult struct {
	BackupDir string   `json:"backupDir"`
	Written   []string `json:"written"`
}

type manifest struct {
	Version   int            `json:"version"`
	Framework string         `json:"framework"`
	Root      string         `json:"root"`
	Created   string         `json:"created"`
	Files     []manifestFile `json:"files"`
}

type manifestFile struct {
	Target  string `json:"target"`
	Kind    string `json:"kind"`
	Existed bool   `json:"existed"`
	Backup  string `json:"backup,omitempty"`
}

// ScanOptions controls optional migration outputs.
type ScanOptions struct {
	// UpdatePackage adds a package.json plan with Krate scripts and dependencies.
	UpdatePackage bool
}

// ScanNext detects a Next.js project and returns a copy plan. It reads the
// project but does not write anything.
func ScanNext(root string) (*Report, error) {
	return ScanNextWithOptions(root, ScanOptions{})
}

// ScanNextWithOptions is ScanNext with optional outputs.
func ScanNextWithOptions(root string, opts ScanOptions) (*Report, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, fmt.Errorf("project root: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("project root %s is not a directory", abs)
	}

	appDir := filepath.Join(abs, "app")
	appPrefix := "app"
	if !isDir(appDir) {
		appDir = filepath.Join(abs, "src", "app")
		appPrefix = "src/app"
	}
	hasApp := isDir(appDir)
	nextConfig := firstExisting(abs, "next.config.ts", "next.config.mjs", "next.config.js", "next.config.cjs")
	packageHasNext := false
	if pkg, err := os.ReadFile(filepath.Join(abs, "package.json")); err == nil {
		packageHasNext = strings.Contains(string(pkg), `"next"`)
	}
	rootPages := filepath.Join(abs, "pages")
	rootHasPages := isDir(rootPages)
	if !hasApp && !rootHasPages && nextConfig == "" && !packageHasNext {
		return nil, fmt.Errorf("not a Next.js project: expected app/, pages/, next.config.*, or a next dependency under %s", abs)
	}

	pagesDir := rootPages
	pagesPrefix := "pages"
	hasPages := rootHasPages
	if !hasPages {
		// `src/pages` is a Next source only for `src/app`, or when the package
		// actually declares Next. With root `app/`, an existing `src/pages` is the
		// likely Krate migration target, not a Next pages tree.
		srcPages := filepath.Join(abs, "src", "pages")
		if isDir(srcPages) && (appPrefix == "src/app" || (!hasApp && packageHasNext)) {
			pagesDir = srcPages
			pagesPrefix = "src/pages"
			hasPages = true
		}
	}

	rep := &Report{Root: abs, Framework: nextFramework}
	var nextCfg nextConfigData
	nextConfigStatic := false
	if nextConfig != "" {
		rep.Detected = append(rep.Detected, filepath.Base(nextConfig))
		data, warnings, ok := readNextConfig(nextConfig)
		for _, w := range warnings {
			rep.Warnings = append(rep.Warnings, filepath.Base(nextConfig)+": "+w)
		}
		if ok {
			nextCfg = data
			nextConfigStatic = true
			rep.Warnings = append(rep.Warnings, "translated static next.config redirects/rewrites/headers/basePath/images; review custom image loaders, env, and experimental keys manually")
		} else {
			rep.Manual = append(rep.Manual, ManualItem{
				Source: filepath.Base(nextConfig),
				Reason: "Next.js configuration could not be proven static and was not translated",
				Hint:   "move redirects/rewrites/headers/images/env into krate.config.ts",
			})
		}
	}
	if opts.UpdatePackage {
		if pkg, ok := packagePlan(abs); ok {
			rep.Files = append(rep.Files, pkg)
			rep.Manual = append(rep.Manual, ManualItem{
				Source: "package.json",
				Reason: "Krate scripts and dependencies were added, but Next.js dependencies remain",
				Hint:   "remove next/react dependencies after the migrated app builds",
			})
		} else {
			rep.Warnings = append(rep.Warnings, "package.json could not be read or parsed; not updated")
		}
	} else if pkg, err := os.ReadFile(filepath.Join(abs, "package.json")); err == nil && strings.Contains(string(pkg), `"next"`) {
		rep.Manual = append(rep.Manual, ManualItem{
			Source: "package.json",
			Reason: "Next.js dependencies and scripts are still present",
			Hint:   "re-run with --update-package, or install @krate/core and change dev/build/serve scripts manually",
		})
	}
	if hasApp {
		rep.Detected = append(rep.Detected, appPrefix+"/")
		if err := scanNextApp(abs, appDir, appPrefix, rep); err != nil {
			return nil, err
		}
	}
	if hasPages {
		rep.Detected = append(rep.Detected, pagesPrefix+"/")
		if err := scanNextPages(abs, pagesDir, pagesPrefix, rep); err != nil {
			return nil, err
		}
	}
	for _, mw := range []string{"middleware.ts", "middleware.js", "src/middleware.ts", "src/middleware.js"} {
		if _, err := os.Stat(filepath.Join(abs, filepath.FromSlash(mw))); err == nil {
			rep.Files = append(rep.Files, plannedMiddleware(abs, mw, rep))
			break
		}
	}
	linkRelativeImports(rep)
	rewriteNextModuleImports(rep, abs)
	if site := rootMetadataSite(rep); len(site) > 0 {
		nextCfg.Site = site
		rep.Warnings = append(rep.Warnings, "mapped static root metadata to site config; the metadata export is left in place for review, as are icons, alternates, and per-page metadata")
	}
	convertPageMetadata(rep)

	hasKrateConfig := fileExists(filepath.Join(abs, "krate.config.ts")) || fileExists(filepath.Join(abs, "krate.config.js")) || fileExists(filepath.Join(abs, "krate.config.mjs")) || fileExists(filepath.Join(abs, "krate.config.json"))
	if !hasKrateConfig {
		entry := "src/pages/index.tsx"
		var pages []string
		for _, f := range rep.Files {
			if f.Kind == KindPage {
				pages = append(pages, f.Target)
			}
		}
		sort.Strings(pages)
		if len(pages) > 0 {
			entry = pages[0]
		}
		for _, p := range pages {
			if p == "src/pages/index.tsx" {
				entry = p
				break
			}
		}
		rep.Files = append(rep.Files, PlannedFile{Kind: KindConfig, Target: "krate.config.ts", Content: configJSON(entry, nextCfg)})
	} else if nextConfigStatic || len(nextCfg.Site) > 0 {
		merged, notes, err := mergeKrateConfig(abs, nextCfg)
		if err != nil {
			rep.Manual = append(rep.Manual, ManualItem{
				Source: "krate.config.ts",
				Reason: "translated Next.js config/metadata could not be merged into the existing Krate config: " + err.Error(),
				Hint:   "merge the generated values manually or move the existing config aside and re-run",
			})
		} else {
			for _, note := range notes {
				rep.Warnings = append(rep.Warnings, "krate.config: "+note)
			}
			if merged.Target != "" {
				rep.Files = append(rep.Files, merged)
				rep.Warnings = append(rep.Warnings, "merged missing Next.js values into "+merged.Target+"; existing config keys were kept")
			}
		}
	}

	dedupeTargets(rep)
	for i := range rep.Files {
		if fileExists(filepath.Join(abs, filepath.FromSlash(rep.Files[i].Target))) {
			rep.Files[i].Conflict = true
		}
	}
	sort.SliceStable(rep.Files, func(i, j int) bool { return rep.Files[i].Target < rep.Files[j].Target })
	sort.SliceStable(rep.Manual, func(i, j int) bool {
		if rep.Manual[i].Source != rep.Manual[j].Source {
			return rep.Manual[i].Source < rep.Manual[j].Source
		}
		return rep.Manual[i].Reason < rep.Manual[j].Reason
	})
	return rep, nil
}

func scanNextApp(root, appDir, prefix string, rep *Report) error {
	return filepath.WalkDir(appDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if skipDir(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(appDir, path)
		if err != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		base := strings.TrimSuffix(filepath.Base(rel), filepath.Ext(rel))
		ext := strings.ToLower(filepath.Ext(rel))
		if !isSourceExt(ext) && !isAssetExt(ext) {
			return nil
		}
		dir := filepath.ToSlash(filepath.Dir(rel))
		route, manual := appRoute(dir)
		source := prefix + "/" + rel
		for _, m := range manual {
			rep.Manual = append(rep.Manual, ManualItem{Source: source, Reason: m, Hint: "flatten or rename the segment before migrating"})
		}
		if route == "" {
			route = "."
		}
		switch base {
		case "page":
			target := routeFile("src/pages", route, ext)
			rep.Files = append(rep.Files, planned(root, source, target, KindPage, ext, rep))
		case "layout":
			rep.Files = append(rep.Files, planned(root, source, layoutFile("src/pages", route, ext), KindLayout, ext, rep))
		case "route":
			apiPath := strings.TrimPrefix(route, "api/")
			if apiPath == route {
				rep.Manual = append(rep.Manual, ManualItem{Source: source, Reason: "route handler outside app/api becomes /api/" + route, Hint: "move or rename the route if the public path matters"})
				apiPath = route
			}
			if apiPath == "." || apiPath == "" {
				apiPath = "index"
			}
			rep.Files = append(rep.Files, planned(root, source, routeFile("src/api", apiPath, ext), KindAPI, ext, rep))
		case "loading":
			rep.Files = append(rep.Files, planned(root, source, loadingFile("src/pages", route, ext), KindLoading, ext, rep))
		case "not-found":
			if route == "." {
				rep.Files = append(rep.Files, planned(root, source, "src/pages/404"+ext, KindNotFound, ext, rep))
			} else {
				rep.Manual = append(rep.Manual, ManualItem{Source: source, Reason: "nested not-found.tsx has no automatic equivalent", Hint: "handle the missing case in the page"})
			}
		case "error", "global-error":
			assetTarget := "src/migrated/" + strings.TrimPrefix(prefix, "src/") + "/" + rel
			rep.Files = append(rep.Files, planned(root, source, assetTarget, KindAsset, ext, rep))
			layoutTarget := layoutFile("src/pages", route, ext)
			if fileExists(filepath.Join(root, filepath.FromSlash(layoutTarget))) || hasTarget(rep, layoutTarget) {
				rep.Manual = append(rep.Manual, ManualItem{Source: source, Reason: "error boundary was copied but an existing layout must be merged manually", Hint: "wrap children with the generated error boundary"})
				return nil
			}
			mod := relativeModulePath(layoutTarget, assetTarget)
			body := "import { Component } from \"react\";\nimport ErrorPage from " + strconv.Quote(mod) + ";\n\n" +
				"class __KrateErrorBoundary extends Component {\n" +
				"  state = { error: null };\n" +
				"  static getDerivedStateFromError(error: any) { return { error }; }\n" +
				"  componentDidCatch(error: any) { console.error(error); }\n" +
				"  reset = () => this.setState({ error: null });\n" +
				"  render() {\n" +
				"    if (this.state.error) return <ErrorPage error={this.state.error} reset={this.reset} />;\n" +
				"    return this.props.children;\n" +
				"  }\n" +
				"}\n\n" +
				"export default function Layout(props: any) {\n" +
				"  return <__KrateErrorBoundary><div>{props.children}</div></__KrateErrorBoundary>;\n" +
				"}\n"
			rep.Files = append(rep.Files, PlannedFile{Kind: KindLayout, Target: layoutTarget, Content: []byte(body), Rewrites: []string{"generated error boundary layout"}})
			rep.Warnings = append(rep.Warnings, source+": copied error boundary and generated "+layoutTarget)
			return nil
		case "template", "default":
			rep.Manual = append(rep.Manual, ManualItem{Source: source, Reason: "Next.js " + base + " boundary is not migrated", Hint: "use an error boundary or a page-level fallback"})
		default:
			if isNextMetadataRoute(base) {
				rep.Manual = append(rep.Manual, ManualItem{Source: source, Reason: "Next.js metadata route " + base + "." + strings.TrimPrefix(ext, ".") + " is not migrated", Hint: "generate the same sitemap/robots/manifest/icon in Krate"})
				return nil
			}
			if isAssetExt(ext) {
				target := "src/migrated/" + strings.TrimPrefix(prefix, "src/") + "/" + rel
				rep.Files = append(rep.Files, planned(root, source, target, KindAsset, ext, rep))
			}
		}
		return nil
	})
}

func scanNextPages(root, pagesDir, prefix string, rep *Report) error {
	return filepath.WalkDir(pagesDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if skipDir(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(pagesDir, path)
		if err != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		ext := strings.ToLower(filepath.Ext(rel))
		if !isSourceExt(ext) {
			return nil
		}
		base := strings.TrimSuffix(filepath.Base(rel), filepath.Ext(rel))
		source := prefix + "/" + rel
		if strings.HasPrefix(rel, "api/") {
			target := "src/api/" + strings.TrimPrefix(rel, "api/")
			rep.Files = append(rep.Files, planned(root, source, target, KindAPI, ext, rep))
			return nil
		}
		switch base {
		case "_app":
			assetTarget := "src/migrated/pages/" + rel
			rep.Files = append(rep.Files, planned(root, source, assetTarget, KindAsset, ext, rep))
			layoutTarget := "src/pages/_layout" + ext
			if fileExists(filepath.Join(root, filepath.FromSlash(layoutTarget))) {
				rep.Manual = append(rep.Manual, ManualItem{Source: source, Reason: "Pages Router _app was copied but an existing _layout.tsx must be merged manually", Hint: "wrap the page tree with the _app provider"})
				return nil
			}
			for _, f := range rep.Files {
				if f.Target == layoutTarget {
					return nil
				}
			}
			mod := relativeModulePath(layoutTarget, assetTarget)
			body := "import AppShell from " + strconv.Quote(mod) + ";\n\nexport default function Layout(props: any) {\n  const Component = () => props.children;\n  return <AppShell Component={Component} pageProps={{}} />;\n}\n"
			rep.Files = append(rep.Files, PlannedFile{Kind: KindLayout, Source: source, Target: layoutTarget, Content: []byte(body), Rewrites: []string{"wrapped Pages Router _app as the root layout"}})
			rep.Warnings = append(rep.Warnings, source+": copied _app and generated src/pages/_layout; verify providers and pageProps usage")
			return nil
		case "_document":
			rep.Manual = append(rep.Manual, ManualItem{Source: source, Reason: "Pages Router _document is not migrated", Hint: "set document metadata in site config or a layout"})
			return nil
		case "_error", "_middleware":
			rep.Manual = append(rep.Manual, ManualItem{Source: source, Reason: "special Pages Router file is not migrated", Hint: "use a Krate error boundary or middleware"})
			return nil
		}
		target := "src/pages/" + rel
		rep.Files = append(rep.Files, planned(root, source, target, KindPage, ext, rep))
		return nil
	})
}

func planned(root, source, target string, kind FileKind, ext string, rep *Report) PlannedFile {
	f := PlannedFile{Kind: kind, Source: source, Target: filepath.ToSlash(target)}
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(source)))
	if err != nil {
		f.Rewrites = append(f.Rewrites, "could not read source: "+err.Error())
		rep.Warnings = append(rep.Warnings, source+": could not read source: "+err.Error())
		return f
	}
	body, rewrites, manual := rewriteNextSource(string(data), kind)
	if kind == KindAPI {
		var apiRewrites, apiManual []string
		body, apiRewrites, apiManual = rewriteAPIServerSource(body)
		rewrites = append(rewrites, apiRewrites...)
		manual = append(manual, apiManual...)
	}
	f.Content = []byte(body)
	f.Rewrites = append(f.Rewrites, rewrites...)
	for _, m := range manual {
		rep.Manual = append(rep.Manual, ManualItem{Source: source, Reason: m, Hint: "review this file after migration"})
	}
	return f
}

// applyManual finds unsupported patterns and returns human-readable notes. The
// caller attaches them to the planned file so the CLI can show them.
func applyManual(src string, kind FileKind) []string {
	var out []string
	checks := []struct {
		needle string
		reason string
	}{
		{"export const metadata", "Next.js metadata export is not translated; use <Head> or krate site config"},
		{"export async function generateMetadata", "generateMetadata is not translated; render <Head> or use site config"},
		{"export function generateMetadata", "generateMetadata is not translated; render <Head> or use site config"},
		{"getStaticProps", "getStaticProps is not supported; use build-time data or createResource"},
		{"getServerSideProps", "getServerSideProps is not supported; use a server render mode or createResource"},
		{"from 'next/font", "next/font is not migrated"},
		{`from "next/font`, "next/font is not migrated"},
		{"from 'next/server'", "next/server request/response helpers need a Request/Response migration"},
		{`from "next/server"`, "next/server request/response helpers need a Request/Response migration"},
		{"from 'next/legacy/image'", "next/legacy/image needs a manual Image migration"},
		{`from "next/legacy/image"`, "next/legacy/image needs a manual Image migration"},
	}
	for _, c := range checks {
		if strings.Contains(src, c.needle) {
			out = append(out, c.reason)
		}
	}
	if kind == KindAPI {
		for _, c := range []string{"NextRequest", "NextResponse"} {
			if strings.Contains(src, c) {
				out = append(out, "Next.js "+c+" is not available; return a standard Response")
			}
		}
	}
	return out
}

var simpleNextImport = regexp.MustCompile(`(?m)^[ \t]*import[ \t]+[A-Za-z_$][\w$]*[ \t]+from[ \t]+["']next/(?:link|image|head)["'][ \t]*;?[ \t]*\r?\n?`)

// rewriteNextSource performs only safe source changes: remove the client
// directive and drop default imports for Next globals Krate provides itself.
func rewriteNextSource(src string, kind FileKind) (string, []string, []string) {
	var rewrites []string
	if body, ok := stripUseClient(src); ok {
		src = body
		rewrites = append(rewrites, "removed \"use client\" directive")
	}
	manual := []string{}
	if kind == KindLayout {
		if body, ok := stripDocumentShell(src); ok {
			src = body
			rewrites = append(rewrites, "replaced <html>/<body> document shell; move html attributes to site config")
		} else if strings.Contains(src, "<html") || strings.Contains(src, "<body") {
			manual = append(manual, "root layout document shell could not be rewritten automatically")
		}
	}
	before := src
	src = simpleNextImport.ReplaceAllString(src, "")
	if src != before {
		rewrites = append(rewrites, "removed next/link, next/image, or next/head default import (Krate global)")
	}
	for _, module := range []string{"next/link", "next/image", "next/head"} {
		if strings.Contains(src, module) {
			rewrites = append(rewrites, "kept a non-default "+module+" import for manual review")
		}
	}
	manual = append(manual, applyManual(src, kind)...)
	return src, rewrites, manual
}

func stripUseClient(src string) (string, bool) {
	lines := strings.SplitAfter(src, "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "//") || strings.HasPrefix(trimmed, "/*") || strings.HasPrefix(trimmed, "*") {
			continue
		}
		if trimmed == `"use client";` || trimmed == `"use client"` || trimmed == `'use client';` || trimmed == `'use client'` {
			lines = append(lines[:i], lines[i+1:]...)
			return strings.Join(lines, ""), true
		}
		break
	}
	return src, false
}

func appRoute(dir string) (string, []string) {
	if dir == "." || dir == "" {
		return ".", nil
	}
	var parts []string
	var manual []string
	for _, p := range strings.Split(filepath.ToSlash(dir), "/") {
		switch {
		case p == "" || p == ".":
			continue
		case strings.HasPrefix(p, "@"):
			manual = append(manual, "parallel route segment "+p+" is not migrated")
		case strings.HasPrefix(p, "_"):
			manual = append(manual, "private segment "+p+" is not a route")
		case strings.HasPrefix(p, "(") && strings.HasSuffix(p, ")"):
			// Route groups do not contribute to the URL.
		default:
			parts = append(parts, p)
		}
	}
	return strings.Join(parts, "/"), manual
}

func routeFile(base, route, ext string) string {
	if route == "." || route == "" {
		return base + "/index" + ext
	}
	return base + "/" + route + ext
}

func layoutFile(base, route, ext string) string {
	if route == "." || route == "" {
		return base + "/_layout" + ext
	}
	return base + "/" + route + "/_layout" + ext
}

func loadingFile(base, route, ext string) string {
	if route == "." || route == "" {
		return base + "/loading" + ext
	}
	return base + "/" + route + "/loading" + ext
}

func hasTarget(rep *Report, target string) bool {
	for _, f := range rep.Files {
		if f.Target == target {
			return true
		}
	}
	return false
}

func dedupeTargets(rep *Report) {
	seen := map[string]string{}
	for i := range rep.Files {
		f := &rep.Files[i]
		if prior, ok := seen[f.Target]; ok {
			rep.Warnings = append(rep.Warnings, fmt.Sprintf("%s and %s both map to %s; keeping the first", prior, f.Source, f.Target))
			f.Conflict = true
			continue
		}
		seen[f.Target] = f.Source
	}
}

func skipDir(name string) bool {
	switch name {
	case "node_modules", ".next", ".git", "dist", ".krate", "coverage":
		return true
	}
	return false
}

func isSourceExt(ext string) bool {
	switch ext {
	case ".tsx", ".jsx", ".ts", ".js", ".mdx", ".md":
		return true
	}
	return false
}

func firstExisting(root string, names ...string) string {
	for _, n := range names {
		p := filepath.Join(root, n)
		if fileExists(p) {
			return p
		}
	}
	return ""
}

func isDir(path string) bool {
	st, err := os.Stat(path)
	return err == nil && st.IsDir()
}

func fileExists(path string) bool {
	st, err := os.Stat(path)
	return err == nil && !st.IsDir()
}

// packagePlan adds Krate scripts and dependencies without removing Next.js
// dependencies. Removing Next itself is left to the user after verification.
func packagePlan(root string) (PlannedFile, bool) {
	path := filepath.Join(root, "package.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return PlannedFile{}, false
	}
	var pkg map[string]any
	if err := json.Unmarshal(data, &pkg); err != nil {
		return PlannedFile{}, false
	}
	scripts, _ := pkg["scripts"].(map[string]any)
	if scripts == nil {
		scripts = map[string]any{}
	}
	for _, name := range []string{"dev", "build", "serve", "start"} {
		if old, ok := scripts[name].(string); !ok || strings.Contains(old, "next") {
			scripts[name] = "krate " + name
		}
	}
	if _, ok := scripts["serve"]; !ok {
		scripts["serve"] = "krate serve"
	}
	pkg["scripts"] = scripts

	deps, _ := pkg["dependencies"].(map[string]any)
	if deps == nil {
		deps = map[string]any{}
	}
	for _, dep := range []string{"@krate/core", "@krate/runtime", "@krate/components"} {
		if _, ok := deps[dep]; !ok {
			deps[dep] = "latest"
		}
	}
	if _, ok := deps["preact"]; !ok {
		// Optional for apps without compiled islands, but required by the island
		// runtime the migrator may copy in.
		deps["preact"] = "^10.20.0"
	}
	pkg["dependencies"] = deps

	out, err := json.MarshalIndent(pkg, "", "  ")
	if err != nil {
		return PlannedFile{}, false
	}
	out = append(out, '\n')
	return PlannedFile{Kind: KindPackage, Target: "package.json", Content: out}, true
}

// Apply writes a plan atomically after preflighting every target. Existing
// targets are only overwritten when Force is set, and are backed up first.
func Apply(rep *Report, opts ApplyOptions) (*ApplyResult, error) {
	if rep == nil {
		return nil, fmt.Errorf("nil migration plan")
	}
	var conflicts []string
	seen := map[string]bool{}
	for _, f := range rep.Files {
		target, err := targetPath(rep.Root, f.Target)
		if err != nil {
			return nil, err
		}
		if seen[target] {
			return nil, fmt.Errorf("migration plan has duplicate target %s", f.Target)
		}
		seen[target] = true
		if fileExists(target) && !opts.Force {
			conflicts = append(conflicts, f.Target)
		}
	}
	if len(conflicts) > 0 {
		sort.Strings(conflicts)
		return nil, fmt.Errorf("refusing to overwrite %d existing file(s): %s (re-run with --force)", len(conflicts), strings.Join(conflicts, ", "))
	}
	if len(rep.Files) == 0 {
		return nil, fmt.Errorf("nothing to migrate")
	}

	backupDir := opts.BackupDir
	if backupDir == "" {
		backupDir = filepath.Join(rep.Root, ".krate", "migrate", "nextjs-"+time.Now().UTC().Format("20060102-150405"))
	}
	if !filepath.IsAbs(backupDir) {
		backupDir = filepath.Join(rep.Root, backupDir)
	}
	if err := os.MkdirAll(backupDir, 0755); err != nil {
		return nil, fmt.Errorf("creating backup directory: %w", err)
	}

	man := manifest{Version: 1, Framework: rep.Framework, Root: rep.Root, Created: time.Now().UTC().Format(time.RFC3339)}
	for _, f := range rep.Files {
		target, err := targetPath(rep.Root, f.Target)
		if err != nil {
			return nil, err
		}
		entry := manifestFile{Target: filepath.ToSlash(f.Target), Kind: string(f.Kind)}
		if fileExists(target) {
			entry.Existed = true
			backupRel := filepath.ToSlash(filepath.Join("files", f.Target))
			entry.Backup = backupRel
			data, err := os.ReadFile(target)
			if err != nil {
				return nil, fmt.Errorf("reading %s for backup: %w", f.Target, err)
			}
			dst := filepath.Join(backupDir, filepath.FromSlash(backupRel))
			if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
				return nil, err
			}
			if err := fsutil.WriteFileAtomic(dst, data, 0644); err != nil {
				return nil, err
			}
		}
		man.Files = append(man.Files, entry)
	}
	manData, err := json.MarshalIndent(man, "", "  ")
	if err != nil {
		return nil, err
	}
	if err := fsutil.WriteFileAtomic(filepath.Join(backupDir, "manifest.json"), append(manData, '\n'), 0644); err != nil {
		return nil, err
	}

	var written []string
	for _, f := range rep.Files {
		target, err := targetPath(rep.Root, f.Target)
		if err != nil {
			return nil, err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			_ = Rollback(backupDir)
			return nil, err
		}
		if err := fsutil.WriteFileAtomic(target, f.Content, 0644); err != nil {
			_ = Rollback(backupDir)
			return nil, fmt.Errorf("writing %s: %w", f.Target, err)
		}
		written = append(written, filepath.ToSlash(f.Target))
	}
	return &ApplyResult{BackupDir: backupDir, Written: written}, nil
}

// Rollback restores every overwritten file and removes every file the migration
// created.
func Rollback(backupDir string) error {
	data, err := os.ReadFile(filepath.Join(backupDir, "manifest.json"))
	if err != nil {
		return fmt.Errorf("read migration manifest: %w", err)
	}
	var man manifest
	if err := json.Unmarshal(data, &man); err != nil {
		return fmt.Errorf("parse migration manifest: %w", err)
	}
	root := man.Root
	for _, f := range man.Files {
		target, err := targetPath(root, f.Target)
		if err != nil {
			return err
		}
		if !f.Existed {
			if err := os.Remove(target); err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("remove %s: %w", f.Target, err)
			}
			continue
		}
		backup := filepath.Join(backupDir, filepath.FromSlash(f.Backup))
		within, err := filepath.Rel(backupDir, backup)
		if err != nil || strings.HasPrefix(within, "..") {
			return fmt.Errorf("backup path escapes migration directory: %s", f.Backup)
		}
		data, err := os.ReadFile(backup)
		if err != nil {
			return fmt.Errorf("read backup for %s: %w", f.Target, err)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return err
		}
		if err := fsutil.WriteFileAtomic(target, data, 0644); err != nil {
			return fmt.Errorf("restore %s: %w", f.Target, err)
		}
	}
	return os.RemoveAll(backupDir)
}

// rootMetadataSite extracts static metadata from the root app layout and maps
// the common fields to site config. The export remains in the layout because
// Krate ignores it; the warning tells the user what was mapped.
func rootMetadataSite(rep *Report) map[string]any {
	for i := range rep.Files {
		f := &rep.Files[i]
		if f.Kind != KindLayout || !strings.HasPrefix(f.Target, "src/pages/_layout.") {
			continue
		}
		obj, ok := staticMetadata(f.Content)
		if !ok {
			return nil
		}
		site := metadataSite(obj)
		if len(site) == 0 {
			return nil
		}
		// The generic "metadata is not translated" note no longer applies.
		var kept []ManualItem
		for _, m := range rep.Manual {
			if m.Source == f.Source && strings.Contains(m.Reason, "metadata export is not translated") {
				continue
			}
			kept = append(kept, m)
		}
		rep.Manual = kept
		return site
	}
	return nil
}

func isNextMetadataRoute(base string) bool {
	switch base {
	case "sitemap", "robots", "manifest", "opengraph-image", "twitter-image", "icon", "apple-icon", "instrumentation", "instrumentation-client":
		return true
	}
	return false
}

func isAssetExt(ext string) bool {
	switch ext {
	case ".tsx", ".jsx", ".ts", ".js", ".css", ".scss", ".sass", ".json", ".mdx", ".md":
		return true
	}
	return false
}

var relativeImport = regexp.MustCompile(`(?m)(\bfrom\s*|\bimport\s*\(\s*|\bimport\s+|\brequire\s*\(\s*)(["'])(\.{1,2}/[^"'\r\n]+)(["'])`)

// linkRelativeImports copies app-local modules under src/migrated and rewrites
// the import specifiers in migrated route files. The copied tree keeps its
// original shape, so imports inside those modules continue to resolve.
func linkRelativeImports(rep *Report) {
	assets := map[string]string{}
	for _, f := range rep.Files {
		if f.Kind == KindAsset {
			assets[f.Source] = f.Target
		}
	}
	for i := range rep.Files {
		f := &rep.Files[i]
		if f.Source == "" || (f.Kind != KindPage && f.Kind != KindLayout && f.Kind != KindAPI) {
			continue
		}
		body, rewrites, unresolved := rewriteRelativeImports(string(f.Content), f.Source, f.Target, assets)
		f.Content = []byte(body)
		f.Rewrites = append(f.Rewrites, rewrites...)
		for _, u := range unresolved {
			rep.Manual = append(rep.Manual, ManualItem{Source: f.Source, Reason: "relative import " + u + " has no copied target", Hint: "copy the imported file under src/ and update the import"})
		}
	}
}

func rewriteRelativeImports(src, source, target string, assets map[string]string) (string, []string, []string) {
	var rewrites []string
	var unresolved []string
	sourceDir := filepath.Dir(filepath.FromSlash(source))
	targetDir := filepath.Dir(filepath.FromSlash(target))
	out := relativeImport.ReplaceAllStringFunc(src, func(match string) string {
		sub := relativeImport.FindStringSubmatch(match)
		if len(sub) != 5 {
			return match
		}
		prefix, quote, spec, suffix := sub[1], sub[2], sub[3], sub[4]
		asset, ok := resolveAsset(sourceDir, spec, assets)
		if !ok {
			unresolved = append(unresolved, spec)
			return match
		}
		rel, err := filepath.Rel(targetDir, filepath.FromSlash(asset))
		if err != nil {
			unresolved = append(unresolved, spec)
			return match
		}
		rel = filepath.ToSlash(rel)
		if filepath.Ext(spec) == "" {
			switch strings.ToLower(filepath.Ext(rel)) {
			case ".tsx", ".ts", ".jsx", ".js", ".mdx", ".md":
				rel = strings.TrimSuffix(rel, filepath.Ext(rel))
			}
		}
		if !strings.HasPrefix(rel, ".") {
			rel = "./" + rel
		}
		rewrites = append(rewrites, "import "+spec+" -> "+rel)
		return prefix + quote + rel + suffix
	})
	return out, rewrites, unresolved
}

func resolveAsset(sourceDir, spec string, assets map[string]string) (string, bool) {
	candidate := filepath.ToSlash(filepath.Clean(filepath.Join(sourceDir, filepath.FromSlash(spec))))
	if target, ok := assets[candidate]; ok {
		return target, true
	}
	if filepath.Ext(candidate) == "" {
		for _, ext := range []string{".tsx", ".ts", ".jsx", ".js", ".css", ".scss", ".sass", ".json", ".mdx", ".md"} {
			if target, ok := assets[candidate+ext]; ok {
				return target, true
			}
			if target, ok := assets[candidate+"/index"+ext]; ok {
				return target, true
			}
		}
	}
	return "", false
}

func targetPath(root, rel string) (string, error) {
	if filepath.IsAbs(rel) {
		return "", fmt.Errorf("migration target must be relative: %s", rel)
	}
	clean := filepath.Clean(filepath.FromSlash(rel))
	if clean == "." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) || clean == ".." {
		return "", fmt.Errorf("migration target escapes project root: %s", rel)
	}
	target := filepath.Join(root, clean)
	within, err := filepath.Rel(root, target)
	if err != nil || strings.HasPrefix(within, "..") {
		return "", fmt.Errorf("migration target escapes project root: %s", rel)
	}
	return target, nil
}
