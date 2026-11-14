package updater

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
)

func mustKeys(t *testing.T) (priv ed25519.PrivateKey, pubB64 string) {
	t.Helper()
	p, pub, err := GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	k, err := PrivateKeyFromSeed(p)
	if err != nil {
		t.Fatal(err)
	}
	return k, pub
}

var testMeta = Meta{AppID: "com.acme.app", Target: "linux-amd64", Version: "1.2.0"}

func sign(t *testing.T, k ed25519.PrivateKey, data []byte) string {
	t.Helper()
	return signAs(t, k, testMeta, data)
}

func signAs(t *testing.T, k ed25519.PrivateKey, m Meta, data []byte) string {
	t.Helper()
	s, err := Sign(k, m, bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func tgz(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, body := range files {
		if strings.HasSuffix(name, "/") {
			if err := tw.WriteHeader(&tar.Header{Name: name, Typeflag: tar.TypeDir, Mode: 0o755}); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err := tw.WriteHeader(&tar.Header{Name: name, Typeflag: tar.TypeReg, Mode: 0o755, Size: int64(len(body))}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	tw.Close()
	gz.Close()
	return buf.Bytes()
}

func zipOf(t *testing.T, files map[string]string, links map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range files {
		h := &zip.FileHeader{Name: name, Method: zip.Deflate}
		h.SetMode(0o755)
		w, _ := zw.CreateHeader(h)
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	for name, target := range links {
		h := &zip.FileHeader{Name: name}
		h.SetMode(fs.ModeSymlink | 0o755)
		w, _ := zw.CreateHeader(h)
		if _, err := w.Write([]byte(target)); err != nil {
			t.Fatal(err)
		}
	}
	zw.Close()
	return buf.Bytes()
}

func feedServer(t *testing.T, version string, artifact []byte, sig string, hits *atomic.Int32) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	var srv *httptest.Server
	mux.HandleFunc("/feed/", func(w http.ResponseWriter, r *http.Request) {
		if hits != nil {
			hits.Add(1)
		}
		if err := json.NewEncoder(w).Encode(Feed{Version: version, Notes: "n", PubDate: "2026-01-01T00:00:00Z",
			URL: srv.URL + "/dl/app-" + version + "-linux-amd64.tar.gz", Signature: sig, Size: int64(len(artifact))}); err != nil {
			t.Error(err)
		}
	})
	mux.HandleFunc("/dl/", func(w http.ResponseWriter, r *http.Request) {
		http.ServeContent(w, r, "a.tar.gz", timeZero, bytes.NewReader(artifact))
	})
	srv = httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestSignatureGoodBadWrongKey(t *testing.T) {
	k, pub := mustKeys(t)
	data := []byte("the update")
	sig := sign(t, k, data)
	pk, _ := DecodePublicKey(pub)
	if err := Verify(pk, testMeta, bytes.NewReader(data), sig); err != nil {
		t.Fatal(err)
	}
	if err := Verify(pk, testMeta, bytes.NewReader([]byte("tampered")), sig); !errors.Is(err, ErrBadSignature) {
		t.Fatalf("tampered: %v", err)
	}
	_, other := mustKeys(t)
	opk, _ := DecodePublicKey(other)
	if err := Verify(opk, testMeta, bytes.NewReader(data), sig); !errors.Is(err, ErrBadSignature) {
		t.Fatalf("wrong key: %v", err)
	}
	if err := Verify(pk, testMeta, bytes.NewReader(data), "not base64!"); !errors.Is(err, ErrBadSignature) {
		t.Fatalf("garbage: %v", err)
	}
	if _, err := DecodePublicKey("AAAA"); err == nil {
		t.Fatal("short key accepted")
	}
}

func TestCheck(t *testing.T) {
	k, pub := mustKeys(t)
	art := tgz(t, map[string]string{"app": "new"})
	sig := sign(t, k, art)
	srv := feedServer(t, "1.2.0", art, sig, nil)
	cfg := Config{URL: srv.URL + "/feed/{target}/{version}", AppID: "com.acme.app", PublicKey: pub, Version: "1.0.0", Target: "linux-amd64", Insecure: true}

	u, err := Check(context.Background(), cfg)
	if err != nil || u == nil || u.Version != "1.2.0" || u.Notes != "n" || u.Size != int64(len(art)) {
		t.Fatalf("%+v %v", u, err)
	}
	cfg.Version = "1.2.0"
	if u, err := Check(context.Background(), cfg); err != nil || u != nil {
		t.Fatalf("up to date: %+v %v", u, err)
	}
	cfg.Version = "1.0.0"
	cfg.Insecure = false
	if _, err := Check(context.Background(), cfg); err == nil {
		t.Fatal("http feed accepted without Insecure")
	}
	if _, err := Check(context.Background(), Config{}); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("%v", err)
	}
	empty := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
	defer empty.Close()
	if u, err := Check(context.Background(), Config{URL: empty.URL, AppID: "com.acme.app", PublicKey: pub, Version: "1.0.0", Insecure: true}); err != nil || u != nil {
		t.Fatalf("204: %+v %v", u, err)
	}
}

func checked(t *testing.T, art []byte, sig, pub string) (*Update, *httptest.Server, string) {
	t.Helper()
	srv := feedServer(t, "1.2.0", art, sig, nil)
	cache := t.TempDir()
	u, err := Check(context.Background(), Config{URL: srv.URL + "/feed/{target}", AppID: "com.acme.app", Target: "linux-amd64", PublicKey: pub, Version: "1.0.0", CacheDir: cache, Insecure: true})
	if err != nil || u == nil {
		t.Fatalf("%+v %v", u, err)
	}
	return u, srv, cache
}

func TestDownloadVerifiesAndProgress(t *testing.T) {
	k, pub := mustKeys(t)
	art := tgz(t, map[string]string{"app": strings.Repeat("x", 200000)})
	u, _, _ := checked(t, art, sign(t, k, art), pub)
	var last, total int64
	if err := u.Download(context.Background(), func(d, tt int64) { last, total = d, tt }); err != nil {
		t.Fatal(err)
	}
	if last != int64(len(art)) || total != int64(len(art)) {
		t.Fatalf("progress %d/%d of %d", last, total, len(art))
	}
	b, _ := os.ReadFile(u.Path())
	if !bytes.Equal(b, art) {
		t.Fatal("content differs")
	}
	// A second call reuses the verified file without the network.
	u.URL = "http://127.0.0.1:1/dl/" + stagedName(u.URL)
	if err := u.Download(context.Background(), nil); err != nil {
		t.Fatalf("cached download: %v", err)
	}
}

func TestDownloadRejectsBadSignature(t *testing.T) {
	k, pub := mustKeys(t)
	art := tgz(t, map[string]string{"app": "payload"})
	sig := sign(t, k, []byte("something else"))
	u, _, cache := checked(t, art, sig, pub)
	if err := u.Download(context.Background(), nil); !errors.Is(err, ErrBadSignature) {
		t.Fatalf("%v", err)
	}
	if u.Path() != "" {
		t.Fatal("path set for a rejected file")
	}
	left, _ := filepath.Glob(filepath.Join(cache, "updates", "*", "*"))
	if len(left) != 0 {
		t.Fatalf("rejected download left files: %v", left)
	}
	if err := u.Apply(); !errors.Is(err, ErrNotDownloaded) {
		t.Fatalf("apply: %v", err)
	}
}

// ---- apply ----

func readFile(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func write(t *testing.T, p, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
}

func staged(t *testing.T, name string, data []byte) string {
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestApplyLinuxReplacesExecutableAndAppDir(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "acme"), "old-exe")
	write(t, filepath.Join(dir, "app", "app.json"), "old-manifest")
	write(t, filepath.Join(dir, "app", "stale.txt"), "stale")
	write(t, filepath.Join(dir, "keep.txt"), "user file")
	art := staged(t, "u.tar.gz", tgz(t, map[string]string{"acme": "new-exe", "app/": "", "app/app.json": "new-manifest"}))
	u := &Update{Version: "1.2.0", path: art, cfg: Config{Exe: filepath.Join(dir, "acme"), GOOS: "linux", AppImage: "-"}}
	u.cfg.AppImage = ""
	t.Setenv("APPIMAGE", "")
	if err := u.Apply(); err != nil {
		t.Fatal(err)
	}
	if readFile(t, filepath.Join(dir, "acme")) != "new-exe" || readFile(t, filepath.Join(dir, "app", "app.json")) != "new-manifest" {
		t.Fatal("files not replaced")
	}
	if _, err := os.Stat(filepath.Join(dir, "app", "stale.txt")); err == nil {
		t.Fatal("stale file of the old app dir survived")
	}
	if readFile(t, filepath.Join(dir, "keep.txt")) != "user file" {
		t.Fatal("unrelated file touched")
	}
	if st, _ := os.Stat(filepath.Join(dir, "acme")); runtime.GOOS != "windows" && st.Mode().Perm()&0o100 == 0 { // no mode bits on Windows
		t.Fatal("new executable is not executable")
	}
	left, _ := filepath.Glob(filepath.Join(dir, ".krate-*"))
	if len(left) != 0 {
		t.Fatalf("leftovers: %v", left)
	}
}

// failFS fails the nth Rename whose destination matches.
type failFS struct {
	OSFS
	failDst string
	renames []string
	failed  bool
}

func (f *failFS) Rename(a, b string) error {
	f.renames = append(f.renames, filepath.Base(a)+"->"+filepath.Base(b))
	if f.failDst != "" && b == f.failDst && !f.failed {
		f.failed = true
		return errors.New("injected rename failure")
	}
	return f.OSFS.Rename(a, b)
}

func TestApplyRollsBackOnFailure(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "acme"), "old-exe")
	write(t, filepath.Join(dir, "app", "app.json"), "old-manifest")
	art := staged(t, "u.tar.gz", tgz(t, map[string]string{"acme": "new-exe", "app/": "", "app/app.json": "new-manifest"}))
	ffs := &failFS{failDst: filepath.Join(dir, "app")}
	u := &Update{Version: "1.2.0", path: art, cfg: Config{Exe: filepath.Join(dir, "acme"), GOOS: "windows", FS: ffs}}
	err := u.Apply()
	if err == nil || !strings.Contains(err.Error(), "injected") {
		t.Fatalf("%v", err)
	}
	if readFile(t, filepath.Join(dir, "acme")) != "old-exe" || readFile(t, filepath.Join(dir, "app", "app.json")) != "old-manifest" {
		t.Fatal("old install not restored")
	}
}

func TestApplyMacBundle(t *testing.T) {
	root := t.TempDir()
	bundle := filepath.Join(root, "Acme.app")
	write(t, filepath.Join(bundle, "Contents", "MacOS", "acme"), "old")
	write(t, filepath.Join(bundle, "Contents", "Resources", "old.txt"), "old")
	art := staged(t, "u.zip", zipOf(t, map[string]string{
		"Acme.app/Contents/MacOS/acme":           "new",
		"Acme.app/Contents/Resources/new.txt":    "new",
		"Acme.app/Contents/Resources/real.dylib": "lib",
	}, map[string]string{"Acme.app/Contents/Resources/link.dylib": "real.dylib"}))
	ffs := &failFS{}
	u := &Update{Version: "1.2.0", path: art, cfg: Config{Exe: filepath.Join(bundle, "Contents", "MacOS", "acme"), GOOS: "darwin", FS: ffs}}
	if err := u.Apply(); err != nil {
		t.Fatal(err)
	}
	if readFile(t, filepath.Join(bundle, "Contents", "MacOS", "acme")) != "new" {
		t.Fatal("bundle not replaced")
	}
	if _, err := os.Stat(filepath.Join(bundle, "Contents", "Resources", "old.txt")); err == nil {
		t.Fatal("old resource survived")
	}
	if l, err := os.Readlink(filepath.Join(bundle, "Contents", "Resources", "link.dylib")); err != nil || l != "real.dylib" {
		t.Fatalf("symlink not preserved: %q %v", l, err)
	}
	entries, _ := os.ReadDir(root)
	if len(entries) != 1 {
		t.Fatalf("leftovers in %s: %v", root, entries)
	}
	// Two renames: old bundle aside, new bundle in.
	if len(ffs.renames) != 2 {
		t.Fatalf("renames %v", ffs.renames)
	}
}

func TestExtractRejectsEscapes(t *testing.T) {
	dir := t.TempDir()
	bad := staged(t, "u.zip", zipOf(t, map[string]string{"../evil": "x"}, nil))
	if err := extractArchive(OSFS{}, bad, filepath.Join(dir, "out")); err == nil {
		t.Fatal("zip slip accepted")
	}
	if _, err := os.Stat(filepath.Join(dir, "evil")); err == nil {
		t.Fatal("file escaped")
	}
	link := staged(t, "l.zip", zipOf(t, nil, map[string]string{"l": "/etc/passwd"}))
	if err := extractArchive(OSFS{}, link, filepath.Join(dir, "out2")); err == nil {
		t.Fatal("absolute symlink accepted")
	}
	up := staged(t, "u2.zip", zipOf(t, nil, map[string]string{"a/l": "../../x"}))
	if err := extractArchive(OSFS{}, up, filepath.Join(dir, "out3")); err == nil {
		t.Fatal("escaping symlink accepted")
	}
}

var _ = io.EOF

func TestExtractRejectsWritingThroughSymlinkChain(t *testing.T) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: "a/", Typeflag: tar.TypeDir, Mode: 0o755}); err != nil {
		t.Fatal(err)
	}
	// Each link stays inside the root when checked on its own.
	if err := tw.WriteHeader(&tar.Header{Name: "a/b", Typeflag: tar.TypeSymlink, Linkname: "..", Mode: 0o755}); err != nil {
		t.Fatal(err)
	}
	if err := tw.WriteHeader(&tar.Header{Name: "c", Typeflag: tar.TypeSymlink, Linkname: "a/b/..", Mode: 0o755}); err != nil {
		t.Fatal(err)
	}
	body := "pwned"
	if err := tw.WriteHeader(&tar.Header{Name: "c/evil", Typeflag: tar.TypeReg, Mode: 0o644, Size: int64(len(body))}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write([]byte(body)); err != nil {
		t.Fatal(err)
	}
	tw.Close()
	gz.Close()

	dir := t.TempDir()
	art := staged(t, "chain.tar.gz", buf.Bytes())
	err := extractArchive(OSFS{}, art, filepath.Join(dir, "out", "stage"))
	if err == nil {
		t.Fatal("a symlink chain let an entry through")
	}
	if _, serr := os.Stat(filepath.Join(dir, "out", "evil")); serr == nil {
		t.Fatal("file written outside the staging directory")
	}
}

func TestRelabeledOlderArtifactRefused(t *testing.T) {
	k, pub := mustKeys(t)
	// A genuine, validly signed 1.0.1 artifact; the feed claims 1.2.0.
	art := tgz(t, map[string]string{"app": "old"})
	old := testMeta
	old.Version = "1.0.1"
	srv := feedServer(t, "1.2.0", art, signAs(t, k, old, art), nil)
	u, err := Check(context.Background(), Config{URL: srv.URL + "/feed/{target}", AppID: testMeta.AppID, Target: testMeta.Target,
		PublicKey: pub, Version: "1.1.0", CacheDir: t.TempDir(), Insecure: true})
	if err != nil || u == nil {
		t.Fatalf("%+v %v", u, err)
	}
	if err := u.Download(context.Background(), nil); !errors.Is(err, ErrBadSignature) {
		t.Fatalf("relabeled artifact: %v", err)
	}
	// Another target's artifact under this target is refused too.
	other := testMeta
	other.Target = "windows-amd64"
	srv2 := feedServer(t, "1.2.0", art, signAs(t, k, other, art), nil)
	u2, _ := Check(context.Background(), Config{URL: srv2.URL + "/feed/x", AppID: testMeta.AppID, Target: testMeta.Target,
		PublicKey: pub, Version: "1.1.0", CacheDir: t.TempDir(), Insecure: true})
	if err := u2.Download(context.Background(), nil); !errors.Is(err, ErrBadSignature) {
		t.Fatalf("other target: %v", err)
	}
}
