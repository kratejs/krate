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
	paths = map[string]string{}   // root\x00repo-relative path → commit date
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

// LastCommit returns the committer date (RFC3339) of the last commit touching
// path, or "" when git is unavailable or the file is untracked/unchanged.
// Results are cached per (root, repository-relative path).
func LastCommit(root, path string) string {
	gr, ok := gitRoot(root)
	if !ok {
		return ""
	}
	abs := path
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(root, path)
	}
	rel, err := filepath.Rel(gr, abs)
	if err != nil {
		return ""
	}
	rel = filepath.ToSlash(rel)
	key := root + "\x00" + rel

	mu.Lock()
	if v, ok := paths[key]; ok {
		mu.Unlock()
		return v
	}
	mu.Unlock()

	out, err := exec.Command("git", "-C", gr, "log", "-1", "--format=%cI", "--", rel).Output()
	val := strings.TrimSpace(string(out))
	if err != nil {
		val = ""
	}

	mu.Lock()
	paths[key] = val
	mu.Unlock()
	return val
}
