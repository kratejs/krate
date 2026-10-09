// Package gitinfo provides lightweight, cached git metadata (last-commit
// dates) for content files. It shells out to `git` and degrades silently when
// git is unavailable or the project is not a repository.
package gitinfo

import (
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

type rootInfo struct {
	dir string
	ok  bool
}

var (
	mu    sync.Mutex
	roots = map[string]rootInfo{} // project root → resolved git top-level
)

// gitRoot resolves the repository top-level for a project root. Results are
// cached PER ROOT (not process-wide) so multi-project processes — the MCP
// server, plugin builds, or tests that build several temp projects — resolve
// each project's own repository.
func gitRoot(root string) (string, bool) {
	mu.Lock()
	defer mu.Unlock()
	if ri, seen := roots[root]; seen {
		return ri.dir, ri.ok
	}
	if root == "" {
		roots[root] = rootInfo{}
		return "", false
	}
	out, err := exec.Command("git", "-C", root, "rev-parse", "--show-toplevel").Output()
	var ri rootInfo
	if err == nil {
		ri.dir = strings.TrimSpace(string(out))
		ri.ok = ri.dir != ""
	}
	roots[root] = ri
	return ri.dir, ri.ok
}

// Available reports whether git metadata can be read for root.
func Available(root string) bool {
	_, ok := gitRoot(root)
	return ok
}

// Index holds per-file git metadata computed in a single `git log` pass, so a
// build reads commit dates/authors for every page without spawning a git
// process per file.
type Index struct {
	root    string
	gr      string
	last    map[string]string   // repo-relative slash path → committer date
	authors map[string][]string // repo-relative slash path → authors (recent first)
}

// indexCache holds one Index per project root for the process lifetime.
var indexCache sync.Map

// indexFor returns the (lazily built, cached) git index for root. It is always
// safe to call; a non-repo root yields an empty index.
func indexFor(root string) *Index {
	if v, ok := indexCache.Load(root); ok {
		return v.(*Index)
	}
	ix := buildIndex(root)
	indexCache.Store(root, ix)
	return ix
}

// canonicalPath resolves symlinks and (on Windows) short/case-variant path
// components so paths originating from different sources — t.TempDir, `git
// rev-parse --show-toplevel` (/private/var vs /var on macOS; long vs 8.3 names
// on Windows) — compare equal under filepath.Rel. Falls back to a cleaned path
// when the target cannot be resolved (e.g. a not-yet-created file).
func canonicalPath(p string) string {
	if p == "" {
		return ""
	}
	if resolved, err := filepath.EvalSymlinks(p); err == nil {
		return resolved
	}
	return filepath.Clean(p)
}

// buildIndex runs one `git log --name-only` pass and records, for each file,
// its most recent committer date and up to a few recent authors.
func buildIndex(root string) *Index {
	ix := &Index{root: root, last: map[string]string{}, authors: map[string][]string{}}
	gr, ok := gitRoot(root)
	if !ok || gr == "" {
		return ix
	}
	// Normalize both the git top-level and the project root so relative paths
	// computed against either agree.
	ix.gr = canonicalPath(gr)
	ix.root = canonicalPath(root)

	// Cap history so enormous repos stay bounded; files untouched in the last
	// 10000 commits simply report no git metadata (as an untracked file would).
	out, err := exec.Command("git", "-C", gr, "log", "--no-merges", "-n", "10000",
		"--name-only", "--format=%x1e%cI%x1f%an").Output()
	if err != nil {
		return ix
	}

	const maxAuthors = 5
	var curDate, curAuthor string
	for _, line := range strings.Split(string(out), "\n") {
		if line == "" {
			continue
		}
		if line[0] == '\x1e' {
			rest := line[1:]
			var date, author string
			if i := strings.IndexByte(rest, '\x1f'); i >= 0 {
				date, author = rest[:i], rest[i+1:]
			} else {
				date = rest
			}
			curDate, curAuthor = strings.TrimSpace(date), strings.TrimSpace(author)
			continue
		}
		file := line
		if _, seen := ix.last[file]; !seen {
			ix.last[file] = curDate
		}
		if curAuthor != "" && len(ix.authors[file]) < maxAuthors {
			dup := false
			for _, a := range ix.authors[file] {
				if a == curAuthor {
					dup = true
					break
				}
			}
			if !dup {
				ix.authors[file] = append(ix.authors[file], curAuthor)
			}
		}
	}
	return ix
}

// relSlash resolves path (absolute, or relative to the project root) to a
// repo-relative slash path for index lookups. Returns "" when unresolvable.
func (ix *Index) relSlash(path string) string {
	if ix.gr == "" {
		return ""
	}
	abs := path
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(ix.root, path)
	}
	// Canonicalize so the path matches the (already canonical) git top-level
	// even when callers pass a symlinked or short/case-variant root.
	abs = canonicalPath(abs)
	rel, err := filepath.Rel(ix.gr, abs)
	if err != nil {
		return ""
	}
	rel = filepath.ToSlash(rel)
	if rel == ".." || strings.HasPrefix(rel, "../") {
		return ""
	}
	return rel
}

// LastCommit returns the committer date (RFC3339) of the last commit touching
// path, or "" when unknown.
func (ix *Index) LastCommit(path string) string {
	rel := ix.relSlash(path)
	if rel == "" {
		return ""
	}
	return ix.last[rel]
}

// Authors returns up to n distinct recent authors of path (most recent first).
func (ix *Index) Authors(path string, n int) []string {
	if n <= 0 {
		return nil
	}
	rel := ix.relSlash(path)
	if rel == "" {
		return nil
	}
	a := ix.authors[rel]
	if len(a) > n {
		a = a[:n]
	}
	return append([]string(nil), a...)
}

// LastCommit returns the committer date (RFC3339) of the last commit touching
// path, or "" when git is unavailable or the file is untracked/unchanged.
// Backed by a single-pass per-root index (built once per process).
func LastCommit(root, path string) string {
	if _, ok := gitRoot(root); !ok {
		return ""
	}
	return indexFor(root).LastCommit(path)
}

// LastAuthors returns up to n distinct author names from the most recent
// commits touching path, most recent first. It returns nil when git is
// unavailable or the file is untracked/unchanged. Results are cached per
// (root, repository-relative path, n).
func LastAuthors(root, path string, n int) []string {
	if n <= 0 {
		return nil
	}
	if _, ok := gitRoot(root); !ok {
		return nil
	}
	return indexFor(root).Authors(path, n)
}
