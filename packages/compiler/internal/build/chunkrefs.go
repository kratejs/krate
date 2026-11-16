package build

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"

	"github.com/kratejs/krate/packages/compiler/internal/diag"
)

// missingChunkRef is a /chunks/ URL the build wrote into an output file
// without writing the chunk it names.
type missingChunkRef struct {
	URL   string
	Files []string // referencing files, relative to the output directory
}

// chunkRefExts are the output files that can name a chunk: page HTML, page
// and island scripts, server bundles and manifests.
var chunkRefExts = map[string]bool{".html": true, ".js": true, ".mjs": true, ".json": true}

// findMissingChunkRefs scans the text outputs under outDir for quoted
// "<basePath>/chunks/<file>.js" URLs and returns the ones whose file is not in
// outDir/chunks, sorted by URL. A URL inside an escaped string (\"...\") is
// page content, not a reference, and is skipped, as is every file skip
// reports (by its slash path relative to outDir).
func findMissingChunkRefs(outDir, basePath string, skip func(rel string) bool) ([]missingChunkRef, error) {
	prefix := []byte(strings.TrimSuffix(basePath, "/") + "/chunks/")
	var files []string
	err := filepath.WalkDir(outDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		if !chunkRefExts[filepath.Ext(path)] {
			return nil
		}
		if skip != nil {
			if rel, err := filepath.Rel(outDir, path); err == nil && skip(filepath.ToSlash(rel)) {
				return nil
			}
		}
		files = append(files, path)
		return nil
	})
	if err != nil {
		return nil, err
	}
	chunksDir := filepath.Join(outDir, "chunks")
	var mu sync.Mutex
	refs := map[string]map[string]bool{} // URL -> referencing files
	exists := map[string]bool{}
	work := make(chan string)
	var wg sync.WaitGroup
	for range max(1, min(runtime.GOMAXPROCS(0), 8)) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for path := range work {
				data, err := os.ReadFile(path)
				if err != nil {
					continue
				}
				urls := chunkRefsIn(data, prefix)
				if len(urls) == 0 {
					continue
				}
				rel, _ := filepath.Rel(outDir, path)
				rel = filepath.ToSlash(rel)
				mu.Lock()
				for _, u := range urls {
					if refs[u] == nil {
						refs[u] = map[string]bool{}
					}
					refs[u][rel] = true
				}
				mu.Unlock()
			}
		}()
	}
	for _, f := range files {
		work <- f
	}
	close(work)
	wg.Wait()
	var out []missingChunkRef
	for u, from := range refs {
		name := u[len(prefix):]
		if _, ok := exists[name]; !ok {
			_, err := os.Stat(filepath.Join(chunksDir, filepath.FromSlash(name)))
			exists[name] = err == nil
		}
		if exists[name] {
			continue
		}
		m := missingChunkRef{URL: u}
		for f := range from {
			m.Files = append(m.Files, f)
		}
		sort.Strings(m.Files)
		out = append(out, m)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].URL < out[j].URL })
	return out, nil
}

// chunkRefsIn returns the distinct quoted chunk URLs in data that start with
// prefix ("/chunks/" or "/base/chunks/").
func chunkRefsIn(data, prefix []byte) []string {
	var out []string
	seen := map[string]bool{}
	for off := 0; ; {
		i := bytes.Index(data[off:], prefix)
		if i < 0 {
			return out
		}
		at := off + i
		off = at + len(prefix)
		if at == 0 || !isChunkQuote(data[at-1]) || (at >= 2 && data[at-2] == '\\') {
			continue
		}
		open := data[at-1]
		end := off
		for end < len(data) && isChunkNameByte(data[end]) {
			end++
		}
		if end >= len(data) || data[end] != open || end == off {
			continue
		}
		u := string(data[at:end])
		if !strings.HasSuffix(u, ".js") || strings.Contains(u, "..") || seen[u] {
			continue
		}
		seen[u] = true
		out = append(out, u)
	}
}

func isChunkQuote(c byte) bool { return c == '"' || c == '\'' || c == '`' }

func isChunkNameByte(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-' || c == '.' || c == '/'
}

// checkChunkRefs fails the build when an output names a chunk that was not
// written: the browser would 404 on it and the island or page script would
// never run. Files copied from the public directory are the app's own and are
// not checked.
func (b *Builder) checkChunkRefs() error {
	var skip func(string) bool
	if pub := b.Cfg.PublicDir; pub != "" {
		skip = func(rel string) bool {
			_, err := os.Stat(filepath.Join(pub, filepath.FromSlash(rel)))
			return err == nil
		}
	}
	missing, err := findMissingChunkRefs(b.Cfg.OutDir, b.Cfg.BaseURLPath(), skip)
	if err != nil || len(missing) == 0 {
		return err
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "the build output references %d chunk(s) that were not written:", len(missing))
	const maxListed = 10
	for i, m := range missing {
		if i == maxListed {
			fmt.Fprintf(&sb, "\n  ... and %d more", len(missing)-maxListed)
			break
		}
		from := m.Files[0]
		if len(m.Files) > 1 {
			from += fmt.Sprintf(" and %d other file(s)", len(m.Files)-1)
		}
		fmt.Fprintf(&sb, "\n  %s (referenced from %s)", m.URL, from)
	}
	file := filepath.Join(b.Cfg.OutDir, filepath.FromSlash(missing[0].Files[0]))
	return diag.Diagnostic{
		File:    b.relPath(file),
		Message: sb.String(),
		Hint:    "this is a Krate bug: the page would fail to load these scripts in the browser. Rebuild from a clean output directory, and report it with the module the chunk belongs to if it persists",
	}.WithCode(diag.CodeChunkMissing)
}
