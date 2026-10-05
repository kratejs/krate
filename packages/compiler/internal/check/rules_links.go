package check

import (
	"fmt"
	stdhtml "html"
	"path"
	"strings"

	"golang.org/x/net/html"
)

// ─── broken links & anchors ─────────────────────────────────────────────────

// ruleBrokenLink flags internal links whose destination is not a known route.
// Requires cfg.Routes (the site route inventory), so it is a no-op when the
// runner has not supplied one.
func ruleBrokenLink(p *Page, cfg *Config) []Finding {
	if len(cfg.Routes) == 0 {
		return nil
	}
	d, err := parseHTML(p.HTML)
	if err != nil {
		return nil
	}
	var out []Finding
	seen := map[string]bool{}
	for _, a := range d.elements("a") {
		href, _ := attr(a, "href")
		href = strings.TrimSpace(href)
		if href == "" || seen[href] || !isInternalPageLink(href) {
			continue
		}
		seen[href] = true
		target := href
		if i := strings.IndexAny(target, "#?"); i >= 0 {
			target = target[:i]
		}
		if target == "" {
			continue // pure fragment (checked by a11y/broken-anchor)
		}
		resolved := resolveHref(p.Route, target)
		if resolved == "" || strings.Contains(resolved, "[") || isNonPageRoute(resolved) {
			continue
		}
		if routeKnown(cfg.Routes, resolved) {
			continue
		}
		out = append(out, Finding{
			Message: "broken internal link: " + href,
			Hint:    "Point the link at an existing route, or add the page/redirect.",
		})
	}
	return out
}

// ruleBrokenAnchor flags in-page fragment links (`#id`) with no matching id.
func ruleBrokenAnchor(p *Page, _ *Config) []Finding {
	d, err := parseHTML(p.HTML)
	if err != nil {
		return nil
	}
	ids := map[string]bool{}
	d.walk(func(n *html.Node) {
		if v, ok := attr(n, "id"); ok && v != "" {
			ids[v] = true
		}
		if v, ok := attr(n, "name"); ok && v != "" {
			ids[v] = true
		}
	})
	var out []Finding
	seen := map[string]bool{}
	for _, a := range d.elements("a") {
		href, _ := attr(a, "href")
		href = strings.TrimSpace(href)
		if !strings.HasPrefix(href, "#") || len(href) < 2 {
			continue
		}
		frag := href[1:]
		if seen[frag] {
			continue
		}
		seen[frag] = true
		if !ids[stdhtml.UnescapeString(frag)] {
			out = append(out, Finding{
				Message: "broken in-page anchor: " + href,
				Hint:    "No element with that id exists on the page.",
			})
		}
	}
	return out
}

// ruleOGImage requires an absolute og:image.
func ruleOGImage(p *Page, _ *Config) []Finding {
	d, err := parseHTML(p.HTML)
	if err != nil {
		return nil
	}
	img, ok := d.metaByProperty("og:image")
	if !ok || strings.TrimSpace(img) == "" {
		return []Finding{{
			Message: "missing og:image",
			Hint:    "Set seo.image (absolute URL) or add <meta property=\"og:image\">.",
		}}
	}
	if !strings.HasPrefix(img, "http://") && !strings.HasPrefix(img, "https://") {
		return []Finding{{
			Message: "og:image is not an absolute URL: " + img,
			Hint:    "Use an absolute URL (https://…) so crawlers can fetch it.",
		}}
	}
	return nil
}

// ruleDuplicateMeta is a per-page no-op; duplicate detection is cross-page and
// handled in Run via duplicateMetaFindings.
func ruleDuplicateMeta(_ *Page, _ *Config) []Finding { return nil }

// duplicateMetaFindings reports titles/descriptions shared by multiple routes.
func duplicateMetaFindings(pages []Page) []Finding {
	type meta struct {
		route string
		title string
		desc  string
	}
	metas := make([]meta, 0, len(pages))
	for i := range pages {
		d, err := parseHTML(pages[i].HTML)
		if err != nil {
			continue
		}
		m := meta{route: pages[i].Route}
		if m.route == "" {
			m.route = "/"
		}
		if t := d.firstElement("title"); t != nil {
			m.title = textContent(t)
		}
		if desc, ok := d.metaByName("description"); ok {
			m.desc = strings.TrimSpace(desc)
		}
		metas = append(metas, m)
	}

	var out []Finding
	titleRoutes := map[string][]string{}
	descRoutes := map[string][]string{}
	for _, m := range metas {
		if m.title != "" {
			titleRoutes[m.title] = append(titleRoutes[m.title], m.route)
		}
		if m.desc != "" {
			descRoutes[m.desc] = append(descRoutes[m.desc], m.route)
		}
	}
	for title, routes := range titleRoutes {
		if len(routes) > 1 {
			out = append(out, Finding{
				Route:   routes[0],
				Message: fmt.Sprintf("duplicate <title> shared by %d pages: %q", len(routes), title),
				Hint:    "Give each page a unique title (routes: " + strings.Join(routes, ", ") + ").",
			})
		}
	}
	for desc, routes := range descRoutes {
		if len(routes) > 1 {
			out = append(out, Finding{
				Route:   routes[0],
				Message: fmt.Sprintf("duplicate meta description shared by %d pages", len(routes)),
				Hint:    "Give each page a unique description (routes: " + strings.Join(routes, ", ") + "). Shared: " + truncate(desc, 60),
			})
		}
	}
	return out
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

// ─── helpers ───────────────────────────────────────────────────────────────

func isInternalPageLink(href string) bool {
	lower := strings.ToLower(href)
	if strings.HasPrefix(href, "//") {
		return false
	}
	for _, scheme := range []string{"http://", "https://", "mailto:", "tel:", "javascript:", "data:", "krate:"} {
		if strings.HasPrefix(lower, scheme) {
			return false
		}
	}
	// Links with a file extension are assets, not routes.
	bare := href
	if i := strings.IndexAny(bare, "#?"); i >= 0 {
		bare = bare[:i]
	}
	if path.Ext(bare) != "" {
		return false
	}
	return true
}

// resolveHref resolves a link target against the page URL. Relative links are
// resolved against the page's DIRECTORY (a route like /docs/guide is a page,
// so "other" resolves to /docs/other, not /docs/guide/other).
func resolveHref(pageRoute, target string) string {
	if strings.HasPrefix(target, "/") {
		return path.Clean(target)
	}
	base := path.Dir(pageRoute)
	if base == "." || pageRoute == "" {
		base = "/"
	}
	return path.Clean(path.Join(base, target))
}

// isNonPageRoute reports whether a resolved path belongs to a namespace that
// never appears in the page route inventory (API routes, dev endpoints), so the
// broken-link rule does not flag it.
func isNonPageRoute(resolved string) bool {
	return resolved == "/api" || strings.HasPrefix(resolved, "/api/") ||
		strings.HasPrefix(resolved, "/__krate/")
}

func routeKnown(routes map[string]bool, resolved string) bool {
	candidates := []string{
		resolved,
		strings.TrimSuffix(resolved, "/"),
		strings.TrimSuffix(resolved, "/") + "/",
	}
	for _, c := range candidates {
		if c == "" {
			continue
		}
		if routes[c] {
			return true
		}
	}
	return false
}
