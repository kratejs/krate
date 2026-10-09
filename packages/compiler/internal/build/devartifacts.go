package build

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/kratejs/krate/packages/compiler/internal/bundler"
)

// refreshPageServerArtifacts updates the SSR sidecar's outputs for a partial
// (single-page) rebuild. Server bundles and runtime component bundles are
// compiled site-wide by BuildAll, but regenerating the whole site on every save
// is exactly what makes dev mode feel slow. Instead, only the artifacts owned by
// the rebuilt pages are refreshed, and their entries are merged into the
// on-disk manifests so the running sidecar sees them.
//
// Specifically:
//   - a rebuilt page whose mode is not SSG gets its server bundle recompiled
//     (CompileServerBundles re-emits the same deterministic bundle path), and
//     its server-manifest entry replaced;
//   - runtime component sources referenced by the rebuilt pages' regions are
//     recompiled in place, and the runtimeComponents/manifest entries updated.
//
// The manifest merge is deliberately additive: entries for pages and components
// outside this rebuild are preserved untouched.
func (b *Builder) refreshPageServerArtifacts(results []*PageResult, runtimeJS string, globalCSS []string) {
	if !b.DevMode || len(results) == 0 {
		return
	}

	ssrPages := make([]*PageResult, 0, len(results))
	for _, r := range results {
		if r.Mode != RenderSSG && r.SourcePath != "" {
			ssrPages = append(ssrPages, r)
		}
	}

	// Recompile server bundles only for the pages in this rebuild.
	var serverBundles map[string]string
	if len(ssrPages) > 0 {
		serverBundles = CompileServerBundles(ssrPages, b.Root, b.Cfg.OutDir, b.Cfg.Sourcemap)
	}

	// Collect runtime component sources referenced by this rebuild's regions
	// and recompile just those.
	runtimeBundles := b.refreshRuntimeComponents(results)

	if len(serverBundles) == 0 && len(runtimeBundles) == 0 {
		return
	}

	if len(serverBundles) > 0 {
		fmt.Printf("  %s⚡%s Refreshed %d server bundle(s)\n", cCyan, cReset, len(serverBundles))
	}
	if len(runtimeBundles) > 0 {
		fmt.Printf("  %s⚡%s Refreshed %d runtime component(s)\n", cCyan, cReset, len(runtimeBundles))
	}

	manifestCSS := ""
	if len(globalCSS) > 0 {
		manifestCSS = globalCSS[0]
	}
	if err := b.mergeManifests(manifestCSS, runtimeJS, serverBundles, runtimeBundles); err != nil {
		fmt.Fprintf(os.Stderr, "  %sWarning: failed to refresh manifest:%s %v\n", cYellow, cReset, err)
	}
}

// refreshRuntimeComponents recompiles the runtime component bundles referenced by
// results' regions. Returns the recompiled bundle metadata.
func (b *Builder) refreshRuntimeComponents(results []*PageResult) []RuntimeComponentBundle {
	seen := make(map[string]bool)
	var bundles []RuntimeComponentBundle
	for _, r := range results {
		for _, reg := range r.Regions {
			if reg.SourcePath == "" {
				continue
			}
			abs := reg.SourcePath
			if !filepath.IsAbs(abs) {
				abs = filepath.Join(b.Root, filepath.FromSlash(reg.SourcePath))
			}
			abs = filepath.Clean(abs)
			if seen[abs] {
				continue
			}
			seen[abs] = true

			// Only runtime components (*.runtime.*) have standalone bundles.
			if !bundler.IsRuntimeComponentFile(abs) {
				continue
			}
			bundle, err := compileSingleRuntimeComponent(b.Root, b.Cfg.OutDir, abs)
			if err != nil {
				fmt.Fprintf(os.Stderr, "  %sRuntime component error (%s):%s %v\n", cRed, filepath.Base(abs), cReset, err)
				continue
			}
			bundles = append(bundles, *bundle)
		}
	}
	return bundles
}

// mergeManifests reads dist/manifest.json and dist/server-manifest.json, replaces
// the entries for the rebuilt pages/components, and writes them back. Missing
// manifests are left absent (nothing to update).
func (b *Builder) mergeManifests(cssFile, runtimeJS string, serverBundles map[string]string, runtimeBundles []RuntimeComponentBundle) error {
	manifestPath := filepath.Join(b.Cfg.OutDir, "manifest.json")
	serverManifestPath := filepath.Join(b.Cfg.OutDir, "server-manifest.json")

	// ── Full manifest ────────────────────────────────────────────────────────
	var man Manifest
	if data, err := os.ReadFile(manifestPath); err == nil {
		_ = json.Unmarshal(data, &man)
	}
	if man.Routes == nil {
		man.Routes = make(map[string]PageMeta)
	}
	if cssFile != "" {
		man.Stylesheet = cssFile
	}
	if runtimeJS != "" {
		man.RuntimeJS = runtimeJS
	}
	if len(runtimeBundles) > 0 {
		mergeRuntimeComponentMeta(&man.RuntimeComponents, runtimeBundles)
	}
	if err := writeJSONIfChanged(manifestPath, &man); err != nil {
		return fmt.Errorf("writing manifest.json: %w", err)
	}

	// ── Server manifest (read by the Node sidecar) ────────────────────────────
	var sm ServerManifest
	if data, err := os.ReadFile(serverManifestPath); err == nil {
		_ = json.Unmarshal(data, &sm)
	}
	if cssFile != "" {
		sm.Stylesheet = cssFile
	}
	if runtimeJS != "" {
		sm.RuntimeJS = runtimeJS
	}
	if len(runtimeBundles) > 0 {
		mergeRuntimeComponentMeta(&sm.RuntimeComponents, runtimeBundles)
	}

	// Replace server-manifest page entries for the bundles we just rebuilt and
	// backfill their bundle paths.
	if len(serverBundles) > 0 {
		bySource := make(map[string]ManifestPage, len(sm.Pages))
		for _, p := range sm.Pages {
			bySource[filepath.ToSlash(p.Source)] = p
		}
		for src, bundle := range serverBundles {
			key := filepath.ToSlash(src)
			if p, ok := bySource[key]; ok {
				p.BundlePath = filepath.ToSlash(bundle)
				bySource[key] = p
			}
		}
		sm.Pages = sm.Pages[:0]
		for _, p := range bySource {
			sm.Pages = append(sm.Pages, p)
		}
	}

	return writeJSONIfChanged(serverManifestPath, &sm)
}

// mergeRuntimeComponentMeta replaces existing entries with matching SourcePath
// and appends new ones, preserving components outside this rebuild.
func mergeRuntimeComponentMeta(dst *[]RuntimeComponentMeta, fresh []RuntimeComponentBundle) {
	bySource := make(map[string]int, len(*dst))
	for i, m := range *dst {
		bySource[filepath.Clean(m.SourcePath)] = i
	}
	for _, b := range fresh {
		meta := RuntimeComponentMeta{
			Name:       b.Name,
			SourcePath: b.SourcePath,
			BundlePath: filepath.ToSlash(b.BundlePath),
		}
		if i, ok := bySource[filepath.Clean(b.SourcePath)]; ok {
			(*dst)[i] = meta
		} else {
			*dst = append(*dst, meta)
		}
	}
}

// writeJSONIfChanged marshals v and writes it only when the bytes differ, so a
// refresh that changes nothing does not re-trigger the watcher.
func writeJSONIfChanged(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	if existing, err := os.ReadFile(path); err == nil && strings.TrimSpace(string(existing)) == strings.TrimSpace(string(data)) {
		return nil
	}
	return os.WriteFile(path, data, 0644)
}
