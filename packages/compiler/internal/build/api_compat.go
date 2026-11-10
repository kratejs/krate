package build

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/kratejs/krate/packages/compiler/internal/diag"
	"github.com/kratejs/krate/packages/compiler/internal/fsutil"
	"github.com/kratejs/krate/packages/compiler/internal/jsruntime"
)

// embeddedRuntime reports whether a runtime option (ssr.apiRuntime or
// ssr.middlewareRuntime) selects the embedded QuickJS runtime.
func embeddedRuntime(option string) bool {
	switch strings.ToLower(option) {
	case "", "quickjs":
		return true
	}
	return false
}

// esbuildBundledInputs returns the absolute paths of the source files that
// contribute code to a bundle, read from its esbuild metafile. Inputs that were
// tree-shaken away entirely are left out.
func esbuildBundledInputs(metafile string) []string {
	var meta struct {
		Inputs  map[string]json.RawMessage `json:"inputs"`
		Outputs map[string]struct {
			Inputs map[string]struct {
				BytesInOutput int `json:"bytesInOutput"`
			} `json:"inputs"`
		} `json:"outputs"`
	}
	if metafile == "" || json.Unmarshal([]byte(metafile), &meta) != nil {
		return nil
	}
	used := map[string]bool{}
	for _, out := range meta.Outputs {
		for in, info := range out.Inputs {
			if info.BytesInOutput > 0 {
				used[in] = true
			}
		}
	}
	// esbuild writes input paths relative to its working directory, which
	// defaults to the process working directory.
	cwd, _ := os.Getwd()
	var files []string
	for in := range meta.Inputs {
		if !used[in] || esbuildNamespaced(in) {
			continue
		}
		p := filepath.FromSlash(in)
		if !filepath.IsAbs(p) {
			p = filepath.Join(cwd, p)
		}
		files = append(files, filepath.Clean(p))
	}
	sort.Strings(files)
	return files
}

// esbuildNamespaced reports whether a metafile input lives in a plugin
// namespace ("ns:path") instead of on disk.
func esbuildNamespaced(in string) bool {
	i := strings.Index(in, ":")
	if i <= 0 {
		return false
	}
	// A Windows drive letter ("C:\...") is a real path.
	return !(i == 1 && len(in) > 2 && (in[2] == '\\' || in[2] == '/'))
}

// scanRuntimeCompat scans the sources of a bundle built for the embedded
// runtime, reports KR7160 for the unsupported APIs they use, and returns the
// ledger features they reference (nil when the scan could not see everything).
// option is jsruntime.CompatAPIRuntime or jsruntime.CompatMiddlewareRuntime.
func (b *Builder) scanRuntimeCompat(metafile, option string) *[]string {
	scan := jsruntime.ScanCompatFiles(esbuildBundledInputs(metafile))
	if ds := jsruntime.CompatDiagnostics(scan, b.Root, option); len(ds) > 0 {
		b.runtimeCompatMu.Lock()
		b.runtimeCompatWarnings = append(b.runtimeCompatWarnings, ds...)
		b.runtimeCompatMu.Unlock()
		printWarningsOnce(os.Stderr, ds)
	}
	if !scan.Complete {
		return nil
	}
	features := scan.Features()
	return &features
}

// RuntimeCompatWarnings returns the KR7160 warnings reported so far.
func (b *Builder) RuntimeCompatWarnings() []diag.Diagnostic {
	b.runtimeCompatMu.Lock()
	defer b.runtimeCompatMu.Unlock()
	return append([]diag.Diagnostic(nil), b.runtimeCompatWarnings...)
}

// middlewareMeta is .krate/middleware.json, written next to middleware.js.
type middlewareMeta struct {
	// Features lists the compat ledger features the middleware sources
	// reference; absent means unknown (load everything).
	Features *[]string `json:"features,omitempty"`
}

func middlewareMetaPath(root string) string {
	return filepath.Join(root, ".krate", "middleware.json")
}

func writeMiddlewareMeta(root string, features *[]string) error {
	data, err := json.MarshalIndent(middlewareMeta{Features: features}, "", "  ")
	if err != nil {
		return err
	}
	return fsutil.WriteFileAtomic(middlewareMetaPath(root), data, 0o644)
}
