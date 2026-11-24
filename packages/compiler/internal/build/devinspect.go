package build

import (
	"encoding/json"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/kratejs/krate/packages/compiler/internal/check"
	"github.com/kratejs/krate/packages/compiler/internal/config"
	"github.com/kratejs/krate/packages/compiler/internal/routetypes"
)

// devManifest is the part of the built manifest the overlay reads, decoded
// leniently (the mode is a number in manifest.json).
type devManifest struct {
	Pages            []devManifestPage `json:"pages"`
	RuntimeJS        string            `json:"runtimeJS"`
	StaticOnlyRoutes []string          `json:"staticOnlyRoutes"`
}

type devManifestPage struct {
	Route         string          `json:"route"`
	Source        string          `json:"source"`
	Mode          json.RawMessage `json:"mode"`
	Revalidate    int             `json:"revalidate"`
	DynamicParams *bool           `json:"dynamicParams"`
	StaticOnly    bool            `json:"staticOnly"`
}

func (p devManifestPage) mode() string {
	return manifestPageInfo{Mode: p.Mode}.modeString()
}

func readDevManifest(outDir string) (devManifest, bool) {
	var m devManifest
	data, err := os.ReadFile(ResolveManifestPath(outDir, "manifest.json"))
	if err != nil || json.Unmarshal(data, &m) != nil {
		return m, false
	}
	return m, true
}

// devRoutePattern is the route pattern of a page: the one its source file's
// place in the pages directory gives ("/blog/[slug]"), or its built route
// when the source is not a page file there (plugin and docs routes).
func devRoutePattern(root string, cfg *config.Config, p devManifestPage) string {
	if p.Source != "" && cfg != nil && cfg.PagesDir != "" {
		abs := p.Source
		if !filepath.IsAbs(abs) {
			abs = filepath.Join(root, filepath.FromSlash(abs))
		}
		if isWithin(abs, cfg.PagesDir) && hasPageExt(abs) {
			if out := pageToOutput(abs, cfg.PagesDir); out != "" {
				return routeFromOutName(out)
			}
		}
	}
	return p.Route
}

// DevRouteSizes are the bytes a page ships (raw and gzipped).
type DevRouteSizes struct {
	HTML        int64 `json:"html"`
	JS          int64 `json:"js"`
	CSS         int64 `json:"css"`
	GzipHTML    int64 `json:"gzipHtml"`
	GzipJS      int64 `json:"gzipJs"`
	GzipCSS     int64 `json:"gzipCss"`
	GzipRuntime int64 `json:"gzipRuntime"`
	GzipIslands int64 `json:"gzipIslands"`
}

// DevRouteIsland is one island chunk a page loads. ID is the island's
// data-krate-island id when the chunk names one, else the chunk's name.
type DevRouteIsland struct {
	ID    string `json:"id"`
	Chunk string `json:"chunk"`
	Gzip  int64  `json:"gzip"`
}

// DevRouteBudget holds the configured checks.budget values in bytes.
type DevRouteBudget struct {
	JS        int `json:"js,omitempty"`
	Hydration int `json:"hydration,omitempty"`
	IslandJS  int `json:"islandJS,omitempty"`
	Route     int `json:"route,omitempty"`
}

// DevRouteInfo is the payload of GET /__krate/dev/route.
type DevRouteInfo struct {
	Path        string            `json:"path"`
	Pattern     string            `json:"pattern"`
	Source      string            `json:"source"`
	Mode        string            `json:"mode"`
	Revalidate  int               `json:"revalidate,omitempty"`
	Params      map[string]string `json:"params"`
	Dynamic     bool              `json:"dynamic"`
	StaticPaths int               `json:"staticPaths"`
	Layouts     []string          `json:"layouts"`
	Sizes       *DevRouteSizes    `json:"sizes,omitempty"`
	Islands     []DevRouteIsland  `json:"islands"`
	Budget      *DevRouteBudget   `json:"budget,omitempty"`
	BuiltAt     int64             `json:"builtAt,omitempty"` // unix ms of the built document
	Document    string            `json:"document,omitempty"`
}

// devURLRoute turns a request URL path into the route it names: no query,
// no site base path, no trailing slash ("/" for the root).
func devURLRoute(urlPath, basePath string) string {
	if i := strings.IndexAny(urlPath, "?#"); i >= 0 {
		urlPath = urlPath[:i]
	}
	p := path.Clean("/" + urlPath)
	if base := strings.TrimRight(basePath, "/"); base != "" && base != "/" {
		if p == base {
			p = "/"
		} else if strings.HasPrefix(p, base+"/") {
			p = strings.TrimPrefix(p, base)
		}
	}
	if p != "/" {
		p = strings.TrimSuffix(p, "/")
	}
	if strings.HasSuffix(p, "/index.html") {
		p = strings.TrimSuffix(p, "/index.html")
		if p == "" {
			p = "/"
		}
	} else if strings.HasSuffix(p, ".html") {
		p = strings.TrimSuffix(p, ".html")
	}
	return p
}

// matchDevPage finds the manifest page serving route: an exact route first
// (a page or a prebuilt generateStaticParams path), then the most specific
// dynamic pattern.
func matchDevPage(root string, cfg *config.Config, m devManifest, route string) (devManifestPage, string, map[string]string, bool) {
	for _, p := range m.Pages {
		if p.Route == route {
			return p, devRoutePattern(root, cfg, p), patternParams(route, devRoutePattern(root, cfg, p)), true
		}
	}
	byPattern := map[string]devManifestPage{}
	var patterns []string
	for _, p := range m.Pages {
		pat := devRoutePattern(root, cfg, p)
		if !isDynamicPattern(strings.TrimPrefix(pat, "/")) {
			continue
		}
		key := strings.TrimPrefix(pat, "/")
		if _, seen := byPattern[key]; !seen {
			patterns = append(patterns, key)
		}
		// Prefer the template's own entry for its mode and source.
		if prev, seen := byPattern[key]; !seen || p.Route == pat || prev.Route != pat {
			byPattern[key] = p
		}
	}
	sortRoutePatterns(patterns)
	for _, pat := range patterns {
		if params, ok := matchRoutePattern(route, pat); ok {
			return byPattern[pat], "/" + pat, params, true
		}
	}
	return devManifestPage{}, "", nil, false
}

func patternParams(route, pattern string) map[string]string {
	if !isDynamicPattern(strings.TrimPrefix(pattern, "/")) {
		return map[string]string{}
	}
	if params, ok := matchRoutePattern(route, strings.TrimPrefix(pattern, "/")); ok {
		return params
	}
	return map[string]string{}
}

// devRouteInfo describes the built page that serves urlPath, from the dev
// output. ok is false when no built page serves it.
func devRouteInfo(root string, cfg *config.Config, outDir, urlPath string) (*DevRouteInfo, bool) {
	m, ok := readDevManifest(outDir)
	if !ok {
		return nil, false
	}
	route := devURLRoute(urlPath, cfg.BaseURLPath())
	page, pattern, params, ok := matchDevPage(root, cfg, m, route)
	if !ok {
		return nil, false
	}
	if params == nil {
		params = map[string]string{}
	}
	info := &DevRouteInfo{
		Path:       route,
		Pattern:    pattern,
		Source:     page.Source,
		Mode:       page.mode(),
		Revalidate: page.Revalidate,
		Params:     params,
		Dynamic:    isDynamicPattern(strings.TrimPrefix(pattern, "/")),
		Layouts:    []string{},
		Islands:    []DevRouteIsland{},
	}
	if info.Dynamic && page.Source != "" {
		for _, p := range m.Pages {
			if p.Source == page.Source && !isDynamicPattern(strings.TrimPrefix(p.Route, "/")) {
				info.StaticPaths++
			}
		}
	}
	doc := builtDocPath(outDir, route)
	if doc == "" {
		doc = builtDocPath(outDir, pattern)
	}
	if doc != "" {
		if rel, err := filepath.Rel(outDir, doc); err == nil {
			info.Document = filepath.ToSlash(rel)
		}
		if st, err := os.Stat(doc); err == nil {
			info.BuiltAt = st.ModTime().UnixMilli()
		}
		if data, err := os.ReadFile(doc); err == nil {
			html := string(data)
			info.Layouts = routeChainFiles(html)
			sizes, islands := measureDevPage(outDir, strings.Trim(cfg.BaseURLPath(), "/"), html, m.RuntimeJS)
			info.Sizes = &sizes
			info.Islands = islands
		}
	}
	if chk, err := check.FromMap(cfg.Checks, root, nil); err == nil {
		b := DevRouteBudget{JS: chk.JSBudgetBytes, Hydration: chk.HydrationBudgetBytes, IslandJS: chk.IslandBudgetBytes, Route: chk.RouteBudgetBytes}
		if b != (DevRouteBudget{}) {
			info.Budget = &b
		}
	}
	return info, true
}

var devRouteInfoRe = regexp.MustCompile(`(?is)<script\b[^>]*\bid=["']?__krate_route\b["']?[^>]*>(.*?)</script>`)

// routeChainFiles returns the layout and template files of a built page,
// outermost first, from its route info block.
func routeChainFiles(html string) []string {
	out := []string{}
	m := devRouteInfoRe.FindStringSubmatch(html)
	if m == nil {
		return out
	}
	var doc struct {
		Chain []struct {
			File string `json:"f"`
		} `json:"chain"`
	}
	if json.Unmarshal([]byte(m[1]), &doc) != nil {
		return out
	}
	for _, e := range doc.Chain {
		if e.File != "" {
			out = append(out, e.File)
		}
	}
	return out
}

var islandIDRe = regexp.MustCompile(`data-krate-island=\\?["']([^"'\\]+)\\?["']`)

// measureDevPage sizes a built page and lists its island chunks.
func measureDevPage(outDir, base, html, runtimeJS string) (DevRouteSizes, []DevRouteIsland) {
	var s DevRouteSizes
	s.HTML, s.GzipHTML = int64(len(html)), gzipLen([]byte(html))
	load := pageLoadOf(outDir, base, html)
	files := fileMeasurer{}
	runtimeAbs := ""
	if runtimeJS != "" {
		runtimeAbs = filepath.Join(outDir, filepath.FromSlash(strings.TrimPrefix(runtimeJS, "/")))
	}
	islands := []DevRouteIsland{}
	for _, ref := range load.Scripts {
		abs := resolveOutURL(outDir, base, ref)
		if abs == "" {
			continue
		}
		f, ok := files.measure(abs)
		if !ok {
			continue
		}
		s.JS += f.raw
		s.GzipJS += f.gz
		switch {
		case abs == runtimeAbs || strings.HasPrefix(path.Base(ref), "runtime."):
			s.GzipRuntime += f.gz
		case isIslandEntry(ref):
			s.GzipIslands += f.gz
			chunk := strings.TrimPrefix(ref, "/")
			ids := islandIDsOf(abs)
			if len(ids) == 0 {
				ids = []string{strings.TrimSuffix(path.Base(ref), ".js")}
			}
			for _, id := range ids {
				islands = append(islands, DevRouteIsland{ID: id, Chunk: chunk, Gzip: f.gz})
			}
		}
	}
	for _, ref := range load.Styles {
		if abs := resolveOutURL(outDir, base, ref); abs != "" {
			if f, ok := files.measure(abs); ok {
				s.CSS += f.raw
				s.GzipCSS += f.gz
			}
		}
	}
	return s, islands
}

// islandIDsOf lists the island ids an island chunk mounts.
func islandIDsOf(abs string) []string {
	data, err := os.ReadFile(abs)
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	var ids []string
	for _, m := range islandIDRe.FindAllStringSubmatch(string(data), -1) {
		if !seen[m[1]] {
			seen[m[1]] = true
			ids = append(ids, m[1])
		}
	}
	sort.Strings(ids)
	return ids
}

// DevRouteEntry is one route of GET /__krate/dev/routes.
type DevRouteEntry struct {
	Pattern    string   `json:"pattern"`
	Source     string   `json:"source,omitempty"`
	Mode       string   `json:"mode,omitempty"`
	Revalidate int      `json:"revalidate,omitempty"`
	Kind       string   `json:"kind"` // "page" or "api"
	Methods    []string `json:"methods,omitempty"`
	Paths      []string `json:"paths"`
}

// devRoutePathsMax caps the concrete paths listed per route.
const devRoutePathsMax = 50

// devRouteList lists the built pages by pattern, then the API routes.
func devRouteList(root string, cfg *config.Config, outDir string, api []DevRouteEntry) []DevRouteEntry {
	m, _ := readDevManifest(outDir)
	byPattern := map[string]*DevRouteEntry{}
	var order []string
	for _, p := range m.Pages {
		pat := devRoutePattern(root, cfg, p)
		e, ok := byPattern[pat]
		if !ok {
			e = &DevRouteEntry{Pattern: pat, Source: p.Source, Kind: "page", Paths: []string{}}
			byPattern[pat] = e
			order = append(order, pat)
		}
		if p.Route == pat || e.Mode == "" {
			e.Mode, e.Revalidate = p.mode(), p.Revalidate
		}
		dynamic := isDynamicPattern(strings.TrimPrefix(p.Route, "/"))
		if !dynamic && len(e.Paths) < devRoutePathsMax {
			e.Paths = append(e.Paths, p.Route)
		}
	}
	sort.Strings(order)
	out := make([]DevRouteEntry, 0, len(order)+len(api))
	for _, pat := range order {
		e := byPattern[pat]
		sort.Strings(e.Paths)
		out = append(out, *e)
	}
	sort.SliceStable(api, func(i, j int) bool { return api[i].Pattern < api[j].Pattern })
	return append(out, api...)
}

// devAPIRoutes lists the JS/TS API routes under src/api and the Go API
// routes of the route manifest, with their methods where known.
func devAPIRoutes(root string, goRoutes map[string][]string) []DevRouteEntry {
	var out []DevRouteEntry
	seen := map[string]int{}
	apiDir := filepath.Join(root, "src", "api")
	_ = filepath.WalkDir(apiDir, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if strings.HasPrefix(d.Name(), "_") || strings.HasPrefix(d.Name(), ".") || d.Name() == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasPrefix(d.Name(), "_") || !isAPIRouteSource(p) || strings.HasSuffix(d.Name(), ".d.ts") || strings.HasSuffix(d.Name(), ".go") {
			return nil
		}
		rel, err := filepath.Rel(apiDir, p)
		if err != nil {
			return nil
		}
		rel = filepath.ToSlash(strings.TrimSuffix(rel, filepath.Ext(rel)))
		rel = strings.TrimSuffix(strings.TrimSuffix(rel, "/index"), "index")
		pattern := strings.TrimSuffix("/api/"+rel, "/")
		src, _ := filepath.Rel(root, p)
		seen[pattern] = len(out)
		out = append(out, DevRouteEntry{Pattern: pattern, Source: filepath.ToSlash(src), Kind: "api", Paths: apiPaths(pattern)})
		return nil
	})
	var goPatterns []string
	for pat := range goRoutes {
		goPatterns = append(goPatterns, pat)
	}
	sort.Strings(goPatterns)
	for _, pat := range goPatterns {
		methods := append([]string(nil), goRoutes[pat]...)
		sort.Strings(methods)
		if i, ok := seen[pat]; ok {
			// Go routes take precedence for the same path.
			out[i].Methods = methods
			out[i].Source = ""
			continue
		}
		out = append(out, DevRouteEntry{Pattern: pat, Kind: "api", Methods: methods, Paths: apiPaths(pat)})
	}
	return out
}

func apiPaths(pattern string) []string {
	if isDynamicPattern(strings.TrimPrefix(pattern, "/")) || len(routetypes.Params(pattern)) > 0 {
		return []string{}
	}
	return []string{pattern}
}

// devPageRoute is the route pattern of a built page for the overlay's
// bootstrap: "/blog/[slug]" for every page generateStaticParams expands.
func (b *Builder) devPageRoute(r *PageResult) string {
	if r.IsErrorPage {
		return devRouteLabel(r.Page, b.Cfg.PagesDir)
	}
	if r.Page != "" && isWithin(r.Page, b.Cfg.PagesDir) && hasPageExt(r.Page) {
		if out := pageToOutput(r.Page, b.Cfg.PagesDir); out != "" {
			return routeFromOutName(out)
		}
	}
	return routeFromOutName(r.OutName)
}
