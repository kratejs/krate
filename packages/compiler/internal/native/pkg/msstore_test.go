package pkg

import (
	"archive/zip"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

func storeMeta(t *testing.T) Metadata {
	m := testMeta(t)
	m.Publisher = ""
	m.WinStore = config.NativeWindowsStore{
		IdentityName: "12345Acme.AcmeNotes", PublisherID: "CN=01234567-89AB-CDEF-0123-456789ABCDEF", PublisherDisplayName: "Acme Inc",
	}
	return m
}

func winStoreOpts(t *testing.T, m Metadata, arch string) Options {
	return Options{Meta: m, Payload: testPayload(t, "windows", arch), Formats: []string{"msix-store"}, Runner: &fakeRunner{goos: "linux"},
		OutDir: filepath.Join(t.TempDir(), "packages")}
}

func TestPackageStoreMSIX(t *testing.T) {
	m := storeMeta(t)
	m.Updater = &config.NativeUpdater{URL: "https://a.b/u.json", PublicKey: "k"}
	o := winStoreOpts(t, m, "amd64")
	// The shell layout's app.json carries the updater block and permission.
	appJSON := filepath.Join(o.Payload.ExtraFiles["app"], "app.json")
	_ = os.WriteFile(appJSON, []byte(`{"appId":"com.acme.notes","updater":{"url":"https://a.b/u.json","publicKey":"k"},"permissions":["kv","updater"]}`), 0o644)
	res, err := Package(o)
	if err != nil {
		t.Fatal(err)
	}
	art := find(t, res, "msix-store")
	if !strings.HasSuffix(art.Path, "acme-notes-1.2.0-windows-amd64-store.msix") {
		t.Errorf("name %s", art.Path)
	}
	zr, err := zip.OpenReader(art.Path)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	got := map[string][]byte{}
	for _, f := range zr.File {
		rc, _ := f.Open()
		b, _ := io.ReadAll(rc)
		rc.Close()
		got[f.Name] = b
	}
	if strings.Contains(string(got["app/app.json"]), "updater") {
		t.Errorf("app.json keeps the updater: %s", got["app/app.json"])
	}
	if !strings.Contains(string(got["krate-dist.json"]), `"store": "msstore"`) {
		t.Errorf("marker = %s", got["krate-dist.json"])
	}
	if probs := CheckStoreMSIX(art.Path, o.Meta); len(probs) != 0 {
		// o.Meta has no WinStore filled by Package; use the test's.
		if probs = CheckStoreMSIX(art.Path, m); len(probs) != 0 {
			t.Errorf("problems: %v", probs)
		}
	}
	up := find(t, res, "msixupload")
	uz, err := zip.OpenReader(up.Path)
	if err != nil {
		t.Fatal(err)
	}
	defer uz.Close()
	if len(uz.File) != 1 || !strings.HasSuffix(uz.File[0].Name, "-store.msix") {
		t.Errorf("upload holds %v", uz.File)
	}
	if !strings.Contains(strings.Join(res.Notes, "\n"), "native.updater is not used in a Microsoft Store build") {
		t.Errorf("notes: %v", res.Notes)
	}
	for _, a := range res.Artifacts {
		if a.Update {
			t.Errorf("store build with an update artifact: %+v", a)
		}
	}
}
