package jseval

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/evanw/esbuild/pkg/api"
)

func setupStub() (*api.PluginBuild, *[]api.OnResolveOptions, *[]func(api.OnResolveArgs) (api.OnResolveResult, error), *[]api.OnLoadOptions, *[]func(api.OnLoadArgs) (api.OnLoadResult, error)) {
	var ro []api.OnResolveOptions
	var rc []func(api.OnResolveArgs) (api.OnResolveResult, error)
	var lo []api.OnLoadOptions
	var lc []func(api.OnLoadArgs) (api.OnLoadResult, error)
	pb := &api.PluginBuild{
		InitialOptions: &api.BuildOptions{},
		OnResolve: func(o api.OnResolveOptions, cb func(api.OnResolveArgs) (api.OnResolveResult, error)) {
			ro, rc = append(ro, o), append(rc, cb)
		},
		OnLoad: func(o api.OnLoadOptions, cb func(api.OnLoadArgs) (api.OnLoadResult, error)) {
			lo, lc = append(lo, o), append(lc, cb)
		},
	}
	return pb, &ro, &rc, &lo, &lc
}

func TestVirtualFilesFilterCoversAbsolutePaths(t *testing.T) {
	virtual := map[string]string{
		filepath.Clean("/proj/virtual.ts"): "export {}",
	}
	pb, ro, rc, _, _ := setupStub()
	virtualFilesPlugin(virtual).Setup(*pb)
	if len(*ro) != 1 {
		t.Fatalf("want 1 OnResolve, got %d", len(*ro))
	}
	re := regexp.MustCompile((*ro)[0].Filter)
	if (*ro)[0].Namespace != "" {
		t.Errorf("namespace option %q would skip importers outside it", (*ro)[0].Namespace)
	}
	handled := []string{filepath.Clean("/proj/virtual.ts")}
	for _, s := range handled {
		r, err := (*rc)[0](api.OnResolveArgs{Path: s})
		if err != nil || r.Path == "" {
			t.Fatalf("callback ignores virtual file %q", s)
		}
		if !re.MatchString(s) {
			t.Errorf("filter excludes virtual file %q", s)
		}
	}
	// Every spelling filepath.IsAbs accepts on some OS must pass the filter.
	for _, s := range []string{"/a/b", `C:\a\b`, "C:/a/b", `z:\a`, `\\srv\share\x`, `\/srv/share/x`, "//srv/share/x", `\\?\C:\a`} {
		if !re.MatchString(s) {
			t.Errorf("filter excludes absolute path %q", s)
		}
	}
	for _, s := range []string{"react", "./x", "../x", "@scope/pkg", "krate/content", "C", "C:", "node:fs", "", `a\b`, "x/y"} {
		if re.MatchString(s) {
			t.Errorf("filter matches non-absolute %q", s)
		}
		if r, err := (*rc)[0](api.OnResolveArgs{Path: s}); err != nil || r.Path != "" {
			t.Errorf("callback handled non-absolute %q", s)
		}
	}
}

func TestImportMetaPreserverLoadsOnlyFileNamespace(t *testing.T) {
	dir := t.TempDir()
	js := filepath.Join(dir, "cfg.ts")
	css := filepath.Join(dir, "a.css")
	for _, f := range []string{js, css} {
		if err := os.WriteFile(f, []byte("export const x = 1\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	pb, _, _, lo, lc := setupStub()
	importMetaPreserver(false, nil, nil).Setup(*pb)
	if len(*lo) != 2 {
		t.Fatalf("want 2 OnLoad, got %d", len(*lo))
	}
	for i, o := range *lo {
		if o.Namespace != "file" {
			t.Errorf("OnLoad %d (%q) has namespace %q, want file", i, o.Filter, o.Namespace)
		}
		re := regexp.MustCompile(o.Filter)
		// A load in another namespace was answered empty before; the callback
		// still says so, and esbuild no longer asks.
		for _, ns := range []string{"", "krate-css", "krate-marker", "krate-go"} {
			if r, err := (*lc)[i](api.OnLoadArgs{Path: js, Namespace: ns}); err != nil || r.Contents != nil {
				t.Errorf("OnLoad %d handled namespace %q", i, ns)
			}
		}
		// The path filters are unchanged; the file namespace still loads.
		for _, p := range []string{js, css} {
			r, err := (*lc)[i](api.OnLoadArgs{Path: p, Namespace: "file"})
			if err != nil {
				t.Fatal(err)
			}
			if re.MatchString(p) && r.Contents == nil {
				t.Errorf("OnLoad %d matches %q in the file namespace but loads nothing", i, p)
			}
		}
	}
}
