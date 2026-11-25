package build

import (
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/kratejs/krate/packages/compiler/internal/diag"
)

// devWarningCap is the most build warnings the dev server sends a tab.
const devWarningCap = 100

// devWarningStore keeps the warnings of the latest build of every page, by
// page source, so a partial rebuild replaces only the pages it rebuilt and
// the overlay still lists the warnings of the pages it did not touch.
type devWarningStore struct {
	mu     sync.Mutex
	byPage map[string][]diag.Diagnostic
}

// record stores the warnings of results. A full build (rebuilt nil) replaces
// everything; otherwise the rebuilt pages lose their old warnings first, so a
// page whose warnings are fixed (or that failed to build) drops them.
func (s *devWarningStore) record(results []*PageResult, rebuilt []string, full bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if full || s.byPage == nil {
		s.byPage = map[string][]diag.Diagnostic{}
	}
	for _, p := range rebuilt {
		delete(s.byPage, filepath.Clean(p))
	}
	fresh := map[string]bool{}
	for _, r := range results {
		if r == nil {
			continue
		}
		key := filepath.Clean(r.Page)
		if !fresh[key] {
			// A page expanded by generateStaticParams yields several results
			// with the same source: keep them all, from this build only.
			fresh[key] = true
			delete(s.byPage, key)
		}
		for _, d := range r.Warnings {
			if d.IsWarning() {
				s.byPage[key] = append(s.byPage[key], d)
			}
		}
	}
}

// list returns the stored warnings deduplicated (a component shared by many
// pages warns once), sorted by location, with files relative to root, and at
// most devWarningCap of them.
func (s *devWarningStore) list(root string) []diag.Diagnostic {
	s.mu.Lock()
	var all []diag.Diagnostic
	for _, ds := range s.byPage {
		all = append(all, ds...)
	}
	s.mu.Unlock()
	return dedupeDevWarnings(root, all)
}

func dedupeDevWarnings(root string, ds []diag.Diagnostic) []diag.Diagnostic {
	out := []diag.Diagnostic{}
	seen := map[string]bool{}
	for _, d := range ds {
		if d.File != "" {
			d.File = displayRel(root, d.File)
		}
		key := strings.Join([]string{string(d.Code), d.File, strconv.Itoa(d.Line), strconv.Itoa(d.Col), d.Message}, "|")
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, d)
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.File != b.File {
			return a.File < b.File
		}
		if a.Line != b.Line {
			return a.Line < b.Line
		}
		if a.Col != b.Col {
			return a.Col < b.Col
		}
		return a.Code < b.Code
	})
	if len(out) > devWarningCap {
		out = out[:devWarningCap]
	}
	return out
}

// recordDevWarnings keeps the warnings of a dev build's pages (no-op outside
// dev). rebuilt are the page sources a partial build was asked to rebuild.
func (b *Builder) recordDevWarnings(results []*PageResult, rebuilt []string, full bool) {
	if !b.DevMode {
		return
	}
	b.devWarnings.record(results, rebuilt, full)
}

// DevWarnings returns the build warnings of the pages as last built, for the
// dev overlay.
func (b *Builder) DevWarnings() []diag.Diagnostic {
	return b.devWarnings.list(b.Root)
}
