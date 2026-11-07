package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateOutputMode(t *testing.T) {
	c := Default()
	c.Output = "bogus"
	if _, err := c.Validate(); err == nil {
		t.Fatal("expected error for invalid output mode")
	}
	c.Output = "static"
	if _, err := c.Validate(); err != nil {
		t.Fatalf("unexpected error for static: %v", err)
	}
}

func TestValidatePorts(t *testing.T) {
	c := Default()
	c.DevServer.Port = 70000
	if _, err := c.Validate(); err == nil {
		t.Fatal("expected error for out-of-range port")
	}
	c = Default()
	c.SSR.Timeout = -1
	if _, err := c.Validate(); err == nil {
		t.Fatal("expected error for negative timeout")
	}
}

func TestUnknownKeyWarnings(t *testing.T) {
	raw := []byte(`{"entry":"src/index.tsx","tailwnd":{},"output":"static"}`)
	warnings := UnknownKeyWarnings(raw)
	if len(warnings) != 1 {
		t.Fatalf("expected 1 warning, got %v", warnings)
	}
	if !strings.Contains(warnings[0], "tailwnd") {
		t.Errorf("warning should name the unknown key: %v", warnings[0])
	}
}

// TestMarkdownMermaidAccepted verifies markdown.mermaid is a recognized nested
// key (no unknown-key warning) now that the markdown config supports it.
func TestMarkdownMermaidAccepted(t *testing.T) {
	raw := []byte(`{"entry":"x","markdown":{"mermaid":true}}`)
	if w := UnknownKeyWarnings(raw); len(w) != 0 {
		t.Errorf("markdown.mermaid should be accepted, got warnings: %v", w)
	}
	raw = []byte(`{"entry":"x","markdown":{"mermaidTypo":true}}`)
	if w := UnknownKeyWarnings(raw); len(w) != 1 {
		t.Errorf("expected a warning for an unknown markdown key, got: %v", w)
	}
}

func TestUnknownKeyWarningsIgnoresValidate(t *testing.T) {
	raw := []byte(`{"entry":"x","validate":"function"}`)
	if w := UnknownKeyWarnings(raw); len(w) != 0 {
		t.Errorf("validate hook should not warn: %v", w)
	}
}

// TestDeprecatedEmitReactAcceptedSilently verifies the removed emitReact option
// still loads without an unknown-key warning, in either boolean state.
func TestDeprecatedEmitReactAcceptedSilently(t *testing.T) {
	for _, raw := range [][]byte{
		[]byte(`{"entry":"x","emitReact":true}`),
		[]byte(`{"entry":"x","emitReact":false}`),
	} {
		if w := UnknownKeyWarnings(raw); len(w) != 0 {
			t.Errorf("emitReact should be accepted silently, got warnings: %v", w)
		}
	}
}

// TestApplyConfigPropEmitReactNoOp verifies the tsconfig-style config loader
// accepts emitReact without error or effect.
func TestApplyConfigPropEmitReactNoOp(t *testing.T) {
	cfg := Default()
	for _, v := range []interface{}{true, false} {
		if err := applyConfigProp(cfg, "emitReact", v); err != nil {
			t.Errorf("applyConfigProp(emitReact, %v) returned error: %v", v, err)
		}
	}
}

// TestUnknownKeyWarningsNested verifies typos inside nested config objects are
// reported, not just top-level ones.
func TestUnknownKeyWarningsNested(t *testing.T) {
	raw := []byte(`{"ssr":{"timoout":5},"api":{"sidecar":{"prot":8080}},"markdown":{"codeThm":"x"}}`)
	warnings := UnknownKeyWarnings(raw)
	joined := strings.Join(warnings, "\n")
	for _, want := range []string{"ssr.timoout", "api.sidecar.prot", "markdown.codeThm"} {
		if !strings.Contains(joined, want) {
			t.Errorf("expected warning for %s, got:\n%s", want, joined)
		}
	}
}

// TestKnownTopLevelKeysNoSpuriousWarnings verifies documented top-level keys
// (server, cors, api, basePath, seo, robots, runtime, tsBaseDir, pathAliases,
// and the component/dir lists) are recognized and never warn as unknown.
func TestKnownTopLevelKeysNoSpuriousWarnings(t *testing.T) {
	raw := []byte(`{
		"entry":"x",
		"server":{"port":3000},
		"cors":{"enabled":true},
		"api":{"sidecar":{"port":3000}},
		"basePath":"/docs",
		"seo":{"baseUrl":"https://x"},
		"robots":{"allow":"/"},
		"runtime":"quickjs",
		"tsBaseDir":".",
		"pathAliases":{"@/*":["./src/*"]},
		"serverComponents":["A"],
		"runtimeComponents":["B"],
		"serverDirs":["d"],
		"runtimeDirs":["e"]
	}`)
	if w := UnknownKeyWarnings(raw); len(w) != 0 {
		t.Errorf("documented keys should not warn, got: %v", w)
	}
}

// TestApplyConfigPropNewKeys verifies the static parser now recognizes the
// documented keys it previously dropped silently.
func TestApplyConfigPropNewKeys(t *testing.T) {
	cfg := Default()
	props := map[string]interface{}{
		"seo":               map[string]interface{}{"baseUrl": "https://x", "siteName": "S", "description": "D", "image": "I"},
		"robots":            map[string]interface{}{"allow": "/", "disallow": "/admin", "sitemap": "https://x/sitemap.xml"},
		"runtime":           "quickjs",
		"tsBaseDir":         "/base",
		"pathAliases":       map[string]interface{}{"@/*": []interface{}{"./src/*"}},
		"serverComponents":  []interface{}{"A"},
		"runtimeComponents": []interface{}{"B"},
		"serverDirs":        []interface{}{"s"},
		"runtimeDirs":       []interface{}{"r"},
	}
	for k, v := range props {
		if err := applyConfigProp(cfg, k, v); err != nil {
			t.Fatalf("applyConfigProp(%s): %v", k, err)
		}
	}
	if cfg.SEO.BaseURL != "https://x" || cfg.SEO.SiteName != "S" || cfg.SEO.Description != "D" || cfg.SEO.Image != "I" {
		t.Errorf("seo not applied: %+v", cfg.SEO)
	}
	if cfg.Robots.Allow != "/" || cfg.Robots.Disallow != "/admin" || cfg.Robots.Sitemap != "https://x/sitemap.xml" {
		t.Errorf("robots not applied: %+v", cfg.Robots)
	}
	if cfg.Runtime != "quickjs" || cfg.TSBaseDir != "/base" {
		t.Errorf("runtime/tsBaseDir not applied: %q %q", cfg.Runtime, cfg.TSBaseDir)
	}
	if len(cfg.ServerComponents) != 1 || cfg.ServerComponents[0] != "A" ||
		len(cfg.RuntimeComponents) != 1 || cfg.RuntimeComponents[0] != "B" ||
		len(cfg.ServerDirs) != 1 || len(cfg.RuntimeDirs) != 1 {
		t.Errorf("component/dir lists not applied: %v %v %v %v", cfg.ServerComponents, cfg.RuntimeComponents, cfg.ServerDirs, cfg.RuntimeDirs)
	}
	if len(cfg.PathAliases) != 1 || cfg.PathAliases[0].Prefix != "@/*" {
		t.Errorf("pathAliases not applied: %+v", cfg.PathAliases)
	}
}

// TestValidateServerAndSidecar verifies bounds checks for the new server/cors/
// api.sidecar/basePath config.
func TestValidateServerAndSidecar(t *testing.T) {
	c := Default()
	c.Server.Port = 70000
	if _, err := c.Validate(); err == nil {
		t.Error("expected error for out-of-range server.port")
	}

	c = Default()
	c.Server.TrustedProxies = -1
	if _, err := c.Validate(); err == nil {
		t.Error("expected error for negative server.trustedProxies")
	}
	if w := UnknownKeyWarnings([]byte(`{"server":{"trustProxy":true,"trustedProxies":2}}`)); len(w) != 0 {
		t.Errorf("server.trustedProxies must be a known key, got warnings %v", w)
	}

	c = Default()
	c.BasePath = "docs"
	if _, err := c.Validate(); err == nil {
		t.Error("expected error for basePath without leading /")
	}

	c = Default()
	c.API.Sidecar = &SidecarConfig{Command: "node"}
	if _, err := c.Validate(); err == nil {
		t.Error("expected error for supervised sidecar without port")
	}

	c = Default()
	c.API.Sidecar = &SidecarConfig{Target: "not-a-url"}
	if _, err := c.Validate(); err == nil {
		t.Error("expected error for invalid sidecar target")
	}

	c = Default()
	c.API.Sidecar = &SidecarConfig{Command: "node", Port: 8080}
	if _, err := c.Validate(); err != nil {
		t.Errorf("valid supervised sidecar should pass: %v", err)
	}
}

// TestUnknownKeyWarningsInsideChecks verifies typos in the free-form `checks`
// map are reported, with the nearest valid name.
func TestUnknownKeyWarningsInsideChecks(t *testing.T) {
	raw := []byte(`{"checks":{"failon":"warning","budget":{"jss":1},"rules":{"a11y/img-atl":"off"},"ignore":["seo/cannonical","custom/mine"]}}`)
	joined := strings.Join(UnknownKeyWarnings(raw), "\n")
	for _, want := range []string{
		`"checks.failon" (ignored; did you mean "failOn"?)`,
		`"checks.budget.jss" (ignored; did you mean "js"?)`,
		`"checks.rules.a11y/img-atl" (ignored; did you mean "a11y/img-alt"?)`,
		`"checks.ignore.seo/cannonical" (ignored; did you mean "seo/canonical"?)`,
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing warning %s in:\n%s", want, joined)
		}
	}
	if strings.Contains(joined, "custom/mine") {
		t.Errorf("custom rule IDs must not warn:\n%s", joined)
	}
	ok := []byte(`{"checks":{"a11y":"error","budget":{"js":100},"failOn":"error","rules":{"perf/js-budget":"off"}}}`)
	if w := UnknownKeyWarnings(ok); len(w) != 0 {
		t.Errorf("valid checks warned: %v", w)
	}
}

// TestLoadWarnsOnUnknownChecksKey runs the real loader so both the static and
// the evaluated config paths reach the warning.
func TestLoadWarnsOnUnknownChecksKey(t *testing.T) {
	dir := t.TempDir()
	src := "export default {\n  entry: 'src/index.tsx',\n  checks: { failon: 'warning', a11y: 'error' },\n}\n"
	if err := os.WriteFile(filepath.Join(dir, "krate.config.ts"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	Warnings = nil
	t.Cleanup(func() { Warnings = nil })
	if _, err := Load(dir); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(Warnings, "\n"), `"checks.failon" (ignored; did you mean "failOn"?)`) {
		t.Fatalf("Load did not warn about checks.failon; warnings: %v", Warnings)
	}
}
