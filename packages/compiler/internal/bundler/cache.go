package bundler

import (
	"sync"
	"time"

	"github.com/kratejs/krate/packages/compiler/ast"
)

// ModuleCache memoizes per-file parse results across every Bundler in a build.
// Bundles previously re-read, re-lexed, and re-parsed each imported module once
// per page (the docs theme/components/runtime were parsed once per page), so a
// build was O(pages x modules). Sharing this cache makes it O(modules).
// Cached values are immutable after creation. The parsed *ast.Program is shared
// by reference across bundles; callers must treat it as read-only for shared
// (non-entry) modules - see the once-per-module transform guard in the build.
type ModuleCache struct {
	mu       sync.Mutex
	programs map[string]*programEntry
	css      map[string]*cssEntry
	assets   map[string]*assetEntry
}

type programEntry struct {
	mtime       time.Time
	size        int64
	program     *ast.Program
	source      string
	imports     []string
	compClass   ComponentClass
	frontmatter map[string]any
}

type cssEntry struct {
	mtime    time.Time
	size     int64
	css      string
	mapping  map[string]string
	isModule bool
	// assets are the `url(...)` assets this sheet references (source path ->
	// hashed URL), replayed into every bundle that reuses the cached CSS.
	assets map[string]string
}

type assetEntry struct {
	mtime time.Time
	size  int64
	url   string
}

// NewModuleCache creates an empty cache. One cache is shared per build (and,
// in dev, across partial rebuilds) so repeated work is done once.
func NewModuleCache() *ModuleCache {
	return &ModuleCache{
		programs: make(map[string]*programEntry),
		css:      make(map[string]*cssEntry),
		assets:   make(map[string]*assetEntry),
	}
}

func (c *ModuleCache) getProgram(abs string, mtime time.Time, size int64) (*programEntry, bool) {
	if c == nil {
		return nil, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.programs[abs]
	if !ok || !e.mtime.Equal(mtime) || e.size != size {
		return nil, false
	}
	return e, true
}

func (c *ModuleCache) putProgram(abs string, e *programEntry) {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.programs[abs] = e
	c.mu.Unlock()
}

func (c *ModuleCache) getCSS(abs string, mtime time.Time, size int64) (*cssEntry, bool) {
	if c == nil {
		return nil, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.css[abs]
	if !ok || !e.mtime.Equal(mtime) || e.size != size {
		return nil, false
	}
	return e, true
}

func (c *ModuleCache) putCSS(abs string, e *cssEntry) {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.css[abs] = e
	c.mu.Unlock()
}

func (c *ModuleCache) getAsset(abs string, mtime time.Time, size int64) (*assetEntry, bool) {
	if c == nil {
		return nil, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.assets[abs]
	if !ok || !e.mtime.Equal(mtime) || e.size != size {
		return nil, false
	}
	return e, true
}

func (c *ModuleCache) putAsset(abs string, e *assetEntry) {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.assets[abs] = e
	c.mu.Unlock()
}
