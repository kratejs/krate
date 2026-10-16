package plugin

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/kratejs/krate/packages/compiler/internal/astjson"
	"github.com/kratejs/krate/packages/compiler/internal/config"
	"github.com/kratejs/krate/packages/compiler/internal/fsutil"
	"github.com/kratejs/krate/packages/compiler/internal/safe"
)

// pluginLimits returns the per-hook plugin output caps, overridable via
// KRATE_PLUGIN_MAX_FILES / KRATE_PLUGIN_MAX_FILE_BYTES.
func pluginLimits() (maxFiles, maxFileBytes int) {
	maxFiles, maxFileBytes = 10000, 64<<20
	if v := os.Getenv("KRATE_PLUGIN_MAX_FILES"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			maxFiles = n
		}
	}
	if v := os.Getenv("KRATE_PLUGIN_MAX_FILE_BYTES"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			maxFileBytes = n
		}
	}
	return maxFiles, maxFileBytes
}

// communityOutput is the JSON result shape a JS plugin hook returns.
type communityOutput struct {
	Files          []fileEntry     `json:"files,omitempty"`
	Routes         []Route         `json:"routes,omitempty"`
	GeneratedPages []GeneratedPage `json:"generatedPages,omitempty"`
	HTML           *string         `json:"html,omitempty"`
	HeadHTML       *string         `json:"headHTML,omitempty"`
	RawCSS         *string         `json:"rawCSS,omitempty"`
	Ast            json.RawMessage `json:"ast,omitempty"`
	Scripts        []string        `json:"scripts,omitempty"`
	MetaTags       []string        `json:"metaTags,omitempty"`
}

// fileEntry describes a single file to write to the output directory.
type fileEntry struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

// runCommunityHook executes a configured community plugin for the given hook.
// The plugin is dispatched by runtime: descriptors returning runtime "go" are
// launched as Go subprocesses, everything else runs as JavaScript inside the
// embedded QuickJS runtime - no subprocess and no stdin/stdout protocol.
func runCommunityHook(hookName string, pc config.PluginConfig, root, outDir string, env CommunityEnv, hookCtx interface{}) error {
	start := time.Now()
	err := safe.Run(func() error {
		if isGoPlugin(pc) {
			return runGoPluginHook(hookName, pc, root, outDir, env, hookCtx)
		}
		return runJSPluginHook(hookName, pc, root, outDir, env, hookCtx)
	})
	traceHook(pc.Name, hookName, time.Since(start), err)
	return err
}

// applyPluginOutput writes plugin-produced files/routes and applies HTML/head/CSS
// modifications back to the typed hook context. Path traversal protection keeps
// all plugin writes inside the output directory.
func applyPluginOutput(hookName string, output *communityOutput, outDir string, hookCtx interface{}) error {
	// Bound a runaway plugin so it cannot exhaust memory or fill the disk. The
	// limits are deliberately generous and overridable:
	// KRATE_PLUGIN_MAX_FILES (default 10000), KRATE_PLUGIN_MAX_FILE_BYTES
	// (default 64 MiB).
	maxFiles, maxFileBytes := pluginLimits()
	if len(output.Files) > maxFiles {
		return fmt.Errorf("plugin produced too many files (%d > %d; raise KRATE_PLUGIN_MAX_FILES if intended)", len(output.Files), maxFiles)
	}

	for _, f := range output.Files {
		if len(f.Content) > maxFileBytes {
			return fmt.Errorf("plugin file %s exceeds the size limit (%d bytes; raise KRATE_PLUGIN_MAX_FILE_BYTES if intended)", f.Path, len(f.Content))
		}
		fullPath := filepath.Join(outDir, f.Path)
		if !fsutil.WithinDir(outDir, fullPath) {
			return fmt.Errorf("plugin attempted path traversal: %s resolves outside output directory", f.Path)
		}
		if err := fsutil.WriteFileAtomic(fullPath, []byte(f.Content), 0644); err != nil {
			return fmt.Errorf("writing %s: %w", f.Path, err)
		}
	}

	for _, r := range output.Routes {
		routePath := strings.Trim(r.Path, "/")
		if routePath == "" {
			routePath = "."
		}
		pageDir := filepath.Join(outDir, routePath)
		if !fsutil.WithinDir(outDir, pageDir) {
			return fmt.Errorf("plugin attempted path traversal: route %s resolves outside output directory", r.Path)
		}
		title := r.Title
		if title == "" {
			title = "Krate"
		}
		html := "<!DOCTYPE html>\n<html lang=\"en\">\n<head>\n<meta charset=\"UTF-8\">\n<meta name=\"viewport\" content=\"width=device-width, initial-scale=1.0\">\n<title>" + title + "</title>\n</head>\n<body>\n<div id=\"root\">" + r.Content + "</div>\n</body>\n</html>\n"
		htmlPath := filepath.Join(pageDir, "index.html")
		if err := fsutil.WriteFileAtomic(htmlPath, []byte(html), 0644); err != nil {
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
					_ = os.MkdirAll(filepath.Dir(gp.Path), 0755)
				}
			}
		}
	}

	// Apply HTML/head/CSS modifications back to the hook context
	switch hookName {
	case "AfterParse":
		if ctx, ok := hookCtx.(*ParseHookCtx); ok {
			if len(output.Ast) > 0 {
				prog, err := astjson.DecodeProgram(output.Ast)
				if err != nil {
					return fmt.Errorf("decoding edited AST: %w", err)
				}
				ctx.Program = prog
			}
		}
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
			ctx.HeadHTML = appendHeadInjections(ctx.HeadHTML, output.Scripts, output.MetaTags)
		}
	case "AfterPage":
		if ctx, ok := hookCtx.(*PageHookCtx); ok {
			if output.HTML != nil {
				ctx.HTML = *output.HTML
			}
			if output.HeadHTML != nil {
				ctx.HeadHTML = ctx.HeadHTML + *output.HeadHTML
			}
			ctx.HeadHTML = appendHeadInjections(ctx.HeadHTML, output.Scripts, output.MetaTags)
		}
	}

	return nil
}

// appendHeadInjections renders plugin script URLs and meta tag attribute strings
// into head HTML. Meta tags carry full attribute markup (e.g.
// `name="description" content="..."`) which is wrapped in <meta ...> tags.
func appendHeadInjections(head string, scripts, metaTags []string) string {
	for _, m := range metaTags {
		head += "\n<meta " + m + ">"
	}
	for _, s := range scripts {
		head += "\n<script src=\"" + s + "\"></script>"
	}
	return head
}
