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
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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

func sign(t *testing.T, k ed25519.PrivateKey, data []byte) string {
	t.Helper()
	s, err := Sign(k, bytes.NewReader(data))
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
			tw.WriteHeader(&tar.Header{Name: name, Typeflag: tar.TypeDir, Mode: 0o755})
			continue
		}
		tw.WriteHeader(&tar.Header{Name: name, Typeflag: tar.TypeReg, Mode: 0o755, Size: int64(len(body))})
		tw.Write([]byte(body))
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
		w.Write([]byte(body))
	}
	for name, target := range links {
		h := &zip.FileHeader{Name: name}
		h.SetMode(fs.ModeSymlink | 0o755)
		w, _ := zw.CreateHeader(h)
		w.Write([]byte(target))
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
		json.NewEncoder(w).Encode(Feed{Version: version, Notes: "n", PubDate: "2026-01-01T00:00:00Z",
			URL: srv.URL + "/dl/app-" + version + "-linux-amd64.tar.gz", Signature: sig, Size: int64(len(artifact))})
	})
	mux.HandleFunc("/dl/", func(w http.ResponseWriter, r *http.Request) {
		http.ServeContent(w, r, "a.tar.gz", timeZero, bytes.NewReader(artifact))
	})
	srv = httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestExpandURL(t *testing.T) {
	got := ExpandURL("https://x.test/{target}/{version}.json", "linux-amd64", "1.0.0-beta.1")
	if got != "https://x.test/linux-amd64/1.0.0-beta.1.json" {
		t.Fatal(got)
	}
}

func TestParseFeed(t *testing.T) {
	k, _ := mustKeys(t)
	sig := sign(t, k, []byte("x"))
	ok := fmt.Sprintf(`{"version":"1.2.0","url":"https://a/b","signature":%q,"size":3}`, sig)
	if f, err := ParseFeed([]byte(ok)); err != nil || f.Version != "1.2.0" || f.Size != 3 {
		t.Fatalf("%+v %v", f, err)
	}
	for name, doc := range map[string]string{
		"json":      `{`,
		"version":   fmt.Sprintf(`{"version":"x","url":"u","signature":%q}`, sig),
		"url":       fmt.Sprintf(`{"version":"1.0.0","signature":%q}`, sig),
		"signature": `{"version":"1.0.0","url":"u","signature":"AAAA"}`,
	} {
		if _, err := ParseFeed([]byte(doc)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestCompareVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"1.2.0", "1.1.9", 1}, {"1.0.0", "1.0.0", 0}, {"1.0.0-rc.1", "1.0.0", -1},
		{"1.0.0-alpha", "1.0.0-alpha.1", -1}, {"1.0.0-alpha.2", "1.0.0-alpha.10", -1},
		{"1.0.0-1", "1.0.0-alpha", -1}, {"2.0.0+b1", "2.0.0+b2", 0}, {"v1.0.1", "1.0.0", 1},
	}
	for _, c := range cases {
		a, _ := parseVersion(c.a)
		b, _ := parseVersion(c.b)
		if got := compareVersions(a, b); got != c.want {
			t.Errorf("%s vs %s = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestSignatureGoodBadWrongKey(t *testing.T) {
	k, pub := mustKeys(t)
	data := []byte("the update")
	sig := sign(t, k, data)
	pk, _ := DecodePublicKey(pub)
	if err := Verify(pk, bytes.NewReader(data), sig); err != nil {
		t.Fatal(err)
	}
	if err := Verify(pk, bytes.NewReader([]byte("tampered")), sig); !errors.Is(err, ErrBadSignature) {
		t.Fatalf("tampered: %v", err)
	}
	_, other := mustKeys(t)
	opk, _ := DecodePublicKey(other)
	if err := Verify(opk, bytes.NewReader(data), sig); !errors.Is(err, ErrBadSignature) {
		t.Fatalf("wrong key: %v", err)
	}
	if err := Verify(pk, bytes.NewReader(data), "not base64!"); !errors.Is(err, ErrBadSignature) {
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
	cfg := Config{URL: srv.URL + "/feed/{target}/{version}", PublicKey: pub, Version: "1.0.0", Target: "linux-amd64", Insecure: true}

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
	if u, err := Check(context.Background(), Config{URL: empty.URL, PublicKey: pub, Version: "1.0.0", Insecure: true}); err != nil || u != nil {
		t.Fatalf("204: %+v %v", u, err)
	}
}

func checked(t *testing.T, art []byte, sig, pub string) (*Update, *httptest.Server, string) {
	t.Helper()
	srv := feedServer(t, "1.2.0", art, sig, nil)
	cache := t.TempDir()
	u, err := Check(context.Background(), Config{URL: srv.URL + "/feed/{target}", PublicKey: pub, Version: "1.0.0", CacheDir: cache, Insecure: true})
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

func TestDownloadResumes(t *testing.T) {
	k, pub := mustKeys(t)
	art := tgz(t, map[string]string{"app": strings.Repeat("abc", 100000)})
	u, srv, cache := checked(t, art, sign(t, k, art), pub)
	// Leave a partial file as an interrupted run would.
	part := filepath.Join(cache, "updates", "1.2.0", stagedName(u.URL)+".part")
	os.MkdirAll(filepath.Dir(part), 0o700)
	half := len(art) / 2
	os.WriteFile(part, art[:half], 0o600)

	var ranged atomic.Value
	srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ranged.Store(r.Header.Get("Range"))
		http.ServeContent(w, r, "a.tar.gz", timeZero, bytes.NewReader(art))
	})
	var first int64 = -1
	err := u.Download(context.Background(), func(d, _ int64) {
		if first < 0 {
			first = d
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := ranged.Load().(string); got != fmt.Sprintf("bytes=%d-", half) {
		t.Fatalf("Range header %q", got)
	}
	if first != int64(half) {
		t.Fatalf("progress started at %d, want %d", first, half)
	}
	b, _ := os.ReadFile(u.Path())
	if !bytes.Equal(b, art) {
		t.Fatal("resumed file differs")
	}
}

func TestDownloadRestartsWhenServerIgnoresRange(t *testing.T) {
	k, pub := mustKeys(t)
	art := tgz(t, map[string]string{"app": strings.Repeat("q", 5000)})
	u, srv, cache := checked(t, art, sign(t, k, art), pub)
	part := filepath.Join(cache, "updates", "1.2.0", stagedName(u.URL)+".part")
	os.MkdirAll(filepath.Dir(part), 0o700)
	os.WriteFile(part, art[:100], 0o600)
	srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(art) })
	if err := u.Download(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(u.Path())
	if !bytes.Equal(b, art) {
		t.Fatal("file differs")
	}
}

func TestDownloadCancel(t *testing.T) {
	k, pub := mustKeys(t)
	art := tgz(t, map[string]string{"app": "x"})
	u, _, _ := checked(t, art, sign(t, k, art), pub)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := u.Download(ctx, nil); err == nil {
		t.Fatal("cancelled download succeeded")
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
	os.MkdirAll(filepath.Dir(p), 0o755)
	if err := os.WriteFile(p, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
}

func staged(t *testing.T, name string, data []byte) string {
	p := filepath.Join(t.TempDir(), name)
	os.WriteFile(p, data, 0o644)
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
	if st, _ := os.Stat(filepath.Join(dir, "acme")); st.Mode().Perm()&0o100 == 0 {
		t.Fatal("new executable is not executable")
	}
	left, _ := filepath.Glob(filepath.Join(dir, ".krate-*"))
	if len(left) != 0 {
		t.Fatalf("leftovers: %v", left)
	}
}

func TestApplyRequiresExecutableInArtifact(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "acme"), "old")
	art := staged(t, "u.tar.gz", tgz(t, map[string]string{"other": "x"}))
	u := &Update{Version: "1.2.0", path: art, cfg: Config{Exe: filepath.Join(dir, "acme"), GOOS: "linux"}}
	t.Setenv("APPIMAGE", "")
	if err := u.Apply(); err == nil || !strings.Contains(err.Error(), "does not contain acme") {
		t.Fatalf("%v", err)
	}
	if readFile(t, filepath.Join(dir, "acme")) != "old" {
		t.Fatal("exe changed")
	}
}

func TestApplyWrapperDirectory(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "acme"), "old")
	art := staged(t, "u.tar.gz", tgz(t, map[string]string{"acme-1.2.0/": "", "acme-1.2.0/acme": "new"}))
	u := &Update{Version: "1.2.0", path: art, cfg: Config{Exe: filepath.Join(dir, "acme"), GOOS: "linux"}}
	t.Setenv("APPIMAGE", "")
	if err := u.Apply(); err != nil {
		t.Fatal(err)
	}
	if readFile(t, filepath.Join(dir, "acme")) != "new" {
		t.Fatal("not replaced")
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

// The Windows strategy renames the running executable aside before the new
// one is moved in; on any host the order of operations is the same.
func TestApplyWindowsOrder(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "acme.exe"), "old")
	art := staged(t, "u.zip", zipOf(t, map[string]string{"acme.exe": "new"}, nil))
	ffs := &failFS{}
	u := &Update{Version: "1.2.0", path: art, cfg: Config{Exe: filepath.Join(dir, "acme.exe"), GOOS: "windows", FS: ffs}}
	if err := u.Apply(); err != nil {
		t.Fatal(err)
	}
	if len(ffs.renames) != 2 || ffs.renames[0] != "acme.exe->acme.exe" {
		t.Fatalf("renames %v", ffs.renames)
	}
	// First rename moves the old exe into the trash dir, second moves the new one in.
	if readFile(t, filepath.Join(dir, "acme.exe")) != "new" {
		t.Fatal("not replaced")
	}
}

func TestCleanupStale(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "acme"), "x")
	write(t, filepath.Join(dir, trashName, "acme"), "old")
	write(t, filepath.Join(dir, ".krate-new-1.2.0", "acme"), "half")
	CleanupStale(filepath.Join(dir, "acme"))
	for _, n := range []string{trashName, ".krate-new-1.2.0"} {
		if _, err := os.Stat(filepath.Join(dir, n)); err == nil {
			t.Errorf("%s survived", n)
		}
	}
	if readFile(t, filepath.Join(dir, "acme")) != "x" {
		t.Fatal("exe removed")
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

func TestApplyMacBundleRestoresOnFailure(t *testing.T) {
	root := t.TempDir()
	bundle := filepath.Join(root, "Acme.app")
	write(t, filepath.Join(bundle, "Contents", "MacOS", "acme"), "old")
	art := staged(t, "u.zip", zipOf(t, map[string]string{"Acme.app/Contents/MacOS/acme": "new"}, nil))
	ffs := &failFS{failDst: bundle}
	u := &Update{Version: "1.2.0", path: art, cfg: Config{Exe: filepath.Join(bundle, "Contents", "MacOS", "acme"), GOOS: "darwin", FS: ffs}}
	// Fail only the install rename; the restore rename (also to bundle) must pass.
	cnt := 0
	wrapped := &countFailFS{failFS: ffs, bundle: bundle, cnt: &cnt}
	u.cfg.FS = wrapped
	if err := u.Apply(); err == nil {
		t.Fatal("expected failure")
	}
	if readFile(t, filepath.Join(bundle, "Contents", "MacOS", "acme")) != "old" {
		t.Fatal("old bundle not restored")
	}
}

type countFailFS struct {
	*failFS
	bundle string
	cnt    *int
}

func (c *countFailFS) Rename(a, b string) error {
	if b == c.bundle {
		*c.cnt++
		if *c.cnt == 1 {
			return errors.New("injected install failure")
		}
	}
	return c.OSFS.Rename(a, b)
}

func TestApplyMacNeedsBundle(t *testing.T) {
	art := staged(t, "u.zip", zipOf(t, map[string]string{"x": "y"}, nil))
	u := &Update{Version: "1.2.0", path: art, cfg: Config{Exe: "/opt/acme/acme", GOOS: "darwin"}}
	if err := u.Apply(); err == nil || !strings.Contains(err.Error(), ".app") {
		t.Fatalf("%v", err)
	}
}

func TestApplyAppImage(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "Acme.AppImage")
	write(t, target, "old-image")
	art := staged(t, "Acme-1.2.0.AppImage", []byte("new-image"))
	u := &Update{Version: "1.2.0", path: art, cfg: Config{Exe: "/tmp/.mount_x/AppRun", GOOS: "linux", AppImage: target}}
	if err := u.Apply(); err != nil {
		t.Fatal(err)
	}
	if readFile(t, target) != "new-image" {
		t.Fatal("not replaced")
	}
	if st, _ := os.Stat(target); st.Mode().Perm()&0o100 == 0 {
		t.Fatal("not executable")
	}
	if _, err := os.Stat(target + ".new"); err == nil {
		t.Fatal("temp file left")
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
	tw.WriteHeader(&tar.Header{Name: "a/", Typeflag: tar.TypeDir, Mode: 0o755})
	// Each link stays inside the root when checked on its own.
	tw.WriteHeader(&tar.Header{Name: "a/b", Typeflag: tar.TypeSymlink, Linkname: "..", Mode: 0o755})
	tw.WriteHeader(&tar.Header{Name: "c", Typeflag: tar.TypeSymlink, Linkname: "a/b/..", Mode: 0o755})
	body := "pwned"
	tw.WriteHeader(&tar.Header{Name: "c/evil", Typeflag: tar.TypeReg, Mode: 0o644, Size: int64(len(body))})
	tw.Write([]byte(body))
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
