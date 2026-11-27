package build

import (
	"path/filepath"
	"sync"
)

// A dev rebuild builds the pages first and their server bundles after all of
// them. When the server render modules the bundles import are the ones of the
// last rebuild (see ssrMemo), a page's bundle does not have to wait for the
// other pages: it is compiled by the page's own worker as soon as the page is
// built, so the esbuild work overlaps the pages that are still building
// instead of following them.
type earlyServerBundles struct {
	mu      sync.Mutex
	run     *serverBundleRun
	memo    *buildMemo
	global  string
	checked sync.Once
	usable  bool
	done    map[string]string
}

// beginEarlyBundles prepares the early bundles of a rebuild of pages, or none
// when nothing is kept from the last one.
func (b *Builder) beginEarlyBundles() {
	b.early = nil
	m := b.ssrMemo
	if !b.DevMode || m == nil || m.parts == nil {
		return
	}
	impls := &clientSSRImpls{res: b.importResolver(), root: b.Root, bundleDir: filepath.Join(b.Cfg.OutDir, ".krate", "server-bundles"), actions: b.actionModules(), basePath: b.Cfg.BaseURLPath()}
	run := newServerBundleRun(impls, b.Root, b.Cfg.OutDir, b.Cfg.Sourcemap, b.actionModules(), b.Cfg.BaseURLPath())
	run.authBanner = authBanner(b.Cfg)
	b.early = &earlyServerBundles{
		run:    run,
		memo:   m,
		global: b.ssrGlobalSig(),
		done:   map[string]string{},
	}
}

// compileEarly compiles the server bundle of a page that was just built, when
// every server render module it imports is the one the last rebuild wrote. A
// bundle that cannot be built is left to the later pass, which reports why.
func (b *Builder) compileEarly(page *PageResult) {
	e := b.early
	if e == nil || page == nil || page.Mode == RenderSSG || page.SourcePath == "" || !pageNeedsNodeServerBundle(page) {
		return
	}
	e.checked.Do(func() {
		e.usable = e.memo.global == e.global && e.memo.stamps.fresh() && e.memo.config.fresh()
	})
	if !e.usable {
		return
	}
	for _, cb := range page.ClientBoundaries {
		if cb.Real {
			continue
		}
		if sig, ok := e.memo.parts[cb.SourcePath+"\x00"+cb.Spec]; !ok || sig != ssrBoundarySig(cb) {
			return
		}
	}
	for _, ci := range e.run.impls.claim(page.ClientBoundaries) {
		ci.e.finish(func() error { return nil })
	}
	rel, failures := e.run.compile(page, func(string, ...any) {})
	if rel == "" || len(failures) > 0 {
		return
	}
	e.mu.Lock()
	e.done[page.SourcePath] = rel
	e.mu.Unlock()
	b.Prof.Add("dev:early-server-bundles", 0)
}

// earlyBundles returns the bundles compiled early in this rebuild, by page
// source, and forgets them.
func (b *Builder) earlyBundles() map[string]string {
	e := b.early
	b.early = nil
	if e == nil {
		return nil
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.done
}
