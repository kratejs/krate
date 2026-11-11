package pkg

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"encoding/xml"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/kratejs/krate/packages/compiler/internal/config"
	"github.com/kratejs/krate/packages/native/updater"
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
	if f.fail[name] {
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

func TestMacDMGUsesHdiutilOnlyOnMac(t *testing.T) {
	// Not on macOS: a note and the zip.
	res := run(t, Options{Meta: testMeta(t), Payload: testPayload(t, "darwin", "amd64"), Runner: &fakeRunner{goos: "linux"}})
	for _, a := range res.Artifacts {
		if a.Format == "dmg" {
			t.Fatal("dmg produced without hdiutil")
		}
	}
	if !strings.Contains(strings.Join(res.Notes, "\n"), "no .dmg") {
		t.Fatalf("notes %v", res.Notes)
	}
	// On macOS with hdiutil: the command runs.
	fr := &fakeRunner{goos: "darwin", tools: map[string]bool{"hdiutil": true}}
	res = run(t, Options{Meta: testMeta(t), Payload: testPayload(t, "darwin", "amd64"), Runner: fr})
	if len(fr.calls) != 1 || !strings.HasPrefix(fr.calls[0], "hdiutil create -volname Acme Notes -srcfolder ") || !strings.Contains(fr.calls[0], "-format UDZO") {
		t.Fatalf("calls %v", fr.calls)
	}
	find(t, res, "dmg")
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

func TestNSISEscaping(t *testing.T) {
	if got := nsisStr(`a "q" $x`); got != `"a $\"q$\" $$x"` {
		t.Fatal(got)
	}
}

// MSIX: the package is a zip with [Content_Types].xml first, a manifest, and
// a block map whose hashes and header sizes match the zip.
func TestMSIXStructure(t *testing.T) {
	m := testMeta(t)
	m.Installer = "msix"
	res := run(t, Options{Meta: m, Payload: testPayload(t, "windows", "arm64")})
	p := find(t, res, "msix").Path
	zr, err := zip.OpenReader(p)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	if zr.File[0].Name != "[Content_Types].xml" {
		t.Fatalf("first entry %q", zr.File[0].Name)
	}
	byName := map[string]*zip.File{}
	for _, f := range zr.File {
		byName[f.Name] = f
		if f.Method != zip.Store {
			t.Errorf("%s is compressed", f.Name)
		}
	}
	read := func(f *zip.File) []byte {
		rc, _ := f.Open()
		defer rc.Close()
		b, _ := io.ReadAll(rc)
		return b
	}
	for _, need := range []string{"AppxManifest.xml", "AppxBlockMap.xml", "Assets/StoreLogo.png", "Assets/Square44x44Logo.png", "Assets/Square150x150Logo.png", "Acme%20Notes.exe", "app/app.json"} {
		if byName[need] == nil {
			t.Errorf("missing %s (have %v)", need, keys(byName))
		}
	}
	manifest := string(read(byName["AppxManifest.xml"]))
	for _, want := range []string{
		`Name="com.acme.notes"`, `Publisher="CN=Acme Inc"`, `Version="1.2.0.0"`, `ProcessorArchitecture="arm64"`,
		`Executable="Acme Notes.exe"`, `EntryPoint="Windows.FullTrustApplication"`, `<uap:Protocol Name="acme-dev">`,
		`windows.protocol`, `runFullTrust`, `Square150x150Logo="Assets\Square150x150Logo.png"`,
	} {
		if !strings.Contains(manifest, want) {
			t.Errorf("manifest lacks %q", want)
		}
	}
	if err := xml.NewDecoder(strings.NewReader(manifest)).Decode(new(struct{})); err != nil && err != io.EOF {
		// A struct{} target ignores content; only syntax errors matter.
		if _, ok := err.(*xml.SyntaxError); ok {
			t.Errorf("manifest XML: %v", err)
		}
	}
	// Block map.
	var bm struct {
		HashMethod string `xml:"HashMethod,attr"`
		Files      []struct {
			Name    string `xml:"Name,attr"`
			Size    int    `xml:"Size,attr"`
			LfhSize int    `xml:"LfhSize,attr"`
			Blocks  []struct {
				Hash string `xml:"Hash,attr"`
				Size string `xml:"Size,attr"`
			} `xml:"Block"`
		} `xml:"File"`
	}
	if err := xml.Unmarshal(read(byName["AppxBlockMap.xml"]), &bm); err != nil {
		t.Fatal(err)
	}
	if bm.HashMethod != "http://www.w3.org/2001/04/xmlenc#sha256" {
		t.Fatal(bm.HashMethod)
	}
	raw, _ := os.ReadFile(p)
	lho := localHeaderOffsets(t, raw)
	seen := 0
	for _, f := range bm.Files {
		zname := partName(strings.ReplaceAll(f.Name, `\`, "/"))
		zf := byName[zname]
		if zf == nil {
			t.Errorf("block map names %q which is not in the package", f.Name)
			continue
		}
		seen++
		data := read(zf)
		if len(data) != f.Size {
			t.Errorf("%s: size %d, map says %d", f.Name, len(data), f.Size)
		}
		off, _ := zf.DataOffset()
		hdr := lho[zf.Name]
		if int(off-hdr) != f.LfhSize {
			t.Errorf("%s: local header is %d bytes, map says %d", f.Name, off-hdr, f.LfhSize)
		}
		if !bytes.Equal(raw[off:off+int64(len(data))], data) {
			t.Errorf("%s: stored data differs", f.Name)
		}
		wantBlocks := (len(data) + blockSize - 1) / blockSize
		if len(f.Blocks) != wantBlocks {
			t.Errorf("%s: %d blocks, want %d", f.Name, len(f.Blocks), wantBlocks)
		}
		for i, b := range f.Blocks {
			end := min((i+1)*blockSize, len(data))
			sum := sha256.Sum256(data[i*blockSize : end])
			if b.Hash != base64.StdEncoding.EncodeToString(sum[:]) {
				t.Errorf("%s block %d: hash mismatch", f.Name, i)
			}
			if b.Size != "" {
				t.Errorf("stored file has a block Size")
			}
		}
	}
	// Every package part except the two generated ones is in the map.
	if seen != len(zr.File)-2 {
		t.Errorf("block map covers %d files, package has %d parts", seen, len(zr.File))
	}
	ct := string(read(byName["[Content_Types].xml"]))
	for _, want := range []string{`Extension="png" ContentType="image/png"`, `Extension="exe"`, `/AppxManifest.xml`, `/AppxBlockMap.xml`} {
		if !strings.Contains(ct, want) {
			t.Errorf("content types lack %q", want)
		}
	}
}

// localHeaderOffsets reads the central directory of a zip and returns the
// offset of each entry's local header.
func localHeaderOffsets(t *testing.T, raw []byte) map[string]int64 {
	t.Helper()
	eocd := bytes.LastIndex(raw, []byte("PK\x05\x06"))
	if eocd < 0 {
		t.Fatal("no end of central directory")
	}
	n := int(binary.LittleEndian.Uint16(raw[eocd+10:]))
	off := int(binary.LittleEndian.Uint32(raw[eocd+16:]))
	out := map[string]int64{}
	for i := 0; i < n; i++ {
		if !bytes.Equal(raw[off:off+4], []byte("PK\x01\x02")) {
			t.Fatal("bad central directory entry")
		}
		nameLen := int(binary.LittleEndian.Uint16(raw[off+28:]))
		extraLen := int(binary.LittleEndian.Uint16(raw[off+30:]))
		commentLen := int(binary.LittleEndian.Uint16(raw[off+32:]))
		lho := int64(binary.LittleEndian.Uint32(raw[off+42:]))
		out[string(raw[off+46:off+46+nameLen])] = lho
		off += 46 + nameLen + extraLen + commentLen
	}
	return out
}

func TestMSIXNeedsPublisher(t *testing.T) {
	m := testMeta(t)
	m.Installer = "msix"
	m.Publisher = ""
	_, err := Package(Options{Meta: m, Payload: testPayload(t, "windows", "amd64"), OutDir: t.TempDir(), Runner: &fakeRunner{}})
	if pe, ok := err.(*Error); !ok || pe.Diag.Code != "KR7229" {
		t.Fatalf("%v", err)
	}
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

func TestLinuxSingleExecutableGoesToUsrBin(t *testing.T) {
	p := testPayload(t, "linux", "arm64")
	p.ExtraFiles = nil
	res := run(t, Options{Meta: testMeta(t), Payload: p, Formats: []string{"deb"}})
	deb := find(t, res, "deb").Path
	if _, err := exec.LookPath("dpkg-deb"); err != nil {
		t.Skip("no dpkg-deb")
	}
	list, _ := exec.Command("dpkg-deb", "--contents", deb).CombinedOutput()
	if !strings.Contains(string(list), "./usr/bin/acme-notes\n") && !strings.Contains(string(list), "./usr/bin/acme-notes") {
		t.Fatalf("%s", list)
	}
	if strings.Contains(string(list), "usr/lib") {
		t.Fatalf("unexpected lib dir:\n%s", list)
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

func TestRPM(t *testing.T) {
	m := testMeta(t)
	res := run(t, Options{Meta: m, Payload: testPayload(t, "linux", "arm64"), Formats: []string{"rpm"}})
	p := find(t, res, "rpm").Path
	if filepath.Base(p) != "acme-notes-1.2.0-1.aarch64.rpm" {
		t.Fatal(p)
	}
	b, _ := os.ReadFile(p)
	if binary.BigEndian.Uint32(b) != 0xedabeedb {
		t.Fatal("not an rpm (lead magic)")
	}
	for _, want := range []string{"acme-notes", "aarch64", "webkit2gtk4.1", "/usr/lib/acme-notes/app/app.json", "/usr/share/applications/com.acme.notes.desktop"} {
		if !bytes.Contains(b, []byte(want)) && want != "/usr/lib/acme-notes/app/app.json" && want != "/usr/share/applications/com.acme.notes.desktop" {
			t.Errorf("rpm lacks %q", want)
		}
	}
	// File names are stored as dir + base tags.
	for _, want := range []string{"app.json", "com.acme.notes.desktop", "/usr/lib/acme-notes/app/"} {
		if !bytes.Contains(b, []byte(want)) {
			t.Errorf("rpm lacks %q", want)
		}
	}
}

func TestAppImageNeedsTool(t *testing.T) {
	_, err := Package(Options{Meta: testMeta(t), Payload: testPayload(t, "linux", "amd64"), Formats: []string{"appimage"}, OutDir: t.TempDir(), Runner: &fakeRunner{}})
	pe, ok := err.(*Error)
	if !ok || pe.Diag.Code != "KR7221" || !strings.Contains(pe.Diag.Hint, "appimagetool") {
		t.Fatalf("%v", err)
	}
	fr := &fakeRunner{tools: map[string]bool{"appimagetool": true}}
	res := run(t, Options{Meta: testMeta(t), Payload: testPayload(t, "linux", "arm64"), Formats: []string{"appimage"}, Runner: fr})
	if len(fr.calls) != 1 || !strings.Contains(fr.calls[0], "appimagetool ") {
		t.Fatalf("%v", fr.calls)
	}
	for _, f := range []string{"AppRun", "acme-notes.desktop", "acme-notes.png", ".DirIcon", "usr/lib/acme-notes/acme-notes"} {
		if _, err := os.Stat(filepath.Join(res.Stage, "acme-notes.AppDir", f)); err != nil {
			t.Errorf("AppDir lacks %s", f)
		}
	}
}

func TestFormatValidation(t *testing.T) {
	_, err := Package(Options{Meta: testMeta(t), Payload: testPayload(t, "linux", "amd64"), Formats: []string{"msix"}, OutDir: t.TempDir()})
	if pe, ok := err.(*Error); !ok || pe.Diag.Code != "KR7222" {
		t.Fatalf("%v", err)
	}
	p := testPayload(t, "linux", "amd64")
	p.Executable = filepath.Join(t.TempDir(), "missing")
	_, err = Package(Options{Meta: testMeta(t), Payload: p, OutDir: t.TempDir()})
	if pe, ok := err.(*Error); !ok || pe.Diag.Code != "KR7220" {
		t.Fatalf("%v", err)
	}
	bad := testMeta(t)
	bad.Version = "one"
	_, err = Package(Options{Meta: bad, Payload: testPayload(t, "linux", "amd64"), OutDir: t.TempDir()})
	if pe, ok := err.(*Error); !ok || pe.Diag.Code != "KR7229" {
		t.Fatalf("%v", err)
	}
	bad = testMeta(t)
	bad.Icon = filepath.Join(t.TempDir(), "nope.png")
	_, err = Package(Options{Meta: bad, Payload: testPayload(t, "linux", "amd64"), OutDir: t.TempDir()})
	if pe, ok := err.(*Error); !ok || pe.Diag.Code != "KR7223" {
		t.Fatalf("%v", err)
	}
}

func TestUpdateArtifactsAreSignedAndFeedWritten(t *testing.T) {
	priv, pub, _ := updater.GenerateKey()
	key, _ := updater.PrivateKeyFromSeed(priv)
	m := testMeta(t)
	m.Updater = &config.NativeUpdater{URL: "https://dl.acme.com/updates/latest-{target}.json", PublicKey: pub}
	res := run(t, Options{Meta: m, Payload: testPayload(t, "linux", "amd64"), UpdaterKey: key, Notes: "bug fixes"})
	tg := find(t, res, "tar.gz")
	if tg.Signature == "" {
		t.Fatal("update artifact not signed")
	}
	feedPath := filepath.Join(filepath.Dir(tg.Path), "latest-linux-amd64.json")
	b, err := os.ReadFile(feedPath)
	if err != nil {
		t.Fatal(err)
	}
	f, err := updater.ParseFeed(b)
	if err != nil {
		t.Fatal(err)
	}
	if f.Version != "1.2.0" || f.Notes != "bug fixes" || f.URL != "acme-notes-1.2.0-linux-amd64.tar.gz" {
		t.Fatalf("%+v", f)
	}
	pk, _ := updater.DecodePublicKey(pub)
	fh, _ := os.Open(tg.Path)
	defer fh.Close()
	if err := updater.Verify(pk, updater.Meta{AppID: m.AppID, Target: "linux-amd64", Version: f.Version}, fh, f.Signature); err != nil {
		t.Fatalf("feed signature does not verify the artifact: %v", err)
	}
	if st, _ := os.Stat(tg.Path); f.Size != st.Size() {
		t.Fatalf("size %d vs %d", f.Size, st.Size())
	}
	// With a base URL the feed URL is absolute.
	res = run(t, Options{Meta: m, Payload: testPayload(t, "linux", "amd64"), UpdaterKey: key, UpdateBaseURL: "https://dl.acme.com/updates/"})
	var f2 updater.Feed
	b, _ = os.ReadFile(filepath.Join(filepath.Dir(find(t, res, "tar.gz").Path), "latest-linux-amd64.json"))
	if err := json.Unmarshal(b, &f2); err != nil {
		t.Fatal(err)
	}
	if f2.URL != "https://dl.acme.com/updates/acme-notes-1.2.0-linux-amd64.tar.gz" {
		t.Fatal(f2.URL)
	}
	// A key that does not match the configured public key is rejected.
	_, otherPub, _ := updater.GenerateKey()
	m.Updater.PublicKey = otherPub
	_, err = Package(Options{Meta: m, Payload: testPayload(t, "linux", "amd64"), UpdaterKey: key, OutDir: t.TempDir(), Runner: &fakeRunner{}})
	if pe, ok := err.(*Error); !ok || pe.Diag.Code != "KR7227" {
		t.Fatalf("%v", err)
	}
	// Configured but no key: a note, no feed.
	m.Updater.PublicKey = pub
	res = run(t, Options{Meta: m, Payload: testPayload(t, "linux", "amd64")})
	if !strings.Contains(strings.Join(res.Notes, "\n"), EnvUpdaterKey) {
		t.Fatalf("notes %v", res.Notes)
	}
}

func TestKeygenAndLoadKey(t *testing.T) {
	p := filepath.Join(t.TempDir(), "k", "updater.key")
	pub, err := GenerateUpdaterKey(p, false)
	if err != nil || pub == "" {
		t.Fatalf("%v", err)
	}
	// Windows has no mode bits; the key is protected by the profile ACLs.
	if st, _ := os.Stat(p); runtime.GOOS != "windows" && st.Mode().Perm() != 0o600 {
		t.Fatalf("key mode %v", st.Mode())
	}
	if _, err := GenerateUpdaterKey(p, false); err == nil {
		t.Fatal("overwrote an existing key")
	}
	k, err := LoadUpdaterKey(func(n string) string {
		if n == EnvUpdaterKeyFile {
			return p
		}
		return ""
	})
	if err != nil || k == nil {
		t.Fatalf("%v", err)
	}
	dp, _ := updater.DecodePublicKey(pub)
	if !dp.Equal(k.Public()) {
		t.Fatal("public key does not match")
	}
	if k, err := LoadUpdaterKey(func(string) string { return "" }); k != nil || err != nil {
		t.Fatal("empty env should give no key")
	}
	if _, err := LoadUpdaterKey(func(n string) string {
		if n == EnvUpdaterKey {
			return "not-a-key"
		}
		return ""
	}); err == nil {
		t.Fatal("bad key accepted")
	}
}

func TestRedactCommand(t *testing.T) {
	got := redactCommand("signtool", []string{"sign", "/f", "a.pfx", "/p", "hunter2", "x.exe"})
	if strings.Contains(got, "hunter2") || !strings.Contains(got, "/p ****") {
		t.Fatal(got)
	}
	if got := redactCommand("xcrun", []string{"notarytool", "--password=abc"}); strings.Contains(got, "abc") {
		t.Fatal(got)
	}
}

func TestVersions(t *testing.T) {
	v, _ := splitVersion("1.2.3-beta.1+b5")
	if v.core() != "1.2.3" || v.msix() != "1.2.3.0" || v.debian() != "1.2.3~beta.1" {
		t.Fatalf("%+v", v)
	}
	if macCategory("") != "public.app-category.utilities" || macCategory("Games") != "public.app-category.games" || macCategory("public.app-category.music") != "public.app-category.music" {
		t.Fatal("category")
	}
}

// report.json names the executable of a release build exactly; Discover
// prefers it to guessing from the product name, and carries the host/
// directory next to it.
func TestDiscoverPrefersReport(t *testing.T) {
	out := t.TempDir()
	dir := filepath.Join(out, "linux-amd64")
	if err := os.MkdirAll(filepath.Join(dir, "host"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"My-App", "host/krate-host-node"} {
		if err := os.WriteFile(filepath.Join(dir, filepath.FromSlash(f)), []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	report := `{"targets":[{"target":"linux/amd64","binary":"linux-amd64/My-App"}]}`
	if err := os.WriteFile(filepath.Join(out, "report.json"), []byte(report), 0o644); err != nil {
		t.Fatal(err)
	}
	p, err := Discover(out, "linux", "amd64", "My/App")
	if err != nil {
		t.Fatal(err)
	}
	if p.Executable != filepath.Join(dir, "My-App") || p.ExtraFiles["host"] != filepath.Join(dir, "host") {
		t.Errorf("payload = %+v", p)
	}
	// A report that names a target outside the output directory is ignored.
	bad := `{"targets":[{"target":"linux/amd64","binary":"../x"}]}`
	if err := os.WriteFile(filepath.Join(out, "report.json"), []byte(bad), 0o644); err != nil {
		t.Fatal(err)
	}
	if p, err := Discover(out, "linux", "amd64", "My-App"); err != nil || p.Executable != filepath.Join(dir, "My-App") {
		t.Errorf("fallback = %+v, %v", p, err)
	}
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

func TestNSISPathsUseTheFileSafeName(t *testing.T) {
	m := Metadata{AppID: "com.acme.app", ProductName: `..\..\Windows`, Version: "1.0.0"}
	script := NSISScript(m, Payload{OS: "windows", Arch: "amd64"}, `C:\stage`, `C:\out\setup.exe`)
	if !strings.Contains(script, `!define DIRNAME "-..-Windows"`) {
		t.Errorf("DIRNAME is not file safe:\n%s", script)
	}
	if !strings.Contains(script, `!define APPNAME "..\..\Windows"`) {
		t.Errorf("the display name must stay as configured")
	}
	for _, line := range strings.Split(script, "\n") {
		if (strings.HasPrefix(line, "InstallDir") || strings.Contains(line, "SMPROGRAMS") || strings.Contains(line, "$DESKTOP")) && strings.Contains(line, "${APPNAME}") {
			t.Errorf("path built from the display name: %s", line)
		}
	}
}
