package check

import (
	"testing"
)

func doc(body, head string) string {
	if head == "" {
		head = `<title>Hello World</title>
<meta name="description" content="A test page.">
<link rel="canonical" href="https://example.com/">
<meta property="og:title" content="Hello World">
<meta property="og:type" content="website">
<meta property="og:url" content="https://example.com/">`
	}
	return "<!DOCTYPE html>\n<html lang=\"en\"><head>" + head + "</head><body>" + body + "</body></html>"
}

func runOne(t *testing.T, page Page, cfg Config) []Finding {
	t.Helper()
	fs, err := Run(cfg, []Page{page})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	return fs
}

func hasRule(fs []Finding, rule string) bool {
	for _, f := range fs {
		if f.Rule == rule {
			return true
		}
	}
	return false
}

func TestImgAltMissing(t *testing.T) {
	cfg := DefaultConfig()
	page := Page{Route: "/", RelSource: "src/pages/index.tsx", HTML: doc(`<img src="/a.png">`, "")}
	fs := runOne(t, page, cfg)
	if !hasRule(fs, "a11y/img-alt") {
		t.Fatalf("expected img-alt finding, got %+v", fs)
	}
}

func TestIgnoreAndSeverityOverride(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Ignore["seo/canonical"] = true
	cfg.Rules["seo/description"] = Error
	page := Page{Route: "/", HTML: "<!DOCTYPE html><html lang=\"en\"><head><title>x</title></head><body><h1>x</h1></body></html>"}
	fs := runOne(t, page, cfg)
	if hasRule(fs, "seo/canonical") {
		t.Fatalf("ignored rule should not report")
	}
	for _, f := range fs {
		if f.Rule == "seo/description" && f.Severity != Error {
			t.Fatalf("expected description override to Error, got %v", f.Severity)
		}
	}
}
