package build

import (
	"encoding/json"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/net/html"

	"github.com/kratejs/krate/packages/compiler/internal/renderer"
)

// The @krate/runtime package ships the hydration runtime twice: as one full
// bundle (krate-hydrate.js) and as a core plus optional feature bundles
// described by krate-features.json. A production build assembles the core and
// only the features the site's client code references, so a counter page does
// not pay for server actions, error boundaries or class helpers. Dev builds
// and older runtime packages use the full bundle; code the build cannot see
// (islands, dynamic imports, HTML rendered per request) gets every feature.

type runtimeFeatureManifest struct {
	Version int         `json:"version"`
	Core    runtimeFeat `json:"core"`
	// Full lists every export of the full hydration bundle.
	Full    []string               `json:"full"`
	Feature map[string]runtimeFeat `json:"features"`
	// Suspense and ESM are absent from older runtime packages (see
	// runtime_shake.go).
	Suspense *runtimeFeat `json:"suspense"`
	// ESM maps each runtime module to its exports.
	ESM map[string][]string `json:"esm"`
}

type runtimeFeat struct {
	File    string   `json:"file"`
	Exports []string `json:"exports"`
	// Modules are the ES modules the piece is built from.
	Modules []string `json:"modules"`
}

// optionalRuntimeFeatures lists the features included only when referenced,
// in assembly order. The router (or its lite variant) is chosen by the
// `spa` setting and always included.
var optionalRuntimeFeatures = []string{"list", "navigation", "helpers", "actions", "classes", "highlight"}

// islandMarkup is the tag of a real-React client island and, followed by a
// colon, the start of the comment that opens a Preact one. An island loads as
// a separate module that may call any runtime API, so a page that has one
// keeps every feature.
const islandMarkup = "krate-island"

// actionsMarkup is the attribute the compiler puts on a form that posts to a
// server action. The actions bundle enhances such forms from a document
// listener it installs at load, so no page code references it by name.
const actionsMarkup = "data-krate-action"

// runtimeUsage is what the built pages need from the optional runtime.
type runtimeUsage struct {
	// Refs are the global names client code references.
	Refs map[string]bool
	// ActionForms is set when a page renders a server action form.
	ActionForms bool
	// Unknown is set when a page runs client code the build did not scan
	// (islands, dynamically imported modules, HTML rendered per request);
	// every feature then ships.
	Unknown bool
	// LazyUnscanned is set when a page loads lazy chunks whose code was not
	// recorded (a page result without RuntimeRefs).
	LazyUnscanned bool
	// ScriptSrcs are the same-origin script URLs (site paths, without the
	// query) that pages load with <script src>.
	ScriptSrcs map[string]bool
}

// recordRuntimeRefs returns the global names a page's generated client code
// references: its hydration script and its lazy chunks, which the page
// script loads with __krateLazy. A name counts when it is referenced both
// before minification (where a minified local can take a runtime export's
// name, such as cn) and after it, when minified is set (where a local the
// author named like an export, such as handleError, is renamed).
func recordRuntimeRefs(pageJS, minified string, lazy []renderer.LazyChunk) []string {
	refs := jsRefs(pageJS)
	if minified != "" {
		kept := jsRefs(minified)
		for name := range refs {
			if !kept[name] {
				delete(refs, name)
			}
		}
	}
	for _, c := range lazy {
		for name := range jsRefs(c.JS) {
			refs[name] = true
		}
	}
	out := make([]string, 0, len(refs))
	for name := range refs {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// runtimeUsageOf collects the runtime usage of the built pages. Code is read
// as code: names that only occur in string literals (a heading in the props,
// a slot id, a code sample) or comments do not count, and markup counts only
// as real elements and attributes, never as text.
func runtimeUsageOf(results []*PageResult) runtimeUsage {
	u := runtimeUsage{Refs: make(map[string]bool)}
	for _, r := range results {
		if r == nil {
			continue
		}
		u.addPage(r)
	}
	// A dynamic import() loads code the build did not scan, and a computed
	// member of window can read any export.
	u.checkDynamic()
	return u
}

// checkDynamic marks the usage unknown when the scanned code can reach any
// global: a dynamic import() loads code the build did not scan, and a
// computed member of window can read any export.
func (u *runtimeUsage) checkDynamic() {
	if u.Refs[dynamicImportRef] || u.Refs[dynamicGlobalRef] {
		u.Unknown = true
	}
}

// addScriptFiles adds the references of the same-origin scripts pages load
// whose source read returns (files from the public directory). Scripts it
// cannot read, such as external URLs, are left alone.
func (u *runtimeUsage) addScriptFiles(read func(sitePath string) (string, bool)) {
	srcs := make([]string, 0, len(u.ScriptSrcs))
	for src := range u.ScriptSrcs {
		srcs = append(srcs, src)
	}
	sort.Strings(srcs)
	for _, src := range srcs {
		if code, ok := read(src); ok {
			for name := range jsRefs(code) {
				u.Refs[name] = true
			}
		}
	}
	u.checkDynamic()
}

// scriptSitePath returns the site path a script src on the page at outName
// loads, or "" for an external, data or other non-file URL.
func scriptSitePath(outName, src string) string {
	src = strings.TrimSpace(src)
	if i := strings.IndexAny(src, "?#"); i >= 0 {
		src = src[:i]
	}
	if src == "" || strings.HasPrefix(src, "//") {
		return ""
	}
	if i := strings.IndexAny(src, ":/"); i >= 0 && src[i] == ':' {
		return ""
	}
	if !strings.HasPrefix(src, "/") {
		src = path.Join("/", path.Dir(filepath.ToSlash(outName)), src)
	}
	return path.Clean(src)
}

func (u *runtimeUsage) addPage(r *PageResult) {
	if r.RuntimeRefs != nil {
		for _, name := range r.RuntimeRefs {
			u.Refs[name] = true
		}
	} else if r.HydrationJS != "" {
		// Not recorded: scan the shipped (minified) script. Its lazy chunks
		// are separate files this cannot see.
		refs := jsRefs(r.HydrationJS)
		for name := range refs {
			u.Refs[name] = true
		}
		if refs["__krateLazy"] {
			u.Unknown = true
			u.LazyUnscanned = true
		}
	}
	scanScripts := func(page string, inline, markup bool) {
		m := scanMarkup(page)
		scripts, island, actions := m.scripts, m.island, m.actions
		if inline {
			for name := range jsRefs(scripts...) {
				u.Refs[name] = true
			}
		}
		for _, src := range m.srcs {
			if p := scriptSitePath(r.OutName, src); p != "" {
				if u.ScriptSrcs == nil {
					u.ScriptSrcs = map[string]bool{}
				}
				u.ScriptSrcs[p] = true
			}
		}
		if markup {
			u.Unknown = u.Unknown || island
			u.ActionForms = u.ActionForms || actions
		}
	}
	scanScripts(r.ScriptHTML, true, false)
	// Head scripts are the framework's (theme, CDN loaders); only the files
	// they load count.
	scanScripts(r.HeadHTML, false, false)
	scanScripts(r.HTML, true, true)
	// HTML produced per request can render forms and client components
	// the build never saw: such a page keeps the whole runtime.
	if perRequestClientCode(r) {
		u.Unknown = true
		u.ActionForms = true
	}
}

// scanPageMarkup returns the bodies of the classic and module scripts in an
// HTML fragment, and whether it has a client island element or a server
// action form (an element with data-krate-action, or an action or formaction
// URL under /__krate/action/). Text, comments and attribute values that
// merely mention the markup do not count.
func scanPageMarkup(fragment string) (scripts []string, island, actions bool) {
	m := scanMarkup(fragment)
	return m.scripts, m.island, m.actions
}

// pageMarkup is what scanMarkup reads from an HTML fragment.
type pageMarkup struct {
	scripts         []string
	srcs            []string // src of the JavaScript script elements
	island, actions bool
}

func scanMarkup(fragment string) (m pageMarkup) {
	if fragment == "" {
		return m
	}
	z := html.NewTokenizer(strings.NewReader(fragment))
	inScript := false
	for {
		tt := z.Next()
		switch tt {
		case html.ErrorToken:
			return m
		case html.TextToken:
			if inScript {
				m.scripts = append(m.scripts, string(z.Text()))
			}
		case html.EndTagToken:
			inScript = false
		case html.CommentToken:
			if strings.HasPrefix(string(z.Text()), islandMarkup+":") {
				m.island = true
			}
		case html.StartTagToken, html.SelfClosingTagToken:
			name, hasAttr := z.TagName()
			tag := string(name)
			if tag == islandMarkup {
				m.island = true
			}
			jsType := true
			src := ""
			for hasAttr {
				var key, val []byte
				key, val, hasAttr = z.TagAttr()
				switch string(key) {
				case actionsMarkup:
					m.actions = true
				case "action", "formaction":
					if strings.Contains(string(val), "/__krate/action/") {
						m.actions = true
					}
				case "type":
					jsType = isJSScriptType(string(val))
				case "src":
					src = string(val)
				}
			}
			if tag == "script" && jsType && src != "" {
				m.srcs = append(m.srcs, src)
			}
			inScript = tag == "script" && tt == html.StartTagToken && jsType
		}
	}
}

// isJSScriptType reports whether a script type attribute value runs as
// JavaScript.
func isJSScriptType(t string) bool {
	t = strings.ToLower(strings.TrimSpace(t))
	if i := strings.IndexByte(t, ';'); i >= 0 {
		t = strings.TrimSpace(t[:i])
	}
	switch t {
	case "", "module", "text/javascript", "application/javascript", "text/ecmascript", "application/ecmascript":
		return true
	}
	return false
}

// loadRuntimeFeatures returns the trimmed hydration runtime, the names of
// the optional features it includes, and whether it includes the Suspense
// runtime, or "" when the split runtime is unavailable (the caller then uses
// the full bundle). A runtime package that ships its ES modules gets a
// runtime bundled for the site (see shakenRuntime); an older one the
// concatenated core and feature bundles.
func loadRuntimeFeatures(projectRoot string, opts runtimeChunkOptions) (string, []string, bool) {
	usage := opts.Usage
	raw := loadNamedBundleFromDisk(projectRoot, "krate-features.json")
	if raw == "" {
		return "", nil, false
	}
	var m runtimeFeatureManifest
	if json.Unmarshal([]byte(raw), &m) != nil || m.Version != 1 || m.Core.File == "" {
		return "", nil, false
	}
	routerName := "router"
	if !opts.SPA {
		routerName = "router-lite"
	}
	router, ok := m.Feature[routerName]
	if !ok {
		return "", nil, false
	}

	// The bootstrap helpers, the compat layer and the Suspense bundle only
	// probe optional features (typeof / window.x checks), so only the page
	// code decides which features ship.
	refs := make(map[string]bool, len(usage.Refs))
	for name := range usage.Refs {
		refs[name] = true
	}
	addHelperRefs(refs, &m)

	var included []string
	for _, name := range optionalRuntimeFeatures {
		f, ok := m.Feature[name]
		if ok && runtimeFeatureNeeded(name, f, refs, usage) {
			included = append(included, name)
		}
	}
	if js := shakenRuntime(projectRoot, &m, routerName, included, opts, refs); js != "" {
		return js, included, opts.Suspense
	}

	core := loadNamedBundleFromDisk(projectRoot, m.Core.File)
	routerJS := loadNamedBundleFromDisk(projectRoot, router.File)
	if core == "" || routerJS == "" {
		return "", nil, false
	}
	var sb strings.Builder
	sb.WriteString(core)
	sb.WriteString("\n")
	sb.WriteString(routerJS)
	for _, name := range included {
		body := loadNamedBundleFromDisk(projectRoot, m.Feature[name].File)
		if body == "" {
			// A listed bundle is missing: do not ship a runtime without it.
			return "", nil, false
		}
		sb.WriteString("\n")
		sb.WriteString(body)
	}
	return sb.String(), included, false
}

// helperFeatures lists the optional runtime features a bootstrap helper needs.
// Page code calls helpers by name and never mentions the runtime API they
// use, so the identifier scan alone would drop the feature (kbindList probes
// krateBindList and silently does nothing when it is missing). The test
// TestBootstrapHelperFeatureDeps keeps this table in step with the helpers.
var helperFeatures = map[string][]string{
	"kbindList": {"list"},
}

// addHelperRefs adds the exports of every feature that a referenced bootstrap
// helper needs.
func addHelperRefs(refs map[string]bool, m *runtimeFeatureManifest) {
	for helper, features := range helperFeatures {
		if !refs[helper] {
			continue
		}
		for _, name := range features {
			for _, export := range m.Feature[name].Exports {
				refs[export] = true
			}
		}
	}
}

// runtimeFeatureNeeded reports whether the optional feature name ships.
func runtimeFeatureNeeded(name string, f runtimeFeat, refs map[string]bool, usage runtimeUsage) bool {
	return usage.Unknown || featureReferenced(f.Exports, refs) || name == "actions" && usage.ActionForms
}

func featureReferenced(exports []string, refs map[string]bool) bool {
	for _, name := range exports {
		if refs[name] {
			return true
		}
	}
	return false
}

// identifierSet returns every identifier-like word in the given texts,
// including those inside string literals and comments. It is the fallback
// for code that does not tokenize (see jsRefs).
func identifierSet(texts ...string) map[string]bool {
	set := make(map[string]bool)
	for _, text := range texts {
		start := -1
		for i := 0; i <= len(text); i++ {
			var c byte
			if i < len(text) {
				c = text[i]
			}
			isID := c == '_' || c == '$' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (start >= 0 && c >= '0' && c <= '9')
			if isID {
				if start < 0 {
					start = i
				}
				continue
			}
			if start >= 0 {
				set[text[start:i]] = true
				start = -1
			}
		}
	}
	return set
}
