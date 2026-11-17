package build

import (
	"os"
	"strings"
	"sync"

	"github.com/kratejs/krate/packages/compiler/internal/bundler"
	"github.com/kratejs/krate/packages/compiler/internal/tsstrip"
)

// serverBundleFiles keeps, for one run of the page server bundles, what their
// plugins derive from a project file alone: its file identity and the source
// they serve for it. The pages load many of the same modules, and the files do
// not change while the bundles build, so each is read and rewritten once for
// the run instead of once per page. A nil *serverBundleFiles keeps nothing.
type serverBundleFiles struct {
	stats       onceCache[os.FileInfo]
	sources     onceCache[[]byte]
	goModules   onceCache[string]
	clientStubs onceCache[string]
}

// onceCache computes the value of each key once; callers that ask while it is
// computed wait for it.
type onceCache[V any] struct{ m sync.Map }

type onceEntry[V any] struct {
	once sync.Once
	v    V
	err  error
}

func (c *onceCache[V]) get(key string, compute func() (V, error)) (V, error) {
	e, _ := c.m.LoadOrStore(key, &onceEntry[V]{})
	entry := e.(*onceEntry[V])
	entry.once.Do(func() { entry.v, entry.err = compute() })
	return entry.v, entry.err
}

// stat is os.Stat, nil when the file cannot be read.
func (f *serverBundleFiles) stat(path string) os.FileInfo {
	if f == nil {
		info, _ := os.Stat(path)
		return info
	}
	info, _ := f.stats.get(path, func() (os.FileInfo, error) {
		info, err := os.Stat(path)
		if err != nil {
			return nil, nil
		}
		return info, nil
	})
	return info
}

// sameFile is sameFileIdentity.
func (f *serverBundleFiles) sameFile(a, b string) bool {
	if f == nil {
		return sameFileIdentity(a, b)
	}
	ai := f.stat(a)
	if ai == nil {
		return false
	}
	bi := f.stat(b)
	return bi != nil && os.SameFile(ai, bi)
}

// serverSource is readServerSource. Callers must not modify the result.
func (f *serverBundleFiles) serverSource(root, path string) ([]byte, error) {
	if f == nil {
		return readServerSource(root, path)
	}
	return f.sources.get(path, func() ([]byte, error) { return readServerSource(root, path) })
}

// goModuleSource is the source the Go-component plugin serves for a project
// module: its server source with the Go call sites annotated.
func (f *serverBundleFiles) goModuleSource(root, path string) (string, error) {
	load := func() (string, error) {
		data, err := f.serverSource(root, path)
		if err != nil {
			return "", err
		}
		return annotateGoModuleSource(path, string(data)), nil
	}
	if f == nil {
		return load()
	}
	return f.goModules.get(path, load)
}

// unusedClientStub is the stub unusedClientModulePlugin serves for the module
// at path, "" when it serves none.
func (f *serverBundleFiles) unusedClientStub(path string) string {
	load := func() (string, error) {
		if strings.Contains(path, "node_modules") {
			return "", nil
		}
		data, err := os.ReadFile(path)
		if err != nil || !bundler.HasUseClientDirective(string(data)) {
			return "", nil
		}
		prog, errs := tsstrip.Parse(path, string(data), tsstrip.Options{})
		if prog == nil || len(errs) > 0 {
			return "", nil
		}
		return clientModuleStub(prog), nil
	}
	if f == nil {
		stub, _ := load()
		return stub
	}
	stub, _ := f.clientStubs.get(path, load)
	return stub
}
