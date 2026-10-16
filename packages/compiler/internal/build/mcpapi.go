package build

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/kratejs/krate/packages/compiler/internal/astjson"
	"github.com/kratejs/krate/packages/compiler/internal/astprint"
	"github.com/kratejs/krate/packages/compiler/internal/routetypes"
)

// RouteSummary describes one route for the MCP `list_routes` tool.
type RouteSummary struct {
	Route      string   `json:"route"`
	Source     string   `json:"source,omitempty"`
	Mode       string   `json:"mode"`
	Params     []string `json:"params,omitempty"`
	StaticOnly bool     `json:"staticOnly,omitempty"`
	Built      bool     `json:"built"`
}

// PageDetailFormat selects which sections PageDetail computes. FormatSource is
// the raw file text, FormatAST the kind-tagged AST document, FormatHTML the
// rendered output (when built).
type PageDetailFormat string

const (
	FormatSource PageDetailFormat = "source"
	FormatAST    PageDetailFormat = "ast"
	FormatHTML   PageDetailFormat = "html"
	FormatAll    PageDetailFormat = "all"
)

// PageDetail is the MCP `read_page` payload. Which sections are populated
// depends on the requested format: Content for source, AST for ast, HTML when
// the site has been built. SourcePath is internal (the runtime uses it to
// resolve the file) and deliberately never crosses the response boundary.
type PageDetail struct {
	Route      string          `json:"route"`
	Source     string          `json:"source"`
	SourcePath string          `json:"-"`
	Mode       string          `json:"mode,omitempty"`
	LossyTypes bool            `json:"lossyTypes"`
	Content    string          `json:"content,omitempty"`
	AST        json.RawMessage `json:"ast,omitempty"`
	HTML       string          `json:"html,omitempty"`
}

// RouteList enumerates the project's routes. When dist/manifest.json exists it
// is the source of truth (it includes plugin- and docs-generated routes plus
// render modes); source pages not yet built are merged in so a freshly added
// file still shows up.
func (b *Builder) RouteList() ([]RouteSummary, error) {
	byRoute := map[string]RouteSummary{}
	var order []string

	// Built routes from the manifest, when present.
	if m, err := readManifestPages(b.Cfg.OutDir); err == nil {
		for _, p := range m.Pages {
			sum := RouteSummary{
				Route:  p.Route,
				Source: p.Source,
				Mode:   p.modeString(),
				Params: routetypes.Params(p.Route),
				Built:  true,
			}
			if _, ok := byRoute[sum.Route]; !ok {
				order = append(order, sum.Route)
			}
			byRoute[sum.Route] = sum
		}
		for _, r := range m.StaticOnlyRoutes {
			if sum, ok := byRoute[r]; ok {
				sum.StaticOnly = true
				byRoute[r] = sum
			}
		}
	}

	// Source routes (covers unbuilt/new pages and static sites).
	pages, err := findPages(b.Cfg.PagesDir)
	if err != nil {
		return nil, err
	}
	for _, p := range pages {
		// 404/500 pages output at the root as error handlers, not navigable
		// routes, so they never appear in RouteList.
		base := strings.TrimSuffix(filepath.Base(p), filepath.Ext(p))
		if base == "404" || base == "500" {
			continue
		}
		out := pageToOutput(p, b.Cfg.PagesDir)
		route := routeFromOutName(out)
		rel, _ := filepath.Rel(b.Root, p)
		rel = filepath.ToSlash(rel)
		if existing, ok := byRoute[route]; ok {
			if existing.Source == "" {
				existing.Source = rel
				byRoute[route] = existing
			}
			continue
		}
		sum := RouteSummary{
			Route:  route,
			Source: rel,
			Mode:   RenderSSG.String(),
			Params: extractParamNames(p, b.Cfg.PagesDir),
		}
		order = append(order, route)
		byRoute[route] = sum
	}

	out := make([]RouteSummary, 0, len(order))
	for _, r := range order {
		out = append(out, byRoute[r])
	}
	return out, nil
}

// PageGraph returns copies of the page->dependencies graph recorded during a
// build on this Builder (empty when it never built). pageDeps is keyed by the
// page's absolute source path; depGraph maps each depended-on file to the pages
// that reference it.
func (b *Builder) PageGraph() (pageDeps map[string][]string, depGraph map[string][]string) {
	b.depMu.Lock()
	defer b.depMu.Unlock()
	pageDeps = make(map[string][]string, len(b.pageDeps))
	for k, v := range b.pageDeps {
		pageDeps[k] = append([]string(nil), v...)
	}
	depGraph = make(map[string][]string, len(b.depGraph))
	for k, v := range b.depGraph {
		depGraph[k] = append([]string(nil), v...)
	}
	return pageDeps, depGraph
}

// PageDetail loads a page by route ("/about") or by source path. It parses the
// source into a kind-tagged AST document and attaches the rendered HTML from
// dist/ when available. Equivalent to PageDetailFor(.., FormatAll).
func (b *Builder) PageDetail(routeOrSource string) (*PageDetail, error) {
	return b.PageDetailFor(routeOrSource, FormatAll)
}

// PageDetailFor loads a page by route or source path, computing only the
// sections requested by format (see PageDetailFormat).
func (b *Builder) PageDetailFor(routeOrSource string, format PageDetailFormat) (*PageDetail, error) {
	src, route, err := b.resolvePageSource(routeOrSource)
	if err != nil {
		return nil, err
	}
	rel, _ := filepath.Rel(b.Root, src)
	rel = filepath.ToSlash(rel)

	detail := &PageDetail{
		Route:      route,
		Source:     rel,
		SourcePath: src,
	}

	wantSource := format == FormatSource || format == FormatAll
	wantAST := format == FormatAST || format == FormatAll
	wantHTML := format == FormatHTML || format == FormatAll

	if wantSource || wantAST {
		data, err := os.ReadFile(src)
		if err != nil {
			return nil, err
		}
		if wantSource {
			detail.Content = string(data)
		}
		if wantAST {
			prog, perrs, dropped := astprint.Parse(string(data))
			if len(perrs) == 0 && prog != nil {
				if doc, err := astjson.EncodeProgram(prog); err == nil {
					detail.AST = doc
				}
			}
			detail.LossyTypes = dropped > 0
		}
	}

	if m, err := readManifestPages(b.Cfg.OutDir); err == nil {
		for _, p := range m.Pages {
			if p.Route == route {
				detail.Mode = p.modeString()
				break
			}
		}
	}
	if wantHTML {
		detail.HTML = readBuiltHTML(b.Cfg.OutDir, route)
	}
	return detail, nil
}

// resolvePageSource maps a route or a source path to an absolute source file.
func (b *Builder) resolvePageSource(routeOrSource string) (src, route string, err error) {
	arg := strings.TrimSpace(routeOrSource)
	if arg == "" {
		return "", "", fmt.Errorf("route or source path is required")
	}

	// Source path forms: "src/pages/about.tsx", "about.tsx", absolute.
	if strings.ContainsAny(arg, `/\`) && hasPageExt(arg) {
		candidates := []string{arg}
		if !filepath.IsAbs(arg) {
			candidates = append(candidates, filepath.Join(b.Root, arg))
		}
		for _, c := range candidates {
			if abs, err := filepath.Abs(c); err == nil {
				if _, statErr := os.Stat(abs); statErr == nil {
					out := pageToOutput(abs, b.Cfg.PagesDir)
					return abs, routeFromOutName(out), nil
				}
			}
		}
		// Fall through: maybe it is a route without a leading slash.
	}

	// Route form.
	norm := routetypes.Normalize(arg)
	if norm == "/" {
		for _, name := range []string{"index.tsx", "index.ts", "index.jsx", "index.js"} {
			p := filepath.Join(b.Cfg.PagesDir, name)
			if _, statErr := os.Stat(p); statErr == nil {
				return p, "/", nil
			}
		}
		return "", "", fmt.Errorf("no root page found in %s", b.Cfg.PagesDir)
	}

	pages, err := findPages(b.Cfg.PagesDir)
	if err != nil {
		return "", "", err
	}
	for _, p := range pages {
		if routeFromOutName(pageToOutput(p, b.Cfg.PagesDir)) == norm {
			return p, norm, nil
		}
	}
	return "", "", fmt.Errorf("no page found for %q", routeOrSource)
}

// readBuiltHTML returns the rendered index.html for a route, or "" when the
// site has not been built.
func readBuiltHTML(outDir, route string) string {
	rel := strings.TrimPrefix(route, "/")
	path := filepath.Join(outDir, filepath.FromSlash(rel), "index.html")
	if rel == "" {
		path = filepath.Join(outDir, "index.html")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(data)
}

func hasPageExt(p string) bool {
	switch strings.ToLower(filepath.Ext(p)) {
	case ".tsx", ".ts", ".jsx", ".js", ".md", ".mdx":
		return true
	}
	return false
}
