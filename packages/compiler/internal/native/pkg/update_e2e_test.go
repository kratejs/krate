package pkg

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
	"github.com/kratejs/krate/packages/native/updater"
)

// keygen -> package -> updater.Check/Download against a served output dir.
func TestPackagedUpdateVerifiesWithUpdaterEndToEnd(t *testing.T) {
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
		t.Fatalf("%v", err)
	}
	m := testMeta(t)
	m.Updater = &config.NativeUpdater{URL: "https://dl.acme.com/updates/latest-{target}.json", PublicKey: pub}
	res := run(t, Options{Meta: m, Payload: testPayload(t, "linux", "amd64"), UpdaterKey: key})
	outDir := filepath.Dir(find(t, res, "tar.gz").Path)

	srv := httptest.NewServer(http.FileServer(http.Dir(outDir)))
	defer srv.Close()
	cfg := updater.Config{
		URL: srv.URL + "/latest-{target}.json", PublicKey: pub, AppID: m.AppID,
		Version: "1.0.0", Target: "linux-amd64", CacheDir: t.TempDir(), Insecure: true,
	}

	u, err := updater.Check(context.Background(), cfg)
	if err != nil || u == nil || u.Version != "1.2.0" {
		t.Fatalf("check: %+v %v", u, err)
	}
	if err := u.Download(context.Background(), nil); err != nil {
		t.Fatalf("download: %v", err)
	}

	// Running a newer version than the feed offers is not an update.
	newer := cfg
	newer.Version = "1.3.0"
	if u, err := updater.Check(context.Background(), newer); err != nil || u != nil {
		t.Fatalf("downgrade offered: %+v %v", u, err)
	}

	// Same artifact for another app or another target does not verify.
	for name, mut := range map[string]func(*updater.Config){
		"appId":  func(c *updater.Config) { c.AppID = "com.evil.app" },
		"target": func(c *updater.Config) { c.Target = "linux-arm64" },
	} {
		c := cfg
		c.CacheDir = t.TempDir()
		mut(&c)
		c.URL = srv.URL + "/latest-linux-amd64.json"
		u, err := updater.Check(context.Background(), c)
		if err != nil || u == nil {
			t.Fatalf("%s check: %+v %v", name, u, err)
		}
		if err := u.Download(context.Background(), nil); !errors.Is(err, updater.ErrBadSignature) {
			t.Errorf("%s: %v", name, err)
		}
	}

	// A feed that relabels the version is caught.
	feedPath := filepath.Join(outDir, "latest-linux-amd64.json")
	b, _ := os.ReadFile(feedPath)
	tampered := []byte(strings.Replace(string(b), `"1.2.0"`, `"9.0.0"`, 1))
	if err := os.WriteFile(feedPath, tampered, 0o644); err != nil {
		t.Fatal(err)
	}
	c := cfg
	c.CacheDir = t.TempDir()
	u, err = updater.Check(context.Background(), c)
	if err != nil || u == nil {
		t.Fatalf("%+v %v", u, err)
	}
	if err := u.Download(context.Background(), nil); !errors.Is(err, updater.ErrBadSignature) {
		t.Fatalf("relabeled version: %v", err)
	}
}
