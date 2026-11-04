package build

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/kratejs/krate/packages/compiler/internal/diag"
	"github.com/kratejs/krate/packages/compiler/internal/fsutil"
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
var segmentVariants sync.Map // page path -> "static" | "window"

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
func errorBoundarySource(spec string) string {
	return "import { Component } from \"react\";\n" +
		"import ErrorPage from " + fmt.Sprintf("%q", spec) + ";\n\n" +
		"class KrateSegmentBoundary extends Component {\n" +
		"  state = { error: null };\n" +
		"  static getDerivedStateFromError(error: any) { return { error }; }\n" +
		"  componentDidCatch(error: any) { console.error(error); }\n" +
		"  reset = () => this.setState({ error: null });\n" +
		"  render() {\n" +
		"    if (this.state.error) return <ErrorPage error={this.state.error} reset={this.reset} />;\n" +
		"    return this.props.children;\n" +
		"  }\n" +
		"}\n\n" +
		"export default function KrateSegmentBoundaryLayout(props: any) {\n" +
		"  return <KrateSegmentBoundary><div style={{ display: \"contents\" }}>{props.children}</div></KrateSegmentBoundary>;\n" +
		"}\n"
}

// errorPageSource is the generated entry for the server-rendered error page.
// The static variant bakes placeholders into the HTML, which the server
// replaces per request; a page whose error component needs client code uses
// the window variant, where the component reads the error the server injects
// as window.__KRATE_ERROR__ (and the HTML carries no text for it until the page
// hydrates).
func errorPageSource(spec, variant string) string {
	var errExpr string
	if variant == "window" {
		errExpr = "(typeof window !== \"undefined\" && (window as any).__KRATE_ERROR__) || { name: \"Error\", message: \"\", digest: \"\", stack: \"\" }"
	} else {
		errExpr = "{ name: \"" + errSentinelName + "\", message: \"" + errSentinelMessage + "\", digest: \"" + errSentinelDigest + "\", stack: \"" + errSentinelStack + "\" }"
	}
	return "import ErrorPage from " + fmt.Sprintf("%q", spec) + ";\n\n" +
		"const krateServerError: any = " + errExpr + ";\n\n" +
		"export default function KrateServerError() {\n" +
		"  return <ErrorPage error={krateServerError} reset={() => location.reload()} />;\n" +
		"}\n"
}

// writeSegmentGen writes a generated source (only when it changed, so module
// caches and mtimes stay stable).
func (b *Builder) writeSegmentGen(path, src string) error {
	return fsutil.WriteFileIfChanged(path, []byte(src), 0o644)
}

// boundaryLayout returns the generated boundary layout for an error.tsx.
func (b *Builder) boundaryLayout(errFile string) (string, error) {
	path := b.segmentGenPath(errFile, "boundary")
	if err := b.writeSegmentGen(path, errorBoundarySource(importSpec(path, errFile))); err != nil {
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
