package pkg

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"encoding/xml"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func testMeta(t *testing.T) Metadata {
	return Metadata{
		AppID: "com.acme.notes", ProductName: "Acme Notes", Version: "1.2.0",
		Icon: writeTestIcon(t, 512, 512), DeepLinks: []string{"acme", "acme-dev"},
		MacCategory: macCategory("productivity"), Publisher: "CN=Acme Inc",
	}
}

// testPayload is a shell layout: an executable plus an app directory.
func testPayload(t *testing.T, goos, arch string) Payload {
	t.Helper()
	d := t.TempDir()
	exe := filepath.Join(d, "krate-shell")
	if err := os.WriteFile(exe, []byte("#!/bin/sh\necho hi\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	app := filepath.Join(d, "app")
	if err := os.MkdirAll(filepath.Join(app, "dist"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(app, "app.json"), []byte(`{"appId":"com.acme.notes"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(app, "dist", "index.html"), []byte("<h1>hi</h1>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("index.html", filepath.Join(app, "dist", "home.html")); err != nil {
		t.Fatal(err)
	}
	return Payload{OS: goos, Arch: arch, Executable: exe, ExtraFiles: map[string]string{"app": app}}
}

type fakeRunner struct {
	tools map[string]bool
	goos  string
	calls []string
	fail  map[string]bool
	out   map[string]string // output of a failing tool (default "boom")
}

func (f *fakeRunner) LookPath(n string) (string, error) {
	if f.tools[n] {
		return "/usr/bin/" + n, nil
	}
	return "", exec.ErrNotFound
}
func (f *fakeRunner) GOOS() string { return f.goos }
func (f *fakeRunner) Run(dir string, env []string, name string, args ...string) ([]byte, error) {
	f.calls = append(f.calls, name+" "+strings.Join(args, " "))
	if !f.fail[name] && name == "snapcraft" && len(args) >= 3 && args[0] == "pack" && args[1] == "--output" {
		// snapcraft writes the package into the project directory.
		if err := os.WriteFile(filepath.Join(dir, args[2]), []byte("snap"), 0o644); err != nil {
			return nil, err
		}
	}
	if f.fail[name] {
		if o, ok := f.out[name]; ok {
			return []byte(o), exec.ErrNotFound
		}
		return []byte("boom"), exec.ErrNotFound
	}
	return nil, nil
}

func run(t *testing.T, o Options) *Result {
	t.Helper()
	o.OutDir = filepath.Join(t.TempDir(), "packages")
	if o.Runner == nil {
		o.Runner = &fakeRunner{goos: "linux"}
	}
	res, err := Package(o)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func find(t *testing.T, r *Result, format string) Artifact {
	t.Helper()
	for _, a := range r.Artifacts {
		if a.Format == format {
			return a
		}
	}
	t.Fatalf("no %s artifact in %+v", format, r.Artifacts)
	return Artifact{}
}

func zipEntries(t *testing.T, p string) map[string]*zip.File {
	t.Helper()
	zr, err := zip.OpenReader(p)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { zr.Close() })
	m := map[string]*zip.File{}
	for _, f := range zr.File {
		m[f.Name] = f
	}
	return m
}

func TestMacAppAndZip(t *testing.T) {
	res := run(t, Options{Meta: testMeta(t), Payload: testPayload(t, "darwin", "arm64")})
	app := find(t, res, "app").Path
	for _, rel := range []string{
		"Contents/Info.plist", "Contents/MacOS/Acme Notes", "Contents/Resources/icon.icns",
		"Contents/Resources/app/app.json", "Contents/Resources/app/dist/index.html", "Contents/PkgInfo",
	} {
		if _, err := os.Stat(filepath.Join(app, rel)); err != nil {
			t.Errorf("missing %s", rel)
		}
	}
	plist, _ := os.ReadFile(filepath.Join(app, "Contents/Info.plist"))
	for _, want := range []string{
		"<key>CFBundleIdentifier</key>\n\t<string>com.acme.notes</string>",
		"<key>CFBundleShortVersionString</key>\n\t<string>1.2.0</string>",
		"public.app-category.productivity", "<key>NSHighResolutionCapable</key>\n\t<true/>",
		"<key>CFBundleExecutable</key>\n\t<string>Acme Notes</string>", "<string>acme-dev</string>",
		"<key>CFBundleURLSchemes</key>", "<key>LSMinimumSystemVersion</key>",
	} {
		if !strings.Contains(string(plist), want) {
			t.Errorf("Info.plist lacks %q", want)
		}
	}
	if err := xml.Unmarshal(plist, new(struct{})); err != nil && !strings.Contains(err.Error(), "EOF") {
		t.Errorf("plist is not well-formed XML: %v", err)
	}
	// The zip keeps modes and symlinks.
	zp := find(t, res, "zip")
	if !zp.Update || !strings.HasSuffix(zp.Path, "acme-notes-1.2.0-darwin-arm64.zip") {
		t.Fatalf("%+v", zp)
	}
	es := zipEntries(t, zp.Path)
	exe := es["Acme Notes.app/Contents/MacOS/Acme Notes"]
	if exe == nil || exe.Mode().Perm() != 0o755 {
		t.Fatalf("executable mode lost: %v", exe)
	}
	link := es["Acme Notes.app/Contents/Resources/app/dist/home.html"]
	if link == nil || link.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("symlink lost: %v", link)
	}
	rc, _ := link.Open()
	target, _ := io.ReadAll(rc)
	rc.Close()
	if string(target) != "index.html" {
		t.Fatalf("symlink target %q", target)
	}
	joined := strings.Join(res.Notes, "\n")
	if !strings.Contains(joined, "ad hoc") {
		t.Fatalf("notes: %v", res.Notes)
	}
}

func TestWindowsZipAndNSIS(t *testing.T) {
	m := testMeta(t)
	m.Installer = "nsis"
	// No makensis: a KR7221 error.
	_, err := Package(Options{Meta: m, Payload: testPayload(t, "windows", "amd64"), OutDir: t.TempDir(), Runner: &fakeRunner{goos: "linux"}})
	pe, ok := err.(*Error)
	if !ok || pe.Diag.Code != "KR7221" || !strings.Contains(pe.Diag.Hint, "nsis") {
		t.Fatalf("%v", err)
	}
	fr := &fakeRunner{goos: "linux", tools: map[string]bool{"makensis": true}}
	res := run(t, Options{Meta: m, Payload: testPayload(t, "windows", "amd64"), Runner: fr})
	if len(fr.calls) != 1 || !strings.HasPrefix(fr.calls[0], "makensis -V2 ") {
		t.Fatalf("calls %v", fr.calls)
	}
	es := zipEntries(t, find(t, res, "zip").Path)
	if es["Acme Notes/Acme Notes.exe"] == nil || es["Acme Notes/app/app.json"] == nil || es["Acme Notes/icon.ico"] == nil {
		t.Fatalf("zip entries: %v", keys(es))
	}
	script, err := os.ReadFile(filepath.Join(res.Stage, "installer.nsi"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`!define APPEXE "Acme Notes.exe"`, `InstallDir "$PROGRAMFILES64\${DIRNAME}"`, `WriteUninstaller`,
		`CreateShortCut "$SMPROGRAMS\${DIRNAME}\${DIRNAME}.lnk"`, `NSISdl::download`, `F3017226-FE2A-4295-8BDF-00C3A9A7E4C5`,
		`WriteRegStr HKCR "acme" "URL Protocol" ""`, `WriteRegStr HKCR "acme-dev\shell\open\command" "" '"$INSTDIR\${APPEXE}" "%1"'`,
		`DeleteRegKey HKCR "acme"`, `Section "Uninstall"`, `File /r `, `RequestExecutionLevel admin`,
	} {
		if !strings.Contains(string(script), want) {
			t.Errorf("nsi lacks %q", want)
		}
	}
}

func keys(m map[string]*zip.File) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestLinuxTarGzAndDeb(t *testing.T) {
	m := testMeta(t)
	res := run(t, Options{Meta: m, Payload: testPayload(t, "linux", "amd64")})
	tg := find(t, res, "tar.gz")
	if !tg.Update || !strings.HasSuffix(tg.Path, "acme-notes-1.2.0-linux-amd64.tar.gz") {
		t.Fatalf("%+v", tg)
	}
	names := tarNames(t, tg.Path)
	for _, want := range []string{"acme-notes", "app/app.json", "app/dist/index.html"} {
		if !names[want] {
			t.Errorf("tar.gz lacks %s (have %v)", want, names)
		}
	}
	deb := find(t, res, "deb").Path
	if filepath.Base(deb) != "acme-notes_1.2.0_amd64.deb" {
		t.Fatal(deb)
	}
	if _, err := exec.LookPath("dpkg-deb"); err != nil {
		t.Skip("dpkg-deb not installed")
	}
	info, err := exec.Command("dpkg-deb", "--info", deb).CombinedOutput()
	if err != nil {
		t.Fatalf("dpkg-deb --info: %v\n%s", err, info)
	}
	for _, want := range []string{"Package: acme-notes", "Version: 1.2.0", "Architecture: amd64", "Depends: libwebkit2gtk-4.1-0, libgtk-3-0", "Maintainer: Acme Inc <noreply@acme.com>"} {
		if !strings.Contains(string(info), want) {
			t.Errorf("control lacks %q:\n%s", want, info)
		}
	}
	list, err := exec.Command("dpkg-deb", "--contents", deb).CombinedOutput()
	if err != nil {
		t.Fatalf("dpkg-deb --contents: %v\n%s", err, list)
	}
	for _, want := range []string{
		"./usr/lib/acme-notes/acme-notes", "./usr/lib/acme-notes/app/app.json", "./usr/share/applications/com.acme.notes.desktop",
		"./usr/share/icons/hicolor/256x256/apps/acme-notes.png", "./usr/bin/acme-notes -> ../lib/acme-notes/acme-notes",
	} {
		if !strings.Contains(string(list), want) {
			t.Errorf("contents lack %q:\n%s", want, list)
		}
	}
	// Extract and check the desktop file and the file modes.
	dest := t.TempDir()
	if out, err := exec.Command("dpkg-deb", "-x", deb, dest).CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	d, _ := os.ReadFile(filepath.Join(dest, "usr/share/applications/com.acme.notes.desktop"))
	for _, want := range []string{"Exec=/usr/bin/acme-notes %u", "MimeType=x-scheme-handler/acme;x-scheme-handler/acme-dev;", "Icon=acme-notes", "StartupWMClass=com.acme.notes", "Categories=Office;"} {
		if !strings.Contains(string(d), want) {
			t.Errorf(".desktop lacks %q:\n%s", want, d)
		}
	}
	st, _ := os.Stat(filepath.Join(dest, "usr/lib/acme-notes/acme-notes"))
	if st.Mode().Perm() != 0o755 {
		t.Errorf("exe mode %v", st.Mode())
	}
	if out, err := exec.Command("dpkg-deb", "--field", deb, "Installed-Size").CombinedOutput(); err != nil || strings.TrimSpace(string(out)) == "0" {
		t.Errorf("installed size: %q %v", out, err)
	}
}

func tarNames(t *testing.T, p string) map[string]bool {
	t.Helper()
	f, err := os.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	tr := tar.NewReader(gz)
	out := map[string]bool{}
	for {
		h, err := tr.Next()
		if err != nil {
			break
		}
		out[strings.TrimSuffix(h.Name, "/")] = true
	}
	return out
}

func TestProductNameCannotInjectOrEscape(t *testing.T) {
	for in, want := range map[string]string{"..": "App", ".": "App", " .. ": "App", "../x": "-x", "A\nB": "AB", "My App": "My App", "a.": "a"} {
		if got := fileSafe(in); got != want {
			t.Errorf("fileSafe(%q) = %q, want %q", in, got, want)
		}
	}
	m := Metadata{AppID: "com.acme.app", ProductName: "Acme\nDepends: evil-pkg", Version: "1.0.0", Publisher: "P\nPre-Depends: x"}
	ctrl := string(debControl(m, Payload{Arch: "amd64"}, 1))
	for _, line := range strings.Split(strings.TrimSpace(ctrl), "\n") {
		if strings.HasPrefix(line, "Depends: evil") || strings.HasPrefix(line, "Pre-Depends") {
			t.Errorf("injected control field %q in:\n%s", line, ctrl)
		}
	}
	if !strings.Contains(maintainer(m), "P Pre-Depends: x <") {
		t.Errorf("maintainer = %q", maintainer(m))
	}
}
