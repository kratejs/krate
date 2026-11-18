package migrate

import (
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// outsideRefSpec matches a quoted or url() relative path in source and CSS.
var outsideRefSpec = regexp.MustCompile(`["'(](\.{1,2}/[^"'()\s?#]+)`)

var outsideRefExts = map[string]bool{".css": true, ".scss": true, ".sass": true, ".less": true, ".ts": true, ".tsx": true, ".js": true, ".jsx": true, ".mjs": true, ".cjs": true, ".mts": true, ".cts": true, ".html": true}

// workspaceRoot returns the nearest ancestor of dir that declares a pnpm or
// package.json workspace, or "".
func workspaceRoot(dir string) string {
	for d := filepath.Dir(dir); d != filepath.Dir(d); d = filepath.Dir(d) {
		if fileExists(filepath.Join(d, "pnpm-workspace.yaml")) {
			return d
		}
		if data, err := os.ReadFile(filepath.Join(d, "package.json")); err == nil && strings.Contains(string(data), `"workspaces"`) {
			return d
		}
	}
	return ""
}

// warnOutsideReferences reports files elsewhere in the workspace that point
// into a route tree Apply moves aside (for example a sibling package's
// stylesheet importing ../../web/src/app/tokens.css). Those references are
// outside the project and cannot be rewritten, so the old path keeps a stub.
func warnOutsideReferences(abs, tree string, rep *Report) {
	wsRoot := workspaceRoot(abs)
	if wsRoot == "" {
		return
	}
	treeAbs := filepath.Join(abs, filepath.FromSlash(tree))
	inTree := map[string]bool{}
	_ = filepath.WalkDir(treeAbs, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			inTree[p] = true
		}
		return nil
	})
	if len(inTree) == 0 {
		return
	}
	newPath := map[string]string{}
	for _, f := range rep.Files {
		if f.Source != "" {
			newPath[filepath.Join(abs, filepath.FromSlash(f.Source))] = f.Target
		}
	}
	refs := map[string][]string{} // moved file -> "file:line" of each outside reference
	count := 0
	_ = filepath.WalkDir(wsRoot, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			switch d.Name() {
			case "node_modules", "dist", ".next", ".git", ".krate", "build", ".turbo":
				return filepath.SkipDir
			}
			if p == abs {
				return filepath.SkipDir
			}
			return nil
		}
		if !outsideRefExts[strings.ToLower(filepath.Ext(p))] {
			return nil
		}
		if count++; count > 5000 {
			return filepath.SkipAll
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return nil
		}
		for _, m := range outsideRefSpec.FindAllSubmatchIndex(data, -1) {
			hit := resolveInTree(filepath.Dir(p), string(data[m[2]:m[3]]), inTree)
			if hit == "" {
				continue
			}
			line := 1 + strings.Count(string(data[:m[2]]), "\n")
			relFile, _ := filepath.Rel(wsRoot, p)
			refs[hit] = append(refs[hit], fmt.Sprintf("%s:%d", filepath.ToSlash(relFile), line))
		}
		return nil
	})
	hits := make([]string, 0, len(refs))
	for hit := range refs {
		hits = append(hits, hit)
	}
	sort.Strings(hits)
	for _, hit := range hits {
		sort.Strings(refs[hit])
		planOutsideStub(abs, hit, refs[hit], newPath[hit], rep)
	}
}

var defaultExport = regexp.MustCompile(`(?m)^\s*export\s+default\b|export\s*\{[^}]*\bas\s+default\b`)

// planOutsideStub keeps a file that something outside the project references
// at its old path. A stylesheet or script that was migrated gets a stub that
// imports or re-exports the moved file, so there is one source of truth. Any
// other file keeps a copy and a manual item. Apply writes the stub after the
// route tree moves aside.
func planOutsideStub(abs, hit string, refs []string, moved string, rep *Report) {
	old, _ := filepath.Rel(abs, hit)
	old = filepath.ToSlash(old)
	ext := strings.ToLower(filepath.Ext(old))
	why := refs[0]
	if len(refs) > 1 {
		why = fmt.Sprintf("%s (+%d more)", refs[0], len(refs)-1)
	}
	var movedContent []byte
	if moved != "" {
		for _, f := range rep.Files {
			if f.Target == moved {
				movedContent = f.Content
			}
		}
	}
	rel := ""
	if moved != "" {
		rel = relativeSpecifier(path.Dir(old), moved, old)
	}
	switch {
	case moved != "" && ext == ".css":
		rep.Files = append(rep.Files, PlannedFile{Kind: KindStub, Target: old, Content: []byte("/* Kept so " + refs[0] + " keeps working; the file moved to " + moved + ". */\n@import '" + rel + "';\n"), Rewrites: []string{"stub that imports " + moved}})
		rep.Info = append(rep.Info, fmt.Sprintf("kept %s as a stub that imports the moved file, because %s references it", old, why))
		return
	case moved != "" && (ext == ".ts" || ext == ".tsx" || ext == ".js" || ext == ".jsx" || ext == ".mjs" || ext == ".mts"):
		spec := rel
		if ext == ".ts" || ext == ".tsx" || ext == ".jsx" {
			spec = strings.TrimSuffix(spec, path.Ext(spec))
		}
		body := "// Kept so " + refs[0] + " keeps working; the module moved to " + moved + ".\nexport * from '" + spec + "';\n"
		if defaultExport.Match(movedContent) {
			body += "export { default } from '" + spec + "';\n"
		}
		rep.Files = append(rep.Files, PlannedFile{Kind: KindStub, Target: old, Content: []byte(body), Rewrites: []string{"stub that re-exports " + moved}})
		rep.Info = append(rep.Info, fmt.Sprintf("kept %s as a stub that re-exports the moved file, because %s references it", old, why))
		return
	}
	data, err := os.ReadFile(hit)
	if err != nil {
		rep.Manual = append(rep.Manual, ManualItem{Source: old, Reason: why + " references " + old + ", which is outside this project and moves to the backup", Hint: "copy the file to where the other package expects it"})
		return
	}
	rep.Files = append(rep.Files, PlannedFile{Kind: KindStub, Target: old, Content: data, Rewrites: []string{"copy kept for " + refs[0]}})
	rep.Manual = append(rep.Manual, ManualItem{
		Source: old,
		Reason: why + " references " + old + " from outside this project; a copy stays at the old path and the migrated app does not use it",
		Hint:   "point the reference at a file the migrated app also uses, or keep the copy in sync by hand",
	})
}

// resolveInTree resolves spec against dir and returns the file in inTree it
// names, trying the usual script extensions and index files.
func resolveInTree(dir, spec string, inTree map[string]bool) string {
	base := filepath.Join(dir, filepath.FromSlash(spec))
	if inTree[base] {
		return base
	}
	for _, ext := range []string{".ts", ".tsx", ".js", ".jsx", ".mjs", ".css"} {
		if inTree[base+ext] {
			return base + ext
		}
		if inTree[filepath.Join(base, "index"+ext)] {
			return filepath.Join(base, "index"+ext)
		}
	}
	return ""
}
