package build

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Manifest is written to dist/manifest.json and read by the SSR server at runtime.
type Manifest struct {
	Pages             []PageMeta             `json:"pages"`
	Stylesheet        string                 `json:"stylesheet,omitempty"`        // global CSS filename
	RuntimeJS         string                 `json:"runtimeJS,omitempty"`         // shared runtime chunk path (relative to outDir)
	Routes            map[string]PageMeta    `json:"-"`                           // URL route -> PageMeta (in-memory only)
	RuntimeComponents []RuntimeComponentMeta `json:"runtimeComponents,omitempty"` // runtime server components
	// Regions maps each server-rendered page route to its dynamic regions.
	Regions map[string][]RegionMeta `json:"regions,omitempty"`
	// StaticOnlyRoutes lists dynamic route patterns (e.g. "/blog/[slug]") whose
	// parameters are closed - the server must 404 for params not baked at build
	// time. Populated from `output: "static"` or `dynamicParams = false`.
	StaticOnlyRoutes []string `json:"staticOnlyRoutes,omitempty"`
}

// RuntimeComponentMeta describes a compiled runtime component bundle.
type RuntimeComponentMeta struct {
	Name       string `json:"name"`       // Component name (e.g. "Counter")
	SourcePath string `json:"sourcePath"` // Original source file path
	BundlePath string `json:"bundlePath"` // Compiled JS path relative to outDir
}

// ManifestPage is a simplified version written to disk for the Node.js renderer.
type ManifestPage struct {
	Route      string `json:"route"`
	Source     string `json:"source"`
	Mode       string `json:"mode"`
	Revalidate int    `json:"revalidate,omitempty"`
	BundlePath string `json:"bundlePath"` // path to server bundle (relative to outDir)
}

// ServerManifest is the on-disk format read by the Node.js renderer server.
type ServerManifest struct {
	Pages             []ManifestPage          `json:"pages"`
	Stylesheet        string                  `json:"stylesheet,omitempty"`
	RuntimeJS         string                  `json:"runtimeJS,omitempty"`         // shared runtime chunk path
	RuntimeComponents []RuntimeComponentMeta  `json:"runtimeComponents,omitempty"` // runtime server components
	Regions           map[string][]RegionMeta `json:"regions,omitempty"`
}

// BuildManifest constructs the manifest from page results.
func BuildManifest(results []*PageResult, cssFile string, runtimeJS string) *Manifest {
	m := &Manifest{
		Stylesheet: cssFile,
		RuntimeJS:  runtimeJS,
		Routes:     make(map[string]PageMeta, len(results)),
	}

	for _, r := range results {
		// Skip error pages (404/500) from manifest - they're served directly
		if r.IsErrorPage {
			continue
		}
		meta := PageMeta{
			Route:         routeFromOutName(r.OutName),
			Source:        r.SourcePath,
			Mode:          r.Mode,
			Revalidate:    r.Revalidate,
			DynamicParams: r.DynamicParams,
			StaticOnly:    r.StaticOnly,
		}
		m.Pages = append(m.Pages, meta)
		m.Routes[meta.Route] = meta

		// Record dynamic regions for this page (Suspense primaries + runtime
		// components) so the sidecar knows which regions to render.
		if len(r.Regions) > 0 {
			if m.Regions == nil {
				m.Regions = make(map[string][]RegionMeta)
			}
			regs := make([]RegionMeta, 0, len(r.Regions))
			for _, reg := range r.Regions {
				regs = append(regs, RegionMeta{
					ID:         reg.ID,
					Component:  reg.ComponentName,
					SourcePath: reg.SourcePath,
					Props:      reg.Props,
					Suspense:   reg.Suspense,
					Revalidate: reg.Revalidate,
				})
			}
			m.Regions[meta.Route] = regs
		}
	}

	// Pages are collected in concurrent-build completion order; sort by route so
	// manifest.json is byte-identical across builds (stable output hashing).
	sort.Slice(m.Pages, func(i, j int) bool {
		if m.Pages[i].Route != m.Pages[j].Route {
			return m.Pages[i].Route < m.Pages[j].Route
		}
		return m.Pages[i].Source < m.Pages[j].Source
	})

	return m
}

// SetRuntimeComponents populates the runtime components section of the manifest.
func (m *Manifest) SetRuntimeComponents(bundles []RuntimeComponentBundle) {
	if len(bundles) == 0 {
		return
	}
	m.RuntimeComponents = make([]RuntimeComponentMeta, 0, len(bundles))
	for _, b := range bundles {
		m.RuntimeComponents = append(m.RuntimeComponents, RuntimeComponentMeta(b))
	}
	linkRegionBundles(m, bundles)
}

// linkRegionBundles resolves each region's BundlePath to the already-compiled
// runtime component bundle that backs it. A suspense-primary region renders
// that single runtime component, so the per-runtime-component bundle (a
// self-contained __krate_render IIFE) is exactly the renderer the sidecar
// needs - no per-region esbuild pass required.
func linkRegionBundles(m *Manifest, bundles []RuntimeComponentBundle) {
	if m == nil || len(bundles) == 0 {
		return
	}
	bySource := make(map[string]string, len(bundles))
	for _, b := range bundles {
		bySource[filepath.Clean(b.SourcePath)] = b.BundlePath
	}
	for route, regs := range m.Regions {
		for i := range regs {
			if regs[i].Component == "" || regs[i].BundlePath != "" {
				continue
			}
			if bp, ok := bySource[filepath.Clean(regs[i].SourcePath)]; ok {
				regs[i].BundlePath = bp
			}
		}
		m.Regions[route] = regs
	}
}

// WriteManifest writes both the full manifest (for Go) and the server manifest (for Node.js).
func WriteManifest(m *Manifest, outDir string, serverBundles map[string]string) error {
	// Write full manifest for Go server
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(outDir, "manifest.json"), data, 0644); err != nil {
		return err
	}

	// Write server manifest for Node.js renderer
	serverPages := make([]ManifestPage, 0, len(m.Pages))
	for _, p := range m.Pages {
		if p.Mode == RenderSSG {
			continue // SSG pages don't need the server renderer
		}
		bundlePath := serverBundles[p.Source]
		sp := ManifestPage{
			Route:      p.Route,
			Source:     p.Source,
			Mode:       p.Mode.String(),
			Revalidate: p.Revalidate,
			BundlePath: bundlePath,
		}
		serverPages = append(serverPages, sp)
	}

	if len(serverPages) > 0 {
		sm := ServerManifest{
			Pages:             serverPages,
			Stylesheet:        m.Stylesheet,
			RuntimeJS:         m.RuntimeJS,
			RuntimeComponents: m.RuntimeComponents,
			Regions:           m.Regions,
		}
		smData, err := json.MarshalIndent(sm, "", "  ")
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(outDir, "server-manifest.json"), smData, 0644); err != nil {
			return err
		}
	}

	return nil
}

// routeFromOutName converts an OutName to a URL route.
func routeFromOutName(outName string) string {
	if outName == "." || outName == "" {
		return "/"
	}
	return "/" + strings.TrimPrefix(outName, "/")
}

// HasSSRPages checks if any pages in the results need server-side rendering.
func HasSSRPages(results []*PageResult) bool {
	for _, r := range results {
		if r.Mode != RenderSSG {
			return true
		}
	}
	return false
}
