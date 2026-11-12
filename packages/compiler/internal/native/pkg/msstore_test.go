package pkg

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
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

func TestStoreManifestUsesPartnerCenterIdentity(t *testing.T) {
	m := storeMeta(t)
	m.Store = "msstore"
	b := AppxManifest(m, Payload{OS: "windows", Arch: "arm64"})
	var doc struct {
		Identity struct {
			Name, Publisher, Version, ProcessorArchitecture string `xml:",attr"`
		} `xml:"Identity"`
		Pub string `xml:"Properties>PublisherDisplayName"`
	}
	if err := xml.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, want := range []string{`Name="12345Acme.AcmeNotes"`, `Publisher="CN=01234567-89AB-CDEF-0123-456789ABCDEF"`, `Version="1.2.0.0"`, `ProcessorArchitecture="arm64"`, `<PublisherDisplayName>Acme Inc</PublisherDisplayName>`, "runFullTrust"} {
		if !strings.Contains(s, want) {
			t.Errorf("manifest lacks %s:\n%s", want, s)
		}
	}
	m.Store = ""
	m.Publisher = "CN=Acme Inc"
	if strings.Contains(string(AppxManifest(m, Payload{Arch: "amd64"})), "12345Acme") {
		t.Error("a direct MSIX must keep the derived identity")
	}
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

func TestStoreMetaValidation(t *testing.T) {
	for name, mut := range map[string]func(*Metadata){
		"name":      func(m *Metadata) { m.WinStore.IdentityName = "" },
		"publisher": func(m *Metadata) { m.WinStore.PublisherID = "" },
		"display":   func(m *Metadata) { m.WinStore.PublisherDisplayName = "" },
		"dn":        func(m *Metadata) { m.WinStore.PublisherID = "Acme Inc" },
	} {
		m := storeMeta(t)
		mut(&m)
		_, err := Package(winStoreOpts(t, m, "amd64"))
		if err == nil || !strings.Contains(err.Error(), "KR7301") || !strings.Contains(err.Error(), "Partner Center") {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestCheckStoreMSIXFindsProblems(t *testing.T) {
	m := storeMeta(t)
	m.Store = "msstore"
	pl := testPayload(t, "windows", "amd64")
	ic, _ := LoadIcon(m.Icon, m.AppID)
	inner := newTree()
	inner.addFile(winExeName(m), 0o755, pl.Executable)
	inner.addData("Update.exe", 0o755, []byte("x"))
	inner.addData("app/app.json", 0o644, []byte(`{"updater":{}}`))
	out := filepath.Join(t.TempDir(), "x.msix")
	if err := BuildMSIX(m, pl, ic, inner, out); err != nil {
		t.Fatal(err)
	}
	wrong := m
	wrong.WinStore.IdentityName = "Other.App"
	probs := strings.Join(CheckStoreMSIX(out, wrong), "\n")
	for _, want := range []string{"does not match Partner Center", "self-updater, which the Store forbids", "Update.exe is a self-updater"} {
		if !strings.Contains(probs, want) {
			t.Errorf("missing %q in:\n%s", want, probs)
		}
	}
	if CheckStoreMSIX(filepath.Join(t.TempDir(), "none"), m)[0] == "" {
		t.Error("an unreadable package is reported")
	}
}

func TestPackageStoreBundle(t *testing.T) {
	m := storeMeta(t)
	o := Options{Meta: m, Runner: &fakeRunner{goos: "linux"}, OutDir: filepath.Join(t.TempDir(), "packages")}
	res, err := PackageStoreBundle(o, testPayload(t, "windows", "amd64"), testPayload(t, "windows", "arm64"))
	if err != nil {
		t.Fatal(err)
	}
	b := find(t, res, "msixbundle")
	zr, err := zip.OpenReader(b.Path)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	files := map[string]*zip.File{}
	for _, f := range zr.File {
		files[f.Name] = f
		if f.Method != zip.Store {
			t.Errorf("%s is compressed", f.Name)
		}
	}
	for _, n := range []string{"[Content_Types].xml", "AppxMetaData/AppxBundleManifest.xml", "AppxBlockMap.xml", "12345Acme.AcmeNotes_1.2.0.0_x64.msix", "12345Acme.AcmeNotes_1.2.0.0_arm64.msix"} {
		if files[n] == nil {
			t.Fatalf("bundle lacks %s: %v", n, zr.File)
		}
	}
	rc, _ := files["AppxMetaData/AppxBundleManifest.xml"].Open()
	manBytes, _ := io.ReadAll(rc)
	rc.Close()
	var man struct {
		Identity struct {
			Name, Publisher, Version string `xml:",attr"`
		} `xml:"Identity"`
		Pkgs []struct {
			Architecture string `xml:"Architecture,attr"`
			FileName     string `xml:"FileName,attr"`
			Offset       int64  `xml:"Offset,attr"`
			Size         int64  `xml:"Size,attr"`
			Type         string `xml:"Type,attr"`
		} `xml:"Packages>Package"`
	}
	if err := xml.Unmarshal(manBytes, &man); err != nil {
		t.Fatal(err)
	}
	if man.Identity.Name != "12345Acme.AcmeNotes" || len(man.Pkgs) != 2 || man.Pkgs[0].Architecture != "arm64" || man.Pkgs[1].Architecture != "x64" {
		t.Fatalf("manifest = %+v", man)
	}
	raw, _ := os.ReadFile(b.Path)
	for _, p := range man.Pkgs {
		if p.Type != "application" {
			t.Errorf("type %s", p.Type)
		}
		f := files[p.FileName]
		rc, _ := f.Open()
		want, _ := io.ReadAll(rc)
		rc.Close()
		if int64(len(want)) != p.Size || !bytes.Equal(raw[p.Offset:p.Offset+p.Size], want) {
			t.Errorf("%s: offset %d size %d does not point at the package bytes", p.FileName, p.Offset, p.Size)
		}
		if got, _ := f.DataOffset(); got != p.Offset {
			t.Errorf("%s: DataOffset %d != manifest offset %d", p.FileName, got, p.Offset)
		}
	}
	up := find(t, res, "msixupload")
	if !strings.HasSuffix(up.Path, "windows-universal.msixupload") {
		t.Errorf("upload %s", up.Path)
	}
	uz, _ := zip.OpenReader(up.Path)
	defer uz.Close()
	if len(uz.File) != 1 || !strings.HasSuffix(uz.File[0].Name, ".msixbundle") {
		t.Errorf("upload holds %v", uz.File)
	}
}
