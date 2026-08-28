package docs

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"krate-compiler/internal/markdown"
	"krate-compiler/internal/pluginutil"
)

// SidebarItem represents a single entry in a recursive sidebar tree.
// Items with Children act as expandable folder sections.
// Items with URL (and no Children) act as clickable links.
// Items with neither are plain text headers.
type SidebarItem struct {
	Title      string        `json:"title"`
	URL        string        `json:"url"`
	Active     bool          `json:"active"`
	Children   []SidebarItem `json:"children,omitempty"`
	IndexURL   string        `json:"indexURL,omitempty"`
	Collapsible bool         `json:"collapsible,omitempty"`
	Expanded   bool          `json:"expanded,omitempty"`
}

// Page represents a single documentation page.
type Page struct {
	Path          string        `json:"path"`          // relative path like "getting-started" or "guides/advanced"
	Title         string        `json:"title"`         // from frontmatter
	Order         int           `json:"order"`         // from frontmatter, default 999
	Content       string        `json:"content"`       // rendered HTML body
	Dir           string        `json:"dir"`           // directory grouping for sidebar
	Sidebar       string        `json:"sidebar"`       // optional sidebar section override from frontmatter
	SourcePath    string        `json:"sourcePath"`    // absolute path to original .md/.mdx file
	Keywords      []string      `json:"keywords,omitempty"` // explicit search keywords from frontmatter
	CustomSidebar []SidebarItem `json:"customSidebar"` // fully custom sidebar items (from JSON in frontmatter)
}

// Config holds configuration for scanning documentation.
type Config struct {
	ContentDir string         // relative to project root
	Root       string         // absolute project root
	MDConfig   markdown.Config // markdown rendering config
}

// Scan walks the content directory and parses all .md/.mdx files.
func Scan(cfg Config) ([]Page, error) {
	absDir := filepath.Join(cfg.Root, cfg.ContentDir)
	if _, err := os.Stat(absDir); os.IsNotExist(err) {
		return nil, nil
	}

	var pages []Page

	err := pluginutil.WalkMD(absDir, func(absPath, relPath string) error {
		data, err := os.ReadFile(absPath)
		if err != nil {
			return fmt.Errorf("reading %s: %w", absPath, err)
		}

		pagePath := strings.TrimSuffix(relPath, filepath.Ext(relPath))
		title := ""
		order := 999
		sidebar := ""
		var keywords []string
		var htmlContent string

		if strings.HasSuffix(absPath, ".mdx") {
			htmlContent, title, order, sidebar, keywords = ParseMDX(string(data), cfg.MDConfig)
		} else {
			htmlContent, title, order, sidebar, keywords = ParseMD(string(data), cfg.MDConfig)
		}

		if title == "" {
			title = PathToTitle(relPath)
		}

		// Detect custom sidebar JSON (starts with [)
		var customSidebar []SidebarItem
		if strings.HasPrefix(sidebar, "[") {
			if err := json.Unmarshal([]byte(sidebar), &customSidebar); err == nil {
				// Don't use custom sidebar JSON as a dir override
				sidebar = ""
			} else {
				customSidebar = nil
			}
		}

		dirPart := sidebar
		if dirPart == "" {
			if idx := strings.LastIndex(pagePath, "/"); idx > 0 {
				dirPart = pagePath[:idx]
			}
		}

		pages = append(pages, Page{
			Path:          pagePath,
			Title:         title,
			Order:         order,
			Content:       htmlContent,
			Dir:           dirPart,
			Sidebar:       sidebar,
			SourcePath:    absPath,
			Keywords:      keywords,
			CustomSidebar: customSidebar,
		})
		return nil
	})

	return pages, err
}

// ParseMD parses a .md file, extracting frontmatter and rendering markdown.
func ParseMD(src string, cfg markdown.Config) (html, title string, order int, sidebar string, keywords []string) {
	fm, segments := markdown.ParseMDXSegments(src, cfg)
	if t, ok := fm["title"]; ok {
		title = t
	}
	order = 999
	if oStr, ok := fm["order"]; ok {
		oStr = strings.TrimSpace(oStr)
		if n, err := strconv.Atoi(oStr); err == nil {
			order = n
		}
	}
	sidebar = fm["sidebar"]
	keywords = ParseKeywords(fm["keywords"])
	html = markdown.RenderSegmentsToHTML(segments)
	return
}

// ParseMDX parses an .mdx file, extracting frontmatter and rendering with JSX support.
func ParseMDX(src string, cfg markdown.Config) (html, title string, order int, sidebar string, keywords []string) {
	fm, segments := markdown.ParseMDXSegments(src, cfg)
	html = markdown.RenderSegmentsToHTML(segments)
	if t, ok := fm["title"]; ok {
		title = t
	}
	order = 999
	if oStr, ok := fm["order"]; ok {
		oStr = strings.TrimSpace(oStr)
		if n, err := strconv.Atoi(oStr); err == nil {
			order = n
		}
	}
	sidebar = fm["sidebar"]
	keywords = ParseKeywords(fm["keywords"])
	return
}

// ParseKeywords splits a frontmatter `keywords:` value (comma or space
// separated) into a slice of trimmed keywords.
func ParseKeywords(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	raw = strings.Trim(raw, "[]\"'")
	if raw == "" {
		return nil
	}
	parts := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t' || r == '\n'
	})
	var out []string
	for _, p := range parts {
		p = strings.Trim(p, ",\"'")
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// ExtractFrontmatter parses YAML frontmatter from markdown content.
func ExtractFrontmatter(src string) map[string]string {
	fm := make(map[string]string)
	if !strings.HasPrefix(strings.TrimSpace(src), "---") {
		return fm
	}
	end := strings.Index(src[3:], "\n---")
	if end < 0 {
		return fm
	}
	fmBlock := src[3 : 3+end]
	for _, line := range strings.Split(fmBlock, "\n") {
		line = strings.TrimSpace(line)
		if idx := strings.Index(line, ":"); idx > 0 {
			key := strings.TrimSpace(line[:idx])
			val := strings.TrimSpace(line[idx+1:])
			val = strings.Trim(val, "\"'")
			fm[key] = val
		}
	}
	return fm
}

// StripFrontmatter removes YAML frontmatter from markdown content.
func StripFrontmatter(src string) string {
	lines := strings.Split(src, "\n")
	if len(lines) < 2 || strings.TrimSpace(lines[0]) != "---" {
		return src
	}
	closeIdx := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			closeIdx = i
			break
		}
	}
	if closeIdx < 0 || closeIdx+1 >= len(lines) {
		return src
	}
	return strings.Join(lines[closeIdx+1:], "\n")
}

// SearchEntry represents a search index entry.
type SearchEntry struct {
	Title   string `json:"title"`
	Path    string `json:"path"`
	Content string `json:"content"`
}

// BuildSearchIndex generates a search index from documentation pages.
func BuildSearchIndex(pages []Page) []SearchEntry {
	var entries []SearchEntry
	for _, p := range pages {
		plainText := StripHTMLTags(p.Content)
		if len(plainText) > 500 {
			plainText = plainText[:500]
		}
		entries = append(entries, SearchEntry{
			Title:   p.Title,
			Path:    "/docs/" + p.Path + "/",
			Content: plainText,
		})
	}
	return entries
}

// StripHTMLTags removes HTML tags from a string.
func StripHTMLTags(s string) string {
	var out strings.Builder
	inTag := false
	for _, r := range s {
		if r == '<' {
			inTag = true
			continue
		}
		if r == '>' {
			inTag = false
			continue
		}
		if !inTag {
			out.WriteRune(r)
		}
	}
	return strings.TrimSpace(out.String())
}
