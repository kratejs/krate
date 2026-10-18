package plugin

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
	"github.com/kratejs/krate/packages/compiler/internal/docs"
)

func TestSidebarConfigResolve(t *testing.T) {
	generated := []docs.SidebarItem{{Title: "Auto"}}

	if got := (SidebarConfig{Mode: "none"}).Resolve(generated, ""); len(got) != 0 {
		t.Errorf("mode none = %v, want empty", got)
	}
	if got := (SidebarConfig{Mode: "auto"}).Resolve(generated, ""); len(got) != 1 || got[0].Title != "Auto" {
		t.Errorf("mode auto = %v, want generated", got)
	}
	if got := (SidebarConfig{Items: []docs.SidebarItem{{Title: "X"}}}).Resolve(generated, ""); len(got) != 1 || got[0].Title != "X" {
		t.Errorf("inline items = %v, want override", got)
	}

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "sidebar.json"), []byte(`[{"title":"FromFile","url":"/x"}]`), 0644); err != nil {
		t.Fatal(err)
	}
	if got := (SidebarConfig{Mode: "file", File: "sidebar.json"}).Resolve(generated, dir); len(got) != 1 || got[0].Title != "FromFile" {
		t.Errorf("file sidebar = %v, want FromFile", got)
	}
}

func TestDocsEditURL(t *testing.T) {
	root := t.TempDir()
	page := docs.Page{SourcePath: filepath.Join(root, "src", "content", "docs", "guide.md")}

	pattern := &DocsPluginOptions{EditLink: EditLinkConfig{Pattern: "https://gh/edit/main/:path"}}
	if got, want := pattern.editURL(root, page), "https://gh/edit/main/src/content/docs/guide.md"; got != want {
		t.Errorf("pattern = %q, want %q", got, want)
	}

	base := &DocsPluginOptions{EditLinkBase: "https://gh/blob"}
	if got, want := base.editURL(root, page), "https://gh/blob/src/content/docs/guide.md"; got != want {
		t.Errorf("base = %q, want %q", got, want)
	}

	page.EditURL = "https://explicit"
	if got := pattern.editURL(root, page); got != "https://explicit" {
		t.Errorf("frontmatter editUrl should win, got %q", got)
	}
}

func TestDocsEditLinkBaseDeprecation(t *testing.T) {
	cfg := &config.Config{Plugins: []config.PluginConfig{{
		Name:    "docs",
		Options: map[string]interface{}{"editLinkBase": "https://example.com/blob"},
	}}}
	w := strings.Join(ConfigWarnings(cfg), "\n")
	if !strings.Contains(w, "editLinkBase is deprecated") {
		t.Errorf("missing editLinkBase deprecation warning: %s", w)
	}
}

func TestDocsOptionToggles(t *testing.T) {
	o := &DocsPluginOptions{}
	if !o.TOCEnabled() || !o.BreadcrumbsEnabled() || !o.PaginationEnabled() {
		t.Fatal("toggles should default to true")
	}
	off := false
	o.TOC, o.Breadcrumbs, o.Pagination = &off, &off, &off
	if o.TOCEnabled() || o.BreadcrumbsEnabled() || o.PaginationEnabled() {
		t.Fatal("explicit false should disable the toggles")
	}
}
