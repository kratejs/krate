package build

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/kratejs/krate/packages/compiler/internal/content"
	"github.com/kratejs/krate/packages/compiler/internal/docs"
	"github.com/kratejs/krate/packages/compiler/internal/frontmatter"
	"github.com/kratejs/krate/packages/compiler/internal/fsutil"
	"github.com/kratejs/krate/packages/compiler/internal/markdown"
	"github.com/kratejs/krate/packages/compiler/internal/plugin"
)

// routeGenDir is where codegen'd virtual modules live (gitignored under
// .krate/). The bundler resolves `krate/content` to a file here.
const routeGenDir = ".krate/gen"

// contentTypeResult separates advisory failures (unreadable files or IO
// problems) from schema validation errors, which are real content bugs.
type contentTypeResult struct {
	Warnings   []error
	Validation []error
}

// ContentValidationError reports content entries that violate their
// collection schema. It fails the build so invalid content is never shipped.
type ContentValidationError struct {
	Errors []error
}

func (e *ContentValidationError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%d content validation error(s):", len(e.Errors))
	for _, err := range e.Errors {
		b.WriteString("\n  ")
		b.WriteString(err.Error())
	}
	b.WriteString("\n  hint: fix the frontmatter above, or relax the collection schema in krate.config.ts")
	return b.String()
}

// contentConfig returns the effective typed content config: the `content:`
// declarations from `krate.config.ts` (`defineContent({...})` or a plain
// object) merged with collections contributed by registered plugins (e.g. the
// docs plugin adds a "docs" collection behind its contentDir). Configured
// collections win on name collisions. Returns nil when there are none.
func (b *Builder) contentConfig() *content.Config {
	cfg := content.ParseConfig(b.Cfg.Content)
	if cfg == nil {
		cfg = &content.Config{Collections: map[string]content.Collection{}}
	}
	for name, col := range plugin.DefaultContributedCollections(b.Cfg) {
		if _, exist := cfg.Collections[name]; exist {
			continue
		}
		cfg.Collections[name] = col
	}
	if len(cfg.Collections) == 0 {
		return nil
	}
	return cfg
}

// prepareContent discovers and validates every collection, writes the generated
// declarations and the `krate/content` runtime module, and records the virtual
// import map on the builder. It must run before page builds so pages that
// `import ... from "krate/content"` resolve.
func (b *Builder) prepareContent() contentTypeResult {
	var res contentTypeResult

	cfg := b.contentConfig()
	if cfg == nil || len(cfg.Collections) == 0 {
		return res
	}

	userCols := content.ParseConfig(b.Cfg.Content)
	entries := map[string][]content.Entry{}
	imageAssets := map[string]string{}
	for name, col := range cfg.Collections {
		// Plugin-contributed collections (docs) render their pages themselves,
		// so static-HTML omissions would only be noise there.
		userDeclared := false
		if userCols != nil {
			_, userDeclared = userCols.Collections[name]
		}
		dir := col.Dir
		if !filepath.IsAbs(dir) {
			dir = filepath.Join(b.Root, filepath.FromSlash(dir))
		}
		found, walkErrs, badMD := discoverContentEntries(b.Root, dir, b.Cfg.Markdown, userDeclared, b.Cfg.BaseURLPath(), imageAssets)
		res.Warnings = append(res.Warnings, walkErrs...)
		res.Validation = append(res.Validation, badMD...)
		for _, e := range found {
			content.ApplyDefaults(col.Schema, e.Data)
			if schemaErrs := content.Validate(col.Schema, e.Data); len(schemaErrs) > 0 {
				for _, se := range schemaErrs {
					res.Validation = append(res.Validation, fmt.Errorf("%s: %s: %w", name, filepath.ToSlash(e.Path), se))
				}
			}
		}
		entries[name] = found
	}

	if len(imageAssets) > 0 {
		if err := b.writeAssetFiles(imageAssets); err != nil {
			res.Warnings = append(res.Warnings, fmt.Errorf("copying content images: %w", err))
		}
	}

	typesDir := filepath.Join(b.Root, filepath.FromSlash(routeTypesDir))
	if err := os.MkdirAll(typesDir, 0755); err != nil {
		res.Warnings = append(res.Warnings, fmt.Errorf("creating types dir: %w", err))
		return res
	}
	if err := fsutil.WriteFileIfChanged(filepath.Join(typesDir, "content.d.ts"), []byte(content.Generate(cfg, entries)), 0644); err != nil {
		res.Warnings = append(res.Warnings, fmt.Errorf("writing content.d.ts: %w", err))
	}
	// Ambient module declaration, placed beside the source so the project's
	// tsconfig include picks it up (it has no imports, so it is a script).
	if err := fsutil.WriteFileIfChanged(b.contentAmbientPath(), []byte(content.GenerateModuleDTS(cfg)), 0644); err != nil {
		res.Warnings = append(res.Warnings, fmt.Errorf("writing content ambient types: %w", err))
	}

	genDir := filepath.Join(b.Root, filepath.FromSlash(routeGenDir))
	if err := os.MkdirAll(genDir, 0755); err != nil {
		res.Warnings = append(res.Warnings, fmt.Errorf("creating gen dir: %w", err))
		return res
	}
	if err := fsutil.WriteFileIfChanged(b.contentModulePath(), []byte(content.GenerateModule(cfg, entries)), 0644); err != nil {
		res.Warnings = append(res.Warnings, fmt.Errorf("writing content module: %w", err))
	}

	b.contentMods = map[string]string{
		"krate/content":  b.contentModulePath(),
		"@krate/content": b.contentModulePath(),
	}
	b.contentCollections = entries
	b.contentModuleDTS = content.GenerateModuleDTS(cfg)
	return res
}

// contentModulePath returns the generated `krate/content` module path.
func (b *Builder) contentModulePath() string {
	return filepath.Join(b.Root, filepath.FromSlash(routeGenDir), "content.ts")
}

// contentAmbientPath returns the path of the generated ambient module
// declaration, placed beside the source like the krate-env.d.ts bridge so the
// default `include: ["src"]` picks it up.
func (b *Builder) contentAmbientPath() string {
	src := filepath.Join(b.Root, "src")
	if fi, err := os.Stat(src); err == nil && fi.IsDir() {
		return filepath.Join(src, "krate-content.d.ts")
	}
	return filepath.Join(b.Root, "krate-content.d.ts")
}

// discoverContentEntries walks a collection dir for .md/.mdx files and parses
// each one's frontmatter plus rendered body. Relative images are recorded in
// assets (source path to hashed site URL, under base) for the caller to copy.
// Hidden files and directories are skipped, and two files that map to the same
// slug are an error.
func discoverContentEntries(root, dir string, mdCfg markdown.Config, warnStatic bool, base string, assets map[string]string) (entries []content.Entry, errs, bad []error) {
	fi, err := os.Stat(dir)
	if err != nil || !fi.IsDir() {
		return nil, nil, nil // a declared collection with no dir yet is not an error
	}
	seenSlugs := map[string]string{}
	walkErr := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if strings.HasPrefix(d.Name(), ".") && path != dir {
			// Editor lock files (.#x.md), swap files and hidden folders.
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if ext != ".md" && ext != ".mdx" {
			return nil
		}
		rel, rerr := filepath.Rel(dir, path)
		if rerr != nil {
			return nil
		}
		slug := strings.TrimSuffix(filepath.ToSlash(rel), ext)
		if prev, dup := seenSlugs[slug]; dup {
			bad = append(bad, fmt.Errorf("%s: slug %q is also used by %s (hint: rename one file; entries are addressed by slug, so the second would be unreachable)",
				filepath.ToSlash(relToRoot(root, path)), slug, filepath.ToSlash(relToRoot(root, prev))))
			return nil
		}
		seenSlugs[slug] = path
		src, rerr := os.ReadFile(path)
		if rerr != nil {
			errs = append(errs, fmt.Errorf("reading %s: %w", path, rerr))
			return nil
		}
		raw := string(src)
		data, _ := frontmatter.Parse(raw)
		if data == nil {
			data = map[string]any{}
		}
		body := docs.StripFrontmatter(raw)
		projRel := path
		if pr, rerr := filepath.Rel(root, path); rerr == nil {
			projRel = pr
		}
		entryCfg := mdCfg
		entryCfg.ImageURL = markdown.LocalImageResolver(root, path, base, func(abs, siteURL string) {
			assets[abs] = siteURL
		})
		compiled := markdown.Compile(raw, entryCfg, markdown.Options{MDX: ext == ".mdx"})
		html := compiled.StaticHTML(entryCfg)
		diags := compiled.Diags
		if ext == ".mdx" && warnStatic {
			diags = append(diags, compiled.StaticWarnings()...)
		}
		for _, d := range diags {
			err := fmt.Errorf("%s:%d:%d: %s (hint: %s)", filepath.ToSlash(projRel), d.Line, d.Col, d.Msg, d.Hint)
			if d.Warning {
				errs = append(errs, err)
			} else {
				bad = append(bad, err)
			}
		}
		entries = append(entries, content.Entry{
			Slug: slug,
			Path: filepath.ToSlash(projRel),
			Body: body,
			HTML: html,
			Data: content.NormalizeData(data),
		})
		return nil
	})
	if walkErr != nil {
		errs = append(errs, fmt.Errorf("walking %s: %w", dir, walkErr))
	}
	return entries, errs, bad
}

func relToRoot(root, path string) string {
	if r, err := filepath.Rel(root, path); err == nil {
		return r
	}
	return path
}
