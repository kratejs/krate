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
// stylesheets it loads, raw and gzipped.
type RouteSize struct {
	Route    string `json:"route"`
	Mode     string `json:"mode"`
	HTML     int64  `json:"html"`
	JS       int64  `json:"js"`
	CSS      int64  `json:"css"`
	GzipHTML int64  `json:"gzipHtml"`
	GzipJS   int64  `json:"gzipJs"`
	GzipCSS  int64  `json:"gzipCss"`
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

func gzipLen(data []byte) int64 {
	var buf bytes.Buffer
	zw, _ := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	_, _ = zw.Write(data)
	_ = zw.Close()
	return int64(buf.Len())
}

// summaryFile caches the raw and gzipped size of a file within one run.
type summaryFile struct{ raw, gz int64 }

// Summarize measures the built pages. Call it after BuildAll. SSR and ISR
// pages that have no emitted document are listed with zero size.
func (b *Builder) Summarize() (*BuildSummary, error) {
	routes, err := b.RouteList()
	if err != nil {
		return nil, err
	}
	outDir := b.Cfg.OutDir
	base := strings.Trim(b.Cfg.BaseURLPath(), "/")
	cache := map[string]summaryFile{}
	measure := func(abs string) (summaryFile, bool) {
		if f, ok := cache[abs]; ok {
			return f, true
		}
		data, err := os.ReadFile(abs)
		if err != nil {
			return summaryFile{}, false
		}
		f := summaryFile{raw: int64(len(data)), gz: gzipLen(data)}
		cache[abs] = f
		return f, true
	}
	// resolve turns a URL path from the HTML into a file under the output.
	resolve := func(ref string) string {
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

	s := &BuildSummary{Time: time.Now().UTC()}
	unique := map[string]bool{}
	for _, r := range routes {
		if !r.Built {
			continue
		}
		name := strings.Trim(r.Route, "/")
		var doc string
		for _, cand := range []string{filepath.Join(outDir, filepath.FromSlash(name), "index.html"), filepath.Join(outDir, filepath.FromSlash(name)+".html")} {
			if _, err := os.Stat(cand); err == nil {
				doc = cand
				break
			}
		}
		rs := RouteSize{Route: r.Route, Mode: r.Mode}
		if doc != "" {
			htmlBytes, err := os.ReadFile(doc)
			if err != nil {
				continue
			}
			rs.HTML, rs.GzipHTML = int64(len(htmlBytes)), gzipLen(htmlBytes)
			unique[doc] = true
			scripts, styles := pageAssets(string(htmlBytes))
			add := func(refs []string, raw, gz *int64) {
				for _, ref := range refs {
					abs := resolve(ref)
					if abs == "" {
						continue
					}
					if f, ok := measure(abs); ok {
						*raw += f.raw
						*gz += f.gz
						unique[abs] = true
					}
				}
			}
			add(scripts, &rs.JS, &rs.GzipJS)
			add(styles, &rs.CSS, &rs.GzipCSS)
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
	fmt.Fprintf(&b, "  %sgzip sizes of the page and the scripts and styles it loads; %s shipped in total (shared files counted once)%s\n", cGray, FormatBytes(s.UniqueGzip), cReset)
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
