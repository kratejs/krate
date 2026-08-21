package build

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// Manifest is written to dist/manifest.json and read by the SSR server at runtime.
type Manifest struct {
	Pages             []PageMeta           `json:"pages"`
	Stylesheet        string               `json:"stylesheet,omitempty"`        // global CSS filename
	RuntimeJS         string               `json:"runtimeJS,omitempty"`         // shared runtime chunk path (relative to outDir)
	Routes            map[string]PageMeta  `json:"-"`                           // URL route → PageMeta (in-memory only)
	RuntimeComponents []RuntimeComponentMeta `json:"runtimeComponents,omitempty"` // runtime server components
}

// RuntimeComponentMeta describes a compiled runtime component bundle.
type RuntimeComponentMeta struct {
	Name       string `json:"name"`       // Component name (e.g. "Counter")
	SourcePath string `json:"sourcePath"` // Original source file path
	BundlePath string `json:"bundlePath"` // Compiled JS path relative to outDir
}

// ManifestPage is a simplified version written to disk for the Node.js renderer.
type ManifestPage struct {
	Route     string `json:"route"`
	Source    string `json:"source"`
	Mode      string `json:"mode"`
	Revalidate int  `json:"revalidate,omitempty"`
	BundlePath string `json:"bundlePath"` // path to server bundle (relative to outDir)
}

// ServerManifest is the on-disk format read by the Node.js renderer server.
type ServerManifest struct {
	Pages             []ManifestPage      `json:"pages"`
	Stylesheet        string              `json:"stylesheet,omitempty"`
	RuntimeJS         string              `json:"runtimeJS,omitempty"`         // shared runtime chunk path
	RuntimeComponents []RuntimeComponentMeta `json:"runtimeComponents,omitempty"` // runtime server components
}

// BuildManifest constructs the manifest from page results.
func BuildManifest(results []*PageResult, cssFile string, runtimeJS string) *Manifest {
	m := &Manifest{
		Stylesheet: cssFile,
		RuntimeJS:  runtimeJS,
		Routes:     make(map[string]PageMeta, len(results)),
	}

	for _, r := range results {
		// Skip error pages (404/500) from manifest — they're served directly
		if r.IsErrorPage {
			continue
		}
		meta := PageMeta{
			Route:     routeFromOutName(r.OutName),
			Source:    r.SourcePath,
			Mode:      r.Mode,
			Revalidate: r.Revalidate,
		}
		m.Pages = append(m.Pages, meta)
		m.Routes[meta.Route] = meta
	}

	return m
}

// SetRuntimeComponents populates the runtime components section of the manifest.
func (m *Manifest) SetRuntimeComponents(bundles []RuntimeComponentBundle) {
	if len(bundles) == 0 {
		return
	}
	m.RuntimeComponents = make([]RuntimeComponentMeta, 0, len(bundles))
	for _, b := range bundles {
		m.RuntimeComponents = append(m.RuntimeComponents, RuntimeComponentMeta{
			Name:       b.Name,
			SourcePath: b.SourcePath,
			BundlePath: b.BundlePath,
		})
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
