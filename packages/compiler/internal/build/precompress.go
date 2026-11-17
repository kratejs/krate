package build

import (
	"bytes"
	"compress/gzip"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/andybalholm/brotli"

	"github.com/kratejs/krate/packages/compiler/internal/fsutil"
)

// precompressMinSize is the smallest asset worth precompressing; below it the
// compressed sibling is often no smaller than the source, so it is skipped.
const precompressMinSize = 1024

// precompressibleExts lists extensions that benefit from compression. Already
// compressed formats (images, fonts, wasm, archives) are excluded.
var precompressibleExts = map[string]bool{
	".html": true, ".htm": true, ".css": true, ".js": true, ".mjs": true,
	".cjs": true, ".json": true, ".map": true, ".svg": true, ".xml": true,
	".txt": true, ".webmanifest": true, ".rss": true, ".atom": true,
}

// brotliLevel is the level of every compressible file. Level 11 is the slowest,
// but a site compresses once per changed file (siblings are skipped by mtime)
// and the result is served from every request: it is about 10% smaller than
// level 6 on HTML and 7% on scripts.
const brotliLevel = 11

// brotliFallbackLevel is also tried for scripts and stylesheets, since the
// maximum level loses on some highly repetitive input; the smaller output is
// kept. Markup and data always compress best at the maximum, so they skip it.
const brotliFallbackLevel = 6

// brotliLevelFor returns the brotli level for a file.
func brotliLevelFor(path string) int { return brotliLevel }

// PrecompressOutput writes `.br` and/or `.gz` siblings for compressible assets
// under outDir. It is idempotent: a sibling at least as new as its source is
// kept, so dev/watch rebuilds only recompress changed files. Returns the number
// of siblings written.
func PrecompressOutput(outDir string, brotliOn, gzipOn bool) (int, error) {
	if !brotliOn && !gzipOn {
		return 0, nil
	}

	type job struct {
		path string
		mod  time.Time
		size int64
	}
	var jobs []job
	err := filepath.WalkDir(outDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if isUnderKrateDir(outDir, p) {
			// Internal build artifacts (.krate/server-bundles, server-renderer,
			// manifest, ...) are not served as static assets.
			return nil
		}
		if !precompressibleExts[strings.ToLower(filepath.Ext(p))] {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if info.Size() < precompressMinSize {
			return nil
		}
		jobs = append(jobs, job{p, info.ModTime(), info.Size()})
		return nil
	})
	if err != nil {
		return 0, err
	}
	if len(jobs) == 0 {
		return 0, nil
	}
	// Largest first: the slowest encodes start early instead of trailing.
	sort.SliceStable(jobs, func(i, j int) bool { return jobs[i].size > jobs[j].size })

	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		written  int
		firstErr error
	)
	ch := make(chan job)
	workers := max(1, runtime.GOMAXPROCS(0))
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range ch {
				n, err := precompressFile(j.path, j.mod, brotliOn, gzipOn)
				mu.Lock()
				written += n
				if err != nil && firstErr == nil {
					firstErr = err
				}
				mu.Unlock()
			}
		}()
	}
	for _, j := range jobs {
		ch <- j
	}
	close(ch)
	wg.Wait()

	return written, firstErr
}

// precompressFile writes the compressed siblings for one file.
func precompressFile(path string, srcMod time.Time, brotliOn, gzipOn bool) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	written := 0
	if brotliOn {
		ok, err := writePrecompressedSibling(path+".br", data, srcMod, func(b []byte) ([]byte, error) { return brotliCompressFor(path, b) })
		if err != nil {
			return written, err
		}
		if ok {
			written++
		}
	}
	if gzipOn {
		ok, err := writePrecompressedSibling(path+".gz", data, srcMod, gzipCompress)
		if err != nil {
			return written, err
		}
		if ok {
			written++
		}
	}
	return written, nil
}

// writePrecompressedSibling encodes data and writes it to dst unless dst is
// already current. It reports whether a file was written; an encoding that does
// not shrink the source is skipped.
func writePrecompressedSibling(dst string, data []byte, srcMod time.Time, encode func([]byte) ([]byte, error)) (bool, error) {
	if fi, err := os.Stat(dst); err == nil && !fi.ModTime().Before(srcMod) {
		return false, nil
	}
	out, err := encode(data)
	if err != nil {
		return false, err
	}
	if len(out) >= len(data) {
		return false, nil
	}
	if err := fsutil.WriteFileAtomic(dst, out, 0644); err != nil {
		return false, err
	}
	return true, nil
}

// isUnderKrateDir reports whether p is inside a `.krate` directory (a path
// segment equal to ".krate") anywhere under outDir.
func isUnderKrateDir(outDir, p string) bool {
	rel, err := filepath.Rel(outDir, p)
	if err != nil {
		return false
	}
	for _, seg := range strings.Split(filepath.ToSlash(rel), "/") {
		if seg == ".krate" {
			return true
		}
	}
	return false
}

// brotliCompressFor encodes a file at its level. A script or stylesheet is also
// encoded at the fallback level and the smaller result is kept.
func brotliCompressFor(path string, data []byte) ([]byte, error) {
	level := brotliLevelFor(path)
	out, err := brotliCompressLevel(data, level)
	if err != nil || level == brotliFallbackLevel {
		return out, err
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".js", ".mjs", ".cjs", ".css":
	default:
		return out, nil
	}
	if base, err := brotliCompressLevel(data, brotliFallbackLevel); err == nil && len(base) < len(out) {
		return base, nil
	}
	return out, nil
}

func brotliCompressLevel(data []byte, level int) ([]byte, error) {
	var buf bytes.Buffer
	w := brotli.NewWriterLevel(&buf, level)
	if _, err := w.Write(data); err != nil {
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func gzipCompress(data []byte) ([]byte, error) {
	var buf bytes.Buffer
	w, err := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	if err != nil {
		return nil, err
	}
	if _, err := w.Write(data); err != nil {
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
