package migrate

import (
	"fmt"
	"io/fs"
	"os"
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
// outside the project and cannot be rewritten.
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
	var hits []string
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
			spec := string(data[m[2]:m[3]])
			hit := resolveInTree(filepath.Dir(p), spec, inTree)
			if hit == "" {
				continue
			}
			dest := "it moves to the backup with the tree"
			if t, ok := newPath[hit]; ok {
				dest = "it is now " + t
			}
			line := 1 + strings.Count(string(data[:m[2]]), "\n")
			relFile, _ := filepath.Rel(wsRoot, p)
			relHit, _ := filepath.Rel(abs, hit)
			hits = append(hits, fmt.Sprintf("%s:%d references %s (%s)", filepath.ToSlash(relFile), line, filepath.ToSlash(relHit), dest))
		}
		return nil
	})
	sort.Strings(hits)
	for _, h := range hits {
		rep.Manual = append(rep.Manual, ManualItem{
			Source: tree,
			Reason: h + "; the file is outside this project and was not rewritten",
			Hint:   "point the reference at the new path, or keep a copy of the file where the other package expects it",
		})
	}
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
