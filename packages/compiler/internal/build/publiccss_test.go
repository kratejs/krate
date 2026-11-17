package build

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
	"github.com/kratejs/krate/packages/compiler/internal/diag"
)

func TestPublicStylesheetWarnings(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "public", "css"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"public/site.css", "public/css/extra.css"} {
		if err := os.WriteFile(filepath.Join(root, f), []byte("a{color:red}"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	b := New(root, cfg)

	doc := `<html><head>
<link rel="icon" href="/site.css">
<link rel="stylesheet" href="/site.css?v=2">
<link href='/css/extra.css' rel='stylesheet'>
<link rel="stylesheet" href="/missing.css">
<link rel="stylesheet" href="https://cdn.example.com/x.css">
<link rel="stylesheet" href="/styles.abc123.css">
</head><body><link rel="stylesheet" href="/site.css"></body></html>`
	got := b.publicStylesheetWarnings(doc)
	if len(got) != 2 {
		t.Fatalf("want 2 warnings (site.css, css/extra.css), got %d: %v", len(got), got)
	}
	for _, d := range got {
		if d.Code != diag.CodePublicStylesheet || d.Severity != diag.SeverityWarning || d.Hint == "" {
			t.Errorf("bad diagnostic: %+v", d)
		}
	}
	if got[0].File != "public/site.css" || !strings.Contains(got[0].Message, "/site.css") {
		t.Errorf("first warning should name public/site.css: %+v", got[0])
	}
	if again := b.publicStylesheetWarnings(doc); len(again) != 0 {
		t.Errorf("each file is reported once per build, got %d more", len(again))
	}
}
