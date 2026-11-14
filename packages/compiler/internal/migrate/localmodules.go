package migrate

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// KindModule is a project-local module (a component, helper or server file
// outside app/) reached from a migrated route. It is rewritten in place:
// Target equals Source, so Apply backs up the original and Rollback restores it.
const KindModule FileKind = "module"

var moduleExts = []string{".tsx", ".ts", ".jsx", ".js", ".mjs", ".mts"}

// anySpecifier matches the string of an import, export-from, dynamic import or
// require call, relative or not.
var anySpecifier = regexp.MustCompile(`(?m)(\bfrom\s*|\bimport\s*\(\s*|\bimport\s+|\brequire\s*\(\s*)(["'])([^"'\r\n]+)(["'])`)

var nextImportSpecifier = regexp.MustCompile(`(?m)(?:\bfrom\s*|\bimport\s*\(\s*|\bimport\s+|\brequire\s*\(\s*)["']next(?:/[^"']*)?["']`)

// localGraph walks the project-local modules reachable from migrated files.
type localGraph struct {
	rep     *Report
	root    string
	assets  map[string]string // app source path -> migrated target
	planned map[string]bool   // every source path already in the plan
	seen    map[string]bool
	queue   []string
	cfg     tsAliases
	bare    map[string]bool
	outside map[string]bool
	modules []PlannedFile
}

func newLocalGraph(rep *Report, assets map[string]string) *localGraph {
	g := &localGraph{rep: rep, root: rep.Root, assets: assets, planned: map[string]bool{}, seen: map[string]bool{}, bare: map[string]bool{}, outside: map[string]bool{}}
	for _, f := range rep.Files {
		if f.Source != "" {
			g.planned[filepath.ToSlash(f.Source)] = true
		}
	}
	g.cfg = loadTSAliases(rep.Root)
	return g
}

func skipModulePath(p string) bool {
	for _, seg := range strings.Split(p, "/") {
		switch seg {
		case "node_modules", ".next", ".krate", ".git", "dist":
			return true
		}
	}
	return false
}

func (g *localGraph) isFile(rel string) bool {
	if rel == "" || rel == "." || rel == ".." || strings.HasPrefix(rel, "../") || skipModulePath(rel) {
		return false
	}
	st, err := os.Stat(filepath.Join(g.root, filepath.FromSlash(rel)))
	return err == nil && st.Mode().IsRegular()
}

// findFile resolves an extensionless or index import base to a project file.
func (g *localGraph) findFile(base string) string {
	base = filepath.ToSlash(filepath.Clean(filepath.FromSlash(base)))
	if g.isFile(base) {
		return base
	}
	ext := filepath.Ext(base)
	if ext == ".js" || ext == ".jsx" || ext == ".mjs" {
		stem := strings.TrimSuffix(base, ext)
		for _, e := range []string{".ts", ".tsx", ".mts"} {
			if g.isFile(stem + e) {
				return stem + e
			}
		}
	}
	for _, e := range moduleExts {
		if g.isFile(base + e) {
			return base + e
		}
	}
	for _, e := range moduleExts {
		if g.isFile(base + "/index" + e) {
			return base + "/index" + e
		}
	}
	return ""
}

func isModuleSource(p string) bool {
	ext := strings.ToLower(filepath.Ext(p))
	for _, e := range moduleExts {
		if ext == e {
			return true
		}
	}
	return false
}

// register records a resolved project file and returns the path an import
// should point at. App files already copied under src/migrated map to their
// copy; other source files are queued to be rewritten in place.
func (g *localGraph) register(p string) (string, bool) {
	if t, ok := g.assets[p]; ok {
		return t, true
	}
	if g.planned[p] {
		return "", false
	}
	if isModuleSource(p) && !g.seen[p] {
		g.seen[p] = true
		g.queue = append(g.queue, p)
	}
	return p, true
}

// resolveRelative is the fallback for relative imports that are not app assets.
func (g *localGraph) resolveRelative(sourceDir, spec string) (string, bool) {
	base := filepath.ToSlash(filepath.Join(filepath.FromSlash(sourceDir), filepath.FromSlash(spec)))
	p := g.findFile(base)
	if p == "" {
		return "", false
	}
	return g.register(p)
}

// rewriteAlias queues modules imported through tsconfig paths and repoints
// alias imports of app files that were copied under src/migrated. Aliased
// modules outside app/ keep their specifier: they stay where they are.
func (g *localGraph) rewriteAlias(src, label, targetDir string) (string, []string) {
	var rewrites []string
	out := anySpecifier.ReplaceAllStringFunc(src, func(match string) string {
		sub := anySpecifier.FindStringSubmatch(match)
		prefix, quote, spec, suffix := sub[1], sub[2], sub[3], sub[4]
		if strings.HasPrefix(spec, ".") || strings.HasPrefix(spec, "/") || strings.Contains(spec, ":") || spec == "next" || strings.HasPrefix(spec, "next/") {
			return match
		}
		cands, outside := g.cfg.candidates(spec)
		for _, c := range cands {
			p := g.findFile(c)
			if p == "" {
				continue
			}
			to, ok := g.register(p)
			if !ok || to == p {
				return match
			}
			rel := relativeSpecifier(targetDir, to, spec)
			rewrites = append(rewrites, "import "+spec+" -> "+rel)
			return prefix + quote + rel + suffix
		}
		if outside && !g.outside[spec] {
			g.outside[spec] = true
			g.rep.Manual = append(g.rep.Manual, ManualItem{Source: label, Reason: "tsconfig path alias for " + spec + " resolves outside the project root, so its next/* imports were not rewritten", Hint: "migrate that package by hand"})
		}
		g.noteWorkspacePackage(spec, label)
		return match
	})
	return out, rewrites
}

// relativeSpecifier is the import path from a directory to a project file,
// without the extension unless the original specifier had one.
func relativeSpecifier(fromDir, to, origSpec string) string {
	rel, err := filepath.Rel(filepath.FromSlash(fromDir), filepath.FromSlash(to))
	if err != nil {
		return origSpec
	}
	rel = filepath.ToSlash(rel)
	if filepath.Ext(origSpec) == "" {
		switch strings.ToLower(filepath.Ext(rel)) {
		case ".tsx", ".ts", ".jsx", ".js", ".mjs", ".mts", ".mdx", ".md":
			rel = strings.TrimSuffix(rel, filepath.Ext(rel))
		}
	}
	if !strings.HasPrefix(rel, ".") {
		rel = "./" + rel
	}
	return rel
}

// noteWorkspacePackage reports a bare import that resolves to a symlinked
// workspace package whose sources import next/*. Those are outside the project
// and are never rewritten.
func (g *localGraph) noteWorkspacePackage(spec, label string) {
	parts := strings.Split(spec, "/")
	name := parts[0]
	if strings.HasPrefix(spec, "@") && len(parts) >= 2 {
		name = parts[0] + "/" + parts[1]
	}
	if g.bare[name] {
		return
	}
	g.bare[name] = true
	dir := g.root
	for {
		link := filepath.Join(dir, "node_modules", filepath.FromSlash(name))
		if st, err := os.Lstat(link); err == nil {
			if st.Mode()&os.ModeSymlink == 0 {
				return
			}
			real, err := filepath.EvalSymlinks(link)
			if err != nil || strings.Contains(filepath.ToSlash(real), "/node_modules/") {
				return
			}
			files := workspaceNextImports(real)
			if len(files) > 0 {
				shown := files
				more := ""
				if len(shown) > 3 {
					shown = shown[:3]
					more = ", ..."
				}
				g.rep.Manual = append(g.rep.Manual, ManualItem{Source: label, Reason: "workspace package " + name + " imports next/* in " + strconv.Itoa(len(files)) + " file(s) (" + strings.Join(shown, ", ") + more + "); it is outside this project and was not rewritten", Hint: "migrate the package's next/* imports by hand"})
			}
			return
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return
		}
		dir = parent
	}
}

// workspaceNextImports lists (relative paths of) source files under dir that
// import next or next/*. The walk is bounded.
func workspaceNextImports(dir string) []string {
	var found []string
	count := 0
	_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			switch d.Name() {
			case "node_modules", "dist", ".next", ".git", "build":
				return filepath.SkipDir
			}
			return nil
		}
		if count++; count > 3000 {
			return filepath.SkipAll
		}
		if !isModuleSource(path) {
			return nil
		}
		data, err := os.ReadFile(path)
		if err == nil && nextImportSpecifier.Match(data) {
			rel, _ := filepath.Rel(dir, path)
			found = append(found, filepath.ToSlash(rel))
		}
		return nil
	})
	sort.Strings(found)
	return found
}

// drain rewrites every queued module and plans it. Modules the later rewrite
// passes leave unchanged are pruned by pruneUnchangedModules.
func (g *localGraph) drain() {
	for len(g.queue) > 0 {
		p := g.queue[0]
		g.queue = g.queue[1:]
		data, err := os.ReadFile(filepath.Join(g.root, filepath.FromSlash(p)))
		if err != nil {
			continue
		}
		src, rewrites, manual := rewriteNextSource(string(data), KindModule)
		if strings.Contains(src, "next/server") {
			var apiRewrites, apiManual []string
			src, apiRewrites, apiManual = rewriteAPIServerSource(src)
			rewrites = append(rewrites, apiRewrites...)
			manual = append(manual, apiManual...)
		}
		src, relRewrites, _, _ := rewriteRelativeImportsWith(src, p, p, g.assets, g.resolveRelative)
		src, aliasRewrites := g.rewriteAlias(src, p, filepath.ToSlash(filepath.Dir(filepath.FromSlash(p))))
		rewrites = append(rewrites, relRewrites...)
		rewrites = append(rewrites, aliasRewrites...)
		for _, m := range manual {
			g.rep.Manual = append(g.rep.Manual, ManualItem{Source: p, Reason: m, Hint: "review this file after migration"})
		}
		g.modules = append(g.modules, PlannedFile{Kind: KindModule, Source: p, Target: p, Content: []byte(src), orig: data, Rewrites: rewrites})
	}
	g.rep.Files = append(g.rep.Files, g.modules...)
}

// pruneUnchangedModules drops in-place modules the rewrite passes left byte
// for byte as they were, so only files that really change are planned.
func pruneUnchangedModules(rep *Report) {
	kept := rep.Files[:0]
	for _, f := range rep.Files {
		if f.Kind == KindModule && string(f.Content) == string(f.orig) {
			continue
		}
		kept = append(kept, f)
	}
	rep.Files = kept
}
