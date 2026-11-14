package build

import (
	"fmt"
	"io"
	"os"
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

// moduleWarningsSeen dedupes module warnings within a build: a shared module
// is bundled once per page but its warnings should print once.
var moduleWarningsSeen = struct {
	mu   sync.Mutex
	seen map[string]bool
}{seen: map[string]bool{}}

func resetModuleWarnings() {
	moduleWarningsSeen.mu.Lock()
	moduleWarningsSeen.seen = map[string]bool{}
	moduleWarningsSeen.mu.Unlock()
}

// printModuleWarnings prints the coded warnings attached to bundled modules
// (React rewrite diagnostics), each once per build.
func printModuleWarnings(w io.Writer, mods []*bundler.Module) {
	var out []diag.Diagnostic
	moduleWarningsSeen.mu.Lock()
	for _, m := range mods {
		for _, d := range m.Diagnostics {
			key := d.Error()
			if moduleWarningsSeen.seen[key] {
				continue
			}
			moduleWarningsSeen.seen[key] = true
			out = append(out, d)
		}
	}
	moduleWarningsSeen.mu.Unlock()
	printTreeWarnings(w, out)
}

// envPending is a module's KR8310 warnings, held back until the build knows
// whether the module is shipped to the browser.
type envPending struct {
	path  string
	diags []diag.Diagnostic
}

// holdEnvWarnings takes the KR8310 warnings off the bundle's modules (copies,
// the parse cache shares the originals) and returns them.
func holdEnvWarnings(bundle *bundler.Bundle) []envPending {
	var held []envPending
	for i, m := range bundle.Modules {
		if m == nil || len(m.Diagnostics) == 0 {
			continue
		}
		var keep, env []diag.Diagnostic
		for _, d := range m.Diagnostics {
			if d.Code == diag.CodeEnvNotExposed {
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
		browser = b.clientCode(bundle, boundaries)
	}
	var mods []*bundler.Module
	var out []diag.Diagnostic
	for _, h := range held {
		if browser != nil && !browser[filepath.Clean(h.path)] && !browser[canonicalPath(h.path)] {
			continue
		}
		mods = append(mods, &bundler.Module{Path: h.path, Diagnostics: h.diags})
		out = append(out, h.diags...)
	}
	printModuleWarnings(os.Stderr, mods)
	return out
}

// clientCode returns the modules that go to the browser in a server tree: the
// files in the chunks of the page's client boundaries. A boundary whose chunk
// was not measured (real React) counts every module it imports.
func (b *Builder) clientCode(bundle *bundler.Bundle, boundaries []ClientBoundary) map[string]bool {
	out := map[string]bool{}
	var unmeasured []ClientBoundary
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
		for p := range info.inputs {
			out[p] = true
		}
	}
	b.clientChunkMu.Unlock()
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
