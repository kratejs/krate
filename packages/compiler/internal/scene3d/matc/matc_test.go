package matc

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/diag"
)

// fakeMatc writes "compiled:<source>" to the -o path.
const fakeMatc = `#!/bin/sh
out=""
src=""
while [ $# -gt 0 ]; do
  case "$1" in
    -o) out="$2"; shift 2 ;;
    -*) shift ;;
    *) src="$1"; shift ;;
  esac
done
printf 'compiled:' > "$out"
cat "$src" >> "$out"
`

func skipWindows(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake matc is a shell script")
	}
}

func tarball(t *testing.T, name, body string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(zw)
	for _, f := range []struct{ name, body string }{
		{"filament/README.md", "readme"},
		{name, body},
	} {
		if err := tw.WriteHeader(&tar.Header{Name: f.name, Mode: 0o755, Size: int64(len(f.body)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(f.body)); err != nil {
			t.Fatal(err)
		}
	}
	_ = tw.Close()
	_ = zw.Close()
	return buf.Bytes()
}

func noEnv(string) string { return "" }

func hint(err error) string {
	if d, ok := diag.FromError(err); ok {
		return d.Hint
	}
	return ""
}

func TestShaderHash(t *testing.T) {
	// FNV-1a 64 of the empty string is the offset basis.
	if got := ShaderHash(nil); got != "cbf29ce484222325" {
		t.Fatalf("empty hash = %s", got)
	}
	a, b := ShaderHash([]byte("a")), ShaderHash([]byte("b"))
	if a == b || len(a) != 16 {
		t.Fatalf("bad hashes %s %s", a, b)
	}
}

func TestResolveEnvOverride(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "my-matc")
	if err := os.WriteFile(bin, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	o := Options{HomeDir: t.TempDir(), BaseURL: "http://127.0.0.1:1", Getenv: func(k string) string {
		if k == EnvMatc {
			return bin
		}
		return ""
	}}
	got, err := o.Resolve(context.Background())
	if err != nil || got != bin {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestResolveEnvOverrideMissing(t *testing.T) {
	o := Options{Getenv: func(k string) string {
		if k == EnvMatc {
			return "/no/such/matc"
		}
		return ""
	}}
	_, err := o.Resolve(context.Background())
	if err == nil || !strings.Contains(hint(err), EnvMatc) {
		t.Fatalf("err = %v", err)
	}
}

func TestResolveCacheHit(t *testing.T) {
	home := t.TempDir()
	o := Options{HomeDir: home, BaseURL: "http://127.0.0.1:1", Getenv: noEnv}
	dir, _ := o.installDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, o.binName())
	if err := os.WriteFile(bin, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := o.Resolve(context.Background())
	if err != nil || got != bin {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestResolveOffline(t *testing.T) {
	o := Options{HomeDir: t.TempDir(), BaseURL: "http://127.0.0.1:1", Getenv: func(k string) string {
		if k == EnvOffline {
			return "1"
		}
		return ""
	}}
	_, err := o.Resolve(context.Background())
	if h := hint(err); !strings.Contains(h, "unset "+EnvOffline) {
		t.Fatalf("hint = %q (err %v)", h, err)
	}
}

func TestResolveDownload(t *testing.T) {
	skipWindows(t)
	var hits atomic.Int32
	archive := tarball(t, "filament/bin/matc", fakeMatc)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		want := "/v" + FilamentVersion + "/filament-v" + FilamentVersion + "-linux.tgz"
		if r.URL.Path != want {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(archive)
	}))
	defer srv.Close()

	home := t.TempDir()
	o := Options{HomeDir: home, BaseURL: srv.URL, GOOS: "linux", Getenv: noEnv}
	got, err := o.Resolve(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(home, ".krate", "tools", "filament", FilamentVersion, "bin", "matc")
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	if fi, err := os.Stat(got); err != nil || fi.Mode().Perm() != 0o755 {
		t.Fatalf("stat %v mode %v", err, fi)
	}
	entries, _ := os.ReadDir(filepath.Dir(got))
	if len(entries) != 1 {
		t.Fatalf("expected only matc in %s, got %d entries", filepath.Dir(got), len(entries))
	}
	// Second call hits the cache, not the server.
	if _, err := o.Resolve(context.Background()); err != nil || hits.Load() != 1 {
		t.Fatalf("err %v hits %d", err, hits.Load())
	}
}

func TestResolveDownloadErrors(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()
	o := Options{HomeDir: t.TempDir(), BaseURL: srv.URL, GOOS: "linux", Getenv: noEnv}
	_, err := o.Resolve(context.Background())
	if err == nil || !strings.Contains(err.Error(), "404") || !strings.Contains(hint(err), EnvMatc) {
		t.Fatalf("err = %v hint = %q", err, hint(err))
	}

	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(tarball(t, "filament/bin/other", "x"))
	}))
	defer bad.Close()
	o = Options{HomeDir: t.TempDir(), BaseURL: bad.URL, GOOS: "linux", Getenv: noEnv}
	if _, err := o.Resolve(context.Background()); err == nil || !strings.Contains(err.Error(), "no bin/matc") {
		t.Fatalf("err = %v", err)
	}
}

func TestPlatformAsset(t *testing.T) {
	for goos, want := range map[string]string{"linux": "linux", "darwin": "mac", "windows": "windows"} {
		got, err := Options{GOOS: goos}.platformAsset()
		if err != nil || got != want {
			t.Fatalf("%s: %q %v", goos, got, err)
		}
	}
	if _, err := (Options{GOOS: "plan9"}).platformAsset(); err == nil {
		t.Fatal("expected error")
	}
	if (Options{GOOS: "windows"}).binName() != "matc.exe" {
		t.Fatal("windows binary name")
	}
}

func TestCompileCacheMissThenHit(t *testing.T) {
	skipWindows(t)
	bin := filepath.Join(t.TempDir(), "matc")
	if err := os.WriteFile(bin, []byte(fakeMatc), 0o755); err != nil {
		t.Fatal(err)
	}
	o := Options{Getenv: func(k string) string {
		if k == EnvMatc {
			return bin
		}
		return ""
	}}
	outDir := filepath.Join(t.TempDir(), "assets")
	src := []byte("material { name : x }")

	p, cached, err := o.Compile(context.Background(), src, outDir)
	if err != nil || cached {
		t.Fatalf("miss: %q cached=%v err=%v", p, cached, err)
	}
	if filepath.Base(p) != ShaderHash(src)+".filamat" {
		t.Fatalf("name %s", p)
	}
	data, _ := os.ReadFile(p)
	if string(data) != "compiled:"+string(src) {
		t.Fatalf("content %q", data)
	}
	if entries, _ := os.ReadDir(outDir); len(entries) != 1 {
		t.Fatalf("temp files left in outDir: %v", entries)
	}

	// Hit must not run matc: point the override at a missing binary.
	o.Getenv = func(k string) string {
		if k == EnvMatc {
			return "/no/such/matc"
		}
		return ""
	}
	p2, cached, err := o.Compile(context.Background(), src, outDir)
	if err != nil || !cached || p2 != p {
		t.Fatalf("hit: %q cached=%v err=%v", p2, cached, err)
	}
}

func TestCompileMatcFailure(t *testing.T) {
	skipWindows(t)
	bin := filepath.Join(t.TempDir(), "matc")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\necho 'bad glsl' >&2\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	o := Options{Getenv: func(k string) string {
		if k == EnvMatc {
			return bin
		}
		return ""
	}}
	_, _, err := o.Compile(context.Background(), []byte("x"), t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "bad glsl") {
		t.Fatalf("err = %v", err)
	}
}
