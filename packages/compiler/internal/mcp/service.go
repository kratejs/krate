package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/kratejs/krate/packages/compiler/internal/astjson"
	"github.com/kratejs/krate/packages/compiler/internal/astprint"
	"github.com/kratejs/krate/packages/compiler/internal/build"
	"github.com/kratejs/krate/packages/compiler/internal/check"
	"github.com/kratejs/krate/packages/compiler/internal/config"
	"github.com/kratejs/krate/packages/compiler/internal/content"
	"github.com/kratejs/krate/packages/compiler/internal/docfind"
	"github.com/kratejs/krate/packages/compiler/internal/docs"
	"github.com/kratejs/krate/packages/compiler/internal/frontmatter"
	"github.com/kratejs/krate/packages/compiler/internal/kratedocs"
	"github.com/kratejs/krate/packages/compiler/internal/markdown"
	"github.com/kratejs/krate/packages/compiler/internal/plugin"
	"github.com/kratejs/krate/packages/compiler/internal/pluginapi"
	"github.com/kratejs/krate/packages/compiler/internal/routetypes"
)

// Options configures a Service.
type Options struct {
	Root    string
	Cfg     *config.Config
	Env     map[string]string
	Verbose bool
}

// Service implements the MCP tools and resources over one in-process Builder.
type Service struct {
	root    string
	cfg     *config.Config
	env     map[string]string
	verbose bool
	builder *build.Builder
	policy  *Policy

	// runMu serializes write operations and build/check (which swap os.Stdout
	// during stdout capture) so concurrent requests never race.
	runMu sync.Mutex
}

// NewService creates a Service and its session Builder.
func NewService(opts Options) *Service {
	return &Service{
		root:    opts.Root,
		cfg:     opts.Cfg,
		env:     opts.Env,
		verbose: opts.Verbose,
		builder: newBuilder(opts),
		policy:  loadPolicy(opts.Root),
	}
}

// newBuilder constructs a Builder. Each build-scoped operation gets a fresh
// one because BuildAll closes plugin subprocesses on completion, so a Builder
// is not safely reusable across builds.
func newBuilder(opts Options) *build.Builder {
	b := build.New(opts.Root, opts.Cfg)
	b.Verbose = opts.Verbose
	b.Env = opts.Env
	return b
}

// Register installs every tool, resource, template, prompt, and completion on
// the MCP server.
func (s *Service) Register(srv *Server) {
	// ── read tools ────────────────────────────────────────────────────────
	srv.RegisterTool(Tool{
		Name:        "list_routes",
		Description: "List every route in the project with its source file, render mode, and dynamic params.",
		InputSchema: objSchema(nil),
		Annotations: readOnlyAnnotations("List Routes"),
		Handler:     s.toolListRoutes,
	})
	srv.RegisterTool(Tool{
		Name:        "read_page",
		Description: "Read a page by route (e.g. /about) or source path. Returns the requested format(s): source (raw file text), ast (kind-tagged document), or html (rendered output when built).",
		InputSchema: objSchema(map[string]any{
			"route":  strSchema("Route path or page source path"),
			"source": strSchema("Page source path (alternative to route)"),
			"format": enumSchema("What to return (default \"source\")", "source", "ast", "html", "all"),
		}),
		Annotations: readOnlyAnnotations("Read Page"),
		Handler:     s.toolReadPage,
	})
	srv.RegisterTool(Tool{
		Name:        "search_docs",
		Description: "Search Krate's own framework documentation (embedded in the compiler) for a query, returning ranked pages with short excerpts. Searches Krate usage — not the current project's content. Read the full page with the krate://docs/{slug} resource, or pass full:true to include each hit's cleaned full text.",
		InputSchema: objSchema(map[string]any{
			"query":    strSchema("Search query"),
			"limit":    numSchema("Maximum results (default 8)"),
			"full":     boolSchema("Include each hit's full cleaned text in a content field (default false)"),
			"maxChars": numSchema("Excerpt window size in characters (default 240; only affects excerpt, not full)"),
		}, "query"),
		Annotations: readOnlyAnnotations("Search Docs"),
		Handler:     s.toolSearchDocs,
	})
	srv.RegisterTool(Tool{
		Name:        "build",
		Description: "Build the site and return diagnostics plus a per-route summary. Read-only with respect to source files.",
		InputSchema: objSchema(nil),
		Annotations: rebuildToolAnnotations("Build Site"),
		Handler:     s.toolBuild,
	})
	srv.RegisterTool(Tool{
		Name:        "check",
		Description: "Run the compiler-enforced quality gates (a11y/SEO/perf) against the site, building first when needed.",
		InputSchema: objSchema(nil),
		Annotations: rebuildToolAnnotations("Run Quality Checks"),
		Handler:     s.toolCheck,
	})

	// ── write tools (dry-run by default) ──────────────────────────────────
	srv.RegisterTool(Tool{
		Name:        "create_page",
		Description: "Create a new page from a template (static, content-list, detail) + optional content entry and layout. Returns a unified diff unless apply=true.",
		InputSchema: objSchema(map[string]any{
			"route":        strSchema("Route path, e.g. /pricing or /blog/[slug], or a source path under pagesDir"),
			"template":     enumSchema("Page template (default static)", "static", "content-list", "detail", "blank"),
			"title":        strSchema("Page title used by the static template"),
			"collection":   strSchema("Content collection name for content-list/detail templates"),
			"contentEntry": objOnlySchema("For content-backed templates: a single entry { slug, data or frontmatter fields } to author alongside the page"),
			"content":      strSchema("Full page source override (when provided, template is ignored)"),
			"withLayout":   boolSchema("Create _layout.tsx if it does not exist (default false)"),
			"apply":        boolSchema("Write the files (default false: dry-run)"),
		}, "route"),
		Annotations: additiveToolAnnotations("Create Page"),
		Handler:     s.toolCreatePage,
	})
	srv.RegisterTool(Tool{
		Name:        "edit_ast",
		Description: "Replace a page's AST with an edited kind-tagged document (from read_page). Returns a unified diff unless apply=true. Refuses files containing TypeScript constructs the parser drops.",
		InputSchema: objSchema(map[string]any{
			"route":  strSchema("Route path or source path"),
			"source": strSchema("Page source path (alternative to route)"),
			"ast":    strSchema("Kind-tagged AST document (JSON)"),
			"apply":  boolSchema("Write the file (default false: dry-run)"),
		}, "ast"),
		Annotations: destructiveToolAnnotations("Edit Page AST"),
		Handler:     s.toolEditAST,
	})
	srv.RegisterTool(Tool{
		Name:        "edit_page",
		Description: "Edit a page or any project file's source directly (no AST required): provide content for a full-file replace, or find+replace for a targeted in-place edit. Returns a unified diff unless apply=true. Editable source (.ts/.tsx/.js/.jsx) is re-parsed and the edit is refused if it would not parse.",
		InputSchema: objSchema(map[string]any{
			"route":      strSchema("Route path (e.g. /about) or a project-relative path (e.g. src/pages/about.tsx, src/styles/main.css, src/content/blog/hello.md)"),
			"content":    strSchema("Full new file content (full replace; creates the file when it does not exist)"),
			"find":       strSchema("Exact text to replace; must match once unless replaceAll is true"),
			"replace":    strSchema("Replacement text for find"),
			"replaceAll": boolSchema("Allow replacing all occurrences of find (default false)"),
			"apply":      boolSchema("Write the file (default false: dry-run)"),
		}, "route"),
		Annotations: destructiveToolAnnotations("Edit Page Source"),
		Handler:     s.toolEditPage,
	})
	srv.RegisterTool(Tool{
		Name:        "list_files",
		Description: "List project files (project-relative paths), skipping node_modules/.git/dist by default. Filter with glob (supports * and **). Use this to discover components, styles, and assets before reading or editing.",
		InputSchema: objSchema(map[string]any{
			"dir":           strSchema("Directory to list, relative to the project root (default: the whole project)"),
			"glob":          strSchema("Optional path glob filter, e.g. src/components/**/*.tsx"),
			"recursive":     boolSchema("Recurse into subdirectories (default true)"),
			"includeDenied": boolSchema("Include paths the policy denies (default false)"),
		}),
		Annotations: readOnlyAnnotations("List Files"),
		Handler:     s.toolListFiles,
	})
	srv.RegisterTool(Tool{
		Name:        "read_file",
		Description: "Read any project file by project-relative path. Returns text content (with a language hint) or a binary marker. Sensitive paths (environment files, keys) are refused unless allowed in .krate/mcp.json.",
		InputSchema: objSchema(map[string]any{
			"path": strSchema("Project-relative path, e.g. src/components/ui/button.tsx"),
		}, "path"),
		Annotations: readOnlyAnnotations("Read File"),
		Handler:     s.toolReadFile,
	})
	srv.RegisterTool(Tool{
		Name:        "create_file",
		Description: "Create a new project file. Refuses to overwrite an existing file (use edit_page to modify), respects the write policy, and parse-gates editable source. Returns a unified diff unless apply=true.",
		InputSchema: objSchema(map[string]any{
			"path":    strSchema("Project-relative path to create"),
			"content": strSchema("Full file content"),
			"apply":   boolSchema("Write the file (default false: dry-run)"),
		}, "path", "content"),
		Annotations: additiveToolAnnotations("Create File"),
		Handler:     s.toolCreateFile,
	})
	srv.RegisterTool(Tool{
		Name:        "delete_file",
		Description: "Delete a project file (files only). Respects the write policy. Returns a preview unless apply=true.",
		InputSchema: objSchema(map[string]any{
			"path":  strSchema("Project-relative path to delete"),
			"apply": boolSchema("Delete the file (default false: dry-run)"),
		}, "path"),
		Annotations: destructiveToolAnnotations("Delete File"),
		Handler:     s.toolDeleteFile,
	})
	srv.RegisterTool(Tool{
		Name:        "move_file",
		Description: "Move or rename a project file. Respects the write policy for both endpoints. Returns a preview unless apply=true.",
		InputSchema: objSchema(map[string]any{
			"from":  strSchema("Current project-relative path"),
			"to":    strSchema("New project-relative path"),
			"apply": boolSchema("Perform the move (default false: dry-run)"),
		}, "from", "to"),
		Annotations: destructiveToolAnnotations("Move File"),
		Handler:     s.toolMoveFile,
	})
	srv.RegisterTool(Tool{
		Name:        "create_component",
		Description: "Scaffold a Krate component (client, static, or server) under src/components/<kebab-name>/, optionally with a companion .css file. Returns a unified diff unless apply=true. Use read_file/edit_page for further changes.",
		InputSchema: objSchema(map[string]any{
			"name":    strSchema("PascalCase component name, e.g. StatusBadge"),
			"kind":    enumSchema("Component tier (default client)", "client", "static", "server"),
			"dir":     strSchema("Base directory (default src/components)"),
			"withCss": boolSchema("Also create a companion CSS file (default false)"),
			"apply":   boolSchema("Write the file(s) (default false: dry-run)"),
		}, "name"),
		Annotations: additiveToolAnnotations("Create Component"),
		Handler:     s.toolCreateComponent,
	})
	srv.RegisterTool(Tool{
		Name:        "read_content",
		Description: "Read content-collection entries. With only a collection, lists its entries (slug + path + frontmatter). With a slug, returns the full entry: raw content, parsed frontmatter, and markdown body.",
		InputSchema: objSchema(map[string]any{
			"collection": strSchema("Collection name as configured under content: in krate.config.ts or contributed by a plugin (e.g. docs)"),
			"slug":       strSchema("Entry slug to read, e.g. hello-world or guides/advanced (omit to list)"),
		}, "collection"),
		Annotations: readOnlyAnnotations("Read Content"),
		Handler:     s.toolReadContent,
	})
	srv.RegisterTool(Tool{
		Name:        "create_content",
		Description: "Create a new entry in a content collection (configured under content: in krate.config.ts or contributed by a plugin, e.g. the docs collection). Validates the frontmatter against the collection schema, refuses existing entries, and returns a unified diff unless apply=true.",
		InputSchema: objSchema(map[string]any{
			"collection": strSchema("Collection name (e.g. blog, or docs when the docs plugin contributes it)"),
			"slug":       strSchema("Entry slug (e.g. hello-world or guides/advanced)"),
			"data":       objOnlySchema("Frontmatter fields, validated against the collection's schema"),
			"body":       strSchema("Markdown body of the entry"),
			"apply":      boolSchema("Write the entry (default false: dry-run)"),
		}, "collection", "slug"),
		Annotations: additiveToolAnnotations("Create Content Entry"),
		Handler:     s.toolCreateContent,
	})
	srv.RegisterTool(Tool{
		Name:        "edit_content",
		Description: "Edit an existing content entry. Modes: full-file replace via content, frontmatter rewrite via data (body kept unless body is given), or a targeted find+replace. Re-parses the result and refuses edits whose frontmatter violates the collection schema. Returns a unified diff unless apply=true.",
		InputSchema: objSchema(map[string]any{
			"collection": strSchema("Collection name (e.g. blog, or docs when the docs plugin contributes it)"),
			"slug":       strSchema("Entry slug (e.g. hello-world or guides/advanced)"),
			"content":    strSchema("Full new entry content (frontmatter + body)"),
			"data":       objOnlySchema("Frontmatter fields to write (merged over existing; body preserved unless body is given)"),
			"body":       strSchema("New markdown body (only used with data)"),
			"find":       strSchema("Exact text to replace; must match once unless replaceAll is true"),
			"replace":    strSchema("Replacement text for find"),
			"replaceAll": boolSchema("Allow replacing all occurrences of find (default false)"),
			"apply":      boolSchema("Write the edit (default false: dry-run)"),
		}, "collection", "slug"),
		Annotations: destructiveToolAnnotations("Edit Content Entry"),
		Handler:     s.toolEditContent,
	})

	// ── resources ─────────────────────────────────────────────────────────
	srv.RegisterResource(Resource{
		URI:         "krate://routes",
		Name:        "routes",
		Description: "Every route in the project (JSON).",
		MIMEType:    "application/json",
		Read: func(ctx context.Context, uri string) (ResourceContents, *rpcError) {
			routes, err := s.builder.RouteList()
			if err != nil {
				return ResourceContents{}, Errorf("listing routes: %v", err)
			}
			return jsonResource(uri, routes)
		},
	})
	srv.RegisterResource(Resource{
		URI:         "krate://content",
		Name:        "content",
		Description: "Typed content collections and their entries (JSON).",
		MIMEType:    "application/json",
		Read:        s.readContentResource,
	})
	srv.RegisterResource(Resource{
		URI:         "krate://manifest",
		Name:        "manifest",
		Description: "The built site manifest (JSON); empty when the site is unbuilt.",
		MIMEType:    "application/json",
		Read:        s.readManifestResource,
	})
	srv.RegisterResource(Resource{
		URI:         "krate://config",
		Name:        "config",
		Description: "The resolved Krate config, curated (relative paths, no env values).",
		MIMEType:    "application/json",
		Read:        s.readConfigResource,
	})
	srv.RegisterResourceTemplate(&ResourceTemplate{
		URITemplate: "krate://page/{route}",
		Name:        "page",
		Title:       "Page by route",
		Description: "A single page by route, e.g. krate://page/about.",
		MIMEType:    "application/json",
		Read:        s.readPageTemplate,
	})
	srv.RegisterResourceTemplate(&ResourceTemplate{
		URITemplate: "krate://docs/{slug}",
		Name:        "docs",
		Title:       "Krate documentation page",
		Description: "A Krate framework documentation page by slug, e.g. krate://docs/features/mcp or krate://docs/cli. Slugs come from search_docs.",
		MIMEType:    "text/markdown",
		Read:        s.readDocsTemplate,
	})

	// ── prompts ───────────────────────────────────────────────────────────
	srv.RegisterPrompt(s.promptAddPage())
	srv.RegisterPrompt(s.promptPublishContent())
	srv.RegisterPrompt(s.promptFixChecks())
	srv.RegisterPrompt(s.promptExplore())

	// ── completions ───────────────────────────────────────────────────────
	srv.RegisterCompletion("ref/resource", "krate://page/{route}", "route", s.completeRoutes)
	srv.RegisterCompletion("ref/resource", "krate://docs/{slug}", "slug", s.completeDocSlugs)
	srv.RegisterCompletion("ref/prompt", "add-page", "template", s.completeTemplates)
	srv.RegisterCompletion("ref/prompt", "add-page", "collection", s.completeCollections)
	srv.RegisterCompletion("ref/prompt", "add-page", "route", s.completeRoutes)
	srv.RegisterCompletion("ref/prompt", "publish-content", "collection", s.completeCollections)
	srv.RegisterCompletion("ref/tool", "edit_page", "route", s.completeRoutes)
	srv.RegisterCompletion("ref/tool", "read_page", "route", s.completeRoutes)
	srv.RegisterCompletion("ref/tool", "read_content", "collection", s.completeCollections)
	srv.RegisterCompletion("ref/tool", "read_content", "slug", s.completeContentSlugs)
	srv.RegisterCompletion("ref/tool", "create_content", "collection", s.completeCollections)
	srv.RegisterCompletion("ref/tool", "create_content", "slug", s.completeContentSlugs)
	srv.RegisterCompletion("ref/tool", "edit_content", "collection", s.completeCollections)
	srv.RegisterCompletion("ref/tool", "edit_content", "slug", s.completeContentSlugs)
}

// ── read tools ──────────────────────────────────────────────────────────────

func (s *Service) toolListRoutes(ctx context.Context, _ map[string]any) (ToolResult, *rpcError) {
	routes, err := s.builder.RouteList()
	if err != nil {
		return ErrorResult("listing routes: " + err.Error()), nil
	}
	return JSONResult(routes)
}

func (s *Service) toolReadPage(ctx context.Context, args map[string]any) (ToolResult, *rpcError) {
	target := argString(args, "route")
	if target == "" {
		target = argString(args, "source")
	}
	if target == "" {
		return ErrorResult("provide route or source"), nil
	}
	format := build.PageDetailFormat(argString(args, "format"))
	switch format {
	case "", build.FormatSource, build.FormatAST, build.FormatHTML, build.FormatAll:
	default:
		return ErrorResult("format must be one of: source, ast, html, all"), nil
	}
	if format == "" {
		format = build.FormatSource
	}
	detail, err := s.builder.PageDetailFor(target, format)
	if err != nil {
		return ErrorResult(err.Error()), nil
	}
	return JSONResult(detail)
}

func (s *Service) toolSearchDocs(ctx context.Context, args map[string]any) (ToolResult, *rpcError) {
	query, rerr := requireString(args, "query")
	if rerr != nil {
		return ToolResult{}, rerr
	}
	limit := 8
	if v, ok := args["limit"].(float64); ok && v > 0 {
		limit = int(v)
	}
	if limit > 50 {
		limit = 50
	}
	maxChars := 240
	if v, ok := args["maxChars"].(float64); ok && v > 0 {
		maxChars = int(v)
	}
	if maxChars < 80 {
		maxChars = 80
	}
	if maxChars > 4000 {
		maxChars = 4000
	}
	full := argBool(args, "full", false)

	pages := kratedocs.Pages()
	if len(pages) == 0 {
		return JSONResult([]any{})
	}
	results, err := docfind.Search(ctx, docs.BuildSearchDocuments(pages), query, limit)
	if err != nil {
		return ErrorResult("searching docs: " + err.Error()), nil
	}

	// Index the framework pages by slug so results can carry a stable slug and
	// a project-independent docs URL.
	byHref := map[string]kratedocs.Doc{}
	for _, d := range kratedocs.Docs() {
		byHref[docs.PageURL(d.Page.Path)] = d
	}

	type hit struct {
		Title      string   `json:"title"`
		Slug       string   `json:"slug"`
		Path       string   `json:"path"`
		Resource   string   `json:"resource"`
		Excerpt    string   `json:"excerpt"`
		Content    string   `json:"content,omitempty"`
		Tags       []string `json:"tags,omitempty"`
		Categories []string `json:"categories,omitempty"`
	}
	hits := make([]hit, 0, len(results))
	for _, r := range results {
		h := hit{Title: r.Title, Path: r.Href}
		if d, ok := byHref[r.Href]; ok {
			body := cleanDocBody(d)
			h.Slug = d.Slug
			h.Resource = "krate://docs/" + d.Slug
			h.Excerpt = excerpt(body, query, maxChars)
			if full {
				h.Content = body
			}
			h.Tags = d.Page.Tags
			h.Categories = d.Page.Categories
		} else {
			h.Excerpt = excerpt(r.Body, query, maxChars)
			if full {
				h.Content = html.UnescapeString(r.Body)
			}
		}
		hits = append(hits, h)
	}
	return JSONResult(hits)
}

func (s *Service) toolBuild(ctx context.Context, _ map[string]any) (ToolResult, *rpcError) {
	out, err := s.runBuild(ctx)
	if err != nil {
		return ErrorResult(fmt.Sprintf("build failed: %v\n\n%s", err, out)), nil
	}
	routes, _ := s.builder.RouteList()
	return JSONResult(map[string]any{
		"ok":     true,
		"routes": len(routes),
		"output": strings.TrimSpace(out),
	})
}

func (s *Service) toolCheck(ctx context.Context, _ map[string]any) (ToolResult, *rpcError) {
	// Build if the output directory is not present, then evaluate gates.
	if _, err := os.Stat(filepath.Join(s.cfg.OutDir, "manifest.json")); err != nil {
		if out, buildErr := s.runBuild(ctx); buildErr != nil {
			return ErrorResult(fmt.Sprintf("build failed: %v\n\n%s", buildErr, out)), nil
		}
	}

	// CheckSite re-reads dist/; capture its stdout so preflight/build noise
	// never reaches the JSON-RPC stream.
	var findings []check.Finding
	var cfg check.Config
	chk := newBuilder(Options{Root: s.root, Cfg: s.cfg, Env: s.env, Verbose: s.verbose})
	_, err := captureStdout(func() error {
		var e error
		findings, cfg, e = chk.CheckSite(true)
		return e
	})
	if err != nil {
		return ErrorResult("running checks: " + err.Error()), nil
	}
	errs, warns := check.Counts(findings)
	if findings == nil {
		findings = []check.Finding{}
	}
	// Render findings with a friendly severity label and portable paths.
	type view struct {
		Rule     string `json:"rule"`
		Category string `json:"category"`
		Severity string `json:"severity"`
		Route    string `json:"route,omitempty"`
		File     string `json:"file,omitempty"`
		Line     int    `json:"line,omitempty"`
		Col      int    `json:"col,omitempty"`
		Message  string `json:"message"`
		Hint     string `json:"hint,omitempty"`
	}
	views := make([]view, 0, len(findings))
	for _, f := range findings {
		views = append(views, view{
			Rule:     f.Rule,
			Category: f.Category,
			Severity: f.Severity.String(),
			Route:    f.Route,
			File:     filepath.ToSlash(f.File),
			Line:     f.Line,
			Col:      f.Col,
			Message:  f.Message,
			Hint:     f.Hint,
		})
	}
	return JSONResult(map[string]any{
		"errors":   errs,
		"warnings": warns,
		"failOn":   cfg.FailOn.String(),
		"failing":  check.Failing(findings, cfg.FailOn),
		"findings": views,
	})
}

// ── write tools ─────────────────────────────────────────────────────────────

// pageTemplate describes how create_page scaffolds a page.
type pageTemplate string

const (
	tmplStatic      pageTemplate = "static"
	tmplContentList pageTemplate = "content-list"
	tmplDetail      pageTemplate = "detail"
	tmplBlank       pageTemplate = "blank"
)

func (s *Service) toolCreatePage(ctx context.Context, args map[string]any) (ToolResult, *rpcError) {
	routeArg, rerr := requireString(args, "route")
	if rerr != nil {
		return ToolResult{}, rerr
	}
	apply := argBool(args, "apply", false)

	route := routetypes.Normalize(routeArg)
	if route == "/" || route == "" {
		return ErrorResult("a non-root route is required"), nil
	}
	clean := strings.TrimPrefix(route, "/")
	if strings.Contains(clean, "..") || filepath.IsAbs(clean) {
		return ErrorResult("invalid route"), nil
	}

	rel, err := routeToPageFile(clean)
	if err != nil {
		return ErrorResult(err.Error()), nil
	}
	abs := filepath.Join(s.cfg.PagesDir, filepath.FromSlash(rel))

	// Scaffold the source. A raw content override wins.
	content := argString(args, "content")
	if content == "" {
		tmpl := pageTemplate(argString(args, "template"))
		if tmpl == "" {
			tmpl = tmplStatic
		}
		if tmpl != tmplStatic && tmpl != tmplContentList && tmpl != tmplDetail && tmpl != tmplBlank {
			return ErrorResult("unknown template: " + string(tmpl) + " (static|content-list|detail|blank)"), nil
		}
		content, err = s.renderPageTemplate(tmpl, rel, args)
		if err != nil {
			return ErrorResult(err.Error()), nil
		}
	}

	// The content entry (content-backed templates) lands next to the page.
	var entryRel string
	var entryContent string
	if !argBool(args, "withEntry", false) {
		if m := argStringMap(args, "contentEntry"); m != nil {
			col := argString(args, "collection")
			if col == "" {
				col = s.inferCollectionFromPage(route, s.root)
			}
			entryRel, entryContent, err = s.renderContentEntry(col, m)
			if err != nil {
				return ErrorResult(err.Error()), nil
			}
		}
	}

	// _layout.tsx depends on the layout option.
	var layoutRel, layoutContent string
	if argBool(args, "withLayout", false) {
		if _, statErr := os.Stat(filepath.Join(s.cfg.PagesDir, "_layout.tsx")); statErr != nil {
			layoutRel = "_layout.tsx"
			layoutContent = defaultLayoutTemplate()
		}
	}

	// Compose the full diff for dry-run; report what would happen.
	var reports []string
	reports = append(reports, "diff --git a/"+filepath.ToSlash(rel)+" b/"+filepath.ToSlash(rel))
	reports = append(reports, unifiedDiff(filepath.ToSlash(rel), "", content))
	if entryRel != "" {
		reports = append(reports, "diff --git "+filepath.ToSlash(entryRel))
		reports = append(reports, unifiedDiff(filepath.ToSlash(entryRel), "", entryContent))
	}
	if layoutRel != "" {
		reports = append(reports, "diff --git "+filepath.ToSlash(layoutRel))
		reports = append(reports, unifiedDiff(filepath.ToSlash(layoutRel), "", layoutContent))
	}

	if !apply {
		body := strings.Join(reports, "\n")
		return TextResult("Dry run (apply: true to write). Would create:\n\n" + body), nil
	}

	if _, err := os.Stat(abs); err == nil {
		return ErrorResult("page already exists: " + rel), nil
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0755); err != nil {
		return ErrorResult("creating directory: " + err.Error()), nil
	}
	if err := os.WriteFile(abs, []byte(content), 0644); err != nil {
		return ErrorResult("writing page: " + err.Error()), nil
	}
	created := []string{rel}
	if entryRel != "" {
		eabs := filepath.Join(s.root, filepath.FromSlash(entryRel))
		if _, err := os.Stat(eabs); err == nil {
			return ErrorResult("content entry already exists: " + entryRel), nil
		}
		if err := os.MkdirAll(filepath.Dir(eabs), 0755); err != nil {
			return ErrorResult("creating entry dir: " + err.Error()), nil
		}
		if err := os.WriteFile(eabs, []byte(entryContent), 0644); err != nil {
			return ErrorResult("writing content entry: " + err.Error()), nil
		}
		created = append(created, entryRel)
	}
	if layoutRel != "" {
		labs := filepath.Join(s.cfg.PagesDir, "_layout.tsx")
		if err := os.WriteFile(labs, []byte(layoutContent), 0644); err != nil {
			return ErrorResult("writing layout: " + err.Error()), nil
		}
		created = append(created, layoutRel)
	}

	// Regenerate route/content types so the new page is typed immediately.
	if errs := build.GenerateTypes(s.root, s.cfg); len(errs) > 0 {
		msgs := make([]string, 0, len(errs))
		for _, e := range errs {
			msgs = append(msgs, e.Error())
		}
		return ErrorResult("created files but type generation reported:\n" + strings.Join(msgs, "\n")), nil
	}

	return TextResult("Created " + strings.Join(created, ", ") + "\n\n" + strings.Join(reports, "\n")), nil
}

// renderPageTemplate produces a page's source for a template.
func (s *Service) renderPageTemplate(tmpl pageTemplate, rel string, args map[string]any) (string, error) {
	title := argString(args, "title")
	if title == "" {
		title = humanizeRoute(strings.TrimSuffix(strings.TrimPrefix(rel, "/"), ".tsx"))
		if title == "" {
			title = "Home"
		}
	}
	component := pageComponentName(rel)

	var b strings.Builder
	switch tmpl {
	case tmplBlank:
		b.WriteString("export default function " + component + "() {\n")
		b.WriteString("  return (\n    <main>\n      <p>Add your page content here.</p>\n    </main>\n  );\n}\n")
	case tmplStatic:
		b.WriteString("export default function " + component + "() {\n")
		b.WriteString("  return (\n")
		b.WriteString("    <main>\n")
		b.WriteString("      <Head>\n")
		fmt.Fprintf(&b, "        <title>%s</title>\n", title)
		b.WriteString("      </Head>\n")
		fmt.Fprintf(&b, "      <h1>%s</h1>\n", title)
		b.WriteString("      <p>Write about this page.</p>\n")
		b.WriteString("    </main>\n")
		b.WriteString("  );\n")
		b.WriteString("}\n")
	case tmplContentList, tmplDetail:
		col := argString(args, "collection")
		if col == "" {
			col = s.inferCollectionFromPage("/x", s.root)
			if col == "" {
				return "", fmt.Errorf("the %s template needs a collection (use collection: \"name\")", tmpl)
			}
		}
		if tmpl == tmplContentList {
			b.WriteString("import { getCollection } from 'krate/content';\n\n")
			fmt.Fprintf(&b, "const posts = getCollection(%q);\n\n", col)
			b.WriteString("export default function " + component + "() {\n")
			b.WriteString("  return (\n    <main>\n")
			fmt.Fprintf(&b, "      <h1>%s</h1>\n", title)
			b.WriteString("      <ul>\n")
			b.WriteString("        {posts.map((post) => (\n")
			b.WriteString("          <li>\n")
			b.WriteString("            <Link href={")
			fmt.Fprintf(&b, "`/%s/${post.slug}`", strings.Trim(routeFromRel(rel), "/"))
			b.WriteString("}>{post.data.title}</Link>\n")
			b.WriteString("          </li>\n")
			b.WriteString("        ))}\n")
			b.WriteString("      </ul>\n")
			b.WriteString("    </main>\n")
			b.WriteString("  );\n")
			b.WriteString("}\n")
		} else {
			b.WriteString("import { getCollection } from 'krate/content';\n\n")
			fmt.Fprintf(&b, "export function generateStaticParams() {\n  return getCollection(%q).map((p) => ({ params: { id: p.slug } }));\n}\n\n", col)
			b.WriteString("export default function " + component + "(props) {\n")
			b.WriteString("  const id = props.params?.id;\n")
			fmt.Fprintf(&b, "  const item = getCollection(%q).find((p) => p.slug === id);\n", col)
			b.WriteString("\n  if (!item) return <main><h1>Not found</h1></main>;\n\n")
			b.WriteString("  return (\n    <main>\n      <Head>\n        <title>{item.data.title}</title>\n      </Head>\n      <h1>{item.data.title}</h1>\n      <div class=\"body\" dangerouslySetInnerHTML={{ __html: item.html }}></div>\n    </main>\n  );\n}\n")
		}
	}
	return b.String(), nil
}

// renderContentEntry renders a markdown entry with YAML-ish frontmatter for a
// collection. m may contain a "slug" and any other fields as frontmatter.
func (s *Service) renderContentEntry(col string, m map[string]any) (rel, content string, err error) {
	if col == "" {
		return "", "", fmt.Errorf("contentEntry requires a collection")
	}
	slug, _ := m["slug"].(string)
	if slug == "" {
		return "", "", fmt.Errorf("contentEntry requires a slug")
	}
	dir := s.collectionDir(col)
	if dir == "" {
		return "", "", fmt.Errorf("unknown collection %q — configure it under content: in krate.config.ts or add a plugin that contributes it", col)
	}
	rel = filepath.ToSlash(filepath.Join(dir, slug+".md"))
	return rel, renderMarkdownEntry(m, "Content for **"+slug+"**."), nil
}

func (s *Service) toolEditAST(ctx context.Context, args map[string]any) (ToolResult, *rpcError) {
	rawAST, rerr := requireString(args, "ast")
	if rerr != nil {
		return ToolResult{}, rerr
	}
	apply := argBool(args, "apply", false)

	target := argString(args, "route")
	if target == "" {
		target = argString(args, "source")
	}
	if target == "" {
		return ErrorResult("provide route or source"), nil
	}

	src, rel, err := s.resolveSource(target)
	if err != nil {
		return ErrorResult(err.Error()), nil
	}

	original, err := os.ReadFile(src)
	if err != nil {
		return ErrorResult("reading page: " + err.Error()), nil
	}

	// The AST printer cannot reproduce TypeScript constructs the parser drops
	// (interfaces, type aliases, annotations). Refuse rather than silently
	// stripping them.
	if _, _, dropped := astprint.Parse(string(original)); dropped > 0 {
		return ErrorResult("refusing to edit: " + rel + " contains TypeScript type syntax (interfaces, aliases, or annotations) that the AST cannot round-trip. Use edit_page to edit the source directly."), nil
	}

	prog, err := astjson.DecodeProgram([]byte(rawAST))
	if err != nil {
		return ErrorResult("invalid AST document: " + err.Error()), nil
	}
	out := astprint.Print(prog)

	// Validate that the printed source reparses cleanly before touching disk.
	if _, perrs, _ := astprint.Parse(out); len(perrs) > 0 {
		msgs := make([]string, 0, len(perrs))
		for _, e := range perrs {
			msgs = append(msgs, e.Error())
		}
		return ErrorResult("edited AST does not produce valid source:\n" + strings.Join(msgs, "\n")), nil
	}
	// Preserve the file's original line ending.
	if strings.Contains(string(original), "\r\n") {
		out = strings.ReplaceAll(out, "\n", "\r\n")
	}

	diff := unifiedDiff(rel, string(original), out)
	if !apply {
		return TextResult(diff), nil
	}
	if err := os.WriteFile(src, []byte(out), 0644); err != nil {
		return ErrorResult("writing page: " + err.Error()), nil
	}
	return TextResult("Updated " + rel + "\n\n" + diff), nil
}

// toolEditPage edits a page or any project file directly (no AST required).
// Two modes: a full-file replace via content, or a targeted find+replace. The
// edited source is re-parsed before writing so broken files are never written.
func (s *Service) toolEditPage(ctx context.Context, args map[string]any) (ToolResult, *rpcError) {
	target := argString(args, "route")
	if target == "" {
		return ErrorResult("provide route (a page route like /about or a project-relative path like src/pages/about.tsx)"), nil
	}
	content := argString(args, "content")
	find := argString(args, "find")
	replace := argString(args, "replace")
	replaceAll := argBool(args, "replaceAll", false)
	apply := argBool(args, "apply", false)

	if content == "" && find == "" {
		return ErrorResult("provide content (full replace) or find+replace (targeted edit)"), nil
	}
	if content != "" && find != "" {
		return ErrorResult("provide either content or find, not both"), nil
	}

	abs, rel, err := s.resolveEditTarget(target)
	if err != nil {
		return ErrorResult(err.Error()), nil
	}

	original, exists := "", true
	if data, readErr := os.ReadFile(abs); readErr == nil {
		original = string(data)
	} else if os.IsNotExist(readErr) {
		if find != "" {
			return ErrorResult("cannot find+replace on a file that does not exist: " + rel), nil
		}
		exists = false
	} else {
		return ErrorResult("reading " + rel + ": " + readErr.Error()), nil
	}

	var out string
	if content != "" {
		out = content
	} else {
		matches := strings.Count(original, find)
		if matches == 0 {
			return ErrorResult("find text not found in " + rel), nil
		}
		if matches > 1 && !replaceAll {
			return ErrorResult(fmt.Sprintf("%q matches %d places in %s; narrow the find text or pass replaceAll: true", find, matches, rel)), nil
		}
		out = strings.ReplaceAll(original, find, replace)
	}

	// Preserve the file's existing line endings (new files default to LF).
	if exists && strings.Contains(original, "\r\n") && out != original {
		out = strings.ReplaceAll(out, "\n", "\r\n")
	}

	// Parse gate: editable source must still parse after the edit.
	if isEditablePageExt(rel) {
		if _, perrs, _ := astprint.Parse(out); len(perrs) > 0 {
			msgs := make([]string, 0, len(perrs))
			for _, e := range perrs {
				msgs = append(msgs, e.Error())
			}
			return ErrorResult("refusing to write " + rel + ": it would not parse after the edit:\n" + strings.Join(msgs, "\n")), nil
		}
	}

	diff := unifiedDiff(rel, original, out)
	if !apply {
		if !exists {
			return TextResult("Dry run (would create " + rel + "; apply: true to write).\n\n" + diff), nil
		}
		return TextResult("Dry run (apply: true to write).\n\n" + diff), nil
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0755); err != nil {
		return ErrorResult("creating the directory for " + rel + ": " + err.Error()), nil
	}
	if err := os.WriteFile(abs, []byte(out), 0644); err != nil {
		return ErrorResult("writing " + rel + ": " + err.Error()), nil
	}
	verb := "Updated"
	if !exists {
		verb = "Created"
	}
	return TextResult(verb + " " + rel + "\n\n" + diff), nil
}

// resolveEditTarget anchors a tool target: "/route" resolves through the page
// builder so dynamic and typed routes work; anything else is treated as a
// project-relative path. Absolute paths and traversal outside the root are
// rejected, and symlinked directories are resolved so a link cannot redirect a
// write outside the project.
func (s *Service) resolveEditTarget(target string) (abs, rel string, err error) {
	if target == "" {
		return "", "", fmt.Errorf("empty route")
	}
	if filepath.IsAbs(target) {
		return "", "", fmt.Errorf("invalid path: %s", target)
	}
	if strings.HasPrefix(target, "/") {
		abs, rel, err = s.resolveSource(target)
		if err != nil {
			return "", "", err
		}
		if s.policy != nil && !s.policy.writable(rel) {
			return "", "", fmt.Errorf("refusing to edit %s: it %s", rel, s.policy.deniedReason(rel))
		}
		return abs, rel, nil
	}
	abs, rel, err = s.resolveProjectPath(target)
	if err != nil {
		return "", "", err
	}
	if s.policy != nil && !s.policy.writable(rel) {
		return "", "", fmt.Errorf("refusing to edit %s: it %s", rel, s.policy.deniedReason(rel))
	}
	return abs, rel, nil
}

// resolveProjectPath turns a project-relative target into an absolute path and
// slash rel, rejecting absolute paths, NUL bytes, and symlink escapes. It does
// not consult the write policy (read tools still call denied separately).
func (s *Service) resolveProjectPath(target string) (abs, rel string, err error) {
	if target == "" {
		return "", "", fmt.Errorf("empty path")
	}
	if filepath.IsAbs(target) {
		return "", "", fmt.Errorf("invalid path: %s", target)
	}
	clean := filepath.ToSlash(target)
	if strings.Contains(clean, "\x00") {
		return "", "", fmt.Errorf("invalid path: %s", target)
	}
	abs, err = pluginapi.SafeJoin(s.root, clean)
	if err != nil {
		return "", "", fmt.Errorf("invalid path (outside the project): %s", target)
	}
	pr, relErr := filepath.Rel(s.root, abs)
	if relErr != nil {
		return "", "", fmt.Errorf("invalid path (outside the project): %s", target)
	}
	return abs, filepath.ToSlash(pr), nil
}

// toolReadContent reads content-collection entries. Without a slug it lists
// every entry (slug, project path, frontmatter); with a slug it returns the
// raw file, parsed frontmatter, markdown body, and rendered HTML for .md.
func (s *Service) toolReadContent(ctx context.Context, args map[string]any) (ToolResult, *rpcError) {
	raw := argString(args, "collection")
	if raw == "" {
		return ErrorResult("provide collection"), nil
	}
	col := s.collectionCanonical(raw)
	if col == "" {
		return ErrorResult("no collection named " + strconv.Quote(raw) + " — configure it under content: in krate.config.ts or add a plugin that contributes it"), nil
	}
	dir := s.collectionDir(col)
	absDir := filepath.Join(s.root, filepath.FromSlash(dir))
	slug := argString(args, "slug")

	if slug == "" {
		files, err := walkContentFiles(absDir)
		if err != nil {
			return ErrorResult("reading collection " + strconv.Quote(col) + ": " + err.Error()), nil
		}
		entries := make([]any, 0, len(files))
		for _, f := range files {
			entry := map[string]any{
				"slug": strings.TrimSuffix(f, filepath.Ext(f)),
				"path": filepath.ToSlash(filepath.Join(dir, f)),
			}
			if raw, e := os.ReadFile(filepath.Join(absDir, f)); e == nil {
				if d, _ := frontmatter.Parse(string(raw)); len(d) > 0 {
					entry["data"] = d
				}
			}
			entries = append(entries, entry)
		}
		return JSONResult(map[string]any{"collection": col, "entries": entries})
	}

	candidate := strings.TrimSuffix(slug, filepath.Ext(slug))
	relFile := candidate + ".md"
	fileData, readErr := os.ReadFile(filepath.Join(absDir, filepath.FromSlash(relFile)))
	if readErr != nil {
		relFile = candidate + ".mdx"
		fileData, readErr = os.ReadFile(filepath.Join(absDir, filepath.FromSlash(relFile)))
	}
	if readErr != nil {
		return ErrorResult("no entry " + strconv.Quote(slug) + " in collection " + strconv.Quote(col)), nil
	}
	text := string(fileData)
	data, body := frontmatter.Parse(text)
	if data == nil {
		data = map[string]any{}
	}
	result := map[string]any{
		"collection": col,
		"slug":       candidate,
		"path":       filepath.ToSlash(filepath.Join(dir, relFile)),
		"content":    text,
		"data":       data,
		"body":       body,
	}
	if strings.EqualFold(filepath.Ext(relFile), ".md") {
		result["html"] = markdown.RenderToHTML(body, s.cfg.Markdown)
	}
	return JSONResult(result)
}

// toolCreateContent creates a new entry in a content collection. Frontmatter is
// validated against the collection schema; existing entries and unsafe slugs
// are refused. Defaults to a dry-run unified diff.
func (s *Service) toolCreateContent(ctx context.Context, args map[string]any) (ToolResult, *rpcError) {
	raw := argString(args, "collection")
	if raw == "" {
		return ErrorResult("provide collection"), nil
	}
	col := s.collectionCanonical(raw)
	if col == "" {
		return ErrorResult("no collection named " + strconv.Quote(raw) + " — configure it under content: in krate.config.ts or add a plugin that contributes it"), nil
	}
	slug := argString(args, "slug")
	if slug == "" {
		return ErrorResult("provide slug"), nil
	}
	if err := validateContentSlug(slug); err != nil {
		return ErrorResult(err.Error()), nil
	}
	apply := argBool(args, "apply", false)

	dir := s.collectionDir(col)
	if dir == "" {
		return ErrorResult("no collection named " + strconv.Quote(col)), nil
	}
	clean := strings.TrimSuffix(slug, filepath.Ext(slug))
	rel := filepath.ToSlash(filepath.Join(dir, clean+".md"))
	abs := filepath.Join(s.root, filepath.FromSlash(rel))
	if _, err := os.Stat(abs); err == nil {
		return ErrorResult("entry already exists: " + rel), nil
	}

	cfg := s.contentConfig().Collections[col]
	if errs := content.Validate(cfg.Schema, argStringMap(args, "data")); len(errs) > 0 {
		msgs := make([]string, 0, len(errs))
		for _, e := range errs {
			msgs = append(msgs, e.Error())
		}
		return ErrorResult("frontmatter does not satisfy the " + col + " schema:\n" + strings.Join(msgs, "\n")), nil
	}
	body := argString(args, "body")
	out := renderMarkdownEntry(argStringMap(args, "data"), body)

	diff := unifiedDiff(rel, "", out)
	if !apply {
		return TextResult("Dry run (apply: true to write). Would create:\n\n" + diff), nil
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0755); err != nil {
		return ErrorResult("creating the directory for " + rel + ": " + err.Error()), nil
	}
	if err := os.WriteFile(abs, []byte(out), 0644); err != nil {
		return ErrorResult("writing " + rel + ": " + err.Error()), nil
	}
	return TextResult("Created " + rel + "\n\n" + diff), nil
}

// toolEditContent edits an existing content entry. Three modes: full-file
// replace via content, frontmatter rewrite via data (body preserved unless
// body is given), or a targeted find+replace. The result is re-parsed and
// validated against the collection schema before any write.
func (s *Service) toolEditContent(ctx context.Context, args map[string]any) (ToolResult, *rpcError) {
	raw := argString(args, "collection")
	if raw == "" {
		return ErrorResult("provide collection"), nil
	}
	col := s.collectionCanonical(raw)
	if col == "" {
		return ErrorResult("no collection named " + strconv.Quote(raw) + " — configure it under content: in krate.config.ts or add a plugin that contributes it"), nil
	}
	slug := argString(args, "slug")
	if slug == "" {
		return ErrorResult("provide slug"), nil
	}
	if err := validateContentSlug(slug); err != nil {
		return ErrorResult(err.Error()), nil
	}
	apply := argBool(args, "apply", false)

	dir := s.collectionDir(col)
	if dir == "" {
		return ErrorResult("no collection named " + strconv.Quote(col)), nil
	}
	clean := strings.TrimSuffix(slug, filepath.Ext(slug))
	absDir := filepath.Join(s.root, filepath.FromSlash(dir))
	relFile := clean + ".md"
	fileData, readErr := os.ReadFile(filepath.Join(absDir, filepath.FromSlash(relFile)))
	if readErr != nil {
		relFile = clean + ".mdx"
		fileData, readErr = os.ReadFile(filepath.Join(absDir, filepath.FromSlash(relFile)))
	}
	if readErr != nil {
		return ErrorResult("no entry " + strconv.Quote(slug) + " in collection " + strconv.Quote(col)), nil
	}
	original := string(fileData)
	projPath := filepath.ToSlash(filepath.Join(dir, relFile))

	full := argString(args, "content")
	data := argStringMap(args, "data")
	body := argString(args, "body")
	find := argString(args, "find")
	replace := argString(args, "replace")
	replaceAll := argBool(args, "replaceAll", false)

	modes := 0
	if full != "" {
		modes++
	}
	if data != nil {
		modes++
	}
	if find != "" {
		modes++
	}
	if modes == 0 {
		return ErrorResult("provide content (full replace), data (frontmatter rewrite), or find+replace (targeted edit)"), nil
	}
	if modes > 1 {
		return ErrorResult("provide only one of content, data, or find+replace"), nil
	}

	var out string
	switch {
	case full != "":
		out = full
	case find != "":
		matches := strings.Count(original, find)
		if matches == 0 {
			return ErrorResult("find text not found in " + projPath), nil
		}
		if matches > 1 && !replaceAll {
			return ErrorResult(fmt.Sprintf("%q matches %d places in %s; narrow the find text or pass replaceAll: true", find, matches, projPath)), nil
		}
		out = strings.ReplaceAll(original, find, replace)
	default:
		exData, exBody := frontmatter.Parse(original)
		if exData == nil {
			exData = map[string]any{}
		}
		for k, v := range data {
			exData[k] = v
		}
		if body != "" {
			exBody = body
		}
		out = renderMarkdownEntry(exData, exBody)
	}

	// Re-parse and validate the resulting frontmatter against the schema.
	newData, _ := frontmatter.Parse(out)
	if newData == nil {
		newData = map[string]any{}
	}
	cfg := s.contentConfig().Collections[col]
	if errs := content.Validate(cfg.Schema, newData); len(errs) > 0 {
		msgs := make([]string, 0, len(errs))
		for _, e := range errs {
			msgs = append(msgs, e.Error())
		}
		return ErrorResult("refusing to edit " + projPath + ": frontmatter would not satisfy the " + col + " schema:\n" + strings.Join(msgs, "\n")), nil
	}

	// Preserve the file's original line ending.
	if strings.Contains(original, "\r\n") && out != original {
		out = strings.ReplaceAll(out, "\n", "\r\n")
	}

	diff := unifiedDiff(projPath, original, out)
	if !apply {
		return TextResult("Dry run (apply: true to write).\n\n" + diff), nil
	}
	if err := os.WriteFile(filepath.Join(absDir, filepath.FromSlash(relFile)), []byte(out), 0644); err != nil {
		return ErrorResult("writing " + projPath + ": " + err.Error()), nil
	}
	return TextResult("Updated " + projPath + "\n\n" + diff), nil
}

// validateContentSlug rejects slugs that would escape the collection directory
// or resolve to the collection root itself.
func validateContentSlug(slug string) error {
	clean := filepath.ToSlash(strings.Trim(strings.TrimSpace(slug), "/"))
	if clean == "" || clean == "." {
		return fmt.Errorf("invalid slug %q", slug)
	}
	if filepath.IsAbs(filepath.FromSlash(clean)) {
		return fmt.Errorf("invalid slug %q: must be relative", slug)
	}
	for _, seg := range strings.Split(clean, "/") {
		if seg == ".." || seg == "" || strings.ContainsAny(seg, `\`) {
			return fmt.Errorf("invalid slug %q", slug)
		}
	}
	return nil
}

// ── resources ───────────────────────────────────────────────────────────────

func (s *Service) readContentResource(ctx context.Context, uri string) (ResourceContents, *rpcError) {
	type entry struct {
		Name   string         `json:"name"`
		Dir    string         `json:"dir,omitempty"`
		Fields map[string]any `json:"fields,omitempty"`
		Count  int            `json:"count"`
		Files  []string       `json:"files,omitempty"`
	}
	cfg := s.contentConfig()
	collections := map[string]any{}
	for name, c := range cfg.Collections {
		e := entry{Name: name, Dir: filepath.ToSlash(c.Dir)}
		if len(c.Schema) > 0 {
			fields := map[string]any{}
			for fname, f := range c.Schema {
				fields[fname] = map[string]any{"type": string(f.Type), "required": f.Required}
			}
			e.Fields = fields
		}
		if abs := filepath.Join(s.root, c.Dir); abs != "" {
			if rel, _ := filepath.Rel(s.root, abs); rel == ".." || filepath.IsAbs(rel) {
				continue
			}
			if files, err := listContentFiles(abs); err == nil {
				e.Count = len(files)
				e.Files = files
			}
		}
		cols := collections
		cols[name] = e
		collections = cols
	}
	return jsonResource(uri, collections)
}

func (s *Service) readManifestResource(ctx context.Context, uri string) (ResourceContents, *rpcError) {
	data, err := os.ReadFile(filepath.Join(s.cfg.OutDir, "manifest.json"))
	if err != nil {
		return jsonResource(uri, map[string]any{"built": false})
	}
	var v any
	if err := json.Unmarshal(data, &v); err != nil {
		return ResourceContents{}, Errorf("parsing manifest: %v", err)
	}
	return jsonResource(uri, v)
}

// configView is a curated view of the resolved config: relative paths (never
// absolute machine paths), no env values, and only agent-relevant settings.
type configView struct {
	Entry             string                    `json:"entry"`
	OutDir            string                    `json:"outDir"`
	PagesDir          string                    `json:"pagesDir"`
	PublicDir         string                    `json:"publicDir"`
	Minify            bool                      `json:"minify"`
	Sourcemap         bool                      `json:"sourcemap"`
	Output            string                    `json:"output,omitempty"`
	DevServer         map[string]any            `json:"devServer,omitempty"`
	ServerComponents  []string                  `json:"serverComponents,omitempty"`
	RuntimeComponents []string                  `json:"runtimeComponents,omitempty"`
	ServerDirs        []string                  `json:"serverDirs,omitempty"`
	RuntimeDirs       []string                  `json:"runtimeDirs,omitempty"`
	Redirects         []config.Redirect         `json:"redirects,omitempty"`
	Rewrites          []config.Rewrite          `json:"rewrites,omitempty"`
	SEO               map[string]any            `json:"seo,omitempty"`
	Robots            map[string]any            `json:"robots,omitempty"`
	Plugins           []map[string]any          `json:"plugins,omitempty"`
	Content           map[string]map[string]any `json:"content,omitempty"`
	Checks            map[string]any            `json:"checks,omitempty"`
	PathAliases       []map[string]any          `json:"pathAliases,omitempty"`
}

func (s *Service) readConfigResource(ctx context.Context, uri string) (ResourceContents, *rpcError) {
	rel := func(p string) string {
		if r, err := filepath.Rel(s.root, p); err == nil {
			return filepath.ToSlash(r)
		}
		return filepath.ToSlash(p)
	}
	c := s.cfg
	v := configView{
		Entry:             rel(c.Entry),
		OutDir:            rel(c.OutDir),
		PagesDir:          rel(c.PagesDir),
		PublicDir:         rel(c.PublicDir),
		Minify:            c.Minify,
		Sourcemap:         c.Sourcemap,
		Output:            c.Output,
		ServerComponents:  c.ServerComponents,
		RuntimeComponents: c.RuntimeComponents,
		ServerDirs:        c.ServerDirs,
		RuntimeDirs:       c.RuntimeDirs,
		Redirects:         c.Redirects,
		Rewrites:          c.Rewrites,
		Checks:            c.Checks,
	}
	if c.DevServer.Port != 0 || c.DevServer.Open {
		v.DevServer = map[string]any{"port": c.DevServer.Port, "open": c.DevServer.Open}
	}
	if c.SEO.BaseURL != "" || c.SEO.SiteName != "" || c.SEO.Description != "" || c.SEO.Image != "" {
		v.SEO = map[string]any{
			"baseUrl":     c.SEO.BaseURL,
			"siteName":    c.SEO.SiteName,
			"description": c.SEO.Description,
			"image":       c.SEO.Image,
		}
	}
	if c.Robots.Allow != "" || c.Robots.Disallow != "" || c.Robots.Sitemap != "" {
		v.Robots = map[string]any{
			"allow":    c.Robots.Allow,
			"disallow": c.Robots.Disallow,
			"sitemap":  c.Robots.Sitemap,
		}
	}
	for _, p := range c.Plugins {
		v.Plugins = append(v.Plugins, map[string]any{
			"name":    p.Name,
			"module":  p.Module,
			"order":   p.Order,
			"options": p.Options,
		})
	}
	ccfg := s.contentConfig()
	if len(ccfg.Collections) > 0 {
		v.Content = map[string]map[string]any{}
		for name, cc := range ccfg.Collections {
			info := map[string]any{"dir": filepath.ToSlash(cc.Dir)}
			if len(cc.Schema) > 0 {
				fields := map[string]any{}
				for fname, f := range cc.Schema {
					fields[fname] = map[string]any{"type": string(f.Type), "required": f.Required}
				}
				info["schema"] = fields
			}
			v.Content[name] = info
		}
	}
	for _, a := range c.PathAliases {
		if len(a.Targets) > 0 {
			v.PathAliases = append(v.PathAliases, map[string]any{
				"prefix":  a.Prefix,
				"targets": a.Targets,
			})
		}
	}
	return jsonResource(uri, v)
}

func (s *Service) readPageTemplate(ctx context.Context, uri string, params map[string]string) (ResourceContents, *rpcError) {
	route := "/" + strings.TrimPrefix(params["route"], "/")
	if route == "/" {
		return ResourceContents{}, &rpcError{Code: codeInvalidParams, Message: "krate://page requires a route, e.g. krate://page/about"}
	}
	detail, err := s.builder.PageDetail(route)
	if err != nil {
		return ResourceContents{}, &rpcError{Code: codeInvalidParams, Message: err.Error()}
	}
	return jsonResource(uri, detail)
}

// readDocsTemplate returns an embedded Krate framework doc as raw markdown.
func (s *Service) readDocsTemplate(ctx context.Context, uri string, params map[string]string) (ResourceContents, *rpcError) {
	slug := strings.Trim(params["slug"], "/")
	doc, ok := kratedocs.Lookup(slug)
	if !ok {
		return ResourceContents{}, &rpcError{Code: codeInvalidParams, Message: "no framework doc " + strconv.Quote(slug) + "; use search_docs to find a slug"}
	}
	return ResourceContents{URI: uri, MIMEType: "text/markdown", Text: doc.Markdown}, nil
}

// ── completions ─────────────────────────────────────────────────────────────

func (s *Service) completeRoutes(ctx context.Context, _ string) ([]string, error) {
	routes, err := s.builder.RouteList()
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(routes))
	for _, r := range routes {
		out = append(out, r.Route)
	}
	return out, nil
}

func (s *Service) completeTemplates(ctx context.Context, _ string) ([]string, error) {
	return []string{"static", "content-list", "detail", "blank"}, nil
}

// completeDocSlugs suggests krate://docs/{slug} values from the embedded
// framework docs.
func (s *Service) completeDocSlugs(ctx context.Context, _ string) ([]string, error) {
	return kratedocs.Slugs(), nil
}

func (s *Service) completeCollections(ctx context.Context, _ string) ([]string, error) {
	out := make([]string, 0, len(s.contentConfig().Collections))
	for name := range s.contentConfig().Collections {
		out = append(out, name)
	}
	sort.Strings(out)
	return out, nil
}

// completeContentSlugs suggests entry slugs for content tools. Slug completion
// is only well-defined for a single effective collection; with several,
// nothing is suggested (the AI should list the collection to discover slugs).
func (s *Service) completeContentSlugs(ctx context.Context, _ string) ([]string, error) {
	cfg := s.contentConfig()
	if len(cfg.Collections) != 1 {
		return nil, nil
	}
	for _, col := range cfg.Collections {
		dir := col.Dir
		if dir == "" {
			return nil, nil
		}
		files, err := walkContentFiles(filepath.Join(s.root, filepath.FromSlash(dir)))
		if err != nil {
			return nil, err
		}
		out := make([]string, 0, len(files))
		for _, f := range files {
			out = append(out, strings.TrimSuffix(f, filepath.Ext(f)))
		}
		sort.Strings(out)
		return out, nil
	}
	return nil, nil
}

// ── prompts ─────────────────────────────────────────────────────────────────

func (s *Service) promptAddPage() *Prompt {
	return &Prompt{
		Name:        "add-page",
		Description: "Add a new page the Krate way: read an existing page for house style, then create_page with the chosen template and content.",
		Arguments: []PromptArgument{
			{Name: "route", Description: "Route to create, e.g. /pricing or /blog/[slug]", Required: true},
			{Name: "title", Description: "Page title for the heading and <title>"},
			{Name: "template", Description: "static, content-list, detail, or blank"},
			{Name: "collection", Description: "Content collection backing a content-list/detail page"},
			{Name: "withLayout", Description: "Create _layout.tsx if missing"},
			{Name: "apply", Description: "Write the page now (default false: show a diff first)"},
		},
		Messages: []PromptMessage{{
			Role:    "user",
			Content: TextContent("Create a new Krate page at {{route}} using the {{template}} template. First read_page an existing, similar page to match the project's conventions (components, Head/title usage, styling). Then call create_page with the prepared arguments. Unless {{apply}} is true, present the returned diff for approval before writing. After the page exists, run build and check to confirm it compiles and passes the site's quality gates."),
		}},
	}
}

func (s *Service) promptPublishContent() *Prompt {
	return &Prompt{
		Name:        "publish-content",
		Description: "Author a new entry into a typed content collection (with frontmatter) and wire it into a page.",
		Arguments: []PromptArgument{
			{Name: "collection", Description: "Collection name as configured under content: in krate.config.ts or contributed by a plugin (e.g. docs)", Required: true},
			{Name: "slug", Description: "Entry slug, e.g. hello-world", Required: true},
			{Name: "title", Description: "Entry title stored in frontmatter"},
			{Name: "fields", Description: "Additional frontmatter fields as a JSON object"},
			{Name: "apply", Description: "Write the entry now (default false: show a diff first)"},
		},
		Messages: []PromptMessage{{
			Role:    "user",
			Content: TextContent("Create a new entry in the {{collection}} content collection with slug {{slug}} and title {{title}}. Use create_content so the frontmatter is validated against the collection schema (read_content or krate://content first to see the schema and house style; add any complex fields under {{fields}}). Unless {{apply}} is true, present the diff before writing."),
		}},
	}
}

func (s *Service) promptFixChecks() *Prompt {
	return &Prompt{
		Name:        "fix-checks",
		Description: "Run the quality gates, then fix the worst a11y/SEO/perf findings via edit_page.",
		Arguments: []PromptArgument{
			{Name: "route", Description: "Scope attention to a single route"},
		},
		Messages: []PromptMessage{{
			Role:    "user",
			Content: TextContent("Run the quality gates with the check tool. For each finding (see the findings array), read_page the source of the offending route, then fix it with edit_page, keeping the diff minimal and in the project's style. Re-run check after applying to confirm the fix."),
		}},
	}
}

func (s *Service) promptExplore() *Prompt {
	return &Prompt{
		Name:        "explore",
		Description: "Summarize the project: routes, content collections, config, and outstanding checks.",
		Arguments: []PromptArgument{
			{Name: "route", Description: "Drill into one route"},
		},
		Messages: []PromptMessage{{
			Role:    "user",
			Content: TextContent("Explore the Krate project: call list_routes and read the krate://routes and krate://config resources to summarize the site structure, then run check to report quality status. If a route is given, read_page it and summarize its structure and rendering mode."),
		}},
	}
}

// ── helpers ─────────────────────────────────────────────────────────────────

// runBuild runs a full build with stdout captured so the JSON-RPC stream stays
// clean. A fresh Builder is used because BuildAll closes plugin subprocesses
// and is not safe to repeat on one instance. Serialized via runMu so builds and
// checks never overlap (they swap os.Stdout during capture).
func (s *Service) runBuild(ctx context.Context) (string, error) {
	// Respect cancellation before starting the expensive build.
	if err := ctx.Err(); err != nil {
		return "", err
	}
	s.runMu.Lock()
	defer s.runMu.Unlock()
	if err := ctx.Err(); err != nil {
		return "", err
	}
	b := newBuilder(Options{Root: s.root, Cfg: s.cfg, Env: s.env, Verbose: s.verbose})
	return captureStdout(func() error {
		return b.BuildAll()
	})
}

// resolveSource maps a route or source path to an absolute file and its
// project-relative path.
func (s *Service) resolveSource(target string) (abs, rel string, err error) {
	detail, derr := s.builder.PageDetail(target)
	if derr != nil {
		return "", "", derr
	}
	return detail.SourcePath, detail.Source, nil
}

func jsonResource(uri string, v any) (ResourceContents, *rpcError) {
	data, err := marshalJSON(v)
	if err != nil {
		return ResourceContents{}, Errorf("encoding resource: %v", err)
	}
	return ResourceContents{URI: uri, MIMEType: "application/json", Text: string(data)}, nil
}

// routeToPageFile maps a normalized route to a page filename under pagesDir.
func routeToPageFile(route string) (string, error) {
	if route == "" {
		return "", fmt.Errorf("empty route")
	}
	segs := strings.Split(route, "/")
	for _, seg := range segs {
		if seg == "" || seg == "." || seg == ".." {
			return "", fmt.Errorf("invalid route segment %q", seg)
		}
	}
	return route + ".tsx", nil
}

func defaultLayoutTemplate() string {
	return "import './global.css';\n\nexport default function Layout(children) {\n" +
		"  return (\n    <div class=\"layout\">\n" +
		"      <nav>\n        <Link href=\"/\" prefetch={false}>Home</Link>\n      </nav>\n" +
		"      <main>{children}</main>\n      <footer>Krate</footer>\n    </div>\n  );\n}\n"
}

// pageComponentName converts a page file path into a PascalCase component name.
func pageComponentName(rel string) string {
	base := strings.TrimSuffix(filepath.Base(strings.TrimSuffix(rel, ".tsx")), ".ts")
	// Dynamic segments like [slug] become Id.
	base = strings.ReplaceAll(base, "[", "")
	base = strings.ReplaceAll(base, "]", "")
	var b strings.Builder
	upper := true
	for _, r := range base {
		if r == '-' || r == '_' || r == '.' {
			upper = true
			continue
		}
		if upper {
			b.WriteString(strings.ToUpper(string(r)))
			upper = false
			continue
		}
		b.WriteRune(r)
	}
	if b.Len() == 0 {
		return "Page"
	}
	return b.String()
}

// humanizeRoute turns a route path into a display title.
func humanizeRoute(p string) string {
	p = strings.Trim(p, "/")
	var parts []string
	for _, seg := range strings.Split(p, "/") {
		if seg == "" {
			continue
		}
		seg = strings.ReplaceAll(seg, "-", " ")
		parts = append(parts, strings.ToUpper(seg[:1])+seg[1:])
	}
	return strings.Join(parts, " ")
}

// routeFromRel converts a page rel path into its route prefix (e.g. blog/[slug].tsx → /blog).
func routeFromRel(rel string) string {
	rel = strings.TrimPrefix(rel, "/")
	dir := filepath.Dir(filepath.FromSlash(rel))
	if dir == "." || dir == "" {
		return ""
	}
	return "/" + filepath.ToSlash(dir)
}

// inferCollectionFromPage guesses the content collection that backs a route by
// matching the route's directory against the effective collection dirs
// (configured collections merged with plugin-contributed ones).
func (s *Service) inferCollectionFromPage(route string, root string) string {
	segs := strings.Split(strings.Trim(route, "/"), "/")
	cfg := s.contentConfig()
	for _, name := range sortedContentNames(cfg) {
		dir := cfg.Collections[name].Dir
		for _, seg := range segs {
			if seg != "" && strings.Contains(dir, seg) {
				return name
			}
		}
	}
	return ""
}

func sortedContentNames(cfg *content.Config) []string {
	names := make([]string, 0, len(cfg.Collections))
	for name := range cfg.Collections {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// contentConfig returns the effective content configuration: `content:`
// collections from krate.config.ts merged with collections contributed by
// plugins (e.g. the docs plugin). Configured collections win on name
// collisions.
func (s *Service) contentConfig() *content.Config {
	cfg := content.ParseConfig(s.cfg.Content)
	if cfg == nil {
		cfg = &content.Config{Collections: map[string]content.Collection{}}
	}
	for name, col := range plugin.DefaultContributedCollections(s.cfg) {
		if _, ok := cfg.Collections[name]; !ok {
			cfg.Collections[name] = col
		}
	}
	return cfg
}

// renderMarkdownEntry builds a markdown file from frontmatter fields and a
// body string. A "slug" key in fields is excluded (it's derived from the
// filename).
func renderMarkdownEntry(fields map[string]any, body string) string {
	var b strings.Builder
	b.WriteString("---\n")
	keys := make([]string, 0, len(fields))
	for k := range fields {
		if k != "slug" {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	for _, k := range keys {
		switch v := fields[k].(type) {
		case string:
			b.WriteString(k + ": " + v + "\n")
		case []any:
			strs := make([]string, 0, len(v))
			for _, item := range v {
				strs = append(strs, fmt.Sprintf("%v", item))
			}
			b.WriteString(k + ": [" + strings.Join(strs, ", ") + "]\n")
		default:
			fmt.Fprintf(&b, "%s: %v\n", k, v)
		}
	}
	b.WriteString("---\n\n")
	if body != "" {
		b.WriteString(body)
		if !strings.HasSuffix(body, "\n") {
			b.WriteString("\n")
		}
	}
	return b.String()
}

func (s *Service) collectionDir(name string) string {
	col, ok := s.contentConfig().Collections[name]
	if !ok {
		return ""
	}
	return col.Dir
}

// collectionCanonical returns the effective collection name for the given
// label, or "" when no such collection exists.
func (s *Service) collectionCanonical(name string) string {
	if name == "" {
		return ""
	}
	if _, ok := s.contentConfig().Collections[name]; ok {
		return name
	}
	return ""
}

// isEditablePageExt reports whether an edit_page target should be re-parsed
// for validation before writing.
func isEditablePageExt(rel string) bool {
	l := strings.ToLower(rel)
	return strings.HasSuffix(l, ".ts") || strings.HasSuffix(l, ".tsx") ||
		strings.HasSuffix(l, ".js") || strings.HasSuffix(l, ".jsx")
}

// listContentFiles returns markdown entries under dir as dir-relative slugs.
// It delegates to walkContentFiles so nested entries (guides/index.md vs
// index.md) keep distinct slugs instead of colliding on the bare filename.
func listContentFiles(dir string) ([]string, error) {
	return walkContentFiles(dir)
}

// walkContentFiles returns markdown files under dir as slash-separated paths
// relative to dir, including subdirectories (so they double as entry slugs).
func walkContentFiles(dir string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if ext != ".md" && ext != ".mdx" {
			return nil
		}
		if relPath, relErr := filepath.Rel(dir, path); relErr == nil {
			files = append(files, filepath.ToSlash(relPath))
		}
		return nil
	})
	sort.Strings(files)
	return files, err
}

// excerpt returns a snippet of text around the earliest query term match. The
// window is snapped outward to word boundaries and ellipses are only added when
// text was actually dropped on that side, so a match at the start of the page
// has no leading "…".
func excerpt(text, query string, n int) string {
	if text == "" {
		return ""
	}
	lower := strings.ToLower(text)
	idx := -1
	// Find the earliest position of any query term (not any document word).
	for _, term := range strings.Fields(strings.ToLower(query)) {
		if i := strings.Index(lower, term); i >= 0 && (idx < 0 || i < idx) {
			idx = i
		}
	}
	if idx < 0 {
		if len(text) > n {
			return strings.TrimSpace(text[:n]) + "…"
		}
		return strings.TrimSpace(text)
	}
	start := idx - n/2
	if start < 0 {
		start = 0
	}
	// Snap outward to word boundaries: back to the start of the first word,
	// forward to the end of the last word. Neither may exclude the match.
	start = snapStart(text, start)
	end := start + n
	if end > len(text) {
		end = len(text)
	}
	end = snapEnd(text, end)
	out := strings.TrimSpace(text[start:end])
	if start > 0 {
		out = "…" + out
	}
	if end < len(text) {
		out += "…"
	}
	return out
}

// snapStart moves i backward to the beginning of the word containing i.
func snapStart(s string, i int) int {
	for i > 0 && !isSpaceByte(s[i-1]) {
		i--
	}
	return i
}

// snapEnd moves i forward to the end of the word containing position i-1.
func snapEnd(s string, i int) int {
	for i < len(s) && !isSpaceByte(s[i]) {
		i++
	}
	return i
}

func isSpaceByte(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\r'
}

// cleanDocBody returns a framework doc's plain text: rendered HTML stripped and
// unescaped, with a leading H1 that merely repeats the title removed.
func cleanDocBody(d kratedocs.Doc) string {
	text := html.UnescapeString(docs.StripHTMLTags(d.Page.Content))
	return stripLeadingTitle(text, d.Page.Title)
}

// stripLeadingTitle drops the first line when it equals title (case-insensitive
// and ignoring surrounding markup/`#`), which the rendered page repeats as its
// H1.
func stripLeadingTitle(body, title string) string {
	title = strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(title), "#"))
	if title == "" {
		return body
	}
	lines := strings.Split(body, "\n")
	if len(lines) == 0 {
		return body
	}
	first := strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(lines[0]), "#"))
	if !strings.EqualFold(first, title) {
		return body
	}
	return strings.TrimLeft(strings.Join(lines[1:], "\n"), "\n")
}
