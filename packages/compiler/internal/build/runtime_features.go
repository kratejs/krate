package build

import (
	"encoding/json"
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
	Version int                `json:"version"`
	Core    runtimeFeatureFile `json:"core"`
	// Full lists every export of the full hydration bundle.
	Full    []string               `json:"full"`
	Feature map[string]runtimeFeat `json:"features"`
}

type runtimeFeatureFile struct {
	File string `json:"file"`
}

type runtimeFeat struct {
	File    string   `json:"file"`
	Exports []string `json:"exports"`
}

// optionalRuntimeFeatures lists the features included only when referenced,
// in assembly order. The router (or its lite variant) is chosen by the
// `spa` setting and always included.
var optionalRuntimeFeatures = []string{"list", "navigation", "helpers", "actions", "classes", "highlight"}

// islandMarkup is the tag of a client island. An island loads as a separate
// module that may call any runtime API, so a page that has one keeps every
// feature.
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
	if u.Refs[dynamicImportRef] {
		u.Unknown = true
	}
	return u
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
	scanScripts := func(page string, markup bool) {
		scripts, island, actions := scanPageMarkup(page)
		for name := range jsRefs(scripts...) {
			u.Refs[name] = true
		}
		if markup {
			u.Unknown = u.Unknown || island
			u.ActionForms = u.ActionForms || actions
		}
	}
	scanScripts(r.ScriptHTML, false)
	scanScripts(r.HTML, true)
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
	if fragment == "" {
		return nil, false, false
	}
	z := html.NewTokenizer(strings.NewReader(fragment))
	inScript := false
	for {
		tt := z.Next()
		switch tt {
		case html.ErrorToken:
			return scripts, island, actions
		case html.TextToken:
			if inScript {
				scripts = append(scripts, string(z.Text()))
			}
		case html.EndTagToken:
			inScript = false
		case html.StartTagToken, html.SelfClosingTagToken:
			name, hasAttr := z.TagName()
			tag := string(name)
			if tag == islandMarkup {
				island = true
			}
			jsType := true
			for hasAttr {
				var key, val []byte
				key, val, hasAttr = z.TagAttr()
				switch string(key) {
				case actionsMarkup:
					actions = true
				case "action", "formaction":
					if strings.Contains(string(val), "/__krate/action/") {
						actions = true
					}
				case "type":
					jsType = isJSScriptType(string(val))
				}
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

// loadRuntimeFeatures returns the assembled trimmed hydration runtime and the
// names of the optional features it includes, or "" when the split runtime is
// unavailable (the caller then uses the full bundle).
func loadRuntimeFeatures(projectRoot string, spa bool, usage runtimeUsage) (string, []string) {
	raw := loadNamedBundleFromDisk(projectRoot, "krate-features.json")
	if raw == "" {
		return "", nil
	}
	var m runtimeFeatureManifest
	if json.Unmarshal([]byte(raw), &m) != nil || m.Version != 1 || m.Core.File == "" {
		return "", nil
	}
	core := loadNamedBundleFromDisk(projectRoot, m.Core.File)
	if core == "" {
		return "", nil
	}
	routerName := "router"
	if !spa {
		routerName = "router-lite"
	}
	router, ok := m.Feature[routerName]
	if !ok {
		return "", nil
	}
	routerJS := loadNamedBundleFromDisk(projectRoot, router.File)
	if routerJS == "" {
		return "", nil
	}

	// The bootstrap helpers, the compat layer and the Suspense bundle only
	// probe optional features (typeof / window.x checks), so only the page
	// code decides which features ship.
	refs := make(map[string]bool, len(usage.Refs))
	for name := range usage.Refs {
		refs[name] = true
	}
	addHelperRefs(refs, &m)

	var sb strings.Builder
	sb.WriteString(core)
	sb.WriteString("\n")
	sb.WriteString(routerJS)
	var included []string
	for _, name := range optionalRuntimeFeatures {
		f, ok := m.Feature[name]
		if !ok {
			continue
		}
		if !runtimeFeatureNeeded(name, f, refs, usage) {
			continue
		}
		body := loadNamedBundleFromDisk(projectRoot, f.File)
		if body == "" {
			// A listed bundle is missing: do not ship a runtime without it.
			return "", nil
		}
		sb.WriteString("\n")
		sb.WriteString(body)
		included = append(included, name)
	}
	return sb.String(), included
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
