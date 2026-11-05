package bundler

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/kratejs/krate/packages/compiler/internal/css"
	"github.com/kratejs/krate/packages/compiler/internal/diag"
	"github.com/kratejs/krate/packages/compiler/internal/resolver"
)

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// cssDep is a file a cached stylesheet was built from.
type cssDep struct {
	path  string
	mtime time.Time
	size  int64
	hash  string
}

// cssSheet collects what one stylesheet read while it was being built.
type cssSheet struct {
	b      *Bundler
	file   string
	assets map[string]string // asset source path -> hashed URL
	deps   map[string]bool   // every other file read
	diags  []diag.Diagnostic
	// lastRead is the most recently read imported file.
	lastRead string
}

func (cs *cssSheet) warn(msg string) {
	cs.diags = append(cs.diags, diag.Diagnostic{
		File:     cs.file,
		Message:  msg,
		Severity: diag.SeverityWarning,
	})
}

func (cs *cssSheet) read(path string) {
	cs.deps[path] = true
	cs.lastRead = path
}

// checkSyntax records a warning for every structural problem in the source of
// one stylesheet file.
func (cs *cssSheet) checkSyntax(file, src string) {
	if file == "" {
		file = cs.file
	}
	shown := file
	if rel, err := filepath.Rel(cs.b.root, file); err == nil && !strings.HasPrefix(rel, "..") {
		shown = filepath.ToSlash(rel)
	}
	for _, p := range css.CheckSyntax(src) {
		cs.diags = append(cs.diags, diag.Diagnostic{
			File:     shown,
			Line:     p.Line,
			Col:      p.Col,
			Message:  p.Message,
			Hint:     "fix the braces in this file; Krate closes the damage at the end of the file so later stylesheets are not affected",
			Severity: diag.SeverityWarning,
			Code:     diag.CodeCSSSyntax,
		})
	}
}

var schemeRe = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9+.-]*:`)

// isExternalCSSURL reports whether a CSS url() target is not a file path
// relative to the stylesheet: absolute URLs, any scheme, data URIs,
// protocol-relative and root-absolute paths, fragments and var().
func isExternalCSSURL(ref string) bool {
	if strings.HasPrefix(ref, "/") || strings.HasPrefix(ref, "#") {
		return true
	}
	if strings.HasPrefix(strings.ToLower(ref), "var(") {
		return true
	}
	return schemeRe.MatchString(ref)
}

// rewriteURLs resolves relative `url(...)` references in a stylesheet against
// the directory of the file the text came from, registers each asset in the
// bundle (content-hashed, served from /assets/) and rewrites the URL. External
// URLs, data URIs, fragments, `var(...)` and root-absolute paths are left
// untouched; a relative file that cannot be found is reported.
func (cs *cssSheet) rewriteURLs(cssText, dir string) string {
	b := cs.b
	cssText = css.AbsolutizeSourcePaths(cssText, dir)
	return css.RewriteURLs(cssText, func(raw string) (string, bool) {
		ref := strings.TrimSpace(raw)
		if ref == "" || isExternalCSSURL(ref) {
			return "", false
		}
		frag := ""
		if i := strings.IndexAny(ref, "?#"); i >= 0 {
			frag = ref[i:]
			ref = ref[:i]
		}
		if ref == "" {
			return "", false
		}
		abs := filepath.Clean(filepath.Join(dir, filepath.FromSlash(ref)))
		if !b.insideRootOrPackage(abs) {
			cs.warn(fmt.Sprintf("url(%s) points outside the project root and was left as written", raw))
			return "", false
		}
		if s := resolver.StatPath(abs); !s.Exists || s.IsDir {
			cs.warn(fmt.Sprintf("url(%s) not found relative to %s; left as written, so the browser resolves it against the built stylesheet", raw, dir))
			return "", false
		}
		url, ok := b.assets[abs]
		if !ok {
			data, err := os.ReadFile(abs)
			if err != nil {
				cs.warn(fmt.Sprintf("url(%s) could not be read: %v", raw, err))
				return "", false
			}
			base := filepath.Base(abs)
			ext := filepath.Ext(base)
			name := strings.TrimSuffix(base, ext)
			url = "/assets/" + name + "-" + hashBytes(data) + ext
			b.assets[abs] = url
		}
		cs.assets[abs] = url
		cs.deps[abs] = true
		return url + frag, true
	})
}

// insideRootOrPackage reports whether abs is inside the project root or in a
// node_modules directory (pnpm may place packages outside the project root).
func (b *Bundler) insideRootOrPackage(abs string) bool {
	if rel, err := filepath.Rel(b.root, abs); err == nil && rel != ".." &&
		!strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return true
	}
	return strings.Contains(filepath.ToSlash(abs), "/node_modules/")
}

// resolveCSSSpecifier resolves a bare @import specifier ("pkg/dist/x.css",
// "@scope/pkg") through node_modules. A package's "style" field is honoured
// for a bare package name; subpaths use package.json exports when they point
// at a stylesheet, else the literal path.
func (b *Bundler) resolveCSSSpecifier(spec, fromDir string) string {
	pkg, sub := splitPackageSpec(spec)
	if pkg == "" {
		return ""
	}
	if sub != "" {
		if p := resolver.NodeModule(fromDir, spec); strings.HasSuffix(strings.ToLower(p), ".css") {
			return p
		}
	}
	dir := fromDir
	if real, err := filepath.EvalSymlinks(fromDir); err == nil {
		dir = real
	}
	for {
		base := filepath.Join(dir, "node_modules", filepath.FromSlash(pkg))
		if s := resolver.StatPath(base); s.Exists && s.IsDir {
			if sub != "" {
				cand := filepath.Join(base, filepath.FromSlash(sub))
				for _, c := range []string{cand, cand + ".css"} {
					if s := resolver.StatPath(c); s.Exists && !s.IsDir {
						return c
					}
				}
				return ""
			}
			return packageStyleEntry(base)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

func splitPackageSpec(spec string) (pkg, sub string) {
	parts := strings.Split(spec, "/")
	if strings.HasPrefix(spec, "@") {
		if len(parts) < 2 {
			return "", ""
		}
		return parts[0] + "/" + parts[1], strings.Join(parts[2:], "/")
	}
	return parts[0], strings.Join(parts[1:], "/")
}

// packageStyleEntry returns a package's stylesheet entry: package.json "style",
// a "main" that is a stylesheet, or index.css.
func packageStyleEntry(pkgDir string) string {
	var pj struct {
		Style string `json:"style"`
		Main  string `json:"main"`
	}
	if data, err := os.ReadFile(filepath.Join(pkgDir, "package.json")); err == nil {
		_ = json.Unmarshal(data, &pj)
	}
	for _, f := range []string{pj.Style, pj.Main} {
		if f == "" || (f == pj.Main && !strings.HasSuffix(strings.ToLower(f), ".css")) {
			continue
		}
		if p := filepath.Join(pkgDir, filepath.FromSlash(f)); fileIsRegular(p) {
			return p
		}
	}
	if p := filepath.Join(pkgDir, "index.css"); fileIsRegular(p) {
		return p
	}
	return ""
}

func fileIsRegular(p string) bool {
	s := resolver.StatPath(p)
	return s.Exists && !s.IsDir
}

// snapshotCSSDeps records the current stamp of every dependency file.
func snapshotCSSDeps(paths map[string]bool) []cssDep {
	out := make([]cssDep, 0, len(paths))
	for _, p := range sortedKeys(paths) {
		s := resolver.StatPath(p)
		if !s.Exists || s.IsDir {
			continue
		}
		out = append(out, cssDep{path: p, mtime: s.Mtime, size: s.Size, hash: fileContentHash(p, s.Size)})
	}
	return out
}

// cssDepsCurrent reports whether every dependency still has its recorded stamp.
func cssDepsCurrent(deps []cssDep) bool {
	for _, d := range deps {
		s := resolver.StatPath(d.path)
		if !s.Exists || s.IsDir || !s.Mtime.Equal(d.mtime) || s.Size != d.size ||
			!stampMatches(d.path, d.mtime, d.size, d.hash) {
			return false
		}
	}
	return true
}

func (b *Bundler) resolveCSSModule(path, abs string, mtime time.Time, size int64, statOK bool) error {
	if statOK {
		if e, ok := b.cache.getCSS(abs, mtime, size); ok {
			// Composed modules come first so the composing sheet can override.
			for _, from := range e.composesFrom {
				if err := b.resolveModule(from, false); err != nil {
					return err
				}
			}
			b.css = append(b.css, e.css)
			if e.isModule {
				b.cssModules[abs] = &CSSModuleInfo{ResolvedPath: abs, ScopedCSS: e.css, Mappings: e.mapping}
			}
			for k, v := range e.assets {
				b.assets[k] = v
			}
			for _, d := range e.deps {
				b.cssDeps[d.path] = true
			}
			b.order = append(b.order, &Module{Path: path, IsCSS: true, Diagnostics: e.diags})
			return nil
		}
	}

	data, err := os.ReadFile(abs)
	if err != nil {
		return fmt.Errorf("reading %s: %w", path, err)
	}

	cs := &cssSheet{b: b, file: abs, assets: make(map[string]string), deps: make(map[string]bool)}
	dir := filepath.Dir(abs)
	// Rewrite this file's url() references first, then inline @imports
	// relative to THIS file's directory; every imported file has its own
	// url() references rewritten relative to itself before it is spliced in.
	cs.checkSyntax(abs, string(data))
	text := css.Balance(cs.rewriteURLs(string(data), dir))
	text = css.InlineImportsWith(text, dir, css.ImportOptions{
		Root:    b.root,
		Resolve: b.resolveCSSSpecifier,
		// An imported sheet is balanced before it is spliced in, so it cannot
		// swallow the rest of the importing sheet.
		RewriteURLs: func(c, d string) string {
			cs.checkSyntax(cs.lastRead, c)
			return css.Balance(cs.rewriteURLs(c, d))
		},
		OnRead:   cs.read,
		Warn:     cs.warn,
		Tailwind: b.twImports,
	})

	isModule := strings.Contains(path, ".module.")
	entry := &cssEntry{mtime: mtime, size: size, isModule: isModule, assets: cs.assets}
	if isModule {
		res := css.ProcessModuleWith(abs, text, css.ModuleOptions{
			Composed: func(spec string) (map[string]string, error) {
				return b.composedMapping(abs, spec, cs, entry)
			},
		})
		for _, w := range res.Warnings {
			cs.warn(w)
		}
		b.css = append(b.css, res.CSS)
		b.cssModules[abs] = &CSSModuleInfo{ResolvedPath: abs, ScopedCSS: res.CSS, Mappings: res.Mapping}
		entry.css = res.CSS
		entry.mapping = res.Mapping
	} else {
		b.css = append(b.css, text)
		entry.css = text
	}
	for p := range cs.deps {
		b.cssDeps[p] = true
	}
	entry.deps = snapshotCSSDeps(cs.deps)
	entry.diags = cs.diags
	if statOK {
		b.cache.putCSS(abs, entry)
	}
	b.order = append(b.order, &Module{Path: path, IsCSS: true, Diagnostics: cs.diags})
	return nil
}

// composedMapping loads the module named by `composes: x from "spec"` (adding
// its CSS to the bundle ahead of the composing sheet) and returns its mapping.
func (b *Bundler) composedMapping(from, spec string, cs *cssSheet, entry *cssEntry) (map[string]string, error) {
	resolved := b.resolveImportForModule(from, spec)
	if resolved == "" {
		return nil, fmt.Errorf("cannot resolve %q", spec)
	}
	resolved = filepath.Clean(resolved)
	if err := b.resolveModule(resolved, false); err != nil {
		return nil, err
	}
	info, ok := b.cssModules[resolved]
	if !ok {
		return nil, fmt.Errorf("%s is not a CSS module (or composes form a cycle)", spec)
	}
	cs.deps[resolved] = true
	entry.composesFrom = append(entry.composesFrom, resolved)
	return info.Mappings, nil
}

// CSSModuleMapping runs a CSS Module through the same pipeline the page bundler
// uses (imports, url() rewriting, scoping, composes) and returns its class
// mapping plus any warnings. SSR builds use it so server-rendered class names
// match the stylesheet the page links.
func CSSModuleMapping(root, abs string) (map[string]string, []diag.Diagnostic, error) {
	res, err := BuildStylesheet(root, abs)
	if err != nil {
		return nil, nil, err
	}
	if res.Mapping == nil {
		return nil, nil, fmt.Errorf("%s is not a CSS module", abs)
	}
	return res.Mapping, res.Warnings, nil
}

// StylesheetResult is a stylesheet built outside a page's module graph.
type StylesheetResult struct {
	CSS      string
	Mapping  map[string]string // set for CSS Modules
	Assets   map[string]string // asset source path -> hashed site URL
	Deps     []string          // every file read besides the stylesheet itself
	Warnings []diag.Diagnostic
}

// BuildStylesheet runs one stylesheet through the page pipeline (imports,
// url() rewriting, CSS Module scoping) for CSS reached from outside the
// bundler's module graph, such as compiled package islands.
func BuildStylesheet(root, abs string) (*StylesheetResult, error) {
	b := New(root)
	b.cssModules = make(map[string]*CSSModuleInfo)
	b.cssDeps = make(map[string]bool)
	abs = filepath.Clean(abs)
	if err := b.resolveModule(abs, false); err != nil {
		return nil, err
	}
	res := &StylesheetResult{
		CSS:    strings.Join(b.css, "\n"),
		Assets: b.assets,
		Deps:   sortedKeys(b.cssDeps),
	}
	if info, ok := b.cssModules[abs]; ok {
		res.Mapping = info.Mappings
	}
	for _, m := range b.order {
		res.Warnings = append(res.Warnings, m.Diagnostics...)
	}
	return res, nil
}
