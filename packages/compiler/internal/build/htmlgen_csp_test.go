package build

import (
	"strings"
	"testing"
)

// Empty-string SHA-256 (never a legitimate inline content hash).
const emptyContentHash = "47DEQpj8HBSa+/TImW+5JCeuQeRkm5NMpJWZG3hSuFU"

func TestGenerateCSPMeta_ExternalOnlyAssets(t *testing.T) {
	scriptHTML := `<script src="/about/index.a1b2c3.js"></script>`
	styleHTML := `<link rel="stylesheet" href="/styles.abc123.css">`
	meta := generateCSPMeta(scriptHTML, styleHTML, "", "")

	if meta == "" || !strings.Contains(meta, "Content-Security-Policy") {
		t.Fatalf("expected a CSP meta tag for external-only assets, got empty: %q", meta)
	}
	if strings.Contains(meta, emptyContentHash) {
		t.Errorf("bogus empty-string SHA-256 hash present in CSP: %s", meta)
	}
	if !strings.Contains(meta, "script-src 'self'") || !strings.Contains(meta, "style-src 'self'") || !strings.Contains(meta, "default-src 'self'") {
		t.Errorf("expected base 'self' directives in CSP: %s", meta)
	}
}

func TestGenerateCSPMeta_InlineHashes(t *testing.T) {
	scriptHTML := `<script>console.log("hi")</script>`
	styleHTML := `<style>body{color:red}</style>`
	meta := generateCSPMeta(scriptHTML, styleHTML, "", "")
	if !strings.Contains(meta, "script-src 'self' 'sha256-") {
		t.Errorf("expected inline script hash, got: %s", meta)
	}
	if !strings.Contains(meta, "style-src 'self' 'sha256-") {
		t.Errorf("expected inline style hash, got: %s", meta)
	}
}

func TestGenerateCSPMeta_HydrationJSHash(t *testing.T) {
	meta := generateCSPMeta("", "", "var x = 1;", "")
	if !strings.Contains(meta, "'sha256-") {
		t.Errorf("expected hydration JS hash in CSP, got: %s", meta)
	}
}

func TestGenerateCSPMeta_CustomDirective(t *testing.T) {
	meta := generateCSPMeta("", "", "", "default-src 'none'; frame-ancestors 'none'")
	if !strings.Contains(meta, "frame-ancestors 'none'") {
		t.Errorf("custom directive not preserved: %s", meta)
	}
}