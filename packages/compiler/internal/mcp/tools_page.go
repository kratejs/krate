package mcp

import (
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/kratejs/krate/packages/compiler/internal/build"
	"github.com/kratejs/krate/packages/compiler/internal/routetypes"
)

// pageRef is a resolved route: the route pattern, its source file and mode.
type pageRef struct {
	Route  string
	Source string
	Mode   string
	Params []string
}

// matchRoutePattern reports whether a concrete path matches a route pattern
// such as /blog/[slug] or /docs/[...rest].
func matchRoutePattern(pattern, p string) bool {
	ps := strings.Split(strings.Trim(pattern, "/"), "/")
	us := strings.Split(strings.Trim(p, "/"), "/")
	if pattern == "/" || pattern == "" {
		return strings.Trim(p, "/") == ""
	}
	for i, seg := range ps {
		if strings.HasPrefix(seg, "[[...") || strings.HasPrefix(seg, "[...") {
			return i < len(us) || strings.HasPrefix(seg, "[[...")
		}
		if i >= len(us) {
			return false
		}
		if strings.HasPrefix(seg, "[") && strings.HasSuffix(seg, "]") {
			continue
		}
		if seg != us[i] {
			return false
		}
	}
	return len(ps) == len(us)
}

// findPage resolves a route (pattern or concrete path) or a source path.
func (s *Service) findPage(target string) (pageRef, error) {
	if target == "" {
		return pageRef{}, fmt.Errorf("provide route (e.g. /about) or source")
	}
	routes, err := s.builder.RouteList()
	if err != nil {
		return pageRef{}, err
	}
	norm := routetypes.Normalize(target)
	for _, r := range routes {
		if r.Route == norm || (r.Source != "" && r.Source == filepath.ToSlash(target)) {
			return pageRef{Route: r.Route, Source: r.Source, Mode: r.Mode, Params: r.Params}, nil
		}
	}
	// A concrete path against dynamic patterns, most specific (fewest params)
	// first.
	var cands []build.RouteSummary
	for _, r := range routes {
		if matchRoutePattern(r.Route, norm) {
			cands = append(cands, r)
		}
	}
	sort.SliceStable(cands, func(i, j int) bool { return len(cands[i].Params) < len(cands[j].Params) })
	if len(cands) > 0 {
		r := cands[0]
		return pageRef{Route: r.Route, Source: r.Source, Mode: r.Mode, Params: r.Params}, nil
	}
	return pageRef{}, fmt.Errorf("no route matches %q; call list_routes to see the routes", target)
}

func (s *Service) pageTarget(args map[string]any) string {
	if t := argString(args, "route"); t != "" {
		return t
	}
	return argString(args, "source")
}

func (s *Service) outDirAbs() string {
	out := s.cfg.OutDir
	if !filepath.IsAbs(out) {
		out = filepath.Join(s.root, out)
	}
	return filepath.Clean(out)
}

// builtHTMLFile returns the project-relative and absolute path of a route's
// built document, or "" when it has not been built.
func (s *Service) builtHTMLFile(route string) (abs string) {
	out := s.outDirAbs()
	trim := strings.Trim(route, "/")
	cands := []string{filepath.Join(out, filepath.FromSlash(trim), "index.html")}
	if trim != "" {
		cands = append(cands, filepath.Join(out, filepath.FromSlash(trim)+".html"))
	}
	for _, c := range cands {
		if info, err := os.Stat(c); err == nil && !info.IsDir() {
			return c
		}
	}
	return ""
}

func (s *Service) readBuilt(route string, max int64) ([]byte, string, error) {
	abs := s.builtHTMLFile(route)
	if abs == "" {
		return nil, "", fmt.Errorf("route %s has not been built (no output in %s); run the build tool first", route, s.relToRoot(s.outDirAbs()))
	}
	f, err := os.Open(abs)
	if err != nil {
		return nil, "", err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, max+1))
	if err != nil {
		return nil, "", err
	}
	if int64(len(data)) > max {
		return nil, "", fmt.Errorf("%s is larger than %d bytes", s.relToRoot(abs), max)
	}
	return data, s.relToRoot(abs), nil
}

// assetCost sizes an emitted asset referenced from a page (raw and gzip).
type assetCost struct {
	URL   string `json:"url"`
	Path  string `json:"path,omitempty"`
	Type  string `json:"type"`
	Bytes int64  `json:"bytes"`
	Gzip  int64  `json:"gzipBytes"`
}

func gzipSize(data []byte) int64 {
	var buf bytes.Buffer
	zw, _ := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	_, _ = zw.Write(data)
	_ = zw.Close()
	return int64(buf.Len())
}

// assetFile maps a page-relative or root-relative URL to a file in outDir.
func (s *Service) assetFile(url string) string {
	if strings.Contains(url, "://") || strings.HasPrefix(url, "//") || strings.HasPrefix(url, "data:") {
		return ""
	}
	u := strings.SplitN(strings.SplitN(url, "?", 2)[0], "#", 2)[0]
	if bp := s.cfg.BaseURLPath(); bp != "" {
		u = strings.TrimPrefix(u, bp)
	}
	u = strings.TrimPrefix(u, "/")
	abs := filepath.Join(s.outDirAbs(), filepath.FromSlash(u))
	if rel, err := filepath.Rel(s.outDirAbs(), abs); err != nil || strings.HasPrefix(rel, "..") {
		return ""
	}
	return abs
}

const maxAssetRead = 8 << 20

func (s *Service) costOf(url, typ string) (assetCost, bool) {
	f := s.assetFile(url)
	if f == "" {
		return assetCost{}, false
	}
	info, err := os.Stat(f)
	if err != nil || info.IsDir() {
		return assetCost{}, false
	}
	c := assetCost{URL: url, Path: s.relToRoot(f), Type: typ, Bytes: info.Size()}
	if info.Size() <= maxAssetRead {
		if data, err := os.ReadFile(f); err == nil {
			c.Gzip = gzipSize(data)
		}
	}
	return c, true
}

// pageAssets lists the JS and CSS a built page loads with their cost.
func (s *Service) pageAssets(info *htmlInfo, htmlBytes int) ([]assetCost, map[string]int64) {
	var out []assetCost
	seen := map[string]bool{}
	add := func(url, typ string) {
		if seen[url] {
			return
		}
		seen[url] = true
		if c, ok := s.costOf(url, typ); ok {
			out = append(out, c)
		}
	}
	for _, sc := range info.Scripts {
		if sc.Src != "" {
			add(sc.Src, "js")
		}
	}
	for _, l := range info.Links {
		if l.Rel == "modulepreload" || (l.Rel == "preload" && l.As == "script") {
			add(l.Href, "js")
		}
	}
	for _, st := range info.Stylesheets {
		add(st, "css")
	}
	totals := map[string]int64{"htmlBytes": int64(htmlBytes)}
	for _, a := range out {
		totals[a.Type+"Bytes"] += a.Bytes
		totals[a.Type+"Gzip"] += a.Gzip
	}
	return out, totals
}

var skipPackages = map[string]bool{"krate": true, "react": true, "react-dom": true, "preact": true, "@krate/runtime": true, "@krate/components": true}

func isFrameworkPackage(spec string) bool {
	name := packageName(spec)
	return skipPackages[name] || strings.HasPrefix(name, "@krate/") || strings.HasPrefix(spec, "krate/") || strings.HasPrefix(spec, "node:")
}

// pageGraph builds the static module graph for a page and its layouts.
func (s *Service) pageGraph(p pageRef) (*moduleGraph, []string) {
	layouts := s.layoutChain(p.Source)
	entries := append(append([]string{}, layouts...), p.Source)
	return s.buildModuleGraph(entries), layouts
}

type islandInfo struct {
	Type       string            `json:"type"` // page, client-component, package-island
	Name       string            `json:"name"`
	File       string            `json:"file,omitempty"`
	Tier       string            `json:"tier,omitempty"`
	Directive  string            `json:"directive,omitempty"`
	Reasons    []hydrationReason `json:"reasons,omitempty"`
	ImportedBy []string          `json:"importedBy,omitempty"`
}

// shortReasons keeps the most telling reasons first: hooks, handlers and
// directives before the weaker browser-API hints.
func shortReasons(rs []hydrationReason, max int) []hydrationReason {
	out := make([]hydrationReason, 0, len(rs))
	for _, r := range rs {
		if r.Kind != "browser-api" {
			out = append(out, r)
		}
	}
	for _, r := range rs {
		if r.Kind == "browser-api" {
			out = append(out, r)
		}
	}
	if len(out) > max {
		out = out[:max]
	}
	return out
}

// islandsOf finds the parts of a page that need the browser.
func (s *Service) islandsOf(g *moduleGraph) []islandInfo {
	var out []islandInfo
	directives := map[string]string{}
	for _, mod := range g.Modules {
		for _, use := range mod.jsx {
			if use.Directive == "" {
				continue
			}
			head := strings.SplitN(use.Tag, ".", 2)[0]
			if spec, ok := mod.localOf[head]; ok {
				if kind, res := s.resolveImport(mod.Path, spec); kind == "local" {
					directives[res] = use.Directive
				}
			}
		}
	}
	for _, rel := range g.Order {
		m := g.Modules[rel]
		if m.Tier != "client" || !m.interactive() {
			continue
		}
		typ := "client-component"
		if rel == g.Entry {
			typ = "page"
		}
		out = append(out, islandInfo{
			Type: typ, Name: strings.TrimSuffix(path.Base(rel), path.Ext(rel)), File: rel, Tier: m.Tier,
			Directive: directives[rel], Reasons: shortReasons(m.Reasons, 6),
		})
	}
	var pkgs []string
	for p := range g.Packages {
		pkgs = append(pkgs, p)
	}
	sort.Strings(pkgs)
	for _, p := range pkgs {
		var importers []string
		for _, imp := range g.Packages[p] {
			m := g.Modules[imp]
			if m == nil || m.Tier != "client" || isFrameworkPackage(p) {
				continue
			}
			importers = append(importers, imp)
		}
		if len(importers) > 0 {
			out = append(out, islandInfo{Type: "package-island", Name: p, ImportedBy: importers})
		}
	}
	return out
}

func tierCounts(g *moduleGraph) map[string]int {
	c := map[string]int{}
	for _, m := range g.Modules {
		c[m.Tier]++
	}
	return c
}

func (s *Service) toolExplainPage(ctx context.Context, args map[string]any) (ToolResult, *rpcError) {
	p, err := s.findPage(s.pageTarget(args))
	if err != nil {
		return ErrorResult(err.Error()), nil
	}
	if p.Source == "" {
		return ErrorResult("route " + p.Route + " has no source file (generated by a plugin); explain_page analyses authored sources"), nil
	}
	g, layouts := s.pageGraph(p)
	page := g.Modules[p.Source]
	if page == nil {
		return ErrorResult("cannot read " + p.Source), nil
	}
	islands := s.islandsOf(g)

	hydratingComponents := 0
	for _, is := range islands {
		if is.Type != "package-island" {
			hydratingComponents++
		}
	}
	out := map[string]any{
		"route":   p.Route,
		"source":  p.Source,
		"mode":    p.Mode,
		"modeWhy": modeExplanation(p.Mode, page),
		"layouts": layouts,
		"page": map[string]any{
			"tier": page.Tier, "tierSource": page.TierSource, "exports": page.Exports, "parseError": page.ParseError,
		},
		"moduleCount":        len(g.Modules),
		"tiers":              tierCounts(g),
		"islands":            emptyIfNil(islands),
		"hydratesComponents": hydratingComponents > 0,
	}
	if g.Truncated {
		out["note"] = fmt.Sprintf("module graph truncated at %d modules", maxGraphModules)
	}

	// Built output: what the browser actually downloads.
	if data, rel, rerr := s.readBuilt(p.Route, 8<<20); rerr == nil {
		info := parseHTMLInfo(data)
		assets, totals := s.pageAssets(info, len(data))
		out["built"] = map[string]any{
			"html": rel, "shipsJavaScript": len(info.Scripts) > 0, "scripts": info.Scripts,
			"assets": assets, "totals": totals,
		}
	} else {
		out["built"] = map[string]any{"built": false, "hint": rerr.Error()}
	}

	serverMods, buildTime := []map[string]any{}, []map[string]any{}
	for _, rel := range g.Order {
		m := g.Modules[rel]
		switch m.Tier {
		case "server", "static":
			buildTime = append(buildTime, map[string]any{"file": rel, "tier": m.Tier, "tierSource": m.TierSource})
		case "runtime":
			serverMods = append(serverMods, map[string]any{"file": rel, "tier": m.Tier, "tierSource": m.TierSource})
		}
	}
	out["buildTimeOnly"] = buildTime
	out["requestTime"] = serverMods

	mods := make([]map[string]any, 0, len(g.Order))
	for _, rel := range g.Order {
		m := g.Modules[rel]
		mods = append(mods, map[string]any{"file": rel, "tier": m.Tier, "interactive": m.interactive(), "sourceBytes": m.Bytes})
	}
	if len(mods) > 60 {
		out["modulesTruncated"] = len(mods) - 60
		mods = mods[:60]
	}
	out["modules"] = mods
	out["moduleNote"] = "sourceBytes is the size of the source file, not its share of a bundle; per-asset sizes under built.assets are exact."
	return JSONResult(out)
}

func modeExplanation(mode string, page *moduleInfo) string {
	switch strings.ToLower(mode) {
	case "ssg", "static":
		return "rendered once at build time into static HTML"
	case "ssr":
		return "rendered per request by the SSR sidecar (the page is dynamic, e.g. dynamic export, async runtime component or request-time data)"
	case "isr":
		return "static HTML regenerated in the background on the page's revalidate interval"
	case "streaming":
		return "shell served immediately; Suspense regions stream in per request"
	}
	return ""
}

func (s *Service) toolWhyHydrated(ctx context.Context, args map[string]any) (ToolResult, *rpcError) {
	p, err := s.findPage(s.pageTarget(args))
	if err != nil {
		return ErrorResult(err.Error()), nil
	}
	if p.Source == "" {
		return ErrorResult("route " + p.Route + " has no authored source file"), nil
	}
	g, layouts := s.pageGraph(p)
	type owner struct {
		File    string            `json:"file"`
		Chain   []string          `json:"importChain"`
		Reasons []hydrationReason `json:"reasons"`
	}
	var owners []owner
	for _, rel := range g.Order {
		m := g.Modules[rel]
		if m.Tier != "client" || !m.interactive() {
			continue
		}
		var rs []hydrationReason
		for _, r := range m.Reasons {
			if r.Kind != "browser-api" {
				rs = append(rs, r)
			}
		}
		owners = append(owners, owner{File: rel, Chain: g.chain(rel), Reasons: shortReasons(rs, 8)})
	}
	out := map[string]any{"route": p.Route, "source": p.Source, "layouts": layouts, "hydratesComponents": len(owners) > 0, "owners": owners}

	var scripts []scriptInfo
	if data, _, rerr := s.readBuilt(p.Route, 8<<20); rerr == nil {
		info := parseHTMLInfo(data)
		scripts = info.Scripts
		out["builtScripts"] = scripts
		out["shipsJavaScript"] = len(scripts) > 0
	}
	var summary string
	switch {
	case len(owners) > 0:
		summary = fmt.Sprintf("%d client module(s) hydrate on this page because they use hooks/signals, event handlers or a client:* directive.", len(owners))
	case len(scripts) > 0:
		summary = "No component on this page needs hydration. The scripts it loads are shared runtime features (router, prefetch, dev tools), not component code."
	default:
		summary = "This page is plain HTML: nothing on it needs the browser."
	}
	out["summary"] = summary

	var tips []string
	for _, o := range owners {
		onlyHandlers := true
		for _, r := range o.Reasons {
			if r.Kind != "event-handler" {
				onlyHandlers = false
			}
		}
		if o.File == p.Source {
			tips = append(tips, fmt.Sprintf("%s is the page itself: move the interactive part into a small client component so the rest of the page stays static HTML.", o.File))
		} else if g.Modules[o.File] != nil {
			hasDirective := false
			for _, r := range o.Reasons {
				if r.Kind == "island-directive" {
					hasDirective = true
				}
			}
			if !hasDirective {
				tips = append(tips, fmt.Sprintf("Render %s with client:visible or client:idle to defer its hydration until it is needed.", o.File))
			}
		}
		if onlyHandlers && len(o.Reasons) == 1 {
			tips = append(tips, fmt.Sprintf("%s hydrates only for %s; if it is a plain link or form, a native element needs no JavaScript.", o.File, o.Reasons[0].Name))
		}
	}
	out["suggestions"] = tips
	return JSONResult(out)
}

func (s *Service) toolListIslands(ctx context.Context, args map[string]any) (ToolResult, *rpcError) {
	routes, err := s.builder.RouteList()
	if err != nil {
		return ErrorResult(err.Error()), nil
	}
	only := argString(args, "route")
	type agg struct {
		Type      string            `json:"type"`
		Name      string            `json:"name"`
		File      string            `json:"file,omitempty"`
		Directive string            `json:"directive,omitempty"`
		Reasons   []hydrationReason `json:"reasons,omitempty"`
		Routes    []string          `json:"routes"`
	}
	byKey := map[string]*agg{}
	var order []string
	scanned := 0
	for _, r := range routes {
		if r.Source == "" || (only != "" && r.Route != routetypes.Normalize(only)) {
			continue
		}
		scanned++
		g, _ := s.pageGraph(pageRef{Route: r.Route, Source: r.Source, Mode: r.Mode})
		for _, is := range s.islandsOf(g) {
			if is.Type == "page" {
				is.Type = "client-component"
			}
			key := is.Type + ":" + is.Name + ":" + is.File
			a, ok := byKey[key]
			if !ok {
				a = &agg{Type: is.Type, Name: is.Name, File: is.File, Directive: is.Directive, Reasons: is.Reasons}
				byKey[key] = a
				order = append(order, key)
			}
			a.Routes = append(a.Routes, r.Route)
		}
	}
	items := make([]*agg, 0, len(order))
	for _, k := range order {
		items = append(items, byKey[k])
	}
	sort.SliceStable(items, func(i, j int) bool { return len(items[i].Routes) > len(items[j].Routes) })
	page, next, rerr := paginate(items, args, 50, 200)
	if rerr != nil {
		return ToolResult{}, rerr
	}
	return JSONResult(map[string]any{"routesScanned": scanned, "total": len(items), "islands": page, "nextCursor": next,
		"note": "static source analysis: client components with hooks, event handlers or client:* directives, and npm packages imported by client modules"})
}

func (s *Service) toolWhyInBundle(ctx context.Context, args map[string]any) (ToolResult, *rpcError) {
	module, rerr := requireString(args, "module")
	if rerr != nil {
		return ToolResult{}, rerr
	}
	module = strings.TrimSpace(module)
	isFile := false
	if abs, rel, err := s.resolveProjectPath(strings.TrimPrefix(module, "./")); err == nil {
		if info, err := os.Stat(abs); err == nil && !info.IsDir() {
			isFile, module = true, rel
		}
	}
	routes, err := s.builder.RouteList()
	if err != nil {
		return ErrorResult(err.Error()), nil
	}
	only := argString(args, "route")
	type hit struct {
		Route     string   `json:"route"`
		Chain     []string `json:"importChain"`
		Tiers     []string `json:"tiers"`
		BuildTime bool     `json:"buildTimeOnly"`
		Via       string   `json:"via,omitempty"`
	}
	var hits []hit
	scanned := 0
	for _, r := range routes {
		if r.Source == "" || (only != "" && r.Route != routetypes.Normalize(only)) {
			continue
		}
		scanned++
		g, _ := s.pageGraph(pageRef{Route: r.Route, Source: r.Source})
		target := ""
		via := ""
		if isFile {
			if _, ok := g.Modules[module]; ok {
				target = module
			}
		} else if imps := g.Packages[packageName(module)]; len(imps) > 0 {
			target, via = imps[0], "imports "+packageName(module)
		}
		if target == "" {
			continue
		}
		chain := g.chain(target)
		h := hit{Route: r.Route, Chain: chain, Via: via}
		for i, c := range chain {
			t := g.Modules[c].Tier
			h.Tiers = append(h.Tiers, t)
			if i > 0 && (t == "server" || t == "static" || t == "runtime") {
				h.BuildTime = true
			}
		}
		hits = append(hits, h)
	}
	if len(hits) == 0 {
		kind := "package"
		if isFile {
			kind = "file"
		}
		return JSONResult(map[string]any{"module": module, "kind": kind, "routesScanned": scanned, "reachable": false,
			"note": "not reachable from any page or layout through static imports; it may be unused, or imported dynamically"})
	}
	page, next, rerr2 := paginate(hits, args, 25, 200)
	if rerr2 != nil {
		return ToolResult{}, rerr2
	}
	return JSONResult(map[string]any{"module": module, "reachable": true, "routesScanned": scanned, "total": len(hits), "routes": page, "nextCursor": next,
		"note": "import chains come from static source analysis. buildTimeOnly means a server/static/runtime component sits on the chain, so the module runs at build or request time and is not part of the browser bundle."})
}

func (s *Service) toolGetPageMetadata(ctx context.Context, args map[string]any) (ToolResult, *rpcError) {
	p, err := s.findPage(s.pageTarget(args))
	if err != nil {
		return ErrorResult(err.Error()), nil
	}
	out := map[string]any{"route": p.Route, "source": p.Source, "mode": p.Mode, "params": p.Params}
	if gr, err := s.builder.SiteGraph(); err == nil {
		for _, n := range gr.Routes {
			if n.Route == p.Route {
				out["segmentPages"] = map[string]any{"notFound": n.NotFound, "error": n.Error, "forbidden": n.Forbidden, "unauthorized": n.Unauthorized}
				out["staticOnly"] = n.StaticOnly
			}
		}
	}
	if p.Source != "" {
		out["layouts"] = s.layoutChain(p.Source)
		if m, err := s.analyzeModule(p.Source); err == nil {
			var meta []string
			for _, e := range m.Exports {
				switch e {
				case "metadata", "generateMetadata", "revalidate", "dynamic", "dynamicParams", "generateStaticParams":
					meta = append(meta, e)
				}
			}
			out["pageExports"] = meta
			out["tier"] = m.Tier
		}
	}
	data, rel, rerr := s.readBuilt(p.Route, 8<<20)
	if rerr != nil {
		out["built"] = false
		out["hint"] = rerr.Error()
		return JSONResult(out)
	}
	info := parseHTMLInfo(data)
	out["built"] = true
	out["html"] = rel
	out["head"] = map[string]any{
		"lang": info.Lang, "title": info.Title, "canonical": info.Canonical, "meta": info.Meta,
		"jsonLdTypes": emptyIfNil(info.JSONLD), "links": info.Links, "stylesheets": info.Stylesheets, "scripts": info.Scripts,
	}
	var missing []string
	if info.Title == "" {
		missing = append(missing, "title")
	}
	if info.Meta["description"] == "" {
		missing = append(missing, "meta description")
	}
	if info.Canonical == "" {
		missing = append(missing, "canonical link")
	}
	if info.Meta["og:title"] == "" {
		missing = append(missing, "og:title")
	}
	out["missing"] = missing
	return JSONResult(out)
}

func (s *Service) toolGetRenderedHTML(ctx context.Context, args map[string]any) (ToolResult, *rpcError) {
	p, err := s.findPage(s.pageTarget(args))
	if err != nil {
		return ErrorResult(err.Error()), nil
	}
	data, rel, rerr := s.readBuilt(p.Route, 16<<20)
	if rerr != nil {
		return ErrorResult(rerr.Error()), nil
	}
	doc := string(data)
	part := argString(args, "part")
	switch part {
	case "", "full":
	case "head":
		doc, _ = splitHTML(doc)
	case "body":
		_, doc = splitHTML(doc)
	default:
		return ErrorResult("part must be one of: full, head, body"), nil
	}
	if argString(args, "format") == "text" {
		doc = parseHTMLInfo(data).Text
	}
	offset := argInt(args, "offset", 0)
	if offset < 0 || offset > len(doc) {
		offset = len(doc)
	}
	max := clampInt(args, "maxBytes", 64<<10, 512<<10)
	chunk, cut := truncateText(doc[offset:], max)
	next := ""
	if cut {
		next = fmt.Sprint(offset + len(chunk))
	}
	return JSONResult(map[string]any{
		"route": p.Route, "file": rel, "totalBytes": len(doc), "offset": offset, "returnedBytes": len(chunk),
		"nextOffset": next, "html": chunk,
	})
}

func (s *Service) toolRenderPreview(ctx context.Context, args map[string]any) (ToolResult, *rpcError) {
	p, err := s.findPage(s.pageTarget(args))
	if err != nil {
		return ErrorResult(err.Error()), nil
	}
	if argBool(args, "rebuild", false) {
		if out, err := s.runBuild(ctx); err != nil {
			return ErrorResult(fmt.Sprintf("build failed: %v\n\n%s", err, out)), nil
		}
	}
	data, rel, rerr := s.readBuilt(p.Route, 16<<20)
	if rerr != nil {
		return ErrorResult(rerr.Error() + " (or pass rebuild: true)"), nil
	}
	info := parseHTMLInfo(data)
	limit := clampInt(args, "limit", 30, 200)
	var anchors []anchorInfo
	internal, external := 0, 0
	for _, a := range info.Anchors {
		if strings.Contains(a.Href, "://") || strings.HasPrefix(a.Href, "//") {
			external++
		} else {
			internal++
		}
		if len(anchors) < limit {
			anchors = append(anchors, a)
		}
	}
	missingAlt := 0
	for _, im := range info.Images {
		if !im.HasAlt {
			missingAlt++
		}
	}
	imgs := info.Images
	if len(imgs) > limit {
		imgs = imgs[:limit]
	}
	heads := info.Headings
	if len(heads) > limit {
		heads = heads[:limit]
	}
	text, cut := truncateText(info.Text, clampInt(args, "textChars", 1500, 20000))
	return JSONResult(map[string]any{
		"route": p.Route, "file": rel, "lang": info.Lang, "title": info.Title, "description": info.Meta["description"],
		"landmarks": info.Landmarks, "headings": heads,
		"links":  map[string]any{"internal": internal, "external": external, "sample": anchors},
		"images": map[string]any{"count": len(info.Images), "missingAlt": missingAlt, "sample": emptyIfNil(imgs)},
		"forms":  emptyIfNil(info.Forms), "buttons": info.Buttons,
		"scripts": len(info.Scripts), "stylesheets": len(info.Stylesheets),
		"words": info.TextWords, "text": text, "textTruncated": cut,
		"note": "a structural text rendering of the built HTML (no layout or paint); use page_check for a real browser",
	})
}

func (s *Service) registerPageTools(srv *Server) {
	routeSchema := func(desc string) map[string]any {
		return map[string]any{
			"route":  strSchema(desc),
			"source": strSchema("Page source path (alternative to route)"),
		}
	}
	s.reg(srv, Tool{
		Name: "explain_page",
		Description: "Explain how a page is built and shipped: render mode and why, its layout chain, the component tier of every module it imports (and which rule decided it), which parts hydrate in the browser (client components, client:* islands, npm package islands), what stays build-time or request-time only, and the exact JS/CSS bytes (raw and gzip) the built page loads. " +
			"Start here for any 'why is this page heavy / dynamic / interactive' question. Source analysis needs no build; sizes need a build.",
		InputSchema: objSchema(routeSchema("Route, e.g. /blog/[slug] or a concrete path like /blog/hello")),
		Annotations: readOnlyAnnotations("Explain Page"),
		Handler:     s.toolExplainPage,
	})
	s.reg(srv, Tool{
		Name:        "why_hydrated",
		Description: "Answer 'why does this page ship JavaScript?' with file:line evidence: every client module that hydrates (hooks and signals, DOM event handlers, client:* directives), the import chain from the page to it, and concrete suggestions (client:visible, splitting the page, native elements). Also tells apart component hydration from shared runtime scripts.",
		InputSchema: objSchema(routeSchema("Route or concrete path")),
		Annotations: readOnlyAnnotations("Why Hydrated"),
		Handler:     s.toolWhyHydrated,
	})
	s.reg(srv, Tool{
		Name:        "why_in_bundle",
		Description: "Trace why a file or npm package is part of a page: the import chain from each route that reaches it, the tier of every hop, and whether a server/static/runtime component on the chain keeps it out of the browser bundle. Use it to find who pulls in a heavy dependency.",
		InputSchema: objSchema(map[string]any{
			"module": strSchema("Project-relative file (src/components/Chart.tsx) or npm package name (lodash, @scope/pkg)"),
			"route":  strSchema("Limit to one route (default: every route)"),
			"limit":  numSchema("Routes per page (default 25, max 200)"),
			"cursor": strSchema("Opaque cursor from a previous result"),
		}, "module"),
		Annotations: readOnlyAnnotations("Why In Bundle"),
		Handler:     s.toolWhyInBundle,
	})
	s.reg(srv, Tool{
		Name:        "list_islands",
		Description: "List the interactive islands of the site: client components with hooks/handlers/client:* directives and npm packages imported by client code, each with the routes that use them, ranked by how many routes pay for them. Use it to find hydration worth removing or deferring.",
		InputSchema: objSchema(map[string]any{
			"route":  strSchema("Limit to one route"),
			"limit":  numSchema("Islands per page (default 50, max 200)"),
			"cursor": strSchema("Opaque cursor from a previous result"),
		}),
		Annotations: readOnlyAnnotations("List Islands"),
		Handler:     s.toolListIslands,
	})
	s.reg(srv, Tool{
		Name:        "get_page_metadata",
		Description: "What contributes to a page's render and head: route, source, mode, layout chain, not-found/error segment pages, the metadata exports, and the built <head> (title, description, canonical, Open Graph, JSON-LD types, preloads, scripts, stylesheets) with a list of missing SEO basics. Equivalent of Next.js get_page_metadata, read from the compiled output so no browser is needed.",
		InputSchema: objSchema(routeSchema("Route or concrete path")),
		Annotations: readOnlyAnnotations("Get Page Metadata"),
		Handler:     s.toolGetPageMetadata,
	})
	s.reg(srv, Tool{
		Name:        "get_rendered_html",
		Description: "Return the built HTML document of a route, whole or only its head or body, as markup or visible text, in byte-bounded chunks (use nextOffset to continue). Reads the build output; run build first if the route is not built.",
		InputSchema: objSchema(map[string]any{
			"route":    strSchema("Route or concrete path"),
			"source":   strSchema("Page source path (alternative to route)"),
			"part":     enumSchema("Which part (default full)", "full", "head", "body"),
			"format":   enumSchema("html markup or visible text (default html)", "html", "text"),
			"maxBytes": numSchema("Chunk size (default 65536, max 524288)"),
			"offset":   numSchema("Byte offset to continue from"),
		}),
		Annotations: readOnlyAnnotations("Get Rendered HTML"),
		Handler:     s.toolGetRenderedHTML,
	})
	s.reg(srv, Tool{
		Name:        "render_preview",
		Description: "A structural preview of a built page without a browser: heading outline, landmarks, links (internal/external), images and missing alt text, forms and fields, script/style counts and the opening text. Cheap way to check what a page says and how it is organised; pass rebuild: true to build first.",
		InputSchema: objSchema(map[string]any{
			"route":     strSchema("Route or concrete path"),
			"source":    strSchema("Page source path (alternative to route)"),
			"rebuild":   boolSchema("Build the site first (default false)"),
			"limit":     numSchema("Max headings/links/images listed (default 30, max 200)"),
			"textChars": numSchema("Visible-text characters to include (default 1500, max 20000)"),
		}),
		Annotations: rebuildToolAnnotations("Render Preview"),
		Handler:     s.toolRenderPreview,
	})
}
