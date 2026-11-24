package build

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// RouteSize is the weight of one built page: its document and the scripts and
// stylesheets it loads, raw and gzipped. JS counts every script the page loads,
// including the chunks those scripts import (see transitiveScripts); OwnJS is
// only the scripts the HTML names directly.
type RouteSize struct {
	Route      string `json:"route"`
	Mode       string `json:"mode"`
	HTML       int64  `json:"html"`
	JS         int64  `json:"js"`
	CSS        int64  `json:"css"`
	GzipHTML   int64  `json:"gzipHtml"`
	GzipJS     int64  `json:"gzipJs"`
	GzipCSS    int64  `json:"gzipCss"`
	OwnJS      int64  `json:"ownJs,omitempty"`
	GzipOwnJS  int64  `json:"gzipOwnJs,omitempty"`
	ThirdParty bool   `json:"thirdParty,omitempty"` // loads mermaid or KaTeX from the CDN on demand
}

// Gzip is the compressed size of everything the route loads.
func (r RouteSize) Gzip() int64 { return r.GzipHTML + r.GzipJS + r.GzipCSS }

// Raw is the uncompressed size of everything the route loads.
func (r RouteSize) Raw() int64 { return r.HTML + r.JS + r.CSS }

// BuildSummary is the per-route size report of one build.
type BuildSummary struct {
	Routes []RouteSize `json:"routes"`
	// Unique is the gzipped size of every distinct file the pages load, so a
	// script shared by all pages counts once.
	UniqueGzip int64     `json:"uniqueGzip"`
	Time       time.Time `json:"time"`
}

var assetRefRe = regexp.MustCompile(`(?is)<(?:script|link)\b[^>]*?>`)
var attrRe = regexp.MustCompile(`(?is)\b(src|href|rel)\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s>]+))`)

// pageAssets lists the local script and stylesheet URLs a page's HTML loads.
func pageAssets(html string) (scripts, styles []string) {
	seen := map[string]bool{}
	for _, tag := range assetRefRe.FindAllString(html, -1) {
		attrs := map[string]string{}
		for _, m := range attrRe.FindAllStringSubmatch(tag, -1) {
			v := m[2]
			if v == "" {
				v = m[3]
			}
			if v == "" {
				v = m[4]
			}
			attrs[strings.ToLower(m[1])] = v
		}
		isScript := strings.HasPrefix(strings.ToLower(tag), "<script")
		ref := attrs["href"]
		if isScript {
			ref = attrs["src"]
		}
		if ref == "" || seen[ref] {
			continue
		}
		rel := strings.ToLower(attrs["rel"])
		switch {
		case isScript:
			scripts = append(scripts, ref)
		case strings.Contains(rel, "stylesheet"):
			styles = append(styles, ref)
		case strings.Contains(rel, "modulepreload"):
			scripts = append(scripts, ref)
		default:
			continue
		}
		seen[ref] = true
	}
	return scripts, styles
}

var importMapRe = regexp.MustCompile(`(?is)<script\b[^>]*type\s*=\s*["']?importmap["']?[^>]*>(.*?)</script>`)

// pageImportMap returns the "imports" table of the page's import map.
func pageImportMap(html string) map[string]string {
	m := importMapRe.FindStringSubmatch(html)
	if m == nil {
		return nil
	}
	var doc struct {
		Imports map[string]string `json:"imports"`
	}
	if json.Unmarshal([]byte(m[1]), &doc) != nil {
		return nil
	}
	return doc.Imports
}

var (
	staticImportRe  = regexp.MustCompile(`(?:\bfrom|\bimport)\s*["']([^"'\\\n]+)["']`)
	dynamicImportRe = regexp.MustCompile(`\bimport\(\s*["']([^"'\\\n]+)["']\s*\)`)
)

// scriptImports lists the module specifiers a script names: static ones, which
// always load with it, and dynamic import("...") ones.
func scriptImports(src string) (static, dynamic []string) {
	for _, m := range staticImportRe.FindAllStringSubmatch(src, -1) {
		static = append(static, m[1])
	}
	for _, m := range dynamicImportRe.FindAllStringSubmatch(src, -1) {
		dynamic = append(dynamic, m[1])
	}
	return static, dynamic
}

// isIslandEntry reports whether a script URL is an island entry chunk. Pages
// import their islands as they hydrate, so a dynamic import of one is part of
// the load; other dynamic imports (lazy components, route chunks) are not.
func isIslandEntry(url string) bool {
	return strings.HasPrefix(path.Base(url), islandEntryPrefix) && strings.HasSuffix(url, ".js")
}

// transitiveScripts returns the URLs of every script a page loads: the entry
// scripts, the modules they import statically (relative, absolute or through
// the page's import map) and the island chunks they import dynamically. read
// returns a script's source by URL, or false when it is not a local file.
func transitiveScripts(entries []string, imports map[string]string, read func(url string) (string, bool)) []string {
	var order []string
	seen := map[string]bool{}
	queue := append([]string(nil), entries...)
	for len(queue) > 0 {
		url := queue[0]
		queue = queue[1:]
		if i := strings.IndexAny(url, "?#"); i >= 0 {
			url = url[:i]
		}
		if seen[url] {
			continue
		}
		src, ok := read(url)
		if !ok {
			continue
		}
		seen[url] = true
		order = append(order, url)
		resolve := func(spec string) string {
			switch {
			case strings.HasPrefix(spec, "./"), strings.HasPrefix(spec, "../"):
				return path.Join(path.Dir(url), spec)
			case strings.HasPrefix(spec, "/") && !strings.HasPrefix(spec, "//"):
				return spec
			}
			return imports[spec]
		}
		static, dynamic := scriptImports(src)
		for _, spec := range static {
			if u := resolve(spec); u != "" {
				queue = append(queue, u)
			}
		}
		for _, spec := range dynamic {
			if u := resolve(spec); u != "" && isIslandEntry(u) {
				queue = append(queue, u)
			}
		}
	}
	return order
}

func gzipLen(data []byte) int64 {
	var buf bytes.Buffer
	zw, _ := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	_, _ = zw.Write(data)
	_ = zw.Close()
	return int64(buf.Len())
}

// summaryFile caches the raw and gzipped size of a file within one run.
type summaryFile struct{ raw, gz int64 }

// resolveOutURL turns a URL path from a built page into the file under
// outDir it names ("" for an external or relative URL). base is the site base
// path without slashes.
func resolveOutURL(outDir, base, ref string) string {
	if i := strings.IndexAny(ref, "?#"); i >= 0 {
		ref = ref[:i]
	}
	if !strings.HasPrefix(ref, "/") || strings.HasPrefix(ref, "//") {
		return ""
	}
	ref = strings.TrimPrefix(ref, "/")
	if base != "" {
		ref = strings.TrimPrefix(strings.TrimPrefix(ref, base), "/")
	}
	clean := path.Clean("/" + ref)
	return filepath.Join(outDir, filepath.FromSlash(strings.TrimPrefix(clean, "/")))
}

// pageLoad is what a built page loads: the scripts its HTML names, every
// script it loads (those and the chunks they import), and its stylesheets,
// as URLs from the page.
type pageLoad struct {
	OwnScripts []string
	Scripts    []string
	Styles     []string
	ThirdParty bool
}

// pageLoadOf lists what the built page html loads from outDir.
func pageLoadOf(outDir, base, html string) pageLoad {
	scripts, styles := pageAssets(html)
	all := transitiveScripts(scripts, pageImportMap(html), func(url string) (string, bool) {
		abs := resolveOutURL(outDir, base, url)
		if abs == "" {
			return "", false
		}
		data, err := os.ReadFile(abs)
		return string(data), err == nil
	})
	return pageLoad{OwnScripts: scripts, Scripts: all, Styles: styles, ThirdParty: strings.Contains(html, cspCDNOrigin+"/")}
}

// fileMeasurer measures files once each: raw and gzipped size.
type fileMeasurer map[string]summaryFile

func (m fileMeasurer) measure(abs string) (summaryFile, bool) {
	if f, ok := m[abs]; ok {
		return f, true
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return summaryFile{}, false
	}
	f := summaryFile{raw: int64(len(data)), gz: gzipLen(data)}
	m[abs] = f
	return f, true
}

// Summarize measures the built pages. Call it after BuildAll. SSR and ISR
// pages that have no emitted document are listed with zero size.
func (b *Builder) Summarize() (*BuildSummary, error) {
	routes, err := b.RouteList()
	if err != nil {
		return nil, err
	}
	outDir := b.Cfg.OutDir
	base := strings.Trim(b.Cfg.BaseURLPath(), "/")
	cache := fileMeasurer{}

	s := &BuildSummary{Time: time.Now().UTC()}
	unique := map[string]bool{}
	for _, r := range routes {
		if !r.Built {
			continue
		}
		doc := builtDocPath(outDir, r.Route)
		rs := RouteSize{Route: r.Route, Mode: r.Mode}
		if doc != "" {
			htmlBytes, err := os.ReadFile(doc)
			if err != nil {
				continue
			}
			rs.HTML, rs.GzipHTML = int64(len(htmlBytes)), gzipLen(htmlBytes)
			unique[doc] = true
			load := pageLoadOf(outDir, base, string(htmlBytes))
			rs.ThirdParty = load.ThirdParty
			add := func(refs []string, raw, gz *int64) {
				for _, ref := range refs {
					abs := resolveOutURL(outDir, base, ref)
					if abs == "" {
						continue
					}
					if f, ok := cache.measure(abs); ok {
						*raw += f.raw
						*gz += f.gz
						unique[abs] = true
					}
				}
			}
			add(load.OwnScripts, &rs.OwnJS, &rs.GzipOwnJS)
			add(load.Scripts, &rs.JS, &rs.GzipJS)
			add(load.Styles, &rs.CSS, &rs.GzipCSS)
		}
		s.Routes = append(s.Routes, rs)
	}
	for abs := range unique {
		if f, ok := cache[abs]; ok {
			s.UniqueGzip += f.gz
		} else if data, err := os.ReadFile(abs); err == nil {
			s.UniqueGzip += gzipLen(data)
		}
	}
	sort.SliceStable(s.Routes, func(i, j int) bool { return s.Routes[i].Route < s.Routes[j].Route })
	return s, nil
}

// builtDocPath is the built HTML document of a route ("/blog/hello"), ""
// when there is none.
func builtDocPath(outDir, route string) string {
	name := strings.Trim(route, "/")
	for _, cand := range []string{filepath.Join(outDir, filepath.FromSlash(name), "index.html"), filepath.Join(outDir, filepath.FromSlash(name)+".html")} {
		if info, err := os.Stat(cand); err == nil && !info.IsDir() {
			return cand
		}
	}
	return ""
}

// SummaryPath is where the last build's summary is kept for the next diff.
func SummaryPath(root string) string {
	return filepath.Join(root, ".krate", "build-summary.json")
}

// LoadSummary reads the previous build's summary; nil when there is none.
func LoadSummary(root string) *BuildSummary {
	data, err := os.ReadFile(SummaryPath(root))
	if err != nil {
		return nil
	}
	var s BuildSummary
	if json.Unmarshal(data, &s) != nil {
		return nil
	}
	return &s
}

// SaveSummary stores the summary for the next build to compare against.
func SaveSummary(root string, s *BuildSummary) error {
	if err := os.MkdirAll(filepath.Dir(SummaryPath(root)), 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(s)
	if err != nil {
		return err
	}
	return os.WriteFile(SummaryPath(root), data, 0o644)
}

// summaryRowLimit caps the table; the heaviest routes are shown.
const summaryRowLimit = 25

// formatDelta renders a size change: "+1.2 kB", "-300 B", "=" or "".
func formatDelta(d int64) string {
	switch {
	case d == 0:
		return "="
	case d > 0:
		return "+" + FormatBytes(d)
	}
	return FormatBytes(d)
}

// FormatSummary renders the per-route table. prev, when given, adds a column
// with each route's change in gzipped size since the last build.
func FormatSummary(s *BuildSummary, prev *BuildSummary) string {
	if len(s.Routes) == 0 {
		return ""
	}
	before := map[string]int64{}
	if prev != nil {
		for _, r := range prev.Routes {
			before[r.Route] = r.Gzip()
		}
	}
	rows := append([]RouteSize(nil), s.Routes...)
	hidden := 0
	if len(rows) > summaryRowLimit {
		sort.SliceStable(rows, func(i, j int) bool { return rows[i].Gzip() > rows[j].Gzip() })
		hidden = len(rows) - summaryRowLimit
		rows = rows[:summaryRowLimit]
		sort.SliceStable(rows, func(i, j int) bool { return rows[i].Route < rows[j].Route })
	}
	width := len("Route")
	for _, r := range rows {
		if n := len(r.Route); n > width {
			width = n
		}
	}
	if width > 48 {
		width = 48
	}
	var b strings.Builder
	header := fmt.Sprintf("  %-*s  %-6s  %9s  %9s  %9s  %10s", width, "Route", "Mode", "HTML", "JS", "CSS", "Total gzip")
	if prev != nil {
		header += "  vs last"
	}
	fmt.Fprintf(&b, "%s%s%s\n", cBold, header, cReset)
	for _, r := range rows {
		route := r.Route
		if len(route) > width {
			route = route[:width-3] + "..."
		}
		line := fmt.Sprintf("  %-*s  %-6s  %9s  %9s  %9s  %10s", width, route, r.Mode, FormatBytes(r.GzipHTML), FormatBytes(r.GzipJS), FormatBytes(r.GzipCSS), FormatBytes(r.Gzip()))
		if prev != nil {
			if was, ok := before[r.Route]; ok {
				line += "  " + colorDelta(r.Gzip()-was)
			} else {
				line += "  " + cCyan + "new" + cReset
			}
		}
		fmt.Fprintln(&b, line)
	}
	if hidden > 0 {
		fmt.Fprintf(&b, "  %s... and %d more route(s); the %d heaviest are shown%s\n", cGray, hidden, summaryRowLimit, cReset)
	}
	fmt.Fprintf(&b, "  %sgzip sizes of the page and the scripts and styles it loads, including the chunks its scripts import; %s shipped in total (shared files counted once)%s\n", cGray, FormatBytes(s.UniqueGzip), cReset)
	thirdParty := 0
	for _, r := range s.Routes {
		if r.ThirdParty {
			thirdParty++
		}
	}
	if thirdParty > 0 {
		fmt.Fprintf(&b, "  %s%d route(s) load mermaid or KaTeX from a CDN when a diagram or formula is shown; those bytes are not counted%s\n", cGray, thirdParty, cReset)
	}
	if prev != nil {
		now := map[string]bool{}
		for _, r := range s.Routes {
			now[r.Route] = true
		}
		var gone []string
		for _, r := range prev.Routes {
			if !now[r.Route] {
				gone = append(gone, r.Route)
			}
		}
		if len(gone) > 0 {
			sort.Strings(gone)
			if len(gone) > 5 {
				gone = append(gone[:5], fmt.Sprintf("and %d more", len(gone)-5))
			}
			fmt.Fprintf(&b, "  %sremoved since last build: %s%s\n", cGray, strings.Join(gone, ", "), cReset)
		}
		fmt.Fprintf(&b, "  %stotal shipped %s (last build %s)%s\n", cGray, formatDelta(s.UniqueGzip-prev.UniqueGzip), FormatBytes(prev.UniqueGzip), cReset)
	}
	return b.String()
}

func colorDelta(d int64) string {
	switch {
	case d == 0:
		return cGray + "=" + cReset
	case d > 0:
		return cYellow + formatDelta(d) + cReset
	}
	return cGreen + formatDelta(d) + cReset
}
