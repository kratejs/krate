package bundler

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"sync"
	"time"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/diag"
)

// Small modules are verified by content hash on every cache lookup. mtime can be
// coarse (or deliberately preserved) on some filesystems, so a same-size edit
// would otherwise replay a stale AST/CSS/asset URL across dev rebuilds.
const contentVerifyLimit = 512 << 10

func fileContentHash(abs string, size int64) string {
	if size < 0 || size > contentVerifyLimit {
		return ""
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func stampMatches(abs string, mtime time.Time, size int64, hash string) bool {
	if size > contentVerifyLimit {
		return true
	}
	return hash != "" && fileContentHash(abs, size) == hash
}

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
	res      *resolverMemo
	renamed  map[string]*renamedEntry
	warned   map[string]bool
	// verified holds, per file, the content hash checked against the file
	// during this build (reset by ResetResolver). Without it every page
	// re-read and re-hashed every module it imports, under mu.
	verified map[string]verifiedStamp
	// parses counts the parses of each file over the cache's lifetime.
	parses map[string]int
	// parsing holds the files a bundle is parsing now: bundles that need one
	// wait for that parse instead of parsing it too.
	parsing map[string]chan struct{}
}

// beginParse claims the parse of abs. It returns a func that ends the claim,
// or nil after waiting for a parse another bundle had claimed: the caller then
// looks the file up again.
func (c *ModuleCache) beginParse(abs string) func() {
	if c == nil {
		return func() {}
	}
	c.mu.Lock()
	if ch, ok := c.parsing[abs]; ok {
		c.mu.Unlock()
		<-ch
		return nil
	}
	if c.parsing == nil {
		c.parsing = map[string]chan struct{}{}
	}
	ch := make(chan struct{})
	c.parsing[abs] = ch
	c.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			c.mu.Lock()
			delete(c.parsing, abs)
			c.mu.Unlock()
			close(ch)
		})
	}
}

type verifiedStamp struct {
	mtime time.Time
	size  int64
	hash  string
}

// matches reports whether the cached entry stamped (mtime, size, hash) is
// still the file at abs. The content of a small file is hashed once per
// build, outside the lock.
func (c *ModuleCache) matches(abs string, mtime time.Time, size int64, hash string) bool {
	if size > contentVerifyLimit {
		return true
	}
	if hash == "" {
		return false
	}
	c.mu.Lock()
	v, ok := c.verified[abs]
	c.mu.Unlock()
	if ok && v.mtime.Equal(mtime) && v.size == size {
		return v.hash == hash
	}
	got := fileContentHash(abs, size)
	if got == "" {
		return false
	}
	c.mu.Lock()
	if c.verified == nil {
		c.verified = map[string]verifiedStamp{}
	}
	c.verified[abs] = verifiedStamp{mtime: mtime, size: size, hash: got}
	c.mu.Unlock()
	return got == hash
}

// ParseCounts returns how many times each file was parsed into the cache.
func (c *ModuleCache) ParseCounts() map[string]int {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make(map[string]int, len(c.parses))
	for k, v := range c.parses {
		out[k] = v
	}
	return out
}

// firstWarning reports whether key has not been seen before, and records it.
func (c *ModuleCache) firstWarning(key string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.warned[key] {
		return false
	}
	if c.warned == nil {
		c.warned = map[string]bool{}
	}
	c.warned[key] = true
	return true
}

// renamedEntry holds copies of one parsed module with private names renamed,
// keyed by the names. It belongs to one parse of the module.
type renamedEntry struct {
	orig   *ast.Program
	copies map[string]*ast.Program
}

func (c *ModuleCache) getRenamed(path string, orig *ast.Program, names string) *ast.Program {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if e := c.renamed[path]; e != nil && e.orig == orig {
		return e.copies[names]
	}
	return nil
}

func (c *ModuleCache) putRenamed(path string, orig *ast.Program, names string, prog *ast.Program) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.renamed == nil {
		c.renamed = map[string]*renamedEntry{}
	}
	e := c.renamed[path]
	if e == nil || e.orig != orig {
		e = &renamedEntry{orig: orig, copies: map[string]*ast.Program{}}
		c.renamed[path] = e
	}
	e.copies[names] = prog
}

// resolverMemo memoizes filesystem-existence probes and import resolutions for a
// build. It is shared across every Bundler in the build (like the parse cache),
// so pages don't repeat the same os.Stat probes and node_modules walks. It is
// reset at the start of each build (file adds/removes between dev rebuilds must
// not be served from a stale negative entry); the parse cache is unaffected.
type resolverMemo struct {
	// sync.Maps: every page of a build reads these at once, and entries are
	// written once and read many times.
	exists  sync.Map // path -> bool
	imports sync.Map // key -> resolved path
}

func newResolverMemo() *resolverMemo {
	return &resolverMemo{}
}

func (m *resolverMemo) existsResult(path string) (bool, bool) {
	if m == nil {
		return false, false
	}
	v, ok := m.exists.Load(path)
	if !ok {
		return false, false
	}
	return v.(bool), true
}

func (m *resolverMemo) putExists(path string, v bool) {
	if m == nil {
		return
	}
	m.exists.Store(path, v)
}

func (m *resolverMemo) importResult(key string) (string, bool) {
	if m == nil {
		return "", false
	}
	v, ok := m.imports.Load(key)
	if !ok {
		return "", false
	}
	return v.(string), true
}

func (m *resolverMemo) putImport(key, v string) {
	if m == nil {
		return
	}
	m.imports.Store(key, v)
}

func (m *resolverMemo) reset() {
	if m == nil {
		return
	}
	m.exists.Clear()
	m.imports.Clear()
}

// DropPrograms forgets every parsed program and renamed copy (see
// applyPrivateRenames) and returns them. A build that bundles few or no more
// modules calls it to free them; a later bundle parses again.
func (c *ModuleCache) DropPrograms() []*ast.Program {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []*ast.Program
	for _, e := range c.programs {
		out = append(out, e.program)
	}
	for _, e := range c.renamed {
		for _, p := range e.copies {
			out = append(out, p)
		}
	}
	c.programs = map[string]*programEntry{}
	c.renamed = nil
	return out
}

// LivePrograms is the set of programs the cache holds now (parsed modules
// and their renamed copies). State kept per program elsewhere is stale for
// any program not in it: an edited module was parsed again, and an entry
// module is parsed on every build.
func (c *ModuleCache) LivePrograms() map[*ast.Program]bool {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make(map[*ast.Program]bool, len(c.programs))
	for _, e := range c.programs {
		out[e.program] = true
	}
	for _, e := range c.renamed {
		for _, p := range e.copies {
			out[p] = true
		}
	}
	return out
}

// ResetResolver clears the filesystem-existence and import-resolution memo so a
// new build re-probes the filesystem. Call once per build.
func (c *ModuleCache) ResetResolver() {
	if c == nil {
		return
	}
	c.res.reset()
	c.mu.Lock()
	c.verified = nil
	c.mu.Unlock()
}

type programEntry struct {
	mtime       time.Time
	size        int64
	hash        string
	program     *ast.Program
	source      string
	imports     []string
	compClass   ComponentClass
	frontmatter map[string]any
	diags       []diag.Diagnostic
}

type cssEntry struct {
	mtime    time.Time
	size     int64
	hash     string
	css      string
	mapping  map[string]string
	isModule bool
	// assets are the `url(...)` assets this sheet references (source path ->
	// hashed URL), replayed into every bundle that reuses the cached CSS.
	assets map[string]string
	// deps are the imported sheets, url() assets and composes sources the
	// cached CSS was built from; the entry is stale when any of them changed.
	deps []cssDep
	// composesFrom are the CSS module files this sheet composes from.
	composesFrom []string
	// diags are the warnings produced while building the sheet, replayed with it.
	diags []diag.Diagnostic
}

type assetEntry struct {
	mtime time.Time
	size  int64
	hash  string
	url   string
}

// NewModuleCache creates an empty cache. One cache is shared per build (and,
// in dev, across partial rebuilds) so repeated work is done once.
func NewModuleCache() *ModuleCache {
	return &ModuleCache{
		programs: make(map[string]*programEntry),
		css:      make(map[string]*cssEntry),
		assets:   make(map[string]*assetEntry),
		res:      newResolverMemo(),
	}
}

func (c *ModuleCache) getProgram(abs string, mtime time.Time, size int64) (*programEntry, bool) {
	if c == nil {
		return nil, false
	}
	c.mu.Lock()
	e, ok := c.programs[abs]
	c.mu.Unlock()
	if !ok || !e.mtime.Equal(mtime) || e.size != size || !c.matches(abs, mtime, size, e.hash) {
		return nil, false
	}
	return e, true
}

func (c *ModuleCache) putProgram(abs string, e *programEntry) {
	if c == nil {
		return
	}
	e.hash = fileContentHash(abs, e.size)
	c.mu.Lock()
	c.programs[abs] = e
	if c.parses == nil {
		c.parses = map[string]int{}
	}
	c.parses[abs]++
	c.mu.Unlock()
}

func (c *ModuleCache) getCSS(abs string, mtime time.Time, size int64) (*cssEntry, bool) {
	if c == nil {
		return nil, false
	}
	c.mu.Lock()
	e, ok := c.css[abs]
	c.mu.Unlock()
	if !ok || !e.mtime.Equal(mtime) || e.size != size || !c.matches(abs, mtime, size, e.hash) {
		return nil, false
	}
	if !cssDepsCurrent(e.deps) {
		return nil, false
	}
	return e, true
}

func (c *ModuleCache) putCSS(abs string, e *cssEntry) {
	if c == nil {
		return
	}
	e.hash = fileContentHash(abs, e.size)
	c.mu.Lock()
	c.css[abs] = e
	c.mu.Unlock()
}

func (c *ModuleCache) getAsset(abs string, mtime time.Time, size int64) (*assetEntry, bool) {
	if c == nil {
		return nil, false
	}
	c.mu.Lock()
	e, ok := c.assets[abs]
	c.mu.Unlock()
	if !ok || !e.mtime.Equal(mtime) || e.size != size || !c.matches(abs, mtime, size, e.hash) {
		return nil, false
	}
	return e, true
}

func (c *ModuleCache) putAsset(abs string, e *assetEntry) {
	if c == nil {
		return
	}
	e.hash = fileContentHash(abs, e.size)
	c.mu.Lock()
	c.assets[abs] = e
	c.mu.Unlock()
}
