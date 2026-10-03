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
// "index" → ""
// "guides/index" → "guides"
// "guides/advanced" → "guides/advanced"
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

// BuildSidebarTree builds a recursive sidebar tree from pages.
// Pages are grouped by their directory structure — each subdirectory
// becomes a nested SidebarItem with Children, supporting infinite nesting.
//
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
		url := PageURL(p.Path)
		if p.SidebarCfg != nil {
			navByURL[url] = p.SidebarCfg
		}
		if p.Badge != nil {
			badgeByURL[url] = p.Badge
		}
	}

	type dirNode struct {
		item    SidebarItem
		subdirs map[string]*dirNode
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
			URL:   PageURL(p.Path),
			Icon:  icon,
			Badge: badge,
		}
		if dir == "" {
			root.item.Children = append(root.item.Children, linkItem)
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
		current.item.Children = append(current.item.Children, linkItem)
	}

	// Convert tree to SidebarItem list (recursive)
	var flatten func(n *dirNode) SidebarItem
	flatten = func(n *dirNode) SidebarItem {
		for _, subdir := range n.subdirs {
			n.item.Children = append(n.item.Children, flatten(subdir))
		}
		if n.item.Title != "__root__" {
			if idxURL := sidebarIndexURLNav(n.item); idxURL != "" {
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
		}
		// Stable sort: sections after links, sections alphabetically,
		// links preserve insertion order (from SortPages: Dir→Order→Title)
		sort.SliceStable(n.item.Children, func(i, j int) bool {
			hasChildrenI := len(n.item.Children[i].Children) > 0
			hasChildrenJ := len(n.item.Children[j].Children) > 0
			if hasChildrenI != hasChildrenJ {
				return !hasChildrenI // links before sections
			}
			if hasChildrenI {
				return n.item.Children[i].Title < n.item.Children[j].Title
			}
			return false // preserve original order for links
		})
		return n.item
	}

	// Collect directory sections, sorted by title
	var dirSections []SidebarItem
	for _, subdir := range root.subdirs {
		dirSections = append(dirSections, flatten(subdir))
	}
	sort.Slice(dirSections, func(i, j int) bool {
		return dirSections[i].Title < dirSections[j].Title
	})

	// Root-level pages first, then directory sections
	return append(root.item.Children, dirSections...)
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// BuildBreadcrumbs returns the breadcrumb trail as a slice.
// Index pages (e.g. "guides/index") produce breadcrumbs ending at the directory name ("Guides") — no "Index" crumb.
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
	if len(items) == 0 {
		return items
	}
	out := make([]SidebarItem, len(items))
	for i, item := range items {
		out[i] = item
		out[i].IndexURL = sidebarIndexURLNav(item)
		out[i].Collapsible = item.Collapsible || (len(item.Children) > 0 && out[i].IndexURL != "")
		out[i].Expanded = item.Expanded || sidebarItemActiveNav(item, currentPath)
		if len(item.Children) > 0 {
			filtered := filterSidebarIndexChildNav(item.Children, out[i].IndexURL)
			out[i].Children = EnrichSidebarItems(filtered, currentPath)
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

func sidebarItemActiveNav(item SidebarItem, currentPath string) bool {
	if item.Active || (item.URL != "" && item.URL == PageURL(currentPath)) {
		return true
	}
	for _, child := range item.Children {
		if sidebarItemActiveNav(child, currentPath) {
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
