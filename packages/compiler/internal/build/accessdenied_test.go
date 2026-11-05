package build

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

func TestSegmentKindsForForbiddenAndUnauthorized(t *testing.T) {
	cases := map[string]SegmentKind{
		"forbidden.tsx":    SegmentForbidden,
		"403.tsx":          SegmentForbidden,
		"unauthorized.tsx": SegmentUnauthorized,
		"401.tsx":          SegmentUnauthorized,
	}
	for name, want := range cases {
		kind, ok := segmentKindOfFile(name)
		if !ok || kind != want {
			t.Errorf("%s: got (%q, %v), want %q", name, kind, ok, want)
		}
		if !isSegmentPageBase(strings.TrimSuffix(name, ".tsx")) {
			t.Errorf("%s must not be a route", name)
		}
	}
	pagesDir := filepath.Join(t.TempDir(), "pages")
	page := filepath.Join(pagesDir, "admin", "forbidden.tsx")
	if got := segmentHTMLRel(page, pagesDir); got != "admin/403.html" {
		t.Errorf("html rel = %q", got)
	}
	if got := segmentAssetOut(filepath.Join(pagesDir, "401.tsx"), pagesDir); got != "__401" {
		t.Errorf("asset out = %q", got)
	}
	if got := SegmentForbidden.status(); got != 403 {
		t.Errorf("status = %d", got)
	}
}

func TestLookupSegmentPageForbiddenWalksUpLikeNotFound(t *testing.T) {
	entries := []SegmentEntry{
		{Key: "", Forbidden: "403.html", NotFound: "404.html"},
		{Key: "admin", Dir: "admin", Forbidden: "admin/403.html"},
		{Key: "admin/users", Dir: "admin/users", Unauthorized: "admin/users/401.html"},
	}
	cases := []struct {
		kind   SegmentKind
		segDir string
		want   string
	}{
		{SegmentForbidden, "admin/users/[id]", "admin/403.html"},
		{SegmentForbidden, "blog", "403.html"},
		{SegmentUnauthorized, "admin/users", "admin/users/401.html"},
		{SegmentUnauthorized, "admin", ""},
	}
	for _, c := range cases {
		m, ok := LookupSegmentPage(entries, c.kind, "/x", true, c.segDir)
		if c.want == "" {
			if ok {
				t.Errorf("%s %s: got %q, want none", c.kind, c.segDir, m.File)
			}
			continue
		}
		if !ok || m.File != c.want {
			t.Errorf("%s %s: got (%q, %v), want %q", c.kind, c.segDir, m.File, ok, c.want)
		}
	}
	// Without a route match the longest URL prefix wins, as for not-found.
	if m, ok := LookupSegmentPage(entries, SegmentForbidden, "/admin/anything", false, ""); !ok || m.File != "admin/403.html" {
		t.Errorf("url lookup = (%q, %v)", m.File, ok)
	}
}

func TestValidateSegmentPagesRejectsTwoForbiddenFiles(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"src/pages/forbidden.tsx": "x",
		"src/pages/403.tsx":       "x",
	})
	err := validateSegmentPages(filepath.Join(root, "src", "pages"))
	if err == nil || !strings.Contains(err.Error(), "KR9001") {
		t.Fatalf("want KR9001, got %v", err)
	}
}

// A build writes 403.html and 401.html next to the directory, records them in
// the manifest and keeps them out of the route list.
func TestBuildWritesForbiddenAndUnauthorizedPages(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"src/pages/_layout.tsx":            `export default function L({ children }: any) { return <div id="root-layout">{children}</div>; }`,
		"src/pages/index.tsx":              devPage("home"),
		"src/pages/forbidden.tsx":          devPage("ROOT403"),
		"src/pages/admin/index.tsx":        devPage("admin"),
		"src/pages/admin/unauthorized.tsx": devPage("ADMIN401"),
	})
	cfg := config.Default()
	cfg.PagesDir = filepath.Join(root, "src", "pages")
	cfg.OutDir = filepath.Join(root, "dist")
	if err := New(root, cfg).BuildAll(); err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	for rel, want := range map[string][]string{
		"403.html":       {"ROOT403", "root-layout", "noindex"},
		"admin/401.html": {"ADMIN401", "root-layout", "noindex"},
	} {
		data, err := os.ReadFile(filepath.Join(cfg.OutDir, filepath.FromSlash(rel)))
		if err != nil {
			t.Errorf("%s not written: %v", rel, err)
			continue
		}
		for _, w := range want {
			if !strings.Contains(string(data), w) {
				t.Errorf("%s missing %q", rel, w)
			}
		}
	}
	m := loadManifestForTest(t, cfg.OutDir)
	byKey := map[string]SegmentEntry{}
	for _, e := range m.Segments {
		byKey[e.Key] = e
	}
	if byKey[""].Forbidden != "403.html" || byKey["admin"].Unauthorized != "admin/401.html" {
		t.Errorf("manifest segments = %+v", m.Segments)
	}
	for _, p := range m.Pages {
		if strings.Contains(p.Route, "403") || strings.Contains(p.Route, "401") || strings.Contains(p.Route, "forbidden") || strings.Contains(p.Route, "unauthorized") {
			t.Errorf("segment page leaked into the route list: %s", p.Route)
		}
	}
}

// forbidden() in a page that is built statically never runs; the build says so
// (KR8604) instead of silently publishing the page to everyone.
func TestStaticPageCallingForbiddenWarns(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"src/pages/admin.tsx": `import { forbidden } from "@krate/runtime/server";
export default function Admin() { forbidden(); return <p>admin</p>; }`,
		"src/pages/ok.tsx": `import { unauthorized } from "@krate/runtime/server";
export const dynamic = "force-dynamic";
export default function Ok() { unauthorized(); return <p>ok</p>; }`,
		"src/pages/_layout.tsx": `import { forbidden } from "@krate/runtime/server";
export default function L({ children }: any) { forbidden(); return <div>{children}</div>; }`,
	})
	cfg := config.Default()
	cfg.PagesDir = filepath.Join(root, "src", "pages")
	cfg.OutDir = filepath.Join(root, "dist")
	out := captureStderr(t, func() {
		if err := New(root, cfg).BuildAll(); err != nil {
			t.Fatalf("BuildAll: %v", err)
		}
	})
	if !strings.Contains(out, "KR8604") || !strings.Contains(out, "admin.tsx") {
		t.Errorf("a static page calling forbidden() must warn KR8604:\n%s", out)
	}
	if !strings.Contains(out, "layouts are baked") {
		t.Errorf("a layout calling forbidden() must warn KR8604:\n%s", out)
	}
	if strings.Contains(out, "ok.tsx") && strings.Contains(out, "unauthorized() is called in a page") {
		t.Errorf("a force-dynamic page must not warn:\n%s", out)
	}
}
