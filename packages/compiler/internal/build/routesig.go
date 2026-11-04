package build

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// routeDirSigCache memoizes routeDirSignature per directory for one build pass.
var routeDirSigCache sync.Map

// routeSignature fingerprints the route-structure files that could wrap a page
// without being among its dependencies yet: for every directory from the
// page's own up to the pages root, which layout/template/loading/error files and
// @slot or (intercept) directories exist. A layout added or removed later
// changes the signature, so the cached page is rebuilt.
func (b *Builder) routeSignature(page string) string {
	h := sha256.New()
	dir := filepath.Dir(page)
	for {
		h.Write([]byte(routeDirSignature(dir)))
		h.Write([]byte{0})
		if dir == b.Cfg.PagesDir {
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return hex.EncodeToString(h.Sum(nil))
}

func routeDirSignature(dir string) string {
	if v, ok := routeDirSigCache.Load(dir); ok {
		return v.(string)
	}
	special := map[string]bool{}
	for _, names := range [][]string{layoutNames, templateNames, loadingNames, errorBoundaryNames} {
		for _, n := range names {
			special[n] = true
		}
	}
	var parts []string
	entries, err := os.ReadDir(dir)
	if err == nil {
		for _, e := range entries {
			name := e.Name()
			switch {
			case special[name] && !e.IsDir():
				parts = append(parts, name)
			case e.IsDir() && strings.HasPrefix(name, "@"):
				// A slot's file list decides which page fills it.
				sub, _ := os.ReadDir(filepath.Join(dir, name))
				names := make([]string, 0, len(sub))
				for _, s := range sub {
					names = append(names, s.Name())
				}
				sort.Strings(names)
				parts = append(parts, name+"/"+strings.Join(names, ","))
			case e.IsDir() && strings.HasPrefix(name, "("):
				parts = append(parts, name+"/")
			}
		}
	}
	sort.Strings(parts)
	sig := dir + "\x00" + strings.Join(parts, "\x00")
	routeDirSigCache.Store(dir, sig)
	return sig
}
