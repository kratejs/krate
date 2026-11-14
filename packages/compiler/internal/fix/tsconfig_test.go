package fix

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/diag"
)

// The tsconfig shipped by the create-krate-app template up to 0.4.0.
const tsconfig040 = `{
  "compilerOptions": {
    "target": "ES2022",
    // 0.4 ignored baseUrl for aliases
    "baseUrl": "./src",
    "paths": {
      "@/*": ["./src/*"]
    }
  },
  "include": ["src"]
}
`

func writeProject(t *testing.T, tsconfig string, dirs ...string) string {
	t.Helper()
	root := t.TempDir()
	for _, d := range dirs {
		if err := os.MkdirAll(filepath.Join(root, d), 0755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "tsconfig.json"), []byte(tsconfig), 0644); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestRunAppliesTSConfigFixAndIsIdempotent(t *testing.T) {
	root := writeProject(t, tsconfig040, "src")
	res, err := Run(root, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Changes) != 1 || res.Changes[0].Code != diag.CodeTSConfigBaseURL {
		t.Fatalf("dry run = %+v", res)
	}
	if got, _ := os.ReadFile(filepath.Join(root, "tsconfig.json")); string(got) != tsconfig040 {
		t.Fatal("dry run wrote tsconfig.json")
	}
	if _, err := Run(root, true); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(filepath.Join(root, "tsconfig.json"))
	if !strings.Contains(string(got), `"baseUrl": "."`) {
		t.Fatalf("apply did not rewrite: %s", got)
	}
	res, err = Run(root, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Changes) != 0 {
		t.Fatalf("second apply should be a no-op: %+v", res.Changes)
	}
}
