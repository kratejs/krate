package docs

import (
	"sort"
	"strings"
)

// Breadcrumb represents a single breadcrumb item.
type Breadcrumb struct {
	Label  string `json:"label"`  // display label
	URL    string `json:"url"`    // URL (empty for current page)
	IsLast bool   `json:"isLast"` // true if this is the current page
}

// SortPages sorts pages by directory, then order, then title. A per-item
// `sidebar: {order}` override wins over the top-level `order:` frontmatter.
func SortPages(pages []Page) {
	sort.Slice(pages, func(i, j int) bool {
		if pages[i].Dir != pages[j].Dir {
			return pages[i].Dir < pages[j].Dir
		}
		oi, oj := effectiveOrder(pages[i]), effectiveOrder(pages[j])
		if oi != oj {
			return oi < oj
		}
		return pages[i].Title < pages[j].Title
	})
}

func effectiveOrder(p Page) int {
	if p.SidebarCfg != nil && p.SidebarCfg.Order > 0 {
		return p.SidebarCfg.Order
	}
	return p.Order
}

// NormalizePagePath strips trailing "/index" from a page path for URL generation.
// "index" -> ""
// "guides/index" -> "guides"
// "guides/advanced" -> "guides/advanced"
func NormalizePagePath(path string) string {
	if path == "index" || path == "" {
		return ""
	}
	return strings.TrimSuffix(path, "/index")
}

// DocsBasePath is the URL prefix the docs plugin mounts pages under. It is the
// single source of truth for absolute docs URLs (sidebar, prev/next links, and
// breadcrumbs) so they agree.
const DocsBasePath = "/docs"

// PageURL returns the full URL for a docs page path.
func PageURL(path string) string {
	normalized := NormalizePagePath(path)
	if normalized == "" {
		return DocsBasePath + "/"
	}
	return DocsBasePath + "/" + normalized + "/"
}

// ApplyTrailingSlash puts a docs URL in the site's trailing-slash form. Only
// "never" changes it: the root "/" is kept and a query or fragment is left
// alone. "always" and "ignore" keep the generated form (a trailing slash).
func ApplyTrailingSlash(u, mode string) string {
	if mode != "never" || u == "" {
		return u
	}
	if i := strings.IndexAny(u, "?#"); i >= 0 {
		return ApplyTrailingSlash(u[:i], mode) + u[i:]
	}
	if t := strings.TrimRight(u, "/"); t != "" {
		return t
	}
	return u
}

// sameURL reports whether two page URLs are the same page, ignoring a
// trailing slash.
func sameURL(a, b string) bool {
	return strings.TrimRight(a, "/") == strings.TrimRight(b, "/")
}

// SidebarOrderedPages returns the pages in the order a reader walks the
// sidebar (depth-first, a section's index page first), for prev/next links.
// Pages the sidebar does not list (hidden pages, or any page under a file
// sidebar that omits it) are left out. ok is false when the sidebar lists no
// page at all, so the caller can fall back to the plain page order.
func SidebarOrderedPages(pages []Page, sections []SidebarItem) (ordered []Page, ok bool) {
	byURL := make(map[string]Page, len(pages))
	for _, p := range pages {
		byURL[strings.TrimRight(pagePathURL(p), "/")] = p
	}
	seen := make(map[string]bool, len(pages))
	add := func(u string) {
		k := strings.TrimRight(u, "/")
		if u == "" || seen[k] {
			return
		}
		if p, found := byURL[k]; found {
			seen[k] = true
			ordered = append(ordered, p)
		}
	}
	var walk func(items []SidebarItem)
	walk = func(items []SidebarItem) {
		for _, it := range items {
			add(it.IndexURL)
			add(it.URL)
			walk(it.Children)
		}
	}
	walk(sections)
	return ordered, len(ordered) > 0
}

// pagePathURL returns a page's URL, preferring the plugin-computed (locale- and
// version-aware) URL when present.
func pagePathURL(p Page) string {
	if p.URL != "" {
		return p.URL
	}
	return PageURL(p.Path)
}

// LocaleURL returns the absolute URL for a docs page in a locale. The default
// locale is unprefixed (/docs/...); other locales are path-prefixed
// (/fr/docs/...).
func LocaleURL(locale, defaultLocale, path string) string {
	base := PageURL(path)
	if locale == "" || locale == defaultLocale {
		return base
	}
	return "/" + locale + base
}

// BuildBreadcrumbsURL is BuildBreadcrumbs with a locale prefix applied to every
// non-default locale's crumb URLs.
func BuildBreadcrumbsURL(locale, defaultLocale, path string) []Breadcrumb {
	crumbs := BuildBreadcrumbs(path)
	if locale == "" || locale == defaultLocale {
		return crumbs
	}
	for i := range crumbs {
		if crumbs[i].URL != "" {
			crumbs[i].URL = "/" + locale + crumbs[i].URL
		}
	}
	return crumbs
}

// BuildSidebarTree builds a recursive sidebar tree from pages.
// Pages are grouped by their directory structure - each subdirectory
// becomes a nested SidebarItem with Children, supporting infinite nesting.
// Frontmatter navigation metadata (`sidebar: {...}` and top-level `badge`) is
// applied here: `hidden` pages are dropped from the nav (still renderable and
// searchable), `label` overrides the displayed title, `order` overrides the
// sort position, and `badge` / `icon` decorate the entry. Directory sections
// inherit `collapsible` / `defaultOpen` / `badge` / `icon` from their index
// page.
func BuildSidebarTree(pages []Page) []SidebarItem {
	if len(pages) == 0 {
		return nil
	}

	SortPages(pages)

	// Per-page nav metadata keyed by URL so directory sections can inherit it
	// from their index page (matching sidebarIndexURLNav's resolution).
	navByURL := make(map[string]*SidebarNavConfig)
	badgeByURL := make(map[string]*Badge)
	for _, p := range pages {
		url := pagePathURL(p)
		if p.SidebarCfg != nil {
			navByURL[url] = p.SidebarCfg
		}
		if p.Badge != nil {
			badgeByURL[url] = p.Badge
		}
	}

	// entry is a link in a directory with the order it sorts by.
	type entry struct {
		item SidebarItem
		rank int
	}
	type dirNode struct {
		item    SidebarItem
		subdirs map[string]*dirNode
		links   []entry
		index   *Page // the directory's visible index page, if any
		idxPage *Page // the directory's index page even when hidden
	}

	root := &dirNode{
		item:    SidebarItem{Title: "__root__"},
		subdirs: make(map[string]*dirNode),
	}

	// Build the tree from the sorted pages list
	for _, p := range pages {
		cfg := p.SidebarCfg
		if cfg != nil && cfg.Hidden {
			continue
		}
		dir := p.Dir
		title := p.Title
		var icon string
		var badge *Badge
		if cfg != nil {
			if cfg.Label != "" {
				title = cfg.Label
			}
			icon = cfg.Icon
			if cfg.Badge != nil {
				badge = cfg.Badge
			}
		}
		if badge == nil {
			badge = p.Badge
		}
		linkItem := SidebarItem{
			Title: title,
			URL:   pagePathURL(p),
			Icon:  icon,
			Badge: badge,
		}
		if dir == "" {
			root.links = append(root.links, entry{linkItem, effectiveOrder(p)})
			continue
		}

		parts := strings.Split(dir, "/")
		current := root
		for _, part := range parts {
			key := strings.ToLower(part)
			next, ok := current.subdirs[key]
			if !ok {
				next = &dirNode{
					item: SidebarItem{
						Title:    PathToTitle(part),
						Children: []SidebarItem{},
					},
					subdirs: make(map[string]*dirNode),
				}
				current.subdirs[key] = next
			}
			current = next
		}
		current.links = append(current.links, entry{linkItem, effectiveOrder(p)})
		if isDirIndexPath(p.Path, dir) {
			pp := p
			current.idxPage = &pp
			if cfg == nil || !cfg.Hidden {
				current.index = &pp
			}
		}
	}

	// Convert tree to SidebarItem list (recursive). Each section carries its
	// sort rank: the order set on its index page, else after every link.
	type ranked struct {
		item    SidebarItem
		rank    int
		section bool
	}
	var flatten func(n *dirNode) ranked
	// sortRanked orders a directory's entries: by rank, links before sections
	// on a tie, sections that tie by title. Links keep their SortPages order.
	sortRanked := func(es []ranked) {
		sort.SliceStable(es, func(i, j int) bool {
			if es[i].rank != es[j].rank {
				return es[i].rank < es[j].rank
			}
			if es[i].section != es[j].section {
				return !es[i].section
			}
			if es[i].section {
				return es[i].item.Title < es[j].item.Title
			}
			return false
		})
	}
	flatten = func(n *dirNode) ranked {
		rank := noRank
		if ip := n.idxPage; ip != nil {
			if t := sectionTitle(*ip); t != "" {
				n.item.Title = t
			}
			if o := effectiveOrder(*ip); o != defaultPageOrder {
				rank = o
			}
		}
		var es []ranked
		for _, l := range n.links {
			es = append(es, ranked{l.item, l.rank, false})
		}
		for _, subdir := range n.subdirs {
			es = append(es, flatten(subdir))
		}
		sortRanked(es)
		n.item.Children = n.item.Children[:0]
		for _, e := range es {
			n.item.Children = append(n.item.Children, e.item)
		}
		if n.index != nil {
			idxURL := pagePathURL(*n.index)
			n.item.IndexURL = idxURL
			if cfg, ok := navByURL[idxURL]; ok {
				n.item.Icon = firstNonEmpty(cfg.Icon, n.item.Icon)
				n.item.Collapsible = n.item.Collapsible || cfg.Collapsible
				n.item.Expanded = n.item.Expanded || cfg.DefaultOpen
				if cfg.Badge != nil {
					n.item.Badge = cfg.Badge
				} else if b, ok2 := badgeByURL[idxURL]; ok2 {
					n.item.Badge = b
				}
			}
		}
		return ranked{n.item, rank, true}
	}

	// Root-level pages first, then directory sections that sort after them
	// unless an index page gives them an order.
	var top []ranked
	for _, l := range root.links {
		top = append(top, ranked{l.item, l.rank, false})
	}
	for _, subdir := range root.subdirs {
		top = append(top, flatten(subdir))
	}
	sortRanked(top)
	out := make([]SidebarItem, 0, len(top))
	for _, e := range top {
		out = append(out, e.item)
	}
	return out
}

const (
	// defaultPageOrder is the order of a page that sets none.
	defaultPageOrder = 999
	// noRank puts a section with no explicit order after every link.
	noRank = int(^uint(0) >> 1)
)

// isDirIndexPath reports whether path is the index page of dir.
func isDirIndexPath(path, dir string) bool {
	return path == dir+"/index"
}

// sectionTitle is the title of a folder section taken from its index page:
// sidebar.label, else an explicit title; "" keeps the folder-name title.
func sectionTitle(p Page) string {
	if p.SidebarCfg != nil && p.SidebarCfg.Label != "" {
		return p.SidebarCfg.Label
	}
	if p.TitleSet && p.Title != "" {
		return p.Title
	}
	return ""
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// BuildBreadcrumbs returns the breadcrumb trail as a slice.
// Index pages (e.g. "guides/index") produce breadcrumbs ending at the directory name ("Guides") - no "Index" crumb.
func BuildBreadcrumbs(path string) []Breadcrumb {
	path = NormalizePagePath(path)
	if path == "" {
		return nil
	}
	parts := strings.Split(path, "/")
	var result []Breadcrumb
	accum := ""
	for i, part := range parts {
		if part == "" {
			continue
		}
		accum += "/" + part
		label := PathToTitle(part)
		isLast := (i == len(parts)-1)
		result = append(result, Breadcrumb{
			Label:  label,
			URL:    DocsBasePath + accum + "/",
			IsLast: isLast,
		})
	}
	return result
}

// BuildBreadcrumbsTitled is BuildBreadcrumbsURL with real page titles: a crumb
// whose path matches a page (keyed by NormalizePagePath) uses that page's
// title instead of the humanised file name.
func BuildBreadcrumbsTitled(locale, defaultLocale, path string, pages []Page) []Breadcrumb {
	crumbs := BuildBreadcrumbsURL(locale, defaultLocale, path)
	if len(crumbs) == 0 {
		return crumbs
	}
	titles := make(map[string]string, len(pages))
	for _, p := range pages {
		if p.Title != "" {
			titles[NormalizePagePath(p.Path)] = p.Title
		}
	}
	norm := strings.Split(NormalizePagePath(path), "/")
	for i := range crumbs {
		if i >= len(norm) {
			break
		}
		if t, ok := titles[strings.Join(norm[:i+1], "/")]; ok {
			crumbs[i].Label = t
		}
	}
	return crumbs
}

// PathToTitle converts a file path or directory name to a human-readable title.
func PathToTitle(path string) string {
	base := path
	if idx := strings.LastIndex(base, "/"); idx >= 0 {
		base = base[idx+1:]
	}
	base = strings.ReplaceAll(base, "-", " ")
	base = strings.ReplaceAll(base, "_", " ")
	if len(base) > 0 {
		base = strings.ToUpper(base[:1]) + base[1:]
	}
	return base
}

// EnrichSidebarItems pre-computes IndexURL, Collapsible, and Expanded fields
// for each sidebar item relative to the given currentPath.
// This allows the TSX renderer to use these values directly without function calls.
// Values already set by frontmatter (sidebar: {collapsible, defaultOpen}) are
// preserved and OR'd with the auto-derived values.
func EnrichSidebarItems(items []SidebarItem, currentPath string) []SidebarItem {
	return EnrichSidebarItemsURL(items, PageURL(currentPath))
}

// EnrichSidebarItemsURL is EnrichSidebarItems keyed by the current page's full
// (locale/version-aware) URL, so active/expanded state is correct under i18n
// and versioning.
func EnrichSidebarItemsURL(items []SidebarItem, activeURL string) []SidebarItem {
	if len(items) == 0 {
		return items
	}
	out := make([]SidebarItem, len(items))
	for i, item := range items {
		out[i] = item
		out[i].IndexURL = sidebarIndexURLNav(item)
		out[i].Collapsible = item.Collapsible || (len(item.Children) > 0 && out[i].IndexURL != "")
		out[i].Expanded = item.Expanded || sidebarItemActiveNav(item, activeURL)
		if len(item.Children) > 0 {
			filtered := filterSidebarIndexChildNav(item.Children, out[i].IndexURL)
			out[i].Children = EnrichSidebarItemsURL(filtered, activeURL)
		}
	}
	return out
}

func slugFromTitleNav(title string) string {
	slug := strings.ToLower(strings.TrimSpace(title))
	slug = strings.ReplaceAll(slug, "_", "-")
	fields := strings.Fields(slug)
	slug = strings.Join(fields, "-")
	var out strings.Builder
	for _, r := range slug {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			out.WriteRune(r)
		}
	}
	return out.String()
}

func lastURLSegmentNav(url string) string {
	url = strings.Trim(url, "/")
	if idx := strings.LastIndex(url, "/"); idx >= 0 {
		return url[idx+1:]
	}
	return url
}

func sidebarIndexURLNav(item SidebarItem) string {
	if item.IndexURL != "" {
		return item.IndexURL
	}
	sectionSlug := slugFromTitleNav(item.Title)
	for _, child := range item.Children {
		if child.URL == "" || len(child.Children) > 0 {
			continue
		}
		if lastURLSegmentNav(child.URL) == sectionSlug {
			return child.URL
		}
	}
	return ""
}

func sidebarItemActiveNav(item SidebarItem, activeURL string) bool {
	if item.Active || (item.URL != "" && sameURL(item.URL, activeURL)) {
		return true
	}
	for _, child := range item.Children {
		if sidebarItemActiveNav(child, activeURL) {
			return true
		}
	}
	return false
}

func filterSidebarIndexChildNav(items []SidebarItem, indexURL string) []SidebarItem {
	if indexURL == "" {
		return items
	}
	filtered := make([]SidebarItem, 0, len(items))
	for _, item := range items {
		if item.URL == indexURL && len(item.Children) == 0 {
			continue
		}
		filtered = append(filtered, item)
	}
	return filtered
}
