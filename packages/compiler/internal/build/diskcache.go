package build

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"sync/atomic"

	krateversion "github.com/kratejs/krate/packages/compiler/internal/version"
)

// buildDiskCache persists per-page build outputs across runs so a rebuild with
// unchanged inputs can skip bundling + compilation + emit entirely.
// A page is keyed by a fingerprint of its transitive input files (source,
// imports, layouts, loading, CSS, assets), the build config, the codegen'd
// content hash, and the compiler version. When every input's content hash is
// unchanged the cached PageResult is replayed (its side effects - hydration JS,
// asset copies, worker/chunk registration, dep graph - are re-applied) and the
// page pipeline is skipped.
type buildDiskCache struct {
	dir         string
	cfgHash     string
	fingerprint string
	enabled     bool

	mu     sync.Mutex
	memo   map[string]cachedFileHash
	hits   int64
	misses int64
}

type cachedFileHash struct {
	mtime int64
	size  int64
	hash  string
}

// diskPageEntry is the serialized form of a cached page. Program is
// intentionally omitted (checks treat a nil Program as "AST rules skipped").
type diskPageEntry struct {
	Key         string
	OutName     string
	HTML        string
	HeadHTML    string
	ScriptHTML  string
	StyleHTML   string
	HydrationJS string
	JSFile      string
	JSBytes     []byte
	CSS         string
	HasJS       bool
	HasCSS      bool
	IsErrorPage bool
	UsedCSS     []string
	UsedFuncs   []string
	LoadingHTML string
	Mode        int
	Revalidate  int
	SourcePath  string

	DynamicParams     bool
	StaticOnly        bool
	IsDynamicTemplate bool

	Deps        []string
	DepHashes   []string
	AssetFiles  map[string]string
	WorkerFiles map[string]string
	WorkerEsm   []string
	DynImports  map[string]string
}

// diskCacheEnabled reports whether the cross-run page cache is active. It is
// disabled by KRATE_NO_BUILD_CACHE, when sourcemaps are enabled (their sidecar
// bytes aren't replayed), when a native plugin has per-page hooks, and when any
// community (JS/TS) plugin is configured - we can't statically prove it lacks
// per-page hooks, so page output could not be replayed faithfully.
func diskCacheEnabled(sourcemap, hasPerPageHooks, hasCommunityPlugins bool) bool {
	if os.Getenv("KRATE_NO_BUILD_CACHE") != "" {
		return false
	}
	if sourcemap || hasPerPageHooks || hasCommunityPlugins {
		return false
	}
	return true
}

func newBuildDiskCache(root, cfgHash string, enabled bool) *buildDiskCache {
	c := &buildDiskCache{
		dir:     filepath.Join(root, ".krate", "cache", "build"),
		cfgHash: cfgHash,
		enabled: enabled,
		memo:    map[string]cachedFileHash{},
	}
	c.fingerprint = c.fingerprintFor("")
	return c
}

// setContentHash folds the codegen'd content-collection hash into the
// fingerprint so content edits invalidate cached pages.
func (c *buildDiskCache) setContentHash(contentHash string) {
	c.mu.Lock()
	c.fingerprint = c.fingerprintFor(contentHash)
	c.mu.Unlock()
}

// diskCacheSchema is bumped whenever the shape or the semantics of cached page
// output change (e.g. new emit passes such as DCE or source maps) so entries
// written by an older pipeline can never be replayed.
var diskCacheSchema = "2"

var (
	buildStampOnce  sync.Once
	buildStampValue string
)

// compilerBuildStamp identifies the running compiler by the content hash of its
// own executable. Unlike the embedded semantic version - which is "dev" for
// every unstamped local `go build` - this changes the moment the compiler is
// rebuilt, so a development binary can never replay output produced by a
// different compiler, while identical binaries (repeat builds, released
// compilers, the same binary copied between paths) still share cache entries.
// Falls back to the semantic version if the executable can't be read.
func compilerBuildStamp() string {
	buildStampOnce.Do(func() {
		if exe, err := os.Executable(); err == nil {
			if data, rerr := os.ReadFile(exe); rerr == nil {
				sum := sha256.Sum256(data)
				buildStampValue = hex.EncodeToString(sum[:])
			}
		}
		if buildStampValue == "" {
			buildStampValue = krateversion.Value
		}
	})
	return buildStampValue
}

// fingerprintFor is the build-wide cache namespace: compiler build stamp +
// cache schema + the resolved config + the content-collection hash.
func (c *buildDiskCache) fingerprintFor(contentHash string) string {
	sum := sha256.Sum256([]byte(compilerBuildStamp() + "\x00" + diskCacheSchema + "\x00" + c.cfgHash + "\x00" + contentHash))
	return hex.EncodeToString(sum[:])
}

func (c *buildDiskCache) hashFile(path string) (string, bool) {
	fi, err := os.Stat(path)
	if err != nil || fi.IsDir() {
		return "", false
	}
	mtime, size := fi.ModTime().UnixNano(), fi.Size()
	c.mu.Lock()
	if h, ok := c.memo[path]; ok && h.mtime == mtime && h.size == size {
		c.mu.Unlock()
		return h.hash, true
	}
	c.mu.Unlock()

	data, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	sum := sha256.Sum256(data)
	hash := hex.EncodeToString(sum[:])
	c.mu.Lock()
	c.memo[path] = cachedFileHash{mtime: mtime, size: size, hash: hash}
	c.mu.Unlock()
	return hash, true
}

// computeKey hashes (fingerprint + sorted deps with their content hashes).
// ok=false when any dep is missing.
func (c *buildDiskCache) computeKey(deps []string) (key string, sorted, hashes []string, ok bool) {
	sorted = append([]string(nil), deps...)
	sort.Strings(sorted)
	hashes = make([]string, len(sorted))
	for i, d := range sorted {
		h, good := c.hashFile(d)
		if !good {
			return "", nil, nil, false
		}
		hashes[i] = h
	}
	h := sha256.New()
	h.Write([]byte(c.fingerprint))
	h.Write([]byte{0})
	for i := range sorted {
		h.Write([]byte(sorted[i]))
		h.Write([]byte{0})
		h.Write([]byte(hashes[i]))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil)), sorted, hashes, true
}

func (c *buildDiskCache) entryPath(page string) string {
	sum := sha256.Sum256([]byte(filepath.ToSlash(page)))
	return filepath.Join(c.dir, "pages", hex.EncodeToString(sum[:])+".json")
}

func (c *buildDiskCache) load(page string) (*diskPageEntry, bool) {
	if !c.enabled {
		return nil, false
	}
	data, err := os.ReadFile(c.entryPath(page))
	if err != nil {
		return nil, false
	}
	var e diskPageEntry
	if json.Unmarshal(data, &e) != nil {
		return nil, false
	}
	return &e, true
}

func (c *buildDiskCache) save(page string, e *diskPageEntry) {
	if !c.enabled {
		return
	}
	p := c.entryPath(page)
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		return
	}
	data, err := json.Marshal(e)
	if err != nil {
		return
	}
	tmp := p + ".tmp"
	if os.WriteFile(tmp, data, 0644) == nil {
		_ = os.Rename(tmp, p)
	}
}

// stats returns cache hit/miss counts for the profile report.
func (c *buildDiskCache) stats() (hits, misses int64) {
	return atomic.LoadInt64(&c.hits), atomic.LoadInt64(&c.misses)
}

func (c *buildDiskCache) markHit()  { atomic.AddInt64(&c.hits, 1) }
func (c *buildDiskCache) markMiss() { atomic.AddInt64(&c.misses, 1) }
func (c *buildDiskCache) string() string {
	h, m := c.stats()
	return fmt.Sprintf("%d hit / %d miss", h, m)
}
