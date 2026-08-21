package plugin

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"krate-compiler/internal/config"
)

// communityOutput is the JSON result shape a JS plugin hook returns.
type communityOutput struct {
	Files          []fileEntry     `json:"files,omitempty"`
	Routes         []Route         `json:"routes,omitempty"`
	GeneratedPages []GeneratedPage `json:"generatedPages,omitempty"`
	HTML           *string         `json:"html,omitempty"`
	HeadHTML       *string         `json:"headHTML,omitempty"`
	RawCSS         *string         `json:"rawCSS,omitempty"`
}

// fileEntry describes a single file to write to the output directory.
type fileEntry struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

// runCommunityHook executes a configured community plugin for the given hook.
// Community plugins are JavaScript modules executed inside the embedded QuickJS
// runtime — there is no subprocess and no stdin/stdout protocol.
func runCommunityHook(hookName string, pc config.PluginConfig, root, outDir string, hookCtx interface{}) error {
	return runJSPluginHook(hookName, pc, root, outDir, hookCtx)
}

// applyPluginOutput writes plugin-produced files/routes and applies HTML/head/CSS
// modifications back to the typed hook context. Path traversal protection keeps
// all plugin writes inside the output directory.
func applyPluginOutput(hookName string, output *communityOutput, outDir string, hookCtx interface{}) error {
	outDirClean := filepath.Clean(outDir)

	for _, f := range output.Files {
		fullPath := filepath.Join(outDir, f.Path)
		fullPath = filepath.Clean(fullPath)
		if !strings.HasPrefix(fullPath, outDirClean+string(filepath.Separator)) && fullPath != outDirClean {
			return fmt.Errorf("plugin attempted path traversal: %s resolves outside output directory", f.Path)
		}
		if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
			return fmt.Errorf("creating directory for %s: %w", f.Path, err)
		}
		if err := os.WriteFile(fullPath, []byte(f.Content), 0644); err != nil {
			return fmt.Errorf("writing %s: %w", f.Path, err)
		}
	}

	for _, r := range output.Routes {
		routePath := strings.Trim(r.Path, "/")
		if routePath == "" {
			routePath = "."
		}
		pageDir := filepath.Join(outDir, routePath)
		pageDir = filepath.Clean(pageDir)
		if !strings.HasPrefix(pageDir, outDirClean+string(filepath.Separator)) && pageDir != outDirClean {
			return fmt.Errorf("plugin attempted path traversal: route %s resolves outside output directory", r.Path)
		}
		title := r.Title
		if title == "" {
			title = "Krate"
		}
		html := "<!DOCTYPE html>\n<html lang=\"en\">\n<head>\n<meta charset=\"UTF-8\">\n<meta name=\"viewport\" content=\"width=device-width, initial-scale=1.0\">\n<title>" + title + "</title>\n</head>\n<body>\n<div id=\"root\">" + r.Content + "</div>\n</body>\n</html>\n"
		if err := os.MkdirAll(pageDir, 0755); err != nil {
			return fmt.Errorf("creating directory for route %s: %w", r.Path, err)
		}
		htmlPath := filepath.Join(pageDir, "index.html")
		if err := os.WriteFile(htmlPath, []byte(html), 0644); err != nil {
			return fmt.Errorf("writing route %s: %w", r.Path, err)
		}
	}

	// Append generated pages to the build hook context so they enter the page
	// pipeline alongside real source pages.
	if len(output.GeneratedPages) > 0 {
		if buildCtx, ok := hookCtx.(*BuildHookCtx); ok && buildCtx.GeneratedPages != nil {
			*buildCtx.GeneratedPages = append(*buildCtx.GeneratedPages, output.GeneratedPages...)
		} else {
			for _, gp := range output.GeneratedPages {
				if gp.Path != "" {
					os.MkdirAll(filepath.Dir(gp.Path), 0755)
				}
			}
		}
	}

	// Apply HTML/head/CSS modifications back to the hook context
	switch hookName {
	case "AfterMarkdownParse":
		if ctx, ok := hookCtx.(*MarkdownHookCtx); ok {
			if output.HTML != nil {
				ctx.HTML = *output.HTML
			}
		}
	case "AfterRender":
		if ctx, ok := hookCtx.(*RenderHookCtx); ok {
			if output.HTML != nil {
				ctx.HTML = *output.HTML
			}
			if output.HeadHTML != nil {
				ctx.HeadHTML = ctx.HeadHTML + *output.HeadHTML
			}
			if output.RawCSS != nil {
				ctx.RawCSS = ctx.RawCSS + "\n" + *output.RawCSS
			}
		}
	case "AfterPage":
		if ctx, ok := hookCtx.(*PageHookCtx); ok {
			if output.HTML != nil {
				ctx.HTML = *output.HTML
			}
			if output.HeadHTML != nil {
				ctx.HeadHTML = ctx.HeadHTML + *output.HeadHTML
			}
		}
	}

	return nil
}
