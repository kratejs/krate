package check

import (
	"strings"
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

func TestImgAltEmptyDecorativeAllowed(t *testing.T) {
	cfg := DefaultConfig()
	page := Page{Route: "/", HTML: doc(`<img src="/a.png" alt="" width="1" height="1">`, "")}
	fs := runOne(t, page, cfg)
	if hasRule(fs, "a11y/img-alt") {
		t.Fatalf("alt=\"\" should be allowed, got %+v", fs)
	}
}

func TestHeadingOrderSkips(t *testing.T) {
	cfg := DefaultConfig()
	page := Page{Route: "/", HTML: doc(`<h1>Title</h1><h3>Skip</h3>`, "")}
	fs := runOne(t, page, cfg)
	if !hasRule(fs, "a11y/heading-order") {
		t.Fatalf("expected heading-order finding, got %+v", fs)
	}
}

func TestHeadingOrderClean(t *testing.T) {
	cfg := DefaultConfig()
	page := Page{Route: "/", HTML: doc(`<h1>Title</h1><h2>Sub</h2><h3>Deep</h3>`, "")}
	fs := runOne(t, page, cfg)
	if hasRule(fs, "a11y/heading-order") {
		t.Fatalf("valid heading order should not report, got %+v", fs)
	}
}

func TestAccessibleName(t *testing.T) {
	cfg := DefaultConfig()
	page := Page{Route: "/", HTML: doc(`<a href="/x"><svg></svg></a><button aria-label="Close"></button>`, "")}
	fs := runOne(t, page, cfg)
	if !hasRule(fs, "a11y/accessible-name") {
		t.Fatalf("expected accessible-name finding for icon-only link, got %+v", fs)
	}
}

func TestSEOTitleDescriptionCanonicalOG(t *testing.T) {
	cfg := DefaultConfig()
	page := Page{Route: "/", HTML: "<!DOCTYPE html><html lang=\"en\"><head></head><body><h1>x</h1></body></html>"}
	fs := runOne(t, page, cfg)
	for _, rule := range []string{"seo/title", "seo/description", "seo/canonical", "seo/og"} {
		if !hasRule(fs, rule) {
			t.Errorf("expected %s finding, got %+v", rule, fs)
		}
	}
}

func TestSEOComplete(t *testing.T) {
	cfg := DefaultConfig()
	page := Page{Route: "/", HTML: doc(`<h1>x</h1>`, "")}
	fs := runOne(t, page, cfg)
	for _, rule := range []string{"seo/title", "seo/description", "seo/canonical", "seo/og", "seo/lang"} {
		if hasRule(fs, rule) {
			t.Errorf("expected no %s finding, got %+v", rule, fs)
		}
	}
}

func TestTitleTooLong(t *testing.T) {
	cfg := DefaultConfig()
	long := strings.Repeat("a", 70)
	head := `<title>` + long + `</title><meta name="description" content="x"><link rel="canonical" href="/"><meta property="og:title" content="a"><meta property="og:type" content="website"><meta property="og:url" content="/">`
	page := Page{Route: "/", HTML: doc(`<h1>x</h1>`, head)}
	fs := runOne(t, page, cfg)
	if !hasRule(fs, "seo/title") {
		t.Fatalf("expected long-title finding, got %+v", fs)
	}
}

func TestJSBudget(t *testing.T) {
	cfg := DefaultConfig()
	cfg.JSBudgetBytes = 100
	page := Page{Route: "/", HTML: doc(`<h1>x</h1>`, ""), JSBytes: 5000}
	fs := runOne(t, page, cfg)
	if !hasRule(fs, "perf/js-budget") {
		t.Fatalf("expected js-budget finding, got %+v", fs)
	}
}

func TestImageDims(t *testing.T) {
	cfg := DefaultConfig()
	page := Page{Route: "/", HTML: doc(`<img src="/a.png" alt="a">`, "")}
	fs := runOne(t, page, cfg)
	if !hasRule(fs, "perf/image-dims") {
		t.Fatalf("expected image-dims finding, got %+v", fs)
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

func TestCategoryOff(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Categories[CategoryA11y] = Off
	page := Page{Route: "/", HTML: doc(`<img src="/a.png">`, "")}
	fs := runOne(t, page, cfg)
	if hasRule(fs, "a11y/img-alt") {
		t.Fatalf("a11y category off should suppress findings")
	}
}

func TestFromMap(t *testing.T) {
	cfg, err := FromMap(map[string]any{
		"seo":    "warning",
		"rules":  map[string]any{"a11y/img-alt": "off"},
		"ignore": []any{"perf/image-dims"},
		"budget": map[string]any{"js": float64(50)},
		"failOn": "warning",
		"custom": []any{"checks/rule.ts"},
	}, "/root", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Active {
		t.Error("config with keys should be active")
	}
	if cfg.Categories[CategorySEO] != Warning {
		t.Errorf("seo severity = %v, want warning", cfg.Categories[CategorySEO])
	}
	if cfg.Rules["a11y/img-alt"] != Off {
		t.Errorf("img-alt override = %v, want off", cfg.Rules["a11y/img-alt"])
	}
	if !cfg.Ignore["perf/image-dims"] {
		t.Error("image-dims should be ignored")
	}
	if cfg.JSBudgetBytes != 50*1024 {
		t.Errorf("budget = %d, want %d", cfg.JSBudgetBytes, 50*1024)
	}
	if cfg.FailOn != Warning {
		t.Errorf("failOn = %v, want warning", cfg.FailOn)
	}
	if len(cfg.Custom) != 1 {
		t.Errorf("custom = %v, want 1 entry", cfg.Custom)
	}
}

func TestFromMapDisabled(t *testing.T) {
	cfg, err := FromMap(map[string]any{"enabled": false}, "/root", nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Active {
		t.Error("enabled:false should deactivate checks")
	}
}

func TestFromMapAbsentInactive(t *testing.T) {
	cfg, err := FromMap(nil, "/root", nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Active {
		t.Error("absent checks should be inactive")
	}
	fs, err := Run(cfg, []Page{{Route: "/", HTML: doc("", "")}})
	if err != nil {
		t.Fatal(err)
	}
	if len(fs) != 0 {
		t.Errorf("inactive run should produce no findings, got %+v", fs)
	}
}
