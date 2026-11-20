package build

import (
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"sync"

	"github.com/kratejs/krate/packages/compiler/internal/bundler"

	"github.com/kratejs/krate/packages/compiler/internal/diag"
)

// printTreeWarnings writes IR build warnings (positioned diagnostics) to w as
// `Warning: file:line:col: message` followed by the indented source line,
// caret and hint.
func printTreeWarnings(w io.Writer, warnings []diag.Diagnostic) {
	for _, d := range warnings {
		text := diag.FormatDiagnostics([]error{d})
		text = strings.ReplaceAll(text, "\n", "\n    ")
		fmt.Fprintf(w, "  %sWarning:%s %s\n", cYellow, cReset, text)
	}
}

// warningSet dedupes warnings within a build: a shared module is bundled once
// per page but its warnings should print once. Each Builder has its own, so
// concurrent builds do not swallow each other's warnings.
type warningSet struct {
	mu   sync.Mutex
	seen map[string]bool
}

// fresh returns the warnings of ds not seen before and records them.
func (s *warningSet) fresh(ds []diag.Diagnostic) []diag.Diagnostic {
	var out []diag.Diagnostic
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.seen == nil {
		s.seen = map[string]bool{}
	}
	for _, d := range ds {
		key := d.Error()
		if s.seen[key] {
			continue
		}
		s.seen[key] = true
		out = append(out, d)
	}
	return out
}

func (s *warningSet) reset() {
	s.mu.Lock()
	s.seen = nil
	s.mu.Unlock()
}

// sharedWarnings dedupes the warnings of callers that have no Builder.
var sharedWarnings warningSet

// printModuleWarnings prints the coded warnings attached to bundled modules
// (React rewrite diagnostics), each once per build.
func (b *Builder) printModuleWarnings(mods []*bundler.Module) {
	var ds []diag.Diagnostic
	for _, m := range mods {
		ds = append(ds, m.Diagnostics...)
	}
	printTreeWarnings(b.stderr(), b.warned.fresh(ds))
}

// envPending is a module's KR8310 and KR7116 warnings, held back until the
// build knows whether the module is shipped to the browser. KR7116 sits on the
// page's entry module: a server tree's entry is not browser code, and its
// client boundaries and server bundle are bundled apart (esbuild renames what
// clashes), so no shared namespace exists to clash in.
type envPending struct {
	path  string
	diags []diag.Diagnostic
}

// holdEnvWarnings takes the KR8310 and KR7116 warnings off the bundle's modules (copies;
// the parse cache shares the originals) and returns them.
func holdEnvWarnings(bundle *bundler.Bundle) []envPending {
	var held []envPending
	for i, m := range bundle.Modules {
		if m == nil || len(m.Diagnostics) == 0 {
			continue
		}
		var keep, env []diag.Diagnostic
		for _, d := range m.Diagnostics {
			if d.Code == diag.CodeEnvNotExposed || d.Code == diag.CodeExportCollision {
				env = append(env, d)
			} else {
				keep = append(keep, d)
			}
		}
		if len(env) == 0 {
			continue
		}
		cp := *m
		cp.Diagnostics = keep
		bundle.Modules[i] = &cp
		held = append(held, envPending{path: m.Path, diags: env})
	}
	return held
}

// releaseEnvWarnings prints the held KR8310 warnings that apply and returns
// them. A page without a server tree ships all its modules. In a server tree
// only the modules a client boundary reaches go to the browser; the others run
// in the server renderer, where process.env is the real environment.
func (b *Builder) releaseEnvWarnings(held []envPending, bundle *bundler.Bundle, boundaries []ClientBoundary, serverTree bool) []diag.Diagnostic {
	if len(held) == 0 {
		return nil
	}
	var browser map[string]bool
	if serverTree {
		browser = b.clientCode(bundle, boundaries, held)
	}
	var mods []*bundler.Module
	var out []diag.Diagnostic
	for _, h := range held {
		if browser != nil && !browser[filepath.Clean(h.path)] && !browser[canonicalPath(h.path)] {
			continue
		}
		diags := make([]diag.Diagnostic, len(h.diags))
		for i, d := range h.diags {
			if d.Code == diag.CodeEnvNotExposed {
				d.Hint = b.envNotExposedHint(d)
			}
			diags[i] = d
		}
		mods = append(mods, &bundler.Module{Path: h.path, Diagnostics: diags})
		out = append(out, diags...)
	}
	b.printModuleWarnings(mods)
	return out
}

// clientCode returns the modules that go to the browser in a server tree: the
// files in the chunks of the page's client boundaries. A boundary whose chunk
// was not measured (real React) counts every module it imports. The answer is
// exact for the modules of held.
func (b *Builder) clientCode(bundle *bundler.Bundle, boundaries []ClientBoundary, held []envPending) map[string]bool {
	out := map[string]bool{}
	var unmeasured []ClientBoundary
	var infos []clientChunkInfo
	b.clientChunkMu.Lock()
	for _, cb := range boundaries {
		if cb.Spec != "" {
			continue
		}
		info, ok := b.clientChunks[cb.SourcePath]
		if !ok || info.inputs == nil {
			unmeasured = append(unmeasured, cb)
			continue
		}
		infos = append(infos, info)
	}
	b.clientChunkMu.Unlock()
	for _, info := range infos {
		if f := info.split; f != nil {
			// The split batch's inputs are an upper bound: a held module
			// only its shared chunks hold needs the chunk's own check.
			for _, h := range held {
				p, c := filepath.Clean(h.path), canonicalPath(h.path)
				if (info.inputs[p] || info.inputs[c]) && !f.ownInputs[p] && !f.ownInputs[c] {
					if exact, _, err := b.exactChunk(info); err == nil {
						info = exact
					}
					break
				}
			}
		}
		for p := range info.inputs {
			out[p] = true
		}
	}
	for p := range clientReachable(bundle, unmeasured) {
		out[p] = true
	}
	return out
}

// clientReachable returns the modules a client boundary imports, directly or
// not: the code of the boundaries' browser chunks.
func clientReachable(bundle *bundler.Bundle, boundaries []ClientBoundary) map[string]bool {
	byPath := map[string]*bundler.Module{}
	for _, m := range bundle.Modules {
		if m != nil && !m.IsExternal && m.Program != nil {
			byPath[filepath.Clean(m.Path)] = m
			byPath[canonicalPath(m.Path)] = m
		}
	}
	seen := map[string]bool{}
	var queue []*bundler.Module
	visit := func(m *bundler.Module) {
		if m != nil && !seen[filepath.Clean(m.Path)] {
			seen[filepath.Clean(m.Path)] = true
			seen[canonicalPath(m.Path)] = true
			queue = append(queue, m)
		}
	}
	for _, cb := range boundaries {
		if cb.Spec == "" {
			visit(byPath[filepath.Clean(cb.SourcePath)])
			visit(byPath[canonicalPath(cb.SourcePath)])
		}
	}
	for len(queue) > 0 {
		m := queue[0]
		queue = queue[1:]
		for _, imp := range m.Imports {
			if bundle.Resolver == nil {
				break
			}
			if resolved := bundle.Resolver.Resolve(m.Path, imp); resolved != "" {
				visit(byPath[filepath.Clean(resolved)])
				visit(byPath[canonicalPath(resolved)])
			}
		}
	}
	return seen
}
