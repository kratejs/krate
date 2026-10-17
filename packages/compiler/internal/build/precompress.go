package build

import (
	"bytes"
	"compress/gzip"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
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

// brotliLevel trades build time for ratio. 6 is a good default for a static
// build (11 is much slower for a marginal gain).
const brotliLevel = 6

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
		jobs = append(jobs, job{p, info.ModTime()})
		return nil
	})
	if err != nil {
		return 0, err
	}
	if len(jobs) == 0 {
		return 0, nil
	}

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
		ok, err := writePrecompressedSibling(path+".br", data, srcMod, brotliCompress)
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

func brotliCompress(data []byte) ([]byte, error) {
	var buf bytes.Buffer
	w := brotli.NewWriterLevel(&buf, brotliLevel)
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
