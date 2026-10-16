package plugin

import (
	"encoding/json"
	"fmt"
	"os"
	"path"
	"sort"
	"strings"

	"github.com/kratejs/krate/packages/compiler/internal/config"
	"github.com/kratejs/krate/packages/compiler/internal/docs"
)

// LLMsPluginOptions holds typed configuration for the llms.txt plugin.
type LLMsPluginOptions struct {
	BaseURL     string `json:"baseUrl"`
	ContentDir  string `json:"contentDir,omitempty"`
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
	// PathPrefix is the URL prefix docs are mounted under (default "/docs").
	PathPrefix string `json:"pathPrefix,omitempty"`
	// Full controls whether llms-full.txt (concatenated page text) is written.
	// Defaults to true.
	Full *bool `json:"full,omitempty"`
}

func init() {
	_ = Register(&HookFunc{name: "llms", order: 103, hooks: PluginHooks{
		AfterBuild: generateLLMs,
	}})
}

func generateLLMs(ctx *BuildResultHookCtx) error {
	cfg, ok := ctx.Config.(*config.Config)
	if !ok {
		return nil
	}
	opts := parseLLMsOptions(cfg)
	if opts == nil {
		return nil // not configured
	}

	baseURL := strings.TrimRight(opts.BaseURL, "/")
	if baseURL == "" {
		baseURL = strings.TrimRight(cfg.SEO.BaseURL, "/")
	}

	contentDir := opts.ContentDir
	if contentDir == "" {
		if docsOpts := parseDocsOptions(cfg); docsOpts != nil {
			contentDir = docsOpts.ContentDir
		}
	}
	if contentDir == "" {
		contentDir = "src/content/docs"
	}

	title := opts.Title
	if title == "" {
		if docsOpts := parseDocsOptions(cfg); docsOpts != nil {
			title = docsOpts.Title
		}
	}
	if title == "" {
		title = cfg.SEO.SiteName
	}
	if title == "" {
		title = "Documentation"
	}
	description := opts.Description
	if description == "" {
		description = cfg.SEO.Description
	}
	prefix := opts.PathPrefix
	if prefix == "" {
		prefix = docs.DocsBasePath
	}
	prefix = "/" + strings.Trim(prefix, "/")

	pages, err := docs.Scan(docs.Config{
		ContentDir: contentDir,
		Root:       ctx.Root,
		MDConfig:   cfg.Markdown,
	})
	if err != nil {
		return fmt.Errorf("llms: scanning docs: %w", err)
	}

	var visible []docs.Page
	for _, p := range pages {
		if p.Draft {
			continue
		}
		visible = append(visible, p)
	}
	sort.SliceStable(visible, func(i, j int) bool {
		if visible[i].Dir != visible[j].Dir {
			return visible[i].Dir < visible[j].Dir
		}
		if visible[i].Order != visible[j].Order {
			return visible[i].Order < visible[j].Order
		}
		return visible[i].Path < visible[j].Path
	})

	pageURL := func(p docs.Page) string {
		u := docsPageURL(prefix, p.Path)
		if baseURL == "" {
			return u
		}
		return baseURL + u
	}

	if err := os.WriteFile(path.Join(ctx.OutDir, "llms.txt"), []byte(renderLLMsIndex(title, description, visible, pageURL)), 0644); err != nil {
		return fmt.Errorf("writing llms.txt: %w", err)
	}
	if opts.Full == nil || *opts.Full {
		if err := os.WriteFile(path.Join(ctx.OutDir, "llms-full.txt"), []byte(renderLLMsFull(title, description, visible, pageURL)), 0644); err != nil {
			return fmt.Errorf("writing llms-full.txt: %w", err)
		}
	}
	return nil
}

func renderLLMsIndex(title, description string, pages []docs.Page, url func(docs.Page) string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", title)
	if description != "" {
		fmt.Fprintf(&b, "> %s\n\n", description)
	}
	lastDir := "\x00"
	for _, p := range pages {
		dir := p.Dir
		if dir == "" {
			dir = "Docs"
		}
		if dir != lastDir {
			fmt.Fprintf(&b, "## %s\n\n", dir)
			lastDir = dir
		}
		fmt.Fprintf(&b, "- [%s](%s)", p.Title, url(p))
		if p.Description != "" {
			fmt.Fprintf(&b, ": %s", p.Description)
		}
		b.WriteString("\n")
	}
	return b.String()
}

func renderLLMsFull(title, description string, pages []docs.Page, url func(docs.Page) string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", title)
	if description != "" {
		fmt.Fprintf(&b, "> %s\n\n", description)
	}
	for _, p := range pages {
		fmt.Fprintf(&b, "# %s\n\n", p.Title)
		fmt.Fprintf(&b, "URL: %s\n\n", url(p))
		b.WriteString(strings.TrimSpace(p.Content))
		b.WriteString("\n\n---\n\n")
	}
	return b.String()
}

func parseLLMsOptions(cfg *config.Config) *LLMsPluginOptions {
	for _, pc := range cfg.Plugins {
		if pc.Name == "llms" {
			opts := &LLMsPluginOptions{}
			if pc.Options != nil {
				data, err := json.Marshal(pc.Options)
				if err != nil {
					return nil
				}
				if err := json.Unmarshal(data, opts); err != nil {
					return nil
				}
			}
			return opts
		}
	}
	return nil
}
