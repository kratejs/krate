package build

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/kratejs/krate/packages/compiler/internal/bundler"
)

// refreshPageServerArtifacts updates the SSR sidecar's outputs for a partial
// (single-page) rebuild. Server bundles and runtime component bundles are
// compiled site-wide by BuildAll, but regenerating the whole site on every save
// is exactly what makes dev mode feel slow. Instead, only the artifacts owned by
// the rebuilt pages are refreshed, and their entries are merged into the
// on-disk manifests so the running sidecar sees them.
// Specifically:
//   - a rebuilt page whose mode is not SSG gets its server bundle recompiled
//     (CompileServerBundles re-emits the same deterministic bundle path), and
//     its server-manifest entry replaced;
//   - runtime component sources referenced by the rebuilt pages' regions are
//     recompiled in place, and the runtimeComponents/manifest entries updated.
//
// The manifest merge is deliberately additive: entries for pages and components
// outside this rebuild are preserved untouched.
func (b *Builder) refreshPageServerArtifacts(results []*PageResult, runtimeJS string, globalCSS []string) error {
	if !b.DevMode || len(results) == 0 {
		return nil
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
		serverBundles = b.rebuildClientSSRModules(ssrPages)
		stop := b.phase("ssr:highlight-tables")
		err := b.writeHighlightTables(serverBundles)
		stop()
		if err != nil {
			return err
		}
	}
	stopRT := b.phase("ssr:runtime-components")

	// Collect runtime component sources referenced by this rebuild's regions
	// and recompile just those.
	runtimeBundles := b.refreshRuntimeComponents(results)
	stopRT()

	defer b.phase("ssr:manifests")()
	if b.serverManifestOutOfStep(results) {
		b.needFullRebuild.Store(true)
	}

	if len(serverBundles) == 0 && len(runtimeBundles) == 0 {
		return nil
	}

	if len(serverBundles) > 0 {
		printf("  %s*%s Refreshed %d server bundle(s)\n", cCyan, cReset, len(serverBundles))
	}
	if len(runtimeBundles) > 0 {
		printf("  %s*%s Refreshed %d runtime component(s)\n", cCyan, cReset, len(runtimeBundles))
	}

	manifestCSS := ""
	if len(globalCSS) > 0 {
		manifestCSS = globalCSS[0]
	}
	if err := b.mergeManifests(manifestCSS, runtimeJS, serverBundles, runtimeBundles); err != nil {
		return fmt.Errorf("refreshing the build manifest under %s: %w", b.Cfg.OutDir, err)
	}
	return nil
}

// recordDevBoundaries remembers the client boundaries of the request-time
// pages built, by module. A full build replaces the record, a partial one
// adds to it.
func (b *Builder) recordDevBoundaries(boundaries []ClientBoundary, replace bool) {
	b.devBoundariesMu.Lock()
	defer b.devBoundariesMu.Unlock()
	if replace || b.devBoundaries == nil {
		b.devBoundaries = map[string]ClientBoundary{}
	}
	for _, cb := range boundaries {
		b.devBoundaries[cb.SourcePath] = cb
	}
}

// rebuildClientSSRModules recompiles the server bundles of ssrPages and the
// server render modules of every client component the site has.
//
// A render module built on its own inlines the modules it imports, so after a
// partial rebuild a provider and the client components rendered inside it came
// from different builds, each with a context of its own, while a full build
// shares them through chunks. Every render module is therefore built again
// with the others, in one code-split build, so all of them share the same
// chunks and a page loaded later finds the same module graph as the layout it
// renders in.
func (b *Builder) rebuildClientSSRModules(ssrPages []*PageResult) map[string]string {
	impls := newClientSSRImpls(b.importResolver(), ssrPages, b.Root, b.Cfg.OutDir, b.actionModules(), b.Cfg.BaseURLPath())
	b.recordDevBoundaries(impls.boundaries, false)
	stopCombined := b.phase("ssr:client-modules-combined")
	b.devBoundariesMu.Lock()
	all := make([]ClientBoundary, 0, len(b.devBoundaries))
	for src, cb := range b.devBoundaries {
		if cb.Spec == "" {
			if _, err := os.Stat(src); err != nil {
				delete(b.devBoundaries, src)
				continue
			}
		}
		all = append(all, cb)
	}
	b.devBoundariesMu.Unlock()
	// The pages' own boundaries come first: the build of a module is the
	// first boundary that claims it.
	own := map[string]bool{}
	for _, cb := range impls.boundaries {
		own[cb.SourcePath] = true
	}
	sort.SliceStable(all, func(i, j int) bool { return own[all[i].SourcePath] && !own[all[j].SourcePath] })
	impls.boundaries = all
	global, parts := b.ssrGlobalSig(), ssrBoundarySigs(impls.boundaries)
	sig := b.ssrBuildSig(global, parts)
	if b.ssrMemo.valid(sig) {
		// Nothing the render modules read changed: the files of the last
		// build are the ones this build would write.
		for _, ci := range impls.claim(impls.boundaries) {
			ci.e.finish(func() error { return nil })
		}
		b.Prof.Add("dev:ssr-modules-reused", 0)
	} else {
		b.ssrMemo = nil
		impls.buildAll(impls.boundaries)
		if impls.combinedInputs != nil {
			b.ssrMemo = &buildMemo{sig: sig, stamps: b.stampInputs(impls.combinedInputs), config: b.projectConfigStamp(), global: global, parts: parts}
		}
	}
	stopCombined()
	defer b.phase("ssr:page-bundles")()
	bundles, _ := compileServerBundlesWith(impls, ssrPages, b.Root, b.Cfg.OutDir, b.Cfg.Sourcemap, b.actionModules(), b.Cfg.BaseURLPath(), b.earlyBundles(), authBanner(b.Cfg))
	return bundles
}

// ssrGlobalSig is what every server render module depends on besides files:
// the action modules they import and the base path.
func (b *Builder) ssrGlobalSig() string {
	return fmt.Sprintf("%v|%s", b.actionModules(), b.Cfg.BaseURLPath())
}

// ssrBoundarySig is what one boundary's server render module depends on
// besides files.
func ssrBoundarySig(cb ClientBoundary) string {
	return fmt.Sprintf("%s\x00%s\x00%s\x00%s\x00%s\x00%v", cb.ModuleID, cb.SourcePath, cb.Spec, cb.entrySource(), cb.resolveDir(), cb.Real)
}

// ssrBoundarySigs signs the boundaries by module. The order they are in only
// says which page's boundaries come first.
func ssrBoundarySigs(boundaries []ClientBoundary) map[string]string {
	parts := make(map[string]string, len(boundaries))
	for _, cb := range boundaries {
		key := cb.SourcePath + "\x00" + cb.Spec
		if _, ok := parts[key]; !ok {
			parts[key] = ssrBoundarySig(cb)
		}
	}
	return parts
}

// ssrBuildSig is what the combined server render build depends on besides
// files: the boundaries it builds and the action modules it imports.
func (b *Builder) ssrBuildSig(global string, parts map[string]string) string {
	keys := make([]string, 0, len(parts))
	for k := range parts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	h := sha256.New()
	h.Write([]byte(global))
	for _, k := range keys {
		fmt.Fprintf(h, "\x00%s\x00%s", k, parts[k])
	}
	return hex.EncodeToString(h.Sum(nil))
}

// serverManifestOutOfStep reports whether a rebuilt page's render mode no
// longer matches its presence in the server manifest: the merge below only
// updates entries that exist, so a page that became request-time (or static)
// needs the whole manifest written again.
func (b *Builder) serverManifestOutOfStep(results []*PageResult) bool {
	known := map[string]bool{}
	if sm, err := readServerManifest(b.Cfg.OutDir); err == nil {
		for _, p := range sm.Pages {
			if !p.Segment {
				known[filepath.ToSlash(p.Source)] = true
			}
		}
	}
	needsServer := map[string]bool{}
	for _, r := range results {
		if r.SourcePath == "" {
			continue
		}
		src := filepath.ToSlash(r.SourcePath)
		if r.Mode != RenderSSG {
			needsServer[src] = true
		} else if _, ok := needsServer[src]; !ok {
			needsServer[src] = false
		}
	}
	for src, need := range needsServer {
		if need != known[src] {
			return true
		}
	}
	return false
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
			bundle, err := compileSingleRuntimeComponent(b.Root, b.Cfg.OutDir, b.Cfg.BaseURLPath(), abs)
			if err != nil {
				fmt.Fprintf(b.stderr(), "  %sRuntime component error (%s):%s %v\n", cRed, filepath.Base(abs), cReset, err)
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
	manifestPath := ManifestPath(b.Cfg.OutDir)
	serverManifestPath := ServerManifestPath(b.Cfg.OutDir)
	readManifest := ResolveManifestPath(b.Cfg.OutDir, "manifest.json")
	readServerManifest := ResolveManifestPath(b.Cfg.OutDir, "server-manifest.json")

	// Full manifest
	var man Manifest
	if data, err := os.ReadFile(readManifest); err == nil {
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

	// Server manifest (read by the Node sidecar)
	var sm ServerManifest
	if data, err := os.ReadFile(readServerManifest); err == nil {
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
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}
