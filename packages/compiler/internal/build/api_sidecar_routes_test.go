package build

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

// TestAPISidecarStaticLeafUnderDynamicDir: a static route file inside a dynamic
// directory (orders/[id]/download.js) is served by the Node and Bun sidecars,
// next to orders/[id].js and a dynamic sibling.
func TestAPISidecarStaticLeafUnderDynamicDir(t *testing.T) {
	for _, runtime := range []string{"node", "bun"} {
		t.Run(runtime, func(t *testing.T) {
			dir := t.TempDir()
			apiDir := filepath.Join(dir, "api")
			files := map[string]string{
				"package.json":                `{"type":"module"}`,
				"api/orders/[id].js":          `export const GET = (_r, c) => Response.json({ route: 'order', id: c.params.id });`,
				"api/orders/[id]/download.js": `export const GET = (_r, c) => Response.json({ route: 'download', id: c.params.id });`,
				"api/orders/[id]/[part].js":   `export const GET = (_r, c) => Response.json({ route: 'part', id: c.params.id, part: c.params.part });`,
			}
			for name, body := range files {
				p := filepath.Join(dir, filepath.FromSlash(name))
				if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			base := startSidecarConformance(t, runtime, apiDir, "")
			for path, want := range map[string]string{
				"/api/orders/7":          `{"route":"order","id":"7"}`,
				"/api/orders/7/download": `{"route":"download","id":"7"}`,
				"/api/orders/7/zip":      `{"route":"part","id":"7","part":"zip"}`,
			} {
				res, err := http.Get(base + path)
				if err != nil {
					t.Fatal(err)
				}
				body, _ := io.ReadAll(res.Body)
				res.Body.Close()
				if res.StatusCode != 200 || string(body) != want {
					t.Errorf("%s: %d %s, want 200 %s", path, res.StatusCode, body, want)
				}
			}
		})
	}
}
