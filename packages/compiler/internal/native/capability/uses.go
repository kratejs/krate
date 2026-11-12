package capability

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/kratejs/krate/packages/compiler/internal/config"
	"github.com/kratejs/krate/packages/compiler/internal/native"
	"github.com/kratejs/krate/packages/compiler/internal/native/features"
)

// AddUses records the permissions the page asks the webview for. getUserMedia
// and getDisplayMedia need "media" and navigator.geolocation needs "location":
// the shell denies the request of a window whose scope lacks them, so a
// derived build has to grant them like it grants the SDK ids the code calls.
func (r *Result) AddUses(u features.Uses) {
	site := func(sym string) Site {
		return Site{File: u.File, Line: u.Line, Kind: "ui", Symbol: sym}
	}
	add := func(perm string, s Site) {
		for _, x := range r.Needs[perm] {
			if x.File == s.File && x.Symbol == s.Symbol {
				return
			}
		}
		r.Needs[perm] = append(r.Needs[perm], s)
	}
	switch {
	case u.Capture:
		add("media", site("getUserMedia"))
	case u.Screen:
		add("media", site("getDisplayMedia"))
	}
	if u.Location {
		add("location", site("navigator.geolocation"))
	}
	r.Permissions = r.Permissions[:0]
	for perm := range r.Needs {
		r.Permissions = append(r.Permissions, perm)
	}
	sort.Strings(r.Permissions)
	r.Permissions = pruneImplied(r.Permissions)
}

// ScanSourceUses finds the webview permission uses in the project's script
// files (what Analyze reads), for the checks that run without a build.
func ScanSourceUses(root string, cfg *config.Config) (features.Uses, error) {
	files, err := native.ProjectFiles(root, cfg)
	if err != nil {
		return features.Uses{}, err
	}
	sort.Strings(files)
	var out features.Uses
	for _, f := range files {
		switch strings.ToLower(filepath.Ext(f)) {
		case ".ts", ".tsx", ".js", ".jsx", ".mjs", ".mts":
		default:
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		u := features.ScanUses(string(b))
		if !u.Any() {
			continue
		}
		if !out.Any() {
			if rel, err := filepath.Rel(root, f); err == nil {
				out.File = filepath.ToSlash(rel)
			}
		}
		out.Capture = out.Capture || u.Capture
		out.Camera = out.Camera || u.Camera
		out.Microphone = out.Microphone || u.Microphone
		out.Screen = out.Screen || u.Screen
		out.Location = out.Location || u.Location
	}
	return out, nil
}
