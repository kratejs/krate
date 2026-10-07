package config

import (
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

// TestValidateServerAndSidecar verifies bounds checks for the new server/cors/
// api.sidecar/basePath config.
func TestValidateServerAndSidecar(t *testing.T) {
	c := Default()
	c.Server.Port = 70000
	if _, err := c.Validate(); err == nil {
		t.Error("expected error for out-of-range server.port")
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
