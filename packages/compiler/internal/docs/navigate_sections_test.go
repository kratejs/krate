package docs

import "testing"

func titles(items []SidebarItem) []string {
	var out []string
	for _, it := range items {
		out = append(out, it.Title)
	}
	return out
}

func eq(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func mk(path, dir, title string, order int, cfg *SidebarNavConfig, titleSet bool) Page {
	return Page{Path: path, Dir: dir, Title: title, TitleSet: titleSet, Order: order, SidebarCfg: cfg}
}

func TestSectionDefaultsUnchanged(t *testing.T) {
	pages := []Page{
		mk("zeta", "", "Zeta", 999, nil, true),
		mk("cli/index", "cli", "Index.md", 999, nil, false),
		mk("cli/run", "cli", "Run", 999, nil, true),
		mk("api/index", "api", "Index.md", 999, nil, false),
		mk("about", "", "About", 999, nil, true),
	}
	got := BuildSidebarTree(pages)
	if want := []string{"About", "Zeta", "Api", "Cli"}; !eq(titles(got), want) {
		t.Fatalf("got %v, want %v", titles(got), want)
	}
	if got[3].IndexURL != "/docs/cli/" {
		t.Errorf("IndexURL = %q", got[3].IndexURL)
	}
}

func TestSectionTitleFromIndex(t *testing.T) {
	pages := []Page{
		mk("cli/index", "cli", "Command Line", 999, nil, true),
		mk("cli/run", "cli", "Run", 999, nil, true),
		mk("api/index", "api", "Index.md", 999, &SidebarNavConfig{Label: "API Reference"}, false),
		mk("api/x", "api", "X", 999, nil, true),
		mk("misc/index", "misc", "Index.md", 999, nil, false),
		mk("misc/y", "misc", "Y", 999, nil, true),
	}
	got := BuildSidebarTree(pages)
	if want := []string{"API Reference", "Command Line", "Misc"}; !eq(titles(got), want) {
		t.Fatalf("got %v, want %v", titles(got), want)
	}
	// The renamed section still links to its index page.
	for _, s := range got {
		if s.Title == "Command Line" && s.IndexURL != "/docs/cli/" {
			t.Errorf("IndexURL = %q", s.IndexURL)
		}
	}
	enriched := EnrichSidebarItemsURL(got, "/docs/cli/run/")
	for _, s := range enriched {
		if s.Title == "Command Line" {
			if s.IndexURL != "/docs/cli/" || !s.Collapsible || !s.Expanded {
				t.Errorf("enriched section = %+v", s)
			}
			for _, c := range s.Children {
				if c.URL == s.IndexURL {
					t.Errorf("index page should not repeat as a child")
				}
			}
		}
	}
}

func TestSectionOrderFromIndex(t *testing.T) {
	pages := []Page{
		mk("a", "", "A", 5, nil, true),
		mk("b", "", "B", 999, nil, true),
		mk("z/index", "z", "Z", 2, nil, true),
		mk("z/p", "z", "P", 999, nil, true),
		mk("m/index", "m", "M", 999, &SidebarNavConfig{Order: 7}, true),
		mk("m/p", "m", "P", 999, nil, true),
		mk("n/index", "n", "N", 999, nil, true),
		mk("n/p", "n", "P", 999, nil, true),
	}
	got := BuildSidebarTree(pages)
	// Z(2) A(5) M(7) B(999 link) N(unset section)
	if want := []string{"Z", "A", "M", "B", "N"}; !eq(titles(got), want) {
		t.Fatalf("got %v, want %v", titles(got), want)
	}
}

func TestSectionOrderTieAndNesting(t *testing.T) {
	pages := []Page{
		mk("l", "", "L", 3, nil, true),
		mk("s/index", "s", "S", 3, nil, true),
		mk("s/inner/index", "s/inner", "Inner", 1, nil, true),
		mk("s/inner/q", "s/inner", "Q", 999, nil, true),
		mk("s/leaf", "s", "Leaf", 2, nil, true),
	}
	got := BuildSidebarTree(pages)
	if want := []string{"L", "S"}; !eq(titles(got), want) {
		t.Fatalf("tie: links first, got %v", titles(got))
	}
	// Inside S: Inner (order 1) before Leaf (2); the index link comes last
	// (order 3) and is hidden by Enrich.
	if want := []string{"Inner", "Leaf", "S"}; !eq(titles(got[1].Children), want) {
		t.Fatalf("children = %v, want %v", titles(got[1].Children), want)
	}
}

func TestApplyTrailingSlash(t *testing.T) {
	for _, c := range []struct{ in, mode, want string }{
		{"/docs/cli/", "never", "/docs/cli"},
		{"/docs/", "never", "/docs"},
		{"/", "never", "/"},
		{"/fr/docs/cli/?q=1#x", "never", "/fr/docs/cli?q=1#x"},
		{"/docs/cli/", "always", "/docs/cli/"},
		{"/docs/cli/", "ignore", "/docs/cli/"},
		{"/docs/cli/", "", "/docs/cli/"},
		{"", "never", ""},
	} {
		if got := ApplyTrailingSlash(c.in, c.mode); got != c.want {
			t.Errorf("ApplyTrailingSlash(%q, %q) = %q, want %q", c.in, c.mode, got, c.want)
		}
	}
}

func TestEnrichIgnoresTrailingSlashDifference(t *testing.T) {
	items := []SidebarItem{{Title: "S", Children: []SidebarItem{{Title: "P", URL: "/docs/s/p"}}}}
	for _, active := range []string{"/docs/s/p/", "/docs/s/p"} {
		out := EnrichSidebarItemsURL(items, active)
		if !out[0].Expanded {
			t.Errorf("active %q did not expand the section", active)
		}
	}
}

func TestSidebarOrderedPages(t *testing.T) {
	pages := []Page{
		{Path: "a", URL: "/docs/a/"}, {Path: "b", URL: "/docs/b/"}, {Path: "c", URL: "/docs/c/"},
	}
	sections := []SidebarItem{
		{Title: "G", IndexURL: "/docs/b", Children: []SidebarItem{{URL: "/docs/b/"}, {URL: "/docs/a"}}},
	}
	got, ok := SidebarOrderedPages(pages, sections)
	if !ok || len(got) != 2 || got[0].Path != "b" || got[1].Path != "a" {
		t.Fatalf("got %v ok=%v", got, ok)
	}
	if _, ok := SidebarOrderedPages(pages, nil); ok {
		t.Error("no sidebar must report ok=false")
	}
}
