package build

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

func TestUAPrefersLegacyDoc(t *testing.T) {
	modern := []string{
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
		"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Safari/605.1.15",
		"Mozilla/5.0 (X11; Linux x86_64; rv:125.0) Gecko/20100101 Firefox/125.0",
		"Mozilla/5.0 (Windows NT 10.0) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36 Edg/120.0.0.0",
	}
	for _, ua := range modern {
		if uaPrefersLegacyDoc(ua, "") {
			t.Errorf("expected modern: %s", ua)
		}
	}
	legacy := []string{
		"Mozilla/5.0 (Windows NT 10.0) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/104.0.0.0 Safari/537.36",
		"Mozilla/5.0 (X11; Linux x86_64; rv:120.0) Gecko/20100101 Firefox/120.0",
		"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/15.3 Safari/605.1.15",
		"curl/8.4.0",
		"",
	}
	for _, ua := range legacy {
		if !uaPrefersLegacyDoc(ua, "") {
			t.Errorf("expected legacy: %q", ua)
		}
	}
}

func TestUAPrefersLegacyClientHints(t *testing.T) {
	// A stale/unknown UA string is overridden by an accurate client hint.
	ua := "Mozilla/5.0 (Windows NT 10.0) AppleWebKit/537.36 (KHTML, like Gecko) Safari/537.36"
	modernHint := `"Chromium";v="120", "Not_A Brand";v="24", "Google Chrome";v="120"`
	if uaPrefersLegacyDoc(ua, modernHint) {
		t.Errorf("client hint should mark this modern")
	}
	legacyHint := `"Chromium";v="104", "Not_A Brand";v="24", "Google Chrome";v="104"`
	if !uaPrefersLegacyDoc(ua, legacyHint) {
		t.Errorf("client hint should mark this legacy")
	}
}

func TestSiblingDocPath(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "about"), 0755); err != nil {
		t.Fatal(err)
	}
	write := func(p string) {
		if err := os.WriteFile(filepath.Join(dir, p), []byte(p), 0644); err != nil {
			t.Fatal(err)
		}
	}
	write("index.legacy.html")
	write("index.modern.html")
	write(filepath.Join("about", "index.legacy.html"))
	write(filepath.Join("about", "index.modern.html"))

	if got := siblingDocPath(dir, "/", "legacy"); got == "" {
		t.Errorf("expected root legacy sibling")
	}
	if got := siblingDocPath(dir, "/", "modern"); got == "" {
		t.Errorf("expected root modern sibling")
	}
	if got := siblingDocPath(dir, "/about/", "legacy"); got == "" {
		t.Errorf("expected about legacy sibling")
	}
	if got := siblingDocPath(dir, "/about/index.html", "modern"); got == "" {
		t.Errorf("expected explicit html modern sibling")
	}
	if got := siblingDocPath(dir, "/styles.abc.css", "legacy"); got != "" {
		t.Errorf("asset should not map to a sibling, got %q", got)
	}
	if got := siblingDocPath(dir, "/missing/", "legacy"); got != "" {
		t.Errorf("missing sibling should return empty, got %q", got)
	}
}

func TestUAOptIn(t *testing.T) {
	if uaOptIn(config.CompatConfig{UserAgent: true, Mode: "hybrid"}) != true {
		t.Error("hybrid+userAgent should opt in")
	}
	if uaOptIn(config.CompatConfig{UserAgent: true, Mode: "modern"}) != false {
		t.Error("modern+userAgent should not opt in")
	}
	if uaOptIn(config.CompatConfig{UserAgent: false, Mode: "hybrid"}) != false {
		t.Error("hybrid without userAgent should not opt in")
	}
}

func TestIsHTMLDocRequest(t *testing.T) {
	html := []string{"", "/", "/about/", "/docs/guide/", "/about/index.html"}
	for _, p := range html {
		if !isHTMLDocRequest(p) {
			t.Errorf("expected HTML doc: %q", p)
		}
	}
	assets := []string{"/styles.abc.css", "/index.abc.js", "/chunks/runtime.abc.js", "/assets/logo-1.png", "/about/index.legacy.html.bak"}
	for _, p := range assets {
		if isHTMLDocRequest(p) {
			t.Errorf("expected asset: %q", p)
		}
	}
}
