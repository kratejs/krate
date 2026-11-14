package build

import (
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
	"github.com/kratejs/krate/packages/compiler/internal/plugin"
)

// buildServeFixturePlugin compiles the fixture Go plugin and returns its exe.
func buildServeFixturePlugin(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(t.TempDir(), "fixtureplugin.exe")
	cmd := exec.Command("go", "build", "-o", exe, "./internal/plugin/testdata/fixtureplugin")
	cmd.Dir = root
	cmd.Env = os.Environ()
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("building fixture plugin: %v\n%s", err, out)
	}
	return exe
}

// buildServeFixture builds the fixture plugin and wraps it in a package with a
// JS descriptor, returning a config that resolves to the Go binary.
func buildServeFixture(t *testing.T) (string, config.PluginConfig) {
	t.Helper()
	exe := buildServeFixturePlugin(t)
	pkgDir := t.TempDir()
	binDir := filepath.Join(pkgDir, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatal(err)
	}
	binName := "fixtureplugin"
	if runtime.GOOS == "windows" {
		binName += ".exe"
	}
	if err := copyFile(exe, filepath.Join(binDir, binName)); err != nil {
		t.Fatal(err)
	}
	desc := `module.exports = function(options) {
  return {
    name: 'fixture-component',
    runtime: 'go',
    hooks: { ServeRequest: null, ServeResponse: null },
    binaries: { ['` + platformKey() + `']: 'bin/` + binName + `' },
  };
};
`
	if err := os.WriteFile(filepath.Join(pkgDir, "index.js"), []byte(desc), 0644); err != nil {
		t.Fatal(err)
	}
	cfg := config.PluginConfig{Name: "fixture-component", Module: pkgDir, Options: map[string]interface{}{}}
	return pkgDir, cfg
}

func copyFile(src, dst string) error {
	b, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, b, 0755)
}

func platformKey() string {
	return runtime.GOOS + "-" + runtime.GOARCH
}

func TestServeHooksGoPlugin(t *testing.T) {
	_, cfg := buildServeFixture(t)
	defer plugin.CloseGoPlugins()

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(200)
		_, _ = w.Write([]byte("<h1>hello</h1>"))
	})

	root := t.TempDir()

	t.Run("ServeResponse buffers and rewrites", func(t *testing.T) {
		handler := wirePluginServeHandlers(root, &config.Config{Plugins: []config.PluginConfig{cfg}}, next)
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "http://example.com/", nil)
		handler.ServeHTTP(rec, req)
		if body := rec.Body.String(); body != "<h1>hello</h1>\n<!-- served-by-go-plugin -->" {
			t.Errorf("ServeResponse body = %q", body)
		}
		if rec.Header().Get("X-Go-Plugin") != "1" {
			t.Errorf("ServeResponse header X-Go-Plugin = %q", rec.Header().Get("X-Go-Plugin"))
		}
	})

	t.Run("ServeRequest responds", func(t *testing.T) {
		handler := wirePluginServeHandlers(root, &config.Config{Plugins: []config.PluginConfig{cfg}}, next)
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "http://example.com/__krate/veto", nil)
		handler.ServeHTTP(rec, req)
		if rec.Code != 418 {
			t.Errorf("ServeRequest status = %d, want 418", rec.Code)
		}
		if body := rec.Body.String(); body != "teapot-told-me" {
			t.Errorf("ServeRequest body = %q", body)
		}
		if rec.Header().Get("X-Plugin") != "go-serve" {
			t.Errorf("ServeRequest header X-Plugin = %q", rec.Header().Get("X-Plugin"))
		}
	})
}
