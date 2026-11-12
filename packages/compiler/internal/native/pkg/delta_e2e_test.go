package pkg

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
	"github.com/kratejs/krate/packages/native/updater"
)

func updaterKeys(t *testing.T) (ed25519.PrivateKey, string) {
	t.Helper()
	keyPath := filepath.Join(t.TempDir(), "updater.key")
	pub, err := GenerateUpdaterKey(keyPath, false)
	if err != nil {
		t.Fatal(err)
	}
	key, err := LoadUpdaterKey(func(k string) string {
		if k == EnvUpdaterKeyFile {
			return keyPath
		}
		return ""
	})
	if err != nil || key == nil {
		t.Fatal(err)
	}
	return key, pub
}

// release packages one version; exeBody makes the executables differ.
func release(t *testing.T, goos, arch, version, exeBody string, key ed25519.PrivateKey, pub string, mut func(*Options)) *Result {
	t.Helper()
	m := testMeta(t)
	m.Version = version
	m.Updater = &config.NativeUpdater{URL: "https://dl.acme.com/updates/latest-{target}.json", PublicKey: pub}
	pl := testPayload(t, goos, arch)
	body := []byte(exeBody)
	if goos == "darwin" {
		// A Mach-O header keeps the signature check quiet; content does not matter here.
		body = append([]byte{0xcf, 0xfa, 0xed, 0xfe}, body...)
	}
	if err := os.WriteFile(pl.Executable, body, 0o755); err != nil {
		t.Fatal(err)
	}
	o := Options{Meta: m, Payload: pl, UpdaterKey: key}
	if mut != nil {
		mut(&o)
	}
	return run(t, o)
}

func bigBody(seed byte, n int) string {
	b := make([]byte, n)
	x := uint32(seed) + 1
	for i := range b {
		x = x*1664525 + 1013904223
		b[i] = byte(x>>24) % 20
	}
	return string(b)
}

func TestPackageWritesPatchesAndClientAppliesThem(t *testing.T) {
	for _, c := range []struct{ goos, arch, kind string }{
		{"linux", "amd64", updater.KindTree},
		{"windows", "amd64", updater.KindTree},
		{"darwin", "arm64", updater.KindBundle},
	} {
		t.Run(c.goos, func(t *testing.T) {
			key, pub := updaterKeys(t)
			base := bigBody(1, 60000)
			edited := []byte(base)
			for i := 50; i < len(edited); i += 700 {
				edited[i] ^= 0x33
			}
			r10 := release(t, c.goos, c.arch, "1.0.0", base, key, pub, nil)
			r11 := release(t, c.goos, c.arch, "1.1.0", string(edited), key, pub, nil)
			upd := func(r *Result) Artifact {
				for _, a := range r.Artifacts {
					if a.Update {
						return a
					}
				}
				t.Fatal("no update artifact")
				return Artifact{}
			}
			old := upd(r10)
			r12 := release(t, c.goos, c.arch, "1.2.0", string(edited)+"tail", key, pub, func(o *Options) {
				o.DeltaFrom = []string{filepath.Dir(old.Path), filepath.Dir(upd(r11).Path)}
				o.DeltaCount = 2
			})
			feedPath := filepath.Join(filepath.Dir(upd(r12).Path), "latest-"+c.goos+"-"+c.arch+".json")
			raw, err := os.ReadFile(feedPath)
			if err != nil {
				t.Fatal(err)
			}
			var feed updater.Feed
			if err := json.Unmarshal(raw, &feed); err != nil {
				t.Fatal(err)
			}
			if len(feed.Patches) != 2 {
				t.Fatalf("patches %+v\n%s", feed.Patches, raw)
			}
			if feed.Patches[0].From != "1.1.0" || feed.Patches[1].From != "1.0.0" || !strings.HasSuffix(feed.Patches[0].URL, ".krpatch") {
				t.Fatalf("patches %+v", feed.Patches)
			}
			for _, p := range feed.Patches {
				if p.Size <= 0 || p.Size >= feed.Size || p.SHA512 == "" || p.Signature == "" {
					t.Fatalf("patch %+v, full %d", p, feed.Size)
				}
			}
			// The old feed shape still parses with this reader, and a client
			// that knows nothing of patches ignores them (struct round trip).
			srv := httptest.NewServer(http.FileServer(http.Dir(filepath.Dir(upd(r12).Path))))
			defer srv.Close()

			// Install 1.0.0 the way the updater would see it and update it.
			install := t.TempDir()
			cfg := updater.Config{
				URL: srv.URL + "/latest-{target}.json", PublicKey: pub, AppID: testMeta(t).AppID, Version: "1.0.0",
				Target: c.goos + "-" + c.arch, CacheDir: t.TempDir(), StateDir: t.TempDir(), Insecure: true, GOOS: c.goos,
			}
			var exe string
			switch c.kind {
			case updater.KindBundle:
				unzip(t, old.Path, install)
				apps, _ := filepath.Glob(filepath.Join(install, "*.app"))
				if len(apps) != 1 {
					t.Fatalf("apps %v", apps)
				}
				exe = filepath.Join(apps[0], "Contents", "MacOS", "krate-shell")
				if c.goos == "darwin" {
					m, _ := filepath.Glob(filepath.Join(apps[0], "Contents", "MacOS", "*"))
					if len(m) > 0 {
						exe = m[0]
					}
				}
			default:
				if strings.HasSuffix(old.Path, ".zip") {
					unzip(t, old.Path, install)
					m, _ := filepath.Glob(filepath.Join(install, "*", "*.exe"))
					if len(m) != 1 {
						t.Fatalf("exe %v", m)
					}
					exe = m[0]
					cfg.Exe = exe
					// The updater steps into the wrapper directory, so the
					// install directory is the one holding the exe.
				} else {
					untar(t, old.Path, install)
					m, _ := filepath.Glob(filepath.Join(install, "acme-notes"))
					if len(m) != 1 {
						t.Fatalf("exe %v", m)
					}
					exe = m[0]
				}
			}
			cfg.Exe = exe
			u, err := updater.Check(context.Background(), cfg)
			if err != nil || u == nil {
				t.Fatalf("%+v %v", u, err)
			}
			if !u.HasDelta() {
				t.Fatal("no delta offered")
			}
			if err := u.Download(context.Background(), nil); err != nil {
				t.Fatal(err)
			}
			if !u.Delta() {
				t.Fatalf("fell back to the full file: %v", u.DeltaError())
			}
			if err := u.Apply(); err != nil {
				t.Fatal(err)
			}
			got, err := os.ReadFile(exe)
			if err != nil {
				t.Fatal(err)
			}
			if c.goos == "darwin" {
				got = got[4:]
			}
			if string(got) != string(edited)+"tail" {
				t.Fatalf("executable after the update has %d bytes, want %d", len(got), len(edited)+4)
			}
		})
	}
}

func TestFeedRolloutAndChannelFlags(t *testing.T) {
	key, pub := updaterKeys(t)
	r := release(t, "linux", "amd64", "1.0.0", "x", key, pub, func(o *Options) {
		o.Channel = "beta"
		o.Rollout = &updater.Rollout{Percentage: 25, RampHours: 48, Start: "2026-11-12T00:00:00Z"}
		o.Critical = true
		o.MinVersion = "0.9.0"
	})
	dir := filepath.Dir(find(t, r, "tar.gz").Path)
	feedPath := filepath.Join(dir, "latest-beta-linux-amd64.json")
	b, err := os.ReadFile(feedPath)
	if err != nil {
		t.Fatal(err)
	}
	f, err := updater.ParseFeed(b)
	if err != nil {
		t.Fatal(err)
	}
	if f.Rollout == nil || f.Rollout.Percentage != 25 || f.Rollout.RampHours != 48 || !f.Critical || f.MinVersion != "0.9.0" {
		t.Fatalf("%+v", f)
	}
	// Halting a rollout rewrites the unsigned field; the artifact signature
	// is untouched.
	if err := EditFeed(feedPath, FeedEdit{Rollout: &updater.Rollout{Percentage: 0}}); err != nil {
		t.Fatal(err)
	}
	b2, _ := os.ReadFile(feedPath)
	f2, err := updater.ParseFeed(b2)
	if err != nil || f2.Rollout.Percentage != 0 || f2.Signature != f.Signature || !f2.Critical {
		t.Fatalf("%+v %v", f2, err)
	}
	no := false
	empty := ""
	if err := EditFeed(feedPath, FeedEdit{ClearRollout: true, Critical: &no, MinVersion: &empty}); err != nil {
		t.Fatal(err)
	}
	b3, _ := os.ReadFile(feedPath)
	f3, _ := updater.ParseFeed(b3)
	if f3.Rollout != nil || f3.Critical || f3.MinVersion != "" || f3.URL != f.URL {
		t.Fatalf("%+v", f3)
	}
	if err := EditFeed(feedPath, FeedEdit{Rollout: &updater.Rollout{Percentage: 400}}); err == nil {
		t.Fatal("invalid percentage accepted")
	}
}

func unzip(t *testing.T, src, dst string) {
	t.Helper()
	zr, err := zip.OpenReader(src)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	for _, f := range zr.File {
		p := filepath.Join(dst, filepath.FromSlash(f.Name))
		switch {
		case f.Mode().IsDir():
			os.MkdirAll(p, 0o755)
		case f.Mode()&os.ModeSymlink != 0:
			rc, _ := f.Open()
			b, _ := io.ReadAll(rc)
			rc.Close()
			os.MkdirAll(filepath.Dir(p), 0o755)
			if err := os.Symlink(string(b), p); err != nil {
				t.Fatal(err)
			}
		default:
			os.MkdirAll(filepath.Dir(p), 0o755)
			rc, _ := f.Open()
			out, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode().Perm()|0o200)
			if err != nil {
				t.Fatal(err)
			}
			io.Copy(out, rc)
			out.Close()
			rc.Close()
			os.Chmod(p, f.Mode().Perm())
		}
	}
}

func untar(t *testing.T, src, dst string) {
	t.Helper()
	f, err := os.Open(src)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return
		}
		if err != nil {
			t.Fatal(err)
		}
		p := filepath.Join(dst, filepath.FromSlash(h.Name))
		switch h.Typeflag {
		case tar.TypeDir:
			os.MkdirAll(p, 0o755)
		case tar.TypeSymlink:
			os.MkdirAll(filepath.Dir(p), 0o755)
			os.Symlink(h.Linkname, p)
		case tar.TypeReg:
			os.MkdirAll(filepath.Dir(p), 0o755)
			out, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, fs.FileMode(h.Mode)|0o200)
			if err != nil {
				t.Fatal(err)
			}
			io.Copy(out, tr)
			out.Close()
			os.Chmod(p, fs.FileMode(h.Mode))
		}
	}
}
