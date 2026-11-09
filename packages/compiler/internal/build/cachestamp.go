package build

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	krateversion "github.com/kratejs/krate/packages/compiler/internal/version"
)

const cacheStampFile = "stamp.json"

// cacheStamp records which compiler first filled a cache directory. It is
// written once and never rewritten, so a cache that outlives an upgrade stays
// recognizable as one that holds entries from an older compiler.
type cacheStamp struct {
	Krate  string `json:"krate"`
	Schema string `json:"schema"`
}

// writeCacheStamp creates the stamp in the page cache directory when it is
// missing.
func writeCacheStamp(dir string) {
	data, err := json.Marshal(cacheStamp{Krate: krateversion.Value, Schema: diskCacheSchema})
	if err != nil {
		return
	}
	f, err := os.OpenFile(filepath.Join(dir, cacheStampFile), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return
	}
	_, _ = f.Write(data)
	_ = f.Close()
}

// CacheStale reports whether the project's page cache (.krate/cache/build)
// should be cleared, with the reason. A cache is stale when it was filled by a
// different compiler version or cache schema than the running one, or when
// some entries cannot be read. A missing cache, or one written before stamps
// existed, is not reported: there is nothing to compare.
func CacheStale(root string) (bool, string) {
	dir := filepath.Join(root, ".krate", "cache", "build")
	if data, err := os.ReadFile(filepath.Join(dir, cacheStampFile)); err == nil {
		var st cacheStamp
		if json.Unmarshal(data, &st) == nil {
			if st.Krate != krateversion.Value {
				return true, fmt.Sprintf("filled by krate v%s, this is v%s", st.Krate, krateversion.Value)
			}
			if st.Schema != diskCacheSchema {
				return true, fmt.Sprintf("cache schema %s, this build uses %s", st.Schema, diskCacheSchema)
			}
		}
	}
	entries, err := os.ReadDir(filepath.Join(dir, "pages"))
	if err != nil {
		return false, ""
	}
	bad := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, "pages", e.Name()))
		if err != nil || !json.Valid(data) {
			bad++
		}
	}
	if bad > 0 {
		return true, fmt.Sprintf("%d unreadable cache entr%s", bad, map[bool]string{true: "y", false: "ies"}[bad == 1])
	}
	return false, ""
}
