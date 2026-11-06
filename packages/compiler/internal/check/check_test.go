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

func TestHydrationAndIslandBudgets(t *testing.T) {
	cfg := DefaultConfig()
	cfg.HydrationBudgetBytes = 100
	cfg.IslandBudgetBytes = 100
	cfg.RouteBudgetBytes = 100
	page := Page{Route: "/", HTML: doc(`<h1>x</h1>`, ""), JSBytes: 200, HydrationBytes: 200, IslandBytes: 200, CSSBytes: 200}
	fs := runOne(t, page, cfg)
	for _, rule := range []string{"perf/hydration-budget", "perf/island-budget", "perf/route-budget"} {
		if !hasRule(fs, rule) {
			t.Errorf("expected %s finding, got %+v", rule, fs)
		}
	}
}

func TestBudgetConfig(t *testing.T) {
	cfg, err := FromMap(map[string]any{
		"budget": map[string]any{"js": 10, "hydration": 20, "islandJS": 30, "route": 40},
	}, "/root", nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.JSBudgetBytes != 10*1024 || cfg.HydrationBudgetBytes != 20*1024 || cfg.IslandBudgetBytes != 30*1024 || cfg.RouteBudgetBytes != 40*1024 {
		t.Fatalf("budgets = js:%d hydration:%d island:%d route:%d", cfg.JSBudgetBytes, cfg.HydrationBudgetBytes, cfg.IslandBudgetBytes, cfg.RouteBudgetBytes)
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

func TestFromMapFailOnRejectsUnknownValues(t *testing.T) {
	for _, bad := range []any{"bogus", "warnings", "", "off", "OFF", " nope "} {
		_, err := FromMap(map[string]any{"failOn": bad}, "/root", nil)
		if err == nil {
			t.Errorf("failOn %q: want an error, got none", bad)
			continue
		}
		msg := err.Error()
		if !strings.Contains(msg, "checks.failOn") || !strings.Contains(msg, `"error"`) || !strings.Contains(msg, `"warning"`) {
			t.Errorf("failOn %q: error should name the key and list valid values, got %q", bad, msg)
		}
	}
	if _, err := FromMap(map[string]any{"failOn": float64(1)}, "/root", nil); err == nil {
		t.Error("failOn: a non-string value should be an error")
	}
}

func TestFromMapFailOnValidValues(t *testing.T) {
	for in, want := range map[string]Severity{
		"error": Error, "warning": Warning, "Warning": Warning, "ERROR": Error, " warn ": Warning,
	} {
		cfg, err := FromMap(map[string]any{"failOn": in}, "/root", nil)
		if err != nil {
			t.Errorf("failOn %q: %v", in, err)
			continue
		}
		if cfg.FailOn != want {
			t.Errorf("failOn %q = %v, want %v", in, cfg.FailOn, want)
		}
	}
	// Absent failOn keeps the default.
	cfg, err := FromMap(map[string]any{"seo": "warning"}, "/root", nil)
	if err != nil || cfg.FailOn != Error {
		t.Errorf("default failOn = %v, %v; want error", cfg.FailOn, err)
	}
}

func TestFromMapSeverityStringsAreStrict(t *testing.T) {
	for name, m := range map[string]map[string]any{
		"category": {"a11y": "loud"},
		"rule":     {"rules": map[string]any{"a11y/img-alt": "fatal"}},
	} {
		if _, err := FromMap(m, "/root", nil); err == nil {
			t.Errorf("%s: unknown severity accepted", name)
		}
	}
}

func TestFromMapRejectsNonStringListEntries(t *testing.T) {
	if _, err := FromMap(map[string]any{"ignore": []any{"seo/og", 7.0}}, "/root", nil); err == nil || !strings.Contains(err.Error(), "checks.ignore[1]") {
		t.Errorf("ignore: err = %v", err)
	}
	if _, err := FromMap(map[string]any{"custom": []any{"a.ts", true}}, "/root", nil); err == nil || !strings.Contains(err.Error(), "checks.custom[1]") {
		t.Errorf("custom: err = %v", err)
	}
}

func TestUnknownKeys(t *testing.T) {
	m := map[string]any{
		"failon": "warning",
		"a11y":   "error",
		"budget": map[string]any{"js": 100.0, "islandjs": 5.0},
		"rules": map[string]any{
			"a11y/img-atl":    "off",
			"perf/js-budget":  "error",
			"custom/no-lorem": "error",
		},
		"ignore": []any{"seo/canonical", "seo/cannonical", "my-own-rule"},
	}
	got := map[string]string{}
	for _, u := range UnknownKeys(m) {
		got[u.Path] = u.Suggestion
	}
	want := map[string]string{
		"checks.failon":                "failOn",
		"checks.budget.islandjs":       "islandJS",
		"checks.rules.a11y/img-atl":    "a11y/img-alt",
		"checks.ignore.seo/cannonical": "seo/canonical",
	}
	if len(got) != len(want) {
		t.Fatalf("UnknownKeys = %v, want %v", got, want)
	}
	for path, sug := range want {
		if g, ok := got[path]; !ok || g != sug {
			t.Errorf("UnknownKeys[%s] = %q (present %v), want %q", path, g, ok, sug)
		}
	}
}

func TestUnknownKeysAcceptsEveryDocumentedKey(t *testing.T) {
	m := map[string]any{
		"enabled": true, "a11y": true, "seo": "warning", "perf": false,
		"rules": map[string]any{}, "ignore": []any{}, "custom": []any{"./x.ts"},
		"budget": map[string]any{"js": 1.0, "hydration": 1.0, "islandJS": 1.0, "route": 1.0},
		"failOn": "error",
	}
	if got := UnknownKeys(m); len(got) != 0 {
		t.Fatalf("documented keys flagged: %v", got)
	}
	if got := UnknownKeys(nil); len(got) != 0 {
		t.Fatalf("nil map flagged: %v", got)
	}
}
