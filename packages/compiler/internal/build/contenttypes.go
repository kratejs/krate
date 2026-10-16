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

	entries := map[string][]content.Entry{}
	for name, col := range cfg.Collections {
		dir := col.Dir
		if !filepath.IsAbs(dir) {
			dir = filepath.Join(b.Root, filepath.FromSlash(dir))
		}
		found, walkErrs := discoverContentEntries(b.Root, dir, b.Cfg.Markdown)
		res.Warnings = append(res.Warnings, walkErrs...)
		for _, e := range found {
			if schemaErrs := content.Validate(col.Schema, e.Data); len(schemaErrs) > 0 {
				for _, se := range schemaErrs {
					res.Validation = append(res.Validation, fmt.Errorf("%s: %s: %w", name, filepath.ToSlash(e.Path), se))
				}
			}
		}
		entries[name] = found
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
// each one's frontmatter plus rendered body.
func discoverContentEntries(root, dir string, mdCfg markdown.Config) ([]content.Entry, []error) {
	var entries []content.Entry
	var errs []error
	fi, err := os.Stat(dir)
	if err != nil || !fi.IsDir() {
		return nil, nil // a declared collection with no dir yet is not an error
	}
	walkErr := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
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
		var html string
		if ext == ".mdx" {
			html, _ = docs.ParseMDX(raw, mdCfg)
		} else {
			html, _ = docs.ParseMD(raw, mdCfg)
		}
		projRel := path
		if pr, rerr := filepath.Rel(root, path); rerr == nil {
			projRel = pr
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
	return entries, errs
}
