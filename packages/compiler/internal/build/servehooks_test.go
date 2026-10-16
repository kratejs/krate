package build

import (
	"fmt"
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

// TestServeHooksNoHooksConfig guards against a plugin that implements no serve
// hooks wiping the buffered response: the serve-hook wrapper must leave the
// page untouched when plugins return { action: 'continue' }.
func TestServeHooksNoHooksConfig(t *testing.T) {
	root := t.TempDir()
	pluginDir := filepath.Join(root, "plugins", "no-serve-hooks")
	_ = os.MkdirAll(pluginDir, 0755)
	js := `export default {
  name: "no-serve-hooks",
  hooks: {
    BeforeBuild(ctx, options, krate) { return {}; },
  },
};
`
	if err := os.WriteFile(filepath.Join(pluginDir, "index.js"), []byte(js), 0644); err != nil {
		t.Fatal(err)
	}
	cfg := config.PluginConfig{Name: "no-serve-hooks", Module: filepath.Join(pluginDir, "index.js"), Options: map[string]interface{}{}}

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(200)
		_, _ = w.Write([]byte("<h1>intact</h1>"))
	})

	h := wirePluginServeHandlers(root, &config.Config{Plugins: []config.PluginConfig{cfg}}, next)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "http://example.com/", nil)
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	if body := rec.Body.String(); body != "<h1>intact</h1>" {
		t.Errorf("body = %q, want %q (serve hooks must not wipe the response)", body, "<h1>intact</h1>")
	}
}

// TestServeResponseContentLengthReconciled guards against the stale
// Content-Length bug: the buffered chain may record a length that no longer
// matches after a plugin rewrites the body, causing ERR_CONTENT_LENGTH_MISMATCH.
// The wiring must reconcile Content-Length with the bytes actually written.
func TestServeResponseContentLengthReconciled(t *testing.T) {
	root := t.TempDir()
	pluginDir := filepath.Join(root, "plugins", "js-clen")
	_ = os.MkdirAll(pluginDir, 0755)
	js := `export default {
  name: "js-clen",
  hooks: {
    ServeResponse(ctx, options, krate) {
      return { status: ctx.status, body: ctx.body + "\n<!-- rewritten -->", headers: ctx.headers || {} };
    },
  },
};
`
	if err := os.WriteFile(filepath.Join(pluginDir, "index.js"), []byte(js), 0644); err != nil {
		t.Fatal(err)
	}
	cfg := config.PluginConfig{Name: "js-clen", Module: filepath.Join(pluginDir, "index.js"), Options: map[string]interface{}{}}

	body := "<p>original</p>"
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Simulate a static-file handler that stamped a Content-Length before
		// buffering; the plugin then rewrites the body to a different length.
		w.Header().Set("Content-Length", "99999")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(body))
	})

	h := wirePluginServeHandlers(root, &config.Config{Plugins: []config.PluginConfig{cfg}}, next)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "http://example.com/", nil)
	h.ServeHTTP(rec, req)

	wantLen := len(body) + len("\n<!-- rewritten -->")
	if got := rec.Header().Get("Content-Length"); got != fmt.Sprintf("%d", wantLen) {
		t.Errorf("Content-Length = %q, want %d (must match rewritten body)", got, wantLen)
	}
	if got := len(rec.Body.Bytes()); got != wantLen {
		t.Errorf("recorded body length = %d, want %d", got, wantLen)
	}
}

func TestServeHooksJSPlugin(t *testing.T) {
	root := t.TempDir()
	out := filepath.Join(root, "dist")
	_ = os.MkdirAll(out, 0755)
	pluginDir := filepath.Join(root, "plugins", "js-serve")
	_ = os.MkdirAll(pluginDir, 0755)
	js := `export default {
  name: "js-serve",
  order: 10,
  hooks: {
    ServeRequest(ctx, options, krate) {
      if (ctx.path === "/js-veto") {
        return { action: "respond", status: 403, body: "js-forbidden", headers: { "x-js": "1" } };
      }
      return { action: "continue" };
    },
    ServeResponse(ctx, options, krate) {
      return { status: ctx.status, body: ctx.body + "\n<!-- js-served -->", headers: Object.assign({ "x-js-serve": "1" }, ctx.headers || {}) };
    },
  },
};
`
	if err := os.WriteFile(filepath.Join(pluginDir, "index.js"), []byte(js), 0644); err != nil {
		t.Fatal(err)
	}
	cfg := config.PluginConfig{Name: "js-serve", Module: filepath.Join(pluginDir, "index.js"), Options: map[string]interface{}{}}

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write([]byte("<p>js body</p>"))
	})

	t.Run("ServeResponse buffers and rewrites", func(t *testing.T) {
		h := wirePluginServeHandlers(root, &config.Config{Plugins: []config.PluginConfig{cfg}}, next)
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "http://example.com/", nil)
		h.ServeHTTP(rec, req)
		if body := rec.Body.String(); body != "<p>js body</p>\n<!-- js-served -->" {
			t.Errorf("JS ServeResponse body = %q", body)
		}
		if rec.Header().Get("X-Js-Serve") != "1" {
			t.Errorf("JS ServeResponse header = %q", rec.Header().Get("X-Js-Serve"))
		}
	})

	t.Run("ServeRequest responds", func(t *testing.T) {
		h := wirePluginServeHandlers(root, &config.Config{Plugins: []config.PluginConfig{cfg}}, next)
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "http://example.com/js-veto", nil)
		h.ServeHTTP(rec, req)
		if rec.Code != 403 {
			t.Errorf("JS ServeRequest status = %d, want 403", rec.Code)
		}
		if body := rec.Body.String(); body != "js-forbidden" {
			t.Errorf("JS ServeRequest body = %q", body)
		}
	})
}
