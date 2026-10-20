package build

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/bundler"
	"github.com/kratejs/krate/packages/compiler/internal/config"
	"github.com/kratejs/krate/packages/compiler/internal/gohost"
	"github.com/kratejs/krate/packages/compiler/internal/gotsgen"
	"github.com/kratejs/krate/packages/compiler/internal/irtree"
)

// Type aliases and thin wrappers keep the serve layer and tests decoupled from
// the gohost package layout.
type goAPIRouteEntry = gohost.RouteEntry

func goAPIServerBinPath(root string) string { return gohost.HostBinaryPath(root) }

func loadGoAPIManifest(path string) ([]gohost.RouteEntry, time.Time, error) {
	return gohost.LoadManifest(path)
}

func goAPIRouteMatches(method, path string, routes []gohost.RouteEntry) bool {
	return gohost.RouteMatches(method, path, routes)
}

func writeGoAPIModule(modDir, apiSrcDir, root string, cfg config.GoCfg) (string, error) {
	return gohost.WriteModule(modDir, apiSrcDir, root, cfg)
}

// goInitHinted guards the one-time `krate go:init` nudge per process.
var goInitHinted bool

// BuildAllGoAPI scaffolds and compiles the Go host (API routes + components).
// Delegates to gohost; serialized so lazy dev rebuilds don't race.
func (b *Builder) BuildAllGoAPI() error {
	b.goHostMu.Lock()
	defer b.goHostMu.Unlock()
	info, err := gohost.Build(b.Root, b.Cfg)
	if err != nil {
		return err
	}
	if info == nil {
		_ = os.Remove(gohost.RoutesManifestPath(b.Root))
		return nil
	}
	printf("  %s▶%s Go host %d route(s), %d component(s) → %s\n", cGreen, cReset, len(info.Routes), info.Components, info.BinPath)
	if !goInitHinted && gohost.SetupMissing(b.Root) {
		goInitHinted = true
		fmt.Fprintf(os.Stderr, "  %s·%s Go editor intellisense isn't set up yet - run %skrate go:init%s\n", cYellow, cReset, cCyan, cReset)
	}
	return nil
}

// cachedGoComponents parses a Go component file once per build.
func (b *Builder) cachedGoComponents(path string) []gotsgen.Component {
	b.goParseMu.Lock()
	defer b.goParseMu.Unlock()
	if b.goParseCache == nil {
		b.goParseCache = map[string][]gotsgen.Component{}
	}
	if comps, ok := b.goParseCache[path]; ok {
		return comps
	}
	data, err := os.ReadFile(path)
	if err != nil {
		b.goParseCache[path] = nil
		return nil
	}
	comps, _ := gotsgen.ParseFile(path, data)
	b.goParseCache[path] = comps
	return comps
}

// collectGoComponents scans the bundled modules for `.go` imports and resolves
// each named/default import to its Go component (file, func, tier).
func (b *Builder) collectGoComponents(bundle *bundler.Bundle, root string) map[string]irtree.GoComponentRef {
	out := map[string]irtree.GoComponentRef{}
	for _, mod := range bundle.Modules {
		if mod == nil || mod.Program == nil || mod.Path == "" {
			continue
		}
		for _, stmt := range mod.Program.Body {
			imp, ok := stmt.(*ast.ImportStmt)
			if !ok {
				continue
			}
			src := strings.Trim(imp.Source, "\"'")
			if !strings.HasSuffix(src, ".go") {
				continue
			}
			abs := src
			if !filepath.IsAbs(abs) {
				abs = filepath.Join(filepath.Dir(mod.Path), src)
			}
			comps := b.cachedGoComponents(abs)
			if len(comps) == 0 {
				continue
			}
			byName := make(map[string]gotsgen.Component, len(comps))
			for _, c := range comps {
				byName[c.Name] = c
			}
			for _, ni := range imp.Named {
				if c, ok := byName[ni.Remote]; ok {
					out[ni.Local] = irtree.GoComponentRef{File: abs, Func: c.Name, Tier: goTierFromGotsgen(c.Tier)}
				}
			}
			if imp.Default != "" && len(comps) == 1 {
				out[imp.Default] = irtree.GoComponentRef{File: abs, Func: comps[0].Name, Tier: goTierFromGotsgen(comps[0].Tier)}
			}
		}
	}
	return out
}

// goTierFromGotsgen maps a Go component tier to the IR tier.
func goTierFromGotsgen(t gotsgen.Tier) irtree.ComponentTier {
	if t == gotsgen.TierRuntime {
		return irtree.TierRuntime
	}
	return irtree.TierServer
}

// renderGoComponents renders the collected build-time Go component requests via
// the host in one batch.
func (b *Builder) renderGoComponents(reqs []irtree.GoRenderRequest) (map[string]string, error) {
	if len(reqs) == 0 {
		return nil, nil
	}
	if _, err := os.Stat(gohost.HostBinaryPath(b.Root)); err != nil {
		if err := b.BuildAllGoAPI(); err != nil {
			return nil, err
		}
	}
	return gohost.Render(b.Root, reqs)
}

// substituteGoRenders replaces each Go-component marker with its rendered HTML.
func substituteGoRenders(html string, rendered map[string]string) string {
	for id, h := range rendered {
		html = strings.ReplaceAll(html, irtree.GoRenderMarker(id), h)
	}
	return html
}

// goBakedMap re-keys rendered Go HTML (by marker id) to source-derived call-site
// ids, so an SSR page bundle can inline a server-tier Go component at its call
// site (see irtree.GoCallID / serverbundle.go). A call-site id that maps to two
// different renders (same component at the same position in two files) is
// dropped, so those call sites fall back to a live host render instead of
// inlining the wrong HTML. Returns nil when there is nothing to bake.
func goBakedMap(reqs []irtree.GoRenderRequest, rendered map[string]string) map[string]string {
	if len(reqs) == 0 || len(rendered) == 0 {
		return nil
	}
	out := make(map[string]string, len(reqs))
	ambiguous := map[string]bool{}
	for _, r := range reqs {
		if r.CallID == "" || ambiguous[r.CallID] {
			continue
		}
		h, ok := rendered[r.ID]
		if !ok {
			continue
		}
		if prev, exists := out[r.CallID]; exists {
			if prev != h {
				delete(out, r.CallID)
				ambiguous[r.CallID] = true
			}
			continue
		}
		out[r.CallID] = h
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
