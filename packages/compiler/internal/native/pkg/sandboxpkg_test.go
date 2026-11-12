package pkg

import (
	"archive/tar"
	"compress/gzip"
	"encoding/xml"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kratejs/krate/packages/compiler/internal/config"
)

func sandboxMeta(t *testing.T) Metadata {
	m := testMeta(t)
	m.Permissions = []string{"net", "notify", "tray", "os:secrets", "media", "dialog", "fs"}
	m.MediaUses = []string{"camera", "microphone"}
	m.Summary = "Take notes"
	m.LongDescription = "Acme Notes keeps your notes.\n\nIt works offline."
	m.License = "MIT"
	m.Homepage = "https://acme.example"
	return m
}

func TestFlatpakFinishArgsFollowPermissions(t *testing.T) {
	args := strings.Join(flatpakFinishArgs(sandboxMeta(t)), " ")
	for _, want := range []string{"--share=ipc", "--socket=wayland", "--socket=fallback-x11", "--device=dri", "--share=network",
		"--talk-name=org.freedesktop.Notifications", "--talk-name=org.kde.StatusNotifierWatcher", "--talk-name=org.freedesktop.secrets",
		"--filesystem=xdg-run/pipewire-0"} {
		if !strings.Contains(args, want) {
			t.Errorf("missing %s in %s", want, args)
		}
	}
	for _, bad := range []string{"--filesystem=home", "--filesystem=host", "--device=all", "GeoClue"} {
		if strings.Contains(args, bad) {
			t.Errorf("unexpected %s in %s", bad, args)
		}
	}
	// A quiet app asks for little.
	q := testMeta(t)
	q.Permissions = []string{"kv", "window"}
	qa := strings.Join(flatpakFinishArgs(q), " ")
	if strings.Contains(qa, "network") || strings.Contains(qa, "Notifications") || strings.Contains(qa, "StatusNotifier") {
		t.Errorf("a quiet app must not get network, notifications or tray: %s", qa)
	}
	q.Flatpak.FinishArgs = []string{"--filesystem=xdg-documents", "--share=ipc"}
	got := flatpakFinishArgs(q)
	if got[len(got)-1] != "--filesystem=xdg-documents" || strings.Count(strings.Join(got, " "), "--share=ipc") != 1 {
		t.Errorf("extra args = %v", got)
	}
}

func TestFlatpakManifestShape(t *testing.T) {
	m := sandboxMeta(t)
	b := string(FlatpakManifest(m, "      - type: dir\n        path: .\n"))
	for _, want := range []string{
		"app-id: com.acme.notes\n", "runtime: org.gnome.Platform\n", `runtime-version: "48"`, "sdk: org.gnome.Sdk\n", "command: acme-notes\n",
		"finish-args:\n", "buildsystem: simple", "/app/share/applications/com.acme.notes.desktop", "/app/share/metainfo/com.acme.notes.metainfo.xml",
		"ln -s ../lib/acme-notes/acme-notes /app/bin/acme-notes", "type: dir",
	} {
		if !strings.Contains(b, want) {
			t.Errorf("manifest lacks %q:\n%s", want, b)
		}
	}
	m.Flatpak = config.NativeFlatpak{Runtime: "org.kde.Platform", RuntimeVersion: "6.8"}
	b = string(FlatpakManifest(m, ""))
	if !strings.Contains(b, "runtime: org.kde.Platform") || !strings.Contains(b, "sdk: org.kde.Sdk") || !strings.Contains(b, `"6.8"`) {
		t.Errorf("custom runtime:\n%s", b)
	}
}

func TestAppStreamMetainfo(t *testing.T) {
	m := sandboxMeta(t)
	b := AppStreamMetainfo(m, "com.acme.notes.desktop", time.Date(2026, 11, 12, 0, 0, 0, 0, time.UTC))
	var doc struct {
		ID      string   `xml:"id"`
		License string   `xml:"project_license"`
		Summary string   `xml:"summary"`
		Paras   []string `xml:"description>p"`
		Launch  string   `xml:"launchable"`
		Release struct {
			Version string `xml:"version,attr"`
			Date    string `xml:"date,attr"`
		} `xml:"releases>release"`
		Home string `xml:"url"`
	}
	if err := xml.Unmarshal(b, &doc); err != nil {
		t.Fatalf("metainfo is not XML: %v\n%s", err, b)
	}
	if doc.ID != "com.acme.notes" || doc.License != "MIT" || doc.Summary != "Take notes" || len(doc.Paras) != 2 ||
		doc.Launch != "com.acme.notes.desktop" || doc.Release.Version != "1.2.0" || doc.Release.Date != "2026-11-12" || doc.Home != "https://acme.example" {
		t.Errorf("parsed = %+v", doc)
	}
	m.Summary, m.LongDescription, m.License, m.ProductName = "", "", "", "A & B <Notes>"
	b = AppStreamMetainfo(m, "x.desktop", time.Now())
	if err := xml.Unmarshal(b, &doc); err != nil || !strings.Contains(string(b), "LicenseRef-proprietary") || !strings.Contains(string(b), "A &amp; B &lt;Notes&gt;") {
		t.Errorf("defaults/escaping: %v\n%s", err, b)
	}
}

func linuxOpts(t *testing.T, m Metadata, r Runner, formats ...string) Options {
	return Options{Meta: m, Payload: testPayload(t, "linux", "amd64"), Formats: formats, Runner: r, OutDir: filepath.Join(t.TempDir(), "packages")}
}

func TestPackageFlatpakWithoutTools(t *testing.T) {
	r := &fakeRunner{goos: "linux"}
	o := linuxOpts(t, sandboxMeta(t), r, "flatpak")
	o.UpdateBaseURL = "https://dl.acme.example/notes/"
	res, err := Package(o)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(o.OutDir, "flatpak-amd64")
	for _, f := range []string{
		"com.acme.notes.yml", "com.acme.notes.desktop", "com.acme.notes.metainfo.xml",
		"icons/hicolor/256x256/apps/com.acme.notes.png", "files/acme-notes", "files/app/app.json",
	} {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			t.Errorf("missing %s: %v", f, err)
		}
	}
	if st, _ := os.Stat(filepath.Join(dir, "files", "acme-notes")); st == nil || st.Mode().Perm()&0o111 == 0 {
		t.Error("the executable lost its mode")
	}
	desk, _ := os.ReadFile(filepath.Join(dir, "com.acme.notes.desktop"))
	if !strings.Contains(string(desk), "Icon=com.acme.notes\n") || !strings.Contains(string(desk), "Exec=acme-notes") {
		t.Errorf("desktop:\n%s", desk)
	}
	flathub, err := os.ReadFile(filepath.Join(o.OutDir, "com.acme.notes.flathub.yml"))
	if err != nil {
		t.Fatal(err)
	}
	fs := string(flathub)
	if !strings.Contains(fs, "type: archive") || !strings.Contains(fs, `"https://dl.acme.example/notes/acme-notes-1.2.0-flatpak-sources-amd64.tar.gz"`) || !strings.Contains(fs, "only-arches:\n          - x86_64") {
		t.Errorf("flathub manifest:\n%s", fs)
	}
	src := filepath.Join(o.OutDir, "acme-notes-1.2.0-flatpak-sources-amd64.tar.gz")
	sum, _ := fileSHA256(src)
	if !strings.Contains(fs, "sha256: "+sum) || len(sum) != 64 {
		t.Errorf("sha256 mismatch: %s", sum)
	}
	// The source archive holds the same layout as the directory.
	f, _ := os.Open(src)
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	tr := tar.NewReader(gz)
	names := map[string]bool{}
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		names[h.Name] = true
	}
	if !names["files/acme-notes"] || !names["com.acme.notes.metainfo.xml"] {
		t.Errorf("archive entries: %v", names)
	}
	joined := strings.Join(res.Notes, "\n")
	if !strings.Contains(joined, "flatpak-builder was not found") || !strings.Contains(joined, "appstreamcli is not installed") {
		t.Errorf("notes:\n%s", joined)
	}
	for _, a := range res.Artifacts {
		if a.Format == "flatpak" {
			t.Error("no bundle without flatpak-builder")
		}
	}
}

func TestPackageFlatpakBuildsBundle(t *testing.T) {
	r := &fakeRunner{goos: "linux", tools: map[string]bool{"flatpak-builder": true, "flatpak": true, "appstreamcli": true}}
	o := linuxOpts(t, sandboxMeta(t), r, "flatpak")
	res, err := Package(o)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.calls) != 3 {
		t.Fatalf("calls = %v", r.calls)
	}
	if !strings.HasPrefix(r.calls[0], "appstreamcli validate --no-net ") {
		t.Errorf("validate: %s", r.calls[0])
	}
	b := r.calls[1]
	if !strings.HasPrefix(b, "flatpak-builder --force-clean --disable-rofiles-fuse --arch=x86_64 --repo=") || !strings.HasSuffix(b, "com.acme.notes.yml") {
		t.Errorf("builder: %s", b)
	}
	if !strings.HasPrefix(r.calls[2], "flatpak build-bundle --arch=x86_64 ") || !strings.HasSuffix(r.calls[2], " com.acme.notes") {
		t.Errorf("bundle: %s", r.calls[2])
	}
	if a := find(t, res, "flatpak"); !strings.HasSuffix(a.Path, "acme-notes-1.2.0-linux-amd64.flatpak") {
		t.Errorf("bundle = %s", a.Path)
	}
}

func TestPackageFlatpakFailures(t *testing.T) {
	r := &fakeRunner{goos: "linux", tools: map[string]bool{"appstreamcli": true}, fail: map[string]bool{"appstreamcli": true}}
	_, err := Package(linuxOpts(t, sandboxMeta(t), r, "flatpak"))
	if err == nil || !strings.Contains(err.Error(), "KR7302") || !strings.Contains(err.Error(), "appstreamcli rejected") {
		t.Errorf("metainfo: %v", err)
	}
	r = &fakeRunner{goos: "linux", tools: map[string]bool{"flatpak-builder": true, "flatpak": true}, fail: map[string]bool{"flatpak-builder": true}}
	_, err = Package(linuxOpts(t, sandboxMeta(t), r, "flatpak"))
	if err == nil || !strings.Contains(err.Error(), "flatpak-builder failed") || !strings.Contains(err.Error(), "flatpak install flathub org.gnome.Platform//48") {
		t.Errorf("builder: %v", err)
	}
	m := sandboxMeta(t)
	m.AppID = "com.acme"
	if _, err := Package(linuxOpts(t, m, &fakeRunner{goos: "linux"}, "flatpak")); err == nil {
		t.Error("an id with two parts is refused")
	}
}

func TestSnapcraftYAML(t *testing.T) {
	m := sandboxMeta(t)
	y := string(SnapcraftYAML(m, "amd64"))
	for _, want := range []string{
		"name: acme-notes\n", `version: "1.2.0"`, "base: core24\n", "grade: stable\n", "confinement: strict\n", `license: "MIT"`,
		"extensions: [gnome]", "command: usr/lib/acme-notes/acme-notes", "desktop: usr/share/applications/com.acme.notes.desktop",
		`GTK_USE_PORTAL: "1"`, "- libwebkit2gtk-4.1-0", "- network\n", "- desktop\n", "- wayland\n", "- x11\n", "- home\n", "- audio-playback\n",
		"- camera\n", "- audio-record\n", "- password-manager-service\n", "- unity7\n", "build-for: [amd64]", "plugin: dump",
		"  Acme Notes keeps your notes.\n", `website: "https://acme.example"`,
	} {
		if !strings.Contains(y, want) {
			t.Errorf("snapcraft.yaml lacks %q:\n%s", want, y)
		}
	}
	m.License = ""
	m.Snap = config.NativeSnap{Grade: "devel", Confinement: "devmode", Plugs: []string{"mount-observe"}}
	y = string(SnapcraftYAML(m, "arm64"))
	if !strings.Contains(y, `license: "Proprietary"`) || !strings.Contains(y, "grade: devel") || !strings.Contains(y, "- mount-observe") || !strings.Contains(y, "build-for: [arm64]") {
		t.Errorf("variants:\n%s", y)
	}
}

func TestSnapName(t *testing.T) {
	for in, want := range map[string]string{"Acme Notes": "acme-notes", "9Lives": "app-9lives", "X": "x-app", "My_App!!": "my-app"} {
		m := Metadata{ProductName: in}
		if got := snapName(m); got != want {
			t.Errorf("snapName(%q) = %q want %q", in, got, want)
		}
	}
	long := strings.Repeat("ab-", 30)
	if got := snapName(Metadata{ProductName: long}); len(got) > 40 || strings.HasSuffix(got, "-") {
		t.Errorf("long name = %q", got)
	}
}

func TestPackageSnap(t *testing.T) {
	r := &fakeRunner{goos: "linux"}
	o := linuxOpts(t, sandboxMeta(t), r, "snap")
	res, err := Package(o)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(o.OutDir, "snap-amd64")
	for _, f := range []string{"snap/snapcraft.yaml", "snap/gui/icon.png", "files/usr/lib/acme-notes/acme-notes", "files/usr/lib/acme-notes/app/app.json", "files/usr/share/applications/com.acme.notes.desktop"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			t.Errorf("missing %s", f)
		}
	}
	desk, _ := os.ReadFile(filepath.Join(dir, "files/usr/share/applications/com.acme.notes.desktop"))
	if !strings.Contains(string(desk), "Icon=${SNAP}/meta/gui/icon.png") {
		t.Errorf("desktop:\n%s", desk)
	}
	if !strings.Contains(strings.Join(res.Notes, "\n"), "snapcraft was not found") {
		t.Errorf("notes: %v", res.Notes)
	}

	r = &fakeRunner{goos: "linux", tools: map[string]bool{"snapcraft": true}}
	t.Setenv("KRATE_SNAP_DESTRUCTIVE", "1")
	res, err = Package(linuxOpts(t, sandboxMeta(t), r, "snap"))
	if err != nil {
		t.Fatal(err)
	}
	if len(r.calls) != 1 || !strings.HasPrefix(r.calls[0], "snapcraft pack --output ") || !strings.HasSuffix(r.calls[0], "--destructive-mode") {
		t.Errorf("calls = %v", r.calls)
	}
	if a := find(t, res, "snap"); !strings.HasSuffix(a.Path, "acme-notes-1.2.0-linux-amd64.snap") {
		t.Errorf("snap = %s", a.Path)
	}
	r.fail = map[string]bool{"snapcraft": true}
	if _, err = Package(linuxOpts(t, sandboxMeta(t), r, "snap")); err == nil || !strings.Contains(err.Error(), "lxd init") {
		t.Errorf("failure hint: %v", err)
	}
}

func TestLinuxFormatsDefaultsStayDeb(t *testing.T) {
	want, err := wantedFormats(Options{Meta: testMeta(t), Payload: Payload{OS: "linux"}})
	if err != nil || !want["deb"] || want["flatpak"] || want["snap"] {
		t.Errorf("default formats = %v %v", want, err)
	}
}
