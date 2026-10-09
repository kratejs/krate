package build

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// SiteGraph is the machine-readable model of a compiled site: every route with
// its render mode, source, dependency/dependent edges, and emitted output
// files. It backs `krate inspect --json`, the MCP `explain` tool, and the
// krate://graph resource.
type SiteGraph struct {
	Root   string              `json:"root"`
	Routes []SiteGraphRoute    `json:"routes"`
	Files  map[string][]string `json:"files,omitempty"` // file → routes that depend on it
}

// SiteGraphRoute is one route in a SiteGraph.
type SiteGraphRoute struct {
	Route        string   `json:"route"`
	Source       string   `json:"source,omitempty"`
	Mode         string   `json:"mode"`
	Params       []string `json:"params,omitempty"`
	Built        bool     `json:"built"`
	StaticOnly   bool     `json:"staticOnly,omitempty"`
	Dependencies []string `json:"dependencies,omitempty"`
	Dependents   []string `json:"dependents,omitempty"`
	Outputs      []string `json:"outputs,omitempty"`
	Bytes        int64    `json:"bytes,omitempty"` // total size of the route's emitted files
}

// SiteGraph assembles the compiled-site model from the Builder's current state.
// Callers should run BuildAll first so the manifest and dependency graph are
// populated.
func (b *Builder) SiteGraph() (*SiteGraph, error) {
	routes, err := b.RouteList()
	if err != nil {
		return nil, err
	}
	pageDeps, depGraph := b.PageGraph()

	g := &SiteGraph{Root: ".", Routes: make([]SiteGraphRoute, 0, len(routes))}
	for _, r := range routes {
		n := SiteGraphRoute{
			Route:      r.Route,
			Source:     filepath.ToSlash(r.Source),
			Mode:       r.Mode,
			Params:     r.Params,
			Built:      r.Built,
			StaticOnly: r.StaticOnly,
		}
		var abs string
		if r.Source != "" {
			abs = filepath.Join(b.Root, filepath.FromSlash(r.Source))
		}
		if abs != "" {
			n.Dependencies = b.relPaths(pageDeps[abs])
			n.Dependents = b.relPaths(depGraph[abs])
		}
		n.Outputs, n.Bytes = b.routeOutputs(r.Route)
		g.Routes = append(g.Routes, n)
	}

	if len(depGraph) > 0 {
		files := make(map[string][]string, len(depGraph))
		for f, pages := range depGraph {
			files[b.relPath(f)] = b.relPaths(pages)
		}
		g.Files = files
	}
	return g, nil
}

// relPath converts a path to project-relative, slash-separated form when it is
// inside the project.
func (b *Builder) relPath(p string) string {
	if rel, err := filepath.Rel(b.Root, p); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return filepath.ToSlash(rel)
	}
	return filepath.ToSlash(p)
}

func (b *Builder) relPaths(paths []string) []string {
	if len(paths) == 0 {
		return nil
	}
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		out = append(out, b.relPath(p))
	}
	return out
}

// routeOutputs lists the files emitted under a route's output directory and
// their total size. The root route has no subdirectory, so only its own
// top-level files are listed (a full-tree walk there would collect the whole
// site).
func (b *Builder) routeOutputs(route string) ([]string, int64) {
	outName := strings.Trim(route, "/")
	dir := filepath.Join(b.Cfg.OutDir, filepath.FromSlash(outName))

	var outs []string
	var total int64
	if outName == "" {
		entries, err := os.ReadDir(dir)
		if err == nil {
			for _, e := range entries {
				if e.IsDir() {
					continue
				}
				outs = append(outs, filepath.ToSlash(e.Name()))
				if fi, ierr := e.Info(); ierr == nil {
					total += fi.Size()
				}
			}
		}
		sort.Strings(outs)
		return outs, total
	}

	_ = filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if rel, rerr := filepath.Rel(b.Cfg.OutDir, p); rerr == nil {
			outs = append(outs, filepath.ToSlash(rel))
		}
		if fi, ferr := d.Info(); ferr == nil {
			total += fi.Size()
		}
		return nil
	})
	sort.Strings(outs)
	return outs, total
}
