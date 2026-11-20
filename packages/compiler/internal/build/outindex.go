package build

import (
	"bytes"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Small static files are served from memory once read. The per-file cap keeps
// big assets on the file system and the total cap bounds the resident copy.
const (
	outIndexMaxFile  = 256 << 10
	outIndexMaxTotal = 64 << 20
	outIndexMaxDepth = 8
)

// outEntry is one file or directory of the output directory as it was when
// the index was built.
type outEntry struct {
	dir  bool
	size int64
	mod  time.Time
	abs  string
	data atomic.Pointer[[]byte]
}

// outIndex is an in-memory view of a production output directory. The served
// output never changes while `krate serve` runs, so existence and metadata
// checks on the request path read this map instead of the file system. Dev
// rebuilds the output and never registers an index. Runtime writers (the ISR
// cache, sidecar scripts) only write under .krate, which is never served.
type outIndex struct {
	root    string
	entries map[string]*outEntry // slash path relative to root, "" is the root
	cached  atomic.Int64
}

var (
	outIndexMu  sync.RWMutex
	outIndexReg = map[string]*outIndex{}
)

// registerOutIndex indexes dir and makes it the answer for file system probes
// on that directory until the returned func is called. Production serve only.
func registerOutIndex(dir string) func() {
	key := filepath.Clean(dir)
	idx := buildOutIndex(key)
	outIndexMu.Lock()
	outIndexReg[key] = idx
	outIndexMu.Unlock()
	return func() {
		outIndexMu.Lock()
		if outIndexReg[key] == idx {
			delete(outIndexReg, key)
		}
		outIndexMu.Unlock()
	}
}

// lookupOutIndex returns the index registered for dir, or nil.
func lookupOutIndex(dir string) *outIndex {
	outIndexMu.RLock()
	idx := outIndexReg[filepath.Clean(dir)]
	outIndexMu.RUnlock()
	return idx
}

func buildOutIndex(root string) *outIndex {
	idx := &outIndex{root: root, entries: map[string]*outEntry{}}
	if fi, err := os.Stat(root); err == nil && fi.IsDir() {
		idx.entries[""] = &outEntry{dir: true, mod: fi.ModTime(), abs: root}
		idx.walk(root, "", 0)
	}
	return idx
}

func (x *outIndex) walk(abs, rel string, depth int) {
	des, err := os.ReadDir(abs)
	if err != nil {
		return
	}
	for _, de := range des {
		name := de.Name()
		childRel := name
		if rel != "" {
			childRel = rel + "/" + name
		}
		childAbs := filepath.Join(abs, name)
		isDir := de.IsDir()
		fi, err := de.Info()
		if de.Type()&fs.ModeSymlink != 0 {
			fi, err = os.Stat(childAbs)
			isDir = err == nil && fi.IsDir()
		}
		if err != nil {
			continue
		}
		if isDir {
			x.entries[childRel] = &outEntry{dir: true, mod: fi.ModTime(), abs: childAbs}
			// Only the top level of .krate is needed (the manifests); the rest
			// is server-only and never served.
			if rel == "" && name == ".krate" {
				x.walkFiles(childAbs, childRel)
			} else if depth < outIndexMaxDepth {
				x.walk(childAbs, childRel, depth+1)
			}
			continue
		}
		if fi.Mode().IsRegular() {
			x.entries[childRel] = &outEntry{size: fi.Size(), mod: fi.ModTime(), abs: childAbs}
		}
	}
}

func (x *outIndex) walkFiles(abs, rel string) {
	des, err := os.ReadDir(abs)
	if err != nil {
		return
	}
	for _, de := range des {
		if fi, err := os.Stat(filepath.Join(abs, de.Name())); err == nil && fi.Mode().IsRegular() {
			x.entries[rel+"/"+de.Name()] = &outEntry{size: fi.Size(), mod: fi.ModTime(), abs: filepath.Join(abs, de.Name())}
		}
	}
}

func cleanRel(urlPath string) string {
	if filepath.Separator == '\\' {
		urlPath = strings.ReplaceAll(urlPath, "\\", "/")
	}
	return strings.TrimPrefix(path.Clean("/"+urlPath), "/")
}

func (x *outIndex) isFile(rel string) bool {
	e := x.entries[rel]
	return e != nil && !e.dir
}

func (x *outIndex) isDir(rel string) bool {
	e := x.entries[rel]
	return e != nil && e.dir
}

// join returns rel/child without a leading slash for the root.
func relJoin(rel, child string) string {
	if rel == "" {
		return child
	}
	return rel + "/" + child
}

// content returns the file's bytes when it is small enough to keep in memory.
func (x *outIndex) content(e *outEntry) []byte {
	if p := e.data.Load(); p != nil {
		return *p
	}
	if e.size > outIndexMaxFile || x.cached.Load()+e.size > outIndexMaxTotal {
		return nil
	}
	b, err := os.ReadFile(e.abs)
	if err != nil || int64(len(b)) != e.size {
		return nil
	}
	if e.data.CompareAndSwap(nil, &b) {
		x.cached.Add(e.size)
	}
	return b
}

// memFile is a file of the index handed to http.FileServer or ServeContent.
type memFile struct {
	*bytes.Reader
	name string
	e    *outEntry
}

func (f *memFile) Close() error                       { return nil }
func (f *memFile) Readdir(int) ([]fs.FileInfo, error) { return nil, io.EOF }
func (f *memFile) Stat() (fs.FileInfo, error)         { return outInfo{name: f.name, e: f.e}, nil }

// dirFile stands in for a directory; the file server only needs Stat.
type dirFile struct {
	name string
	e    *outEntry
}

func (f *dirFile) Close() error                       { return nil }
func (f *dirFile) Read([]byte) (int, error)           { return 0, io.EOF }
func (f *dirFile) Seek(int64, int) (int64, error)     { return 0, nil }
func (f *dirFile) Readdir(int) ([]fs.FileInfo, error) { return nil, io.EOF }
func (f *dirFile) Stat() (fs.FileInfo, error)         { return outInfo{name: f.name, e: f.e}, nil }

type outInfo struct {
	name string
	e    *outEntry
}

func (i outInfo) Name() string { return i.name }
func (i outInfo) Size() int64  { return i.e.size }
func (i outInfo) Mode() fs.FileMode {
	if i.e.dir {
		return fs.ModeDir | 0o755
	}
	return 0o644
}
func (i outInfo) ModTime() time.Time { return i.e.mod }
func (i outInfo) IsDir() bool        { return i.e.dir }
func (i outInfo) Sys() any           { return nil }

// open returns the file for rel: from memory when small, else from disk.
func (x *outIndex) open(rel string) (http.File, error) {
	e := x.entries[rel]
	if e == nil {
		return nil, os.ErrNotExist
	}
	name := path.Base(rel)
	if rel == "" {
		name = "/"
	}
	if e.dir {
		return &dirFile{name: name, e: e}, nil
	}
	if b := x.content(e); b != nil {
		return &memFile{Reader: bytes.NewReader(b), name: name, e: e}, nil
	}
	f, err := os.Open(e.abs)
	if err != nil {
		return nil, err
	}
	return f, nil
}

// indexedDir is the production http.FileSystem: the same hardening as
// hardenedDir, with existence and metadata answered from the index.
type indexedDir struct {
	idx *outIndex
}

func (d indexedDir) Open(name string) (http.File, error) {
	if isInternalOutputPath(name) || isHiddenPath(name) {
		return nil, os.ErrNotExist
	}
	rel := cleanRel(name)
	e := d.idx.entries[rel]
	if e == nil {
		return nil, os.ErrNotExist
	}
	if e.dir && !d.idx.isFile(relJoin(rel, "index.html")) {
		return nil, os.ErrNotExist
	}
	return d.idx.open(rel)
}

// newStaticFS returns the file system behind the static file server.
func newStaticFS(absOut string) http.FileSystem {
	if idx := lookupOutIndex(absOut); idx != nil {
		return indexedDir{idx}
	}
	return hardenedDir{http.Dir(absOut)}
}

// isFile reports whether name is a servable file: the Open checks without
// opening it.
func (d indexedDir) isFile(name string) bool {
	if isInternalOutputPath(name) || isHiddenPath(name) {
		return false
	}
	return d.idx.isFile(cleanRel(name))
}
