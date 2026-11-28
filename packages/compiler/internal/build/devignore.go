package build

import (
	"os/exec"
	"path/filepath"
	"sync"
)

// gitIgnores answers whether a path is ignored by the git ignore rules of the
// project's work tree. The dev watcher uses it for changes that no build
// depends on: an ignored file is runtime data or build output (an API route's
// audit log, a media job's progress file or HLS segments), so writing it must
// not rebuild the site. Without git, or outside a work tree, nothing is
// ignored.
type gitIgnores struct {
	root string
	mu   sync.Mutex
	// ok is false when git is missing or root is not in a work tree.
	ok      bool
	checked bool
	// paths caches each answer; a .gitignore edit clears it.
	paths map[string]bool
}

func newGitIgnores(root string) *gitIgnores {
	return &gitIgnores{root: root}
}

// reset forgets the cached answers (a .gitignore changed).
func (g *gitIgnores) reset() {
	g.mu.Lock()
	g.paths = nil
	g.mu.Unlock()
}

// maxIgnoreCache bounds the cache: a job that writes many new files must not
// grow it without end.
const maxIgnoreCache = 4096

// ignored reports whether path is ignored. A file is ignored when its
// directory is (the usual case: a whole data or output directory), so the
// answer for one directory covers every file a job writes into it.
func (g *gitIgnores) ignored(path string, isDir bool) bool {
	if g == nil {
		return false
	}
	dir := path
	if !isDir {
		dir = filepath.Dir(path)
	}
	if g.check(dir) {
		return true
	}
	return !isDir && g.check(path)
}

func (g *gitIgnores) check(path string) bool {
	g.mu.Lock()
	if !g.checked {
		g.checked = true
		g.ok = exec.Command("git", "-C", g.root, "rev-parse", "--is-inside-work-tree").Run() == nil
	}
	if !g.ok {
		g.mu.Unlock()
		return false
	}
	if v, hit := g.paths[path]; hit {
		g.mu.Unlock()
		return v
	}
	g.mu.Unlock()
	rel, err := filepath.Rel(g.root, path)
	if err != nil {
		return false
	}
	// Exit status 0: ignored; 1: not ignored; anything else is an error,
	// taken as not ignored.
	ign := exec.Command("git", "-C", g.root, "check-ignore", "-q", "--", filepath.ToSlash(rel)).Run() == nil
	g.mu.Lock()
	if g.paths == nil || len(g.paths) >= maxIgnoreCache {
		g.paths = map[string]bool{}
	}
	g.paths[path] = ign
	g.mu.Unlock()
	return ign
}
