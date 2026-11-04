package build

import (
	"encoding/json"
	"strings"
)

// The @krate/runtime package ships the hydration runtime twice: as one full
// bundle (krate-hydrate.js) and as a core plus optional feature bundles
// described by krate-features.json. A production build assembles the core and
// only the features the site's client code references, so a counter page does
// not pay for server actions, error boundaries or class helpers. Dev builds,
// older runtime packages and code the scan cannot see through use the full
// bundle.

type runtimeFeatureManifest struct {
	Version int                    `json:"version"`
	Core    runtimeFeatureFile     `json:"core"`
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
var optionalRuntimeFeatures = []string{"list", "helpers", "actions", "classes"}

// unknownClientCodeMarkers mark client code the identifier scan cannot see:
// dynamically imported chunks, lazy components and client islands load code
// that may use any runtime API.
var unknownClientCodeMarkers = []string{"import(", "__krateLazy", "krate-island"}

// runtimeScanText joins the client-side code of the built pages that runs
// against the runtime globals.
func runtimeScanText(results []*PageResult) string {
	var sb strings.Builder
	for _, r := range results {
		if r == nil {
			continue
		}
		sb.WriteString(r.HydrationJS)
		sb.WriteByte('\n')
		sb.WriteString(r.ScriptHTML)
		sb.WriteByte('\n')
	}
	return sb.String()
}

// loadRuntimeFeatures returns the assembled trimmed hydration runtime and the
// names of the optional features it includes, or "" when the split runtime is
// unavailable (the caller then uses the full bundle).
func loadRuntimeFeatures(projectRoot string, spa bool, scan string) (string, []string) {
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
	refs := identifierSet(scan)
	all := false
	for _, marker := range unknownClientCodeMarkers {
		if strings.Contains(scan, marker) {
			all = true
			break
		}
	}

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
		if !all && !featureReferenced(f.Exports, refs) {
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

func featureReferenced(exports []string, refs map[string]bool) bool {
	for _, name := range exports {
		if refs[name] {
			return true
		}
	}
	return false
}

// identifierSet returns every identifier-like token in the given texts,
// including those inside string literals (window["name"]).
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
