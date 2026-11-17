package build

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

// The runtime features were once decided by a text scan of the page code
// that counted names inside string literals and markup mentioned in text.
// The tests here keep that scan as a reference: for every page of a fixture,
// a feature the old scan found must still ship, unless each reason the old
// scan had is a known false positive listed in legacyFalsePositives.

// legacyScanMarkers are the old scan's markers for code it could not see.
var legacyScanMarkers = []string{"import(", "__krateLazy", islandMarkup}

// legacyRuntimeScanText is the old runtimeScanText.
func legacyRuntimeScanText(r *PageResult) string {
	var sb strings.Builder
	sb.WriteString(r.HydrationJS)
	sb.WriteByte('\n')
	sb.WriteString(r.ScriptHTML)
	sb.WriteByte('\n')
	if perRequestClientCode(r) {
		sb.WriteString(" " + islandMarkup + " " + actionsMarkup + " ")
	}
	if strings.Contains(r.HTML, "<"+islandMarkup) {
		sb.WriteString(" " + islandMarkup + " ")
	}
	if strings.Contains(r.HTML, actionsMarkup) || strings.Contains(r.HTML, "/__krate/action/") {
		sb.WriteString(" " + actionsMarkup + " ")
	}
	return sb.String()
}

var legacyCompatMarkers = []string{"useImperativeHandle", "useDebugValue", "isValidElement", "Children", "cloneElement", "createRef", "__krateLazy("}

// legacyFeatureReasons returns, for each feature the old scan shipped for
// the page, why: the names or markers it matched. "compat" and "suspense"
// stand for the compat layer and the Suspense bundle.
func legacyFeatureReasons(m *runtimeFeatureManifest, r *PageResult) map[string][]string {
	scan := legacyRuntimeScanText(r)
	refs := identifierSet(scan)
	addHelperRefs(refs, m)
	out := map[string][]string{}
	var markers []string
	for _, mk := range legacyScanMarkers {
		if strings.Contains(scan, mk) {
			markers = append(markers, "marker "+mk)
		}
	}
	for _, name := range optionalRuntimeFeatures {
		f, ok := m.Feature[name]
		if !ok {
			continue
		}
		var why []string
		why = append(why, markers...)
		for _, e := range f.Exports {
			if refs[e] {
				why = append(why, e)
			}
		}
		if name == "actions" && strings.Contains(scan, actionsMarkup) {
			why = append(why, "markup "+actionsMarkup)
		}
		if len(why) > 0 {
			out[name] = why
		}
	}
	for _, mk := range legacyCompatMarkers {
		if strings.Contains(r.HydrationJS, mk) {
			out["compat"] = append(out["compat"], mk)
		}
	}
	for _, mk := range suspenseMarkers {
		if strings.Contains(r.HydrationJS, mk) {
			out["suspense"] = append(out["suspense"], mk)
		}
	}
	return out
}

// currentFeatures returns the features the current detection ships for the
// page alone.
func currentFeatures(m *runtimeFeatureManifest, r *PageResult) map[string]bool {
	u := runtimeUsageOf([]*PageResult{r})
	refs := map[string]bool{}
	for k := range u.Refs {
		refs[k] = true
	}
	addHelperRefs(refs, m)
	out := map[string]bool{}
	for _, name := range optionalRuntimeFeatures {
		if f, ok := m.Feature[name]; ok && runtimeFeatureNeeded(name, f, refs, u) {
			out[name] = true
		}
	}
	out["compat"] = runtimeNeedsCompat(u)
	out["suspense"] = runtimeNeedsSuspense(u)
	return out
}

// legacyFalsePositives lists reasons the old scan had that do not mean the
// page needs the feature, keyed "page feature: reason" (page is the output
// name). Each is checked by hand against the built output; add one only
// with a comment saying why it is false.
var legacyFalsePositives = map[string]string{
	// The component id string "1.ExpressionChildren.ExpressionChildren_c0"
	// contains Children; the page calls no Children API.
	"edge-cases compat: Children": "examples/src/pages/edge-cases.tsx",
	// Only the component id strings "1e.ChildrenDemo..." contain Children:
	// the compiler folds the page's static Children.map / Children.count.
	"react-demo compat: Children": "examples/src/pages/react-demo.tsx",
	// The old scan shipped the compat layer for any lazy chunk, unseen. The
	// chunk (lazy-LazyPanel) is now scanned with the page and uses no compat
	// API.
	"react-suspense compat: __krateLazy(": "examples/src/pages/react-suspense.tsx",

	// The output audit fixture page src/content/docs/apis.md names APIs in
	// its headings, which reach the code only as TOC titles in the props.
	"docs/apis actions: requestFormReset": "TOC title",
	"docs/apis compat: Children":          "TOC title",
	"docs/apis highlight: krateHighlight": "TOC title",
	"docs/apis navigation: useParams":     "TOC title",

	// packages/web (KRATE_FEATURE_DIFF_ROOT=packages/web). Each name is a
	// table of contents title in the layout props, {title:"..."}.
	"docs/core-concepts/routing navigation: forbidden":     "TOC title Forbidden and unauthorized",
	"docs/core-concepts/routing navigation: unauthorized":  "TOC title Forbidden and unauthorized",
	"docs/guides/compatibility compat: Children":           "TOC title Children, keys, and refs",
	"docs/guides/server-actions actions: requestFormReset": "TOC title requestFormReset",
	"docs/reference/runtime-api helpers: Slot":             "TOC title Class helpers & Slot",
	"docs/reference/runtime-api highlight: krateHighlight": "TOC title krateHighlight",
	// The slot id string of kbindRef("cn", ...).
	"docs/reference/runtime-api helpers: cn": "slot id",
	// A highlighted code sample shows data-krate-action as text in spans.
	"docs/guides/server-actions actions: markup data-krate-action": "code sample text",
}

// featureRegression is a feature the old scan shipped for a page and the
// current detection does not, for reasons not all known false positives.
type featureRegression struct {
	page, feature string
	reasons       []string
}

func legacyRegressions(m *runtimeFeatureManifest, results []*PageResult) (bad []featureRegression, explained []featureRegression) {
	for _, r := range results {
		if r == nil {
			continue
		}
		now := currentFeatures(m, r)
		for feature, why := range legacyFeatureReasons(m, r) {
			if now[feature] {
				continue
			}
			reg := featureRegression{page: r.OutName, feature: feature, reasons: why}
			ok := true
			for _, w := range why {
				if _, known := legacyFalsePositives[r.OutName+" "+feature+": "+w]; !known {
					ok = false
				}
			}
			if ok {
				explained = append(explained, reg)
			} else {
				bad = append(bad, reg)
			}
		}
	}
	sort.Slice(bad, func(i, j int) bool { return bad[i].page+bad[i].feature < bad[j].page+bad[j].feature })
	return bad, explained
}

// pageResultForScan keeps the fields the feature detection reads.
func pageResultForScan(r *PageResult) *PageResult {
	if r == nil {
		return nil
	}
	return &PageResult{
		Page: r.Page, OutName: r.OutName, HTML: r.HTML, ScriptHTML: r.ScriptHTML,
		HydrationJS: r.HydrationJS, RuntimeRefs: r.RuntimeRefs, Mode: r.Mode,
		Regions: r.Regions, ClientBoundaries: r.ClientBoundaries, ServerActions: r.ServerActions,
	}
}

func captureRuntimeResults(b *Builder, into *[]*PageResult) {
	b.runtimeResultsHook = func(rs []*PageResult) {
		*into = (*into)[:0]
		for _, r := range rs {
			if r != nil {
				*into = append(*into, pageResultForScan(r))
			}
		}
	}
}

func checkLegacyCoverage(t *testing.T, results []*PageResult) {
	t.Helper()
	_, m := loadFeatureManifest(t)
	if len(results) == 0 {
		t.Fatal("no page results were captured")
	}
	bad, explained := legacyRegressions(&m, results)
	for _, e := range explained {
		t.Logf("%s: %s no longer ships (known false positives: %s)", e.page, e.feature, strings.Join(e.reasons, ", "))
	}
	for _, e := range bad {
		t.Errorf("%s: the old scan shipped %s (because of %s) and the current detection does not; if every reason is a false positive, add it to legacyFalsePositives with why", e.page, e.feature, strings.Join(e.reasons, ", "))
	}
}

// TestRuntimeFeaturesCoverLegacyScanExamples builds examples/ for production
// and compares the features of every page with the old scan.
func TestRuntimeFeaturesCoverLegacyScanExamples(t *testing.T) {
	if testing.Short() {
		t.Skip("builds examples/")
	}
	repoRoot, err := repoRootPath()
	if err != nil {
		t.Fatal(err)
	}
	ensureRuntimeDist(t)
	// A replayed page from an older cache has no recorded references.
	t.Setenv("KRATE_NO_BUILD_CACHE", "1")
	root := os.Getenv("KRATE_FEATURE_DIFF_ROOT")
	if root == "" {
		root = filepath.Join(repoRoot, "examples")
	}
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	cfg.OutDir = filepath.Join(t.TempDir(), "dist")
	b := New(root, cfg)
	var results []*PageResult
	captureRuntimeResults(b, &results)
	if err := b.BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	t.Logf("site ships runtime features %v", b.runtimeFeatures)
	if os.Getenv("KRATE_FEATURE_DIFF_VERBOSE") != "" {
		_, m := loadFeatureManifest(t)
		for _, r := range results {
			u := runtimeUsageOf([]*PageResult{r})
			var why []string
			if u.Unknown {
				why = append(why, "unknown")
			}
			if u.ActionForms {
				why = append(why, "action-forms")
			}
			for _, f := range optionalRuntimeFeatures {
				for _, e := range m.Feature[f].Exports {
					if u.Refs[e] {
						why = append(why, f+":"+e)
					}
				}
			}
			if len(why) > 0 {
				t.Logf("uses %s: %v", r.OutName, why)
			}
		}
		for _, r := range results {
			old := legacyFeatureReasons(&m, r)
			now := currentFeatures(&m, r)
			var lines []string
			for f, why := range old {
				if !now[f] {
					lines = append(lines, fmt.Sprintf("  -%s (%s)", f, strings.Join(why, ", ")))
				}
			}
			u := runtimeUsageOf([]*PageResult{r})
			for f, on := range now {
				if _, had := old[f]; on && !had {
					var hits []string
					for _, e := range m.Feature[f].Exports {
						if u.Refs[e] {
							hits = append(hits, e)
						}
					}
					lines = append(lines, fmt.Sprintf("  +%s %v unknown=%v", f, hits, u.Unknown))
				}
			}
			if len(lines) > 0 {
				sort.Strings(lines)
				t.Logf("%s\n%s", r.OutName, strings.Join(lines, "\n"))
			}
		}
	}
	checkLegacyCoverage(t, results)
}
