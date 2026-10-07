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
	mu      sync.Mutex
	roots   = map[string]rootInfo{} // project root ��' resolved git top-level
	paths   = map[string]string{}   // root\x00repo-relative path ��' commit date
	authors = map[string][]string{} // root\x00rel\x00n ��' author names
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

// LastAuthors returns up to n distinct author names from the most recent
// commits touching path, most recent first. It returns nil when git is
// unavailable or the file is untracked/unchanged. Results are cached per
// (root, repository-relative path, n).
func LastAuthors(root, path string, n int) []string {
	if n <= 0 {
		return nil
	}
	gr, ok := gitRoot(root)
	if !ok {
		return nil
	}
	abs := path
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(root, path)
	}
	rel, err := filepath.Rel(gr, abs)
	if err != nil {
		return nil
	}
	rel = filepath.ToSlash(rel)
	key := root + "\x00" + rel + "\x00" + itoa(n)

	mu.Lock()
	if v, ok := authors[key]; ok {
		mu.Unlock()
		return v
	}
	mu.Unlock()

	out, err := exec.Command("git", "-C", gr, "log", "-n", itoa(n*4), "--format=%an", "--", rel).Output()
	var names []string
	if err == nil {
		seen := map[string]bool{}
		for _, line := range strings.Split(string(out), "\n") {
			name := strings.TrimSpace(line)
			if name == "" || seen[name] {
				continue
			}
			seen[name] = true
			names = append(names, name)
			if len(names) >= n {
				break
			}
		}
	}

	mu.Lock()
	authors[key] = names
	mu.Unlock()
	return names
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
