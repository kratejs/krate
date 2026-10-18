package plugin

import (
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/docs"
)

func TestVersionSwitchLinksPrefersEquivalentPage(t *testing.T) {
	links := versionSwitchLinks(
		[]string{"v2", "v1"}, "v2", "en", "en",
		map[string]string{"v2": "/docs/", "v1": "/docs/v1/getting-started/"},
		nil,
	)
	if len(links) != 2 {
		t.Fatalf("got %d links, want 2", len(links))
	}
	if links[0].URL != "/docs/" || links[1].URL != "/docs/v1/getting-started/" {
		t.Fatalf("unexpected links: %+v", links)
	}
}

func TestVersionSwitchLinksFallsBackToVersionRoot(t *testing.T) {
	links := versionSwitchLinks(
		[]string{"v2", "v1"}, "v2", "en", "en",
		map[string]string{"v2": "/docs/"},
		map[string]string{"en\x00v1": "/docs/v1/getting-started/"},
	)
	if links[1].URL != "/docs/v1/getting-started/" {
		t.Fatalf("v1 fallback = %q, want /docs/v1/getting-started/", links[1].URL)
	}
}

func TestVersionSwitchLinksFallsBackToDefaultLocaleRoot(t *testing.T) {
	// `fr` has no v1 content, so the target falls back to the default locale's
	// v1 root instead of the non-existent /fr/docs/v1/.
	links := versionSwitchLinks(
		[]string{"v2", "v1"}, "v2", "fr", "en",
		map[string]string{"v2": "/fr/docs/getting-started/"},
		map[string]string{"en\x00v1": "/docs/v1/getting-started/"},
	)
	if links[1].URL != "/docs/v1/getting-started/" {
		t.Fatalf("fr v1 fallback = %q, want /docs/v1/getting-started/", links[1].URL)
	}
}

func TestVersionSwitchLinksLastResortIsVersionIndex(t *testing.T) {
	links := versionSwitchLinks(
		[]string{"v2", "v1"}, "v2", "en", "en",
		map[string]string{"v2": "/docs/"},
		nil,
	)
	if links[1].URL != docsURL("en", "en", "v1", "v2", "index") {
		t.Fatalf("v1 last resort = %q", links[1].URL)
	}
}

func TestFirstSidebarURLDepthFirst(t *testing.T) {
	items := []docs.SidebarItem{
		{Title: "S", Children: []docs.SidebarItem{
			{Title: "A", URL: "/docs/a/"},
		}},
		{Title: "B", URL: "/docs/b/"},
	}
	if got := firstSidebarURL(items); got != "/docs/a/" {
		t.Fatalf("firstSidebarURL = %q, want /docs/a/", got)
	}
	if got := firstSidebarURL([]docs.SidebarItem{{Title: "Empty"}}); got != "" {
		t.Fatalf("firstSidebarURL(empty) = %q, want empty", got)
	}
}

func TestLocaleSwitchLinksLabels(t *testing.T) {
	links := localeSwitchLinksWithLabels([]string{"en", "fr"}, "en",
		map[string]string{"fr": "/fr/docs/"}, map[string]string{"fr": "Francais (FR)"})
	if len(links) != 2 {
		t.Fatalf("links = %d, want 2", len(links))
	}
	if links[1].Label != "Francais (FR)" {
		t.Errorf("fr label = %q, want configured label", links[1].Label)
	}
	fallback := localeSwitchLinksWithLabels([]string{"en"}, "en", nil, nil)
	if fallback[0].Label != "English" {
		t.Errorf("default label = %q, want English", fallback[0].Label)
	}
}

func TestVersionSwitchLinksLabels(t *testing.T) {
	links := versionSwitchLinksWithLabels([]string{"v2", "v1"}, "v2", "en", "en", nil, nil,
		map[string]string{"v1": "1.x (legacy)"})
	if len(links) != 2 {
		t.Fatalf("links = %d, want 2", len(links))
	}
	if links[1].Label != "1.x (legacy)" {
		t.Errorf("v1 label = %q, want configured label", links[1].Label)
	}
}
