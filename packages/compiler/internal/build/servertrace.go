package build

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/kratejs/krate/packages/compiler/internal/pluginapi"
)

// serverTrace copies the packages a node-target bundle imports at run time
// (and their dependencies) into a minimal node_modules next to server.mjs.
type serverTrace struct {
	destNM   string
	seen     map[string]string // destination dir -> real source dir
	copied   int
	Warnings []string
}

func newServerTrace(destNM string) *serverTrace {
	return &serverTrace{destNM: destNM, seen: map[string]string{}}
}

// resolvePackageDir finds node_modules/<name> by walking up from dir, the way
// Node resolves a bare import. It returns "" when the package is not installed.
func resolvePackageDir(name, from string) string {
	d := from
	for {
		cand := filepath.Join(d, "node_modules", filepath.FromSlash(name))
		if filepath.Base(d) == "node_modules" {
			cand = filepath.Join(d, filepath.FromSlash(name))
		}
		if regularFileExists(filepath.Join(cand, "package.json")) {
			return cand
		}
		parent := filepath.Dir(d)
		if parent == d {
			return ""
		}
		d = parent
	}
}

// AddPackages copies each named package, resolved from root, with its
// dependencies and optionalDependencies. Optional packages that are not
// installed (the other platforms' binaries) are skipped silently; a missing
// dependency or top-level package is a warning.
func (t *serverTrace) AddPackages(root string, names []string) {
	for _, n := range names {
		if !t.add(n, root, t.destNM, "") {
			t.Warnings = append(t.Warnings, fmt.Sprintf("package %q is external to the server bundle but is not installed under %s; install it before running the server", n, root))
		}
	}
}

// add copies one package and its dependency closure into nm. dependent is the
// destination directory of the package that needs it ("" at the top level),
// where a second version of the same package is nested. It returns false only
// when the package cannot be found.
func (t *serverTrace) add(name, from, nm, dependent string) bool {
	src := resolvePackageDir(name, from)
	if src == "" {
		return false
	}
	real, err := filepath.EvalSymlinks(src)
	if err != nil {
		return false
	}
	dest := filepath.Join(nm, filepath.FromSlash(name))
	if prev, ok := t.seen[dest]; ok {
		if prev == real {
			return true
		}
		if dependent == "" {
			t.Warnings = append(t.Warnings, fmt.Sprintf("two versions of %q are installed; the server output keeps the first", name))
			return true
		}
		return t.add(name, from, filepath.Join(dependent, "node_modules"), dependent)
	}
	t.seen[dest] = real
	if err := copyPackageDir(real, dest); err != nil {
		t.Warnings = append(t.Warnings, fmt.Sprintf("copying package %q: %v", name, err))
		return true
	}
	t.copied++
	for _, d := range packageDeps(real) {
		if !t.add(d.name, real, nm, dest) && !d.optional {
			t.Warnings = append(t.Warnings, fmt.Sprintf("package %q depends on %q, which is not installed", name, d.name))
		}
	}
	return true
}

type pkgDep struct {
	name     string
	optional bool
}

func packageDeps(dir string) []pkgDep {
	data, err := os.ReadFile(filepath.Join(dir, "package.json"))
	if err != nil {
		return nil
	}
	var pkg struct {
		Dependencies         map[string]string `json:"dependencies"`
		OptionalDependencies map[string]string `json:"optionalDependencies"`
	}
	if json.Unmarshal(data, &pkg) != nil {
		return nil
	}
	var out []pkgDep
	for n := range pkg.Dependencies {
		out = append(out, pkgDep{name: n})
	}
	for n := range pkg.OptionalDependencies {
		out = append(out, pkgDep{name: n, optional: true})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].name < out[j].name })
	return out
}

// copyPackageDir copies a package directory, following symlinked files and
// skipping its nested node_modules (those are traced as dependencies) and .git.
func copyPackageDir(src, dest string) error {
	return filepath.WalkDir(src, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		if d.IsDir() {
			if rel != "." && (d.Name() == "node_modules" || d.Name() == ".git") {
				return filepath.SkipDir
			}
			return os.MkdirAll(filepath.Join(dest, rel), 0755)
		}
		info, err := os.Stat(p)
		if err != nil || !info.Mode().IsRegular() {
			return nil
		}
		return copyFileMode(p, filepath.Join(dest, rel), info.Mode().Perm())
	})
}

func copyFileMode(src, dst string, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode|0200)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// copyTraceIncludes copies the project files matched by the
// outputFileTracingIncludes globs into destDir, keeping their project-relative
// paths. Every route key contributes: files are not tied to a route in the
// self-contained output. It returns the number of files copied and the globs
// that matched nothing.
func copyTraceIncludes(root string, includes map[string][]string, destDir string) (int, []string, error) {
	globSet := map[string]bool{}
	for _, globs := range includes {
		for _, g := range globs {
			globSet[filepath.ToSlash(path.Clean(filepath.ToSlash(g)))] = true
		}
	}
	globs := make([]string, 0, len(globSet))
	for g := range globSet {
		globs = append(globs, g)
	}
	sort.Strings(globs)
	copied := map[string]bool{}
	var unmatched []string
	for _, g := range globs {
		matched := 0
		files, err := expandProjectGlob(root, g)
		if err != nil {
			return len(copied), unmatched, err
		}
		for _, rel := range files {
			if copied[rel] {
				matched++
				continue
			}
			src := filepath.Join(root, filepath.FromSlash(rel))
			info, err := os.Stat(src)
			if err != nil || !info.Mode().IsRegular() {
				continue
			}
			if err := copyFileMode(src, filepath.Join(destDir, filepath.FromSlash(rel)), info.Mode().Perm()); err != nil {
				return len(copied), unmatched, fmt.Errorf("outputFileTracingIncludes %q: copying %s: %w", g, rel, err)
			}
			copied[rel] = true
			matched++
		}
		if matched == 0 {
			unmatched = append(unmatched, g)
		}
	}
	return len(copied), unmatched, nil
}

func hasGlobMeta(s string) bool { return strings.ContainsAny(s, "*?[") }

// expandProjectGlob lists the files under root (as slash paths) that match
// glob. A glob without wildcards that names a directory matches everything in
// it. Symlinked directories are followed, so a pnpm node_modules entry works;
// outside node_modules a path must also resolve inside the project root.
func expandProjectGlob(root, glob string) ([]string, error) {
	segs := strings.Split(glob, "/")
	prefix := 0
	for prefix < len(segs) && !hasGlobMeta(segs[prefix]) {
		prefix++
	}
	startRel := strings.Join(segs[:prefix], "/")
	start := filepath.Join(root, filepath.FromSlash(startRel))
	if !pluginapi.WithinRoot(root, start) {
		return nil, fmt.Errorf("outputFileTracingIncludes glob %q leaves the project root", glob)
	}
	info, err := os.Stat(start)
	if err != nil {
		return nil, nil
	}
	inNM := startRel == "node_modules" || strings.HasPrefix(startRel, "node_modules/") || strings.Contains(startRel, "/node_modules/")
	var out []string
	if !info.IsDir() {
		if prefix == len(segs) && checkRealInside(root, start, inNM) {
			out = append(out, startRel)
		}
		return out, nil
	}
	literalDir := prefix == len(segs)
	visited := map[string]bool{}
	var walk func(dirRel string, nm bool)
	walk = func(dirRel string, nm bool) {
		abs := filepath.Join(root, filepath.FromSlash(dirRel))
		real, err := filepath.EvalSymlinks(abs)
		if err != nil || visited[real] {
			return
		}
		visited[real] = true
		entries, err := os.ReadDir(abs)
		if err != nil {
			return
		}
		for _, e := range entries {
			name := e.Name()
			rel := name
			if dirRel != "" {
				rel = dirRel + "/" + name
			}
			child := filepath.Join(abs, name)
			fi, err := os.Stat(child)
			if err != nil {
				continue
			}
			if fi.IsDir() {
				if name == ".git" || (name == "node_modules" && !nm) {
					continue
				}
				if !nm && !checkRealInside(root, child, false) {
					continue
				}
				walk(rel, nm || name == "node_modules")
				continue
			}
			if !fi.Mode().IsRegular() || (!nm && !checkRealInside(root, child, false)) {
				continue
			}
			if literalDir || matchSegs(segs, strings.Split(rel, "/")) {
				out = append(out, rel)
			}
		}
	}
	walk(startRel, inNM)
	sort.Strings(out)
	return out, nil
}

// checkRealInside reports whether p resolves, through symlinks, inside root.
// Inside node_modules a package manager's links to a store outside the project
// (a monorepo's root node_modules) are expected, so they pass.
func checkRealInside(root, p string, inNodeModules bool) bool {
	if inNodeModules {
		return true
	}
	return pluginapi.WithinRootEval(root, p)
}

func matchSegs(pat, name []string) bool {
	if len(pat) == 0 {
		return len(name) == 0
	}
	if pat[0] == "**" {
		for i := 0; i <= len(name); i++ {
			if matchSegs(pat[1:], name[i:]) {
				return true
			}
		}
		return false
	}
	if len(name) == 0 {
		return false
	}
	ok, err := path.Match(pat[0], name[0])
	return err == nil && ok && matchSegs(pat[1:], name[1:])
}
