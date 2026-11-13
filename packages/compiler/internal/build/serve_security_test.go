package build

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

func TestRewriteDestinationOpenRedirect(t *testing.T) {
	tests := []struct {
		name, path, source, dest, want string
	}{
		{"encoded slash in splat", "/legacy//evil.com", "/legacy/*", "/:splat", "/evil.com"},
		{"backslash in splat", `/legacy/\evil.com`, "/legacy/*", "/:splat", "/%5Cevil.com"},
		{"double backslash param", `/u/\\evil.com`, "/u/:name", "/:name", "/%5C%5Cevil.com"},
		{"scheme in splat stays a path", "/legacy/https://evil.com", "/legacy/*", "/:splat", "/https://evil.com"},
		{"control chars", "/legacy/a\nb", "/legacy/*", "/x/:splat", "/x/a%0Ab"},
		{"append form", `/old/\evil.com`, "/old/*", "/new", "/new/%5Cevil.com"},
		{"configured absolute stays", "/go/x", "/go/*", "https://example.com/:splat", "https://example.com/x"},
		{"configured scheme-relative stays", "/go/x", "/go/*", "//cdn.example.com/:splat", "//cdn.example.com/x"},
		{"spaces escaped", "/legacy/a b", "/legacy/*", "/new/:splat", "/new/a%20b"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := rewriteDestination(tt.path, tt.source, tt.dest)
			if got != tt.want {
				t.Fatalf("rewriteDestination(%q, %q, %q) = %q, want %q", tt.path, tt.source, tt.dest, got, tt.want)
			}
			if strings.HasPrefix(tt.dest, "/") && !strings.HasPrefix(tt.dest, "//") {
				if strings.HasPrefix(got, "//") || strings.HasPrefix(got, `/\`) {
					t.Fatalf("destination %q is not on-site", got)
				}
			}
		})
	}
}

func writeTree(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for rel, content := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestImageSrcOutsidePublicIsRejected(t *testing.T) {
	root := t.TempDir()
	svg := `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 10 10"><circle cx="5" cy="5" r="4"/></svg>`
	writeTree(t, root, map[string]string{
		"src/pages/index.tsx": `export default function Page() {
  return <div><Image src="/../secret.svg" alt="x" /><Image src="/hero.svg" alt="ok" /></div>;
}`,
		"secret.svg":      svg,
		"public/hero.svg": svg,
	})
	cfg := config.Default()
	cfg.Images.Unoptimized = true
	cfg.Resolve(root)
	err := New(root, cfg).BuildAll()
	if err == nil || !strings.Contains(err.Error(), "KR8801") || !strings.Contains(err.Error(), "resolves outside public/") {
		t.Fatalf("an escaping Image src must fail the build with KR8801, got %v", err)
	}
	if strings.Contains(err.Error(), "hero.svg") {
		t.Errorf("the valid Image src was reported: %v", err)
	}
}
