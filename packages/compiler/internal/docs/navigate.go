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

// SortPages sorts pages by directory, then order, then title.
func SortPages(pages []Page) {
	sort.Slice(pages, func(i, j int) bool {
		if pages[i].Dir != pages[j].Dir {
			return pages[i].Dir < pages[j].Dir
		}
		if pages[i].Order != pages[j].Order {
			return pages[i].Order < pages[j].Order
		}
		return pages[i].Title < pages[j].Title
	})
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

// PageURL returns the full URL for a docs page path.
func PageURL(path string) string {
	normalized := NormalizePagePath(path)
	if normalized == "" {
		return "/docs/"
	}
	return "/docs/" + normalized + "/"
}

// BuildSidebarTree builds a recursive sidebar tree from pages.
// Pages are grouped by their directory structure — each subdirectory
// becomes a nested SidebarItem with Children, supporting infinite nesting.
func BuildSidebarTree(pages []Page) []SidebarItem {
	if len(pages) == 0 {
		return nil
	}

	SortPages(pages)

	type dirNode struct {
		item     SidebarItem
		subdirs  map[string]*dirNode
	}

	root := &dirNode{
		item:    SidebarItem{Title: "__root__"},
		subdirs: make(map[string]*dirNode),
	}

	// Build the tree from the sorted pages list
	for _, p := range pages {
		dir := p.Dir
		linkItem := SidebarItem{
			Title: p.Title,
			URL:   PageURL(p.Path),
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
			URL:    accum,
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
func EnrichSidebarItems(items []SidebarItem, currentPath string) []SidebarItem {
	if len(items) == 0 {
		return items
	}
	out := make([]SidebarItem, len(items))
	for i, item := range items {
		out[i] = item
		out[i].IndexURL = sidebarIndexURLNav(item)
		out[i].Collapsible = len(item.Children) > 0 && out[i].IndexURL != ""
		out[i].Expanded = sidebarItemActiveNav(item, currentPath)
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
