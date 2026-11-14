package build

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/kratejs/krate/packages/compiler/internal/diag"
	"github.com/kratejs/krate/packages/compiler/internal/fsutil"
	"github.com/kratejs/krate/packages/compiler/internal/renderer"
)

// Build-time pieces of segment pages: the generated sources that adapt a
// user's error.tsx to the two places it runs (a client error boundary around
// the segment's children, and the server-rendered 500 page).

// Placeholders the server-rendered error page carries until a request fills
// them in (see FillServerError).
const (
	errSentinelName    = "__KRATE_ERR_NAME__"
	errSentinelMessage = "__KRATE_ERR_MESSAGE__"
	errSentinelDigest  = "__KRATE_ERR_DIGEST__"
	errSentinelStack   = "__KRATE_ERR_STACK__"
)

// segmentVariants records which source variant of an error page is being built
// (see errorPageSource).
var segmentVariants sync.Map // page path -> "static" | "client"

// segmentGenDir is where generated segment sources live. It is not under
// .krate/gen, which the docs plugin clears on every build.
func (b *Builder) segmentGenDir() string {
	return filepath.Join(b.Root, ".krate", "segments")
}

// segmentGenPath returns the generated file for a user file and a purpose.
func (b *Builder) segmentGenPath(userFile, purpose string) string {
	key := userFile
	if rel, err := filepath.Rel(b.Root, userFile); err == nil && !strings.HasPrefix(rel, "..") {
		key = rel
	}
	sum := sha256.Sum256([]byte(filepath.ToSlash(key)))
	return filepath.Join(b.segmentGenDir(), hex.EncodeToString(sum[:])[:12], purpose+".tsx")
}

// importSpec returns the module specifier that reaches target from the
// generated file at from.
func importSpec(from, target string) string {
	rel, err := filepath.Rel(filepath.Dir(from), target)
	if err != nil {
		rel = target
	}
	rel = filepath.ToSlash(rel)
	switch filepath.Ext(rel) {
	case ".tsx", ".ts", ".jsx", ".js":
		rel = strings.TrimSuffix(rel, filepath.Ext(rel))
	}
	if !strings.HasPrefix(rel, ".") && !filepath.IsAbs(target) {
		rel = "./" + rel
	}
	return rel
}

// errorBoundarySource is the generated layout that makes a segment's error.tsx
// a client error boundary around the segment's children. It reuses the class
// component boundary machinery, so render, effect and reactive errors below it
// show the error page in place and reset() re-mounts the children.
func errorBoundarySource(spec, id string) string {
	cls, layout := "KrateSegmentBoundary"+id, "KrateSegmentLayout"+id
	return "import { Component } from \"react\";\n" +
		"import ErrorPage from " + fmt.Sprintf("%q", spec) + ";\n\n" +
		"class " + cls + " extends Component {\n" +
		"  state = { error: null };\n" +
		"  static getDerivedStateFromError(error: any) { return { error }; }\n" +
		"  componentDidCatch(error: any) { console.error(error); }\n" +
		"  reset = () => this.setState({ error: null });\n" +
		"  render() {\n" +
		"    if (this.state.error) return <ErrorPage error={this.state.error} reset={this.reset} />;\n" +
		"    return this.props.children;\n" +
		"  }\n" +
		"}\n\n" +
		"export default function " + layout + "(props: any) {\n" +
		"  return <" + cls + "><div style={{ display: \"contents\" }}>{props.children}</div></" + cls + ">;\n" +
		"}\n"
}

// errorFallbackSource is the generated client component a request-time error
// boundary renders when its page throws. The content it replaced came from the
// server, so reset() reloads the page.
func errorFallbackSource(spec string) string {
	return "'use client';\n" +
		"import ErrorPage from " + fmt.Sprintf("%q", spec) + ";\n\n" +
		"export default function KrateBoundaryFallback(props: any) {\n" +
		"  return <ErrorPage error={props.error} reset={() => location.reload()} />;\n" +
		"}\n"
}

// serverBoundarySource is the generated layout that makes a segment's error.tsx
// the error boundary of a page that renders per request (under an async
// layout). Unlike errorBoundarySource it runs only in the SSR sidecar.
func serverBoundarySource() string {
	return "import { ServerErrorBoundary } from \"@krate/runtime/server\";\n" +
		"import Fallback from \"./fallback\";\n\n" +
		"export default function KrateServerBoundary(props: any) {\n" +
		"  return <ServerErrorBoundary fallback={Fallback}>{props.children}</ServerErrorBoundary>;\n" +
		"}\n"
}

// serverBoundaryLayout returns the generated request-time boundary layout for
// an error.tsx.
func (b *Builder) serverBoundaryLayout(errFile string) (string, error) {
	path := b.segmentGenPath(errFile, "server-boundary")
	fallback := b.segmentGenPath(errFile, "fallback")
	if err := b.writeSegmentGen(fallback, errorFallbackSource(importSpec(fallback, errFile))); err != nil {
		return "", fmt.Errorf("writing generated error fallback for %s: %w", errFile, err)
	}
	if err := b.writeSegmentGen(path, serverBoundarySource()); err != nil {
		return "", fmt.Errorf("writing generated server error boundary for %s: %w", errFile, err)
	}
	return path, nil
}

// errorPageSource is the generated entry for the server-rendered error page.
// The static variant bakes placeholders into the HTML, which the server
// replaces per request; a page whose error component needs client code uses
// the client variant, where the component reads the error the server injects as
// a JSON data block (#__krate_error), and the HTML carries no text for it until
// the page hydrates.
func errorPageSource(spec, variant string) string {
	if variant == "client" {
		// The error lives in component state, so the build treats the page as
		// client code: the HTML carries no text for it and hydration fills it in.
		return "import { useState } from \"react\";\n" +
			"import ErrorPage from " + fmt.Sprintf("%q", spec) + ";\n\n" +
			"function readServerError(): any {\n" +
			"  const el = typeof document !== \"undefined\" ? document.getElementById(\"__krate_error\") : null;\n" +
			"  try {\n" +
			"    return JSON.parse((el && el.textContent) || \"null\") || { name: \"Error\", message: \"\", digest: \"\", stack: \"\" };\n" +
			"  } catch (e) {\n" +
			"    return { name: \"Error\", message: \"\", digest: \"\", stack: \"\" };\n" +
			"  }\n" +
			"}\n\n" +
			"export default function KrateServerError() {\n" +
			"  const [error] = useState(readServerError);\n" +
			"  return <ErrorPage error={error} reset={() => location.reload()} />;\n" +
			"}\n"
	}
	errExpr := "{ name: \"" + errSentinelName + "\", message: \"" + errSentinelMessage + "\", digest: \"" + errSentinelDigest + "\", stack: \"" + errSentinelStack + "\" }"
	return "import ErrorPage from " + fmt.Sprintf("%q", spec) + ";\n\n" +
		"const krateServerError: any = " + errExpr + ";\n\n" +
		"export default function KrateServerError() {\n" +
		"  return <ErrorPage error={krateServerError} reset={() => location.reload()} />;\n" +
		"}\n"
}

// writeSegmentGen writes a generated source (only when it changed, so module
// caches and mtimes stay stable). Pages build in parallel and share these files,
// so a source is written once: on Windows, replacing a file fails a concurrent
// open by another page with a sharing violation.
func (b *Builder) writeSegmentGen(path, src string) error {
	b.segmentGenMu.Lock()
	defer b.segmentGenMu.Unlock()
	if b.segmentGen[path] == src {
		if _, err := os.Stat(path); err == nil {
			return nil
		}
	}
	if err := fsutil.WriteFileIfChanged(path, []byte(src), 0o644); err != nil {
		return err
	}
	if b.segmentGen == nil {
		b.segmentGen = map[string]string{}
	}
	b.segmentGen[path] = src
	return nil
}

// boundaryLayout returns the generated boundary layout for an error.tsx.
func (b *Builder) boundaryLayout(errFile string) (string, error) {
	path := b.segmentGenPath(errFile, "boundary")
	id := filepath.Base(filepath.Dir(path))[:8]
	if err := b.writeSegmentGen(path, errorBoundarySource(importSpec(path, errFile), id)); err != nil {
		return "", fmt.Errorf("writing generated error boundary for %s: %w", errFile, err)
	}
	return path, nil
}

// routeStack is findRouteStack with each error.tsx replaced by its generated
// boundary layout. A generation failure leaves the user's file in place, which
// then fails the page's build with its own diagnostic.
func (b *Builder) routeStack(page string) []routeBoundary {
	stack := findRouteStack(page, b.Cfg.PagesDir)
	for i, rb := range stack {
		if rb.Kind != "error" {
			continue
		}
		if gen, err := b.boundaryLayout(rb.Path); err == nil {
			stack[i].Origin = rb.Path
			stack[i].Path = gen
		}
	}
	return stack
}

// segmentEntryFile returns the file buildPage bundles for page: the page
// itself, or for a segment error page the generated entry that supplies its
// error and reset props.
func (b *Builder) segmentEntryFile(page string) (string, error) {
	kind, ok := segmentKindOfFile(page)
	if !ok || kind != SegmentError {
		return page, nil
	}
	variant := "static"
	if v, ok := segmentVariants.Load(page); ok {
		variant = v.(string)
	}
	path := b.segmentGenPath(page, "error-page")
	if err := b.writeSegmentGen(path, errorPageSource(importSpec(path, page), variant)); err != nil {
		return "", diag.Diagnostic{File: page, Line: 1, Col: 1, Message: "writing the generated error page entry: " + err.Error(), Hint: "check that .krate is writable"}
	}
	return path, nil
}

// addSegmentsToManifest records the built segment pages and each route's
// source directory in the manifest.
func (b *Builder) addSegmentsToManifest(m *Manifest, results []*PageResult) {
	var pages []string
	for _, r := range results {
		if r != nil && r.IsErrorPage && r.Page != "" {
			pages = append(pages, r.Page)
		}
	}
	m.Segments = segmentEntries(pages, b.Cfg.PagesDir)
	for _, r := range results {
		if r == nil || !r.isDynamicSegment() || r.Page == "" {
			continue
		}
		kind, _ := segmentKindOfFile(r.Page)
		key := segmentDirKey(r.Page, b.Cfg.PagesDir)
		for i := range m.Segments {
			if m.Segments[i].Key == key {
				m.Segments[i].Dynamic = append(m.Segments[i].Dynamic, kind)
			}
		}
	}
	for i := range m.Pages {
		m.Pages[i].SegDir = routeSegDir(b.Root, b.Cfg.PagesDir, m.Pages[i].Source)
		m.Routes[m.Pages[i].Route] = m.Pages[i]
	}
}

// hasRobotsMeta reports whether a head fragment already sets a robots meta.
func hasRobotsMeta(head string) bool {
	return robotsMetaRe.MatchString(head)
}

var robotsMetaRe = regexp.MustCompile(`(?i)<meta[^>]+name\s*=\s*["']?robots["']?`)

// segmentModel is the source-level view of the project's segment pages, used by
// `krate inspect`, the MCP tools and the route types.
type segmentModel struct {
	entries  []SegmentEntry
	sources  map[string]string // output path -> project-relative source
	graph    []SiteGraphSegment
	pagesDir string
}

// segmentModel reads the segment pages from the source tree.
func (b *Builder) segmentModel() *segmentModel {
	files := collectSegmentFiles(b.Cfg.PagesDir)
	var pages []string
	m := &segmentModel{sources: map[string]string{}, pagesDir: b.Cfg.PagesDir}
	for _, f := range files {
		if pathHasSlot(relOrSelf(b.Cfg.PagesDir, f.Path)) {
			continue
		}
		pages = append(pages, f.Path)
		rel := f.Path
		if r, err := filepath.Rel(b.Root, f.Path); err == nil {
			rel = r
		}
		m.sources[segmentHTMLRel(f.Path, b.Cfg.PagesDir)] = filepath.ToSlash(rel)
	}
	m.entries = segmentEntries(pages, b.Cfg.PagesDir)
	for _, e := range m.entries {
		dir := "/" + e.Dir
		m.graph = append(m.graph, SiteGraphSegment{Dir: dir, NotFound: m.sources[e.NotFound], Error: m.sources[e.Error], Forbidden: m.sources[e.Forbidden], Unauthorized: m.sources[e.Unauthorized], Group: e.Group})
	}
	return m
}

// forSource returns the sources of the nearest not-found and error pages of the
// route defined by a page source file.
func (m *segmentModel) forSource(root, pagesDir, source string) (notFound, errPage string) {
	if source == "" || len(m.entries) == 0 {
		return "", ""
	}
	dir := routeSegDir(root, pagesDir, source)
	if nf, ok := LookupSegmentPage(m.entries, SegmentNotFound, "", true, dir); ok {
		notFound = m.sources[nf.File]
	}
	if ep, ok := LookupSegmentPage(m.entries, SegmentError, "", true, dir); ok {
		errPage = m.sources[ep.File]
	}
	return notFound, errPage
}

// sourceFor returns the source of the nearest page of a kind for the route
// defined by a page source file.
func (m *segmentModel) sourceFor(kind SegmentKind, root, pagesDir, source string) string {
	if source == "" || len(m.entries) == 0 {
		return ""
	}
	if hit, ok := LookupSegmentPage(m.entries, kind, "", true, routeSegDir(root, pagesDir, source)); ok {
		return m.sources[hit.File]
	}
	return ""
}

// errorPlaceholdersMangled reports whether a statically built error page
// altered a placeholder (lowercased, sliced, concatenated into a longer
// token): the server could not fill it in, so the page must read the error on
// the client instead.
func errorPlaceholdersMangled(html string) bool {
	for _, s := range []string{errSentinelName, errSentinelMessage, errSentinelDigest, errSentinelStack} {
		html = strings.ReplaceAll(html, s, "")
	}
	lower := strings.ToLower(html)
	return strings.Contains(lower, "krate_err") || strings.Contains(lower, "__kr")
}

// noteSegmentErrorPage adds a hint to build-time throws (KR3010) of a page that
// has a segment error page: that page answers request-time errors, so it does
// not stand in for a page that cannot be prerendered, and the build still fails.
func (b *Builder) noteSegmentErrorPage(page string, errs []error) []error {
	if kind, ok := segmentKindOfFile(page); ok && kind == SegmentError {
		return errs
	}
	segs := b.segmentModel()
	_, errSource := segs.forSource(b.Root, b.Cfg.PagesDir, page)
	if errSource == "" {
		return errs
	}
	out := make([]error, len(errs))
	for i, err := range errs {
		out[i] = err
		var bt renderer.BuildThrow
		if errors.As(err, &bt) {
			bt.Hint += "; " + errSource + " is this page's error page, but it only answers errors at request time (an ssr or isr page, or a client error), so it is not used as a build-time fallback"
			out[i] = bt
		}
	}
	return out
}
