package build

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/kratejs/krate/packages/compiler/ast"
	"github.com/kratejs/krate/packages/compiler/internal/bundler"
	"github.com/kratejs/krate/packages/compiler/internal/diag"
)

// dataCacheUsage summarizes how a module reads data through the request-time
// data cache (Next.js-style fetch options and @krate/runtime/cache helpers).
// It drives the page render mode when the page states none explicitly: a
// static (SSG) page is baked once at build time, so revalidate windows, cache
// tags and no-store reads would be silently frozen otherwise.
type dataCacheUsage struct {
	// noStore: fetch(..., { cache: "no-store" }), next.revalidate = 0, or
	// unstable_noStore(). The page must render per request.
	noStore    bool
	noStorePos ast.Pos
	// tagged: a fetch or unstable_cache call with non-empty tags. The page is
	// cached (ISR) so revalidateTag can invalidate it.
	tagged    bool
	taggedPos ast.Pos
	// minRevalidate is the smallest positive revalidate window (seconds) a
	// fetch or unstable_cache call declares; 0 when none does.
	minRevalidate int
	revalidatePos ast.Pos
}

// cacheModuleSources are the import sources that provide unstable_cache and
// unstable_noStore.
var cacheModuleSources = map[string]bool{
	"@krate/runtime/cache":  true,
	"@krate/runtime/server": true,
	"next/cache":            true,
}

// merge folds another module's usage into u.
func (u *dataCacheUsage) merge(o dataCacheUsage) {
	if o.noStore && !u.noStore {
		u.noStore, u.noStorePos = true, o.noStorePos
	}
	if o.tagged && !u.tagged {
		u.tagged, u.taggedPos = true, o.taggedPos
	}
	if o.minRevalidate > 0 && (u.minRevalidate == 0 || o.minRevalidate < u.minRevalidate) {
		u.minRevalidate, u.revalidatePos = o.minRevalidate, o.revalidatePos
	}
}

// detectDataCacheUsage scans one module for data-cache reads.
func detectDataCacheUsage(prog *ast.Program) dataCacheUsage {
	var u dataCacheUsage
	if prog == nil {
		return u
	}
	cacheFn := map[string]bool{}
	noStoreFn := map[string]bool{}
	for _, stmt := range prog.Body {
		imp, ok := stmt.(*ast.ImportStmt)
		if !ok || !cacheModuleSources[strings.Trim(imp.Source, "\"'`")] {
			continue
		}
		for _, n := range imp.Named {
			switch n.Remote {
			case "unstable_cache":
				cacheFn[n.Local] = true
			case "unstable_noStore":
				noStoreFn[n.Local] = true
			}
		}
	}
	walkProgramExprs(prog, func(e ast.Expr) bool {
		call, ok := e.(*ast.CallExpr)
		if !ok {
			return true
		}
		name := dataCalleeName(call.Callee)
		switch {
		case name == "fetch":
			if len(call.Args) >= 2 {
				if opts, ok := call.Args[1].(*ast.ObjectExpr); ok {
					u.fetchOptions(opts, call.Position)
				}
			}
		case cacheFn[name]:
			if len(call.Args) >= 3 {
				if opts, ok := call.Args[2].(*ast.ObjectExpr); ok {
					u.cacheOptions(opts, call.Position)
				}
			}
		case noStoreFn[name]:
			if !u.noStore {
				u.noStore, u.noStorePos = true, call.Position
			}
		}
		return true
	})
	return u
}

// dataCalleeName returns "fetch" for fetch / globalThis.fetch / window.fetch and
// the identifier name for a plain identifier callee.
func dataCalleeName(e ast.Expr) string {
	switch v := e.(type) {
	case *ast.Identifier:
		return v.Name
	case *ast.MemberExpr:
		obj, ok := v.Object.(*ast.Identifier)
		prop, ok2 := v.Property.(*ast.Identifier)
		if ok && ok2 && prop.Name == "fetch" && (obj.Name == "globalThis" || obj.Name == "window" || obj.Name == "self") {
			return "fetch"
		}
	}
	return ""
}

func (u *dataCacheUsage) fetchOptions(opts *ast.ObjectExpr, pos ast.Pos) {
	for _, p := range opts.Properties {
		switch p.Key {
		case "cache":
			if lit, ok := p.Value.(*ast.Literal); ok && lit.Kind == ast.StringLit && lit.Value == "no-store" && !u.noStore {
				u.noStore, u.noStorePos = true, pos
			}
		case "next":
			if next, ok := p.Value.(*ast.ObjectExpr); ok {
				u.cacheOptions(next, pos)
			}
		}
	}
}

// cacheOptions reads { revalidate, tags } (fetch `next` options or the
// unstable_cache options argument).
func (u *dataCacheUsage) cacheOptions(opts *ast.ObjectExpr, pos ast.Pos) {
	for _, p := range opts.Properties {
		switch p.Key {
		case "revalidate":
			n, ok := constNumber(p.Value)
			if !ok {
				continue
			}
			if n == 0 {
				if !u.noStore {
					u.noStore, u.noStorePos = true, pos
				}
			} else if n > 0 && (u.minRevalidate == 0 || int(n) < u.minRevalidate) {
				u.minRevalidate, u.revalidatePos = int(n), pos
			}
		case "tags":
			if arr, ok := p.Value.(*ast.ArrayExpr); ok && len(arr.Elements) > 0 && !u.tagged {
				u.tagged, u.taggedPos = true, pos
			} else if !ok && !u.tagged {
				// A computed tag list still tags the read.
				u.tagged, u.taggedPos = true, pos
			}
		}
	}
}

// taggedOnlyISRRevalidate is the ISR window for a page whose cached reads
// carry tags but no time-based revalidate: the page stays cached until a
// revalidateTag/revalidatePath call invalidates it (one year as the timer).
const taggedOnlyISRRevalidate = 365 * 24 * 60 * 60

// inferRenderModeFromData returns the render mode implied by data-cache usage
// for a page that states no mode itself, and ok=false when the usage implies
// nothing (no tagged, timed or no-store reads).
func inferRenderModeFromData(u dataCacheUsage) (RenderMode, int, string, bool) {
	switch {
	case u.noStore:
		return RenderSSR, 0, "uncached (no-store) data read", true
	case u.minRevalidate > 0:
		return RenderISR, u.minRevalidate, "data read with a revalidate window", true
	case u.tagged:
		return RenderISR, taggedOnlyISRRevalidate, "tagged data read", true
	}
	return RenderSSG, 0, "", false
}

// pageDataCacheUsage merges the data-cache usage of a page's local modules
// (third-party packages are not scanned).
func pageDataCacheUsage(mods []*bundler.Module) dataCacheUsage {
	var u dataCacheUsage
	for _, m := range mods {
		if m == nil || m.Program == nil || m.IsExternal || strings.Contains(filepath.ToSlash(m.Path), "/node_modules/") {
			continue
		}
		u.merge(detectDataCacheUsage(m.Program))
	}
	return u
}

// resolvePageRenderMode is detectRenderMode plus the build-time reporting of
// unsupported segment-export values and the data-cache inference: a page that
// states no mode but reads tagged, timed or no-store data renders as ISR/SSR
// instead of being frozen at build time.
func resolvePageRenderMode(page string, entry *bundler.Module, mods []*bundler.Module) (RenderMode, int, dataCacheUsage) {
	cfg := parsePageConfig(entry.Program)
	var issues []diag.Diagnostic
	for _, is := range cfg.issues {
		issues = append(issues, warnAt(page, entry.SourceCode, is.pos, diag.CodeSegmentExportValue, is.message, is.hint))
	}
	printWarningsOnce(os.Stderr, issues)

	usage := pageDataCacheUsage(mods)
	mode, reval := detectRenderMode(entry.Program)
	if _, _, explicit := explicitRenderMode(cfg); explicit || mode != RenderSSG {
		return mode, reval, usage
	}
	if m, r, reason, ok := inferRenderModeFromData(usage); ok {
		printf("  %s⚡%s %s → %s (%s)\n", cCyan, cReset, filepath.Base(page), m, reason)
		return m, r, usage
	}
	return mode, reval, usage
}

// staticDataCacheWarning reports a page that ends up static (SSG) although it
// reads data through tagged, timed or no-store cache options: the HTML is
// baked once, so revalidateTag/revalidatePath and revalidate windows cannot
// refresh it.
func staticDataCacheWarning(page, source string, u dataCacheUsage, why string) (diag.Diagnostic, bool) {
	var pos ast.Pos
	var what string
	switch {
	case u.tagged:
		pos, what = u.taggedPos, "reads tagged data (fetch next.tags / unstable_cache tags)"
	case u.minRevalidate > 0:
		pos, what = u.revalidatePos, "reads data with a revalidate window"
	case u.noStore:
		pos, what = u.noStorePos, "reads uncached (no-store) data"
	default:
		return diag.Diagnostic{}, false
	}
	msg := fmt.Sprintf("page %s but is rendered statically (%s); its HTML is baked at build time and revalidateTag/revalidatePath cannot refresh it", what, why)
	hint := "remove the static pin (`dynamic = \"force-static\"`, `revalidate = false`, `output: \"static\"`) or export `revalidate = N` to make the page ISR"
	return warnAt(page, source, pos, diag.CodeStaticDataCache, msg, hint), true
}

// warnAt builds a coded warning diagnostic with the source line for a caret.
func warnAt(file, source string, pos ast.Pos, code diag.Code, msg, hint string) diag.Diagnostic {
	line := ""
	if pos.Line > 0 {
		lines := strings.Split(source, "\n")
		if pos.Line-1 < len(lines) {
			line = lines[pos.Line-1]
		}
	}
	d := diag.New(file, pos.Line, pos.Col, msg, hint, line).WithCode(code)
	d.Severity = diag.SeverityWarning
	return d
}

// printWarningsOnce prints warnings not printed earlier in this build (pages
// expanded per generateStaticParams entry would otherwise repeat them).
func printWarningsOnce(w io.Writer, ds []diag.Diagnostic) {
	var out []diag.Diagnostic
	moduleWarningsSeen.mu.Lock()
	for _, d := range ds {
		key := d.Error()
		if moduleWarningsSeen.seen[key] {
			continue
		}
		moduleWarningsSeen.seen[key] = true
		out = append(out, d)
	}
	moduleWarningsSeen.mu.Unlock()
	printTreeWarnings(w, out)
}
