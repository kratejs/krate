// Package migrate plans and applies framework migrations into a Krate project.
//
// The Next.js migrator is deliberately conservative: it copies route files into
// Krate's src/pages and src/api trees, strips imports that Krate provides as
// globals, and reports every pattern it cannot safely rewrite. It never deletes
// the original Next.js tree: --apply moves it into the backup directory, after
// every write succeeded, and `krate migrate rollback` moves it back.
package migrate

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path"
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
	KindPage         FileKind = "page"
	KindLayout       FileKind = "layout"
	KindAPI          FileKind = "api"
	KindLoading      FileKind = "loading"
	KindNotFound     FileKind = "not-found"
	KindForbidden    FileKind = "forbidden"
	KindUnauthorized FileKind = "unauthorized"
	KindError        FileKind = "error"
	KindConfig       FileKind = "config"
	KindPackage      FileKind = "package"
	KindAsset        FileKind = "asset"
	// KindMetadataRoute is a sitemap/robots/manifest/opengraph-image/twitter-image
	// module; Krate runs it natively from src/pages.
	KindMetadataRoute FileKind = "metadata-route"
	// KindPublic is a static metadata file (robots.txt, opengraph-image.png, ...)
	// copied byte for byte into public/.
	KindPublic     FileKind = "public"
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
	// Overwrites marks a target that already exists. --apply refuses to
	// replace it without --force (the original is backed up first).
	Overwrites   bool     `json:"overwrites,omitempty"`
	Rewrites     []string `json:"rewrites,omitempty"`
	Dependencies []string `json:"dependencies,omitempty"`
	Content      []byte   `json:"-"`
	// orig is the unmodified source of an in-place module, used to drop
	// modules no pass changed.
	orig []byte
}

// Report is a complete migration plan. It is safe to print as JSON.
type Report struct {
	Root      string        `json:"root"`
	Framework string        `json:"framework"`
	Detected  []string      `json:"detected,omitempty"`
	Files     []PlannedFile `json:"files"`
	Routes    []Route       `json:"routes,omitempty"`
	Manual    []ManualItem  `json:"manual,omitempty"`
	Warnings  []string      `json:"warnings,omitempty"`
	// MoveAside lists the original Next route trees (app/, src/app/, pages/)
	// and a Next 16 proxy file that --apply moves into the backup directory
	// after every write succeeds. `krate migrate rollback` moves them back.
	MoveAside []string `json:"moveAside,omitempty"`

	apiRewrites []nextRewrite
	headLinks   []string
}

// Route is the route inventory the migration will produce.
type Route struct {
	Route        string   `json:"route"`
	Source       string   `json:"source,omitempty"`
	Target       string   `json:"target"`
	Kind         string   `json:"kind"`
	Status       string   `json:"status"`
	Dependencies []string `json:"dependencies,omitempty"`
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
	// Moved lists the original route trees moved into the backup directory.
	Moved []string `json:"moved,omitempty"`
}

type manifest struct {
	Version   int            `json:"version"`
	Framework string         `json:"framework"`
	Root      string         `json:"root"`
	Created   string         `json:"created"`
	Files     []manifestFile `json:"files"`
	// Moved lists project-relative trees moved to <backup>/moved/<path>.
	Moved []string `json:"moved,omitempty"`
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
	// KrateVersion is the version of the running compiler. The @krate/*
	// dependencies the package plan adds are pinned to it; empty or "dev"
	// means "latest".
	KrateVersion string
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
		data, notes, ok := readNextConfig(nextConfig)
		for _, w := range notes.Warnings {
			rep.Warnings = append(rep.Warnings, filepath.Base(nextConfig)+": "+w)
		}
		if ok {
			nextCfg = data
			nextConfigStatic = true
			rep.Warnings = append(rep.Warnings, "translated static next.config redirects/rewrites/headers/basePath/images; every key or rule left out is listed separately")
			rep.Manual = append(rep.Manual, notes.Manual...)
			if len(notes.Skipped) > 0 {
				rep.Manual = append(rep.Manual, ManualItem{
					Source: filepath.Base(nextConfig),
					Reason: "next.config keys were not translated: " + strings.Join(notes.Skipped, ", ") + "; the other static keys were",
					Hint:   "write the skipped keys into krate.config.ts by hand",
				})
			}
		} else {
			rep.Manual = append(rep.Manual, ManualItem{
				Source: filepath.Base(nextConfig),
				Reason: "Next.js configuration could not be proven static and was not translated",
				Hint:   "move redirects/rewrites/headers/images/env into krate.config.ts",
			})
		}
	}
	if opts.UpdatePackage {
		if pkg, ok := packagePlan(abs, opts.KrateVersion); ok {
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
	planMiddleware(abs, rep)
	planInstrumentation(abs, rep)
	linkRelativeImports(rep)
	migrateNextFonts(rep, abs)
	rewriteNextModuleImports(rep, abs)
	warnClientBoundaryRuntime(rep)
	rewritePublicEnv(rep, readPublicEnv(abs), nextCfg.Env)
	rewriteMetadataTypes(rep)
	rewriteTestImports(rep)
	flagInlineUseServer(rep)
	// Before pruning, so modules routes import count as migrated.
	warnUnmigratedSources(abs, rep)
	pruneUnchangedModules(rep)
	addAPIRewrites(&nextCfg, rep)
	nextCfg.Head = append(nextCfg.Head, rep.headLinks...)
	var trees []string
	if hasApp {
		trees = append(trees, appPrefix)
	}
	if hasPages {
		trees = append(trees, pagesPrefix)
	}
	// A Next 16 proxy.ts is planned as middleware.ts; move the original aside too.
	for _, f := range rep.Files {
		if f.Kind == KindMiddleware && f.Source != "" && f.Source != f.Target {
			trees = append(trees, f.Source)
		}
	}
	planMoveAside(abs, trees, rep)

	hasKrateConfig := fileExists(filepath.Join(abs, "krate.config.ts")) || fileExists(filepath.Join(abs, "krate.config.js")) || fileExists(filepath.Join(abs, "krate.config.mjs")) || fileExists(filepath.Join(abs, "krate.config.json"))
	if !hasKrateConfig {
		// entry only matters when a site has no pages, so it is set for an
		// index page and omitted otherwise (never a route-group page).
		entry := ""
		for _, f := range rep.Files {
			if f.Kind == KindPage && f.Target == "src/pages/index.tsx" {
				entry = f.Target
			}
		}
		rep.Files = append(rep.Files, PlannedFile{Kind: KindConfig, Target: "krate.config.ts", Content: configJSON(entry, nextCfg)})
	} else if nextConfigStatic || len(nextCfg.Site) > 0 || len(nextCfg.Rewrites) > 0 || len(nextCfg.Head) > 0 {
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

	if ts, ok, note := tsconfigPlan(abs, rep); ok {
		rep.Files = append(rep.Files, ts)
		rep.Warnings = append(rep.Warnings, "merged/generated tsconfig.json for Krate JSX and route types")
	} else if note != "" {
		rep.Manual = append(rep.Manual, ManualItem{Source: "tsconfig.json", Reason: note, Hint: "set jsxImportSource to @krate/runtime and include .krate/types"})
	}
	dedupeTargets(rep)
	aggregateSegmentConfig(rep)
	for i := range rep.Files {
		if fileExists(filepath.Join(abs, filepath.FromSlash(rep.Files[i].Target))) {
			rep.Files[i].Conflict = true
			rep.Files[i].Overwrites = true
		}
	}
	sort.SliceStable(rep.Files, func(i, j int) bool { return rep.Files[i].Target < rep.Files[j].Target })
	rep.Routes = routeInventory(rep)
	sort.SliceStable(rep.Manual, func(i, j int) bool {
		if rep.Manual[i].Source != rep.Manual[j].Source {
			return rep.Manual[i].Source < rep.Manual[j].Source
		}
		return rep.Manual[i].Reason < rep.Manual[j].Reason
	})
	return rep, nil
}

// scopedBases are the App Router files that apply to a directory's subtree. A
// directory with one of them keeps its page in the directory (x/index.tsx), so
// the layout, template, loading, error and not-found files next to it wrap it
// (a flat x.tsx would live outside x/).
var scopedBases = map[string]bool{"layout": true, "template": true, "loading": true, "error": true, "not-found": true, "forbidden": true, "unauthorized": true}

// scopedAppDirs returns the filesystem routes of directories holding scoped files.
func scopedAppDirs(appDir string) map[string]bool {
	out := map[string]bool{}
	_ = filepath.WalkDir(appDir, func(path string, d fs.DirEntry, err error) error {
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
		ext := strings.ToLower(filepath.Ext(rel))
		if !isSourceExt(ext) || !scopedBases[strings.TrimSuffix(filepath.Base(rel), filepath.Ext(rel))] {
			return nil
		}
		_, fsRoute, _ := appRoute(filepath.ToSlash(filepath.Dir(rel)))
		out[fsRoute] = true
		return nil
	})
	return out
}

func scanNextApp(root, appDir, prefix string, rep *Report) error {
	scoped := scopedAppDirs(appDir)
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
		if planStaticIcon(root, prefix, rel, rep) || planStaticMetadata(root, appDir, prefix, rel, rep) {
			return nil
		}
		if !isSourceExt(ext) && !isAssetExt(ext) {
			return nil
		}
		dir := filepath.ToSlash(filepath.Dir(rel))
		route, fsRoute, manual := appRoute(dir)
		source := prefix + "/" + rel
		for _, m := range manual {
			rep.Manual = append(rep.Manual, ManualItem{Source: source, Reason: m, Hint: "flatten or rename the segment before migrating"})
		}
		if route == "" {
			route = "."
		}
		switch base {
		case "page":
			target := routeFile("src/pages", fsRoute, ext)
			if isSlotRoute(fsRoute) || (scoped[fsRoute] && fsRoute != "." && fsRoute != "") {
				target = "src/pages/" + fsRoute + "/index" + ext
			}
			rep.Files = append(rep.Files, planned(root, source, target, KindPage, ext, rep))
		case "layout":
			rep.Files = append(rep.Files, planned(root, source, layoutFile("src/pages", fsRoute, ext), KindLayout, ext, rep))
		case "route":
			apiPath := strings.TrimPrefix(route, "api/")
			if apiPath == route {
				apiPath = route
				if rw, ok := routeRewrite(route, apiPath); ok {
					rep.apiRewrites = append(rep.apiRewrites, rw)
					rep.Warnings = append(rep.Warnings, source+": route handler moved to /api/"+route+"; a rewrite from "+rw.Source+" keeps the public URL working")
				} else {
					rep.Manual = append(rep.Manual, ManualItem{Source: source, Reason: "route handler at the app root becomes /api", Hint: "a rewrite for / would shadow the home page; point clients at /api or add the rewrite by hand"})
				}
			}
			if apiPath == "." || apiPath == "" {
				apiPath = "index"
			}
			rep.Files = append(rep.Files, planned(root, source, routeFile("src/api", apiPath, ext), KindAPI, ext, rep))
		case "loading":
			rep.Files = append(rep.Files, planned(root, source, loadingFile("src/pages", fsRoute, ext), KindLoading, ext, rep))
		case "not-found":
			if isSlotRoute(fsRoute) {
				rep.Manual = append(rep.Manual, ManualItem{Source: source, Reason: "not-found.tsx inside a parallel-route slot has no equivalent", Hint: "move the not-found UI to the slot's parent directory"})
				return nil
			}
			rep.Files = append(rep.Files, planned(root, source, segmentFile("src/pages", fsRoute, "404", ext), KindNotFound, ext, rep))
		case "forbidden", "unauthorized":
			if isSlotRoute(fsRoute) {
				rep.Manual = append(rep.Manual, ManualItem{Source: source, Reason: base + ".tsx inside a parallel-route slot has no equivalent", Hint: "move the page to the slot's parent directory"})
				return nil
			}
			kind, status := KindForbidden, "403"
			if base == "unauthorized" {
				kind, status = KindUnauthorized, "401"
			}
			rep.Files = append(rep.Files, planned(root, source, segmentFile("src/pages", fsRoute, status, ext), kind, ext, rep))
		case "error":
			if isSlotRoute(fsRoute) {
				rep.Manual = append(rep.Manual, ManualItem{Source: source, Reason: "error.tsx inside a parallel-route slot has no equivalent", Hint: "move the error UI to the slot's parent directory"})
				return nil
			}
			rep.Files = append(rep.Files, planned(root, source, segmentFile("src/pages", fsRoute, "error", ext), KindError, ext, rep))
		case "global-error":
			if fsRoute != "." {
				rep.Manual = append(rep.Manual, ManualItem{Source: source, Reason: "global-error.tsx outside the app root has no equivalent", Hint: "use error.tsx in the directory instead"})
				return nil
			}
			if nextRootHas(appDir, "error") {
				rep.Manual = append(rep.Manual, ManualItem{Source: source, Reason: "global-error.tsx and the root error.tsx both map to the root error page", Hint: "merge them into src/pages/error.tsx; global-error.tsx renders its own <html> and <body>, which Krate's root layout already provides"})
				return nil
			}
			rep.Files = append(rep.Files, planned(root, source, segmentFile("src/pages", fsRoute, "error", ext), KindError, ext, rep))
			rep.Warnings = append(rep.Warnings, source+": global-error.tsx became the root error page (src/pages/error.tsx). Krate renders it inside the root layout, so remove the <html> and <body> elements it renders")
		case "template":
			target := "src/pages/_template" + ext
			if fsRoute != "." {
				target = "src/pages/" + fsRoute + "/_template" + ext
			}
			rep.Files = append(rep.Files, planned(root, source, target, KindLayout, ext, rep))
		case "default":
			if isSlotRoute(fsRoute) {
				rep.Files = append(rep.Files, planned(root, source, "src/pages/"+fsRoute+"/default"+ext, KindPage, ext, rep))
			} else {
				rep.Manual = append(rep.Manual, ManualItem{Source: source, Reason: "Next.js default.tsx outside a parallel-route slot has no equivalent", Hint: "render the fallback from the layout or page"})
			}
		default:
			if isNextMetadataRoute(base) {
				planMetadataRoute(root, source, base, ext, fsRoute, rep)
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
			rep.Files = append(rep.Files, PlannedFile{Kind: KindLayout, Source: source, Target: layoutTarget, Content: []byte(body), Rewrites: []string{"wrapped Pages Router _app as the root layout"}, Dependencies: []string{assetTarget}})
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
		{"export function generateViewport", "generateViewport is not supported; export a static `viewport` object or set the viewport meta in <Head>"},
		{"export async function generateViewport", "generateViewport is not supported; export a static `viewport` object or set the viewport meta in <Head>"},
		{"getStaticProps", "getStaticProps is not supported; use build-time data or createResource"},
		{"getServerSideProps", "getServerSideProps is not supported; use a server render mode or createResource"},
		{"from 'next/legacy/image'", "next/legacy/image needs a manual Image migration"},
		{`from "next/legacy/image"`, "next/legacy/image needs a manual Image migration"},
	}
	for _, c := range checks {
		if strings.Contains(src, c.needle) {
			out = append(out, c.reason)
		}
	}
	if kind != KindAPI && kind != KindModule && (strings.Contains(src, "from 'next/server'") || strings.Contains(src, `from "next/server"`)) {
		out = append(out, "next/server request/response helpers need a Request/Response migration")
	}
	for _, m := range segmentConfigExport.FindAllStringSubmatch(src, -1) {
		if m[1] == "maxDuration" && kind == KindAPI {
			if keptMaxDuration.MatchString(src) {
				continue
			}
			// Krate reads it, but only as a literal number of seconds.
			out = append(out, "export const maxDuration must be a literal number of seconds for Krate to apply it as the route's request timeout; the default of 30s applies otherwise")
			continue
		}
		out = append(out, "route segment config "+m[1]+" has no Krate equivalent and was left in place")
	}
	if useCacheDirective.MatchString(src) {
		out = append(out, `"use cache" is not supported; use revalidate/tags on fetch or unstable_cache`)
	}
	return out
}

var (
	asyncDefaultExport  = regexp.MustCompile(`export\s+default\s+async\b`)
	segmentConfigExport = regexp.MustCompile(`(?m)^export\s+const\s+(fetchCache|runtime|preferredRegion|maxDuration)\b`)
	keptMaxDuration     = regexp.MustCompile(`(?m)^export\s+const\s+maxDuration\s*=\s*\d+\s*(?:as\s+const\s*)?;?[ \t]*\r?$`)
	useCacheDirective   = regexp.MustCompile(`(?m)^\s*["']use cache(?::[a-z-]+)?["']`)
)

var simpleNextImport = regexp.MustCompile(`(?m)^[ \t]*import[ \t]+[A-Za-z_$][\w$]*[ \t]+from[ \t]+["']next/(?:link|image|head)["'][ \t]*;?[ \t]*\r?\n?`)

// rewriteNextSource performs only safe source changes: drop default imports
// for Next globals Krate provides itself and unwrap the root document shell.
// The "use client" directive is kept: Krate honors it as a client boundary
// inside server component trees.
func rewriteNextSource(src string, kind FileKind) (string, []string, []string) {
	var rewrites []string
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
	src, dropped := dropSegmentConfig(src, kind)
	rewrites = append(rewrites, dropped...)
	manual = append(manual, applyManual(src, kind)...)
	notes, layoutManual := segmentConfigNotes(src, kind)
	rewrites = append(rewrites, notes...)
	manual = append(manual, layoutManual...)
	return src, rewrites, manual
}

var (
	revalidateExport    = regexp.MustCompile(`(?m)^export\s+const\s+revalidate\s*=\s*(false|\d+)\s*;?`)
	dynamicExport       = regexp.MustCompile(`(?m)^export\s+const\s+dynamic\s*=\s*["'](auto|force-dynamic|force-static|error)["']`)
	dynamicParamsExport = regexp.MustCompile(`(?m)^export\s+const\s+dynamicParams\s*=\s*(true|false)\b`)
)

// segmentConfigNotes explains what Krate does with the Next.js route segment
// exports it reads natively (revalidate, dynamic, dynamicParams). On a page
// they are kept and honored, which is a rendering mode change worth stating.
// In a layout Krate does not read them, so that is manual work.
func segmentConfigNotes(src string, kind FileKind) (notes, manual []string) {
	type finding struct{ text, name string }
	var found []finding
	if m := revalidateExport.FindStringSubmatch(src); m != nil {
		var text string
		switch m[1] {
		case "false":
			text = "export const revalidate = false is kept: the page stays static"
		case "0":
			text = "export const revalidate = 0 is kept: the page renders per request (SSR)"
		default:
			text = "export const revalidate = " + m[1] + " is kept: the page renders as ISR, revalidated every " + m[1] + "s"
		}
		found = append(found, finding{text, "revalidate"})
	}
	if m := dynamicExport.FindStringSubmatch(src); m != nil {
		var text string
		switch m[1] {
		case "force-dynamic":
			text = `export const dynamic = "force-dynamic" is kept: the page renders per request (SSR)`
		case "force-static", "error":
			text = "export const dynamic = \"" + m[1] + "\" is kept: the page is built statically"
		default:
			text = `export const dynamic = "auto" is kept: Krate picks the render mode from the page`
		}
		found = append(found, finding{text, "dynamic"})
	}
	if m := dynamicParamsExport.FindStringSubmatch(src); m != nil {
		text := "export const dynamicParams = true is kept: params that generateStaticParams did not list render on demand"
		if m[1] == "false" {
			text = "export const dynamicParams = false is kept: params that generateStaticParams did not list answer 404"
		}
		found = append(found, finding{text, "dynamicParams"})
	}
	for _, f := range found {
		switch kind {
		case KindPage:
			notes = append(notes, f.text)
		case KindLayout:
			manual = append(manual, "route segment config "+f.name+" in a layout is not read by Krate")
		}
	}
	return notes, manual
}

// appRoute maps an app-router directory to its URL route (groups, slots and
// intercepting segments removed) and to the directory Krate uses under
// src/pages. Krate's file router understands route groups, "@slot"
// directories and intercepting "(.)x" segments natively, so the file path
// keeps them.
func appRoute(dir string) (urlRoute, fsRoute string, manual []string) {
	if dir == "." || dir == "" {
		return ".", ".", nil
	}
	var url, fs []string
	for _, p := range strings.Split(filepath.ToSlash(dir), "/") {
		switch {
		case p == "" || p == ".":
			continue
		case strings.HasPrefix(p, "_"):
			manual = append(manual, "private segment "+p+" is not a route")
		case strings.HasPrefix(p, "@"):
			fs = append(fs, p)
		case isInterceptingSegment(p):
			fs = append(fs, p)
		case strings.HasPrefix(p, "(") && strings.HasSuffix(p, ")"):
			fs = append(fs, p)
		default:
			url = append(url, p)
			fs = append(fs, p)
		}
	}
	urlRoute, fsRoute = strings.Join(url, "/"), strings.Join(fs, "/")
	if fsRoute == "" {
		fsRoute = "."
	}
	return urlRoute, fsRoute, manual
}

// isInterceptingSegment reports whether a directory name starts with one of
// Next's interception markers: (.) (..) (..)(..) (...).
func isInterceptingSegment(seg string) bool {
	for _, marker := range []string{"(...)", "(..)(..)", "(..)", "(.)"} {
		if strings.HasPrefix(seg, marker) && len(seg) > len(marker) {
			return true
		}
	}
	return false
}

// isSlotRoute reports whether the last directory of a Krate route is a
// parallel-route slot.
func isSlotRoute(fsRoute string) bool {
	return strings.HasPrefix(path.Base(fsRoute), "@")
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

func routeInventory(rep *Report) []Route {
	manual := map[string]bool{}
	for _, m := range rep.Manual {
		manual[m.Source] = true
	}
	var out []Route
	for _, f := range rep.Files {
		route := ""
		switch f.Kind {
		case KindPage:
			route = pageRoute(f.Target)
		case KindAPI:
			route = apiRoute(f.Target)
		}
		if route == "" {
			continue
		}
		status := "auto"
		if f.Conflict {
			status = "conflict"
		} else if manual[f.Source] {
			status = "manual"
		}
		out = append(out, Route{Route: route, Source: f.Source, Target: f.Target, Kind: string(f.Kind), Status: status, Dependencies: f.Dependencies})
	}
	return out
}

func pageRoute(target string) string {
	rel := strings.TrimPrefix(filepath.ToSlash(target), "src/pages/")
	rel = strings.TrimSuffix(rel, filepath.Ext(rel))
	var parts []string
	for _, seg := range strings.Split(rel, "/") {
		switch {
		case strings.HasPrefix(seg, "@"):
			// Parallel-route slot content is rendered by a layout, not routed.
			return ""
		case strings.HasPrefix(seg, "(") && strings.HasSuffix(seg, ")") && !isInterceptingSegment(seg):
			// Route groups do not contribute to the URL.
		default:
			parts = append(parts, seg)
		}
	}
	last := ""
	if len(parts) > 0 {
		last = parts[len(parts)-1]
	}
	switch last {
	case "_layout", "_template", "loading":
		return ""
	case "index":
		parts = parts[:len(parts)-1]
	}
	if len(parts) == 0 {
		return "/"
	}
	return "/" + strings.Join(parts, "/")
}

func apiRoute(target string) string {
	rel := strings.TrimPrefix(filepath.ToSlash(target), "src/api/")
	rel = strings.TrimSuffix(rel, filepath.Ext(rel))
	if rel == "index" || rel == "" {
		return "/api"
	}
	rel = strings.TrimSuffix(rel, "/index")
	return "/api/" + rel
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
func packagePlan(root, krateVersion string) (PlannedFile, bool) {
	path := filepath.Join(root, "package.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return PlannedFile{}, false
	}
	var pkg map[string]any
	if err := json.Unmarshal(data, &pkg); err != nil {
		return PlannedFile{}, false
	}
	var rewrites []string
	scripts, _ := pkg["scripts"].(map[string]any)
	if scripts == nil {
		scripts = map[string]any{}
	}
	for _, name := range []string{"dev", "build", "serve", "start"} {
		if old, ok := scripts[name].(string); !ok || strings.Contains(old, "next") {
			scripts[name] = "krate " + name
			if ok {
				rewrites = append(rewrites, fmt.Sprintf("scripts.%s: %q -> %q (the original is in the backup)", name, old, "krate "+name))
			} else {
				rewrites = append(rewrites, fmt.Sprintf("scripts.%s: added %q", name, "krate "+name))
			}
		}
	}
	if _, ok := scripts["serve"]; !ok {
		scripts["serve"] = "krate serve"
		rewrites = append(rewrites, `scripts.serve: added "krate serve"`)
	}
	pkg["scripts"] = scripts

	deps, _ := pkg["dependencies"].(map[string]any)
	if deps == nil {
		deps = map[string]any{}
	}
	spec := "latest"
	if v := strings.TrimPrefix(krateVersion, "v"); v != "" && v != "dev" {
		spec = "^" + v
	}
	var added []string
	for _, dep := range []string{"@krate/core", "@krate/runtime", "@krate/components"} {
		if _, ok := deps[dep]; !ok {
			deps[dep] = spec
			added = append(added, dep+"@"+spec)
		}
	}
	if _, ok := deps["preact"]; !ok {
		// Optional for apps without compiled islands, but required by the island
		// runtime the migrator may copy in.
		deps["preact"] = "^10.20.0"
		added = append(added, "preact@^10.20.0 (runs islands and \"use client\" components)")
	}
	if len(added) > 0 {
		rewrites = append(rewrites, "dependencies added: "+strings.Join(added, ", "))
	}
	var kept []string
	for _, dep := range []string{"next", "react", "react-dom"} {
		if _, ok := deps[dep]; ok {
			kept = append(kept, dep)
		}
	}
	if len(kept) > 0 {
		rewrites = append(rewrites, "dependencies kept: "+strings.Join(kept, ", ")+" (remove them once the app builds)")
	}
	pkg["dependencies"] = deps

	out, err := json.MarshalIndent(pkg, "", "  ")
	if err != nil {
		return PlannedFile{}, false
	}
	out = append(out, '\n')
	return PlannedFile{Kind: KindPackage, Source: "package.json", Target: "package.json", Content: out, Rewrites: rewrites}, true
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
	var moves []string
	for _, tree := range rep.MoveAside {
		abs, err := targetPath(rep.Root, tree)
		if err != nil {
			return nil, err
		}
		if _, err := os.Stat(abs); err == nil {
			moves = append(moves, filepath.ToSlash(filepath.Clean(filepath.FromSlash(tree))))
		}
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

	man := manifest{Version: 1, Framework: rep.Framework, Root: rep.Root, Created: time.Now().UTC().Format(time.RFC3339), Moved: moves}
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
	// Move the original route trees aside only after every write succeeded, so
	// a failed apply leaves them where they were.
	for _, tree := range moves {
		src, _ := targetPath(rep.Root, tree)
		if err := moveTree(src, filepath.Join(backupDir, "moved", filepath.FromSlash(tree))); err != nil {
			_ = Rollback(backupDir)
			return nil, fmt.Errorf("moving %s into the backup: %w", tree, err)
		}
	}
	return &ApplyResult{BackupDir: backupDir, Written: written, Moved: moves}, nil
}

// Rollback restores every overwritten file, moves the original route trees back
// and removes every file the migration created.
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
	for _, tree := range man.Moved {
		dest, err := targetPath(root, tree)
		if err != nil {
			return err
		}
		saved := filepath.Join(backupDir, "moved", filepath.FromSlash(tree))
		if _, err := os.Stat(saved); err != nil {
			continue
		}
		if _, err := os.Lstat(dest); err == nil {
			return fmt.Errorf("cannot restore %s: it already exists (the original is kept in %s)", tree, saved)
		}
		if err := moveTree(saved, dest); err != nil {
			return fmt.Errorf("restore %s: %w", tree, err)
		}
	}
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

// metadataRouteExts are the module extensions Krate runs for each convention.
var metadataRouteExts = map[string]map[string]bool{
	"sitemap":         {".ts": true, ".js": true, ".mjs": true},
	"robots":          {".ts": true, ".js": true, ".mjs": true},
	"manifest":        {".ts": true, ".js": true, ".mjs": true},
	"opengraph-image": {".ts": true, ".tsx": true, ".js": true, ".jsx": true},
	"twitter-image":   {".ts": true, ".tsx": true, ".js": true, ".jsx": true},
}

// planMetadataRoute maps a Next.js metadata route module to the same file in
// the same segment of src/pages. icon, apple-icon and instrumentation have no
// equivalent and stay manual items.
func planMetadataRoute(root, source, base, ext, fsRoute string, rep *Report) {
	if exts, ok := metadataRouteExts[base]; ok && exts[ext] {
		rep.Files = append(rep.Files, planned(root, source, segmentFile("src/pages", fsRoute, base, ext), KindMetadataRoute, ext, rep))
		return
	}
	reason := "Next.js metadata route " + base + "." + strings.TrimPrefix(ext, ".") + " is not migrated"
	hint := "render the icon once (ImageResponse or any image tool), save it as icon.png/apple-icon.png under public/, and link it from the site head config"
	if _, ok := metadataRouteExts[base]; ok {
		hint = "Krate runs " + base + " files with the extensions .ts, .js, .mjs (sitemap, robots, manifest) or .ts, .tsx, .js, .jsx (images)"
	}
	rep.Manual = append(rep.Manual, ManualItem{Source: source, Reason: reason, Hint: hint})
}

var staticMetadataNames = map[string]bool{"sitemap.xml": true, "robots.txt": true, "manifest.json": true, "manifest.webmanifest": true}
var staticMetadataImageExts = map[string]bool{".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".webp": true}

// planStaticMetadata handles the non-module metadata files: static sitemap.xml,
// robots.txt and manifest are served from public/, a static opengraph-image or
// twitter-image is copied next to its segment's page (where Krate's own
// convention picks it up). opengraph-image.alt.txt is a manual item: export alt
// from a module instead. It reports whether the file was one of these.
func planStaticMetadata(root, appDir, prefix, rel string, rep *Report) bool {
	name := filepath.Base(rel)
	ext := strings.ToLower(filepath.Ext(name))
	stem := strings.TrimSuffix(name, filepath.Ext(name))
	dir := filepath.ToSlash(filepath.Dir(rel))
	_, fsRoute, _ := appRoute(dir)
	source := prefix + "/" + rel
	var target string
	switch {
	case staticMetadataNames[name]:
		target = segmentFile("public", fsRoute, name, "")
	case (stem == "opengraph-image" || stem == "twitter-image") && staticMetadataImageExts[ext]:
		target = segmentFile("src/pages", fsRoute, stem, ext)
	case name == "opengraph-image.alt.txt" || name == "twitter-image.alt.txt":
		rep.Manual = append(rep.Manual, ManualItem{Source: source, Reason: "the " + name + " alt text file is not read", Hint: "export const alt = \"...\" from an opengraph-image/twitter-image module, or set metadata.openGraph.images[].alt"})
		return true
	default:
		return false
	}
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(source)))
	if err != nil {
		rep.Warnings = append(rep.Warnings, source+": could not read source: "+err.Error())
		return true
	}
	kind := KindPublic
	rep.Files = append(rep.Files, PlannedFile{Kind: kind, Source: source, Target: filepath.ToSlash(target), Content: data, Rewrites: []string{"copied unchanged"}})
	return true
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
	g := newLocalGraph(rep, assets)
	for i := range rep.Files {
		f := &rep.Files[i]
		if f.Source == "" || (f.Kind != KindPage && f.Kind != KindLayout && f.Kind != KindAPI && f.Kind != KindMetadataRoute && f.Kind != KindNotFound && f.Kind != KindForbidden && f.Kind != KindUnauthorized && f.Kind != KindError && f.Kind != KindAsset && f.Kind != KindLoading && f.Kind != KindMiddleware) {
			continue
		}
		body, rewrites, unresolved, deps := rewriteRelativeImportsWith(string(f.Content), f.Source, f.Target, assets, g.resolveRelative)
		body, aliasRewrites := g.rewriteAlias(body, f.Source, filepath.ToSlash(filepath.Dir(filepath.FromSlash(f.Target))))
		f.Content = []byte(body)
		f.Rewrites = append(f.Rewrites, rewrites...)
		f.Rewrites = append(f.Rewrites, aliasRewrites...)
		f.Dependencies = append(f.Dependencies, deps...)
		if f.Kind == KindAsset || f.Kind == KindMiddleware {
			continue
		}
		for _, u := range unresolved {
			rep.Manual = append(rep.Manual, ManualItem{Source: f.Source, Reason: "relative import " + u + " has no copied target", Hint: "copy the imported file under src/ and update the import"})
		}
	}
	g.drain()
}

func rewriteRelativeImports(src, source, target string, assets map[string]string) (string, []string, []string, []string) {
	return rewriteRelativeImportsWith(src, source, target, assets, nil)
}

// rewriteRelativeImportsWith is rewriteRelativeImports with a fallback for
// specifiers that are not copied app assets. local resolves a project file
// and returns the path the import should end up pointing at.
func rewriteRelativeImportsWith(src, source, target string, assets map[string]string, local func(sourceDir, spec string) (string, bool)) (string, []string, []string, []string) {
	var rewrites []string
	var unresolved []string
	var deps []string
	sourceDir := filepath.Dir(filepath.FromSlash(source))
	targetDir := filepath.Dir(filepath.FromSlash(target))
	out := relativeImport.ReplaceAllStringFunc(src, func(match string) string {
		sub := relativeImport.FindStringSubmatch(match)
		if len(sub) != 5 {
			return match
		}
		prefix, quote, spec, suffix := sub[1], sub[2], sub[3], sub[4]
		asset, ok := resolveAsset(sourceDir, spec, assets)
		if !ok && local != nil {
			asset, ok = local(filepath.ToSlash(sourceDir), spec)
			if ok && sourceDir == targetDir {
				return match
			}
		}
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
			case ".tsx", ".ts", ".jsx", ".js", ".mjs", ".mts", ".mdx", ".md":
				rel = strings.TrimSuffix(rel, filepath.Ext(rel))
			}
		} else if se := filepath.Ext(spec); (se == ".js" || se == ".jsx" || se == ".mjs") && filepath.Ext(rel) != se && isModuleSource(rel) {
			rel = strings.TrimSuffix(rel, filepath.Ext(rel)) + se
		}
		if !strings.HasPrefix(rel, ".") {
			rel = "./" + rel
		}
		if rel == spec {
			deps = append(deps, asset)
			return match
		}
		rewrites = append(rewrites, "import "+spec+" -> "+rel)
		deps = append(deps, asset)
		return prefix + quote + rel + suffix
	})
	return out, rewrites, unresolved, uniqueStrings(deps)
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

// warnClientBoundaryRuntime notes that client components rendered inside
// async server components hydrate with Preact, which the project must install.
func warnClientBoundaryRuntime(rep *Report) {
	var client, server bool
	for _, f := range rep.Files {
		if f.Kind != KindPage && f.Kind != KindLayout && f.Kind != KindAsset && f.Kind != KindNotFound && f.Kind != KindForbidden && f.Kind != KindUnauthorized && f.Kind != KindError {
			continue
		}
		src := string(f.Content)
		if hasUseClientDirective(src) {
			client = true
		}
		if f.Kind != KindAsset && asyncDefaultExport.MatchString(src) {
			server = true
		}
	}
	if client && server {
		rep.Warnings = append(rep.Warnings, `"use client" components inside async server components hydrate with Preact; install preact (krate migrate nextjs --update-package adds it)`)
	}
}

// segmentFile returns the target of a segment page (404.tsx / error.tsx) for a
// filesystem route, keeping route groups.
func segmentFile(base, fsRoute, name, ext string) string {
	if fsRoute == "." || fsRoute == "" {
		return base + "/" + name + ext
	}
	return base + "/" + fsRoute + "/" + name + ext
}

// nextRootHas reports whether the app root has a source file with the given
// base name (error.tsx, error.js, ...).
func nextRootHas(appDir, name string) bool {
	for _, ext := range []string{".tsx", ".ts", ".jsx", ".js"} {
		if _, err := os.Stat(filepath.Join(appDir, name+ext)); err == nil {
			return true
		}
	}
	return false
}
