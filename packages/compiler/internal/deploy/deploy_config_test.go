package deploy

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

func TestEmitNetlifyHeaders(t *testing.T) {
	out := t.TempDir()
	cfg := &config.Config{Headers: []config.HeaderRule{{
		Source:  "/assets/*",
		Headers: map[string]string{"X-Test": "1", "Cache-Control": "public, max-age=31536000, immutable"},
	}}}
	files, err := Emit("netlify", cfg, out)
	if err != nil {
		t.Fatalf("Emit: %v", err)
	}

	var headers string
	for _, f := range files {
		if filepath.Base(f) == "_headers" {
			data, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			headers = string(data)
		}
	}
	if headers == "" {
		t.Fatal("_headers not written for netlify")
	}
	for _, want := range []string{"/assets/*", "  Cache-Control: public, max-age=31536000, immutable", "  X-Test: 1"} {
		if !strings.Contains(headers, want) {
			t.Errorf("_headers missing %q:\n%s", want, headers)
		}
	}
}

func TestEmitVercelHeaders(t *testing.T) {
	out := t.TempDir()
	cfg := &config.Config{Headers: []config.HeaderRule{{
		Source:  "/assets/*",
		Headers: map[string]string{"X-Test": "1"},
	}}}
	files, err := Emit("vercel", cfg, out)
	if err != nil {
		t.Fatalf("Emit: %v", err)
	}
	data, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	rules, ok := doc["headers"].([]any)
	if !ok || len(rules) == 0 {
		t.Fatalf("vercel.json missing headers: %s", data)
	}
	if !strings.Contains(string(data), `"key": "X-Test"`) {
		t.Errorf("vercel.json missing header key: %s", data)
	}
}

func TestEmitVercelDeployOptions(t *testing.T) {
	out := t.TempDir()
	cfg := &config.Config{Deploy: config.DeployConfig{Options: map[string]string{
		"cleanUrls":     "false",
		"trailingSlash": "true",
	}}}
	files, err := Emit("vercel", cfg, out)
	if err != nil {
		t.Fatalf("Emit: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("files = %d, want 1", len(files))
	}
	data, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	if doc["cleanUrls"] != false {
		t.Errorf("cleanUrls = %v, want false", doc["cleanUrls"])
	}
	if doc["trailingSlash"] != true {
		t.Errorf("trailingSlash = %v, want true", doc["trailingSlash"])
	}
}
